package audioio

import (
	"context"
	"testing"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// TestReaperClosesRegistered checks the core wiring without FFmpeg: a decoder
// added to a reaper is closed by CloseAll, the set is emptied (a second call is a
// no-op), and Close stays idempotent so a node closed both ways never errors.
func TestReaperClosesRegistered(t *testing.T) {
	r := &Reaper{}
	d1 := &Decoder{}
	d2 := &Decoder{}
	r.add(d1)
	r.add(d2)

	if err := r.CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	if !d1.closed || !d2.closed {
		t.Fatalf("CloseAll left a decoder open: d1=%v d2=%v", d1.closed, d2.closed)
	}
	// Second CloseAll has nothing left and must not touch d1/d2 again.
	if err := r.CloseAll(); err != nil {
		t.Fatalf("second CloseAll: %v", err)
	}
	// The decoder's own Close is still safe to call afterwards (idempotent).
	if err := d1.Close(); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}
}

// TestReaperFromContext checks the context round-trip and that a bare context
// reports no reaper (the no-op fallback for nodes closed by their own handle).
func TestReaperFromContext(t *testing.T) {
	if reaperFrom(context.Background()) != nil {
		t.Fatal("bare context reported a reaper")
	}
	r := &Reaper{}
	if got := reaperFrom(WithReaper(context.Background(), r)); got != r {
		t.Fatalf("reaperFrom round-trip = %p, want %p", got, r)
	}
}

// TestOpenDecoderRegistersWithReaper is the integration path: a decoder opened
// under a reaper context is reaped by CloseAll, after which it reads as closed.
func TestOpenDecoderRegistersWithReaper(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	path, err := genmedia.SineAudio(t.TempDir(), "sine.wav", 440, 1.0, 44100)
	if err != nil {
		t.Fatalf("gen sine: %v", err)
	}
	r := &Reaper{}
	dec, err := OpenDecoder(WithReaper(context.Background(), r), path, DecoderOptions{})
	if err != nil {
		t.Fatalf("open decoder: %v", err)
	}
	if len(r.decoders) != 1 {
		t.Fatalf("reaper registered %d decoders, want 1", len(r.decoders))
	}
	if err := r.CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	if err := dec.ReadInto(0, 10, make([]float32, 10*dec.Channels())); err != clip.ErrClosed {
		t.Fatalf("read after reap = %v, want ErrClosed", err)
	}
}
