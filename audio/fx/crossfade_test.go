package audiofx_test

import (
	"math"
	"testing"
	"time"

	audiofx "github.com/mowshon/moviego/v2/audio/fx"
)

// TestEqualPowerFadeInEndpoints checks the sqrt ramp starts at 0, hits ~0.707 at
// the midpoint, and reaches 1.
func TestEqualPowerFadeInEndpoints(t *testing.T) {
	f := audiofx.EqualPowerFadeIn(&constClip{val: 1, dur: time.Second}, time.Second)
	buf := pull(t, f, 0, sr)
	if buf.Samples[0] != 0 {
		t.Errorf("start gain = %f, want 0", buf.Samples[0])
	}
	if mid := buf.Samples[sr/2]; math.Abs(float64(mid)-math.Sqrt(0.5)) > 0.01 {
		t.Errorf("midpoint gain = %f, want ~0.707", mid)
	}
	if last := buf.Samples[sr-1]; last < 0.99 {
		t.Errorf("end gain = %f, want ~1", last)
	}
}

// TestEqualPowerCrossfadeConstantPower is the perceptual point of the curve: at
// the midpoint the summed power of the faded-out tail and faded-in head stays at
// unity, where a linear pair would dip to 0.5 (~3 dB).
func TestEqualPowerCrossfadeConstantPower(t *testing.T) {
	out := audiofx.EqualPowerFadeOut(&constClip{val: 1, dur: time.Second}, time.Second)
	in := audiofx.EqualPowerFadeIn(&constClip{val: 1, dur: time.Second}, time.Second)
	bo := pull(t, out, 0, sr)
	bi := pull(t, in, 0, sr)
	go2 := float64(bo.Samples[sr/2])
	gi2 := float64(bi.Samples[sr/2])
	power := go2*go2 + gi2*gi2
	if math.Abs(power-1) > 0.02 {
		t.Errorf("summed power at midpoint = %f, want ~1.0", power)
	}
}
