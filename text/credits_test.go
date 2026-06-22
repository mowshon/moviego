package text

import (
	"testing"

	"github.com/mowshon/moviego/v2/clip"
)

func TestCreditsValidation(t *testing.T) {
	if _, err := NewCredits(nil, CreditsOptions{Size: clip.Size{W: 100, H: 100}}); err == nil {
		t.Error("expected an error for no lines")
	}
	if _, err := NewCredits([]string{"x"}, CreditsOptions{}); err == nil {
		t.Error("expected an error for zero viewport size")
	}
}

func TestCreditsScrollsThroughViewport(t *testing.T) {
	n, err := NewCredits(
		[]string{"Directed by", "A. Director", "Produced by", "B. Producer"},
		CreditsOptions{Size: clip.Size{W: 320, H: 180}, FontSize: 24, Speed: 80},
	)
	if err != nil {
		t.Fatal(err)
	}
	if n.Duration() <= 0 {
		t.Fatalf("duration = %v, want positive", n.Duration())
	}
	if got := n.Size(); got != (clip.Size{W: 320, H: 180}) {
		t.Errorf("size = %+v, want the viewport", got)
	}
	// At the start the block sits just below the frame (not yet visible); halfway
	// through it is scrolling across.
	if start := sumAlpha(t, n, 0); start != 0 {
		t.Errorf("alpha at t=0 = %d, want 0 (block still below frame)", start)
	}
	if mid := sumAlpha(t, n, n.Duration()/2); mid == 0 {
		t.Error("alpha at midpoint = 0, want text scrolling through the viewport")
	}
}
