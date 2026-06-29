package metrics

import (
	"math"
	"testing"
	"time"
)

func sample(ms int, ok bool) Sample {
	return Sample{Latency: time.Duration(ms) * time.Millisecond, OK: ok}
}

func TestSummarizeEmpty(t *testing.T) {
	got := Summarize(nil)
	if got.Total != 0 || got.SuccessRate != 0 || got.P95ms != 0 {
		t.Fatalf("empty summary should be zero-valued, got %+v", got)
	}
}

func TestSummarizeCountsAndRate(t *testing.T) {
	samples := []Sample{
		sample(10, true),
		sample(20, true),
		sample(30, false),
		sample(40, true),
	}
	got := Summarize(samples)
	if got.Total != 4 {
		t.Fatalf("Total = %d, want 4", got.Total)
	}
	if got.Succeeded != 3 || got.Failed != 1 {
		t.Fatalf("Succeeded/Failed = %d/%d, want 3/1", got.Succeeded, got.Failed)
	}
	if math.Abs(got.SuccessRate-0.75) > 1e-9 {
		t.Fatalf("SuccessRate = %v, want 0.75", got.SuccessRate)
	}
}

func TestSummarizeIgnoresFailedLatencies(t *testing.T) {
	// The single failure has a huge latency that must not pollute percentiles.
	samples := []Sample{
		sample(10, true),
		sample(20, true),
		sample(9999, false),
	}
	got := Summarize(samples)
	if got.MaxMs != 20 {
		t.Fatalf("MaxMs = %v, want 20 (failed latency must be excluded)", got.MaxMs)
	}
}

func TestSummarizeAllFailed(t *testing.T) {
	got := Summarize([]Sample{sample(10, false), sample(20, false)})
	if got.SuccessRate != 0 {
		t.Fatalf("SuccessRate = %v, want 0", got.SuccessRate)
	}
	if got.P95ms != 0 || got.MaxMs != 0 {
		t.Fatalf("percentiles should be 0 with no successes, got p95=%v max=%v", got.P95ms, got.MaxMs)
	}
}

func TestPercentileNearestRank(t *testing.T) {
	// 1..100 ms ascending. Nearest-rank p-th = the p-th element (1-based).
	xs := make([]time.Duration, 100)
	for i := range xs {
		xs[i] = time.Duration(i+1) * time.Millisecond
	}
	cases := []struct {
		p    float64
		want time.Duration
	}{
		{50, 50 * time.Millisecond},
		{95, 95 * time.Millisecond},
		{99, 99 * time.Millisecond},
		{100, 100 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := percentile(xs, tc.p); got != tc.want {
			t.Fatalf("percentile(%v) = %v, want %v", tc.p, got, tc.want)
		}
	}
}

func TestPercentileSingleElement(t *testing.T) {
	xs := []time.Duration{42 * time.Millisecond}
	if got := percentile(xs, 95); got != 42*time.Millisecond {
		t.Fatalf("percentile of single element = %v, want 42ms", got)
	}
}
