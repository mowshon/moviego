package audio

import (
	"context"
	"math"
	"time"

	"github.com/mowshon/moviego/v2/clip"
)

// AudioMixNode sums its children's samples over a shared timeline. The output
// sample rate is the max child rate and the channel count is the max child
// channel count; each child is gated to its half-open [start, end) window and
// remixed (mono children duplicated across channels) before being added. Sums
// are left un-clamped here — clamping happens once, at the quantization output
// boundary (the encoder).
//
// SamplesInto reuses a per-node scratch buffer, so a single node is NOT safe for
// concurrent SamplesInto calls; the audio render drives one mixer from one
// goroutine. Each With* variant gets its own scratch, and a nested mixer child
// has its own, so the single-goroutine constraint is per node, not global.
type AudioMixNode struct {
	children   []AudioClip
	sampleRate int
	channels   int
	dur        clip.Time
	hasDur     bool
	start      clip.Time

	scratch *clip.AudioBuffer // reused per-child pull buffer (single-render use)
}

// Mix builds a mixer from children. The output rate/channels are the max across
// children; the duration is the latest child end when every child end is known.
func Mix(children ...AudioClip) *AudioMixNode {
	n := &AudioMixNode{
		children: append([]AudioClip(nil), children...),
		scratch:  &clip.AudioBuffer{},
	}
	for _, c := range children {
		if r := c.SampleRate(); r > n.sampleRate {
			n.sampleRate = r
		}
		if ch := c.Channels(); ch > n.channels {
			n.channels = ch
		}
	}
	n.dur, n.hasDur = latestEnd(children)
	return n
}

// latestEnd returns the max child end, unknown if any child end is unknown.
func latestEnd(children []AudioClip) (clip.Time, bool) {
	if len(children) == 0 {
		return 0, false
	}
	var end clip.Time
	for _, c := range children {
		e := c.End()
		if !clip.Finite(e) {
			return 0, false
		}
		if e > end {
			end = e
		}
	}
	return end, true
}

// Timeline metadata.

func (n *AudioMixNode) Start() clip.Time { return n.start }

func (n *AudioMixNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }

func (n *AudioMixNode) End() clip.Time { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *AudioMixNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *AudioMixNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *AudioMixNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	c.scratch = &clip.AudioBuffer{}
	return &c
}

// Audio metadata.

func (n *AudioMixNode) SampleRate() int { return n.sampleRate }
func (n *AudioMixNode) Channels() int   { return n.channels }

// SamplesInto sums every active child into dst. dst is zeroed, then each child
// is pulled at its own rate and added within its [start, end) window, with
// channel up-mixing for mono sources.
func (n *AudioMixNode) SamplesInto(ctx context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dst.Reset(count, n.channels)
	clear(dst.Samples)
	if count == 0 {
		return nil
	}
	chunkSpan := clip.SampleTime(count, n.sampleRate)
	for _, c := range n.children {
		cs := c.Start()
		// Skip a child that does not overlap [start, start+chunkSpan).
		if start+chunkSpan <= cs {
			continue
		}
		if cend := c.End(); clip.Finite(cend) && start >= cend {
			continue
		}
		if err := n.addChild(ctx, c, start, count, dst); err != nil {
			return err
		}
	}
	return nil
}

// addChild pulls one child for the chunk and adds its gated, remixed samples
// into dst. The child is pulled at its child-local time; its own SamplesInto
// zero-pads outside the file, and the [start, end) window is enforced here.
func (n *AudioMixNode) addChild(ctx context.Context, c AudioClip, start clip.Time, count int, dst *clip.AudioBuffer) error {
	cs := c.Start()
	local := start - cs
	childRate := c.SampleRate()
	childCh := c.Channels()

	// Output-sample window that falls inside the child's [start, end).
	lo := firstSampleAtOrAfter(cs-start, n.sampleRate)
	hi := count
	if cend := c.End(); clip.Finite(cend) {
		hi = firstSampleAtOrAfter(cend-start, n.sampleRate)
	}
	lo = clampInt(lo, 0, count)
	hi = clampInt(hi, 0, count)
	if lo >= hi {
		return nil
	}

	if childRate == n.sampleRate {
		// 1:1 alignment: pull only the in-window run [lo, hi) so leading/trailing
		// frames outside the child's window are never decoded. idxOf is offset by lo
		// because scratch[0] corresponds to output frame lo.
		if err := c.SamplesInto(ctx, local+clip.SampleTime(lo, n.sampleRate), hi-lo, n.scratch); err != nil {
			return err
		}
		addWindow(dst, n.scratch, lo, hi, n.channels, childCh, func(j int) int { return j - lo })
		return nil
	}

	// Differing rates: pull the child range covering the chunk and linearly
	// interpolate each output sample from the two adjacent child samples. Linear
	// interpolation avoids the zipper/aliasing artifacts of nearest-neighbor at
	// a negligible cost; decoding each source at the mix rate
	// (DecoderOptions.SampleRate) would avoid this Go resampler and keep the 1:1
	// path. The +2 guard frames cover the k+1 read at the last output sample.
	// Note: because consecutive chunks share their boundary source frame, childLo
	// for the next chunk lands one frame behind the decoder's forward position,
	// so a file source here restarts FFmpeg per chunk — another reason the
	// decode-at-mix-rate path is preferred (see
	// docs/ffmpeg-policy.md).
	childLo := clip.SampleIndex(local, childRate)
	childHi := clip.SampleIndex(local+clip.SampleTime(count, n.sampleRate), childRate) + 2
	childCount := childHi - childLo
	if childCount < 1 {
		childCount = 1
	}
	if err := c.SamplesInto(ctx, clip.SampleTime(childLo, childRate), childCount, n.scratch); err != nil {
		return err
	}
	// posOf returns the scratch-relative fractional child position for output
	// frame j. Output sample (mixOff+j) plays at time (mixOff+j)/mixRate; the
	// child sample at that time is (that − childStart)·childRate. Working in
	// sample units (not via SampleTime) keeps an integer rate ratio exact.
	mixOff := clip.SampleIndex(start, n.sampleRate)
	csSec := float64(cs) / float64(time.Second)
	posOf := func(j int) (int, float32) {
		pos := (float64(mixOff+j)/float64(n.sampleRate)-csSec)*float64(childRate) - float64(childLo)
		whole := math.Floor(pos)
		return int(whole), float32(pos - whole)
	}
	addWindowLerp(dst, n.scratch, lo, hi, n.channels, childCh, posOf)
	return nil
}

// addWindow adds src samples for output frames [lo, hi) into dst, mapping the
// output frame index to a src frame index via idxOf and up-mixing channels
// (the last src channel is duplicated when dst has more channels).
func addWindow(dst, src *clip.AudioBuffer, lo, hi, dstCh, srcCh int, idxOf func(j int) int) {
	for j := lo; j < hi; j++ {
		s := idxOf(j)
		if s < 0 || s >= src.Count {
			continue
		}
		sBase := s * srcCh
		dBase := j * dstCh
		for ch := 0; ch < dstCh; ch++ {
			sc := ch
			if sc >= srcCh {
				sc = srcCh - 1
			}
			dst.Samples[dBase+ch] += src.Samples[sBase+sc]
		}
	}
}

// addWindowLerp adds linearly interpolated src samples for output frames
// [lo, hi) into dst. posOf returns, for an output frame, the integer src frame
// index k and the fractional offset frac in [0, 1) toward k+1; the output is
// src[k]*(1-frac) + src[k+1]*frac, up-mixed (the last src channel is duplicated
// when dst has more channels). Indices are clamped to the pulled src range, so
// the edge sample degrades to a hold rather than reading out of bounds.
func addWindowLerp(dst, src *clip.AudioBuffer, lo, hi, dstCh, srcCh int, posOf func(j int) (int, float32)) {
	for j := lo; j < hi; j++ {
		k, frac := posOf(j)
		if k < 0 {
			k, frac = 0, 0
		}
		if k >= src.Count {
			continue
		}
		k2 := k + 1
		if k2 >= src.Count {
			k2 = src.Count - 1
		}
		b0, b1, dBase := k*srcCh, k2*srcCh, j*dstCh
		for ch := 0; ch < dstCh; ch++ {
			sc := ch
			if sc >= srcCh {
				sc = srcCh - 1
			}
			dst.Samples[dBase+ch] += src.Samples[b0+sc]*(1-frac) + src.Samples[b1+sc]*frac
		}
	}
}

// Close releases nothing: a mixer never owns the children it was given.
func (n *AudioMixNode) Close() error { return nil }

// firstSampleAtOrAfter returns the smallest sample index whose timestamp is >= d
// at sample rate sr (the ceil counterpart to SampleIndex's floor). A negative d
// returns a negative index; callers clamp.
func firstSampleAtOrAfter(d clip.Time, sr int) int {
	i := clip.SampleIndex(d, sr)
	if clip.SampleTime(i, sr) < d {
		i++
	}
	return i
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
