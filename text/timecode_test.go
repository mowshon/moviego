package text

import (
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
)

func TestFormatTimecode(t *testing.T) {
	d := time.Hour + 23*time.Minute + 45*time.Second + 678*time.Millisecond
	cases := []struct {
		format TimecodeFormat
		rate   clip.Rate
		want   string
	}{
		{TCClock, clip.Rate{}, "01:23:45"},
		{TCMillis, clip.Rate{}, "01:23:45.678"},
		{TCFrames, clip.Rate{Num: 25, Den: 1}, "01:23:45:16"}, // 0.678s * 25 = 16.95 → 16
		{TCFrames, clip.Rate{}, "01:23:45.678"},               // no rate → millis fallback
	}
	for _, c := range cases {
		if got := formatTimecode(clip.Time(d), c.format, c.rate); got != c.want {
			t.Errorf("formatTimecode(%v) = %q, want %q", c.format, got, c.want)
		}
	}
}

func TestTimecodeNodeFixedSizeAndAdvance(t *testing.T) {
	n, err := NewTimecode(TimecodeOptions{Format: TCClock, FontSize: 24})
	if err != nil {
		t.Fatal(err)
	}
	if sz := n.Size(); sz.W <= 0 || sz.H <= 0 {
		t.Fatalf("size = %+v, want positive", sz)
	}
	// The canvas never changes size, but later times paint a different readout.
	zero := sumAlpha(t, n, 0)
	later := sumAlpha(t, n, 12*time.Second)
	if zero == 0 || later == 0 {
		t.Errorf("timecode rendered blank: zero=%d later=%d", zero, later)
	}
}
