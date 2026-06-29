package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/thefcan/k8s-resilience-harness/internal/experiment"
)

type fakeKiller struct {
	called atomic.Int32
	names  []string
	err    error
}

func (f *fakeKiller) Kill(_ context.Context, _ int) ([]string, error) {
	f.called.Add(1)
	return f.names, f.err
}

func testExperiment(target string) *experiment.Experiment {
	return &experiment.Experiment{
		Name:        "t",
		Target:      target,
		Probe:       experiment.Probe{Path: "/", RPS: 100, Concurrency: 10, TimeoutMs: 1000},
		SteadyState: experiment.SteadyState{MinSuccessRate: 0.95, MaxP95Ms: 500},
		Fault:       experiment.Fault{Type: experiment.FaultPodKill, Namespace: "ns", Selector: "app=x", Count: 1},
		Phases:      experiment.Phases{BaselineSeconds: 0, FaultSeconds: 1, RecoveryTimeoutSeconds: 5},
	}
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestExecutePassesAgainstHealthyServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	killer := &fakeKiller{names: []string{"p1"}}
	rep, err := execute(context.Background(), discardLog(), testExperiment(ts.URL), killer)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if killer.called.Load() != 1 {
		t.Fatalf("killer called %d times, want 1", killer.called.Load())
	}
	if !rep.Verdict.Pass {
		t.Fatalf("expected pass, reasons: %v", rep.Verdict.Reasons)
	}
	if len(rep.KilledPods) != 1 || rep.KilledPods[0] != "p1" {
		t.Fatalf("killed pods = %v, want [p1]", rep.KilledPods)
	}
	if rep.FaultWindow.Total == 0 {
		t.Fatal("expected requests recorded during the fault window")
	}
}

func TestExecuteFailsAgainstBrokenServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	rep, err := execute(context.Background(), discardLog(), testExperiment(ts.URL), &fakeKiller{names: []string{"p1"}})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rep.Verdict.Pass {
		t.Fatalf("expected fail against a 503 server, reasons: %v", rep.Verdict.Reasons)
	}
}
