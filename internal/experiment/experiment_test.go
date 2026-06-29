package experiment

import (
	"os"
	"path/filepath"
	"testing"
)

const validYAML = `
name: testapp-pod-kill
description: kill a pod
target: http://localhost:30080
probe:
  path: /work
  rps: 50
steadyState:
  minSuccessRate: 0.95
  maxP95Ms: 300
fault:
  type: pod-kill
  namespace: kresil
  selector: app=testapp
phases:
  baselineSeconds: 5
  faultSeconds: 10
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "exp.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return path
}

func TestLoadAppliesDefaults(t *testing.T) {
	e, err := Load(writeTemp(t, validYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if e.Probe.Concurrency != 20 {
		t.Errorf("default concurrency = %d, want 20", e.Probe.Concurrency)
	}
	if e.Probe.TimeoutMs != 2000 {
		t.Errorf("default timeoutMs = %d, want 2000", e.Probe.TimeoutMs)
	}
	if e.Fault.Count != 1 {
		t.Errorf("default fault.count = %d, want 1", e.Fault.Count)
	}
	if e.Phases.RecoveryTimeoutSeconds != 30 {
		t.Errorf("default recoveryTimeout = %d, want 30", e.Phases.RecoveryTimeoutSeconds)
	}
	if got := e.TargetURL(); got != "http://localhost:30080/work" {
		t.Errorf("TargetURL = %q", got)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	if _, err := Load(writeTemp(t, validYAML+"\nbogus: true\n")); err == nil {
		t.Fatal("expected strict parse error for unknown field")
	}
}

func TestValidate(t *testing.T) {
	base := func() Experiment {
		e := Experiment{
			Name:        "x",
			Target:      "http://h",
			Probe:       Probe{Path: "/work", RPS: 10, Concurrency: 1, TimeoutMs: 1000},
			SteadyState: SteadyState{MinSuccessRate: 0.95, MaxP95Ms: 100},
			Fault:       Fault{Type: FaultPodKill, Namespace: "ns", Selector: "app=x", Count: 1},
			Phases:      Phases{BaselineSeconds: 1, FaultSeconds: 1, RecoveryTimeoutSeconds: 5},
		}
		return e
	}
	valid := base()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid experiment rejected: %v", err)
	}

	cases := map[string]func(*Experiment){
		"bad fault type":  func(e *Experiment) { e.Fault.Type = "node-nuke" },
		"no selector":     func(e *Experiment) { e.Fault.Selector = "" },
		"rate over 1":     func(e *Experiment) { e.SteadyState.MinSuccessRate = 1.5 },
		"zero p95":        func(e *Experiment) { e.SteadyState.MaxP95Ms = 0 },
		"zero rps":        func(e *Experiment) { e.Probe.RPS = 0 },
		"zero faultSecs":  func(e *Experiment) { e.Phases.FaultSeconds = 0 },
		"empty namespace": func(e *Experiment) { e.Fault.Namespace = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := base()
			mutate(&e)
			if err := e.Validate(); err == nil {
				t.Fatalf("expected validation error for %q", name)
			}
		})
	}
}
