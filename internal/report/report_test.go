package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thefcan/k8s-resilience-harness/internal/metrics"
)

var thresholds = Thresholds{MinSuccessRate: 0.95, MaxP95Ms: 300, RecoveryTimeoutSeconds: 30}

func TestVerdictPasses(t *testing.T) {
	fw := metrics.Summary{Total: 1000, Succeeded: 1000, SuccessRate: 1.0, P95ms: 50}
	v := BuildVerdict(thresholds, fw, true, 0)
	if !v.Pass {
		t.Fatalf("expected pass, got fail: %v", v.Reasons)
	}
}

func TestVerdictFailsOnLowSuccessRate(t *testing.T) {
	fw := metrics.Summary{Total: 1000, Succeeded: 800, SuccessRate: 0.80, P95ms: 50}
	v := BuildVerdict(thresholds, fw, true, 0)
	if v.Pass {
		t.Fatal("expected fail on low success rate")
	}
	if !strings.Contains(strings.Join(v.Reasons, " "), "success rate") {
		t.Fatalf("reasons should mention success rate: %v", v.Reasons)
	}
}

func TestVerdictFailsOnHighP95(t *testing.T) {
	fw := metrics.Summary{Total: 1000, Succeeded: 1000, SuccessRate: 1.0, P95ms: 500}
	if BuildVerdict(thresholds, fw, true, 0).Pass {
		t.Fatal("expected fail on high p95")
	}
}

func TestVerdictFailsWhenNotRecovered(t *testing.T) {
	fw := metrics.Summary{Total: 1000, Succeeded: 1000, SuccessRate: 1.0, P95ms: 50}
	if BuildVerdict(thresholds, fw, false, 0).Pass {
		t.Fatal("expected fail when not recovered")
	}
}

func TestVerdictFailsOnSlowRecovery(t *testing.T) {
	fw := metrics.Summary{Total: 1000, Succeeded: 1000, SuccessRate: 1.0, P95ms: 50}
	v := BuildVerdict(thresholds, fw, true, 45)
	if v.Pass {
		t.Fatal("expected fail when recovery exceeds timeout")
	}
}

func TestVerdictFailsOnEmptyWindow(t *testing.T) {
	if BuildVerdict(thresholds, metrics.Summary{}, true, 0).Pass {
		t.Fatal("expected fail when no requests were recorded")
	}
}

func TestHumanContainsVerdict(t *testing.T) {
	fw := metrics.Summary{Total: 10, Succeeded: 10, SuccessRate: 1.0, P95ms: 5}
	r := Report{
		Experiment:  "x",
		Fault:       "pod-kill",
		Affected:    []string{"p1"},
		FaultWindow: fw,
		Verdict:     BuildVerdict(thresholds, fw, true, 0),
	}
	if !strings.Contains(r.Human(), "PASS") {
		t.Fatalf("human report missing verdict:\n%s", r.Human())
	}
}

func TestWriteRoundTripsReportAsJSON(t *testing.T) {
	fw := metrics.Summary{Total: 100, Succeeded: 99, SuccessRate: 0.99, P95ms: 42}
	want := Report{
		Experiment:      "roundtrip",
		Fault:           "node-drain",
		Affected:        []string{"testapp-1", "testapp-2"},
		Thresholds:      thresholds,
		FaultWindow:     fw,
		RecoverySeconds: 1.5,
		Recovered:       true,
		Verdict:         BuildVerdict(thresholds, fw, true, 1.5),
	}
	// A nested path exercises the MkdirAll branch.
	path := filepath.Join(t.TempDir(), "nested", "run.json")
	if err := Write(path, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("written report should end with a trailing newline")
	}
	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal written report: %v", err)
	}
	if got.Experiment != want.Experiment || got.Fault != want.Fault {
		t.Errorf("round-trip mismatch: got %q/%q, want %q/%q", got.Experiment, got.Fault, want.Experiment, want.Fault)
	}
	if len(got.Affected) != 2 || got.Affected[0] != "testapp-1" {
		t.Errorf("affected did not round-trip: %v", got.Affected)
	}
	if got.Verdict.Pass != want.Verdict.Pass {
		t.Errorf("verdict did not round-trip: got %v, want %v", got.Verdict.Pass, want.Verdict.Pass)
	}
	if got.FaultWindow.SuccessRate != want.FaultWindow.SuccessRate {
		t.Errorf("fault-window metrics did not round-trip: got %v, want %v", got.FaultWindow.SuccessRate, want.FaultWindow.SuccessRate)
	}
}
