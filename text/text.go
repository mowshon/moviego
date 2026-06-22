package text

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
)

// ErrCaptionSize reports a caption request without a positive width to wrap to.
var ErrCaptionSize = errors.New("text: caption mode needs a positive Size.W")

// ErrNoSize reports a label request with a non-positive font size (label mode
// cannot auto-size; only caption mode bisects).
var ErrNoSize = errors.New("text: label mode needs a positive FontSize")

// Align selects horizontal alignment of multi-line text.
type Align int

const (
	// AlignLeft left-aligns lines (default).
	AlignLeft Align = iota
	// AlignCenter centers lines.
	AlignCenter
	// AlignRight right-aligns lines.
	AlignRight
)

// Options configures a text rasterization.
//
// Two layout modes mirror MoviePy: label (Caption=false) sizes the image to the
// text at the given FontSize and only breaks on explicit newlines; caption
// (Caption=true) wraps the text to a fixed Size.W and, when FontSize is zero,
// bisects for the largest size that still fits Size.
type Options struct {
	Text     string
	FontPath string  // empty → embedded Go Regular
	FontSize float64 // pixels; 0 with Caption bisects to fit Size

	Color  color.Color // text fill; nil → opaque white
	BG     color.Color // background; nil → transparent (frame carries a mask)
	Stroke color.Color // outline; nil with StrokeWidth>0 → black

	StrokeWidth float64 // outline radius in pixels; 0 → no outline
	Caption     bool    // false=label (auto-size image), true=caption (fixed box + wrap)
	Size        clip.Size
	Align       Align
	LineSpacing float64 // multiplier on the font's line height; 0 → 1.0
}

// layout is the resolved geometry of a rasterization: the wrapped lines, the
// face that produced them, integer pixel metrics, and the frame dimensions.
// Vertical metrics follow MoviePy: a single line occupies ascent+descent, each
// additional line adds one lineGap, and the stroke pads the top and bottom.
type layout struct {
	lines   []string
	face    font.Face
	lineW   []int // pixel advance of each line
	ascent  int
	descent int
	lineGap int
	stroke  int
	width   int
	height  int
}

// Render rasterizes the options into a packed clip.Frame: RGBA (carrying alpha)
// when the background is transparent, RGB24 when an opaque background is set.
func Render(opts Options) (*clip.Frame, error) {
	lay, err := buildLayout(opts)
	if err != nil {
		return nil, err
	}
	return imagex.FromImage(paint(lay, opts)), nil
}

// New rasterizes the options once and wraps the result as a static image clip,
// splitting a transparent result's alpha into a mask sidecar. Set a duration on
// the returned node before export.
func New(opts Options) (*video.ImageNode, error) {
	f, err := Render(opts)
	if err != nil {
		return nil, err
	}
	if f.Format == clip.RGBA {
		rgb, alpha, err := imagex.SplitAlpha(f)
		if err != nil {
			return nil, err
		}
		return video.NewImage(rgb, alpha), nil
	}
	return video.NewImage(f, nil), nil
}

// buildLayout resolves the font, chooses the font size (bisecting in caption
// mode when none is given), wraps the text, and computes pixel geometry.
func buildLayout(opts Options) (layout, error) {
	fnt, err := resolveFont(opts.FontPath)
	if err != nil {
		return layout{}, err
	}
	stroke := pxCeil(fixed.Int26_6(opts.StrokeWidth * 64))

	if opts.Caption {
		if opts.Size.W <= 0 {
			return layout{}, ErrCaptionSize
		}
		size := opts.FontSize
		if size <= 0 {
			size = fitFontSize(fnt, opts, stroke)
		}
		return captionLayout(fnt, size, opts, stroke)
	}

	if opts.FontSize <= 0 {
		return layout{}, ErrNoSize
	}
	return labelLayout(fnt, opts.FontSize, opts, stroke)
}

// labelLayout breaks only on explicit newlines and sizes the frame to the text.
func labelLayout(fnt *Font, size float64, opts Options, stroke int) (layout, error) {
	face, err := fnt.faceAt(size)
	if err != nil {
		return layout{}, err
	}
	lay := measure(face, splitLines(opts.Text), stroke, opts.LineSpacing)
	maxW := 0
	for _, w := range lay.lineW {
		if w > maxW {
			maxW = w
		}
	}
	lay.width = max1(maxW + 2*stroke)
	lay.height = max1(lay.contentHeight())
	return lay, nil
}

// captionLayout wraps to Size.W and sizes the frame to Size (height auto when
// Size.H is zero).
func captionLayout(fnt *Font, size float64, opts Options, stroke int) (layout, error) {
	face, err := fnt.faceAt(size)
	if err != nil {
		return layout{}, err
	}
	lines := wrapText(face, opts.Text, opts.Size.W-2*stroke)
	lay := measure(face, lines, stroke, opts.LineSpacing)
	lay.width = opts.Size.W
	if opts.Size.H > 0 {
		lay.height = opts.Size.H
	} else {
		lay.height = max1(lay.contentHeight())
	}
	return lay, nil
}

// measure fills per-line widths and shared vertical metrics for a face and an
// already-wrapped set of lines.
func measure(face font.Face, lines []string, stroke int, spacing float64) layout {
	m := face.Metrics()
	gap := m.Height
	if spacing > 0 {
		gap = fixed.Int26_6(float64(m.Height) * spacing)
	}
	lay := layout{
		lines:   lines,
		face:    face,
		lineW:   make([]int, len(lines)),
		ascent:  pxCeil(m.Ascent),
		descent: pxCeil(m.Descent),
		lineGap: pxCeil(gap),
		stroke:  stroke,
	}
	for i, ln := range lines {
		lay.lineW[i] = pxCeil(font.MeasureString(face, ln))
	}
	return lay
}

// contentHeight is the MoviePy height: a single-line band (ascent+descent) plus
// one line gap per extra line, plus the stroke on top and bottom.
func (l layout) contentHeight() int {
	h := l.ascent + l.descent + 2*l.stroke
	if len(l.lines) > 1 {
		h += (len(l.lines) - 1) * l.lineGap
	}
	return h
}

// fitFontSize bisects for the largest integer pixel size whose wrapped text fits
// the caption box; it returns at least 1 so a frame is always produced.
func fitFontSize(fnt *Font, opts Options, stroke int) float64 {
	hi := opts.Size.H
	if hi <= 0 {
		hi = opts.Size.W // no height bound: cap by width so the search terminates
	}
	innerW := opts.Size.W - 2*stroke
	fits := func(size int) bool {
		face, err := fnt.faceAt(float64(size))
		if err != nil {
			return false
		}
		lay := measure(face, wrapText(face, opts.Text, innerW), stroke, opts.LineSpacing)
		for _, w := range lay.lineW {
			if w > innerW {
				return false // an unbreakable token overflows the width
			}
		}
		return opts.Size.H <= 0 || lay.contentHeight() <= opts.Size.H
	}
	best, lo := 1, 1
	for lo <= hi {
		mid := (lo + hi) / 2
		if fits(mid) {
			best, lo = mid, mid+1
		} else {
			hi = mid - 1
		}
	}
	return float64(best)
}

// paint draws the laid-out lines onto an RGBA canvas: an opaque background fill
// when BG is set, the stroke outline, then the fill text.
func paint(lay layout, opts Options) image.Image {
	canvas := image.NewRGBA(image.Rect(0, 0, lay.width, lay.height))
	if opts.BG != nil {
		draw.Draw(canvas, canvas.Bounds(), image.NewUniform(opts.BG), image.Point{}, draw.Src)
	}
	fill := opts.Color
	if fill == nil {
		fill = color.White
	}
	stroke := opts.Stroke
	if stroke == nil {
		stroke = color.Black
	}
	for i, ln := range lay.lines {
		x := lineOriginX(lay, i, opts.Align)
		y := lay.stroke + lay.ascent + i*lay.lineGap
		if lay.stroke > 0 {
			drawStroke(canvas, lay.face, ln, x, y, lay.stroke, stroke)
		}
		drawLine(canvas, lay.face, ln, x, y, fill)
	}
	return canvas
}

// lineOriginX is the pen x for line i under the alignment, in pixels.
func lineOriginX(lay layout, i int, align Align) int {
	switch align {
	case AlignCenter:
		return lay.stroke + (lay.width-2*lay.stroke-lay.lineW[i])/2
	case AlignRight:
		return lay.width - lay.stroke - lay.lineW[i]
	default:
		return lay.stroke
	}
}

// drawLine draws s once at (x, baseline) in col.
func drawLine(dst draw.Image, face font.Face, s string, x, baseline int, col color.Color) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(baseline)},
	}
	d.DrawString(s)
}

// drawStroke paints s in col at every offset within the stroke radius so the
// fill drawn on top sits inside an outline.
func drawStroke(dst draw.Image, face font.Face, s string, x, baseline, r int, col color.Color) {
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if (dx == 0 && dy == 0) || dx*dx+dy*dy > r*r {
				continue
			}
			drawLine(dst, face, s, x+dx, baseline+dy, col)
		}
	}
}

// splitLines splits on explicit newlines, normalizing CRLF.
func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// wrapText breaks text to fit maxWidth pixels: paragraphs split on newlines,
// then words on spaces, falling back to mid-word rune breaks for a token wider
// than the line (scripts without spaces).
func wrapText(face font.Face, text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return splitLines(text)
	}
	var out []string
	for _, para := range splitLines(text) {
		out = append(out, wrapParagraph(face, para, maxWidth)...)
	}
	return out
}

// wrapParagraph greedily packs words into lines no wider than maxWidth, breaking
// an over-wide word into rune-runs.
func wrapParagraph(face font.Face, para string, maxWidth int) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Split(para, " ") {
		cand := w
		if cur != "" {
			cand = cur + " " + w
		}
		if fitsWidth(face, cand, maxWidth) {
			cur = cand
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
			cur = ""
		}
		if fitsWidth(face, w, maxWidth) {
			cur = w
			continue
		}
		// w alone overflows: emit full pieces, keep the remainder as the line start.
		pieces := breakWord(face, w, maxWidth)
		for i, p := range pieces {
			if i == len(pieces)-1 {
				cur = p
			} else {
				lines = append(lines, p)
			}
		}
	}
	if cur != "" || len(lines) == 0 {
		lines = append(lines, cur)
	}
	return lines
}

// breakWord splits a single token into rune-runs that each fit maxWidth.
func breakWord(face font.Face, word string, maxWidth int) []string {
	var pieces []string
	cur := ""
	for _, r := range word {
		cand := cur + string(r)
		if cur != "" && !fitsWidth(face, cand, maxWidth) {
			pieces = append(pieces, cur)
			cur = string(r)
			continue
		}
		cur = cand
	}
	if cur != "" {
		pieces = append(pieces, cur)
	}
	return pieces
}

// fitsWidth reports whether s rasterizes within maxWidth pixels.
func fitsWidth(face font.Face, s string, maxWidth int) bool {
	return pxCeil(font.MeasureString(face, s)) <= maxWidth
}

// pxCeil converts a fixed-point length to integer pixels, rounding up so glyphs
// are never clipped.
func pxCeil(v fixed.Int26_6) int { return v.Ceil() }

// max1 clamps a dimension to at least one pixel so a frame is never zero-sized.
func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
