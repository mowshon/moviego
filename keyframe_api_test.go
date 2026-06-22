package mgo_test

import (
	"errors"
	"testing"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/render"
	"github.com/mowshon/moviego/v2/video"
)

func sampleClip(d mgo.Time) *mgo.Video {
	return mgo.Color(64, 48, [3]byte{10, 20, 30}).WithDuration(d)
}

func describe(t *testing.T, v *mgo.Video) mgo.Report {
	t.Helper()
	rep, err := mgo.Describe(v, mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	return rep
}

func TestAnimateOpacityAddsMask(t *testing.T) {
	v := sampleClip(time.Second).Animate(mgo.PropOpacity, []mgo.Keyframe{
		{At: 0, Val: 0}, {At: time.Second, Val: 1},
	}, mgo.EaseInOut)
	if v.Err() != nil {
		t.Fatalf("unexpected error: %v", v.Err())
	}
	if rep := describe(t, v); !rep.Transparent {
		t.Error("opacity-animated clip should report transparent (carries a mask)")
	}
}

func TestAnimateEmptyKeyframesErrors(t *testing.T) {
	v := sampleClip(time.Second).Animate(mgo.PropOpacity, nil, mgo.Linear)
	if v.Err() == nil {
		t.Fatal("empty keyframes should record a build error")
	}
}

func TestAnimatePositionEmptyErrors(t *testing.T) {
	// An empty key list is a build error, symmetric with scalar Animate.
	v := sampleClip(time.Second).AnimatePosition(nil, mgo.Linear)
	if !errors.Is(v.Err(), effect.ErrNoKeyframes) {
		t.Errorf("err = %v, want ErrNoKeyframes", v.Err())
	}
}

func TestAnimateScalePreservesSizeAndDuration(t *testing.T) {
	v := sampleClip(2*time.Second).Animate(mgo.PropScale, []mgo.Keyframe{
		{At: 0, Val: 1}, {At: 2 * time.Second, Val: 1.5},
	}, mgo.EaseSmooth)
	if v.Err() != nil {
		t.Fatalf("unexpected error: %v", v.Err())
	}
	if s := v.Size(); s.W != 64 || s.H != 48 {
		t.Errorf("size = %v, want 64x48", s)
	}
	if d := v.Duration(); d != 2*time.Second {
		t.Errorf("duration = %v, want 2s", d)
	}
}

func TestTimeRemapEnginePerAccess(t *testing.T) {
	base := sampleClip(5 * time.Second)
	forward := base.TimeRemap([]mgo.TimePoint{
		{Out: 0, In: 0},
		{Out: 2 * time.Second, In: time.Second},
		{Out: 3 * time.Second, In: 5 * time.Second},
	}, mgo.EaseSmooth)
	if rep := describe(t, forward); rep.Class != render.ClassLinear {
		t.Errorf("forward remap class = %v, want Linear", rep.Class)
	}
	reverse := base.TimeRemap([]mgo.TimePoint{
		{Out: 0, In: 0},
		{Out: time.Second, In: 2 * time.Second},
		{Out: 2 * time.Second, In: time.Second},
	}, mgo.Linear)
	rep := describe(t, reverse)
	if rep.Class != render.ClassRandom {
		t.Errorf("reverse remap class = %v, want Random", rep.Class)
	}
	if rep.Engine != mgo.EngineSequential {
		t.Errorf("reverse remap engine = %v, want Sequential", rep.Engine)
	}
}

func TestTimeRemapValidationError(t *testing.T) {
	v := sampleClip(time.Second).TimeRemap([]mgo.TimePoint{{Out: 0, In: 0}}, nil)
	if !errors.Is(v.Err(), video.ErrTimeRemapPoints) {
		t.Errorf("err = %v, want ErrTimeRemapPoints", v.Err())
	}
}

func TestFreezeDurations(t *testing.T) {
	base := sampleClip(2 * time.Second)
	cases := []struct {
		name string
		v    *mgo.Video
		want mgo.Time
	}{
		{"Freeze", base.Freeze(time.Second, time.Second), 3 * time.Second},
		{"FreezeStart", base.FreezeStart(time.Second), 3 * time.Second},
		{"FreezeEnd", base.FreezeEnd(500 * time.Millisecond), 2500 * time.Millisecond},
	}
	for _, c := range cases {
		if c.v.Err() != nil {
			t.Fatalf("%s: %v", c.name, c.v.Err())
		}
		if d := c.v.Duration(); d != c.want {
			t.Errorf("%s duration = %v, want %v", c.name, d, c.want)
		}
	}
}

func TestFreezeInfiniteDurationRejected(t *testing.T) {
	v := mgo.Color(64, 48, [3]byte{}).Freeze(time.Second, time.Second) // no WithDuration
	if !errors.Is(v.Err(), clip.ErrNoDuration) {
		t.Errorf("err = %v, want ErrNoDuration", v.Err())
	}
}

func TestAnimatePositionNoErrorAndPlaysStandalone(t *testing.T) {
	v := sampleClip(time.Second).AnimatePosition([]mgo.PositionKeyframe{
		{At: 0, X: 0, Y: 0},
		{At: time.Second, X: 100, Y: 50},
	}, mgo.Linear)
	if v.Err() != nil {
		t.Fatalf("unexpected error: %v", v.Err())
	}
	// Placement is a no-op for a standalone export; duration is unchanged.
	if d := v.Duration(); d != time.Second {
		t.Errorf("duration = %v, want 1s", d)
	}
}
