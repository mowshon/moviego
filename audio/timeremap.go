package audio

import (
	"context"
	"math"
	"time"

	"github.com/mowshon/moviego/v2/clip"
)

// TimeRemap warps inner audio onto a new timeline: output time t plays the
// source at mapT(t). It is the sidecar for video.TimeRemap and the Freeze
// family, built from the same output→source map so audio tracks the remapped
// video. Samples are linearly interpolated between adjacent source frames and
// the rate is unchanged: a slowed segment stretches the audio, a held (constant)
// region sustains the source level at that instant, and a decreasing region
// plays the source backward. Output frames outside [0, dur) are silent so a bare
// sidecar pulled to a longer composition stops at the clip end.
//
// A nil inner or map returns inner unchanged.
func TimeRemap(inner AudioClip, mapT func(clip.Time) clip.Time, dur clip.Time) AudioClip {
	if inner == nil || mapT == nil {
		return inner
	}
	return &remapNode{inner: inner, mapT: mapT, dur: dur, hasDur: clip.Finite(dur), scratch: &clip.AudioBuffer{}}
}

type remapNode struct {
	inner   AudioClip
	mapT    func(clip.Time) clip.Time
	dur     clip.Time
	hasDur  bool
	start   clip.Time
	scratch *clip.AudioBuffer
	posBuf  []float64
}

func (n *remapNode) Start() clip.Time    { return n.start }
func (n *remapNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }
func (n *remapNode) End() clip.Time      { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *remapNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	c.scratch = &clip.AudioBuffer{}
	c.posBuf = nil
	return &c
}

func (n *remapNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	c.scratch = &clip.AudioBuffer{}
	c.posBuf = nil
	return &c
}

func (n *remapNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	c.scratch = &clip.AudioBuffer{}
	c.posBuf = nil
	return &c
}

func (n *remapNode) SampleRate() int { return n.inner.SampleRate() }
func (n *remapNode) Channels() int   { return n.inner.Channels() }
func (n *remapNode) Close() error    { return nil }

func (n *remapNode) SamplesInto(ctx context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error {
	rate := n.inner.SampleRate()
	ch := n.inner.Channels()
	dst.Reset(count, ch)
	if count == 0 {
		return nil
	}
	clear(dst.Samples)

	startIdx := clip.SampleIndex(start, rate)
	durIdx := -1
	if n.hasDur {
		durIdx = clip.SampleIndex(n.dur, rate)
	}

	// First pass: map each in-window output frame to a fractional source-sample
	// position, recording the span we must pull. Out-of-window frames stay silent
	// (NaN marks them) so a single pull covers exactly the source range touched.
	if cap(n.posBuf) < count {
		n.posBuf = make([]float64, count)
	}
	pos := n.posBuf[:count]
	have := false
	var lo, hi float64
	for j := 0; j < count; j++ {
		s := startIdx + j
		if s < 0 || (durIdx >= 0 && s >= durIdx) {
			pos[j] = math.NaN()
			continue
		}
		p := float64(n.mapT(clip.SampleTime(s, rate))) * float64(rate) / float64(time.Second)
		if p < 0 {
			p = 0
		}
		pos[j] = p
		if !have {
			lo, hi, have = p, p, true
		} else if p < lo {
			lo = p
		} else if p > hi {
			hi = p
		}
	}
	if !have {
		return nil
	}

	srcLo := int(math.Floor(lo))
	if srcLo < 0 {
		srcLo = 0
	}
	srcCount := int(math.Floor(hi)) + 2 - srcLo // +1 span, +1 guard for the k+1 read
	if srcCount < 1 {
		srcCount = 1
	}
	if err := n.inner.SamplesInto(ctx, clip.SampleTime(srcLo, rate), srcCount, n.scratch); err != nil {
		return err
	}

	// Second pass: linearly interpolate each output frame from the pulled window.
	for j := 0; j < count; j++ {
		p := pos[j]
		if math.IsNaN(p) {
			continue
		}
		whole := math.Floor(p)
		k := int(whole) - srcLo
		frac := float32(p - whole)
		if k < 0 {
			k, frac = 0, 0
		} else if k >= srcCount {
			k, frac = srcCount-1, 0
		}
		k2 := k + 1
		if k2 >= srcCount {
			k2 = srcCount - 1
		}
		b0, b1, dBase := k*ch, k2*ch, j*ch
		for c := 0; c < ch; c++ {
			dst.Samples[dBase+c] = n.scratch.Samples[b0+c]*(1-frac) + n.scratch.Samples[b1+c]*frac
		}
	}
	return nil
}
