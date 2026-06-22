package mgo

import (
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/composite"
)

// TestPlacementSurvivesEffectChain verifies the review fix: a Position/Layer set
// mid-chain is carried through later effects and transforms, so a fluent
// `clip.Position(Center).FadeIn(...)` does not silently drop the placement.
func TestPlacementSurvivesEffectChain(t *testing.T) {
	v := Color(20, 20, [3]byte{255, 0, 0}).
		WithDuration(time.Second).
		Position(Center).
		Layer(7).
		FadeIn(200 * time.Millisecond). // a TransformNode-producing effect
		Resize(0.5).                    // an eager static remap
		WithStart(100 * time.Millisecond)
	if v.err != nil {
		t.Fatalf("build: %v", v.err)
	}
	if v.pos.Keyword != composite.PosCenter {
		t.Errorf("position keyword = %v, want Center (lost across the chain)", v.pos.Keyword)
	}
	if v.layer != 7 {
		t.Errorf("layer = %d, want 7 (lost across the chain)", v.layer)
	}
}

// TestPlacementDefaultsClear confirms a clip with no placement keeps the zero
// position keyword and layer 0. (Position holds a func field, so it is compared
// field-wise rather than with ==.)
func TestPlacementDefaultsClear(t *testing.T) {
	v := Color(8, 8, [3]byte{}).WithDuration(time.Second)
	if v.pos.Keyword != composite.PosNone || v.pos.X != 0 || v.pos.Y != 0 || v.layer != 0 {
		t.Errorf("default placement = %+v layer %d, want zero", v.pos, v.layer)
	}
}
