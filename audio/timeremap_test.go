package audio_test

import (
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// samples renders count single-channel sample frames starting at start.
func samples(t *testing.T, c audio.AudioClip, start clip.Time, count int) []float32 {
	t.Helper()
	buf := pull(t, c, start, count)
	out := make([]float32, count)
	copy(out, buf.Samples[:count])
	return out
}

func TestTimeRemapIdentity(t *testing.T) {
	sr := 8
	src := newFake(sr, 1, time.Second, func(i int) float32 { return float32(i) })
	// An identity map must reproduce the source sample-for-sample.
	rm := audio.TimeRemap(src, func(t clip.Time) clip.Time { return t }, time.Second)
	got := samples(t, rm, 0, 8)
	for i, v := range got {
		if v != float32(i) {
			t.Errorf("identity sample[%d] = %v, want %d", i, v, i)
		}
	}
}

func TestTimeRemapSlowMotion(t *testing.T) {
	sr := 8
	src := newFake(sr, 1, time.Second, func(i int) float32 { return float32(i) })
	// Half-speed: output time t reads source t/2, so each source sample is held
	// for two output frames (with linear interpolation between).
	rm := audio.TimeRemap(src, func(t clip.Time) clip.Time { return t / 2 }, 2*time.Second)
	got := samples(t, rm, 0, 8)
	want := []float32{0, 0.5, 1, 1.5, 2, 2.5, 3, 3.5}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("slow-mo sample[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestTimeRemapFreezeHolds(t *testing.T) {
	sr := 8
	src := newFake(sr, 1, time.Second, func(i int) float32 { return float32(i) })
	// A constant map sustains the source level at that instant.
	at := clip.SampleTime(3, sr)
	rm := audio.TimeRemap(src, func(clip.Time) clip.Time { return at }, time.Second)
	got := samples(t, rm, 0, 4)
	for i, v := range got {
		if v != 3 {
			t.Errorf("held sample[%d] = %v, want 3", i, v)
		}
	}
}

func TestTimeRemapSilentOutsideDuration(t *testing.T) {
	sr := 8
	src := newFake(sr, 1, time.Second, func(int) float32 { return 1 })
	rm := audio.TimeRemap(src, func(t clip.Time) clip.Time { return t }, 500*time.Millisecond)
	// Pull past the 0.5s duration: frames at/after durIdx must be silent.
	got := samples(t, rm, 0, 8)
	for i := 4; i < 8; i++ {
		if got[i] != 0 {
			t.Errorf("out-of-window sample[%d] = %v, want 0", i, got[i])
		}
	}
}
