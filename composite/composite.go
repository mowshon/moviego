package composite

import (
	"context"
	"sort"

	"github.com/moviego/moviego/audio"
	"github.com/moviego/moviego/clip"
	"github.com/moviego/moviego/composite/blend"
	"github.com/moviego/moviego/video"
)

// Compile-time guarantee that a composite is a usable video clip.
var _ video.VideoClip = (*CompositeNode)(nil)

// Options configures a CompositeNode. A zero Size adopts the first child's
// size. When Transparent is true the canvas starts fully transparent and the
// composite carries an alpha sidecar; otherwise the canvas is filled with
// BGColor (default black) and the output is opaque.
type Options struct {
	Size        clip.Size
	BGColor     [3]byte
	Transparent bool
}

// CompositeNode layers children onto a canvas in z-order. It owns no mutable
// per-frame state: RenderInto allocates its scratch per call, so it is
// parallel-safe whenever all children are.
type CompositeNode struct {
	children    []CompositeChild // sorted by layer, stable (input order breaks ties)
	size        clip.Size
	bgColor     [3]byte
	transparent bool

	start   clip.Time
	dur     clip.Time
	hasDur  bool
	rate    clip.Rate
	hasRate bool
	access  video.AccessClass
}

// New builds a composite from children and options. Children are sorted by
// layer index (stable). The output rate is the max child rate; the duration is
// the latest child end when all child ends are known.
func New(children []CompositeChild, opts Options) *CompositeNode {
	cs := make([]CompositeChild, len(children))
	copy(cs, children)
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Layer < cs[j].Layer })

	size := opts.Size
	if (size == clip.Size{}) && len(cs) > 0 {
		size = cs[0].Clip.Size()
	}

	n := &CompositeNode{
		children:    cs,
		size:        size,
		bgColor:     opts.BGColor,
		transparent: opts.Transparent,
	}
	n.rate, n.hasRate = maxChildRate(cs)
	n.dur, n.hasDur = latestChildEnd(cs)
	n.access = childAccess(cs)
	return n
}

func maxChildRate(cs []CompositeChild) (clip.Rate, bool) {
	var r clip.Rate
	has := false
	for _, c := range cs {
		if cr, ok := c.Clip.Rate(); ok {
			if !has {
				r, has = cr, true
			} else {
				r = clip.MaxRate(r, cr)
			}
		}
	}
	return r, has
}

// latestChildEnd returns the max (child.Start + childDuration) across children.
// It is unknown if any child has an unknown duration, mirroring MoviePy's
// "duration only when all child ends are known".
func latestChildEnd(cs []CompositeChild) (clip.Time, bool) {
	var end clip.Time
	for _, c := range cs {
		d := c.Clip.Duration()
		if !clip.Finite(d) {
			return 0, false
		}
		if e := c.Start + d; e > end {
			end = e
		}
	}
	return end, len(cs) > 0
}

// childAccess returns the most-random access class among children: a composite
// is at most as parallel-friendly as its least-linear child.
func childAccess(cs []CompositeChild) video.AccessClass {
	acc := video.AccessStatic
	for _, c := range cs {
		if a := c.Clip.SourceAccess(); a > acc {
			acc = a
		}
	}
	return acc
}

// Timeline metadata.

func (n *CompositeNode) Start() clip.Time { return n.start }

func (n *CompositeNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }

func (n *CompositeNode) End() clip.Time { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *CompositeNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	return &c
}

func (n *CompositeNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	return &c
}

func (n *CompositeNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	return &c
}

// WithRate overrides the reported output rate (used by the planner to inject a
// concrete rate when children are all static).
func (n *CompositeNode) WithRate(r clip.Rate) *CompositeNode {
	c := *n
	c.rate, c.hasRate = r, true
	return &c
}

// Video metadata.

func (n *CompositeNode) Size() clip.Size         { return n.size }
func (n *CompositeNode) Rate() (clip.Rate, bool) { return n.rate, n.hasRate }
func (n *CompositeNode) HasMask() bool           { return n.transparent }

// Audio mixes every child's audio sidecar, each placed at the child's composite
// start so it plays in lockstep with that child's video (RenderInto renders a
// child at local time t-ch.Start; the mix gates and samples the child's audio
// over the same [ch.Start, ch.Start+dur) window). Children without audio
// contribute silence; the result is nil only when no child has audio. A single
// child is still wrapped in a Mix so its start placement is honored on render
// (a bare audio node ignores its own Start; only the mixer applies it).
func (n *CompositeNode) Audio() audio.AudioClip {
	tracks := make([]audio.AudioClip, 0, len(n.children))
	for _, ch := range n.children {
		a := ch.Clip.Audio()
		if a == nil {
			continue
		}
		tracks = append(tracks, a.WithStart(ch.Start).(audio.AudioClip))
	}
	if len(tracks) == 0 {
		return nil
	}
	var mix audio.AudioClip = audio.Mix(tracks...)
	// A duration override (WithDuration/WithEnd) shortens video gating; mirror it
	// onto the audio so a nested, shortened composite truncates its mix instead of
	// over-playing past the node's reported end. Skip when the duration is unknown
	// (applying zero would silence everything).
	if n.hasDur {
		mix = mix.WithDuration(n.dur).(audio.AudioClip)
	}
	return mix
}

func (n *CompositeNode) SourceAccess() video.AccessClass { return n.access }

// Children exposes the layered child clips for planner graph traversal.
func (n *CompositeNode) Children() []video.VideoClip {
	cs := make([]video.VideoClip, len(n.children))
	for i, c := range n.children {
		cs[i] = c.Clip
	}
	return cs
}

// ParallelSafe reports whether RenderInto may be called concurrently for
// different t: true only when every child is parallel-safe (scratch is per
// call, so the compositor itself adds no shared state).
func (n *CompositeNode) ParallelSafe() bool {
	for _, c := range n.children {
		if !c.Clip.ParallelSafe() {
			return false
		}
	}
	return true
}

// playing reports whether child c is active at composition time t, using the
// half-open interval [start, end). A child with an unknown duration is treated
// as playing for every t >= its start.
func playing(c CompositeChild, t clip.Time) bool {
	if t < c.Start {
		return false
	}
	if d := c.Clip.Duration(); clip.Finite(d) {
		return t < c.Start+d
	}
	return true
}

// RenderInto fills rgbDst (and, for a transparent composite, alphaDst) in one
// pass. Scratch frames are allocated per call, so concurrent calls for
// different t never share state.
func (n *CompositeNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// A transparent composite must write to an alpha buffer; allocate a throwaway
	// when the caller opts out of alpha output so the blend paths never dereference
	// nil. Mirror the pattern FrameInto already uses. Return false in this case to
	// signal that no alpha was produced for the caller.
	hasAlphaDst := alphaDst != nil
	if n.transparent && !hasAlphaDst {
		alphaDst = clip.NewFrame(n.size.W, n.size.H, clip.Gray8)
	}
	if n.transparent {
		clear(rgbDst.Pix)
		clear(alphaDst.Pix)
	} else {
		fillRGB(rgbDst, n.bgColor)
	}

	for _, ch := range n.children {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !playing(ch, t) {
			continue
		}
		childSize := ch.Clip.Size()
		// Placement animation is relative to the child's own playback time, so a
		// child starting at 1s begins animating from 0, not from the composite's
		// global t. Static positions ignore t, so this is a no-op for them.
		local := t - ch.Start
		px, py := ComputePosition(childSize, n.size, ch.Pos, local)
		dst, src, ok := Overlap(n.size, childSize, px, py)
		if !ok {
			continue
		}

		childRGB := clip.NewFrame(childSize.W, childSize.H, clip.RGB24)
		var childAlpha *clip.Frame
		if ch.Clip.HasMask() {
			childAlpha = clip.NewFrame(childSize.W, childSize.H, clip.Gray8)
		}
		hasA, err := ch.Clip.RenderInto(ctx, local, childRGB, childAlpha)
		if err != nil {
			return false, err
		}
		opaque := !hasA || regionMin(childAlpha, src) == 255
		n.blendRegion(rgbDst, alphaDst, childRGB, childAlpha, dst, src, opaque)
	}
	return n.transparent && hasAlphaDst, nil
}

// blendRegion blends one child's overlap region into the destination, selecting
// the compose_on path from the destination (transparent?) and source (opaque?)
// mask state.
func (n *CompositeNode) blendRegion(rgbDst, alphaDst, childRGB, childAlpha *clip.Frame, dst, src Rect, opaque bool) {
	for r := 0; r < dst.H; r++ {
		dRGB := rgbDst.Pix[(dst.Y+r)*rgbDst.Stride+dst.X*3:]
		sRGB := childRGB.Pix[(src.Y+r)*childRGB.Stride+src.X*3:]
		switch {
		case !n.transparent && opaque: // path a
			blend.CopyRGBRow(dRGB, sRGB, dst.W)
		case !n.transparent: // path c: masked source over opaque canvas
			sA := childAlpha.Pix[(src.Y+r)*childAlpha.Stride+src.X:]
			blend.OverOpaqueRow(dRGB, sRGB, sA, dst.W)
		case opaque: // path b: opaque source over transparent canvas
			blend.CopyRGBRow(dRGB, sRGB, dst.W)
			dA := alphaDst.Pix[(dst.Y+r)*alphaDst.Stride+dst.X:]
			blend.FillAlphaRow(dA, dst.W)
		default: // path d: both masked
			sA := childAlpha.Pix[(src.Y+r)*childAlpha.Stride+src.X:]
			dA := alphaDst.Pix[(dst.Y+r)*alphaDst.Stride+dst.X:]
			blend.OverRow(dRGB, sRGB, sA, dA, dst.W)
		}
	}
}

// FrameInto renders only the RGB (opaque-only convenience). A transparent
// composite still recovers un-premultiplied RGB in RenderInto, so a throwaway
// alpha buffer is supplied here.
func (n *CompositeNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	var alpha *clip.Frame
	if n.transparent {
		alpha = clip.NewFrame(n.size.W, n.size.H, clip.Gray8)
	}
	_, err := n.RenderInto(ctx, t, dst, alpha)
	return err
}

// MaskInto renders only the alpha sidecar (interactive convenience). It runs
// the full composite into a throwaway RGB buffer and keeps the alpha.
func (n *CompositeNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	if !n.transparent {
		return false, nil
	}
	rgb := clip.NewFrame(n.size.W, n.size.H, clip.RGB24)
	return n.RenderInto(ctx, t, rgb, dst)
}

// Close releases nothing: a composite never owns the children it was given.
func (n *CompositeNode) Close() error { return nil }

// regionMin returns the smallest alpha byte in the src rectangle of mask, or 0
// when mask is nil. A result of 255 means the region is fully opaque.
func regionMin(mask *clip.Frame, src Rect) byte {
	if mask == nil {
		return 0
	}
	min := byte(255)
	for r := 0; r < src.H; r++ {
		row := mask.Pix[(src.Y+r)*mask.Stride+src.X:]
		for x := 0; x < src.W; x++ {
			if row[x] < min {
				min = row[x]
				if min == 0 {
					return 0
				}
			}
		}
	}
	return min
}

// fillRGB paints every pixel of f with col. It writes the first row, then
// copies it down, so the per-pixel work happens once.
func fillRGB(f *clip.Frame, col [3]byte) {
	row := f.Pix[:f.W*3]
	for x := 0; x < f.W; x++ {
		row[x*3], row[x*3+1], row[x*3+2] = col[0], col[1], col[2]
	}
	for y := 1; y < f.H; y++ {
		copy(f.Pix[y*f.Stride:y*f.Stride+f.W*3], row)
	}
}
