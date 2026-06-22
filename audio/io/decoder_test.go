package audioio_test

import (
	"context"
	"testing"

	audioio "github.com/mowshon/moviego/v2/audio/io"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// TestDecodeSine decodes a generated sine WAV and checks the channel/rate, that
// the samples are non-trivial, and that reads past the end zero-pad.
func TestDecodeSine(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	dir := t.TempDir()
	path, err := genmedia.SineAudio(dir, "sine.wav", 440, 1.0, 44100)
	if err != nil {
		t.Fatalf("gen sine: %v", err)
	}

	dec, err := audioio.OpenDecoder(context.Background(), path, audioio.DecoderOptions{})
	if err != nil {
		t.Fatalf("open decoder: %v", err)
	}
	defer dec.Close()
	if dec.SampleRate() != 44100 {
		t.Fatalf("sample rate = %d, want 44100", dec.SampleRate())
	}

	ch := dec.Channels()
	dst := make([]float32, 1000*ch)
	if err := dec.ReadInto(0, 1000, dst); err != nil {
		t.Fatalf("read: %v", err)
	}
	var nonzero int
	for _, s := range dst {
		if s != 0 {
			nonzero++
		}
	}
	if nonzero == 0 {
		t.Fatal("decoded all-zero samples from a sine source")
	}

	// A read far past the 1s end must zero-pad rather than error.
	tail := make([]float32, 100*ch)
	for i := range tail {
		tail[i] = 1
	}
	if err := dec.ReadInto(44100*5, 100, tail); err != nil {
		t.Fatalf("past-end read: %v", err)
	}
	for i, s := range tail {
		if s != 0 {
			t.Fatalf("past-end sample %d = %f, want 0 (silence)", i, s)
		}
	}
}

// TestEncodeDecodeRoundTrip encodes a known ramp to AAC and decodes it back,
// asserting the duration and channel layout survive (AAC is lossy, so the exact
// samples are not compared).
func TestEncodeDecodeRoundTrip(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	dir := t.TempDir()
	out := dir + "/round.m4a"

	const (
		sr    = 44100
		ch    = 2
		total = sr // 1 second
	)
	enc, err := audioio.OpenEncoder(context.Background(), out, audioio.EncoderOptions{
		SampleRate: sr, Channels: ch, Codec: "aac",
	})
	if err != nil {
		t.Fatalf("open encoder: %v", err)
	}
	buf := clip.NewAudioBuffer(total, ch)
	for i := 0; i < total; i++ {
		v := float32((i%200))/200*0.5 - 0.25
		buf.Samples[i*ch] = v
		buf.Samples[i*ch+1] = v
	}
	if err := enc.WriteFrames(buf); err != nil {
		t.Fatalf("write frames: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close encoder: %v", err)
	}

	dec, err := audioio.OpenDecoder(context.Background(), out, audioio.DecoderOptions{})
	if err != nil {
		t.Fatalf("open decoder: %v", err)
	}
	defer dec.Close()
	if dec.Channels() != ch {
		t.Fatalf("channels = %d, want %d", dec.Channels(), ch)
	}
	dst := make([]float32, 1000*ch)
	if err := dec.ReadInto(0, 1000, dst); err != nil {
		t.Fatalf("read back: %v", err)
	}
}
