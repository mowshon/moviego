package composite_test

import (
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite"
	"github.com/mowshon/moviego/v2/video"
)

// markerAudio is a mono test clip that emits a constant value over its content
// window [0, dur) and silence outside it (zero-padded like a real decoder). The
// distinct per-clip value lets a placement test read back which child is playing
// at a given composite time, which is exactly the A/V-sync property under test.
type markerAudio struct {
	val     float32
	rate    int
	samples int // content length in sample-frames
	dur     clip.Time
	start   clip.Time
}

func newMarker(val float32, d clip.Time, rate int) *markerAudio {
	return &markerAudio{val: val, rate: rate, samples: clip.SampleIndex(d, rate), dur: d}
}

func (m *markerAudio) Start() clip.Time    { return m.start }
func (m *markerAudio) Duration() clip.Time { return m.dur }
func (m *markerAudio) End() clip.Time      { return m.start + m.dur }

func (m *markerAudio) WithStart(t clip.Time) clip.Clip {
	c := *m
	c.start = t
	return &c
}

func (m *markerAudio) WithDuration(d clip.Time) clip.Clip {
	c := *m
	c.dur, c.samples = d, clip.SampleIndex(d, m.rate)
	return &c
}

func (m *markerAudio) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *m
	if changeDuration {
		c.dur = end - c.start
		c.samples = clip.SampleIndex(c.dur, m.rate)
	} else {
		c.start = end - c.dur
	}
	return &c
}

func (m *markerAudio) SampleRate() int { return m.rate }
func (m *markerAudio) Channels() int   { return 1 }
func (m *markerAudio) Close() error    { return nil }

func (m *markerAudio) SamplesInto(_ context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error {
	dst.Reset(count, 1)
	base := clip.SampleIndex(start, m.rate)
	for j := 0; j < count; j++ {
		if idx := base + j; idx >= 0 && idx < m.samples {
			dst.Samples[j] = m.val
		} else {
			dst.Samples[j] = 0
		}
	}
	return nil
}

// markerVideo builds a 4x4 color clip of duration d carrying a markerAudio
// sidecar emitting val.
func markerVideo(val float32, d clip.Time) video.VideoClip {
	vc := video.NewColor(clip.Size{W: 4, H: 4}, [3]byte{}).WithDuration(d).(video.VideoClip)
	return video.WithAudio(vc, newMarker(val, d, 48000))
}

// sampleAt pulls a single output frame at composite time t and returns channel 0.
func sampleAt(t *testing.T, a audio.AudioClip, at clip.Time) float32 {
	t.Helper()
	buf := &clip.AudioBuffer{}
	if err := a.SamplesInto(context.Background(), at, 1, buf); err != nil {
		t.Fatalf("SamplesInto(%v): %v", at, err)
	}
	return buf.Samples[0]
}

// TestCompositeSurfacesChildAudio verifies that a composite of two audio-bearing
// clips exports their mixed audio, each placed at its composite start so it stays
// in sync with the child's video. Before the fix CompositeNode.Audio() returned
// nil and the export was silent.
func TestCompositeSurfacesChildAudio(t *testing.T) {
	a := composite.CompositeChild{Clip: markerVideo(0.5, time.Second), Start: 0}
	b := composite.CompositeChild{Clip: markerVideo(0.25, time.Second), Start: time.Second}
	n := composite.New([]composite.CompositeChild{a, b}, composite.Options{Size: clip.Size{W: 4, H: 4}})

	au := n.Audio()
	if au == nil {
		t.Fatal("CompositeNode.Audio() = nil, want a mix of the children")
	}
	// Mid-first-clip plays A only; mid-second-clip plays B only (they abut at 1s).
	if got := sampleAt(t, au, 500*time.Millisecond); got != 0.5 {
		t.Fatalf("at 0.5s = %v, want child A (0.5)", got)
	}
	if got := sampleAt(t, au, 1500*time.Millisecond); got != 0.25 {
		t.Fatalf("at 1.5s = %v, want child B (0.25)", got)
	}
	// Past the latest child end is silent (half-open).
	if got := sampleAt(t, au, 2500*time.Millisecond); got != 0 {
		t.Fatalf("at 2.5s = %v, want silence", got)
	}
}

// TestCompositeOverlapSumsAudio: overlapping children are summed, which is the
// composite mixing semantic (and proves placement is honored, not just one child).
func TestCompositeOverlapSumsAudio(t *testing.T) {
	a := composite.CompositeChild{Clip: markerVideo(0.5, time.Second), Start: 0}
	b := composite.CompositeChild{Clip: markerVideo(0.25, time.Second), Start: 500 * time.Millisecond}
	n := composite.New([]composite.CompositeChild{a, b}, composite.Options{Size: clip.Size{W: 4, H: 4}})

	au := n.Audio()
	if got := sampleAt(t, au, 250*time.Millisecond); got != 0.5 {
		t.Fatalf("at 0.25s = %v, want A only (0.5)", got)
	}
	if got := sampleAt(t, au, 750*time.Millisecond); got != 0.75 {
		t.Fatalf("at 0.75s (overlap) = %v, want A+B (0.75)", got)
	}
	if got := sampleAt(t, au, 1250*time.Millisecond); got != 0.25 {
		t.Fatalf("at 1.25s = %v, want B only (0.25)", got)
	}
}

// TestCompositeNoAudioIsNil: a composite of audio-less clips surfaces no audio.
func TestCompositeNoAudioIsNil(t *testing.T) {
	plain := composite.CompositeChild{Clip: solid(4, 4, [3]byte{}, time.Second)}
	n := composite.New([]composite.CompositeChild{plain}, composite.Options{Size: clip.Size{W: 4, H: 4}})
	if au := n.Audio(); au != nil {
		t.Fatalf("Audio() = %v, want nil (no child has audio)", au)
	}
}

// TestConcatChainSequencesAudio: a chain plays each child's audio back to back,
// in sync with the video switch. Before the fix the concat exported silent.
func TestConcatChainSequencesAudio(t *testing.T) {
	chain, err := composite.ConcatChain([]video.VideoClip{
		markerVideo(0.5, time.Second),
		markerVideo(0.25, time.Second),
	})
	if err != nil {
		t.Fatalf("concat chain: %v", err)
	}
	au := chain.Audio()
	if au == nil {
		t.Fatal("ConcatChainNode.Audio() = nil, want a mix of the children")
	}
	if got := sampleAt(t, au, 500*time.Millisecond); got != 0.5 {
		t.Fatalf("at 0.5s = %v, want first clip (0.5)", got)
	}
	if got := sampleAt(t, au, 1500*time.Millisecond); got != 0.25 {
		t.Fatalf("at 1.5s = %v, want second clip (0.25)", got)
	}
}

// TestNestedShortenedCompositeTruncatesAudio: a composite shortened via
// WithDuration and nested inside a parent must truncate its audio mix to the
// override, not play the full child window. Video gates on the shortened
// duration, so audio must too — otherwise a nested shortened clip plays audio
// past where its video stops.
func TestNestedShortenedCompositeTruncatesAudio(t *testing.T) {
	inner := composite.New([]composite.CompositeChild{
		{Clip: markerVideo(0.5, 2*time.Second), Start: 0},
	}, composite.Options{Size: clip.Size{W: 4, H: 4}})
	short := inner.WithDuration(time.Second).(video.VideoClip)

	parent := composite.New([]composite.CompositeChild{
		{Clip: short, Start: 0},
	}, composite.Options{Size: clip.Size{W: 4, H: 4}})

	au := parent.Audio()
	if au == nil {
		t.Fatal("parent Audio() = nil, want a mix")
	}
	if got := sampleAt(t, au, 500*time.Millisecond); got != 0.5 {
		t.Fatalf("at 0.5s = %v, want 0.5 (inside shortened window)", got)
	}
	if got := sampleAt(t, au, 1500*time.Millisecond); got != 0 {
		t.Fatalf("at 1.5s = %v, want silence (shortened composite must truncate audio)", got)
	}
}

// TestNestedShortenedConcatTruncatesAudio: same property for a concat chain
// shortened via WithDuration and nested in a parent.
func TestNestedShortenedConcatTruncatesAudio(t *testing.T) {
	chain, err := composite.ConcatChain([]video.VideoClip{
		markerVideo(0.5, 2*time.Second),
		markerVideo(0.25, 2*time.Second),
	})
	if err != nil {
		t.Fatalf("concat chain: %v", err)
	}
	short := chain.WithDuration(time.Second).(video.VideoClip)

	parent := composite.New([]composite.CompositeChild{
		{Clip: short, Start: 0},
	}, composite.Options{Size: clip.Size{W: 4, H: 4}})

	au := parent.Audio()
	if got := sampleAt(t, au, 500*time.Millisecond); got != 0.5 {
		t.Fatalf("at 0.5s = %v, want 0.5 (first clip, inside window)", got)
	}
	if got := sampleAt(t, au, 1500*time.Millisecond); got != 0 {
		t.Fatalf("at 1.5s = %v, want silence (shortened chain must truncate audio)", got)
	}
}

// TestConcatComposeSurfacesAudio: ConcatCompose embeds CompositeNode, so it
// inherits the mixed-audio surfacing with cumulative placement.
func TestConcatComposeSurfacesAudio(t *testing.T) {
	cc, err := composite.ConcatCompose([]video.VideoClip{
		markerVideo(0.5, time.Second),
		markerVideo(0.25, time.Second),
	}, composite.ConcatOptions{})
	if err != nil {
		t.Fatalf("concat compose: %v", err)
	}
	au := cc.Audio()
	if au == nil {
		t.Fatal("ConcatComposeNode.Audio() = nil, want a mix")
	}
	if got := sampleAt(t, au, 500*time.Millisecond); got != 0.5 {
		t.Fatalf("at 0.5s = %v, want first clip (0.5)", got)
	}
	if got := sampleAt(t, au, 1500*time.Millisecond); got != 0.25 {
		t.Fatalf("at 1.5s = %v, want second clip (0.25)", got)
	}
}
