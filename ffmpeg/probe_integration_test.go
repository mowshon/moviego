package ffmpeg_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

func TestProbeVideoOnly(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path, err := genmedia.ColorVideo(dir, "red.mp4", "red", 160, 120, 2, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	info := mustProbe(t, path)
	if info.Video == nil {
		t.Fatal("no video stream")
	}
	if info.Audio != nil {
		t.Error("unexpected audio stream")
	}
	if info.Video.Size != (clip.Size{W: 160, H: 120}) {
		t.Errorf("size = %v, want 160x120", info.Video.Size)
	}
	if info.Video.Rate != (clip.Rate{Num: 30, Den: 1}) {
		t.Errorf("rate = %v, want 30/1", info.Video.Rate)
	}
	if d := info.Duration; d < 1900*time.Millisecond || d > 2100*time.Millisecond {
		t.Errorf("duration = %v, want ~2s", d)
	}
}

func TestProbeAudioOnly(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path, err := genmedia.SineAudio(dir, "tone.wav", 440, 1, 44100)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	info := mustProbe(t, path)
	if info.Video != nil {
		t.Error("unexpected video stream")
	}
	if info.Audio == nil {
		t.Fatal("no audio stream")
	}
	if info.Audio.SampleRate != 44100 {
		t.Errorf("sample rate = %d, want 44100", info.Audio.SampleRate)
	}
}

func TestProbeVideoAudio(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path, err := genmedia.VideoWithAudio(dir, "av.mp4", 128, 96, 1, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	info := mustProbe(t, path)
	if info.Video == nil || info.Audio == nil {
		t.Fatalf("want video+audio, got video=%v audio=%v", info.Video, info.Audio)
	}
	if info.Video.Rate != (clip.Rate{Num: 25, Den: 1}) {
		t.Errorf("rate = %v, want 25/1", info.Video.Rate)
	}
}

func TestProbeNTSCRate(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path, err := genmedia.ColorVideo(dir, "ntsc.mp4", "blue", 64, 64, 1, "30000/1001")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	info := mustProbe(t, path)
	if info.Video.Rate != (clip.Rate{Num: 30000, Den: 1001}) {
		t.Errorf("rate = %v, want 30000/1001", info.Video.Rate)
	}
}

func TestProbeRotation(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path, err := genmedia.RotatedVideo(dir, "rot.mp4", 160, 120, 1, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	info := mustProbe(t, path)
	// Stored 160x120 with a 90-degree rotation must display as 120x160.
	if info.Video.Rotation != 90 && info.Video.Rotation != 270 {
		t.Errorf("rotation = %d, want 90 or 270", info.Video.Rotation)
	}
	if info.Video.Size != (clip.Size{W: 120, H: 160}) {
		t.Errorf("display size = %v, want 120x160 (swapped)", info.Video.Size)
	}
	if info.Video.RawSize != (clip.Size{W: 160, H: 120}) {
		t.Errorf("raw size = %v, want 160x120", info.Video.RawSize)
	}
}

func TestProbeCorruptInput(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	// A .mp4 that is not actually media.
	bad := dir + "/broken.mp4"
	if err := os.WriteFile(bad, []byte("not a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ffmpeg.Probe(context.Background(), bad, ffmpeg.ProbeOptions{})
	if err == nil {
		t.Fatal("expected error for corrupt input")
	}
	if !isCorrupted(err) {
		t.Errorf("error = %v, want ErrVideoCorrupted", err)
	}
}

func TestProbeCancelNotCorrupted(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	path, err := genmedia.ColorVideo(dir, "ok.mp4", "green", 64, 64, 1, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before probing

	_, err = ffmpeg.Probe(ctx, path, ffmpeg.ProbeOptions{})
	if err == nil {
		t.Fatal("expected error on canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if errors.Is(err, clip.ErrVideoCorrupted) {
		t.Error("cancellation must not be reported as corrupt media")
	}
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
}

func mustProbe(t *testing.T, path string) *ffmpeg.MediaInfo {
	t.Helper()
	info, err := ffmpeg.Probe(context.Background(), path, ffmpeg.ProbeOptions{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	return info
}

func isCorrupted(err error) bool {
	return errors.Is(err, clip.ErrVideoCorrupted)
}
