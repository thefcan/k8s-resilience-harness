// Command testapp is the System Under Test (SUT) for the resilience harness.
//
// It is a small, horizontally-scaled HTTP service backed by Redis. It exists to
// be deployed into a kind cluster and then disrupted (pods killed, nodes
// drained, network degraded) while the harness checks whether a steady-state
// hypothesis still holds.
//
// Probe design follows Kubernetes best practice:
//   - /livez    liveness  — always 200 while the process is up; MUST NOT depend
//     on Redis, or a backend outage would make Kubernetes kill otherwise-healthy
//     pods instead of just marking them not-ready.
//   - /healthz  readiness — 200 only when Redis is reachable, so traffic drains
//     away from a replica that cannot serve real work.
//   - /work     a tiny Redis-backed unit of work (atomic INCR).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/thefcan/k8s-resilience-harness/internal/logger"
)

const workCounterKey = "kresil:work:count"

// drainDelay is how long we keep serving in-flight traffic after SIGTERM while
// readiness reports not-ready, so kube-proxy removes this pod from the Service
// endpoints before we stop the listener. Without it, pod deletion (M2 pod-kill)
// would briefly produce connection-refused errors that have nothing to do with
// the system's actual resilience.
const drainDelay = 3 * time.Second

func main() {
	log := logger.New(os.Stderr, parseLevel(), logger.FormatJSON)

	redisAddr := getenv("REDIS_ADDR", "localhost:6379")
	rdb := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	defer func() { _ = rdb.Close() }()

	host, _ := os.Hostname()
	srv := &server{rdb: rdb, host: host, log: log}
	srv.ready.Store(true)

	addr := ":" + getenv("PORT", "8080")
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		log.Info("testapp listening", "addr", addr, "pod", host, "redis_addr", redisAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()

	// Drain: fail readiness, wait for endpoint removal to propagate, then stop.
	log.Info("shutting down: draining", "pod", host, "drain", drainDelay.String())
	srv.ready.Store(false)
	time.Sleep(drainDelay)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
	}
}

type server struct {
	rdb   *redis.Client
	host  string
	log   *slog.Logger
	ready atomic.Bool
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", s.livez)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /work", s.work)
	mux.HandleFunc("GET /", s.root)
	return mux
}

// livez is liveness: the process is running. Independent of Redis by design.
func (s *server) livez(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "alive", "pod": s.host})
}

// healthz is readiness: we are not draining and we can reach Redis, so we can
// serve real work. Error details are logged server-side, not leaked to callers.
func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	if !s.ready.Load() {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "draining", "pod": s.host})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()
	if err := s.rdb.Ping(ctx).Err(); err != nil {
		s.log.Warn("readiness check failed", "error", err, "pod", s.host)
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "pod": s.host})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "pod": s.host})
}

// work performs a minimal Redis-backed unit of work: an atomic counter bump.
func (s *server) work(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	n, err := s.rdb.Incr(ctx, workCounterKey).Result()
	if err != nil {
		s.log.Warn("work failed", "error", err, "pod", s.host)
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "error", "pod": s.host})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "pod": s.host, "count": n})
}

func (s *server) root(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"service": "testapp",
		"pod":     s.host,
		"routes":  []string{"/livez", "/healthz", "/work"},
	})
}

func (s *server) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.log.Error("encode response failed", "error", err)
	}
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func parseLevel() slog.Level {
	lvl, err := logger.ParseLevel(getenv("LOG_LEVEL", "info"))
	if err != nil {
		return slog.LevelInfo
	}
	return lvl
}
