package video

import (
	"context"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
)

// TimeTransformNode maps output time to source time (subclip, speed, loop,
// reverse). It owns no pixels: rendering delegates to the inner clip at the
// mapped time. SourceAccess is set by the constructor that builds it.
type TimeTransformNode struct {
	inner  VideoClip
	mapT   func(clip.Time) clip.Time
	dur    clip.Time
	hasDur bool
	start  clip.Time
	access AccessClass
	// audioClip is the inner audio remapped by the same time transform (built by
	// the Subclip/MultiplySpeed/Loop constructors), so a trimmed or sped-up clip
	// carries trimmed or sped-up audio. It is nil when the inner clip has none.
	audioClip audio.AudioClip
	// filterFn, when set, advertises the time transform as an FFmpeg filter
	// (trim+setpts for a forward Subclip, setpts for a positive MultiplySpeed)
	// so a linear graph can fuse. Random-access transforms (Loop, reverse) leave
	// it nil and force the Go path.
	filterFn func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool)
}

// Timeline metadata.

func (n *TimeTransformNode) Start() clip.Time { return n.start }

func (n *TimeTransformNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }

func (n *TimeTransformNode) End() clip.Time { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *TimeTransformNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	if c.audioClip != nil {
		c.audioClip = c.audioClip.WithStart(t).(audio.AudioClip)
	}
	return &c
}

func (n *TimeTransformNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	if c.audioClip != nil {
		c.audioClip = c.audioClip.WithDuration(d).(audio.AudioClip)
	}
	return &c
}

func (n *TimeTransformNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	if c.audioClip != nil {
		c.audioClip = c.audioClip.WithEnd(end, changeDuration).(audio.AudioClip)
	}
	return &c
}

// Video metadata mirrors the inner clip except for the remapped timeline.

func (n *TimeTransformNode) Size() clip.Size           { return n.inner.Size() }
func (n *TimeTransformNode) Rate() (clip.Rate, bool)   { return n.inner.Rate() }
func (n *TimeTransformNode) HasMask() bool             { return n.inner.HasMask() }
func (n *TimeTransformNode) Audio() audio.AudioClip    { return n.audioClip }
func (n *TimeTransformNode) ParallelSafe() bool        { return n.inner.ParallelSafe() }
func (n *TimeTransformNode) SourceAccess() AccessClass { return n.access }

// Children exposes the wrapped clip for planner graph traversal.
func (n *TimeTransformNode) Children() []VideoClip { return []VideoClip{n.inner} }

// Filter advertises the time remap as a filtergraph fragment when the
// constructor set one (forward subclip / positive speed); otherwise the node is
// not FFmpeg-expressible and the planner falls back to Go rendering.
func (n *TimeTransformNode) Filter(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
	if n.filterFn == nil {
		return ffmpeg.FilterFragment{}, false
	}
	return n.filterFn(ctx)
}

// Rendering delegates to the inner clip at the mapped source time.

func (n *TimeTransformNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	return n.inner.RenderInto(ctx, n.mapT(t), rgbDst, alphaDst)
}

func (n *TimeTransformNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	return n.inner.FrameInto(ctx, n.mapT(t), dst)
}

func (n *TimeTransformNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	return n.inner.MaskInto(ctx, n.mapT(t), dst)
}

// Close releases only what this node created. A time transform wraps a clip it
// does not own, so it closes nothing; the handle that opened the inner clip is
// responsible for closing it. This keeps a trimmed wrapper from tearing down
// the original source or sibling trims.
func (n *TimeTransformNode) Close() error { return nil }

// Subclip trims inner to the source window [a, b), mapping output t to a+t. A
// negative bound is taken relative to the source duration (counting back from
// the end), and b == 0 means "to the end". The end boundary tolerates one
// rational tick of overshoot before being clamped to the source duration. The
// window is clamped into [0, srcDur] and to a non-negative duration, so an
// out-of-range or inverted request never yields a negative-duration clip.
func Subclip(inner VideoClip, a, b clip.Time) VideoClip {
	srcDur := inner.Duration()
	if clip.Finite(srcDur) {
		if a < 0 {
			a = srcDur + a
		}
		if b <= 0 {
			b = srcDur + b
		}
		if rate, ok := inner.Rate(); ok {
			tol := rate.FrameTime(1)
			if b > srcDur && b-srcDur <= tol {
				b = srcDur
			}
		}
		a = clampTime(a, 0, srcDur)
		b = clampTime(b, 0, srcDur)
		if b < a {
			b = a
		}
		return &TimeTransformNode{
			inner:     inner,
			mapT:      func(t clip.Time) clip.Time { return a + t },
			dur:       b - a,
			hasDur:    true,
			access:    AccessLinear,
			audioClip: subclipAudio(inner.Audio(), a, b-a),
			filterFn:  trimFilter(a, b-a),
		}
	}
	// Unknown source duration: relative bounds (negative a, b<=0) cannot be
	// resolved against an end that is not known. Clamp a non-negative absolute
	// start, and only claim a duration when b is an absolute positive bound;
	// otherwise leave the duration unknown rather than fabricating b-a.
	if a < 0 {
		a = 0
	}
	dur := b - a
	resolved := b > a
	var au audio.AudioClip
	var ff func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool)
	if resolved {
		au = subclipAudio(inner.Audio(), a, dur)
		ff = trimFilter(a, dur)
	}
	return &TimeTransformNode{
		inner:     inner,
		mapT:      func(t clip.Time) clip.Time { return a + t },
		dur:       dur,
		hasDur:    resolved,
		access:    AccessLinear,
		audioClip: au,
		filterFn:  ff,
	}
}

// trimFilter builds the filtergraph fragment for a forward subclip: a trim to
// [start, start+dur) with the PTS rebased to zero.
func trimFilter(start, dur clip.Time) func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
	return func(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
		in := ctx.Inputs[0]
		line := ffmpeg.Trim(in.Label, ctx.OutLabel, start, dur)
		return ffmpeg.FilterFragment{
			Lines: []string{line},
			Out:   ffmpeg.Pad{Label: ctx.OutLabel, Rate: in.Rate, PixFmt: in.PixFmt},
		}, true
	}
}

// subclipAudio trims the sidecar audio to the same [offset, offset+dur) window
// the video was already resolved to, or returns nil when there is no audio. It
// uses the dur-based primitive because the window is pre-resolved.
func subclipAudio(a audio.AudioClip, offset, dur clip.Time) audio.AudioClip {
	if a == nil {
		return nil
	}
	return audio.SubclipDur(a, offset, dur)
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

// MultiplySpeed plays inner faster (factor > 1) or slower (factor < 1) by
// mapping output time t to source time t*factor and scaling the duration by
// 1/factor. Only positive factors are supported here (monotonic source access,
// AccessLinear); reverse/negative speed would require random-access reads.
func MultiplySpeed(inner VideoClip, factor float64) VideoClip {
	dur := inner.Duration()
	hasDur := clip.Finite(dur)
	out := dur
	if hasDur {
		out = clip.Time(float64(dur) / factor)
	}
	var au audio.AudioClip
	if a := inner.Audio(); a != nil {
		au = audio.Speed(a, factor)
	}
	return &TimeTransformNode{
		inner:     inner,
		mapT:      func(t clip.Time) clip.Time { return clip.Time(float64(t) * factor) },
		dur:       out,
		hasDur:    hasDur,
		access:    AccessLinear,
		audioClip: au,
		filterFn: func(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
			in := ctx.Inputs[0]
			return ffmpeg.FilterFragment{
				Lines: []string{ffmpeg.SetPTS(in.Label, ctx.OutLabel, factor)},
				Out:   ffmpeg.Pad{Label: ctx.OutLabel, Rate: in.Rate, PixFmt: in.PixFmt},
			}, true
		},
	}
}

// Loop repeats inner n times back-to-back by wrapping output time into the
// source window with a modulo. Because the source is read from the start on
// each pass, source access is random. When the inner duration is unknown the
// modulo cannot be formed, so the node degrades to an identity passthrough that
// inherits the inner clip's access class rather than falsely claiming random
// access (which would mislead the export planner).
func Loop(inner VideoClip, n int) VideoClip {
	srcDur := inner.Duration()
	if clip.Finite(srcDur) && srcDur > 0 {
		var au audio.AudioClip
		if a := inner.Audio(); a != nil {
			au = audio.Loop(a, n)
		}
		return &TimeTransformNode{
			inner:     inner,
			mapT:      func(t clip.Time) clip.Time { return t % srcDur },
			dur:       srcDur * clip.Time(n),
			hasDur:    true,
			access:    AccessRandom,
			audioClip: au,
		}
	}
	return &TimeTransformNode{
		inner:     inner,
		mapT:      func(t clip.Time) clip.Time { return t },
		hasDur:    false,
		access:    inner.SourceAccess(),
		audioClip: inner.Audio(),
	}
}

// TransformNode applies a pure per-frame pixel transform to its inner clip,
// optionally changing the output size (resize, crop, rotate) or just the colors
// (fade). The inner-size source frame it renders into is scratch: it comes from
// a per-node pool and is returned after the transform runs, so a high frame
// count (and the parallel pipeline's many concurrent calls) reuses buffers
// instead of allocating one per frame. The pool is the node's only shared state
// and is itself concurrency-safe, so RenderInto stays safe to call concurrently
// when the inner clip is parallel-safe.
//
// rgbFn fills the caller's outSize RGB24 destination from an inner-size source
// frame. maskFn does the same for the Gray8 mask; when it is nil the inner mask
// passes through unchanged (valid only when the size is preserved).
type TransformNode struct {
	inner   VideoClip
	outSize clip.Size
	rgbFn   func(t clip.Time, dst, src *clip.Frame) error
	maskFn  func(t clip.Time, dst, src *clip.Frame) error
	scratch *clip.FramePool
	// filterFn, when set by the effect that built this node, advertises the
	// equivalent FFmpeg filter (scale/crop/fade) so the graph can fuse. It is
	// nil for transforms with no FFmpeg expression, which forces the Go path.
	filterFn func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool)
}

// NewTransform wraps inner with a per-frame transform producing outSize frames.
func NewTransform(inner VideoClip, outSize clip.Size, rgbFn, maskFn func(t clip.Time, dst, src *clip.Frame) error) *TransformNode {
	return &TransformNode{inner: inner, outSize: outSize, rgbFn: rgbFn, maskFn: maskFn, scratch: clip.NewFramePool()}
}

// WithFilter records the FFmpeg-filter advertisement for this transform and
// returns the node, so an effect can build the Go transform and its fused
// equivalent in one place. It mutates and returns the receiver (called at
// construction, before the node is shared).
func (n *TransformNode) WithFilter(f func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool)) *TransformNode {
	n.filterFn = f
	return n
}

// Filter advertises the transform as a filtergraph fragment when one was set.
func (n *TransformNode) Filter(ctx ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool) {
	if n.filterFn == nil {
		return ffmpeg.FilterFragment{}, false
	}
	return n.filterFn(ctx)
}

// Timeline metadata delegates to the inner clip so placement and the With*
// builders propagate to the mask/audio through the inner clip's own methods.

func (n *TransformNode) Start() clip.Time    { return n.inner.Start() }
func (n *TransformNode) Duration() clip.Time { return n.inner.Duration() }
func (n *TransformNode) End() clip.Time      { return n.inner.End() }

func (n *TransformNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.inner = n.inner.WithStart(t).(VideoClip)
	return &c
}

func (n *TransformNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.inner = n.inner.WithDuration(d).(VideoClip)
	return &c
}

func (n *TransformNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	c.inner = n.inner.WithEnd(end, changeDuration).(VideoClip)
	return &c
}

// Video metadata mirrors the inner clip except for the transformed size.

func (n *TransformNode) Size() clip.Size           { return n.outSize }
func (n *TransformNode) Rate() (clip.Rate, bool)   { return n.inner.Rate() }
func (n *TransformNode) HasMask() bool             { return n.inner.HasMask() }
func (n *TransformNode) Audio() audio.AudioClip    { return n.inner.Audio() }
func (n *TransformNode) ParallelSafe() bool        { return n.inner.ParallelSafe() }
func (n *TransformNode) SourceAccess() AccessClass { return n.inner.SourceAccess() }

// Children exposes the wrapped clip for planner graph traversal.
func (n *TransformNode) Children() []VideoClip { return []VideoClip{n.inner} }

// Rendering.

func (n *TransformNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	in := n.inner.Size()
	srcRGB := n.scratch.Get(in.W, in.H, clip.RGB24)
	defer srcRGB.Release()
	var srcAlpha *clip.Frame
	if n.inner.HasMask() {
		srcAlpha = n.scratch.Get(in.W, in.H, clip.Gray8)
		defer srcAlpha.Release()
	}
	hasAlpha, err := n.inner.RenderInto(ctx, t, srcRGB, srcAlpha)
	if err != nil {
		return false, err
	}
	if err := n.rgbFn(t, rgbDst, srcRGB); err != nil {
		return false, err
	}
	if hasAlpha && alphaDst != nil {
		if err := n.applyMask(t, alphaDst, srcAlpha); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (n *TransformNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	in := n.inner.Size()
	srcRGB := n.scratch.Get(in.W, in.H, clip.RGB24)
	defer srcRGB.Release()
	if err := n.inner.FrameInto(ctx, t, srcRGB); err != nil {
		return err
	}
	return n.rgbFn(t, dst, srcRGB)
}

func (n *TransformNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	if !n.inner.HasMask() {
		return false, nil
	}
	in := n.inner.Size()
	srcAlpha := n.scratch.Get(in.W, in.H, clip.Gray8)
	defer srcAlpha.Release()
	ok, err := n.inner.MaskInto(ctx, t, srcAlpha)
	if err != nil || !ok {
		return false, err
	}
	if err := n.applyMask(t, dst, srcAlpha); err != nil {
		return false, err
	}
	return true, nil
}

// applyMask transforms the inner mask into dst, falling back to a verbatim copy
// when no mask transform is set (size-preserving effects like fade).
func (n *TransformNode) applyMask(t clip.Time, dst, src *clip.Frame) error {
	if n.maskFn != nil {
		return n.maskFn(t, dst, src)
	}
	copyPixels(dst, src)
	return nil
}

// Close releases only what this node created: nothing. The handle that opened
// the inner clip owns its lifecycle.
func (n *TransformNode) Close() error { return nil }
