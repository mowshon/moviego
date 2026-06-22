package mgo_test

import (
	"testing"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/transition"
)

func colorClip(col [3]byte, d time.Duration) *mgo.Video {
	return mgo.Color(4, 4, col).WithDuration(d)
}

func TestSequenceBuilder(t *testing.T) {
	a := colorClip([3]byte{255, 0, 0}, 2*time.Second)
	b := colorClip([3]byte{0, 255, 0}, 2*time.Second)
	c := colorClip([3]byte{0, 0, 255}, 2*time.Second)

	v := mgo.NewSequence().
		Add(a).
		Then(mgo.Crossfade(500 * time.Millisecond)).
		Add(b).
		Then(mgo.WipeLeft(500 * time.Millisecond)).
		Add(c).
		Video()
	if v.Err() != nil {
		t.Fatalf("build error: %v", v.Err())
	}
	if d := v.Duration(); d != 5*time.Second {
		t.Errorf("duration = %v, want 5s", d)
	}
}

func TestConcatWithUniform(t *testing.T) {
	a := colorClip([3]byte{255, 0, 0}, time.Second)
	b := colorClip([3]byte{0, 0, 255}, time.Second)
	v := mgo.ConcatWith(mgo.ConcatOptions{
		Transition: transition.CrossFade{},
		Duration:   250 * time.Millisecond,
	}, a, b)
	if v.Err() != nil {
		t.Fatalf("build error: %v", v.Err())
	}
	if d := v.Duration(); d != 1750*time.Millisecond {
		t.Errorf("duration = %v, want 1.75s", d)
	}
}

func TestConcatWithNoTransitionIsPlainConcat(t *testing.T) {
	a := colorClip([3]byte{255, 0, 0}, time.Second)
	b := colorClip([3]byte{0, 0, 255}, time.Second)
	v := mgo.ConcatWith(mgo.ConcatOptions{}, a, b)
	if v.Err() != nil {
		t.Fatalf("build error: %v", v.Err())
	}
	if d := v.Duration(); d != 2*time.Second {
		t.Errorf("duration = %v, want 2s", d)
	}
}

func TestSequenceOverlapTooLongErrors(t *testing.T) {
	a := colorClip([3]byte{255, 0, 0}, time.Second)
	b := colorClip([3]byte{0, 0, 255}, time.Second)
	v := mgo.NewSequence().
		Add(a).
		Then(mgo.Crossfade(2 * time.Second)). // longer than either clip
		Add(b).
		Video()
	if v.Err() == nil {
		t.Error("expected an overlap-too-long error")
	}
}

func TestSequencePropagatesClipError(t *testing.T) {
	// FadeOut on a clip with no known duration records a build error that the
	// sequence must surface rather than swallow.
	bad := mgo.Color(4, 4, [3]byte{}).FadeOut(time.Second)
	good := colorClip([3]byte{0, 0, 255}, time.Second)
	v := mgo.NewSequence().Add(bad).Then(mgo.Crossfade(250 * time.Millisecond)).Add(good).Video()
	if v.Err() == nil {
		t.Error("expected the carried clip error to propagate")
	}
}

func TestCustomTransitionThroughFacade(t *testing.T) {
	// A user's inline transition needs no moviego change: drop it in via
	// UseTransition and the facade wires it into the timeline. (That it actually
	// renders is asserted in composite.TestSequenceCustomTransitionRenders, which
	// has direct frame access.)
	custom := transition.Func("custom", func(p float64, dst, a, b *clip.Frame) {
		copy(dst.Pix, a.Pix)
	})
	a := colorClip([3]byte{255, 0, 0}, time.Second)
	b := colorClip([3]byte{0, 0, 255}, time.Second)
	v := mgo.NewSequence().
		Add(a).
		Then(mgo.UseTransition(custom, 250*time.Millisecond)).
		Add(b).
		Video()
	if v.Err() != nil {
		t.Fatalf("build error: %v", v.Err())
	}
	if d := v.Duration(); d != 1750*time.Millisecond {
		t.Errorf("duration = %v, want 1.75s", d)
	}
	if _, err := mgo.Describe(v, mgo.ExportOptions{Rate: mgo.Rate{Num: 10, Den: 1}}); err != nil {
		t.Fatalf("describe: %v", err)
	}
}
