package audio

import (
	"context"

	"github.com/mowshon/moviego/v2/clip"
)

// Reverse plays inner audio backward: output sample s reads source sample
// total-1-s, where total is the source's sample-frame count. It is a pure
// sample flip — the rate is unchanged and no interpolation happens — so a
// reversed clip stays perfectly aligned with a frame-reflected reversed video.
// An unbounded source has no last sample to reflect from, so it is returned
// unchanged (matching Loop/Speed).
func Reverse(inner AudioClip) AudioClip {
	if inner == nil {
		return nil
	}
	dur := inner.Duration()
	if !clip.Finite(dur) {
		return inner
	}
	return &reverseNode{inner: inner, dur: dur, scratch: &clip.AudioBuffer{}}
}

type reverseNode struct {
	inner   AudioClip
	dur     clip.Time
	start   clip.Time
	scratch *clip.AudioBuffer
}

func (n *reverseNode) Start() clip.Time    { return n.start }
func (n *reverseNode) Duration() clip.Time { return n.dur }
func (n *reverseNode) End() clip.Time      { return n.start + n.dur }

func (n *reverseNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *reverseNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur = d
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *reverseNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur = end - c.start
	} else {
		c.start = end - c.dur
	}
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *reverseNode) SampleRate() int { return n.inner.SampleRate() }
func (n *reverseNode) Channels() int   { return n.inner.Channels() }
func (n *reverseNode) Close() error    { return nil }

// SamplesInto fills the forward-walking output chunk by pulling the mirrored
// source run in one contiguous read and copying it back to front. Output frames
// outside [0, outputEnd) stay silent, and the reversal pivot is always
// innerTotal (derived from inner.Duration()), so WithDuration changes only how
// many output samples are produced, never the frame that each output index
// mirrors: output j always maps to source innerTotal-1-(startIdx+j).
func (n *reverseNode) SamplesInto(ctx context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error {
	sr := n.inner.SampleRate()
	ch := n.inner.Channels()
	dst.Reset(count, ch)
	if count == 0 {
		return nil
	}
	clear(dst.Samples)

	// innerTotal is the reversal pivot: it stays fixed at the source's own
	// duration so that shortening or extending n.dur via WithDuration never
	// changes which source frame an output index mirrors.
	innerTotal := clip.SampleIndex(n.inner.Duration(), sr)
	// outputEnd caps how far into the output the caller can receive samples.
	outputEnd := clip.SampleIndex(n.dur, sr)
	startIdx := clip.SampleIndex(start, sr)

	// Output frames in [jLo, jHi) land inside [0, outputEnd) and map to a
	// non-negative source index (innerTotal-1-(startIdx+j) >= 0).
	jLo := 0
	if startIdx < 0 {
		jLo = -startIdx
	}
	jHi := count
	if rem := outputEnd - startIdx; rem < jHi {
		jHi = rem
	}
	// Cap at the last output frame whose mirrored source index is non-negative.
	if valid := innerTotal - startIdx; valid < jHi {
		jHi = valid
	}
	if jLo >= jHi {
		return nil
	}

	// Mirror: output frame j reads source frame innerTotal-1-(startIdx+j). j is
	// increasing so the source index decreases; the lowest source frame backs
	// the last in-window output, the highest backs the first.
	low := innerTotal - 1 - (startIdx + (jHi - 1))
	run := jHi - jLo
	if err := n.inner.SamplesInto(ctx, clip.SampleTime(low, sr), run, n.scratch); err != nil {
		return err
	}
	for j := jLo; j < jHi; j++ {
		src := innerTotal - 1 - (startIdx + j)
		m := src - low // index into the pulled run
		copy(dst.Samples[j*ch:(j+1)*ch], n.scratch.Samples[m*ch:(m+1)*ch])
	}
	return nil
}
