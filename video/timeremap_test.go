package video_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ease"
	"github.com/mowshon/moviego/v2/video"
)

// mappedTime renders one frame at output time t and returns the source time the
// inner clip was asked for.
func mappedTime(t *testing.T, c video.VideoClip, src *fakeClip, at clip.Time) clip.Time {
	t.Helper()
	dst := clip.NewFrame(2, 2, clip.RGB24)
	if err := c.FrameInto(context.Background(), at, dst); err != nil {
		t.Fatalf("frame at %v: %v", at, err)
	}
	return src.lastT
}

func TestTimeRemapMappingAndDuration(t *testing.T) {
	src := &fakeClip{rate: clip.Rate{Num: 30, Den: 1}, dur: 5 * time.Second}
	rm, err := video.TimeRemap(src, []video.TimePoint{
		{Out: 0, In: 0},
		{Out: 2 * time.Second, In: time.Second}, // 2x slow-mo
		{Out: 3 * time.Second, In: 5 * time.Second},
	}, ease.Linear)
	if err != nil {
		t.Fatalf("TimeRemap: %v", err)
	}
	if d := rm.Duration(); d != 3*time.Second {
		t.Errorf("duration = %v, want 3s", d)
	}
	// Output 1s sits halfway through the first segment → source 0.5s.
	if got := mappedTime(t, rm, src, time.Second); got != 500*time.Millisecond {
		t.Errorf("map(1s) = %v, want 500ms", got)
	}
	// Output 2.5s is halfway through the second segment (In 1s→5s) → 3s.
	if got := mappedTime(t, rm, src, 2500*time.Millisecond); got != 3*time.Second {
		t.Errorf("map(2.5s) = %v, want 3s", got)
	}
}

func TestTimeRemapAccessClass(t *testing.T) {
	src := &fakeClip{rate: clip.Rate{Num: 30, Den: 1}, dur: 5 * time.Second}
	forward, _ := video.TimeRemap(src, []video.TimePoint{
		{Out: 0, In: 0},
		{Out: 2 * time.Second, In: time.Second},
		{Out: 3 * time.Second, In: 5 * time.Second}, // still increasing
	}, nil)
	if forward.SourceAccess() != video.AccessLinear {
		t.Errorf("all-forward access = %v, want Linear", forward.SourceAccess())
	}
	rewind, _ := video.TimeRemap(src, []video.TimePoint{
		{Out: 0, In: 0},
		{Out: time.Second, In: 2 * time.Second},
		{Out: 2 * time.Second, In: time.Second}, // decreasing segment
	}, nil)
	if rewind.SourceAccess() != video.AccessRandom {
		t.Errorf("rewind access = %v, want Random", rewind.SourceAccess())
	}
}

func TestTimeRemapValidation(t *testing.T) {
	src := &fakeClip{rate: clip.Rate{Num: 30, Den: 1}, dur: 5 * time.Second}
	cases := [][]video.TimePoint{
		{{Out: 0, In: 0}}, // too few
		{{Out: time.Second, In: 0}, {Out: time.Second, In: 1}},  // non-increasing Out
		{{Out: -time.Second, In: 0}, {Out: time.Second, In: 1}}, // negative Out
	}
	for i, pts := range cases {
		if _, err := video.TimeRemap(src, pts, nil); !errors.Is(err, video.ErrTimeRemapPoints) {
			t.Errorf("case %d: err = %v, want ErrTimeRemapPoints", i, err)
		}
	}
}

func TestFreezeMappingAndDuration(t *testing.T) {
	src := &fakeClip{rate: clip.Rate{Num: 30, Den: 1}, dur: 3 * time.Second}
	fr, err := video.Freeze(src, time.Second, time.Second)
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if d := fr.Duration(); d != 4*time.Second {
		t.Errorf("duration = %v, want 4s", d)
	}
	if fr.SourceAccess() != video.AccessLinear {
		t.Errorf("access = %v, want Linear", fr.SourceAccess())
	}
	checks := []struct{ out, want clip.Time }{
		{500 * time.Millisecond, 500 * time.Millisecond},   // before the hold
		{1500 * time.Millisecond, time.Second},             // during the hold
		{2500 * time.Millisecond, 1500 * time.Millisecond}, // after, shifted back by hold
	}
	for _, c := range checks {
		if got := mappedTime(t, fr, src, c.out); got != c.want {
			t.Errorf("map(%v) = %v, want %v", c.out, got, c.want)
		}
	}
}

func TestFreezeStartAndEnd(t *testing.T) {
	src := &fakeClip{rate: clip.Rate{Num: 30, Den: 1}, dur: 2 * time.Second}
	start, _ := video.FreezeStart(src, time.Second)
	if d := start.Duration(); d != 3*time.Second {
		t.Errorf("FreezeStart duration = %v, want 3s", d)
	}
	if got := mappedTime(t, start, src, 500*time.Millisecond); got != 0 {
		t.Errorf("FreezeStart map(0.5s) = %v, want 0 (held)", got)
	}
	if got := mappedTime(t, start, src, 1500*time.Millisecond); got != 500*time.Millisecond {
		t.Errorf("FreezeStart map(1.5s) = %v, want 500ms", got)
	}

	end, _ := video.FreezeEnd(src, time.Second)
	if d := end.Duration(); d != 3*time.Second {
		t.Errorf("FreezeEnd duration = %v, want 3s", d)
	}
	// During the tail hold the source time clamps near the end (within one tick).
	tick := (clip.Rate{Num: 30, Den: 1}).FrameTime(1)
	got := mappedTime(t, end, src, 2500*time.Millisecond)
	if got > 2*time.Second || got < 2*time.Second-tick {
		t.Errorf("FreezeEnd hold map = %v, want just inside 2s", got)
	}
}

func TestFreezeNeedsDuration(t *testing.T) {
	src := &fakeClip{rate: clip.Rate{Num: 30, Den: 1}, dur: clip.Infinite}
	if _, err := video.Freeze(src, time.Second, time.Second); !errors.Is(err, clip.ErrNoDuration) {
		t.Errorf("err = %v, want ErrNoDuration", err)
	}
}
