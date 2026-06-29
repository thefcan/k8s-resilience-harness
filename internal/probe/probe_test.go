package probe

import (
	"testing"
	"time"
)

var base = time.Unix(1_000_000, 0)

// samplesInBin returns n samples placed inside bin index `binIdx` (1s bins),
// all with the given OK value.
func samplesInBin(binIdx, n int, ok bool) []Sample {
	out := make([]Sample, 0, n)
	at := base.Add(time.Duration(binIdx)*time.Second + 100*time.Millisecond)
	for i := 0; i < n; i++ {
		out = append(out, Sample{At: at, Latency: 5 * time.Millisecond, OK: ok})
	}
	return out
}

func TestSummarizeWindowFiltersByTime(t *testing.T) {
	samples := []Sample{
		{At: base.Add(-time.Second), OK: true},     // before window
		{At: base.Add(1 * time.Second), OK: true},  // in window
		{At: base.Add(2 * time.Second), OK: false}, // in window
		{At: base.Add(10 * time.Second), OK: true}, // after window
	}
	got := SummarizeWindow(samples, base, base.Add(5*time.Second))
	if got.Total != 2 {
		t.Fatalf("window total = %d, want 2", got.Total)
	}
	if got.Succeeded != 1 || got.Failed != 1 {
		t.Fatalf("window ok/fail = %d/%d, want 1/1", got.Succeeded, got.Failed)
	}
}

func TestRecoveryNoDip(t *testing.T) {
	var s []Sample
	for b := 0; b < 5; b++ {
		s = append(s, samplesInBin(b, 10, true)...)
	}
	d, ok := Recovery(s, base, base.Add(5*time.Second), 0.95, time.Second)
	if !ok || d != 0 {
		t.Fatalf("no-dip recovery = (%v, %v), want (0, true)", d, ok)
	}
}

func TestRecoveryDipThenRecover(t *testing.T) {
	var s []Sample
	s = append(s, samplesInBin(0, 10, false)...) // bin 0 fails
	s = append(s, samplesInBin(1, 10, false)...) // bin 1 fails
	for b := 2; b < 6; b++ {                     // bins 2..5 healthy
		s = append(s, samplesInBin(b, 10, true)...)
	}
	d, ok := Recovery(s, base, base.Add(6*time.Second), 0.95, time.Second)
	if !ok || d != 2*time.Second {
		t.Fatalf("dip-then-recover = (%v, %v), want (2s, true)", d, ok)
	}
}

func TestRecoveryStillFailing(t *testing.T) {
	var s []Sample
	s = append(s, samplesInBin(0, 10, true)...)
	s = append(s, samplesInBin(1, 10, true)...)
	s = append(s, samplesInBin(2, 10, false)...) // last bin still failing
	d, ok := Recovery(s, base, base.Add(3*time.Second), 0.95, time.Second)
	if ok {
		t.Fatalf("still-failing recovery should report ok=false, got (%v, %v)", d, ok)
	}
}
