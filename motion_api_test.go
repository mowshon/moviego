package mgo_test

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	mgo "github.com/mowshon/moviego/v2"
)

// renderFrames renders v to a temp directory at 10 fps and returns how many
// frames were written. It exercises the whole graph in Go (Color/Canvas sources
// need no FFmpeg), so it is a real end-to-end check of a node's RenderInto.
func renderFrames(t *testing.T, v *mgo.Video) int {
	t.Helper()
	if v.Err() != nil {
		t.Fatalf("build error: %v", v.Err())
	}
	n, err := mgo.WriteFrameSequence(context.Background(), v, t.TempDir(), mgo.FrameSequenceOptions{
		Rate: mgo.Rate{Num: 10, Den: 1},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return n
}

func TestCanvasRendersShapes(t *testing.T) {
	c := mgo.NewCanvas(160, 120).
		Background(color.RGBA{R: 20, G: 20, B: 30, A: 255}).
		Rect(10, 10, 60, 40, mgo.Paint{Fill: color.RGBA{R: 200, A: 255}}).
		Circle(120, 60, 25, mgo.Paint{Fill: color.White, Stroke: color.Black, StrokeWidth: 3}).
		Line(0, 110, 160, 110, color.White, 5)

	v := c.WithDuration(time.Second)
	if v.Err() != nil {
		t.Fatal(v.Err())
	}
	if got := v.Size(); got != (mgo.Size{W: 160, H: 120}) {
		t.Errorf("canvas size = %+v, want 160x120", got)
	}
	if n := renderFrames(t, v); n == 0 {
		t.Error("canvas produced no frames")
	}
}

func TestAnimatedTextOverVideo(t *testing.T) {
	title, err := mgo.AnimatedText("Hello", mgo.TextOptions{FontSize: 36, Color: color.White},
		mgo.TextAnim{Type: mgo.AnimTypewriter, Dur: 600 * time.Millisecond, Easing: mgo.EaseOut})
	if err != nil {
		t.Fatal(err)
	}
	if title.Duration() != 600*time.Millisecond {
		t.Errorf("default duration = %v, want the animation length", title.Duration())
	}
	title = title.WithDuration(time.Second).Position(mgo.Center)

	bg := mgo.Color(320, 180, [3]byte{0, 0, 0}).WithDuration(time.Second)
	out := mgo.CompositeWith(mgo.CompositeOptions{Size: mgo.Size{W: 320, H: 180}}, bg, title)
	if n := renderFrames(t, out); n == 0 {
		t.Error("animated text composite produced no frames")
	}
}

func TestCreditsRoll(t *testing.T) {
	v, err := mgo.Credits([]string{"Directed by", "A. Director"},
		mgo.CreditsOptions{Size: mgo.Size{W: 240, H: 160}, FontSize: 24, Speed: 200})
	if err != nil {
		t.Fatal(err)
	}
	if !mgo.Finite(v.Duration()) || v.Duration() <= 0 {
		t.Fatalf("credits duration = %v, want a positive finite value", v.Duration())
	}
	if n := renderFrames(t, v); n == 0 {
		t.Error("credits produced no frames")
	}
}

func TestWatermarkComposites(t *testing.T) {
	base := mgo.Color(320, 180, [3]byte{10, 10, 10}).WithDuration(time.Second)
	logo := mgo.NewCanvas(40, 40).Circle(20, 20, 18, mgo.Paint{Fill: color.White}).Image()

	out := base.Watermark(logo, mgo.Corner(mgo.BottomRight), mgo.WithMargin(8))
	if out.Err() != nil {
		t.Fatal(out.Err())
	}
	if !mgo.Finite(out.Duration()) {
		t.Errorf("watermarked duration = %v, want finite (logo held for the base)", out.Duration())
	}
	if got := out.Size(); got != (mgo.Size{W: 320, H: 180}) {
		t.Errorf("watermarked size = %+v, want the base size", got)
	}
	if n := renderFrames(t, out); n == 0 {
		t.Error("watermark produced no frames")
	}
}

func TestBurnTimecode(t *testing.T) {
	base := mgo.Color(320, 180, [3]byte{0, 0, 0}).WithDuration(2 * time.Second)
	out := base.BurnTimecode(mgo.TimecodeOptions{Format: mgo.TCClock, FontSize: 20})
	if out.Err() != nil {
		t.Fatal(out.Err())
	}
	if n := renderFrames(t, out); n == 0 {
		t.Error("timecode burn produced no frames")
	}
}

func TestSplitAtCues(t *testing.T) {
	v := mgo.Color(64, 64, [3]byte{0, 0, 0}).WithDuration(6 * time.Second)
	cues := []mgo.Cue{
		{Start: 0, End: 2 * time.Second},
		{Start: 2 * time.Second, End: 5 * time.Second},
	}
	parts := mgo.SplitAtCues(v, cues)
	if len(parts) != 2 {
		t.Fatalf("got %d parts, want 2", len(parts))
	}
	if parts[0].Duration() != 2*time.Second {
		t.Errorf("part0 duration = %v, want 2s", parts[0].Duration())
	}
	if parts[1].Duration() != 3*time.Second {
		t.Errorf("part1 duration = %v, want 3s", parts[1].Duration())
	}
}

func TestSubtitlesASS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dialogue.ass")
	content := "[Events]\n" +
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: 0,0:00:00.00,0:00:02.00,Default,,0,0,0,,Hello\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := mgo.SubtitlesASS(path, mgo.SubtitleOptions{Size: mgo.Size{W: 320, H: 180}})
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Size(); got != (mgo.Size{W: 320, H: 180}) {
		t.Errorf("subtitles size = %+v, want the canvas", got)
	}
}
