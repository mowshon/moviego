package audio_test

import (
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// TestReverseFlipsSamples reverses a ramp (sample value == frame index) and
// checks each output sample is the mirrored source index total-1-s, both when
// the chunk starts at 0 and partway through.
func TestReverseFlipsSamples(t *testing.T) {
	rev := audio.Reverse(ramp(time.Second))
	if d := rev.Duration(); d != time.Second {
		t.Fatalf("duration = %v, want 1s", d)
	}
	total := clip.SampleIndex(time.Second, sr)

	// From the start.
	buf := pull(t, rev, 0, 8)
	for j := 0; j < buf.Count; j++ {
		if want := float32(total - 1 - j); buf.Samples[j] != want {
			t.Fatalf("sample %d = %f, want %f", j, buf.Samples[j], want)
		}
	}

	// Partway through: output frame at offset off maps to source total-1-off.
	const off = 100
	mid := pull(t, rev, clip.SampleTime(off, sr), 8)
	for j := 0; j < mid.Count; j++ {
		if want := float32(total - 1 - (off + j)); mid.Samples[j] != want {
			t.Fatalf("mid sample %d = %f, want %f", j, mid.Samples[j], want)
		}
	}
}

// TestReverseSilentPastEnd checks output frames at/after the clip end are
// silent, so a bare reversed sidecar pulled past its end stops cleanly.
func TestReverseSilentPastEnd(t *testing.T) {
	rev := audio.Reverse(ramp(time.Second))
	total := clip.SampleIndex(time.Second, sr)
	buf := pull(t, rev, clip.SampleTime(total-2, sr), 5)
	// Frames total-2, total-1 are in-window (non-silent at the head of the clip);
	// total and beyond are silent.
	if buf.Samples[0] != 1 || buf.Samples[1] != 0 {
		t.Fatalf("tail samples = %v, want [1 0 ...]", buf.Samples[:2])
	}
	for j := 2; j < buf.Count; j++ {
		if buf.Samples[j] != 0 {
			t.Fatalf("past-end sample %d = %f, want 0", j, buf.Samples[j])
		}
	}
}

// TestReverseUnboundedUnchanged checks an unbounded source is returned as-is.
func TestReverseUnboundedUnchanged(t *testing.T) {
	unbounded := newFake(sr, 1, 0, func(int) float32 { return 0 })
	if got := audio.Reverse(unbounded); got != unbounded {
		t.Error("reversing an unbounded clip should return it unchanged")
	}
}
