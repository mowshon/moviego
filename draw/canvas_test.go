package draw_test

import (
	"image/color"
	"testing"

	"golang.org/x/image/vector"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/draw"
)

// at returns the RGBA bytes of the pixel at (x, y) in an RGBA frame.
func at(f *clip.Frame, x, y int) (r, g, b, a byte) {
	o := y*f.Stride + x*4
	return f.Pix[o], f.Pix[o+1], f.Pix[o+2], f.Pix[o+3]
}

func TestEmptyCanvasIsTransparent(t *testing.T) {
	f := draw.New(8, 8).Render()
	if f.Format != clip.RGBA {
		t.Fatalf("format = %v, want rgba", f.Format)
	}
	if _, _, _, a := at(f, 4, 4); a != 0 {
		t.Errorf("empty canvas pixel alpha = %d, want 0", a)
	}
}

func TestOpaqueBackgroundDropsAlpha(t *testing.T) {
	f := draw.New(8, 8).Background(color.RGBA{R: 10, G: 20, B: 30, A: 255}).Render()
	if f.Format != clip.RGB24 {
		t.Fatalf("format = %v, want rgb24 for opaque background", f.Format)
	}
}

func TestFilledRectCoversItsInterior(t *testing.T) {
	f := draw.New(20, 20).
		Rect(5, 5, 10, 10, draw.Paint{Fill: color.RGBA{R: 255, A: 255}}).
		Render()
	// Center of the rect is opaque red.
	r, _, _, a := at(f, 10, 10)
	if a < 250 || r < 250 {
		t.Errorf("rect center = (r=%d a=%d), want opaque red", r, a)
	}
	// A corner outside the rect stays transparent.
	if _, _, _, a := at(f, 1, 1); a != 0 {
		t.Errorf("outside rect alpha = %d, want 0", a)
	}
}

func TestRectOutlineLeavesHollowCenter(t *testing.T) {
	f := draw.New(40, 40).
		Rect(5, 5, 30, 30, draw.Paint{Stroke: color.White, StrokeWidth: 3}).
		Render()
	// The border is painted...
	if _, _, _, a := at(f, 5, 20); a < 200 {
		t.Errorf("outline edge alpha = %d, want painted", a)
	}
	// ...but the interior is hollow.
	if _, _, _, a := at(f, 20, 20); a != 0 {
		t.Errorf("outline interior alpha = %d, want hollow (0)", a)
	}
}

func TestCircleIsRoundNotSquare(t *testing.T) {
	f := draw.New(40, 40).
		Circle(20, 20, 15, draw.Paint{Fill: color.White}).
		Render()
	if _, _, _, a := at(f, 20, 20); a < 250 {
		t.Errorf("circle center alpha = %d, want opaque", a)
	}
	// The bounding-box corner lies outside the circle.
	if _, _, _, a := at(f, 6, 6); a != 0 {
		t.Errorf("circle corner alpha = %d, want 0 (round, not square)", a)
	}
}

func TestLineDrawsAlongSegment(t *testing.T) {
	f := draw.New(40, 40).
		Line(5, 20, 35, 20, color.White, 4).
		Render()
	if _, _, _, a := at(f, 20, 20); a < 200 {
		t.Errorf("line midpoint alpha = %d, want painted", a)
	}
	if _, _, _, a := at(f, 20, 35); a != 0 {
		t.Errorf("off-line pixel alpha = %d, want 0", a)
	}
}

// TestCustomShape exercises the Shape extension point with a user-defined
// primitive drawn through Canvas.Draw.
func TestCustomShape(t *testing.T) {
	f := draw.New(20, 20).Draw(fullBleed{}, draw.Paint{Fill: color.White}).Render()
	if _, _, _, a := at(f, 0, 0); a < 250 {
		t.Errorf("custom shape did not fill the corner: alpha = %d", a)
	}
}

// fullBleed is a Shape that fills the whole 20×20 canvas, standing in for a
// third-party primitive added through Canvas.Draw.
type fullBleed struct{}

func (fullBleed) Fill(rz *vector.Rasterizer) {
	rz.MoveTo(0, 0)
	rz.LineTo(20, 0)
	rz.LineTo(20, 20)
	rz.LineTo(0, 20)
	rz.ClosePath()
}

func (fullBleed) Outline(*vector.Rasterizer, float64) {}
