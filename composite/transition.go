package composite

import (
	"context"
	"errors"

	"github.com/mowshon/moviego/v2/audio"
	audiofx "github.com/mowshon/moviego/v2/audio/fx"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite/blend"
	"github.com/mowshon/moviego/v2/transition"
	"github.com/mowshon/moviego/v2/video"
)

// Transition assembly errors.
var (
	// ErrNilTransition is returned when a transition node is built with no clip
	// or no transition.
	ErrNilTransition = errors.New("transition: clip and transition must be non-nil")
	// ErrTransitionOverlap is returned when the overlap is not positive or is
	// longer than the clip span it consumes (so a body would be empty).
	ErrTransitionOverlap = errors.New("transition: overlap must be positive and fit within the clip duration")
	// ErrTransitionSize is returned when the two clips of a transition differ in
	// size (the assembler does not resize; pre-pad to a common size first).
	ErrTransitionSize = errors.New("transition: clips must share a size")
)

// CrossfadeCurve selects the gain shape used to mix A's tail with B's head
// across a transition's audio overlap.
type CrossfadeCurve int

const (
	// CrossfadeEqualPower keeps the summed power flat across the overlap (the
	// perceptually correct default; a linear pair dips ~3 dB at the midpoint).
	CrossfadeEqualPower CrossfadeCurve = iota
	// CrossfadeLinear ramps each side linearly (reuses audiofx.FadeIn/FadeOut).
	CrossfadeLinear
	// CrossfadeNone does not blend audio: A's tail plays through the overlap and
	// B's audio resumes at the body boundary.
	CrossfadeNone
)

// transitionNode is the two-input clip that covers the overlap window where A
// hands over to B. Its output duration is the overlap d: at output time t it
// renders A at its tail (durA-d+t) and B at its head (t), then asks the
// transition to blend them. It owns no per-frame state beyond a concurrency-safe
// scratch pool, so RenderInto is parallel-safe whenever both inputs are.
type transitionNode struct {
	a, b   video.VideoClip
	tr     transition.Transition
	maskTr transition.MaskTransition // non-nil when tr blends alpha

	d       clip.Time // overlap duration
	durA    clip.Time // A's full duration, for the tail offset
	size    clip.Size
	rate    clip.Rate
	hasRate bool
	hasMask bool
	access  video.AccessClass
	safe    bool
	curve   CrossfadeCurve

	start   clip.Time
	scratch *clip.FramePool
}

var _ video.VideoClip = (*transitionNode)(nil)

// NewTransition builds the overlap node for the handover from a to b over d
// using tr. a and b must share a size and each must be at least d long. curve
// selects the audio crossfade shape.
func NewTransition(a, b video.VideoClip, tr transition.Transition, d clip.Time, curve CrossfadeCurve) (video.VideoClip, error) {
	if a == nil || b == nil || tr == nil {
		return nil, clip.Wrap("transition", ErrNilTransition)
	}
	if a.Size() != b.Size() {
		return nil, clip.Wrap("transition", ErrTransitionSize)
	}
	durA, durB := a.Duration(), b.Duration()
	if d <= 0 || !clip.Finite(durA) || !clip.Finite(durB) || d > durA || d > durB {
		return nil, clip.Wrap("transition", ErrTransitionOverlap)
	}
	rate, hasRate := maxRate([]video.VideoClip{a, b})
	maskTr, _ := tr.(transition.MaskTransition)
	access := a.SourceAccess()
	if ba := b.SourceAccess(); ba > access {
		access = ba
	}
	return &transitionNode{
		a:       a,
		b:       b,
		tr:      tr,
		maskTr:  maskTr,
		d:       d,
		durA:    durA,
		size:    a.Size(),
		rate:    rate,
		hasRate: hasRate,
		// The output is transparent only when an input is: a MaskTransition merely
		// *can* blend alpha, it does not introduce it. Reporting a mask for an
		// opaque crossfade would force every plain mp4 export down the alpha path.
		hasMask: a.HasMask() || b.HasMask(),
		access:  access,
		safe:    a.ParallelSafe() && b.ParallelSafe(),
		curve:   curve,
		scratch: clip.NewFramePool(),
	}, nil
}

// progress maps output time t to normalized transition progress in [0,1].
func (n *transitionNode) progress(t clip.Time) float64 {
	p := float64(t) / float64(n.d)
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

// aTime is the source time on A (its tail) for output time t.
func (n *transitionNode) aTime(t clip.Time) clip.Time { return n.durA - n.d + t }

// Timeline metadata. The node's media duration is the overlap; only placement
// (start) is mutable.

func (n *transitionNode) Start() clip.Time    { return n.start }
func (n *transitionNode) Duration() clip.Time { return n.d }
func (n *transitionNode) End() clip.Time      { return n.start + n.d }

func (n *transitionNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	return &c
}

func (n *transitionNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.d = d
	return &c
}

func (n *transitionNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.d = end - c.start
	} else {
		c.start = end - c.d
	}
	return &c
}

// Video metadata.

func (n *transitionNode) Size() clip.Size                 { return n.size }
func (n *transitionNode) Rate() (clip.Rate, bool)         { return n.rate, n.hasRate }
func (n *transitionNode) HasMask() bool                   { return n.hasMask }
func (n *transitionNode) ParallelSafe() bool              { return n.safe }
func (n *transitionNode) SourceAccess() video.AccessClass { return n.access }

// Children exposes both inputs for planner graph traversal. Because a clip's
// straight-through body and its transition both reach the same source, an
// assembled timeline over shared sources reports multiplicity >= 2, so the
// planner renders it on the sequential engine: one forward decoder cannot serve
// a body and a transition's disjoint windows of the same source per frame.
func (n *transitionNode) Children() []video.VideoClip { return []video.VideoClip{n.a, n.b} }

// Audio mixes A's tail (faded out) with B's head (faded in) over the overlap,
// using the configured crossfade curve. The result lives on the local [0,d)
// timeline; the assembling chain places it at the right offset.
func (n *transitionNode) Audio() audio.AudioClip {
	aa, ba := n.a.Audio(), n.b.Audio()
	var aTail, bHead audio.AudioClip
	if aa != nil {
		aTail = audio.SubclipDur(aa, n.durA-n.d, n.d)
	}
	if ba != nil {
		bHead = audio.SubclipDur(ba, 0, n.d)
	}
	switch n.curve {
	case CrossfadeNone:
		if aTail != nil {
			return aTail
		}
		return bHead
	case CrossfadeLinear:
		if aTail != nil {
			aTail = audiofx.FadeOut(aTail, n.d)
		}
		if bHead != nil {
			bHead = audiofx.FadeIn(bHead, n.d)
		}
	default: // CrossfadeEqualPower
		if aTail != nil {
			aTail = audiofx.EqualPowerFadeOut(aTail, n.d)
		}
		if bHead != nil {
			bHead = audiofx.EqualPowerFadeIn(bHead, n.d)
		}
	}
	switch {
	case aTail != nil && bHead != nil:
		return audio.Mix(aTail, bHead)
	case aTail != nil:
		return aTail
	default:
		return bHead
	}
}

// RenderInto fills rgbDst (and alphaDst when the node carries a mask) by
// rendering both inputs into scratch and blending through the transition.
func (n *transitionNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	p := n.progress(t)
	w, h := n.size.W, n.size.H

	aRGB := n.scratch.Get(w, h, clip.RGB24)
	defer aRGB.Release()
	bRGB := n.scratch.Get(w, h, clip.RGB24)
	defer bRGB.Release()

	if !n.hasMask || alphaDst == nil {
		if err := n.a.FrameInto(ctx, n.aTime(t), aRGB); err != nil {
			return false, err
		}
		if err := n.b.FrameInto(ctx, t, bRGB); err != nil {
			return false, err
		}
		n.tr.Frame(p, rgbDst, aRGB, bRGB)
		return false, nil
	}

	aA := n.scratch.Get(w, h, clip.Gray8)
	defer aA.Release()
	bA := n.scratch.Get(w, h, clip.Gray8)
	defer bA.Release()
	if ok, err := n.a.RenderInto(ctx, n.aTime(t), aRGB, aA); err != nil {
		return false, err
	} else if !ok {
		fillOpaque(aA)
	}
	if ok, err := n.b.RenderInto(ctx, t, bRGB, bA); err != nil {
		return false, err
	} else if !ok {
		fillOpaque(bA)
	}
	n.tr.Frame(p, rgbDst, aRGB, bRGB)
	if n.maskTr != nil {
		n.maskTr.FrameMask(p, alphaDst, aA, bA)
	} else {
		// A plain transition does not describe its own alpha, so the sidecar
		// follows a straight crossfade — a sensible default for transparent inputs.
		blend.LerpRow(alphaDst.Pix, aA.Pix, bA.Pix, w*h, byte(p*255+0.5))
	}
	return true, nil
}

func (n *transitionNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	_, err := n.RenderInto(ctx, t, dst, nil)
	return err
}

func (n *transitionNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	if !n.hasMask {
		return false, nil
	}
	rgb := n.scratch.Get(n.size.W, n.size.H, clip.RGB24)
	defer rgb.Release()
	return n.RenderInto(ctx, t, rgb, dst)
}

// Close releases nothing: a transition never owns the clips it was given; the
// handle that opened each source closes it.
func (n *transitionNode) Close() error { return nil }

// TransitionSpec describes the transition on one boundary of a Sequence: the
// transition to run, the overlap duration, and the audio crossfade curve.
type TransitionSpec struct {
	T     transition.Transition
	Dur   clip.Time
	Curve CrossfadeCurve
}

// active reports whether the spec produces a real transition node (a nil spec,
// nil transition, or non-positive duration is a hard cut).
func (s *TransitionSpec) active() bool {
	return s != nil && s.T != nil && s.Dur > 0
}

// Sequence stitches clips into one timeline, joining clip i to clip i+1 with
// specs[i] (len(specs) must be len(clips)-1; a nil or zero-duration entry is a
// hard cut). Each interior clip's body is trimmed to make room for the overlaps
// on both sides, and the bodies and overlap nodes are concatenated into one
// same-size ConcatChain.
//
// It validates that every clip has a known duration and that the overlaps a clip
// feeds (one for an endpoint, two for an interior clip) fit within its duration.
// A clip wholly consumed by its overlaps contributes no body, just the overlap
// node(s) — e.g. a crossfade as long as two equal clips becomes a single node.
func Sequence(clips []video.VideoClip, specs []*TransitionSpec) (video.VideoClip, error) {
	if len(clips) == 0 {
		return nil, clip.Wrap("sequence", ErrEmptyConcat)
	}
	if len(clips) == 1 {
		return clips[0], nil
	}
	if len(specs) != len(clips)-1 {
		return nil, clip.Wrap("sequence", ErrTransitionOverlap)
	}

	n := len(clips)
	heads := make([]clip.Time, n) // overlap consumed from each clip's head
	tails := make([]clip.Time, n) // overlap consumed from each clip's tail
	for i, s := range specs {
		if s.active() {
			tails[i] = s.Dur
			heads[i+1] = s.Dur
		}
	}

	segments := make([]video.VideoClip, 0, 2*n-1)
	for i, c := range clips {
		d := c.Duration()
		if !clip.Finite(d) {
			return nil, clip.Wrap("sequence", ErrNoDuration)
		}
		if heads[i]+tails[i] > d {
			return nil, clip.Wrap("sequence", ErrTransitionOverlap)
		}
		// The body is the span left after both overlaps. When the overlaps consume
		// the whole clip (head+tail == d) the body is empty and is dropped — passing
		// an end of 0 to Subclip would instead hit its "to the end" sentinel and
		// wrongly replay the full clip.
		if bodyStart, bodyEnd := heads[i], d-tails[i]; bodyEnd > bodyStart {
			segments = append(segments, video.Subclip(c, bodyStart, bodyEnd))
		}
		if i < n-1 && specs[i].active() {
			tn, err := NewTransition(c, clips[i+1], specs[i].T, specs[i].Dur, specs[i].Curve)
			if err != nil {
				return nil, err
			}
			segments = append(segments, tn)
		}
	}
	return ConcatChain(segments)
}
