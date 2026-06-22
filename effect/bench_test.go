package effect_test

import (
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/video"
)

// BenchmarkResizeCropFadeChain measures rendering one frame through a
// resize -> crop -> fade chain (the §14 "resize/crop/fade chain" workload),
// FFmpeg-free over a generated color source.
func BenchmarkResizeCropFadeChain(b *testing.B) {
	base := video.NewColor(clip.Size{W: 1280, H: 720}, [3]byte{20, 40, 60}).
		WithDuration(time.Second).(video.VideoClip)

	v, err := effect.Resize{Scale: 0.5}.ApplyVideo(base) // -> 640x360
	if err != nil {
		b.Fatal(err)
	}
	if v, err = (effect.Crop{X: 0, Y: 0, W: 480, H: 270}).ApplyVideo(v); err != nil {
		b.Fatal(err)
	}
	if v, err = (effect.FadeIn{Dur: 300 * time.Millisecond}).ApplyVideo(v); err != nil {
		b.Fatal(err)
	}

	size := v.Size()
	dst := clip.NewFrame(size.W, size.H, clip.RGB24)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := v.FrameInto(ctx, 100*time.Millisecond, dst); err != nil {
			b.Fatal(err)
		}
	}
}

// benchEffect renders one 1280x720 frame through eff repeatedly, the shape used
// to track the hot pixel loops as new effects land.
func benchEffect(b *testing.B, eff effect.VideoEffect) {
	b.Helper()
	base := video.NewColor(clip.Size{W: 1280, H: 720}, [3]byte{40, 120, 200}).
		WithDuration(time.Second).(video.VideoClip)
	v, err := eff.ApplyVideo(base)
	if err != nil {
		b.Fatal(err)
	}
	size := v.Size()
	dst := clip.NewFrame(size.W, size.H, clip.RGB24)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := v.FrameInto(ctx, 0, dst); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBrightnessLUT measures the per-channel LUT color path (three table
// reads per pixel).
func BenchmarkBrightnessLUT(b *testing.B) { benchEffect(b, effect.Brightness{Delta: 0.1}) }

// BenchmarkSaturation measures the channel-mixing color path (per-pixel luma).
func BenchmarkSaturation(b *testing.B) { benchEffect(b, effect.Saturation{Amount: 1.3}) }

// BenchmarkGaussianBlur measures the separable two-pass blur.
func BenchmarkGaussianBlur(b *testing.B) { benchEffect(b, effect.GaussianBlur{Radius: 3}) }
