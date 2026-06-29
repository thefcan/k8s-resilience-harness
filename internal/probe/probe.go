// Package probe drives constant-rate load at the SUT and turns the resulting
// timestamped samples into per-phase steady-state metrics and a recovery time.
//
// The pacing engine is the same deadlock-safe shape as loadgen (ticker + bounded
// worker pool + concurrent collector), but the prober runs until its context is
// cancelled and timestamps every sample so the orchestrator can slice the run
// into baseline / fault / recovery windows after the fact.
package probe

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/thefcan/k8s-resilience-harness/internal/metrics"
)

// Sample is a single timestamped request outcome.
type Sample struct {
	At      time.Time
	Latency time.Duration
	OK      bool
}

// Prober drives load at a target URL.
type Prober struct {
	target      string
	rps         int
	concurrency int
	client      *http.Client
}

// New builds a Prober.
func New(target string, rps, concurrency int, timeout time.Duration) *Prober {
	return &Prober{
		target:      target,
		rps:         rps,
		concurrency: concurrency,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        concurrency * 2,
				MaxIdleConnsPerHost: concurrency * 2,
			},
		},
	}
}

// Run drives constant-rate load until ctx is cancelled, returning every
// timestamped sample.
func (p *Prober) Run(ctx context.Context) []Sample {
	jobs := make(chan time.Time, p.concurrency)
	results := make(chan Sample, p.concurrency)

	var workers sync.WaitGroup
	for i := 0; i < p.concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for start := range jobs {
				results <- p.do(ctx, start)
			}
		}()
	}

	var samples []Sample
	collected := make(chan struct{})
	go func() {
		for s := range results {
			samples = append(samples, s)
		}
		close(collected)
	}()

	interval := time.Second / time.Duration(p.rps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case now := <-ticker.C:
			select {
			case jobs <- now:
			default:
				results <- Sample{At: now, OK: false} // pool saturated
			}
		}
	}

	close(jobs)
	workers.Wait()
	close(results)
	<-collected
	return samples
}

func (p *Prober) do(ctx context.Context, start time.Time) Sample {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.target, nil)
	if err != nil {
		return Sample{At: start, OK: false}
	}
	resp, err := p.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return Sample{At: start, Latency: latency, OK: false}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return Sample{At: start, Latency: latency, OK: resp.StatusCode == http.StatusOK}
}

// SummarizeWindow aggregates samples whose timestamp is in [from, to).
func SummarizeWindow(samples []Sample, from, to time.Time) metrics.Summary {
	var window []metrics.Sample
	for _, s := range samples {
		if !s.At.Before(from) && s.At.Before(to) {
			window = append(window, metrics.Sample{Latency: s.Latency, OK: s.OK})
		}
	}
	return metrics.Summarize(window)
}

// Recovery measures how long after injectedAt the success rate returned to and
// stayed at >= minRate, bucketing samples into bins of width `bucket` over
// [injectedAt, until). It returns the time from injection to the start of the
// sustained-healthy run and whether recovery happened at all.
//
//   - no dip below minRate after injection  -> (0, true)
//   - dipped then recovered                  -> (offset to first sustained-healthy bin, true)
//   - still below minRate in the final bin   -> (observed window, false)
func Recovery(samples []Sample, injectedAt, until time.Time, minRate float64, bucket time.Duration) (time.Duration, bool) {
	if bucket <= 0 || !until.After(injectedAt) {
		return 0, true
	}

	type bin struct {
		ok, total int
	}
	bins := map[int]*bin{}
	maxIdx := -1
	for _, s := range samples {
		if s.At.Before(injectedAt) || !s.At.Before(until) {
			continue
		}
		idx := int(s.At.Sub(injectedAt) / bucket)
		b := bins[idx]
		if b == nil {
			b = &bin{}
			bins[idx] = b
		}
		b.total++
		if s.OK {
			b.ok++
		}
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	if maxIdx < 0 {
		return 0, true // no samples observed in the window
	}

	healthy := func(idx int) bool {
		b := bins[idx]
		if b == nil || b.total == 0 {
			return true // no data in this bin: not evidence of a failure
		}
		return float64(b.ok)/float64(b.total) >= minRate
	}

	// Find the last unhealthy bin; healthy state begins right after it.
	lastUnhealthy := -1
	for idx := 0; idx <= maxIdx; idx++ {
		if !healthy(idx) {
			lastUnhealthy = idx
		}
	}
	if lastUnhealthy == maxIdx {
		return until.Sub(injectedAt), false // still failing at the end of the window
	}
	return time.Duration(lastUnhealthy+1) * bucket, true
}
