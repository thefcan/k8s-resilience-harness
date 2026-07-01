// Package prom queries an in-cluster Prometheus for the server-side view of a
// fault window, so a run can corroborate its own client-side probe against what
// the cluster actually observed. The Querier interface keeps the collection
// logic testable without a live Prometheus.
package prom

import (
	"context"
	"fmt"
	"math"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// Querier runs instant PromQL queries at a given evaluation time.
type Querier interface {
	Query(ctx context.Context, query string, ts time.Time) (model.Value, error)
}

// Client is a thin adapter over the official Prometheus HTTP API client.
type Client struct{ api promv1.API }

// NewClient builds a Prometheus API client for baseURL (e.g. http://localhost:30090).
func NewClient(baseURL string) (*Client, error) {
	c, err := promapi.NewClient(promapi.Config{Address: baseURL})
	if err != nil {
		return nil, fmt.Errorf("prometheus client: %w", err)
	}
	return &Client{api: promv1.NewAPI(c)}, nil
}

// Query runs an instant query, discarding non-fatal warnings.
func (c *Client) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	val, _, err := c.api.Query(ctx, query, ts, promv1.WithTimeout(10*time.Second))
	return val, err
}

// ServerView is the Prometheus-scraped picture of a fault window, used to
// corroborate the harness's own client-side measurements.
type ServerView struct {
	// TargetsUp is how many testapp pods Prometheus saw as up at the window end.
	TargetsUp int `json:"targets_up"`
	// RequestsServed is the server-counted increase in handled requests over the
	// window (vs. the probe's client-side count).
	RequestsServed float64 `json:"requests_served"`
	// ServerErrors is the increase in 5xx responses over the window.
	ServerErrors float64 `json:"server_errors"`
	// MinRedisUp is the minimum testapp_redis_up across pods over the window:
	// 0 means at least one pod saw its Redis dependency go away (the partition).
	MinRedisUp float64 `json:"min_redis_up"`
}

// CollectServerView evaluates the window-scoped PromQL at faultEnd. window is
// the fault duration; queries use it as the range so the result reflects the
// fault window rather than the recovery phase that follows it.
func CollectServerView(ctx context.Context, q Querier, job string, faultEnd time.Time, window time.Duration) (ServerView, error) {
	w := rangeStr(window)
	var v ServerView

	up, err := scalar(ctx, q, fmt.Sprintf(`count(up{job=%q} == 1)`, job), faultEnd)
	if err != nil {
		return v, fmt.Errorf("query targets up: %w", err)
	}
	v.TargetsUp = int(math.Round(up))

	served, err := scalar(ctx, q, fmt.Sprintf(`sum(increase(testapp_requests_total{job=%q}[%s]))`, job, w), faultEnd)
	if err != nil {
		return v, fmt.Errorf("query requests served: %w", err)
	}
	v.RequestsServed = served

	errs, err := scalar(ctx, q, fmt.Sprintf(`sum(increase(testapp_requests_total{job=%q,code=~"5.."}[%s]))`, job, w), faultEnd)
	if err != nil {
		return v, fmt.Errorf("query server errors: %w", err)
	}
	v.ServerErrors = errs

	redis, err := scalar(ctx, q, fmt.Sprintf(`min(min_over_time(testapp_redis_up{job=%q}[%s]))`, job, w), faultEnd)
	if err != nil {
		return v, fmt.Errorf("query redis up: %w", err)
	}
	v.MinRedisUp = redis

	return v, nil
}

// scalar runs an instant query expected to return a single value and extracts
// it. An empty result (no matching series) is reported as 0, which is the right
// default for the count/increase aggregations used here.
func scalar(ctx context.Context, q Querier, query string, ts time.Time) (float64, error) {
	val, err := q.Query(ctx, query, ts)
	if err != nil {
		return 0, err
	}
	vec, ok := val.(model.Vector)
	if !ok {
		return 0, fmt.Errorf("query %q: expected instant vector, got %s", query, val.Type())
	}
	if len(vec) == 0 {
		return 0, nil
	}
	return float64(vec[0].Value), nil
}

// rangeStr renders a duration as a PromQL range like "15s".
func rangeStr(d time.Duration) string {
	secs := int(math.Round(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return fmt.Sprintf("%ds", secs)
}
