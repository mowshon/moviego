package audio

import (
	"context"
	"errors"
	"math"

	"github.com/mowshon/moviego/v2/clip"
)

// ErrInvalidSpeed reports a non-positive speed factor. Only forward (positive)
// speed has a monotonic output→source time map; zero or negative would yield an
// undefined or infinite duration.
var ErrInvalidSpeed = errors.New("audio speed: factor must be positive")

// Subclip trims inner audio to the source window [a, b), mapping output time t
// to source time a+t. It mirrors video.Subclip so a trimmed video's audio
// follows the same rules: a negative bound counts back from the source duration,
// and b == 0 means "to the end". With an unknown/unbounded source duration the
// relative bounds cannot be resolved, so only an absolute positive window claims
// a duration; otherwise the result stays unbounded rather than fabricating one.
// The window is clamped into [0, srcDur] and to a non-negative duration. (There
// is no frame-tick tolerance as in video: audio has no frames.)
func Subclip(inner AudioClip, a, b clip.Time) AudioClip {
	srcDur := inner.Duration()
	if clip.Finite(srcDur) {
		if a < 0 {
			a = srcDur + a
		}
		if b <= 0 {
			b = srcDur + b
		}
		a = clampTime(a, 0, srcDur)
		b = clampTime(b, 0, srcDur)
		if b < a {
			b = a
		}
		return newShift(inner, a, b-a, true)
	}
	if a < 0 {
		a = 0
	}
	return newShift(inner, a, b-a, b > a)
}

// SubclipDur trims inner over an already-resolved [offset, offset+dur) window,
// without the relative-bound resolution Subclip performs. It is the primitive
// used by video.Subclip, which has already resolved and clamped the window
// against the video duration so the audio sidecar matches the video exactly. A
// non-finite or negative dur leaves the duration unknown rather than claiming a
// bogus finite length.
func SubclipDur(inner AudioClip, offset, dur clip.Time) AudioClip {
	return newShift(inner, offset, dur, clip.Finite(dur) && dur >= 0)
}

// newShift builds a shiftNode with its own scratch buffer.
func newShift(inner AudioClip, offset, dur clip.Time, hasDur bool) *shiftNode {
	return &shiftNode{inner: inner, offset: offset, dur: dur, hasDur: hasDur, scratch: &clip.AudioBuffer{}}
}

func clampTime(t, lo, hi clip.Time) clip.Time {
	if t < lo {
		return lo
	}
	if t > hi {
		return hi
	}
	return t
}

// shiftNode offsets the source time by a fixed amount (source = offset + t) and
// reports an independent duration. It is the offset-only fast path: the rate is
// unchanged, so an output chunk maps 1:1 onto a contiguous source range. scratch
// holds the in-window source run so a pull past the trim never decodes the
// inner source for frames it would discard.
type shiftNode struct {
	inner   AudioClip
	offset  clip.Time
	dur     clip.Time
	hasDur  bool
	start   clip.Time
	scratch *clip.AudioBuffer
}

func (n *shiftNode) Start() clip.Time    { return n.start }
func (n *shiftNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }

func (n *shiftNode) End() clip.Time { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *shiftNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *shiftNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *shiftNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *shiftNode) SampleRate() int { return n.inner.SampleRate() }
func (n *shiftNode) Channels() int   { return n.inner.Channels() }
func (n *shiftNode) Close() error    { return nil }

func (n *shiftNode) SamplesInto(ctx context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error {
	if !n.hasDur {
		// Unbounded trim: the inner source's own zero-padding bounds the output.
		return n.inner.SamplesInto(ctx, n.offset+start, count, dst)
	}
	ch := n.inner.Channels()
	dst.Reset(count, ch)
	if count == 0 {
		return nil
	}
	clear(dst.Samples)

	// Enforce the trim's own [0, dur) window and pull ONLY the in-window run, so a
	// bare trimmed sidecar attached to a longer clip (pulled to the video duration
	// by render.renderAudio) neither overplays past the trim nor decodes the inner
	// source for frames it would discard. [start, start+dur) is half-open, matching
	// the mixer and source clips.
	sr := n.inner.SampleRate()
	base := clip.SampleIndex(start, sr)             // this trim's output frame for dst[0]
	srcBase := clip.SampleIndex(n.offset+start, sr) // inner output frame for dst[0]
	durIdx := clip.SampleIndex(n.dur, sr)

	// Overlap of [base, base+count) with [0, durIdx), in dst-frame coordinates.
	jLo := 0
	if base < 0 {
		jLo = -base
	}
	jHi := count
	if rem := durIdx - base; rem < jHi {
		jHi = rem
	}
	if jLo >= jHi {
		return nil // nothing in-window; dst stays silent
	}
	run := jHi - jLo
	// Pull the run at the inner frame aligned to dst[jLo]; SampleTime is the left
	// inverse of SampleIndex, so this lands on inner frame srcBase+jLo exactly.
	if err := n.inner.SamplesInto(ctx, clip.SampleTime(srcBase+jLo, sr), run, n.scratch); err != nil {
		return err
	}
	copy(dst.Samples[jLo*ch:jHi*ch], n.scratch.Samples[:run*ch])
	return nil
}

// Speed time-scales inner audio by factor (>1 faster/higher-pitched, like
// MoviePy): output time t maps to source time t*factor, and the duration scales
// by 1/factor. Samples are linearly interpolated between the two adjacent source
// frames; the rate is unchanged.
//
// Only positive factors are meaningful: a zero or negative factor has no
// monotonic output→source time map (and would yield an infinite or negative
// duration), so it is rejected by returning inner unchanged. Callers that want a
// surfaced error should validate before calling (the facade does).
func Speed(inner AudioClip, factor float64) AudioClip {
	if factor <= 0 {
		return inner
	}
	dur := inner.Duration()
	ok := clip.Finite(dur)
	out := dur
	if ok {
		out = clip.Time(float64(dur) / factor)
	}
	return &speedNode{inner: inner, factor: factor, dur: out, hasDur: ok, scratch: &clip.AudioBuffer{}}
}

type speedNode struct {
	inner   AudioClip
	factor  float64
	dur     clip.Time
	hasDur  bool
	start   clip.Time
	scratch *clip.AudioBuffer
}

func (n *speedNode) Start() clip.Time    { return n.start }
func (n *speedNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }

func (n *speedNode) End() clip.Time { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *speedNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *speedNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *speedNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *speedNode) SampleRate() int { return n.inner.SampleRate() }
func (n *speedNode) Channels() int   { return n.inner.Channels() }
func (n *speedNode) Close() error    { return nil }

func (n *speedNode) SamplesInto(ctx context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error {
	rate := n.inner.SampleRate()
	ch := n.inner.Channels()
	dst.Reset(count, ch)
	if count == 0 {
		return nil
	}
	// Fractional source sample position for output frame j. Input and output
	// share the rate, so output sample (startSample+j) maps to source sample
	// (startSample+j)*factor exactly — no clip.Time round-trip, so an integer
	// factor lands on integer source samples (frac == 0).
	startSample := clip.SampleIndex(start, rate)
	posAt := func(j int) float64 { return float64(startSample+j) * n.factor }
	srcLo := int(math.Floor(posAt(0)))
	srcHi := int(math.Floor(posAt(count))) + 2 // +1 span, +1 guard for the k+1 read
	srcCount := srcHi - srcLo
	if srcCount < 1 {
		srcCount = 1
	}
	if err := n.inner.SamplesInto(ctx, clip.SampleTime(srcLo, rate), srcCount, n.scratch); err != nil {
		return err
	}
	for j := 0; j < count; j++ {
		pos := posAt(j)
		whole := math.Floor(pos)
		k := int(whole) - srcLo
		frac := float32(pos - whole)
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

// Loop repeats inner audio n times back to back, wrapping output time into the
// source window with a modulo. When the inner duration is unknown the modulo
// cannot be formed and the clip is returned unchanged.
func Loop(inner AudioClip, n int) AudioClip {
	srcDur := inner.Duration()
	if !clip.Finite(srcDur) || srcDur <= 0 || n < 1 {
		return inner
	}
	return &loopNode{inner: inner, srcDur: srcDur, dur: srcDur * clip.Time(n), scratch: &clip.AudioBuffer{}}
}

type loopNode struct {
	inner   AudioClip
	srcDur  clip.Time
	dur     clip.Time
	start   clip.Time
	scratch *clip.AudioBuffer
}

func (n *loopNode) Start() clip.Time    { return n.start }
func (n *loopNode) Duration() clip.Time { return n.dur }
func (n *loopNode) End() clip.Time      { return n.start + n.dur }

func (n *loopNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *loopNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur = d
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *loopNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur = end - c.start
	} else {
		c.start = end - c.dur
	}
	c.scratch = &clip.AudioBuffer{}
	return &c
}

func (n *loopNode) SampleRate() int { return n.inner.SampleRate() }
func (n *loopNode) Channels() int   { return n.inner.Channels() }
func (n *loopNode) Close() error    { return nil }

// SamplesInto fills the chunk by copying contiguous runs from the source. The
// source is treated as exactly srcLen sample frames, and output sample s maps to
// source sample s % srcLen. Wrapping on the sample count (rather than a
// time-modulo paired with a separate sample-floor index) keeps the mapping exact
// when the source duration is not a whole number of samples, so no sample is
// dropped or duplicated at a loop boundary. Each run is one stretch up to the
// next wrap.
func (n *loopNode) SamplesInto(ctx context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error {
	rate := n.inner.SampleRate()
	ch := n.inner.Channels()
	dst.Reset(count, ch)
	srcLen := clip.SampleIndex(n.srcDur, rate)
	if srcLen < 1 {
		srcLen = 1
	}
	startIdx := clip.SampleIndex(start, rate)
	// The loop spans [0, dur) = n repeats; output frames outside it are silent so
	// a bare looped sidecar on a longer video stops after n repeats.
	durIdx := clip.SampleIndex(n.dur, rate)

	// [jLo, jHi) is the output window that falls inside [0, durIdx).
	jLo := 0
	if startIdx < 0 {
		jLo = -startIdx
	}
	jHi := count
	if rem := durIdx - startIdx; rem < count {
		jHi = rem
	}
	jLo = clampInt(jLo, 0, count)
	jHi = clampInt(jHi, 0, count)

	// Zero silent regions at the start and end in one shot.
	if jLo > 0 {
		clear(dst.Samples[:jLo*ch])
	}
	if jHi < count {
		clear(dst.Samples[jHi*ch:])
	}

	for j := jLo; j < jHi; {
		s := startIdx + j
		srcIdx := s % srcLen
		run := jHi - j
		if avail := srcLen - srcIdx; avail < run {
			run = avail
		}
		if err := n.inner.SamplesInto(ctx, clip.SampleTime(srcIdx, rate), run, n.scratch); err != nil {
			return err
		}
		copy(dst.Samples[j*ch:(j+run)*ch], n.scratch.Samples[:run*ch])
		j += run
	}
	return nil
}
