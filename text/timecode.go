package text

import (
	"context"
	"fmt"
	"image/color"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/cache"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

var _ video.VideoClip = (*TimecodeNode)(nil)

// TimecodeFormat selects how a running timecode is rendered.
type TimecodeFormat int

const (
	// TCClock is HH:MM:SS.
	TCClock TimecodeFormat = iota
	// TCMillis is HH:MM:SS.mmm.
	TCMillis
	// TCFrames is HH:MM:SS:FF, the frame index within the second; it needs a
	// Rate and falls back to TCMillis when none is set.
	TCFrames
)

const defaultTimecodeFontSize = 24

// TimecodeOptions configures a running-timecode raster. The clip counts up from
// Start; FF formatting uses Rate. Zero fields take sensible defaults (white 24px
// text, 1px black outline).
type TimecodeOptions struct {
	Start       clip.Time
	Format      TimecodeFormat
	Rate        clip.Rate
	FontPath    string
	FontSize    float64
	Color       color.Color
	Stroke      color.Color
	StrokeWidth float64
}

// TimecodeNode is a transparent, fixed-size clip showing the timecode at the
// current time. The canvas is sized once to the widest the chosen format can
// get (all-8 digits), and each frame's string is rendered right-aligned into it
// and cached, so the clip footprint never changes as the digits do. It is
// AccessStatic and parallel-safe.
type TimecodeNode struct {
	opts   TimecodeOptions
	size   clip.Size
	cache  *cache.LRU[string, *clip.Frame]
	render func(Options) (*clip.Frame, error)

	start  clip.Time
	dur    clip.Time
	hasDur bool
}

// NewTimecode builds a running-timecode clip. Its duration is unset; place it
// over a video (it adopts the video's length) or set one with WithDuration.
func NewTimecode(opts TimecodeOptions) (*TimecodeNode, error) {
	opts = withTimecodeDefaults(opts)
	n := &TimecodeNode{
		opts:   opts,
		cache:  cache.NewLRU[string, *clip.Frame](256),
		render: Render,
	}
	// Size the canvas to the widest the format can render.
	widest, err := n.raster(widestTimecode(opts.Format))
	if err != nil {
		return nil, err
	}
	n.size = clip.Size{W: widest.W, H: widest.H}
	return n, nil
}

func withTimecodeDefaults(o TimecodeOptions) TimecodeOptions {
	if o.FontSize <= 0 {
		o.FontSize = defaultTimecodeFontSize
	}
	if o.Color == nil {
		o.Color = color.White
	}
	if o.Stroke == nil {
		o.Stroke = color.Black
	}
	if o.StrokeWidth == 0 {
		o.StrokeWidth = 1
	}
	return o
}

// raster renders one timecode string to an RGBA label frame.
func (n *TimecodeNode) raster(s string) (*clip.Frame, error) {
	if f, ok := n.cache.Get(s); ok {
		return f, nil
	}
	f, err := n.render(Options{
		Text:        s,
		FontPath:    n.opts.FontPath,
		FontSize:    n.opts.FontSize,
		Color:       n.opts.Color,
		Stroke:      n.opts.Stroke,
		StrokeWidth: n.opts.StrokeWidth,
	})
	if err != nil {
		return nil, err
	}
	if f.Format != clip.RGBA {
		f = toRGBAOpaque(f)
	}
	n.cache.Put(s, f)
	return f, nil
}

// Timeline metadata.

func (n *TimecodeNode) Start() clip.Time    { return n.start }
func (n *TimecodeNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }
func (n *TimecodeNode) End() clip.Time      { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *TimecodeNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	return &c
}

func (n *TimecodeNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	return &c
}

func (n *TimecodeNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	return &c
}

// Video metadata. A timecode clip is always transparent.

func (n *TimecodeNode) Size() clip.Size                 { return n.size }
func (n *TimecodeNode) Rate() (clip.Rate, bool)         { return clip.Rate{}, false }
func (n *TimecodeNode) HasMask() bool                   { return true }
func (n *TimecodeNode) Audio() audio.AudioClip          { return nil }
func (n *TimecodeNode) ParallelSafe() bool              { return true }
func (n *TimecodeNode) SourceAccess() video.AccessClass { return video.AccessStatic }

// RenderInto clears the canvas and blits the timecode at Start+t, right-aligned
// so the seconds stay put as the leading digits change width.
func (n *TimecodeNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	clear(rgbDst.Pix)
	if alphaDst != nil {
		clear(alphaDst.Pix)
	}
	f, err := n.raster(formatTimecode(n.opts.Start+t, n.opts.Format, n.opts.Rate))
	if err != nil {
		return false, err
	}
	blitText(rgbDst, alphaDst, f, n.size.W-f.W, 0, 1)
	return true, nil
}

func (n *TimecodeNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	_, err := n.RenderInto(ctx, t, dst, nil)
	return err
}

func (n *TimecodeNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	rgbScratch := clip.NewFrame(n.size.W, n.size.H, clip.RGB24)
	return n.RenderInto(ctx, t, rgbScratch, dst)
}

// Close releases nothing: a timecode clip owns only decoded buffers.
func (n *TimecodeNode) Close() error { return nil }

// formatTimecode renders d as a timecode string in the chosen format. TCFrames
// falls back to TCMillis when rate is unset.
func formatTimecode(d clip.Time, f TimecodeFormat, rate clip.Rate) string {
	if d < 0 {
		d = 0
	}
	totalMs := int64(d) / int64(clip.Time(1e6))
	h := totalMs / 3600000
	m := (totalMs / 60000) % 60
	s := (totalMs / 1000) % 60
	ms := totalMs % 1000
	switch f {
	case TCMillis:
		return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
	case TCFrames:
		if rate.Num > 0 && rate.Den > 0 {
			fps := float64(rate.Num) / float64(rate.Den)
			ff := int(float64(ms) / 1000 * fps)
			return fmt.Sprintf("%02d:%02d:%02d:%02d", h, m, s, ff)
		}
		return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
	default: // TCClock
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
}

// widestTimecode returns the widest string the format can produce (all-8
// digits), used to fix the canvas size. TCFrames shares the millis template so
// the canvas still fits when it falls back to millis for a missing rate.
func widestTimecode(f TimecodeFormat) string {
	if f == TCClock {
		return "88:88:88"
	}
	return "88:88:88.888"
}
