package effect

import (
	"context"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/keyframe"
	"github.com/mowshon/moviego/v2/video"
)

// ErrNoKeyframes reports an animated effect built with an empty keyframe track:
// there is nothing to interpolate.
var ErrNoKeyframes = clip.Wrap("animate: no keyframes", clip.ErrVideoCorrupted)

// Opacity animates a clip's alpha over clip-local time. It attaches a mask (so
// the clip becomes transparent where opacity < 1) and scales any existing mask
// by the keyframed value, clamped to [0, 1]. The opacity is visible when the
// clip is composited; a standalone export has no alpha channel to carry it. The
// RGB is untouched. Reach it through the facade's Animate(PropOpacity, …).
type Opacity struct {
	Track keyframe.Track
}

func (o Opacity) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (o Opacity) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if o.Track.Empty() {
		return nil, ErrNoKeyframes
	}
	return &opacityNode{inner: c, track: o.Track, scratch: clip.NewFramePool()}, nil
}

// opacityNode wraps a clip and synthesizes (or scales) its alpha sidecar from a
// keyframe track. It always reports a mask: the gainNode of the video side. The
// scratch pool is its only shared state and is concurrency-safe, so RenderInto
// stays parallel-safe when the inner clip is.
type opacityNode struct {
	inner   video.VideoClip
	track   keyframe.Track
	scratch *clip.FramePool
}

func (n *opacityNode) Start() clip.Time    { return n.inner.Start() }
func (n *opacityNode) Duration() clip.Time { return n.inner.Duration() }
func (n *opacityNode) End() clip.Time      { return n.inner.End() }

func (n *opacityNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.inner = n.inner.WithStart(t).(video.VideoClip)
	return &c
}

func (n *opacityNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.inner = n.inner.WithDuration(d).(video.VideoClip)
	return &c
}

func (n *opacityNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	c.inner = n.inner.WithEnd(end, changeDuration).(video.VideoClip)
	return &c
}

func (n *opacityNode) Size() clip.Size                 { return n.inner.Size() }
func (n *opacityNode) Rate() (clip.Rate, bool)         { return n.inner.Rate() }
func (n *opacityNode) HasMask() bool                   { return true }
func (n *opacityNode) Audio() audio.AudioClip          { return n.inner.Audio() }
func (n *opacityNode) ParallelSafe() bool              { return n.inner.ParallelSafe() }
func (n *opacityNode) SourceAccess() video.AccessClass { return n.inner.SourceAccess() }
func (n *opacityNode) Children() []video.VideoClip     { return []video.VideoClip{n.inner} }

func (n *opacityNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	size := n.inner.Size()
	srcRGB := n.scratch.Get(size.W, size.H, clip.RGB24)
	defer srcRGB.Release()
	var srcAlpha *clip.Frame
	if n.inner.HasMask() {
		srcAlpha = n.scratch.Get(size.W, size.H, clip.Gray8)
		defer srcAlpha.Release()
	}
	hasAlpha, err := n.inner.RenderInto(ctx, t, srcRGB, srcAlpha)
	if err != nil {
		return false, err
	}
	copyFrameInto(rgbDst, srcRGB)
	if alphaDst != nil {
		scaleMaskInto(alphaDst, srcAlpha, hasAlpha, n.opacity(t))
		return true, nil
	}
	return false, nil
}

func (n *opacityNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	return n.inner.FrameInto(ctx, t, dst)
}

func (n *opacityNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	size := n.inner.Size()
	var srcAlpha *clip.Frame
	hasAlpha := false
	if n.inner.HasMask() {
		srcAlpha = n.scratch.Get(size.W, size.H, clip.Gray8)
		defer srcAlpha.Release()
		var err error
		if hasAlpha, err = n.inner.MaskInto(ctx, t, srcAlpha); err != nil {
			return false, err
		}
	}
	scaleMaskInto(dst, srcAlpha, hasAlpha, n.opacity(t))
	return true, nil
}

func (n *opacityNode) Close() error { return nil }

// opacity is the keyframed alpha at t, clamped to [0, 1].
func (n *opacityNode) opacity(t clip.Time) float64 {
	v := n.track.Eval(t)
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// scaleMaskInto fills dst with the source alpha scaled by opacity, or with a
// flat opacity level when the source has no mask. dst is Gray8.
func scaleMaskInto(dst, src *clip.Frame, hasSrc bool, opacity float64) {
	a := int(opacity*255 + 0.5)
	if !hasSrc {
		fillMask(dst, byte(a))
		return
	}
	for y := 0; y < dst.H; y++ {
		d := dst.Pix[y*dst.Stride : y*dst.Stride+dst.W]
		s := src.Pix[y*src.Stride : y*src.Stride+src.W]
		for x := range d {
			d[x] = byte((int(s[x])*a + 127) / 255)
		}
	}
}
