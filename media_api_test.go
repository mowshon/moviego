package mgo_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// TestParseTime checks the facade re-export parses the documented formats.
func TestParseTime(t *testing.T) {
	cases := map[string]time.Duration{
		"33.5":         33500 * time.Millisecond,
		"1:33.5":       93500 * time.Millisecond,
		"01:23:45.678": time.Hour + 23*time.Minute + 45*time.Second + 678*time.Millisecond,
	}
	for in, want := range cases {
		got, err := mgo.ParseTime(in)
		if err != nil {
			t.Errorf("ParseTime(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseTime(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestEaseReexport checks the facade easing curves match their package
// definitions at the midpoint.
func TestEaseReexport(t *testing.T) {
	if mgo.Linear(0.5) != 0.5 {
		t.Errorf("Linear(0.5) = %v, want 0.5", mgo.Linear(0.5))
	}
	if mgo.EaseIn(0.5) != 0.25 {
		t.Errorf("EaseIn(0.5) = %v, want 0.25", mgo.EaseIn(0.5))
	}
	if mgo.EaseSmooth(0) != 0 || mgo.EaseSmooth(1) != 1 {
		t.Error("EaseSmooth endpoints not fixed")
	}
}

// TestDescribeRandomSequential asserts Describe reports the sequential engine
// for a random-access graph (a loop). It needs no toolchain (static source).
func TestDescribeRandomSequential(t *testing.T) {
	clip := mgo.Color(16, 16, [3]byte{0, 0, 0}).WithDuration(time.Second).Loop(2)
	rep, err := mgo.Describe(clip, mgo.ExportOptions{
		Rate:    mgo.Rate{Num: 24, Den: 1},
		Workers: 4,
	})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if rep.Engine != mgo.EngineSequential {
		t.Errorf("engine = %s, want sequential", rep.Engine)
	}
}

// TestValidateCarriesBuildError checks Validate surfaces a handle's build error
// before planning.
func TestValidateCarriesBuildError(t *testing.T) {
	// Resize{Scale:0} records a build error on the handle.
	bad := mgo.Color(16, 16, [3]byte{0, 0, 0}).WithDuration(time.Second).Resize(0)
	if err := mgo.Validate(bad); err == nil {
		t.Fatal("Validate accepted a clip carrying a build error")
	}
}

// TestWriteFrameSequencePNG renders a solid color clip to numbered PNGs without
// any external toolchain.
func TestWriteFrameSequencePNG(t *testing.T) {
	dir := t.TempDir()
	clip := mgo.Color(8, 8, [3]byte{10, 20, 30}).WithDuration(500 * time.Millisecond)
	n, err := mgo.WriteFrameSequence(context.Background(), clip, dir, mgo.FrameSequenceOptions{
		Rate: mgo.Rate{Num: 10, Den: 1},
	})
	if err != nil {
		t.Fatalf("write frame sequence: %v", err)
	}
	if n != 5 {
		t.Fatalf("wrote %d frames, want 5", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "frame_00000.png")); err != nil {
		t.Errorf("first frame missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "frame_00004.png")); err != nil {
		t.Errorf("last frame missing: %v", err)
	}
}

// TestWriteFrameSequenceJPEG checks the JPEG path writes .jpg files.
func TestWriteFrameSequenceJPEG(t *testing.T) {
	dir := t.TempDir()
	clip := mgo.Color(8, 8, [3]byte{10, 20, 30}).WithDuration(300 * time.Millisecond)
	n, err := mgo.WriteFrameSequence(context.Background(), clip, dir, mgo.FrameSequenceOptions{
		Format:  mgo.FormatJPEG,
		Quality: 80,
		Rate:    mgo.Rate{Num: 10, Den: 1},
	})
	if err != nil {
		t.Fatalf("write frame sequence: %v", err)
	}
	if n != 3 {
		t.Fatalf("wrote %d frames, want 3", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "frame_00000.jpg")); err != nil {
		t.Errorf("first jpg missing: %v", err)
	}
}

// TestWriteFrameSequenceInvalidRate guards the hang: a malformed override rate
// must error promptly rather than spin forever (FrameTime stays 0 with Den=0).
func TestWriteFrameSequenceInvalidRate(t *testing.T) {
	clip := mgo.Color(8, 8, [3]byte{0, 0, 0}).WithDuration(time.Second)
	// A directory that does not exist yet, so we can assert the rejected call
	// left no filesystem side effect.
	dir := filepath.Join(t.TempDir(), "frames")
	done := make(chan error, 1)
	go func() {
		_, err := mgo.WriteFrameSequence(context.Background(), clip, dir, mgo.FrameSequenceOptions{
			Rate: mgo.Rate{Num: 24, Den: 0},
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("invalid rate accepted; want an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WriteFrameSequence hung on an invalid rate")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("invalid rate created the output directory (stat err = %v)", err)
	}
}

// TestPositionFuncComposites checks an animated placement builds and renders a
// composite without error.
func TestPositionFuncComposites(t *testing.T) {
	bg := mgo.Color(20, 20, [3]byte{0, 0, 0}).WithDuration(time.Second)
	fg := mgo.Color(4, 4, [3]byte{255, 255, 255}).WithDuration(time.Second).
		PositionFunc(func(t mgo.Time) mgo.Position {
			x := float64(t) / float64(time.Second) * 10
			return mgo.At(int(x), 0)
		})
	comp := mgo.Composite(bg, fg)
	if comp.Err() != nil {
		t.Fatalf("composite build error: %v", comp.Err())
	}
	if err := mgo.Validate(comp, mgo.ExportOptions{Rate: mgo.Rate{Num: 10, Den: 1}}); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// TestProbeRealFile probes a generated clip and checks the surfaced metadata.
func TestProbeRealFile(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	path, err := genmedia.ColorVideo(t.TempDir(), "c.mp4", "red", 64, 48, 2, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	info, err := mgo.Probe(path)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Size != (mgo.Size{W: 64, H: 48}) {
		t.Errorf("size = %v, want 64x48", info.Size)
	}
	if info.Duration < time.Second {
		t.Errorf("duration = %v, want ~2s", info.Duration)
	}
	if info.Codec == "" || info.PixFmt == "" {
		t.Errorf("missing codec/pixfmt: %+v", info)
	}
	if info.HasAlpha {
		t.Errorf("yuv420p clip reported HasAlpha")
	}
}

// TestExtractFrameRealFile decodes a single frame from a generated clip.
func TestExtractFrameRealFile(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	path, err := genmedia.ColorVideo(t.TempDir(), "c.mp4", "green", 32, 24, 2, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	img, err := mgo.ExtractFrame(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 32 || b.Dy() != 24 {
		t.Errorf("frame size = %dx%d, want 32x24", b.Dx(), b.Dy())
	}
}
