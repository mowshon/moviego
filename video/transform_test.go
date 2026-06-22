package video_test

import (
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

func TestSubclipMetadata(t *testing.T) {
	rate := clip.Rate{Num: 30, Den: 1}
	tick := rate.FrameTime(1)
	cases := []struct {
		name    string
		srcDur  clip.Time
		a, b    clip.Time
		wantDur clip.Time
	}{
		{"window", 5 * time.Second, time.Second, 3 * time.Second, 2 * time.Second},
		{"negative end", 5 * time.Second, time.Second, -time.Second, 3 * time.Second},
		{"negative start", 5 * time.Second, -2 * time.Second, 0, 2 * time.Second},
		{"zero end is full", 5 * time.Second, 0, 0, 5 * time.Second},
		{"overshoot within tolerance", 5 * time.Second, 0, 5*time.Second + tick/2, 5 * time.Second},
		{"overshoot clamped", 5 * time.Second, 0, 6 * time.Second, 5 * time.Second},
		{"start past end clamps to empty", 5 * time.Second, 6 * time.Second, 7 * time.Second, 0},
		{"inverted window is empty", 5 * time.Second, 3 * time.Second, time.Second, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := &fakeClip{rate: rate, dur: c.srcDur}
			sub := video.Subclip(src, c.a, c.b)
			if d := sub.Duration(); d != c.wantDur {
				t.Errorf("duration = %v, want %v", d, c.wantDur)
			}
		})
	}
}

func TestSubclipTimeMapping(t *testing.T) {
	src := &fakeClip{rate: clip.Rate{Num: 30, Den: 1}, dur: 5 * time.Second}
	sub := video.Subclip(src, 2*time.Second, 4*time.Second)
	dst := clip.NewFrame(2, 2, clip.RGB24)
	if err := sub.FrameInto(context.Background(), 500*time.Millisecond, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}
	// Output t=0.5s maps to source 2s+0.5s.
	if want := 2500 * time.Millisecond; src.lastT != want {
		t.Errorf("mapped time = %v, want %v", src.lastT, want)
	}
}
