package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsEndpointExposesInstruments(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.routes()
	doGET(t, h, "/work") // generate at least one series

	rec := doGET(t, h, "/metrics")
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics code = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"testapp_requests_total",
		"testapp_request_duration_seconds",
		"testapp_inflight_requests",
		"testapp_redis_up",
		"go_goroutines", // the runtime collector is registered too
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics exposition missing %q", want)
		}
	}
}

func TestRequestsCounterCountsByRouteAndCode(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.routes()
	for i := 0; i < 3; i++ {
		doGET(t, h, "/work")
	}
	if got := testutil.ToFloat64(srv.metrics.requests.WithLabelValues("/work", "200")); got != 3 {
		t.Fatalf(`requests_total{path="/work",code="200"} = %v, want 3`, got)
	}
}

func TestRedisUpGaugeReflectsBackend(t *testing.T) {
	srv, mr := newTestServer(t)
	h := srv.routes()

	doGET(t, h, "/work")
	if got := testutil.ToFloat64(srv.metrics.redisUp); got != 1 {
		t.Fatalf("redis_up after a successful op = %v, want 1", got)
	}

	mr.Close() // simulate a backend outage
	doGET(t, h, "/work")
	if got := testutil.ToFloat64(srv.metrics.redisUp); got != 0 {
		t.Fatalf("redis_up after a failed op = %v, want 0", got)
	}
}
