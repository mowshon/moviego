package text

import (
	"context"
	"image/color"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
)

// sumAlpha renders the node at t and returns the total alpha coverage, a proxy
// for "how much text is visible."
func sumAlpha(t *testing.T, n interface {
	Size() clip.Size
	RenderInto(context.Context, clip.Time, *clip.Frame, *clip.Frame) (bool, error)
}, at clip.Time,
) int {
	t.Helper()
	sz := n.Size()
	rgb := clip.NewFrame(sz.W, sz.H, clip.RGB24)
	alpha := clip.NewFrame(sz.W, sz.H, clip.Gray8)
	if _, err := n.RenderInto(context.Background(), at, rgb, alpha); err != nil {
		t.Fatalf("render at %v: %v", at, err)
	}
	total := 0
	for _, b := range alpha.Pix {
		total += int(b)
	}
	return total
}

func animOpts() Options {
	return Options{Text: "ABCDE", FontSize: 40, Color: color.White}
}

func TestAnimatedTextEmptyContentErrors(t *testing.T) {
	if _, err := NewAnimated(Options{FontSize: 20}, TextAnim{}); err == nil {
		t.Fatal("expected an error for empty animated text")
	}
}

func TestAnimatedFadeRampsOpacity(t *testing.T) {
	n, err := NewAnimated(animOpts(), TextAnim{Type: AnimFadeIn, Dur: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	start := sumAlpha(t, n, 0)
	end := sumAlpha(t, n, time.Second)
	if start != 0 {
		t.Errorf("fade-in alpha at t=0 = %d, want 0 (fully transparent)", start)
	}
	if end == 0 {
		t.Error("fade-in alpha at end = 0, want the text fully visible")
	}
	if mid := sumAlpha(t, n, 500*time.Millisecond); mid <= 0 || mid >= end {
		t.Errorf("fade-in alpha at midpoint = %d, want between 0 and %d", mid, end)
	}
}

func TestAnimatedTypewriterReveals(t *testing.T) {
	n, err := NewAnimated(animOpts(), TextAnim{Type: AnimTypewriter, Dur: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	start := sumAlpha(t, n, 0)
	mid := sumAlpha(t, n, 500*time.Millisecond)
	end := sumAlpha(t, n, time.Second)
	if start != 0 {
		t.Errorf("typewriter alpha at t=0 = %d, want 0 (nothing revealed)", start)
	}
	if !(mid > 0 && mid < end) {
		t.Errorf("typewriter reveal not monotonic: start=%d mid=%d end=%d", start, mid, end)
	}
}

func TestAnimatedCustomOverridesType(t *testing.T) {
	// A custom curve that is fully hidden until the very end.
	n, err := NewAnimated(animOpts(), TextAnim{
		Dur: time.Second,
		Custom: func(p float64) AnimState {
			return AnimState{Reveal: 1, Alpha: p * p, Scale: 1}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := sumAlpha(t, n, 0); got != 0 {
		t.Errorf("custom alpha at t=0 = %d, want 0", got)
	}
	if got := sumAlpha(t, n, time.Second); got == 0 {
		t.Error("custom alpha at end = 0, want visible")
	}
}

func TestAnimatedSlideNeverClips(t *testing.T) {
	// A slide moves the text across the frame; the canvas must carry enough
	// headroom that the full text stays visible at every point of the entrance,
	// not just at rest. (A canvas sized exactly to the text clips the moving text
	// at its edge.)
	for _, k := range []AnimKind{AnimSlideUp, AnimSlideDown, AnimSlideLeft, AnimSlideRight} {
		n, err := NewAnimated(animOpts(), TextAnim{Type: k, Dur: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		rest := sumAlpha(t, n, time.Second)
		for _, at := range []clip.Time{0, clip.Time(250 * time.Millisecond), clip.Time(500 * time.Millisecond)} {
			if got := sumAlpha(t, n, at); got != rest {
				t.Errorf("slide kind %d at %v: visible alpha = %d, want %d (no clipping)", k, at, got, rest)
			}
		}
	}
}

func TestAnimatedTextDurationAndSize(t *testing.T) {
	n, err := NewAnimated(animOpts(), TextAnim{Type: AnimSlideUp, Dur: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if n.Duration() != 2*time.Second {
		t.Errorf("duration = %v, want 2s", n.Duration())
	}
	if sz := n.Size(); sz.W <= 0 || sz.H <= 0 {
		t.Errorf("size = %+v, want positive", sz)
	}
	// Extending the duration holds the text past the animation.
	held := n.WithDuration(5 * time.Second).(*AnimatedTextNode)
	if held.Duration() != 5*time.Second {
		t.Errorf("held duration = %v, want 5s", held.Duration())
	}
}
