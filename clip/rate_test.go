package clip

import (
	"testing"
	"time"
)

func TestFrameTime(t *testing.T) {
	cases := []struct {
		rate Rate
		i    int
		want Time
	}{
		{Rate{30, 1}, 0, 0},
		{Rate{30, 1}, 30, time.Second},
		{Rate{25, 1}, 25, time.Second},
		{Rate{60, 1}, 90, 1500 * time.Millisecond},
		// 30000/1001 ~ 29.97 fps: frame 30 lands at 1.001s.
		{Rate{30000, 1001}, 30, 1001 * time.Millisecond},
	}
	for _, c := range cases {
		if got := c.rate.FrameTime(c.i); got != c.want {
			t.Errorf("FrameTime(%v, %d) = %v, want %v", c.rate, c.i, got, c.want)
		}
	}
}

func TestTimeToFrame(t *testing.T) {
	cases := []struct {
		rate Rate
		t    Time
		want int
	}{
		{Rate{30, 1}, 0, 0},
		{Rate{30, 1}, time.Second, 30},
		// 2.999999s @ 30 maps to frame 89 (matches int(30*2.999999 + 1e-5)).
		{Rate{30, 1}, 2999999 * time.Microsecond, 89},
		// Exact 3s @ 30 maps to frame 90.
		{Rate{30, 1}, 3 * time.Second, 90},
		{Rate{30000, 1001}, 1001 * time.Millisecond, 30},
		// Negative timestamps truncate toward zero like int(), not floor:
		// int(30*-0.001 + 1e-5) = int(-0.02999) = 0.
		{Rate{30, 1}, -1 * time.Millisecond, 0},
		// int(30*-0.04 + 1e-5) = int(-1.19999) = -1.
		{Rate{30, 1}, -40 * time.Millisecond, -1},
	}
	for _, c := range cases {
		if got := c.rate.TimeToFrame(c.t); got != c.want {
			t.Errorf("TimeToFrame(%v, %v) = %d, want %d", c.rate, c.t, got, c.want)
		}
	}
}

func TestFrameTimeLargeIndex(t *testing.T) {
	// Beyond the int64 fast path (i*Den > ~9.2e9): big.Int must keep it exact.
	r := Rate{Num: 30000, Den: 1001}
	for _, i := range []int{20_000_000, 50_000_000} {
		ft := r.FrameTime(i)
		if got := r.TimeToFrame(ft); got != i {
			t.Errorf("FrameTime/TimeToFrame round-trip at i=%d: got %d", i, got)
		}
	}
}

func TestSnapNTSC(t *testing.T) {
	cases := []struct {
		fps  float64
		want Rate
	}{
		{23.976, Rate{24000, 1001}},
		{29.97, Rate{30000, 1001}},
		{49.95, Rate{50000, 1001}},
		// 59.94 ~ 60*coef, but 60 is not in MoviePy's snap list, so no snap.
		{59.94, RateFromFloat(59.94)},
		{30.0, Rate{30, 1}},
		{25.0, Rate{25, 1}},
		{24.0, Rate{24, 1}},
	}
	for _, c := range cases {
		got := SnapNTSC(c.fps)
		if got != c.want {
			t.Errorf("SnapNTSC(%v) = %v, want %v", c.fps, got, c.want)
		}
	}
}

func TestMaxRate(t *testing.T) {
	cases := []struct {
		a, b, want Rate
	}{
		{Rate{30, 1}, Rate{25, 1}, Rate{30, 1}},
		{Rate{24, 1}, Rate{30000, 1001}, Rate{30000, 1001}},
		{Rate{30, 1}, Rate{30, 1}, Rate{30, 1}}, // tie keeps a
		{Rate{30000, 1001}, Rate{30, 1}, Rate{30, 1}},
	}
	for _, c := range cases {
		if got := MaxRate(c.a, c.b); got != c.want {
			t.Errorf("MaxRate(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func FuzzTimeToFrame(f *testing.F) {
	rates := []Rate{{24, 1}, {25, 1}, {30, 1}, {60, 1}, {30000, 1001}, {24000, 1001}}
	f.Add(0, 0)
	f.Add(5, 12345)
	f.Add(2, 1_000_000)
	f.Fuzz(func(t *testing.T, rateIdx, frame int) {
		r := rates[((rateIdx%len(rates))+len(rates))%len(rates)]
		if frame < 0 {
			frame = -frame
		}
		frame %= 5_000_000 // keep FrameTime within int64 range

		// Round-trip: the index of frame i's exact timestamp is i.
		ft := r.FrameTime(frame)
		got := r.TimeToFrame(ft)
		if got != frame {
			t.Fatalf("round-trip %v frame %d: FrameTime=%v TimeToFrame=%d", r, frame, ft, got)
		}

		// Monotonicity: an earlier frame's timestamp never maps later.
		if frame > 0 {
			prev := r.TimeToFrame(r.FrameTime(frame - 1))
			if prev > got {
				t.Fatalf("non-monotonic at %v frame %d: prev=%d cur=%d", r, frame, prev, got)
			}
		}
	})
}
