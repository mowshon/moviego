package audio_test

import (
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

const sr = 48000

func pull(t *testing.T, c audio.AudioClip, start clip.Time, count int) *clip.AudioBuffer {
	t.Helper()
	buf := &clip.AudioBuffer{}
	if err := c.SamplesInto(context.Background(), start, count, buf); err != nil {
		t.Fatalf("SamplesInto: %v", err)
	}
	return buf
}

// TestMixSum adds two overlapping constant sources and checks the per-sample sum.
func TestMixSum(t *testing.T) {
	a := newFake(sr, 1, time.Second, func(int) float32 { return 0.2 })
	b := newFake(sr, 1, time.Second, func(int) float32 { return 0.3 })
	m := audio.Mix(a, b)

	buf := pull(t, m, 0, 100)
	for i, s := range buf.Samples {
		if diff := s - 0.5; diff > 1e-6 || diff < -1e-6 {
			t.Fatalf("sample %d = %f, want 0.5", i, s)
		}
	}
}

// TestMixHalfOpenGate verifies a child only contributes within its
// [start, end) window: before its start and at/after its end it is silent.
func TestMixHalfOpenGate(t *testing.T) {
	// Child plays only during [0.5s, 1.0s).
	child := newFake(sr, 1, 500*time.Millisecond, func(int) float32 { return 1 })
	shifted := child.WithStart(500 * time.Millisecond).(audio.AudioClip)
	m := audio.Mix(shifted)

	// A chunk spanning [0, 1.0s): first half silent, second half == 1.
	count := sr // one second
	buf := pull(t, m, 0, count)
	mid := clip.SampleIndex(500*time.Millisecond, sr)
	if buf.Samples[mid-1] != 0 {
		t.Fatalf("sample just before start = %f, want 0", buf.Samples[mid-1])
	}
	if buf.Samples[mid] != 1 {
		t.Fatalf("sample at start = %f, want 1", buf.Samples[mid])
	}
	// At/after 1.0s the child has ended.
	end := clip.SampleIndex(time.Second, sr)
	if end < count && buf.Samples[end-1] != 1 {
		t.Fatalf("sample just before end = %f, want 1", buf.Samples[end-1])
	}
}

// TestMixMonoToStereo verifies a mono child is duplicated across a stereo mix.
func TestMixMonoToStereo(t *testing.T) {
	mono := newFake(sr, 1, time.Second, func(int) float32 { return 0.4 })
	stereo := newFake(sr, 2, time.Second, func(int) float32 { return 0.1 })
	m := audio.Mix(mono, stereo)
	if m.Channels() != 2 {
		t.Fatalf("mix channels = %d, want 2", m.Channels())
	}
	buf := pull(t, m, 0, 10)
	for j := 0; j < buf.Count; j++ {
		l, r := buf.Samples[j*2], buf.Samples[j*2+1]
		if approx(l, 0.5) != true || approx(r, 0.5) != true {
			t.Fatalf("frame %d = (%f,%f), want (0.5,0.5)", j, l, r)
		}
	}
}

func TestMixDuration(t *testing.T) {
	a := newFake(sr, 1, 2*time.Second, func(int) float32 { return 0 })
	b := newFake(sr, 1, time.Second, func(int) float32 { return 0 }).WithStart(1500 * time.Millisecond).(audio.AudioClip)
	m := audio.Mix(a, b)
	d := m.Duration()
	if d != 2500*time.Millisecond {
		t.Fatalf("mix duration = %v, want 2.5s", d)
	}
}

// TestMixResampleLinear mixes a 24 kHz ramp with a silent 48 kHz source. The
// mix rate is the max (48 kHz), so the ramp is resampled 1:2. A ramp is linear,
// so linear interpolation reproduces it exactly: output frame j maps to child
// position j/2 with value j/2 (an odd j lands on a .5 fraction and averages the
// two neighbors). Nearest-neighbor would instead snap odd j to a whole sample
// (value 0 or 1 at j=1), so this asserts the interpolation, not just that it ran.
func TestMixResampleLinear(t *testing.T) {
	ramp := newFake(24000, 1, time.Second, func(i int) float32 { return float32(i) })
	silent := newFake(48000, 1, time.Second, func(int) float32 { return 0 })
	m := audio.Mix(ramp, silent)
	if m.SampleRate() != 48000 {
		t.Fatalf("mix rate = %d, want 48000", m.SampleRate())
	}
	buf := pull(t, m, 0, 20)
	for j := 0; j < buf.Count; j++ {
		want := float32(j) / 2
		if !approx(buf.Samples[j], want) {
			t.Fatalf("resampled sample %d = %f, want %f", j, buf.Samples[j], want)
		}
	}
}

// TestMixResampleChunkContinuity checks the resampler has no seam across a chunk
// boundary: pulling [0, 2n) at once must equal pulling [0, n) then [n, 2n).
func TestMixResampleChunkContinuity(t *testing.T) {
	mk := func() audio.AudioClip {
		ramp := newFake(24000, 1, time.Second, func(i int) float32 { return float32(i) })
		silent := newFake(48000, 1, time.Second, func(int) float32 { return 0 })
		return audio.Mix(ramp, silent)
	}
	const n = 257 // odd, so the split lands mid-fraction
	whole := pull(t, mk(), 0, 2*n)
	m := mk()
	first := pull(t, m, 0, n)
	second := pull(t, m, clip.SampleTime(n, 48000), n)
	for j := 0; j < n; j++ {
		if !approx(first.Samples[j], whole.Samples[j]) {
			t.Fatalf("first-half sample %d = %f, want %f", j, first.Samples[j], whole.Samples[j])
		}
		if !approx(second.Samples[j], whole.Samples[n+j]) {
			t.Fatalf("second-half sample %d = %f, want %f", j, second.Samples[j], whole.Samples[n+j])
		}
	}
}

func approx(a, b float32) bool {
	d := a - b
	return d < 1e-5 && d > -1e-5
}
