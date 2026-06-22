package text

import (
	"context"
	"errors"
	"image/color"
	"sort"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/cache"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// SubtitlesNode is a video.VideoClip.
var _ video.VideoClip = (*SubtitlesNode)(nil)

// ErrNoCanvas reports a subtitles clip built without a positive canvas size.
var ErrNoCanvas = errors.New("subtitles: Size must be positive")

const (
	defaultSubFontSize  = 24
	defaultSubStroke    = 1
	defaultSubCacheSize = 64
)

// SubtitleLayout selects how a cue is placed on the canvas.
type SubtitleLayout int

const (
	// LayoutCaption wraps each cue to the canvas width and anchors it to the
	// bottom margin — the classic burned-in caption.
	LayoutCaption SubtitleLayout = iota
	// LayoutWordCenter shows one token at a time, centered, timed by per-word
	// timing (Cue.Words). It is the social/vertical-video "word pops in the
	// center" style. A cue without Words falls back to its whole text, centered,
	// over the cue interval.
	LayoutWordCenter
)

// SubtitleOptions configures a SubtitlesNode. Size is the (required) transparent
// canvas the cues are drawn onto. In the default LayoutCaption the text is
// wrapped to the canvas width minus the horizontal margin and anchored to the
// bottom margin; in LayoutWordCenter each word is centered. Zero fields take
// sensible defaults (white text, 1px black outline, 24px font, margins of one
// twentieth of the canvas).
type SubtitleOptions struct {
	Size        clip.Size
	FontPath    string
	FontSize    float64
	Color       color.Color
	Stroke      color.Color
	StrokeWidth float64
	Align       Align
	Layout      SubtitleLayout
	VPos        float64 // vertical center as a fraction of height (0,1]; 0 → layout default
	MarginH     int
	MarginV     int
	CacheSize   int // bound on rendered-cue frames; <=0 → default
}

// unit is one timed renderable: a wrapped cue (caption) or a single word/cue
// (word-centered). Units are sorted by start and looked up by binary search.
type unit struct {
	start, end clip.Time
	text       string
}

// SubtitlesNode is a transparent, canvas-sized video clip that shows the active
// timed unit's rasterized text and is fully transparent otherwise. Rendered
// frames are cached in a bounded LRU (MoviePy re-renders and grows an unbounded
// dict instead); the active unit is found by binary search over unit starts. It
// is parallel-safe and AccessStatic: each frame is an independent lookup, and a
// cache miss renders into its own face and buffer with no shared mutable state.
type SubtitlesNode struct {
	units  []unit
	opts   SubtitleOptions
	cache  *cache.LRU[int, *clip.Frame]
	render func(Options) (*clip.Frame, error) // == Render; overridden in tests to count

	start  clip.Time
	dur    clip.Time
	hasDur bool
}

// NewSubtitles builds a subtitles clip from cues over a transparent canvas of
// opts.Size. Its duration is the latest unit end.
func NewSubtitles(cues []Cue, opts SubtitleOptions) (*SubtitlesNode, error) {
	if opts.Size.W <= 0 || opts.Size.H <= 0 {
		return nil, clip.Wrap("subtitles", ErrNoCanvas)
	}
	opts = withSubDefaults(opts)
	units := buildUnits(cues, opts.Layout)

	capSize := opts.CacheSize
	if capSize <= 0 {
		capSize = defaultSubCacheSize
	}
	var dur clip.Time
	for _, u := range units {
		if u.end > dur {
			dur = u.end
		}
	}
	return &SubtitlesNode{
		units:  units,
		opts:   opts,
		cache:  cache.NewLRU[int, *clip.Frame](capSize),
		render: Render,
		dur:    dur,
		hasDur: dur > 0,
	}, nil
}

// NewSubtitlesFromSRT and NewSubtitlesFromVTT are file-loading conveniences.
func NewSubtitlesFromSRT(path string, opts SubtitleOptions) (*SubtitlesNode, error) {
	cues, err := ParseSRTFile(path)
	if err != nil {
		return nil, err
	}
	return NewSubtitles(cues, opts)
}

func NewSubtitlesFromVTT(path string, opts SubtitleOptions) (*SubtitlesNode, error) {
	cues, err := ParseVTTFile(path)
	if err != nil {
		return nil, err
	}
	return NewSubtitles(cues, opts)
}

// NewSubtitlesFromJSON loads a JSON subtitle file (either the simple array form
// or the rich {styles,cues} form, including per-word timing) and applies any
// styles (hex color, font path) over opts that the caller left unset.
func NewSubtitlesFromJSON(path string, opts SubtitleOptions) (*SubtitlesNode, error) {
	styles, cues, err := ParseJSONStyledFile(path)
	if err != nil {
		return nil, err
	}
	return NewSubtitles(cues, applyStyles(opts, styles))
}

// buildUnits flattens cues into the renderable units for the layout, sorted by
// start. Caption uses one unit per cue; word-center uses one unit per word
// (falling back to the whole cue when a cue carries no word timing).
func buildUnits(cues []Cue, layout SubtitleLayout) []unit {
	var units []unit
	for _, c := range cues {
		if layout == LayoutWordCenter && len(c.Words) > 0 {
			for _, w := range c.Words {
				units = append(units, unit{start: w.Start, end: w.End, text: w.Text})
			}
			continue
		}
		units = append(units, unit{start: c.Start, end: c.End, text: c.Text})
	}
	sort.SliceStable(units, func(i, j int) bool { return units[i].start < units[j].start })
	return units
}

func withSubDefaults(o SubtitleOptions) SubtitleOptions {
	if o.FontSize <= 0 {
		o.FontSize = defaultSubFontSize
	}
	if o.Color == nil {
		o.Color = color.White
	}
	if o.Stroke == nil {
		o.Stroke = color.Black
	}
	if o.StrokeWidth == 0 {
		o.StrokeWidth = defaultSubStroke
	}
	if o.MarginH <= 0 {
		o.MarginH = o.Size.W / 20
	}
	if o.MarginV <= 0 {
		o.MarginV = o.Size.H / 20
	}
	return o
}

// Prerender renders every unit into the cache up front (sizing the cache to hold
// them all) so the first display of each unit does not stall the render loop.
// Must be called before any concurrent RenderInto calls; it is not goroutine-safe.
func (n *SubtitlesNode) Prerender() error {
	if len(n.units) > n.cache.Cap() {
		n.cache = cache.NewLRU[int, *clip.Frame](len(n.units))
	}
	for i := range n.units {
		if _, err := n.unitFrame(i); err != nil {
			return err
		}
	}
	return nil
}

// activeUnit returns the index of the unit playing at local time t, or -1. Among
// overlapping units it prefers the latest-starting one still within its end: it
// starts at the last unit whose start is <= t and scans backward to the first
// one that has not yet ended, so a short unit ending early does not hide an
// earlier, longer unit that is still active.
func (n *SubtitlesNode) activeUnit(t clip.Time) int {
	i := sort.Search(len(n.units), func(i int) bool { return n.units[i].start > t }) - 1
	for ; i >= 0; i-- {
		if t < n.units[i].end {
			return i
		}
	}
	return -1
}

// unitFrame renders (or returns the cached) RGBA frame for unit idx. Caption
// units wrap to the canvas width; word-centered units are label-sized to the
// token.
func (n *SubtitlesNode) unitFrame(idx int) (*clip.Frame, error) {
	if f, ok := n.cache.Get(idx); ok {
		return f, nil
	}
	o := Options{
		Text:        n.units[idx].text,
		FontPath:    n.opts.FontPath,
		FontSize:    n.opts.FontSize,
		Color:       n.opts.Color,
		Stroke:      n.opts.Stroke,
		StrokeWidth: n.opts.StrokeWidth,
		Align:       n.opts.Align,
	}
	if n.opts.Layout == LayoutCaption {
		o.Caption = true
		o.Size = clip.Size{W: n.opts.Size.W - 2*n.opts.MarginH}
	}
	f, err := n.render(o)
	if err != nil {
		return nil, err
	}
	if f.Format != clip.RGBA { // transparent BG always yields RGBA; guard anyway
		f = toRGBAOpaque(f)
	}
	n.cache.Put(idx, f)
	return f, nil
}

// Timeline metadata.

func (n *SubtitlesNode) Start() clip.Time    { return n.start }
func (n *SubtitlesNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }
func (n *SubtitlesNode) End() clip.Time      { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *SubtitlesNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	return &c
}

func (n *SubtitlesNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	return &c
}

func (n *SubtitlesNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	return &c
}

// Video metadata. A subtitles clip is always transparent (it carries a mask).

func (n *SubtitlesNode) Size() clip.Size                 { return n.opts.Size }
func (n *SubtitlesNode) Rate() (clip.Rate, bool)         { return clip.Rate{}, false }
func (n *SubtitlesNode) HasMask() bool                   { return true }
func (n *SubtitlesNode) Audio() audio.AudioClip          { return nil }
func (n *SubtitlesNode) ParallelSafe() bool              { return true }
func (n *SubtitlesNode) SourceAccess() video.AccessClass { return video.AccessStatic }

// RenderInto fills the transparent canvas: cleared to fully transparent, with
// the active unit's text blitted in. The text is centered horizontally and
// placed vertically per the layout (bottom margin for captions, centered for
// word-centered), clipped to the canvas.
func (n *SubtitlesNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	clear(rgbDst.Pix)
	if alphaDst != nil {
		clear(alphaDst.Pix)
	}
	idx := n.activeUnit(t)
	if idx < 0 {
		return true, nil
	}
	f, err := n.unitFrame(idx)
	if err != nil {
		return false, err
	}
	x := (n.opts.Size.W - f.W) / 2
	blitRGBA(rgbDst, alphaDst, f, x, n.originY(f.H))
	return true, nil
}

// originY is the top-left y for a frame of height frameH: a VPos-driven center
// when VPos is set, else the layout default (bottom margin for captions, canvas
// center for word-centered).
func (n *SubtitlesNode) originY(frameH int) int {
	if n.opts.VPos > 0 {
		return int(float64(n.opts.Size.H)*n.opts.VPos) - frameH/2
	}
	if n.opts.Layout == LayoutWordCenter {
		return (n.opts.Size.H - frameH) / 2
	}
	return n.opts.Size.H - n.opts.MarginV - frameH
}

func (n *SubtitlesNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	_, err := n.RenderInto(ctx, t, dst, nil)
	return err
}

func (n *SubtitlesNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	rgbScratch := clip.NewFrame(n.opts.Size.W, n.opts.Size.H, clip.RGB24)
	return n.RenderInto(ctx, t, rgbScratch, dst)
}

// Close releases nothing: a subtitles clip owns only decoded buffers.
func (n *SubtitlesNode) Close() error { return nil }

// blitRGBA copies the RGBA src into an RGB24 destination (rgbDst) and a Gray8
// alpha destination (alphaDst) at offset (ox, oy), clipped to the destination.
func blitRGBA(rgbDst, alphaDst, src *clip.Frame, ox, oy int) {
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
			d := drow[dx*3:]
			d[0], d[1], d[2] = s[0], s[1], s[2]
			if arow != nil {
				arow[dx] = s[3]
			}
		}
	}
}

// toRGBAOpaque promotes an RGB24 frame to RGBA with full opacity (a defensive
// fallback; transparent rasterization normally already yields RGBA).
func toRGBAOpaque(f *clip.Frame) *clip.Frame {
	out := clip.NewFrame(f.W, f.H, clip.RGBA)
	for y := 0; y < f.H; y++ {
		s := f.Pix[y*f.Stride:]
		d := out.Pix[y*out.Stride:]
		for x := 0; x < f.W; x++ {
			d[x*4], d[x*4+1], d[x*4+2], d[x*4+3] = s[x*3], s[x*3+1], s[x*3+2], 255
		}
	}
	return out
}
