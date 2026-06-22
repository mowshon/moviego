package text

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/mowshon/moviego/v2/clip"
)

func newTestSubs(t *testing.T, opts SubtitleOptions, cues ...Cue) *SubtitlesNode {
	t.Helper()
	if opts.Size == (clip.Size{}) {
		opts.Size = clip.Size{W: 320, H: 240}
	}
	n, err := NewSubtitles(cues, opts)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSubtitlesRequiresSize(t *testing.T) {
	if _, err := NewSubtitles(nil, SubtitleOptions{}); err == nil {
		t.Fatal("subtitles without a canvas size must error")
	}
}

func TestActiveCueBoundaries(t *testing.T) {
	n := newTestSubs(t, SubtitleOptions{},
		Cue{Start: sec(1), End: sec(2), Text: "a"},
		Cue{Start: sec(3), End: sec(5), Text: "b"},
	)
	cases := []struct {
		t    clip.Time
		want int
	}{
		{sec(0.5), -1},
		{sec(1), 0},   // start is inclusive
		{sec(1.9), 0}, //
		{sec(2), -1},  // end is exclusive
		{sec(2.5), -1},
		{sec(3), 1},
		{sec(4.999), 1},
		{sec(5), -1},
	}
	for _, c := range cases {
		if got := n.activeUnit(c.t); got != c.want {
			t.Errorf("activeUnit(%v) = %d, want %d", c.t, got, c.want)
		}
	}
}

func TestActiveCueOverlappingPrefersLatestStillActive(t *testing.T) {
	// A long cue overlaps a short later one. After the short cue ends, the long
	// one must still show; while both play, the latest-starting wins.
	n := newTestSubs(t, SubtitleOptions{},
		Cue{Start: sec(0), End: sec(10), Text: "A"},
		Cue{Start: sec(2), End: sec(3), Text: "B"},
	)
	cases := []struct {
		t    clip.Time
		want string
	}{
		{sec(1), "A"},   // only A
		{sec(2.5), "B"}, // both play → latest-starting wins
		{sec(4), "A"},   // B ended, A still active (the regression case)
		{sec(9.9), "A"}, // A still active near its end
	}
	for _, c := range cases {
		idx := n.activeUnit(c.t)
		if idx < 0 {
			t.Fatalf("activeUnit(%v) = inactive, want %q", c.t, c.want)
		}
		if got := n.units[idx].text; got != c.want {
			t.Errorf("activeUnit(%v) = %q, want %q", c.t, got, c.want)
		}
	}
	if n.activeUnit(sec(10)) != -1 {
		t.Error("after the longest cue ends, nothing is active")
	}
}

func TestSubtitlesDurationIsLastEnd(t *testing.T) {
	n := newTestSubs(t, SubtitleOptions{},
		Cue{Start: sec(1), End: sec(2), Text: "a"},
		Cue{Start: sec(3), End: sec(5), Text: "b"},
	)
	d := n.Duration()
	if d != sec(5) {
		t.Fatalf("duration = %v; want 5s", d)
	}
}

func TestNoActiveCueIsFullyTransparent(t *testing.T) {
	n := newTestSubs(t, SubtitleOptions{}, Cue{Start: sec(1), End: sec(2), Text: "x"})
	rgb := clip.NewFrame(320, 240, clip.RGB24)
	alpha := clip.NewFrame(320, 240, clip.Gray8)
	// Dirty the buffers so we can confirm RenderInto clears them.
	for i := range alpha.Pix {
		alpha.Pix[i] = 200
	}
	has, err := n.RenderInto(context.Background(), sec(0.5), rgb, alpha)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("subtitles clip must report alpha")
	}
	for _, v := range alpha.Pix {
		if v != 0 {
			t.Fatal("between cues the alpha must be fully transparent")
		}
	}
}

func TestActiveCuePaintsAlpha(t *testing.T) {
	n := newTestSubs(t, SubtitleOptions{}, Cue{Start: sec(1), End: sec(2), Text: "Caption"})
	rgb := clip.NewFrame(320, 240, clip.RGB24)
	alpha := clip.NewFrame(320, 240, clip.Gray8)
	if _, err := n.RenderInto(context.Background(), sec(1.5), rgb, alpha); err != nil {
		t.Fatal(err)
	}
	painted := 0
	for _, v := range alpha.Pix {
		if v != 0 {
			painted++
		}
	}
	if painted == 0 {
		t.Fatal("an active cue must paint some opaque text pixels")
	}
}

func TestCueFramesCachedNotRerendered(t *testing.T) {
	n := newTestSubs(t, SubtitleOptions{}, Cue{Start: sec(1), End: sec(3), Text: "once"})
	var calls int32
	n.render = func(o Options) (*clip.Frame, error) {
		atomic.AddInt32(&calls, 1)
		return Render(o)
	}
	rgb := clip.NewFrame(320, 240, clip.RGB24)
	alpha := clip.NewFrame(320, 240, clip.Gray8)
	for _, ts := range []clip.Time{sec(1.1), sec(1.5), sec(2.9)} {
		if _, err := n.RenderInto(context.Background(), ts, rgb, alpha); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("same cue rendered %d times; want 1 (cached)", got)
	}
}

func TestCueCacheEviction(t *testing.T) {
	n := newTestSubs(t, SubtitleOptions{CacheSize: 1},
		Cue{Start: sec(0), End: sec(1), Text: "a"},
		Cue{Start: sec(1), End: sec(2), Text: "b"},
	)
	var calls int32
	n.render = func(o Options) (*clip.Frame, error) {
		atomic.AddInt32(&calls, 1)
		return Render(o)
	}
	rgb := clip.NewFrame(320, 240, clip.RGB24)
	alpha := clip.NewFrame(320, 240, clip.Gray8)
	// a, b, a — with capacity 1, the second 'a' is a miss again → 3 renders.
	for _, ts := range []clip.Time{sec(0.5), sec(1.5), sec(0.5)} {
		if _, err := n.RenderInto(context.Background(), ts, rgb, alpha); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("capacity-1 cache should force 3 renders, got %d", got)
	}
}

func TestPrerenderFillsCache(t *testing.T) {
	n := newTestSubs(t, SubtitleOptions{},
		Cue{Start: sec(0), End: sec(1), Text: "a"},
		Cue{Start: sec(1), End: sec(2), Text: "b"},
	)
	var calls int32
	n.render = func(o Options) (*clip.Frame, error) {
		atomic.AddInt32(&calls, 1)
		return Render(o)
	}
	if err := n.Prerender(); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("prerender should render each cue once, got %d", got)
	}
	// A subsequent render of either cue is a cache hit (no new calls).
	rgb := clip.NewFrame(320, 240, clip.RGB24)
	alpha := clip.NewFrame(320, 240, clip.Gray8)
	if _, err := n.RenderInto(context.Background(), sec(0.5), rgb, alpha); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("prerendered cue should not re-render, got %d calls", got)
	}
}

func TestWordCenterLayoutSplitsWords(t *testing.T) {
	cue := Cue{Start: sec(1), End: sec(4), Text: "Welcome to the", Words: []Word{
		{Text: "Welcome", Start: sec(1), End: sec(1.5)},
		{Text: "to", Start: sec(1.5), End: sec(2)},
		{Text: "the", Start: sec(2), End: sec(3)},
	}}
	n := newTestSubs(t, SubtitleOptions{Layout: LayoutWordCenter}, cue)
	if len(n.units) != 3 {
		t.Fatalf("word layout must yield one unit per word, got %d", len(n.units))
	}
	// Each word is active only within its own interval.
	if got := n.units[n.activeUnit(sec(1.2))].text; got != "Welcome" {
		t.Fatalf("at 1.2s want Welcome, got %q", got)
	}
	if got := n.units[n.activeUnit(sec(1.7))].text; got != "to" {
		t.Fatalf("at 1.7s want to, got %q", got)
	}
	if n.activeUnit(sec(3.5)) != -1 {
		t.Fatal("gap after last word must be inactive")
	}
}

func TestWordCenterFallsBackToWholeCue(t *testing.T) {
	// A cue with no per-word timing renders as a single centered unit.
	n := newTestSubs(t, SubtitleOptions{Layout: LayoutWordCenter},
		Cue{Start: sec(0), End: sec(2), Text: "no words here"})
	if len(n.units) != 1 || n.units[0].text != "no words here" {
		t.Fatalf("expected one whole-cue unit, got %+v", n.units)
	}
}

func TestWordCenterPlacesNearVerticalCenter(t *testing.T) {
	n := newTestSubs(t, SubtitleOptions{Layout: LayoutWordCenter, Size: clip.Size{W: 200, H: 400}},
		Cue{Start: sec(0), End: sec(2), Words: []Word{{Text: "Hi", Start: sec(0), End: sec(2)}}})
	rgb := clip.NewFrame(200, 400, clip.RGB24)
	alpha := clip.NewFrame(200, 400, clip.Gray8)
	if _, err := n.RenderInto(context.Background(), sec(1), rgb, alpha); err != nil {
		t.Fatal(err)
	}
	// Find the painted band's vertical center; it should sit near canvas center,
	// not at the bottom margin (which is where a caption would land).
	first, last := -1, -1
	for y := 0; y < 400; y++ {
		rowPainted := false
		for x := 0; x < 200; x++ {
			if alpha.Pix[y*alpha.Stride+x] != 0 {
				rowPainted = true
				break
			}
		}
		if rowPainted {
			if first < 0 {
				first = y
			}
			last = y
		}
	}
	if first < 0 {
		t.Fatal("word layout painted nothing")
	}
	mid := (first + last) / 2
	if mid < 150 || mid > 250 {
		t.Fatalf("word band center y=%d not near canvas center (150..250)", mid)
	}
}

func TestVPosOverridesPlacement(t *testing.T) {
	cue := Cue{Start: sec(0), End: sec(2), Text: "X"}
	low := newTestSubs(t, SubtitleOptions{Size: clip.Size{W: 200, H: 400}, VPos: 0.25}, cue)
	rgb := clip.NewFrame(200, 400, clip.RGB24)
	alpha := clip.NewFrame(200, 400, clip.Gray8)
	if _, err := low.RenderInto(context.Background(), sec(1), rgb, alpha); err != nil {
		t.Fatal(err)
	}
	first := -1
	for y := 0; y < 400 && first < 0; y++ {
		for x := 0; x < 200; x++ {
			if alpha.Pix[y*alpha.Stride+x] != 0 {
				first = y
				break
			}
		}
	}
	if first < 0 || first > 200 {
		t.Fatalf("VPos 0.25 should place text in the upper half, first painted row=%d", first)
	}
}

func TestConcurrentRenderInto(t *testing.T) {
	n := newTestSubs(t, SubtitleOptions{},
		Cue{Start: sec(0), End: sec(2), Text: "one"},
		Cue{Start: sec(2), End: sec(4), Text: "two"},
	)
	const workers = 16
	done := make(chan error, workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			rgb := clip.NewFrame(320, 240, clip.RGB24)
			alpha := clip.NewFrame(320, 240, clip.Gray8)
			ts := clip.Time(int64(w) * int64(sec(0.25)))
			_, err := n.RenderInto(context.Background(), ts, rgb, alpha)
			done <- err
		}(w)
	}
	for i := 0; i < workers; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
