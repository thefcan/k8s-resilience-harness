package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/thefcan/k8s-resilience-harness/internal/experiment"
	"github.com/thefcan/k8s-resilience-harness/internal/inject"
	"github.com/thefcan/k8s-resilience-harness/internal/k8s"
	"github.com/thefcan/k8s-resilience-harness/internal/logger"
	"github.com/thefcan/k8s-resilience-harness/internal/probe"
	"github.com/thefcan/k8s-resilience-harness/internal/report"
)

// recoveryBucket is the bin width used to detect when success rate returns to
// steady state after the fault.
const recoveryBucket = time.Second

// runExperiment is the `harness run` subcommand: load an experiment, drive load,
// inject the fault, and emit a steady-state verdict + report.
func runExperiment(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("harness run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		expPath    string
		kubeconfig string
		outPath    string
		logLevel   string
	)
	fs.StringVar(&expPath, "experiment", "", "path to the experiment YAML (required)")
	fs.StringVar(&kubeconfig, "kubeconfig", "", "kubeconfig path (default: in-cluster or ~/.kube/config)")
	fs.StringVar(&outPath, "out", "", "write the JSON report to this path (optional)")
	fs.StringVar(&logLevel, "log-level", "info", "log level: debug|info|warn|error")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if expPath == "" {
		return errors.New("-experiment is required")
	}

	level, err := logger.ParseLevel(logLevel)
	if err != nil {
		return err
	}
	log := logger.New(stderr, level, logger.FormatText)

	exp, err := experiment.Load(expPath)
	if err != nil {
		return err
	}
	clientset, err := k8s.NewClientset(kubeconfig)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	injector, err := newInjector(clientset, exp.Fault)
	if err != nil {
		return err
	}
	rep, err := execute(ctx, log, exp, injector)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(stdout, "\n"+rep.Human())
	if outPath != "" {
		if err := report.Write(outPath, rep); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stderr, "==> wrote report to %s\n", outPath)
	}
	if !rep.Verdict.Pass {
		return errors.New("steady-state hypothesis violated")
	}
	return nil
}

// newInjector builds the fault injector named by the experiment.
func newInjector(client kubernetes.Interface, f experiment.Fault) (inject.Injector, error) {
	switch f.Type {
	case experiment.FaultPodKill:
		return inject.NewPodKiller(client, f.Namespace, f.Selector, f.Count), nil
	case experiment.FaultNodeDrain:
		return inject.NewNodeDrainer(client, f.Namespace, f.Selector), nil
	default:
		return nil, fmt.Errorf("unsupported fault type %q", f.Type)
	}
}

// execute drives the probe through baseline -> fault -> observation, computes
// per-window metrics + recovery, and builds the report.
func execute(ctx context.Context, log *slog.Logger, exp *experiment.Experiment, injector inject.Injector) (report.Report, error) {
	prober := probe.New(exp.TargetURL(), exp.Probe.RPS, exp.Probe.Concurrency, exp.Probe.Timeout())

	probeCtx, cancelProbe := context.WithCancel(ctx)
	defer cancelProbe()
	samplesCh := make(chan []probe.Sample, 1)
	go func() { samplesCh <- prober.Run(probeCtx) }()

	startedAt := time.Now()
	log.Info("baseline phase", "seconds", exp.Phases.BaselineSeconds, "target", exp.TargetURL())
	if err := sleepCtx(ctx, exp.Phases.Baseline()); err != nil {
		return report.Report{}, err
	}

	injectedAt := time.Now()
	log.Info("injecting fault", "type", exp.Fault.Type, "namespace", exp.Fault.Namespace, "selector", exp.Fault.Selector)
	affected, err := injector.Inject(ctx)
	// Always undo standing cluster state (e.g. a cordoned node), even on
	// cancellation. Pod-kill's rollback is a no-op.
	defer func() {
		rbCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if rbErr := injector.Rollback(rbCtx); rbErr != nil {
			log.Warn("fault rollback failed", "err", rbErr)
		} else {
			log.Debug("fault rolled back")
		}
	}()
	if err != nil {
		return report.Report{}, fmt.Errorf("inject fault: %w", err)
	}
	log.Info("fault injected", "affected", affected)

	log.Info("observing fault + recovery", "seconds", exp.Phases.FaultSeconds)
	if err := sleepCtx(ctx, exp.Phases.Fault()); err != nil {
		return report.Report{}, err
	}

	cancelProbe()
	samples := <-samplesCh

	faultEnd := injectedAt.Add(exp.Phases.Fault())
	baseline := probe.SummarizeWindow(samples, startedAt, injectedAt)
	faultWindow := probe.SummarizeWindow(samples, injectedAt, faultEnd)
	recoveryDur, recovered := probe.Recovery(samples, injectedAt, faultEnd, exp.SteadyState.MinSuccessRate, recoveryBucket)

	th := report.Thresholds{
		MinSuccessRate:         exp.SteadyState.MinSuccessRate,
		MaxP95Ms:               exp.SteadyState.MaxP95Ms,
		RecoveryTimeoutSeconds: float64(exp.Phases.RecoveryTimeoutSeconds),
	}
	verdict := report.BuildVerdict(th, faultWindow, recovered, recoveryDur.Seconds())

	return report.Report{
		Experiment:      exp.Name,
		StartedAt:       startedAt.UTC().Format(time.RFC3339),
		Fault:           string(exp.Fault.Type),
		Affected:        affected,
		Thresholds:      th,
		Baseline:        baseline,
		FaultWindow:     faultWindow,
		RecoverySeconds: recoveryDur.Seconds(),
		Recovered:       recovered,
		Verdict:         verdict,
	}, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
