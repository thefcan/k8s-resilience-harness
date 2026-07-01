package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metrics holds the Prometheus instruments the SUT exposes at /metrics. Each
// instance owns its own registry, so tests stay isolated and no global default
// registry state leaks between servers.
//
// These server-side signals are what the harness cross-checks against its own
// client-side probe in M4: testapp_requests_total / _redis_up let a run confirm
// (or contradict) the verdict from the cluster's point of view.
type metrics struct {
	reg      *prometheus.Registry
	requests *prometheus.CounterVec   // labels: path, code
	duration *prometheus.HistogramVec // labels: path
	inflight prometheus.Gauge
	redisUp  prometheus.Gauge
}

func newMetrics() *metrics {
	m := &metrics{
		reg: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "testapp_requests_total",
			Help: "Total HTTP requests handled, by route and status code.",
		}, []string{"path", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "testapp_request_duration_seconds",
			Help:    "HTTP request latency in seconds, by route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"path"}),
		inflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "testapp_inflight_requests",
			Help: "In-flight HTTP requests.",
		}),
		redisUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "testapp_redis_up",
			Help: "1 if the last Redis operation succeeded, 0 otherwise.",
		}),
	}
	m.reg.MustRegister(
		m.requests, m.duration, m.inflight, m.redisUp,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	m.redisUp.Set(1) // optimistic until the first probe corrects it
	return m
}

// handler serves the Prometheus exposition for this metrics set.
func (m *metrics) handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// instrument wraps h so every request is counted (by route + status code),
// timed, and reflected in the in-flight gauge.
func (m *metrics) instrument(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.inflight.Inc()
		defer m.inflight.Dec()

		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		start := time.Now()
		h.ServeHTTP(rec, r)

		path := routeLabel(r.URL.Path)
		m.duration.WithLabelValues(path).Observe(time.Since(start).Seconds())
		m.requests.WithLabelValues(path, strconv.Itoa(rec.code)).Inc()
	})
}

// setRedisUp records the outcome of a Redis operation as a 0/1 gauge.
func (m *metrics) setRedisUp(ok bool) {
	if ok {
		m.redisUp.Set(1)
		return
	}
	m.redisUp.Set(0)
}

// routeLabel bounds request-metric label cardinality to the known route set.
func routeLabel(p string) string {
	switch p {
	case "/livez", "/healthz", "/work", "/metrics", "/":
		return p
	default:
		return "other"
	}
}

// statusRecorder captures the response status code for metrics without altering
// the response written to the client.
type statusRecorder struct {
	http.ResponseWriter
	code    int
	written bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.written {
		r.code = code
		r.written = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.written = true // an implicit 200 if WriteHeader was never called
	return r.ResponseWriter.Write(b)
}
