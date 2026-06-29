// Package report assembles the experiment outcome — baseline vs. fault-window
// metrics, recovery time, and a pass/fail verdict against the steady-state
// hypothesis — and renders it as JSON and a human-readable summary.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/thefcan/k8s-resilience-harness/internal/metrics"
)

// Thresholds are the steady-state conditions the fault window is judged against.
type Thresholds struct {
	MinSuccessRate         float64 `json:"min_success_rate"`
	MaxP95Ms               float64 `json:"max_p95_ms"`
	RecoveryTimeoutSeconds float64 `json:"recovery_timeout_seconds"`
}

// Verdict is the pass/fail outcome with human-readable reasons.
type Verdict struct {
	Pass    bool     `json:"pass"`
	Reasons []string `json:"reasons"`
}

// Report is the full experiment artifact written to results/.
type Report struct {
	Experiment      string          `json:"experiment"`
	StartedAt       string          `json:"started_at"`
	Fault           string          `json:"fault"`
	KilledPods      []string        `json:"killed_pods"`
	Thresholds      Thresholds      `json:"thresholds"`
	Baseline        metrics.Summary `json:"baseline"`
	FaultWindow     metrics.Summary `json:"fault_window"`
	RecoverySeconds float64         `json:"recovery_seconds"`
	Recovered       bool            `json:"recovered"`
	Verdict         Verdict         `json:"verdict"`
}

// BuildVerdict judges the fault-window metrics and recovery against the
// thresholds. The verdict passes only if every condition holds.
func BuildVerdict(th Thresholds, faultWindow metrics.Summary, recovered bool, recoverySeconds float64) Verdict {
	var reasons []string
	if faultWindow.Total == 0 {
		reasons = append(reasons, "no requests recorded during the fault window")
	}
	if faultWindow.SuccessRate < th.MinSuccessRate {
		reasons = append(reasons, fmt.Sprintf("success rate %.4f < required %.4f", faultWindow.SuccessRate, th.MinSuccessRate))
	}
	if faultWindow.P95ms > th.MaxP95Ms {
		reasons = append(reasons, fmt.Sprintf("p95 %.1fms > allowed %.1fms", faultWindow.P95ms, th.MaxP95Ms))
	}
	if !recovered {
		reasons = append(reasons, "system did not return to steady state within the observed window")
	} else if recoverySeconds > th.RecoveryTimeoutSeconds {
		reasons = append(reasons, fmt.Sprintf("recovery took %.1fs > timeout %.1fs", recoverySeconds, th.RecoveryTimeoutSeconds))
	}
	if len(reasons) == 0 {
		return Verdict{Pass: true, Reasons: []string{"all steady-state conditions held"}}
	}
	return Verdict{Pass: false, Reasons: reasons}
}

// Human renders a compact, readable summary block.
func (r Report) Human() string {
	status := "PASS"
	if !r.Verdict.Pass {
		status = "FAIL"
	}
	b := &strings.Builder{}
	fmt.Fprintf(b, "Experiment: %s\n", r.Experiment)
	fmt.Fprintf(b, "Fault:      %s  killed=[%s]\n", r.Fault, strings.Join(r.KilledPods, ", "))
	fmt.Fprintf(b, "Baseline:   %s\n", r.Baseline.String())
	fmt.Fprintf(b, "Fault win:  %s\n", r.FaultWindow.String())
	fmt.Fprintf(b, "Recovery:   %.1fs (recovered=%v)\n", r.RecoverySeconds, r.Recovered)
	fmt.Fprintf(b, "Verdict:    %s\n", status)
	for _, reason := range r.Verdict.Reasons {
		fmt.Fprintf(b, "  - %s\n", reason)
	}
	return b.String()
}

// Write persists the report as indented JSON, creating parent dirs as needed.
func Write(path string, r Report) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create report dir: %w", err)
		}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
