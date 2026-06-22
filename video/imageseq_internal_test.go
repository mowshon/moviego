package video

import (
	"context"
	"errors"
	"testing"

	"github.com/mowshon/moviego/v2/clip"
)

// countingLoader returns a fresh solid frame per path and records how many
// times it was called, so cache behavior is observable without touching disk.
type countingLoader struct {
	calls int
	size  clip.Size
}

func (l *countingLoader) load(path string) (*clip.Frame, error) {
	l.calls++
	f := clip.NewFrame(l.size.W, l.size.H, clip.RGB24)
	f.Pix[0] = path[len(path)-1] // marker from the last path byte
	return f, nil
}

func TestImageSequenceCachesDiskReads(t *testing.T) {
	loader := &countingLoader{size: clip.Size{W: 2, H: 2}}
	n, err := newImageSequence(loader.load, []string{"img0", "img1", "img2"},
		ImageSequenceOptions{FPS: clip.Rate{Num: 1, Den: 1}, CacheSize: 8})
	if err != nil {
		t.Fatalf("seq: %v", err)
	}
	// Construction decodes the first image once.
	if loader.calls != 1 {
		t.Fatalf("after construction calls = %d, want 1", loader.calls)
	}
	// Render each image's window three times; with an ample cache each distinct
	// image is read at most once (image 0 already cached at construction).
	ctx := context.Background()
	dst := clip.NewFrame(2, 2, clip.RGB24)
	for pass := 0; pass < 3; pass++ {
		for sec := 0; sec < 3; sec++ {
			if err := n.FrameInto(ctx, clip.Time(sec)*clip.Time(1e9), dst); err != nil {
				t.Fatalf("frame: %v", err)
			}
		}
	}
	if loader.calls != 3 {
		t.Errorf("loader calls = %d, want 3 (one per distinct image, no re-reads)", loader.calls)
	}
}

func TestImageSequenceLRUEvictsAndReloads(t *testing.T) {
	loader := &countingLoader{size: clip.Size{W: 2, H: 2}}
	n, err := newImageSequence(loader.load, []string{"a", "b"},
		ImageSequenceOptions{FPS: clip.Rate{Num: 1, Den: 1}, CacheSize: 1})
	if err != nil {
		t.Fatalf("seq: %v", err)
	}
	ctx := context.Background()
	dst := clip.NewFrame(2, 2, clip.RGB24)
	// idx0 (cached at construction), idx1 (evicts 0), idx0 again (reload).
	_ = n.FrameInto(ctx, 0, dst)              // hit
	_ = n.FrameInto(ctx, clip.Time(1e9), dst) // miss -> load b, evict a
	_ = n.FrameInto(ctx, 0, dst)              // miss -> reload a
	if loader.calls != 3 {
		t.Errorf("loader calls = %d, want 3 under capacity-1 eviction", loader.calls)
	}
}

func TestImageSequenceDimensionMismatch(t *testing.T) {
	loader := func(path string) (*clip.Frame, error) {
		if path == "bad" {
			return clip.NewFrame(4, 4, clip.RGB24), nil // wrong size
		}
		return clip.NewFrame(2, 2, clip.RGB24), nil
	}
	n, err := newImageSequence(loader, []string{"ok", "bad"},
		ImageSequenceOptions{FPS: clip.Rate{Num: 1, Den: 1}})
	if err != nil {
		t.Fatalf("seq: %v", err)
	}
	dst := clip.NewFrame(2, 2, clip.RGB24)
	err = n.FrameInto(context.Background(), clip.Time(1e9), dst) // selects "bad"
	if !errors.Is(err, ErrInconsistentSequence) {
		t.Errorf("err = %v, want ErrInconsistentSequence on dimension mismatch", err)
	}
}
