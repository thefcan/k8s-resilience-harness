package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestServer wires the server against an in-memory miniredis so the real
// go-redis code path is exercised without a live Redis.
func newTestServer(t *testing.T) (*server, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	srv := &server{rdb: rdb, host: "test-pod", log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return srv, mr
}

func doGET(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	h.ServeHTTP(rec, req)
	return rec
}

func TestLivezAlwaysAlive(t *testing.T) {
	srv, mr := newTestServer(t)
	mr.Close() // even with Redis down, liveness must stay green

	rec := doGET(t, srv.routes(), "/livez")
	if rec.Code != http.StatusOK {
		t.Fatalf("/livez code = %d, want 200 (liveness must not depend on Redis)", rec.Code)
	}
}

func TestHealthzReflectsRedis(t *testing.T) {
	srv, mr := newTestServer(t)

	if rec := doGET(t, srv.routes(), "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("/healthz with Redis up = %d, want 200", rec.Code)
	}

	mr.Close() // simulate backend outage
	if rec := doGET(t, srv.routes(), "/healthz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/healthz with Redis down = %d, want 503", rec.Code)
	}
}

func TestWorkIncrementsCounter(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.routes()

	for want := int64(1); want <= 3; want++ {
		rec := doGET(t, h, "/work")
		if rec.Code != http.StatusOK {
			t.Fatalf("/work code = %d, want 200", rec.Code)
		}
		var body struct {
			Status string `json:"status"`
			Count  int64  `json:"count"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode /work body: %v", err)
		}
		if body.Status != "ok" || body.Count != want {
			t.Fatalf("/work body = %+v, want status=ok count=%d", body, want)
		}
	}
}

func TestWorkFailsWhenRedisDown(t *testing.T) {
	srv, mr := newTestServer(t)
	mr.Close()

	rec := doGET(t, srv.routes(), "/work")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/work with Redis down = %d, want 503", rec.Code)
	}
}
