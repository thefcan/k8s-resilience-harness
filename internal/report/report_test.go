package report

import (
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
		KilledPods:  []string{"p1"},
		FaultWindow: fw,
		Verdict:     BuildVerdict(thresholds, fw, true, 0),
	}
	if !strings.Contains(r.Human(), "PASS") {
		t.Fatalf("human report missing verdict:\n%s", r.Human())
	}
}
