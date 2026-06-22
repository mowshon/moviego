package mgo

import (
	"image/color"

	"github.com/mowshon/moviego/v2/text"
)

// TextAnim configures an entrance animation for AnimatedText. See text.TextAnim;
// set Type to one of the Anim* constants, or Custom for a hand-written curve.
type TextAnim = text.TextAnim

// AnimState is the per-instant transform a custom text animation produces
// (reveal, opacity, offset, scale). See text.AnimState.
type AnimState = text.AnimState

// AnimKind selects a built-in text entrance animation.
type AnimKind = text.AnimKind

// Built-in text animations, re-exported so callers need only import the facade.
const (
	AnimNone       = text.AnimNone
	AnimFadeIn     = text.AnimFadeIn
	AnimTypewriter = text.AnimTypewriter
	AnimSlideUp    = text.AnimSlideUp
	AnimSlideDown  = text.AnimSlideDown
	AnimSlideLeft  = text.AnimSlideLeft
	AnimSlideRight = text.AnimSlideRight
	AnimPop        = text.AnimPop
)

// AnimatedText renders content as a transparent clip that animates in over
// anim.Dur, then holds. Composite it over a video and place it with Position.
// The clip's duration defaults to the animation length; extend it with
// WithDuration to hold the text longer:
//
//	title, _ := mgo.AnimatedText("Hello", opts, mgo.TextAnim{
//		Type: mgo.AnimTypewriter, Dur: 2 * time.Second, Easing: mgo.EaseOut,
//	})
//	title = title.WithDuration(5 * time.Second).Position(mgo.Center)
func AnimatedText(content string, opts TextOptions, anim TextAnim) (*Video, error) {
	opts.Text = content
	n, err := text.NewAnimated(opts, anim)
	if err != nil {
		return nil, err
	}
	return &Video{inner: n}, nil
}

// CreditsOptions configures a scrolling credits roll. See text.CreditsOptions.
type CreditsOptions = text.CreditsOptions

// Credits builds a transparent clip that scrolls lines upward through a
// viewport of opts.Size, the classic end-roll. Its duration is the time for the
// block to travel fully through the frame. Composite it over a background.
func Credits(lines []string, opts CreditsOptions) (*Video, error) {
	n, err := text.NewCredits(lines, opts)
	if err != nil {
		return nil, err
	}
	return &Video{inner: n}, nil
}

// SubtitlesASS loads an .ass/.ssa file and builds a subtitles clip over
// opts.Size. The common subset is supported (dialogue events with override
// tags stripped); styling and positioning tags are ignored.
func SubtitlesASS(path string, opts SubtitleOptions) (*Video, error) {
	n, err := text.NewSubtitlesFromASS(path, opts)
	if err != nil {
		return nil, err
	}
	return &Video{inner: n}, nil
}

// SplitAtCues slices v into one clip per cue, each trimmed to that cue's
// [Start, End) interval — the inverse of burning captions in, useful for
// exporting a clip per caption. The returned clips share v's source and do not
// own it, so close v when done. A build error on v is propagated to a single
// returned clip.
func SplitAtCues(v *Video, cues []Cue) []*Video {
	if v.err != nil {
		return []*Video{v}
	}
	out := make([]*Video, len(cues))
	for i, c := range cues {
		out[i] = v.Subclip(c.Start, c.End)
	}
	return out
}

// TimecodeFormat selects how BurnTimecode renders the running time.
type TimecodeFormat = text.TimecodeFormat

// Timecode formats, re-exported from the text package.
const (
	TCClock  = text.TCClock
	TCMillis = text.TCMillis
	TCFrames = text.TCFrames
)

// defaultOverlayMargin insets a default-placed timecode or watermark from the
// frame edges.
const defaultOverlayMargin = 24

// overlayLayer puts burned-in overlays (timecode, watermark) above ordinary
// composite layers.
const overlayLayer = 1_000_000

// TimecodeOptions configures a burned-in running timecode. Position places the
// readout within the frame; a zero Position defaults to the bottom-right corner
// inset by Margin. Rate for the frame-count format (TCFrames) is taken from the
// underlying video automatically.
type TimecodeOptions struct {
	Start       Time
	Format      TimecodeFormat
	FontPath    string
	FontSize    float64
	Color       color.Color
	Stroke      color.Color
	StrokeWidth float64
	Position    Position
	Margin      int
}

// BurnTimecode overlays a running timecode counting up from opts.Start onto the
// clip, the common dailies/review burn-in. The readout is composited over the
// video at opts.Position (bottom-right by default).
func (v *Video) BurnTimecode(opts TimecodeOptions) *Video {
	if v.err != nil {
		return v
	}
	rate, _ := v.Rate()
	tn, err := text.NewTimecode(text.TimecodeOptions{
		Start:       opts.Start,
		Format:      opts.Format,
		Rate:        rate,
		FontPath:    opts.FontPath,
		FontSize:    opts.FontSize,
		Color:       opts.Color,
		Stroke:      opts.Stroke,
		StrokeWidth: opts.StrokeWidth,
	})
	if err != nil {
		c := v.derive(v.inner)
		c.err = err
		return c
	}
	tc := (&Video{inner: tn}).Position(opts.place(v.Size(), tn.Size())).Layer(overlayLayer)
	if d := v.Duration(); Finite(d) {
		tc = tc.WithDuration(d)
	}
	return Composite(v, tc)
}

// place resolves the timecode placement: an explicit Position when set,
// otherwise the bottom-right corner inset by Margin.
func (o TimecodeOptions) place(canvas, tc Size) Position {
	p := o.Position
	unset := p.Animated == nil && p.Keyword == 0 && p.X == 0 && p.Y == 0 && !p.RelX && !p.RelY
	if !unset {
		return p
	}
	m := o.Margin
	if m == 0 {
		m = defaultOverlayMargin
	}
	return At(canvas.W-tc.W-m, canvas.H-tc.H-m)
}

// CornerPos names a frame corner for watermark placement.
type CornerPos int

const (
	// TopLeft anchors to the top-left corner.
	TopLeft CornerPos = iota
	// TopRight anchors to the top-right corner.
	TopRight
	// BottomLeft anchors to the bottom-left corner.
	BottomLeft
	// BottomRight anchors to the bottom-right corner.
	BottomRight
)

// WatermarkOption tunes Watermark placement. Compose them: Corner picks the
// anchor, WithMargin sets the inset.
type WatermarkOption func(*watermarkConfig)

type watermarkConfig struct {
	corner CornerPos
	margin int
}

// Corner selects which frame corner the watermark anchors to.
func Corner(c CornerPos) WatermarkOption {
	return func(w *watermarkConfig) { w.corner = c }
}

// WithMargin sets the watermark's inset from the frame edges in pixels.
func WithMargin(px int) WatermarkOption {
	return func(w *watermarkConfig) { w.margin = px }
}

// Watermark composites logo onto the clip at a corner (bottom-right by
// default), inset by the margin. A logo with no duration of its own (a still
// image or canvas) is held for the whole clip:
//
//	out := v.Watermark(logo, mgo.Corner(mgo.BottomRight), mgo.WithMargin(20))
func (v *Video) Watermark(logo *Video, opts ...WatermarkOption) *Video {
	if v.err != nil {
		return v
	}
	if logo == nil {
		return v
	}
	if logo.err != nil {
		c := v.derive(v.inner)
		c.err = logo.err
		return c
	}
	cfg := watermarkConfig{corner: BottomRight, margin: defaultOverlayMargin}
	for _, o := range opts {
		o(&cfg)
	}
	wm := logo.Position(cornerPosition(v.Size(), logo.Size(), cfg.corner, cfg.margin)).Layer(overlayLayer)
	if d := v.Duration(); Finite(d) && !Finite(logo.Duration()) {
		wm = wm.WithDuration(d)
	}
	return Composite(v, wm)
}

// cornerPosition is the absolute top-left placing a logo of size item into the
// chosen corner of canvas, inset by margin.
func cornerPosition(canvas, item Size, corner CornerPos, margin int) Position {
	right := canvas.W - item.W - margin
	bottom := canvas.H - item.H - margin
	switch corner {
	case TopLeft:
		return At(margin, margin)
	case TopRight:
		return At(right, margin)
	case BottomLeft:
		return At(margin, bottom)
	default: // BottomRight
		return At(right, bottom)
	}
}
