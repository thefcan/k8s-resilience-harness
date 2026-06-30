// Package experiment defines the declarative resilience-experiment format and
// loads/validates it from YAML.
//
// An experiment states a steady-state hypothesis ("success rate stays >= X and
// p95 latency stays <= Y"), a fault to inject, and the phase timing used to
// observe baseline, fault, and recovery.
package experiment

import (
	"fmt"
	"os"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// FaultType enumerates the supported fault injectors.
type FaultType string

const (
	// FaultPodKill deletes one or more pods matching a selector.
	FaultPodKill FaultType = "pod-kill"
	// FaultNodeDrain cordons a node hosting the target workload and evicts the
	// workload's pods from it, forcing them to reschedule elsewhere.
	FaultNodeDrain FaultType = "node-drain"
)

// Experiment is a single declarative resilience experiment.
type Experiment struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Target      string      `json:"target"` // base URL the prober drives, e.g. http://localhost:30080
	Probe       Probe       `json:"probe"`
	SteadyState SteadyState `json:"steadyState"`
	Fault       Fault       `json:"fault"`
	Phases      Phases      `json:"phases"`
}

// Probe configures the load the harness drives while observing the system.
type Probe struct {
	Path        string `json:"path"`        // request path, e.g. /work
	RPS         int    `json:"rps"`         // requests per second
	Concurrency int    `json:"concurrency"` // max in-flight requests
	TimeoutMs   int    `json:"timeoutMs"`   // per-request timeout
}

// SteadyState is the hypothesis the system must satisfy to pass.
type SteadyState struct {
	MinSuccessRate float64 `json:"minSuccessRate"` // fraction in [0,1]
	MaxP95Ms       float64 `json:"maxP95Ms"`       // p95 latency ceiling, milliseconds
}

// Fault describes the disruption to inject.
type Fault struct {
	Type      FaultType `json:"type"`
	Namespace string    `json:"namespace"`
	Selector  string    `json:"selector"` // label selector, e.g. app=testapp
	Count     int       `json:"count"`    // how many pods to kill (pod-kill only)
}

// Phases is the timeline of the run, in seconds.
type Phases struct {
	BaselineSeconds        int `json:"baselineSeconds"`        // observe before the fault
	FaultSeconds           int `json:"faultSeconds"`           // observe from injection onward
	RecoveryTimeoutSeconds int `json:"recoveryTimeoutSeconds"` // max time to wait for recovery
}

// Timeout returns the per-request probe timeout.
func (p Probe) Timeout() time.Duration { return time.Duration(p.TimeoutMs) * time.Millisecond }

// Baseline returns the baseline phase duration.
func (p Phases) Baseline() time.Duration { return time.Duration(p.BaselineSeconds) * time.Second }

// Fault returns the fault-observation phase duration.
func (p Phases) Fault() time.Duration { return time.Duration(p.FaultSeconds) * time.Second }

// TargetURL joins the base target and the probe path.
func (e Experiment) TargetURL() string {
	return strings.TrimRight(e.Target, "/") + "/" + strings.TrimLeft(e.Probe.Path, "/")
}

// Load reads and validates an experiment from a YAML file, applying defaults.
func Load(path string) (*Experiment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read experiment: %w", err)
	}
	var e Experiment
	if err := yaml.UnmarshalStrict(data, &e); err != nil {
		return nil, fmt.Errorf("parse experiment %s: %w", path, err)
	}
	e.applyDefaults()
	if err := e.Validate(); err != nil {
		return nil, fmt.Errorf("invalid experiment %s: %w", path, err)
	}
	return &e, nil
}

func (e *Experiment) applyDefaults() {
	if e.Probe.Path == "" {
		e.Probe.Path = "/work"
	}
	if e.Probe.Concurrency == 0 {
		e.Probe.Concurrency = 20
	}
	if e.Probe.TimeoutMs == 0 {
		e.Probe.TimeoutMs = 2000
	}
	if e.Fault.Count == 0 {
		e.Fault.Count = 1
	}
	if e.Phases.RecoveryTimeoutSeconds == 0 {
		e.Phases.RecoveryTimeoutSeconds = 30
	}
}

// Validate checks the experiment is internally consistent and runnable.
func (e *Experiment) Validate() error {
	var errs []string
	if strings.TrimSpace(e.Name) == "" {
		errs = append(errs, "name is required")
	}
	if strings.TrimSpace(e.Target) == "" {
		errs = append(errs, "target is required")
	}
	switch e.Fault.Type {
	case FaultPodKill, FaultNodeDrain:
	default:
		errs = append(errs, fmt.Sprintf("unsupported fault type %q (supported: %q, %q)", e.Fault.Type, FaultPodKill, FaultNodeDrain))
	}
	if strings.TrimSpace(e.Fault.Namespace) == "" {
		errs = append(errs, "fault.namespace is required")
	}
	if strings.TrimSpace(e.Fault.Selector) == "" {
		errs = append(errs, "fault.selector is required")
	}
	if e.Fault.Count < 1 {
		errs = append(errs, "fault.count must be >= 1")
	}
	if e.Probe.RPS < 1 {
		errs = append(errs, "probe.rps must be >= 1")
	}
	if e.Probe.Concurrency < 1 {
		errs = append(errs, "probe.concurrency must be >= 1")
	}
	if e.SteadyState.MinSuccessRate <= 0 || e.SteadyState.MinSuccessRate > 1 {
		errs = append(errs, "steadyState.minSuccessRate must be in (0,1]")
	}
	if e.SteadyState.MaxP95Ms <= 0 {
		errs = append(errs, "steadyState.maxP95Ms must be > 0")
	}
	if e.Phases.BaselineSeconds < 0 || e.Phases.FaultSeconds < 1 {
		errs = append(errs, "phases.faultSeconds must be >= 1 and baselineSeconds >= 0")
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
