package text

import (
	"context"
	"errors"
	"math"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/cache"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ease"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
)

var _ video.VideoClip = (*AnimatedTextNode)(nil)

// ErrNoAnimText reports an animated-text clip built with empty text.
var ErrNoAnimText = errors.New("text: animated text needs non-empty content")

// AnimKind selects a built-in text entrance animation. Set TextAnim.Custom for
// anything outside this set.
type AnimKind int

const (
	// AnimNone shows the text fully from the first frame (no animation).
	AnimNone AnimKind = iota
	// AnimFadeIn ramps opacity 0→1.
	AnimFadeIn
	// AnimTypewriter reveals the text one character at a time, left to right.
	AnimTypewriter
	// AnimSlideUp slides the text up into place from below.
	AnimSlideUp
	// AnimSlideDown slides the text down into place from above.
	AnimSlideDown
	// AnimSlideLeft slides the text left into place from the right.
	AnimSlideLeft
	// AnimSlideRight slides the text right into place from the left.
	AnimSlideRight
	// AnimPop scales the text up from nothing to full size while fading in.
	AnimPop
)

// AnimState is the per-instant transform applied to the text raster: how much
// of the text is revealed, its opacity, its pixel offset from the resting
// position, and its scale. It is what a TextAnim resolves to at a given eased
// progress, and the unit a custom animation produces.
type AnimState struct {
	Reveal float64 // fraction of characters shown, 0..1 (typewriter); 1 shows all
	Alpha  float64 // opacity multiplier, 0..1
	Dx, Dy float64 // offset from the resting position, in pixels
	Scale  float64 // 1 is natural size; about the canvas center
}

// TextAnim configures how an animated text clip enters over its first Dur of
// playback. Type picks a built-in; Easing shapes the progress (nil → Linear);
// Distance overrides a slide's travel (0 → the raster's own width/height). A
// slide adds that travel as headroom on the side it enters from so the text is
// never clipped mid-entrance; the resting text stays flush against the opposite
// edge, so a slide-up/left clip places exactly like a plain label.
// Custom, when non-nil, replaces Type entirely: it maps eased progress (0..1) to
// an AnimState, so any motion can be expressed without touching this package.
type TextAnim struct {
	Type     AnimKind
	Dur      clip.Time
	Easing   ease.Func
	Distance float64
	Custom   func(p float64) AnimState
}

// stateAt resolves the animation to an AnimState at eased progress p. text is
// the resting raster size, used to default a slide's distance to a full
// traversal of the text's own width or height.
func (a TextAnim) stateAt(p float64, text clip.Size) AnimState {
	if a.Custom != nil {
		return a.Custom(p)
	}
	rest := 1 - p // how far from the resting position the entrance still is
	dh := slideDistance(a.Distance, text.W)
	dv := slideDistance(a.Distance, text.H)
	switch a.Type {
	case AnimFadeIn:
		return AnimState{Reveal: 1, Alpha: p, Scale: 1}
	case AnimTypewriter:
		return AnimState{Reveal: p, Alpha: 1, Scale: 1}
	case AnimSlideUp:
		return AnimState{Reveal: 1, Alpha: 1, Dy: rest * dv, Scale: 1}
	case AnimSlideDown:
		return AnimState{Reveal: 1, Alpha: 1, Dy: -rest * dv, Scale: 1}
	case AnimSlideLeft:
		return AnimState{Reveal: 1, Alpha: 1, Dx: rest * dh, Scale: 1}
	case AnimSlideRight:
		return AnimState{Reveal: 1, Alpha: 1, Dx: -rest * dh, Scale: 1}
	case AnimPop:
		return AnimState{Reveal: 1, Alpha: p, Scale: p}
	default: // AnimNone
		return AnimState{Reveal: 1, Alpha: 1, Scale: 1}
	}
}

// AnimatedTextNode renders a single text raster animated over time onto a
// transparent canvas the size of the resting text. It pre-renders the full
// raster once; the typewriter path renders and caches one raster per revealed
// prefix in a bounded LRU. It is AccessStatic and parallel-safe: every frame is
// an independent function of time over immutable config and a mutex-guarded
// cache.
type AnimatedTextNode struct {
	opts         Options
	anim         TextAnim
	full         *clip.Frame // RGBA, the resting text, immutable
	runes        []rune
	textSize     clip.Size                    // the resting raster size; drives default slide travel
	canvas       clip.Size                    // output size: text plus slide headroom
	restX, restY int                          // top-left of the resting text within the canvas
	cache        *cache.LRU[int, *clip.Frame] // typewriter prefixes keyed by char count
	render       func(Options) (*clip.Frame, error)

	start  clip.Time
	dur    clip.Time
	hasDur bool
}

// NewAnimated rasterizes opts once and wraps it as an animated text clip driven
// by anim. The clip's default duration is anim.Dur (the entrance length);
// extend it with WithDuration to hold the text after it has arrived.
func NewAnimated(opts Options, anim TextAnim) (*AnimatedTextNode, error) {
	if opts.Text == "" {
		return nil, clip.Wrap("animated text", ErrNoAnimText)
	}
	full, err := Render(opts)
	if err != nil {
		return nil, err
	}
	if full.Format != clip.RGBA {
		full = toRGBAOpaque(full)
	}
	runes := []rune(opts.Text)
	textSize := clip.Size{W: full.W, H: full.H}
	canvas, rx, ry := animCanvas(anim, textSize)
	return &AnimatedTextNode{
		opts:     opts,
		anim:     anim,
		full:     full,
		runes:    runes,
		textSize: textSize,
		canvas:   canvas,
		restX:    rx,
		restY:    ry,
		cache:    cache.NewLRU[int, *clip.Frame](len(runes) + 1),
		render:   Render,
		dur:      anim.Dur,
		hasDur:   anim.Dur > 0,
	}, nil
}

// animCanvas sizes the output canvas and locates the resting text within it. A
// slide moves the text across the frame, so the canvas needs headroom on the
// side the text slides in from, or the moving text is clipped at the edge. The
// headroom is added only on that side, leaving the resting text flush against
// the opposite edge — so for the slide-up/left entrances the resting text's
// top-left stays at the canvas origin and placing the clip places the text
// exactly as a non-animated label would. Non-sliding entrances (fade, pop,
// typewriter) and custom curves render at the natural text size.
func animCanvas(a TextAnim, text clip.Size) (canvas clip.Size, restX, restY int) {
	canvas = text
	if a.Custom != nil {
		return canvas, 0, 0
	}
	switch a.Type {
	case AnimSlideUp: // enters from below: pad the bottom
		canvas.H += slidePad(a.Distance, text.H)
	case AnimSlideDown: // enters from above: pad the top
		restY = slidePad(a.Distance, text.H)
		canvas.H += restY
	case AnimSlideLeft: // enters from the right: pad the right
		canvas.W += slidePad(a.Distance, text.W)
	case AnimSlideRight: // enters from the left: pad the left
		restX = slidePad(a.Distance, text.W)
		canvas.W += restX
	}
	return canvas, restX, restY
}

// slidePad is the headroom a slide needs on its entry axis, rounded up so a
// fractional travel never clips the last pixel.
func slidePad(distance float64, fallback int) int {
	return int(math.Ceil(slideDistance(distance, fallback)))
}

// slideDistance resolves a slide's travel: the configured Distance, or a full
// traversal of the given text dimension when none is set.
func slideDistance(d float64, fallback int) float64 {
	if d > 0 {
		return d
	}
	return float64(fallback)
}

// progress maps local time to eased animation progress, clamped to [0,1]. A
// non-positive Dur makes the animation instantaneous (always fully arrived).
func (n *AnimatedTextNode) progress(t clip.Time) float64 {
	if n.anim.Dur <= 0 {
		return 1
	}
	p := float64(t) / float64(n.anim.Dur)
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	e := n.anim.Easing
	if e == nil {
		e = ease.Linear
	}
	return e(p)
}

// prefix renders (or returns the cached) raster for the first k characters,
// left-anchored for a stable typewriter reveal.
func (n *AnimatedTextNode) prefix(k int) (*clip.Frame, error) {
	if f, ok := n.cache.Get(k); ok {
		return f, nil
	}
	o := n.opts
	o.Text = string(n.runes[:k])
	f, err := n.render(o)
	if err != nil {
		return nil, err
	}
	if f.Format != clip.RGBA {
		f = toRGBAOpaque(f)
	}
	n.cache.Put(k, f)
	return f, nil
}

// Timeline metadata.

func (n *AnimatedTextNode) Start() clip.Time    { return n.start }
func (n *AnimatedTextNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }
func (n *AnimatedTextNode) End() clip.Time      { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *AnimatedTextNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	return &c
}

func (n *AnimatedTextNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	return &c
}

func (n *AnimatedTextNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	return &c
}

// Video metadata. An animated text clip is always transparent.

func (n *AnimatedTextNode) Size() clip.Size                 { return n.canvas }
func (n *AnimatedTextNode) Rate() (clip.Rate, bool)         { return clip.Rate{}, false }
func (n *AnimatedTextNode) HasMask() bool                   { return true }
func (n *AnimatedTextNode) Audio() audio.AudioClip          { return nil }
func (n *AnimatedTextNode) ParallelSafe() bool              { return true }
func (n *AnimatedTextNode) SourceAccess() video.AccessClass { return video.AccessStatic }

// RenderInto clears the canvas and blits the text in its state at time t: a
// revealed prefix (typewriter) anchored top-left, or the full raster scaled and
// offset about the center for every other animation.
func (n *AnimatedTextNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	clear(rgbDst.Pix)
	if alphaDst != nil {
		clear(alphaDst.Pix)
	}
	st := n.anim.stateAt(n.progress(t), n.textSize)
	if st.Alpha <= 0 {
		return true, nil
	}
	if st.Reveal < 1 {
		return true, n.revealInto(rgbDst, alphaDst, st)
	}
	return true, n.transformInto(rgbDst, alphaDst, st)
}

// revealInto draws the typewriter prefix anchored at the top-left.
func (n *AnimatedTextNode) revealInto(rgbDst, alphaDst *clip.Frame, st AnimState) error {
	k := int(math.Round(st.Reveal * float64(len(n.runes))))
	if k <= 0 {
		return nil
	}
	if k > len(n.runes) {
		k = len(n.runes)
	}
	f, err := n.prefix(k)
	if err != nil {
		return err
	}
	blitText(rgbDst, alphaDst, f, n.restX, n.restY, st.Alpha)
	return nil
}

// transformInto draws the full raster scaled about the center and offset by
// (Dx, Dy), faded by Alpha.
func (n *AnimatedTextNode) transformInto(rgbDst, alphaDst *clip.Frame, st AnimState) error {
	f := n.full
	if st.Scale > 0 && st.Scale != 1 {
		w := max1(int(math.Round(float64(f.W) * st.Scale)))
		h := max1(int(math.Round(float64(f.H) * st.Scale)))
		scaled, err := imagex.Resize(f, clip.Size{W: w, H: h})
		if err != nil {
			return err
		}
		f = scaled
	}
	// Center the (possibly scaled) raster in the resting text region, then apply
	// the slide offset; the region sits at (restX, restY) within the canvas.
	ox := n.restX + (n.textSize.W-f.W)/2 + int(math.Round(st.Dx))
	oy := n.restY + (n.textSize.H-f.H)/2 + int(math.Round(st.Dy))
	blitText(rgbDst, alphaDst, f, ox, oy, st.Alpha)
	return nil
}

func (n *AnimatedTextNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	_, err := n.RenderInto(ctx, t, dst, nil)
	return err
}

func (n *AnimatedTextNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	rgbScratch := clip.NewFrame(n.canvas.W, n.canvas.H, clip.RGB24)
	return n.RenderInto(ctx, t, rgbScratch, dst)
}

// Close releases nothing: an animated text clip owns only decoded buffers.
func (n *AnimatedTextNode) Close() error { return nil }

// blitText copies the RGBA src into an RGB24 destination and a Gray8 alpha
// destination at (ox, oy), scaling the source alpha by the opacity multiplier
// and clipping to the destination bounds.
func blitText(rgbDst, alphaDst, src *clip.Frame, ox, oy int, alpha float64) {
	mul := int(alpha*255 + 0.5)
	for sy := 0; sy < src.H; sy++ {
		dy := oy + sy
		if dy < 0 || dy >= rgbDst.H {
			continue
		}
		srow := src.Pix[sy*src.Stride:]
		drow := rgbDst.Pix[dy*rgbDst.Stride:]
		var arow []byte
		if alphaDst != nil {
			arow = alphaDst.Pix[dy*alphaDst.Stride:]
		}
		for sx := 0; sx < src.W; sx++ {
			dx := ox + sx
			if dx < 0 || dx >= rgbDst.W {
				continue
			}
			s := srow[sx*4:]
			if s[3] == 0 {
				continue
			}
			d := drow[dx*3:]
			d[0], d[1], d[2] = s[0], s[1], s[2]
			if arow != nil {
				arow[dx] = byte((int(s[3]) * mul) / 255)
			}
		}
	}
}
