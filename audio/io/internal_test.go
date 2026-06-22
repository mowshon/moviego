package audioio

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// q quantizes one normalized sample and returns the resulting int16.
func q(s float32) int16 {
	dst := make([]byte, nbytes)
	quantizePCM([]float32{s}, dst)
	return int16(binary.LittleEndian.Uint16(dst))
}

// TestQuantizeClampsFullScale verifies that a sample at or beyond full scale is
// clamped to ±0.99 before the int conversion, so it never wraps.
// Without the clamp, +1.0 * 32768 = 32768 wraps to -32768.
func TestQuantizeClampsFullScale(t *testing.T) {
	if v := q(1.0); v <= 0 {
		t.Fatalf("q(1.0) = %d, want positive (no full-scale wrap)", v)
	}
	clamp := q(quantClamp)
	if v := q(1.0); v != clamp {
		t.Fatalf("q(1.0) = %d, want clamp to q(0.99) = %d", v, clamp)
	}
	if v := q(5.0); v != clamp {
		t.Fatalf("q(5.0) = %d, want clamp to q(0.99) = %d", v, clamp)
	}
	if v, want := q(-5.0), q(-quantClamp); v != want {
		t.Fatalf("q(-5.0) = %d, want clamp to q(-0.99) = %d", v, want)
	}
	// The clamp must keep the magnitude strictly under full scale.
	if clamp >= pcmFullScale-1 {
		t.Fatalf("q(0.99) = %d, want < full scale %d", clamp, int(pcmFullScale))
	}
}

// TestQuantizeRoundsToNearest checks the round-half-away-from-zero behavior of
// the +0.5 / -0.5 bias before truncation.
func TestQuantizeRoundsToNearest(t *testing.T) {
	cases := []struct {
		in   float32
		want int16
	}{
		{0, 0},
		{100.0 / pcmFullScale, 100},
		{100.4 / pcmFullScale, 100},
		{100.6 / pcmFullScale, 101},
		{-100.4 / pcmFullScale, -100},
		{-100.6 / pcmFullScale, -101},
	}
	for _, c := range cases {
		if v := q(c.in); v != c.want {
			t.Fatalf("q(%v) = %d, want %d", c.in, v, c.want)
		}
	}
}

// TestQuantizeDecodeRoundTrip checks that values exactly representable as
// k/32768 survive a quantize -> decode round trip bit-for-bit.
func TestQuantizeDecodeRoundTrip(t *testing.T) {
	for _, k := range []int{0, 1, -1, 100, -100, 16384, -16384, 32000, -32000} {
		s := float32(k) / pcmFullScale
		dst := make([]byte, nbytes)
		quantizePCM([]float32{s}, dst)
		back := make([]float32, 1)
		decodePCM(dst, back)
		if back[0] != s {
			t.Fatalf("round trip %d/%d: got %f, want %f", k, int(pcmFullScale), back[0], s)
		}
	}
}

// TestDecoderSkipEqualsRestart verifies the seek state table: reading a target
// range by skipping forward (the steady-state path) yields the same samples as
// reading it after a backward jump forces an FFmpeg restart. It exercises the
// restarts hook to prove which path each read took.
func TestDecoderSkipEqualsRestart(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	dir := t.TempDir()
	path, err := genmedia.SineAudio(dir, "sine.wav", 440, 2.0, 44100)
	if err != nil {
		t.Fatalf("gen sine: %v", err)
	}
	const target, n = 50000, 1000
	ctx := context.Background()

	// Skip path: a fresh decoder reads [target, target+n) forward from 0.
	skipDec, err := OpenDecoder(ctx, path, DecoderOptions{})
	if err != nil {
		t.Fatalf("open skip decoder: %v", err)
	}
	defer skipDec.Close()
	ch := skipDec.Channels()
	skipBuf := make([]float32, n*ch)
	if err := skipDec.ReadInto(target, n, skipBuf); err != nil {
		t.Fatalf("skip read: %v", err)
	}
	if skipDec.restarts != 0 {
		t.Fatalf("skip path restarted %d times, want 0", skipDec.restarts)
	}

	// Restart path: push pos beyond target, then read [target, target+n); the
	// backward jump forces a restart.
	restartDec, err := OpenDecoder(ctx, path, DecoderOptions{})
	if err != nil {
		t.Fatalf("open restart decoder: %v", err)
	}
	defer restartDec.Close()
	ahead := make([]float32, n*ch)
	if err := restartDec.ReadInto(target+5*n, n, ahead); err != nil {
		t.Fatalf("ahead read: %v", err)
	}
	restartBuf := make([]float32, n*ch)
	if err := restartDec.ReadInto(target, n, restartBuf); err != nil {
		t.Fatalf("restart read: %v", err)
	}
	if restartDec.restarts == 0 {
		t.Fatal("restart path did not restart on the backward jump")
	}

	// Both paths must land on the same audio. PCM/WAV seeks are sample-accurate,
	// so allow only a tiny tolerance for any single-sample seek slop.
	var mismatch int
	for i := range skipBuf {
		if d := skipBuf[i] - restartBuf[i]; d > 1e-3 || d < -1e-3 {
			mismatch++
		}
	}
	if mismatch*100 > len(skipBuf) { // >1% disagreement
		t.Fatalf("skip vs restart disagree on %d/%d samples", mismatch, len(skipBuf))
	}
}
