package render

import (
	"context"
	"testing"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/internal/genmedia"
	"github.com/mowshon/moviego/v2/video"
	videoio "github.com/mowshon/moviego/v2/video/io"
)

// openSource opens a generated test clip as a file source for the provider.
func openSource(t *testing.T) video.FileSource {
	t.Helper()
	if !genmedia.Available() {
		t.Skip("ffmpeg/ffprobe not available")
	}
	path, err := genmedia.TestPatternVideo(t.TempDir(), "src.mp4", 32, 24, 1, "10")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	n, err := video.OpenFile(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = n.Close() })
	return n
}

// referenceFrame decodes one source frame with a fresh forward decoder, the
// independent oracle the provider's output is compared against.
func referenceFrame(t *testing.T, s video.FileSource, idx int) []byte {
	t.Helper()
	dec, err := videoio.OpenDecoder(context.Background(), s.SourcePath(), videoio.DecoderOptions{
		Size: s.SourceSize(),
		Rate: s.SourceRate(),
	})
	if err != nil {
		t.Fatalf("open decoder: %v", err)
	}
	defer dec.Close()
	size := s.SourceSize()
	f := clip.NewFrame(size.W, size.H, clip.RGB24)
	if err := dec.SeekToFrame(idx); err != nil {
		t.Fatalf("seek %d: %v", idx, err)
	}
	if err := dec.ReadInto(f); err != nil {
		t.Fatalf("read %d: %v", idx, err)
	}
	out := make([]byte, len(f.Pix))
	copy(out, f.Pix)
	return out
}

func TestReverseProviderBackwardWalk(t *testing.T) {
	s := openSource(t)
	const n = 10 // 1s @ 10fps
	const window = 4

	prov, err := newReverseProviderWindow(context.Background(), []video.FileSource{s}, window)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	defer prov.close()

	size := s.SourceSize()
	dst := clip.NewFrame(size.W, size.H, clip.RGB24)
	for i := n - 1; i >= 0; i-- {
		if err := prov.SourceFrameInto(context.Background(), s.SourceKey(), i, dst); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if want := referenceFrame(t, s, i); !equalBytes(dst.Pix, want) {
			t.Fatalf("backward frame %d differs from reference decode", i)
		}
	}

	// A window of 4 over 10 frames refills ceil(10/4) = 3 times. The point of the
	// optimization is that this is O(N/window), not O(N).
	if got := prov.sources[s.SourceKey()].reloads; got != 3 {
		t.Errorf("reloads = %d, want 3 for n=%d window=%d", got, n, window)
	}
}

func TestReverseProviderForwardWalkNoRefill(t *testing.T) {
	s := openSource(t)
	const n = 10

	prov, err := newReverseProviderWindow(context.Background(), []video.FileSource{s}, 4)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	defer prov.close()

	size := s.SourceSize()
	dst := clip.NewFrame(size.W, size.H, clip.RGB24)
	for i := 0; i < n; i++ {
		if err := prov.SourceFrameInto(context.Background(), s.SourceKey(), i, dst); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if want := referenceFrame(t, s, i); !equalBytes(dst.Pix, want) {
			t.Fatalf("forward frame %d differs from reference decode", i)
		}
	}
	// A pure forward walk extends the window in place: one initial fill, no
	// backward refills.
	if got := prov.sources[s.SourceKey()].reloads; got != 1 {
		t.Errorf("reloads = %d, want 1 for a forward walk", got)
	}
}

// TestReverseProviderShortClipPrefill verifies the short-clip optimisation:
// when the window is larger than the clip's total frame count, the provider
// loads all frames in one forward pass (prefillFull) and serves the entire
// backward walk from cache — reloads stays zero.
func TestReverseProviderShortClipPrefill(t *testing.T) {
	s := openSource(t)
	const n = 10 // 1s @ 10fps

	// Window larger than the clip: short-clip pre-fill path.
	prov, err := newReverseProviderWindow(context.Background(), []video.FileSource{s}, n+5)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	defer prov.close()

	size := s.SourceSize()
	dst := clip.NewFrame(size.W, size.H, clip.RGB24)

	// Backward walk: every frame should be a cache hit after the initial prefill.
	for i := n - 1; i >= 0; i-- {
		if err := prov.SourceFrameInto(context.Background(), s.SourceKey(), i, dst); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if want := referenceFrame(t, s, i); !equalBytes(dst.Pix, want) {
			t.Fatalf("prefill frame %d differs from reference decode", i)
		}
	}

	r := prov.sources[s.SourceKey()]
	if r.reloads != 0 {
		t.Errorf("reloads = %d, want 0 (short-clip prefill should have no sliding-window reloads)", r.reloads)
	}
	if !r.prefilled {
		t.Error("prefilled = false, want true after short-clip load")
	}
}

// TestReverseProviderClampsPastEndEOF verifies that a request one index past the
// last real frame — what the reverse map produces when FrameCount overcounts
// from a probed duration that exceeds the decodable frames — clamps to the last
// real frame instead of erroring or returning an empty result. It is checked on
// both the sliding-window path (small window) and the short-clip prefill path
// (window larger than the clip).
func TestReverseProviderClampsPastEndEOF(t *testing.T) {
	s := openSource(t)
	const n = 10 // real frames 0..9

	for _, window := range []int{4, n + 5} {
		prov, err := newReverseProviderWindow(context.Background(), []video.FileSource{s}, window)
		if err != nil {
			t.Fatalf("provider (window %d): %v", window, err)
		}

		size := s.SourceSize()
		dst := clip.NewFrame(size.W, size.H, clip.RGB24)
		// Frame n is one past the last real frame (9): must not error.
		if err := prov.SourceFrameInto(context.Background(), s.SourceKey(), n, dst); err != nil {
			prov.close()
			t.Fatalf("past-end frame %d (window %d): %v", n, window, err)
		}
		if want := referenceFrame(t, s, n-1); !equalBytes(dst.Pix, want) {
			prov.close()
			t.Fatalf("past-end frame %d (window %d) should clamp to last real frame %d", n, window, n-1)
		}
		prov.close()
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
