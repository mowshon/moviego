package video

import (
	"context"
	"errors"
	"testing"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/internal/genmedia"
)

// recordingAudio is a minimal AudioClip that counts Close calls.
type recordingAudio struct{ closed int }

func (a *recordingAudio) Start() clip.Time                  { return 0 }
func (a *recordingAudio) Duration() clip.Time               { return clip.Infinite }
func (a *recordingAudio) End() clip.Time                    { return clip.Infinite }
func (a *recordingAudio) WithStart(clip.Time) clip.Clip     { return a }
func (a *recordingAudio) WithDuration(clip.Time) clip.Clip  { return a }
func (a *recordingAudio) WithEnd(clip.Time, bool) clip.Clip { return a }
func (a *recordingAudio) SampleRate() int                   { return 44100 }
func (a *recordingAudio) Channels() int                     { return 2 }
func (a *recordingAudio) Close() error                      { a.closed++; return nil }
func (a *recordingAudio) SamplesInto(context.Context, clip.Time, int, *clip.AudioBuffer) error {
	return nil
}

// TestVideoFileNodeClosesAudioSidecar verifies ownership: a file node owns the
// audio sidecar it created in OpenFile, so its Close must release it. For a real
// AudioFileNode that release kills the FFmpeg decode
// subprocess, so without this the audio decoder leaks on the normal export path
// (the render never closes the user's nodes). Idempotency is checked too.
func TestVideoFileNodeClosesAudioSidecar(t *testing.T) {
	a := &recordingAudio{}
	n := &VideoFileNode{path: "x.mp4", audioClip: a, r: newReader()}
	if err := n.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if a.closed == 0 {
		t.Fatal("Close did not release the audio sidecar")
	}
	if err := n.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestVideoFileNodeClosesRealDecoder proves the chain end-to-end: pulling audio
// samples lazily opens the sidecar's FFmpeg decoder, and closing the video file
// node closes that decoder (a later pull returns ErrClosed). This is the leak
// fix observed against a real subprocess, without racy process-count sampling.
func TestVideoFileNodeClosesRealDecoder(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	src, err := genmedia.VideoWithAudio(t.TempDir(), "av.mp4", 48, 32, 4, "25")
	if err != nil {
		t.Fatalf("gen a/v: %v", err)
	}
	n, err := OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	a := n.Audio()
	if a == nil {
		t.Fatal("expected an audio sidecar")
	}
	// Open the decoder by pulling a partial range (well short of EOF).
	buf := &clip.AudioBuffer{}
	if err := a.SamplesInto(context.Background(), 0, 1000, buf); err != nil {
		t.Fatalf("SamplesInto: %v", err)
	}
	if err := n.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The sidecar's decoder is now closed: a further pull must report it rather
	// than silently reopening (which would defeat the release).
	if err := a.SamplesInto(context.Background(), 0, 1000, buf); !errors.Is(err, clip.ErrClosed) {
		t.Fatalf("SamplesInto after Close = %v, want ErrClosed", err)
	}
}
