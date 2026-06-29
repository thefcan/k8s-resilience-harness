// Package metrics aggregates per-request outcomes into the steady-state
// signals the harness reasons about: success rate and latency percentiles.
//
// It is deliberately dependency-free and reusable: loadgen uses it to build a
// baseline report (M1), and the in-fault probe (M2) will reuse it to score a
// run against its steady-state hypothesis.
package metrics

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Sample is a single observed request outcome.
type Sample struct {
	Latency time.Duration
	OK      bool
}

// Summary is the aggregated view of a batch of samples. Latencies are reported
// in milliseconds so the JSON report stays human-readable.
//
// Latency percentiles are computed over successful requests only: the latency
// of a failed/timed-out request is not a meaningful service-time signal.
type Summary struct {
	Total       int     `json:"total"`
	Succeeded   int     `json:"succeeded"`
	Failed      int     `json:"failed"`
	SuccessRate float64 `json:"success_rate"` // fraction in [0,1]
	P50ms       float64 `json:"p50_ms"`
	P95ms       float64 `json:"p95_ms"`
	P99ms       float64 `json:"p99_ms"`
	MaxMs       float64 `json:"max_ms"`
}

// Summarize aggregates samples into a Summary. It is safe to call with an empty
// slice, which yields a zero-valued Summary.
func Summarize(samples []Sample) Summary {
	var s Summary
	s.Total = len(samples)
	if s.Total == 0 {
		return s
	}

	okLatencies := make([]time.Duration, 0, len(samples))
	for _, sample := range samples {
		if sample.OK {
			s.Succeeded++
			okLatencies = append(okLatencies, sample.Latency)
		}
	}
	s.Failed = s.Total - s.Succeeded
	s.SuccessRate = float64(s.Succeeded) / float64(s.Total)

	if len(okLatencies) == 0 {
		return s
	}
	sort.Slice(okLatencies, func(i, j int) bool { return okLatencies[i] < okLatencies[j] })
	s.P50ms = ms(percentile(okLatencies, 50))
	s.P95ms = ms(percentile(okLatencies, 95))
	s.P99ms = ms(percentile(okLatencies, 99))
	s.MaxMs = ms(okLatencies[len(okLatencies)-1])
	return s
}

// percentile returns the p-th percentile of a sorted (ascending) slice using
// the nearest-rank method. p is clamped to (0,100]. Callers must pass a
// non-empty, pre-sorted slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if p <= 0 {
		return sorted[0]
	}
	if p > 100 {
		p = 100
	}
	// nearest-rank: rank = ceil(p/100 * N), 1-based.
	rank := int(math.Ceil((p / 100) * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

func ms(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

// String renders a compact, human-readable one-block summary.
func (s Summary) String() string {
	return fmt.Sprintf(
		"requests=%d ok=%d fail=%d success_rate=%.4f p50=%.1fms p95=%.1fms p99=%.1fms max=%.1fms",
		s.Total, s.Succeeded, s.Failed, s.SuccessRate, s.P50ms, s.P95ms, s.P99ms, s.MaxMs,
	)
}
