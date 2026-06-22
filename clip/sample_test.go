package clip_test

import (
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
)

// TestSampleRoundTrip verifies SampleIndex is a left inverse of SampleTime for
// every index across several sample rates: floor(ceil(idx)) == idx. This is the
// property the chunked audio render relies on for gap-free contiguity.
func TestSampleRoundTrip(t *testing.T) {
	rates := []int{8000, 11025, 22050, 44100, 48000, 96000}
	for _, sr := range rates {
		for idx := 0; idx < 200000; idx += 137 {
			got := clip.SampleIndex(clip.SampleTime(idx, sr), sr)
			if got != idx {
				t.Fatalf("sr=%d idx=%d round-trip got %d", sr, idx, got)
			}
		}
	}
}

// TestSampleContiguity verifies consecutive chunks computed from SampleTime are
// contiguous in sample index: the first sample of chunk k+1 immediately follows
// the last sample of chunk k, with no gap or overlap.
func TestSampleContiguity(t *testing.T) {
	const sr = 44100
	const chunk = 1024
	for k := 0; k < 500; k++ {
		off := k * chunk
		startIdx := clip.SampleIndex(clip.SampleTime(off, sr), sr)
		if startIdx != off {
			t.Fatalf("chunk %d: start index %d != %d", k, startIdx, off)
		}
	}
}

func TestSampleIndexZeroRate(t *testing.T) {
	if got := clip.SampleIndex(time.Second, 0); got != 0 {
		t.Fatalf("zero rate index = %d, want 0", got)
	}
	if got := clip.SampleTime(10, 0); got != 0 {
		t.Fatalf("zero rate time = %v, want 0", got)
	}
}

// TestSampleIndexFloorsNegative verifies SampleIndex floors (toward -inf) rather
// than truncating toward zero. A time a hair before 0 must map to -1, not 0, so
// a clip placed at a sub-sample negative offset gates at the right sample and an
// "out of range" check around zero is not silently swallowed.
func TestSampleIndexFloorsNegative(t *testing.T) {
	const sr = 48000
	one := clip.SampleTime(1, sr) // exact time of sample index 1
	cases := []struct {
		t    clip.Time
		want int
	}{
		{0, 0},
		{-1, -1},     // 1ns before 0 → sample -1 (floor), not 0 (trunc)
		{-one, -2},   // one sample-time (ceil'd) before 0 lands just past -1 → -2
		{one - 1, 0}, // just before sample 1 still plays sample 0
		{one, 1},
	}
	for _, c := range cases {
		if got := clip.SampleIndex(c.t, sr); got != c.want {
			t.Fatalf("SampleIndex(%d) = %d, want %d", int64(c.t), got, c.want)
		}
	}
}

// TestSampleIndexFloorsNegativeBig exercises the floor adjustment on the big.Int
// path: a large negative timestamp past the int64 fast-path bound must still
// floor, not truncate.
func TestSampleIndexFloorsNegativeBig(t *testing.T) {
	const sr = 96000
	// A magnitude past the fast-path bound (|t| > MaxInt64/sr) with a guaranteed
	// non-zero remainder so the floor branch (subtract one) is taken.
	t0 := -clip.SampleTime(80*3600*sr, sr) - 1
	got := clip.SampleIndex(t0, sr)
	// Flooring means the result is <= the truncated-toward-zero value. Compare to
	// the exact next sample boundary: SampleTime(got) <= t0 < SampleTime(got+1).
	if lo := clip.SampleTime(got, sr); lo > t0 {
		t.Fatalf("floor broken: SampleTime(%d)=%d > t=%d", got, int64(lo), int64(t0))
	}
	if hi := clip.SampleTime(got+1, sr); hi <= t0 {
		t.Fatalf("not maximal: SampleTime(%d)=%d <= t=%d", got+1, int64(hi), int64(t0))
	}
}

// TestSampleRoundTripLargeIndex exercises the big.Int overflow fallback: at a
// high index/rate the int64 product idx*1e9 would overflow, but the round trip
// must still be exact. ~80 hours at 96 kHz is past the int64 fast-path bound.
func TestSampleRoundTripLargeIndex(t *testing.T) {
	const sr = 96000
	for _, idx := range []int{
		1 << 40,                    // ~127 days of samples; idx*1e9 overflows int64
		int(8 * 3600 * int64(sr)),  // 8 hours
		int(80 * 3600 * int64(sr)), // 80 hours
	} {
		got := clip.SampleIndex(clip.SampleTime(idx, sr), sr)
		if got != idx {
			t.Fatalf("large round-trip sr=%d idx=%d got %d", sr, idx, got)
		}
	}
}
