// Command loadgen drives a constant request rate at a target URL and records a
// baseline steady-state report: success rate and latency percentiles.
//
// In M1 it establishes the *baseline* — what "healthy" looks like with no fault
// injected. Later milestones compare in-fault behaviour against this baseline.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/thefcan/k8s-resilience-harness/internal/metrics"
)

// Config describes a single load run.
type Config struct {
	Target      string
	RPS         int
	Duration    time.Duration
	Concurrency int
	Timeout     time.Duration
}

// Report is the serialised baseline artifact written to results/.
type Report struct {
	Target       string          `json:"target"`
	RequestedRPS int             `json:"requested_rps"`
	DurationSec  float64         `json:"duration_sec"`
	Concurrency  int             `json:"concurrency"`
	AchievedRPS  float64         `json:"achieved_rps"`
	Summary      metrics.Summary `json:"summary"`
}

// Generator executes a Config and produces request samples.
type Generator struct {
	cfg    Config
	client *http.Client
}

func newGenerator(cfg Config) *Generator {
	return &Generator{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        cfg.Concurrency * 2,
				MaxIdleConnsPerHost: cfg.Concurrency * 2,
			},
		},
	}
}

// Run dispatches requests at the configured constant rate until the duration
// elapses or ctx is cancelled, returning one Sample per dispatched request.
//
// Pacing is decoupled from execution: a ticker emits at the target rate and
// hands work to a bounded worker pool. If every worker is busy when a tick
// fires, that tick is recorded as a failed (overloaded) sample rather than
// blocking — so the achieved rate stays honest and saturation is visible.
func (g *Generator) Run(ctx context.Context) []metrics.Sample {
	// dispatchCtx bounds how long we *start* new requests. Requests already
	// in flight use the parent ctx so they finish (or hit their own timeout)
	// instead of being cancelled the instant the duration elapses.
	dispatchCtx, cancel := context.WithTimeout(ctx, g.cfg.Duration)
	defer cancel()

	jobs := make(chan struct{}, g.cfg.Concurrency)
	results := make(chan metrics.Sample, g.cfg.Concurrency)

	var workers sync.WaitGroup
	for i := 0; i < g.cfg.Concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range jobs {
				results <- g.doRequest(ctx)
			}
		}()
	}

	// Drain results concurrently with dispatch so senders never block on a full
	// buffer — otherwise a long run deadlocks once the buffer fills.
	var samples []metrics.Sample
	collected := make(chan struct{})
	go func() {
		for s := range results {
			samples = append(samples, s)
		}
		close(collected)
	}()

	interval := time.Second / time.Duration(g.cfg.RPS)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

loop:
	for {
		select {
		case <-dispatchCtx.Done():
			break loop
		case <-ticker.C:
			select {
			case jobs <- struct{}{}:
			default:
				results <- metrics.Sample{OK: false} // pool saturated
			}
		}
	}

	close(jobs)    // workers drain remaining jobs, then return
	workers.Wait() // all in-flight requests done sending
	close(results) // collector drains the tail, then signals
	<-collected
	return samples
}

func (g *Generator) doRequest(ctx context.Context) metrics.Sample {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.cfg.Target, nil)
	if err != nil {
		return metrics.Sample{OK: false}
	}
	start := time.Now()
	resp, err := g.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return metrics.Sample{Latency: latency, OK: false}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return metrics.Sample{Latency: latency, OK: resp.StatusCode == http.StatusOK}
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "loadgen:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("loadgen", flag.ContinueOnError)
	fs.SetOutput(stderr)

	cfg := Config{}
	var outPath string
	fs.StringVar(&cfg.Target, "target", "http://localhost:8080/work", "target URL to drive load at")
	fs.IntVar(&cfg.RPS, "rps", 50, "requests per second")
	fs.DurationVar(&cfg.Duration, "duration", 30*time.Second, "how long to generate load")
	fs.IntVar(&cfg.Concurrency, "concurrency", 20, "max in-flight requests")
	fs.DurationVar(&cfg.Timeout, "timeout", 2*time.Second, "per-request timeout")
	fs.StringVar(&outPath, "out", "", "write JSON baseline report to this path (optional)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.RPS < 1 {
		return fmt.Errorf("rps must be >= 1, got %d", cfg.RPS)
	}
	if cfg.Concurrency < 1 {
		return fmt.Errorf("concurrency must be >= 1, got %d", cfg.Concurrency)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	_, _ = fmt.Fprintf(stderr, "==> driving %d rps at %s for %s (concurrency %d)\n",
		cfg.RPS, cfg.Target, cfg.Duration, cfg.Concurrency)

	gen := newGenerator(cfg)
	started := time.Now()
	samples := gen.Run(ctx)
	elapsed := time.Since(started)

	report := Report{
		Target:       cfg.Target,
		RequestedRPS: cfg.RPS,
		DurationSec:  elapsed.Seconds(),
		Concurrency:  cfg.Concurrency,
		AchievedRPS:  float64(len(samples)) / elapsed.Seconds(),
		Summary:      metrics.Summarize(samples),
	}

	_, _ = fmt.Fprintf(stdout, "\nBaseline report\n---------------\n%s\nachieved_rps=%.1f over %.1fs\n",
		report.Summary.String(), report.AchievedRPS, report.DurationSec)

	if outPath != "" {
		if err := writeReport(outPath, report); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stderr, "==> wrote baseline report to %s\n", outPath)
	}
	return nil
}

func writeReport(path string, report Report) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create report dir: %w", err)
		}
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
