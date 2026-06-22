package audio_test

import (
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// ramp is a source whose sample value equals its frame index, so a remap is
// observable directly in the sample values.
func ramp(dur clip.Time) *fakeClip {
	return newFake(sr, 1, dur, func(i int) float32 { return float32(i) })
}

func TestSubclipShift(t *testing.T) {
	// End-based window [500ms, 750ms) -> 250ms duration at offset 500ms.
	sub := audio.Subclip(ramp(time.Second), 500*time.Millisecond, 750*time.Millisecond)
	d := sub.Duration()
	if d != 250*time.Millisecond {
		t.Fatalf("subclip duration = %v, want 250ms", d)
	}
	off := clip.SampleIndex(500*time.Millisecond, sr)
	buf := pull(t, sub, 0, 10)
	for j := 0; j < buf.Count; j++ {
		if want := float32(off + j); buf.Samples[j] != want {
			t.Fatalf("subclip sample %d = %f, want %f", j, buf.Samples[j], want)
		}
	}
}

// TestSubclipEndRules mirrors video.Subclip: an omitted/zero end runs to the
// source end, a negative end counts back from it, and an unbounded source stays
// unbounded rather than gaining a multi-year length.
func TestSubclipEndRules(t *testing.T) {
	// b == 0 -> to the end: [250ms, 1s) = 750ms.
	if d := audio.Subclip(ramp(time.Second), 250*time.Millisecond, 0).Duration(); d != 750*time.Millisecond {
		t.Errorf("open-end duration = %v, want 750ms", d)
	}
	// Negative end counts back from the source end: [0, 1s-250ms) = 750ms.
	if d := audio.Subclip(ramp(time.Second), 0, -250*time.Millisecond).Duration(); d != 750*time.Millisecond {
		t.Errorf("negative-end duration = %v, want 750ms", d)
	}
	// An unbounded source (dur 0 => Infinite) with an open end must stay Infinite,
	// not become ~MaxInt64.
	unbounded := newFake(sr, 1, 0, func(int) float32 { return 0 })
	if d := audio.Subclip(unbounded, 250*time.Millisecond, 0).Duration(); clip.Finite(d) {
		t.Errorf("unbounded open-end duration = %v, want Infinite", d)
	}
	// An absolute positive window over an unbounded source IS resolvable.
	if d := audio.Subclip(unbounded, 250*time.Millisecond, 750*time.Millisecond).Duration(); d != 500*time.Millisecond {
		t.Errorf("unbounded bounded-window duration = %v, want 500ms", d)
	}
}

// TestSubclipZeroesPastTrim: a trimmed clip must fall silent at and past its own
// duration even when the inner source has more data, so a bare trimmed sidecar
// attached to a longer video (pulled to the video duration) does not overplay.
func TestSubclipZeroesPastTrim(t *testing.T) {
	// 1s ramp trimmed to the first 250ms. ramp(i) == i, offset 0, so an in-window
	// output frame at local index idx has value idx.
	sub := audio.Subclip(ramp(time.Second), 0, 250*time.Millisecond)
	durIdx := clip.SampleIndex(250*time.Millisecond, sr)

	// A chunk that straddles the trim end: valid before durIdx, silent at/after.
	start := 200 * time.Millisecond
	base := clip.SampleIndex(start, sr)
	buf := pull(t, sub, start, 5000)
	for j := 0; j < buf.Count; j++ {
		idx := base + j
		got := buf.Samples[j]
		switch {
		case idx < durIdx && got != float32(idx):
			t.Fatalf("in-window sample %d (idx %d) = %f, want %f", j, idx, got, float32(idx))
		case idx >= durIdx && got != 0:
			t.Fatalf("sample %d (idx %d) = %f past trim, want silence", j, idx, got)
		}
	}

	// A chunk entirely past the trim is all silence.
	past := pull(t, sub, 300*time.Millisecond, 1000)
	for j, s := range past.Samples {
		if s != 0 {
			t.Fatalf("past-trim sample %d = %f, want silence", j, s)
		}
	}
}

// TestLoopZeroesPastEnd: a loop must fall silent at and past n repeats even when
// pulled further, so a bare looped sidecar on a longer video does not loop
// forever.
func TestLoopZeroesPastEnd(t *testing.T) {
	lp := audio.Loop(ramp(250*time.Millisecond), 2) // dur 500ms
	durIdx := clip.SampleIndex(500*time.Millisecond, sr)
	srcLen := clip.SampleIndex(250*time.Millisecond, sr)

	start := 480 * time.Millisecond
	base := clip.SampleIndex(start, sr)
	buf := pull(t, lp, start, 2000) // crosses the 500ms loop end
	for j := 0; j < buf.Count; j++ {
		s := base + j
		got := buf.Samples[j]
		switch {
		case s < durIdx && got != float32(s%srcLen):
			t.Fatalf("in-window sample %d (s %d) = %f, want %f", j, s, got, float32(s%srcLen))
		case s >= durIdx && got != 0:
			t.Fatalf("sample %d (s %d) = %f past loop end, want silence", j, s, got)
		}
	}
}

func TestSpeedUp(t *testing.T) {
	sp := audio.Speed(ramp(time.Second), 2)
	d := sp.Duration()
	if d != 500*time.Millisecond {
		t.Fatalf("speed duration = %v, want 500ms", d)
	}
	buf := pull(t, sp, 0, 10)
	for j := 0; j < buf.Count; j++ {
		// Source index for output j is nearest to 2*j at the same rate.
		if want := float32(2 * j); buf.Samples[j] != want {
			t.Fatalf("speed sample %d = %f, want %f", j, buf.Samples[j], want)
		}
	}
}

// TestSpeedInvalidFactor: a zero or negative factor has no monotonic
// output→source map, so audio.Speed defensively returns the inner clip
// unchanged (the facade surfaces ErrInvalidSpeed instead).
func TestSpeedInvalidFactor(t *testing.T) {
	inner := ramp(time.Second)
	for _, f := range []float64{0, -1, -2.5} {
		got := audio.Speed(inner, f)
		if got != inner {
			t.Fatalf("Speed(inner, %v) = %v, want inner unchanged", f, got)
		}
	}
	// A positive factor still wraps.
	if sp := audio.Speed(inner, 2); sp == inner {
		t.Fatal("Speed(inner, 2) returned inner; want a speed node")
	}
}

func TestLoopWrap(t *testing.T) {
	srcDur := 500 * time.Millisecond
	lp := audio.Loop(ramp(srcDur), 3)
	d := lp.Duration()
	if d != 1500*time.Millisecond {
		t.Fatalf("loop duration = %v, want 1.5s", d)
	}
	wrap := clip.SampleIndex(srcDur, sr) // first frame after wrapping
	buf := pull(t, lp, 0, wrap+5)
	if buf.Samples[wrap-1] != float32(wrap-1) {
		t.Fatalf("pre-wrap sample = %f, want %f", buf.Samples[wrap-1], float32(wrap-1))
	}
	if buf.Samples[wrap] != 0 {
		t.Fatalf("post-wrap sample = %f, want 0 (restart)", buf.Samples[wrap])
	}
	if buf.Samples[wrap+1] != 1 {
		t.Fatalf("post-wrap+1 sample = %f, want 1", buf.Samples[wrap+1])
	}
}

// TestLoopWrapUnaligned uses a source whose duration is not a whole number of
// samples (100µs @ 48kHz = 4.8 samples, so srcLen = 4). Output sample s must map
// to source sample s%4 with no drift, drop, or duplicate across wraps.
func TestLoopWrapUnaligned(t *testing.T) {
	srcDur := 100 * time.Microsecond
	srcLen := clip.SampleIndex(srcDur, sr) // floor(4.8) = 4
	if srcLen != 4 {
		t.Fatalf("srcLen = %d, want 4 (test setup)", srcLen)
	}
	lp := audio.Loop(ramp(srcDur), 3)
	buf := pull(t, lp, 0, 3*srcLen+2)
	for s := 0; s < buf.Count; s++ {
		if want := float32(s % srcLen); buf.Samples[s] != want {
			t.Fatalf("loop sample %d = %f, want %f (s%%%d)", s, buf.Samples[s], want, srcLen)
		}
	}
}
