package draw

import (
	"image"
	"image/color"

	"golang.org/x/image/vector"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/imagex"
)

// Paint describes how a shape is colored. A nil Fill skips the interior; a nil
// Stroke (or a non-positive StrokeWidth) skips the outline. Both may be set to
// fill a shape and draw a contrasting border in one operation.
type Paint struct {
	Fill        color.Color
	Stroke      color.Color
	StrokeWidth float64
}

// op is one drawing instruction: a shape and how to paint it. Ops are replayed
// in insertion order at Render time, so later shapes draw over earlier ones.
type op struct {
	shape Shape
	paint Paint
}

// Canvas accumulates vector drawing operations over a fixed-size transparent
// surface. Its drawing methods return the canvas so calls chain fluently; Render
// flattens the operations into a single frame. A zero background keeps the
// surface transparent (the rendered frame carries an alpha mask); set one with
// Background.
type Canvas struct {
	w, h int
	bg   color.Color // nil → transparent
	ops  []op
}

// New creates a w×h transparent canvas.
func New(w, h int) *Canvas {
	return &Canvas{w: w, h: h}
}

// Size reports the canvas dimensions.
func (c *Canvas) Size() clip.Size { return clip.Size{W: c.w, H: c.h} }

// Background sets a solid fill drawn behind every shape. An opaque background
// makes Render produce an opaque (mask-free) frame.
func (c *Canvas) Background(col color.Color) *Canvas {
	c.bg = col
	return c
}

// Draw appends an arbitrary Shape painted with p. It is the extension point for
// custom primitives; the named helpers below are thin wrappers over it.
func (c *Canvas) Draw(s Shape, p Paint) *Canvas {
	c.ops = append(c.ops, op{shape: s, paint: p})
	return c
}

// Rect draws an axis-aligned rectangle with top-left (x, y) and size (w, h).
func (c *Canvas) Rect(x, y, w, h int, p Paint) *Canvas {
	return c.Draw(Rect{X: float64(x), Y: float64(y), W: float64(w), H: float64(h)}, p)
}

// Circle draws a circle of radius r centered at (cx, cy).
func (c *Canvas) Circle(cx, cy, r int, p Paint) *Canvas {
	return c.Draw(Circle{CX: float64(cx), CY: float64(cy), R: float64(r)}, p)
}

// Ellipse draws an ellipse of radii (rx, ry) centered at (cx, cy).
func (c *Canvas) Ellipse(cx, cy, rx, ry int, p Paint) *Canvas {
	return c.Draw(Ellipse{CX: float64(cx), CY: float64(cy), RX: float64(rx), RY: float64(ry)}, p)
}

// Line draws a round-capped segment of the given width from (x1, y1) to
// (x2, y2) in col.
func (c *Canvas) Line(x1, y1, x2, y2 int, col color.Color, width float64) *Canvas {
	return c.Draw(
		Line{X1: float64(x1), Y1: float64(y1), X2: float64(x2), Y2: float64(y2)},
		Paint{Stroke: col, StrokeWidth: width},
	)
}

// Render rasterizes every operation into a packed frame: RGBA when the result
// has any transparency (the common transparent-background case), RGB24 when an
// opaque background makes every pixel solid.
func (c *Canvas) Render() *clip.Frame {
	img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	if c.bg != nil {
		fillUniform(img, c.bg)
	}
	for _, o := range c.ops {
		if o.paint.Fill != nil {
			rasterize(img, o.shape.Fill, o.paint.Fill)
		}
		if o.paint.Stroke != nil && o.paint.StrokeWidth > 0 {
			w := o.paint.StrokeWidth
			rasterize(img, func(rz *vector.Rasterizer) { o.shape.Outline(rz, w) }, o.paint.Stroke)
		}
	}
	return imagex.FromImage(img)
}

// rasterize fills the path traced by trace with a solid color, compositing over
// whatever is already in img. Each shape gets its own rasterizer so winding from
// one path never leaks into another.
func rasterize(img *image.RGBA, trace func(*vector.Rasterizer), col color.Color) {
	rz := vector.NewRasterizer(img.Bounds().Dx(), img.Bounds().Dy())
	trace(rz)
	rz.Draw(img, img.Bounds(), image.NewUniform(col), image.Point{})
}

// fillUniform paints the whole image with col (opaque source over).
func fillUniform(img *image.RGBA, col color.Color) {
	src := color.RGBAModel.Convert(col).(color.RGBA)
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = src.R, src.G, src.B, src.A
	}
}
