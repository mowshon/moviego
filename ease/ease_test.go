package ease_test

import (
	"math"
	"testing"

	"github.com/mowshon/moviego/v2/ease"
)

// TestEndpointsFixed pins f(0)=0 and f(1)=1 for every built-in so they compose
// without shifting an animation's start or end.
func TestEndpointsFixed(t *testing.T) {
	curves := map[string]ease.Func{
		"Linear":     ease.Linear,
		"EaseIn":     ease.EaseIn,
		"EaseOut":    ease.EaseOut,
		"EaseInOut":  ease.EaseInOut,
		"EaseSmooth": ease.EaseSmooth,
	}
	for name, f := range curves {
		if got := f(0); math.Abs(got) > 1e-9 {
			t.Errorf("%s(0) = %v, want 0", name, got)
		}
		if got := f(1); math.Abs(got-1) > 1e-9 {
			t.Errorf("%s(1) = %v, want 1", name, got)
		}
	}
}

// TestMidpoints checks each curve's defining shape at p=0.5.
func TestMidpoints(t *testing.T) {
	cases := []struct {
		name string
		f    ease.Func
		want float64
	}{
		{"Linear", ease.Linear, 0.5},
		{"EaseIn", ease.EaseIn, 0.25},
		{"EaseOut", ease.EaseOut, 0.75},
		{"EaseInOut", ease.EaseInOut, 0.5},
		{"EaseSmooth", ease.EaseSmooth, 0.5},
	}
	for _, c := range cases {
		if got := c.f(0.5); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s(0.5) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestMonotonic verifies the curves never run backward over [0,1].
func TestMonotonic(t *testing.T) {
	curves := map[string]ease.Func{
		"EaseIn": ease.EaseIn, "EaseOut": ease.EaseOut,
		"EaseInOut": ease.EaseInOut, "EaseSmooth": ease.EaseSmooth,
	}
	for name, f := range curves {
		prev := f(0)
		for i := 1; i <= 100; i++ {
			p := float64(i) / 100
			v := f(p)
			if v < prev-1e-9 {
				t.Errorf("%s decreased at p=%v: %v < %v", name, p, v, prev)
			}
			prev = v
		}
	}
}
