package composite_test

import (
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite"
	"github.com/mowshon/moviego/v2/video"
)

// TestConcatChainSelectsByTime: a same-size chain plays each child in its own
// time window and reports the summed duration.
func TestConcatChainSelectsByTime(t *testing.T) {
	red := solid(4, 4, [3]byte{255, 0, 0}, time.Second)
	green := solid(4, 4, [3]byte{0, 255, 0}, 2*time.Second)
	chain, err := composite.ConcatChain([]video.VideoClip{red, green})
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	if d := chain.Duration(); d != 3*time.Second {
		t.Errorf("duration = %v, want 3s", d)
	}
	// First window -> red.
	if got := pixel(chainFrame(t, chain, 500*time.Millisecond), 0, 0); got != [3]byte{255, 0, 0} {
		t.Errorf("t=0.5s -> %v, want red", got)
	}
	// Second window -> green.
	if got := pixel(chainFrame(t, chain, 2*time.Second), 0, 0); got != [3]byte{0, 255, 0} {
		t.Errorf("t=2s -> %v, want green", got)
	}
}

func chainFrame(t *testing.T, n video.VideoClip, at clip.Time) *clip.Frame {
	t.Helper()
	sz := n.Size()
	dst := clip.NewFrame(sz.W, sz.H, clip.RGB24)
	if err := n.FrameInto(context.Background(), at, dst); err != nil {
		t.Fatalf("render: %v", err)
	}
	return dst
}

// TestConcatChainSizeMismatch: chain rejects clips of differing sizes.
func TestConcatChainSizeMismatch(t *testing.T) {
	a := solid(4, 4, [3]byte{}, time.Second)
	b := solid(8, 8, [3]byte{}, time.Second)
	if _, err := composite.ConcatChain([]video.VideoClip{a, b}); err == nil {
		t.Fatal("expected size-mismatch error")
	}
}

// TestConcatChainMaskFillOpaque: when one child has a mask, the chain has a
// mask and a maskless child's frames are fully opaque.
func TestConcatChainMaskFillOpaque(t *testing.T) {
	plain := solid(2, 2, [3]byte{10, 10, 10}, time.Second)
	transp := masked(2, 2, [3]byte{20, 20, 20}, 128, time.Second)
	chain, err := composite.ConcatChain([]video.VideoClip{plain, transp})
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	if !chain.HasMask() {
		t.Fatal("chain with a masked child should have a mask")
	}
	mask := clip.NewFrame(2, 2, clip.Gray8)
	// First child (plain) -> opaque fill.
	ok, err := chain.MaskInto(context.Background(), 0, mask)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	if mask.Pix[0] != 255 {
		t.Errorf("maskless child alpha = %d, want 255", mask.Pix[0])
	}
	// Second child -> its own mask.
	if _, err := chain.MaskInto(context.Background(), time.Second, mask); err != nil {
		t.Fatalf("mask into 2: %v", err)
	}
	if mask.Pix[0] != 128 {
		t.Errorf("masked child alpha = %d, want 128", mask.Pix[0])
	}
}

// TestConcatComposeCentersAndSizes: a compose-concat sizes the canvas to the
// largest child and centers each child, placing them at cumulative starts.
func TestConcatComposeCentersAndSizes(t *testing.T) {
	small := solid(2, 2, [3]byte{255, 0, 0}, time.Second)
	big := solid(4, 4, [3]byte{0, 255, 0}, time.Second)
	comp, err := composite.ConcatCompose([]video.VideoClip{small, big}, composite.ConcatOptions{})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if comp.Size() != (clip.Size{W: 4, H: 4}) {
		t.Fatalf("canvas = %v, want 4x4", comp.Size())
	}
	if d := comp.Duration(); d != 2*time.Second {
		t.Errorf("duration = %v, want 2s", d)
	}
	// At t=0 the small red clip is centered on the 4x4 black canvas: center
	// pixel red, corner black.
	f := chainFrame(t, comp, 0)
	if got := pixel(f, 1, 1); got != [3]byte{255, 0, 0} {
		t.Errorf("center = %v, want red", got)
	}
	if got := pixel(f, 0, 0); got != [3]byte{0, 0, 0} {
		t.Errorf("corner = %v, want black", got)
	}
}

// TestConcatComposeNegativePadding overlaps consecutive clips so the total
// duration is less than the sum of durations.
func TestConcatComposeNegativePadding(t *testing.T) {
	a := solid(4, 4, [3]byte{}, 2*time.Second)
	b := solid(4, 4, [3]byte{}, 2*time.Second)
	comp, err := composite.ConcatCompose([]video.VideoClip{a, b}, composite.ConcatOptions{Padding: -time.Second})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	// starts: a@0 (end 2s), b@(2-1)=1s (end 3s). Latest end = 3s.
	if d := comp.Duration(); d != 3*time.Second {
		t.Errorf("overlapped duration = %v, want 3s", d)
	}
}
