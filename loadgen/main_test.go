package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestGeneratorAllSuccess(t *testing.T) {
	var hits int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	gen := newGenerator(Config{
		Target:      ts.URL,
		RPS:         100,
		Duration:    300 * time.Millisecond,
		Concurrency: 10,
		Timeout:     time.Second,
	})
	res := gen.Run(context.Background())

	if len(res.samples) == 0 {
		t.Fatal("expected some samples, got 0")
	}
	for _, s := range res.samples {
		if !s.OK {
			t.Fatalf("expected all samples OK against a 200 server, got a failure: %+v", s)
		}
	}
	// Roughly rps * duration; allow generous slack for scheduling jitter.
	if len(res.samples) < 10 {
		t.Fatalf("dispatched too few requests: %d", len(res.samples))
	}
	// A fast 200 server should not saturate a 10-worker pool at 100 rps.
	if res.saturated != 0 {
		t.Fatalf("unexpected saturation against a fast server: %d", res.saturated)
	}
	if res.requests != len(res.samples) {
		t.Fatalf("requests (%d) should equal samples (%d) with no saturation", res.requests, len(res.samples))
	}
}

// TestGeneratorHighVolumeDoesNotDeadlock is a regression test: an earlier
// version buffered results and only drained them after dispatch finished, so a
// run producing more samples than the buffer deadlocked. Here the sample count
// (~rps*duration) far exceeds the internal buffer (== concurrency), and Run
// must still return promptly.
func TestGeneratorHighVolumeDoesNotDeadlock(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	gen := newGenerator(Config{
		Target:      ts.URL,
		RPS:         500,
		Duration:    400 * time.Millisecond,
		Concurrency: 4, // tiny pool + buffer relative to ~200 samples
		Timeout:     time.Second,
	})

	done := make(chan int, 1)
	go func() { done <- len(gen.Run(context.Background()).samples) }()

	select {
	case n := <-done:
		if n < 20 {
			t.Fatalf("expected many samples, got %d", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return — likely deadlocked")
	}
}

func TestGeneratorCountsNon200AsFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	gen := newGenerator(Config{
		Target:      ts.URL,
		RPS:         50,
		Duration:    200 * time.Millisecond,
		Concurrency: 5,
		Timeout:     time.Second,
	})
	res := gen.Run(context.Background())
	if len(res.samples) == 0 {
		t.Fatal("expected some samples, got 0")
	}
	for _, s := range res.samples {
		if s.OK {
			t.Fatal("expected all samples to fail against a 503 server")
		}
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	var out, errBuf devNull
	if err := run([]string{"-rps=0"}, &out, &errBuf); err == nil {
		t.Fatal("expected error for rps=0")
	}
}

func TestWriteReportRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "baseline.json")
	want := Report{Target: "http://x/work", RequestedRPS: 10, AchievedRPS: 9.9}
	if err := writeReport(path, want); err != nil {
		t.Fatalf("writeReport: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Target != want.Target || got.RequestedRPS != want.RequestedRPS {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}
}

// devNull is a throwaway io.Writer for tests that ignore output.
type devNull struct{}

func (devNull) Write(p []byte) (int, error) { return len(p), nil }
