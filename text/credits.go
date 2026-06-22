package text

import (
	"context"
	"errors"
	"image/color"
	"math"
	"strings"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

var _ video.VideoClip = (*CreditsNode)(nil)

// ErrNoCredits reports a credits clip built without a positive viewport size or
// with no lines.
var ErrNoCredits = errors.New("credits: need a positive Size and at least one line")

const (
	defaultCreditsFontSize = 32
	defaultCreditsSpeed    = 60 // pixels per second
)

// CreditsOptions configures a scrolling credits roll. Size is the (required)
// transparent viewport the credits scroll through. Speed is the upward scroll
// rate in pixels per second. Zero fields take sensible defaults (white 32px
// centered text, 60 px/s).
//
// Note: because AlignLeft is the zero value of Align, it is indistinguishable
// from "unset" and is silently promoted to AlignCenter by the defaults pass.
// Credits always center by convention; AlignCenter and AlignRight are the only
// effective choices.
type CreditsOptions struct {
	Size        clip.Size
	FontPath    string
	FontSize    float64
	Color       color.Color
	Stroke      color.Color
	StrokeWidth float64
	Align       Align
	Speed       float64
	LineSpacing float64
}

// CreditsNode is a transparent, viewport-sized clip that scrolls a tall text
// block upward from below the frame to above it. The full block is rasterized
// once at construction; each frame is a vertical-offset blit, so the node is
// AccessStatic and parallel-safe. Its duration is the time for the block to
// travel from just below the viewport to fully above it.
type CreditsNode struct {
	roll    *clip.Frame // RGBA, the full stacked text block, immutable
	size    clip.Size   // viewport
	originX int         // horizontal placement of the block (centered)
	speed   float64     // pixels per second

	start  clip.Time
	dur    clip.Time
	hasDur bool
}

// NewCredits stacks lines into one centered text block and wraps it as a
// scrolling credits clip over a transparent viewport of opts.Size.
func NewCredits(lines []string, opts CreditsOptions) (*CreditsNode, error) {
	if opts.Size.W <= 0 || opts.Size.H <= 0 || len(lines) == 0 {
		return nil, clip.Wrap("credits", ErrNoCredits)
	}
	opts = withCreditsDefaults(opts)
	roll, err := Render(Options{
		Text:        strings.Join(lines, "\n"),
		FontPath:    opts.FontPath,
		FontSize:    opts.FontSize,
		Color:       opts.Color,
		Stroke:      opts.Stroke,
		StrokeWidth: opts.StrokeWidth,
		Align:       opts.Align,
		LineSpacing: opts.LineSpacing,
	})
	if err != nil {
		return nil, err
	}
	if roll.Format != clip.RGBA {
		roll = toRGBAOpaque(roll)
	}
	travel := float64(opts.Size.H + roll.H) // viewport entry to full exit
	dur := clip.Time(travel / opts.Speed * float64(clip.Time(1e9)))
	return &CreditsNode{
		roll:    roll,
		size:    opts.Size,
		originX: (opts.Size.W - roll.W) / 2,
		speed:   opts.Speed,
		dur:     dur,
		hasDur:  true,
	}, nil
}

func withCreditsDefaults(o CreditsOptions) CreditsOptions {
	if o.FontSize <= 0 {
		o.FontSize = defaultCreditsFontSize
	}
	if o.Color == nil {
		o.Color = color.White
	}
	if o.Speed <= 0 {
		o.Speed = defaultCreditsSpeed
	}
	// Credits read centered by convention; an unset Align (AlignLeft) would
	// left-flush ragged lines within the centered block.
	if o.Align == AlignLeft {
		o.Align = AlignCenter
	}
	return o
}

// Timeline metadata.

func (n *CreditsNode) Start() clip.Time    { return n.start }
func (n *CreditsNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }
func (n *CreditsNode) End() clip.Time      { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *CreditsNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	return &c
}

func (n *CreditsNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	return &c
}

func (n *CreditsNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	return &c
}

// Video metadata. A credits roll is always transparent.

func (n *CreditsNode) Size() clip.Size                 { return n.size }
func (n *CreditsNode) Rate() (clip.Rate, bool)         { return clip.Rate{}, false }
func (n *CreditsNode) HasMask() bool                   { return true }
func (n *CreditsNode) Audio() audio.AudioClip          { return nil }
func (n *CreditsNode) ParallelSafe() bool              { return true }
func (n *CreditsNode) SourceAccess() video.AccessClass { return video.AccessStatic }

// RenderInto clears the viewport and blits the text block at its scrolled
// vertical offset for time t: it enters at the bottom edge and exits past the
// top as t advances.
func (n *CreditsNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	clear(rgbDst.Pix)
	if alphaDst != nil {
		clear(alphaDst.Pix)
	}
	sec := float64(t) / float64(clip.Time(1e9))
	oy := int(math.Round(float64(n.size.H) - n.speed*sec))
	blitText(rgbDst, alphaDst, n.roll, n.originX, oy, 1)
	return true, nil
}

func (n *CreditsNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	_, err := n.RenderInto(ctx, t, dst, nil)
	return err
}

func (n *CreditsNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	rgbScratch := clip.NewFrame(n.size.W, n.size.H, clip.RGB24)
	return n.RenderInto(ctx, t, rgbScratch, dst)
}

// Close releases nothing: a credits clip owns only decoded buffers.
func (n *CreditsNode) Close() error { return nil }
