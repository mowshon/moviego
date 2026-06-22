package keyframe_test

import (
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/ease"
	"github.com/mowshon/moviego/v2/keyframe"
)

func TestEmptyTrack(t *testing.T) {
	var zero keyframe.Track
	if !zero.Empty() {
		t.Fatal("zero Track should be Empty")
	}
	if got := zero.Eval(time.Second); got != 0 {
		t.Errorf("empty Eval = %v, want 0", got)
	}
}

func TestSingleKeyIsConstant(t *testing.T) {
	tr := keyframe.New([]keyframe.Key{{At: time.Second, Val: 0.7}}, nil)
	for _, at := range []time.Duration{0, time.Second, 5 * time.Second} {
		if got := tr.Eval(at); got != 0.7 {
			t.Errorf("Eval(%v) = %v, want 0.7", at, got)
		}
	}
}

func TestClampsToEndpoints(t *testing.T) {
	tr := keyframe.New([]keyframe.Key{
		{At: time.Second, Val: 0.2},
		{At: 3 * time.Second, Val: 0.8},
	}, nil)
	if got := tr.Eval(0); got != 0.2 {
		t.Errorf("before first = %v, want 0.2", got)
	}
	if got := tr.Eval(10 * time.Second); got != 0.8 {
		t.Errorf("after last = %v, want 0.8", got)
	}
}

func TestLinearInterpolation(t *testing.T) {
	tr := keyframe.New([]keyframe.Key{
		{At: 0, Val: 0},
		{At: 2 * time.Second, Val: 1},
	}, ease.Linear)
	if got := tr.Eval(time.Second); !approx(got, 0.5) {
		t.Errorf("midpoint = %v, want 0.5", got)
	}
}

func TestEasingShapesSegment(t *testing.T) {
	tr := keyframe.New([]keyframe.Key{
		{At: 0, Val: 0},
		{At: 4 * time.Second, Val: 1},
	}, ease.EaseIn) // p*p
	// At a quarter of the way, p=0.25, eased = 0.0625.
	if got := tr.Eval(time.Second); !approx(got, 0.0625) {
		t.Errorf("eased quarter = %v, want 0.0625", got)
	}
}

func TestNewSortsAndCopies(t *testing.T) {
	keys := []keyframe.Key{
		{At: 2 * time.Second, Val: 1},
		{At: 0, Val: 0},
	}
	tr := keyframe.New(keys, ease.Linear)
	if got := tr.Eval(time.Second); !approx(got, 0.5) {
		t.Errorf("after sort midpoint = %v, want 0.5", got)
	}
	// The caller's slice must be untouched.
	if keys[0].At != 2*time.Second {
		t.Error("New mutated the caller's slice")
	}
}

func approx(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}
