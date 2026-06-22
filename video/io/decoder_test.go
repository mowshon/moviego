package videoio

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// fixture writes a 5s 64x64 testsrc clip at 30fps (150 frames) and returns a
// decoder for it, skipping when FFmpeg is unavailable.
func fixture(t *testing.T) (*Decoder, clip.Size) {
	t.Helper()
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	dir := t.TempDir()
	path, err := genmedia.TestPatternVideo(dir, "pat.mp4", 64, 64, 5, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	dec, err := OpenDecoder(context.Background(), path, DecoderOptions{
		Size: clip.Size{W: 64, H: 64},
		Rate: clip.Rate{Num: 30, Den: 1},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { dec.Close() })
	return dec, clip.Size{W: 64, H: 64}
}

func TestDecoderReadSequential(t *testing.T) {
	dec, size := fixture(t)
	f := clip.NewFrame(size.W, size.H, clip.RGB24)
	for i := 0; i < 10; i++ {
		if err := dec.ReadInto(f); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if dec.Pos() != i+1 {
			t.Fatalf("pos after read %d = %d, want %d", i, dec.Pos(), i+1)
		}
	}
}

func TestDecoderSkipEqualsRestart(t *testing.T) {
	const target = 50

	skipDec, size := fixture(t)
	skipFrame := clip.NewFrame(size.W, size.H, clip.RGB24)
	if err := skipDec.SeekToFrame(target); err != nil { // pos 0 -> 50: skip path
		t.Fatalf("skip seek: %v", err)
	}
	if skipDec.restarts != 0 {
		t.Fatalf("expected skip, got %d restarts", skipDec.restarts)
	}
	if err := skipDec.ReadInto(skipFrame); err != nil {
		t.Fatalf("skip read: %v", err)
	}

	restartDec, _ := fixture(t)
	restartFrame := clip.NewFrame(size.W, size.H, clip.RGB24)
	if err := restartDec.SeekToFrame(120); err != nil { // far ahead: restart
		t.Fatalf("seek 120: %v", err)
	}
	if err := restartDec.SeekToFrame(target); err != nil { // backward: restart
		t.Fatalf("restart seek: %v", err)
	}
	if restartDec.restarts != 2 {
		t.Fatalf("expected 2 restarts, got %d", restartDec.restarts)
	}
	if err := restartDec.ReadInto(restartFrame); err != nil {
		t.Fatalf("restart read: %v", err)
	}

	if !bytes.Equal(skipFrame.Pix, restartFrame.Pix) {
		t.Errorf("skip-path and restart-path pixels differ for frame %d", target)
	}
}

func TestDecoderThresholdBoundary(t *testing.T) {
	atThreshold, _ := fixture(t)
	if err := atThreshold.SeekToFrame(restartThreshold); err != nil { // pos+100: skip
		t.Fatalf("seek: %v", err)
	}
	if atThreshold.restarts != 0 {
		t.Errorf("seek to pos+%d should skip, got %d restarts", restartThreshold, atThreshold.restarts)
	}

	pastThreshold, _ := fixture(t)
	if err := pastThreshold.SeekToFrame(restartThreshold + 1); err != nil { // pos+101: restart
		t.Fatalf("seek: %v", err)
	}
	if pastThreshold.restarts != 1 {
		t.Errorf("seek to pos+%d should restart, got %d restarts", restartThreshold+1, pastThreshold.restarts)
	}
}

func TestDecoderBackwardRestart(t *testing.T) {
	dec, size := fixture(t)
	f := clip.NewFrame(size.W, size.H, clip.RGB24)
	for i := 0; i < 5; i++ {
		if err := dec.ReadInto(f); err != nil {
			t.Fatalf("read: %v", err)
		}
	}
	if err := dec.SeekToFrame(1); err != nil { // backward: restart
		t.Fatalf("seek back: %v", err)
	}
	if dec.restarts != 1 {
		t.Errorf("backward seek should restart, got %d restarts", dec.restarts)
	}
	if dec.Pos() != 1 {
		t.Errorf("pos = %d, want 1", dec.Pos())
	}
}

func TestDecoderCancelClearsProc(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	path, err := genmedia.TestPatternVideo(t.TempDir(), "pat.mp4", 64, 64, 5, "30")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	dec, err := OpenDecoder(ctx, path, DecoderOptions{
		Size: clip.Size{W: 64, H: 64},
		Rate: clip.Rate{Num: 30, Den: 1},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dec.Close()

	f := clip.NewFrame(64, 64, clip.RGB24)
	cancel()
	// Reads drain the pipe buffer, then fail once the killed process closes it.
	// The failure must not be reached as a clean EOF, and the dead process must
	// be cleared so the decoder is not left in a broken state.
	var readErr error
	for i := 0; i < 60 && readErr == nil; i++ {
		readErr = dec.ReadInto(f)
	}
	if readErr == nil {
		t.Fatal("expected a read error after cancellation")
	}
	if errors.Is(readErr, clip.ErrEOF) {
		t.Errorf("cancellation reported as clean EOF: %v", readErr)
	}
	if dec.proc != nil {
		t.Error("dead process not cleared after cancellation")
	}
}

func TestDecoderEOF(t *testing.T) {
	dec, size := fixture(t)
	if err := dec.SeekToFrame(149); err != nil { // last frame of a 150-frame clip
		t.Fatalf("seek: %v", err)
	}
	f := clip.NewFrame(size.W, size.H, clip.RGB24)
	if err := dec.ReadInto(f); err != nil { // frame 149
		t.Fatalf("read last: %v", err)
	}
	if err := dec.ReadInto(f); !errors.Is(err, clip.ErrEOF) { // past end
		t.Errorf("read past end = %v, want ErrEOF", err)
	}
}
