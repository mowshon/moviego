package mgo_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

func TestFacadeOpenTrimWrite(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	src, err := genmedia.TestPatternVideo(t.TempDir(), "src.mp4", 64, 48, 4, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}

	v, err := mgo.OpenVideo(src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer v.Close()

	if d := v.Duration(); !mgo.Finite(d) || d < 3900*time.Millisecond || d > 4100*time.Millisecond {
		t.Errorf("duration = %v, want ~4s", d)
	}

	out := filepath.Join(t.TempDir(), "out.mp4")
	clip := v.Subclip(mgo.Sec(1), mgo.Sec(2)) // 1s window
	if err := mgo.WriteVideo(context.Background(), clip, out, mgo.ExportOptions{
		Rate: mgo.Rate{Num: 30, Den: 1},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := ffmpeg.Probe(context.Background(), out, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.Video == nil || info.Video.Size != (mgo.Size{W: 64, H: 48}) {
		t.Fatalf("video = %+v, want 64x48", info.Video)
	}
	if d := info.Duration; d < 900*time.Millisecond || d > 1100*time.Millisecond {
		t.Errorf("output duration = %v, want ~1s", d)
	}
}

// TestFacadeSubclipOpenEnd: Subclip with only a start trims to the end of the
// media; passing an explicit end trims to that end. Both forms share one
// variadic method.
func TestFacadeSubclipOpenEnd(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	src, err := genmedia.TestPatternVideo(t.TempDir(), "src.mp4", 32, 24, 4, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	v, err := mgo.OpenVideo(src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer v.Close()

	full := v.Duration()
	// Open-ended: start at 1s, run to the end (~3s remaining).
	open := v.Subclip(mgo.Sec(1))
	if got, want := open.Duration(), full-mgo.Sec(1); got < want-50*time.Millisecond || got > want+50*time.Millisecond {
		t.Errorf("open-ended subclip duration = %v, want ~%v", got, want)
	}
	// Explicit end matches the two-argument form.
	bounded := v.Subclip(mgo.Sec(1), mgo.Sec(2))
	if got := bounded.Duration(); got < 950*time.Millisecond || got > 1050*time.Millisecond {
		t.Errorf("bounded subclip duration = %v, want ~1s", got)
	}
}

// TestFacadeAudioSpeedInvalid: a non-positive speed factor is rejected on the
// returned handle with audio.ErrInvalidSpeed rather than building a node with an
// undefined time map.
func TestFacadeAudioSpeedInvalid(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	path, err := genmedia.SineAudio(t.TempDir(), "sine.wav", 440, 1.0, 44100)
	if err != nil {
		t.Fatalf("gen sine: %v", err)
	}
	a, err := mgo.OpenAudio(path)
	if err != nil {
		t.Fatalf("open audio: %v", err)
	}
	defer a.Close()

	for _, f := range []float64{0, -1, -3.5} {
		if got := a.Speed(f).Err(); !errors.Is(got, audio.ErrInvalidSpeed) {
			t.Fatalf("Speed(%v).Err() = %v, want ErrInvalidSpeed", f, got)
		}
	}
	if got := a.Speed(2).Err(); got != nil {
		t.Fatalf("Speed(2).Err() = %v, want nil", got)
	}
}

// TestFacadeAudioSubclipOpenEnd: (*Audio).Subclip with only a start trims to the
// end of the audio; an explicit end matches. This proves the variadic audio
// facade resolves the open end against the source duration like video.
func TestFacadeAudioSubclipOpenEnd(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	path, err := genmedia.SineAudio(t.TempDir(), "sine.wav", 440, 2.0, 44100)
	if err != nil {
		t.Fatalf("gen sine: %v", err)
	}
	a, err := mgo.OpenAudio(path)
	if err != nil {
		t.Fatalf("open audio: %v", err)
	}
	defer a.Close()

	full := a.Duration()
	if !mgo.Finite(full) {
		t.Fatalf("source duration = %v, want finite", full)
	}
	// Open-ended: start at 0.5s, run to the end.
	open := a.Subclip(mgo.Sec(0.5))
	if got, want := open.Duration(), full-mgo.Sec(0.5); got < want-50*time.Millisecond || got > want+50*time.Millisecond {
		t.Errorf("open-ended audio subclip = %v, want ~%v", got, want)
	}
	// Explicit end matches the two-argument form.
	bounded := a.Subclip(mgo.Sec(0.5), mgo.Sec(1.0))
	if got := bounded.Duration(); got < 450*time.Millisecond || got > 550*time.Millisecond {
		t.Errorf("bounded audio subclip = %v, want ~500ms", got)
	}
}

// TestFacadeInfiniteDurationRejected: an unbounded clip (a Color with no
// duration) cannot be exported — the planner rejects Infinite with
// ErrNoDuration before touching any file.
func TestFacadeInfiniteDurationRejected(t *testing.T) {
	c := mgo.Color(64, 48, [3]byte{0, 0, 0}) // no WithDuration -> Infinite
	if d := c.Duration(); mgo.Finite(d) {
		t.Fatalf("color duration = %v, want Infinite", d)
	}
	out := filepath.Join(t.TempDir(), "out.mp4")
	err := mgo.WriteVideo(context.Background(), c, out, mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}})
	if !errors.Is(err, clip.ErrNoDuration) {
		t.Fatalf("WriteVideo err = %v, want ErrNoDuration", err)
	}
}
