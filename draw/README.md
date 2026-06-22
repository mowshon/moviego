# draw — vector drawing for MovieGo

`draw` rasterizes simple 2D vector shapes — rectangles, circles, ellipses, and
round-capped lines — into a `clip.Frame`. The facade wraps that frame in a
static clip, so you can draw lower thirds, callout shapes, progress bars, and
logos without an external image asset or a heavyweight 2D library.

It is a thin assembler over [`golang.org/x/image/vector`](https://pkg.go.dev/golang.org/x/image/vector),
the same antialiased fill rasterizer `x/image` already uses for glyphs. That is
MovieGo's *only* external dependency, so the drawing layer adds nothing new.

## Quick start (facade)

```go
import (
    "image/color"
    "time"

    mgo "github.com/mowshon/moviego/v2"
)

logo := mgo.NewCanvas(640, 360).
    Background(color.RGBA{R: 12, G: 14, B: 20, A: 255}).
    Circle(320, 180, 80, mgo.Paint{Fill: color.White, Stroke: color.Black, StrokeWidth: 4}).
    Rect(40, 40, 200, 60, mgo.Paint{Fill: color.RGBA{R: 255, G: 90, B: 0, A: 200}}).
    Line(0, 300, 640, 300, color.White, 6).
    WithDuration(3 * time.Second)
```

`NewCanvas(w, h)` starts a transparent surface. Each drawing method returns the
canvas, so calls chain. Finish with:

- `WithDuration(d) *Video` — rasterize and give the clip a duration, or
- `Image() *Video` — rasterize with no duration set (give it one later, or let a
  `Composite` adopt it).

A transparent canvas produces a clip with an alpha mask, so it composites
straight over video. Set `Background(col)` with an opaque color to get a solid
(mask-free) frame.

## The paint model

`Paint` says how a shape is colored:

```go
type Paint struct {
    Fill        color.Color // nil → no fill
    Stroke      color.Color // nil → no outline
    StrokeWidth float64     // outline width in pixels
}
```

A shape may be filled, outlined, or both. Strokes are not a separate engine: an
outline is the filled band between an outer edge and a reversed inner edge, so
the rasterizer only ever fills. (`Line` is stroke-only — it has no interior; its
width comes entirely from the stroke.)

## Built-in shapes

| Method | Shape |
| --- | --- |
| `Rect(x, y, w, h, paint)` | axis-aligned rectangle, top-left at `(x, y)` |
| `Circle(cx, cy, r, paint)` | circle centered at `(cx, cy)` |
| `Ellipse(cx, cy, rx, ry, paint)` | axis-aligned ellipse |
| `Line(x1, y1, x2, y2, col, width)` | round-capped segment |

Circles and ellipses are traced as four cubic Béziers, so they stay crisp at any
size instead of faceting.

## Extending: custom shapes

Every primitive is a `Shape`:

```go
type Shape interface {
    Fill(rz *vector.Rasterizer)              // append the solid interior
    Outline(rz *vector.Rasterizer, w float64) // append the stroke band
}
```

To add a primitive — a rounded rectangle, a star, an arrow — implement those two
methods and draw it through `Canvas.Draw(shape, paint)`. Nothing else changes;
the canvas, paint model, and frame plumbing are shape-agnostic.

```go
// Triangle is a custom filled primitive.
type Triangle struct{ Ax, Ay, Bx, By, Cx, Cy float64 }

func (t Triangle) Fill(rz *vector.Rasterizer) {
    rz.MoveTo(float32(t.Ax), float32(t.Ay))
    rz.LineTo(float32(t.Bx), float32(t.By))
    rz.LineTo(float32(t.Cx), float32(t.Cy))
    rz.ClosePath()
}

func (t Triangle) Outline(rz *vector.Rasterizer, w float64) { /* optional */ }

// Use it:
v := mgo.NewCanvas(200, 200).
    Draw(Triangle{100, 20, 180, 180, 20, 180}, mgo.Paint{Fill: color.White}).
    WithDuration(time.Second)
```

The same `Shape` lives at both layers: `draw.Shape` for package-level use and the
`mgo.Shape` alias for the facade.

## Notes

- **Coordinates** are pixels with the origin at the top-left, y growing downward.
- **Winding for holes**: outlines use the rasterizer's absolute-winding rule — an
  outer path plus a reversed inner path leaves a ring. Built-in outlines do this
  for you.
- **Antialiasing** is always on (coverage-based), matching glyph rendering.
- **Animation**: a canvas is a still. To move or fade a drawn graphic, composite
  the clip and drive it with the keyframe engine (`Animate`, `AnimatePosition`).
