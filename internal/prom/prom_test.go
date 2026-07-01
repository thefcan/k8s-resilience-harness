package prom

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
)

// fakeQuerier returns canned scalars, dispatching on a distinctive token in each
// query so a single fake can serve the whole CollectServerView sequence.
type fakeQuerier struct {
	up, served, errs, redis float64
	returnEmpty             bool
	wrongType               bool
	err                     error
	calls                   []string
}

func (f *fakeQuerier) Query(_ context.Context, query string, _ time.Time) (model.Value, error) {
	f.calls = append(f.calls, query)
	switch {
	case f.err != nil:
		return nil, f.err
	case f.wrongType:
		return &model.Scalar{Value: 1}, nil
	case f.returnEmpty:
		return model.Vector{}, nil
	}
	var v float64
	switch {
	case strings.Contains(query, "count(up{"):
		v = f.up
	case strings.Contains(query, `code=~"5.."`):
		v = f.errs
	case strings.Contains(query, "testapp_requests_total"):
		v = f.served
	case strings.Contains(query, "testapp_redis_up"):
		v = f.redis
	}
	return model.Vector{{Value: model.SampleValue(v)}}, nil
}

func TestCollectServerViewHappyPath(t *testing.T) {
	f := &fakeQuerier{up: 3, served: 750, errs: 0, redis: 1}
	v, err := CollectServerView(context.Background(), f, "testapp", time.Unix(1000, 0), 15*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.TargetsUp != 3 || v.RequestsServed != 750 || v.ServerErrors != 0 || v.MinRedisUp != 1 {
		t.Fatalf("view = %+v, want {3 750 0 1}", v)
	}
	joined := strings.Join(f.calls, "\n")
	if !strings.Contains(joined, "[15s]") {
		t.Errorf("expected the fault duration rendered as [15s] in the range queries:\n%s", joined)
	}
	if !strings.Contains(joined, `job="testapp"`) {
		t.Errorf("expected the job label in queries:\n%s", joined)
	}
}

func TestCollectServerViewPartition(t *testing.T) {
	// Redis gone: min redis_up collapses to 0 and 5xx dominate — the FAIL story
	// as Prometheus sees it, independent of the client-side probe.
	f := &fakeQuerier{up: 3, served: 1000, errs: 994, redis: 0}
	v, err := CollectServerView(context.Background(), f, "testapp", time.Unix(1000, 0), 20*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.MinRedisUp != 0 {
		t.Errorf("MinRedisUp = %v, want 0 (the partition)", v.MinRedisUp)
	}
	if v.ServerErrors != 994 {
		t.Errorf("ServerErrors = %v, want 994", v.ServerErrors)
	}
}

func TestCollectServerViewEmptyIsZero(t *testing.T) {
	f := &fakeQuerier{returnEmpty: true}
	v, err := CollectServerView(context.Background(), f, "testapp", time.Unix(1, 0), time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != (ServerView{}) {
		t.Fatalf("empty query results should yield a zero view, got %+v", v)
	}
}

func TestCollectServerViewPropagatesError(t *testing.T) {
	f := &fakeQuerier{err: errors.New("prometheus unreachable")}
	if _, err := CollectServerView(context.Background(), f, "testapp", time.Unix(1, 0), time.Second); err == nil {
		t.Fatal("expected the query error to propagate")
	}
}

func TestCollectServerViewRejectsNonVector(t *testing.T) {
	f := &fakeQuerier{wrongType: true}
	if _, err := CollectServerView(context.Background(), f, "testapp", time.Unix(1, 0), time.Second); err == nil {
		t.Fatal("expected an error when Prometheus returns a non-vector result")
	}
}

func TestRangeStr(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{15 * time.Second, "15s"},
		{20 * time.Second, "20s"},
		{0, "1s"},                       // clamped to a valid range
		{1500 * time.Millisecond, "2s"}, // rounds to nearest second
	}
	for _, c := range cases {
		if got := rangeStr(c.d); got != c.want {
			t.Errorf("rangeStr(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
