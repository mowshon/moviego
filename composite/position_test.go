package composite

import (
	"testing"

	"github.com/mowshon/moviego/v2/clip"
)

func TestComputePosition(t *testing.T) {
	canvas := clip.Size{W: 100, H: 80}
	child := clip.Size{W: 20, H: 10}
	cases := []struct {
		name  string
		pos   Position
		wantX int
		wantY int
	}{
		{"origin", Position{}, 0, 0},
		{"absolute", Position{X: 30, Y: 15}, 30, 15},
		{"center", Position{Keyword: PosCenter}, 40, 35},
		{"left", Position{Keyword: PosLeft}, 0, 35},
		{"right", Position{Keyword: PosRight}, 80, 35},
		{"top", Position{Keyword: PosTop}, 40, 0},
		{"bottom", Position{Keyword: PosBottom}, 40, 70},
		{"relative-half", Position{X: 0.5, RelX: true, Y: 0.5, RelY: true}, 40, 35},
		{"relative-end", Position{X: 1, RelX: true, Y: 1, RelY: true}, 80, 70},
		{"truncates", Position{X: 12.9, Y: 5.9}, 12, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x, y := ComputePosition(child, canvas, c.pos, 0)
			if x != c.wantX || y != c.wantY {
				t.Errorf("got (%d,%d), want (%d,%d)", x, y, c.wantX, c.wantY)
			}
		})
	}
}

func TestComputePositionAnimated(t *testing.T) {
	pos := Position{Animated: func(t clip.Time) (float64, float64) {
		return float64(t) / float64(clip.Time(1)), 0
	}}
	x, _ := ComputePosition(clip.Size{W: 10, H: 10}, clip.Size{W: 100, H: 100}, pos, 42)
	if x != 42 {
		t.Errorf("animated x = %d, want 42", x)
	}
}

func TestOverlap(t *testing.T) {
	canvas := clip.Size{W: 100, H: 100}
	child := clip.Size{W: 40, H: 30}
	cases := []struct {
		name     string
		px, py   int
		wantOK   bool
		dst, src Rect
	}{
		{"inside", 10, 20, true, Rect{10, 20, 40, 30}, Rect{0, 0, 40, 30}},
		{"clipped-top-left", -10, -5, true, Rect{0, 0, 30, 25}, Rect{10, 5, 30, 25}},
		{"clipped-bottom-right", 80, 90, true, Rect{80, 90, 20, 10}, Rect{0, 0, 20, 10}},
		{"fully-left", -40, 0, false, Rect{}, Rect{}},
		{"fully-below", 0, 100, false, Rect{}, Rect{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst, src, ok := Overlap(canvas, child, c.px, c.py)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if dst != c.dst || src != c.src {
				t.Errorf("dst=%+v src=%+v, want dst=%+v src=%+v", dst, src, c.dst, c.src)
			}
		})
	}
}
