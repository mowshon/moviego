package composite_test

import (
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite"
)

// benchComposite measures one RenderInto of a 640x360 opaque composite with a
// solid background and the given number of half-size overlays, the §14
// "static-over-video" / "multi-overlay" workloads. It is FFmpeg-free so it runs
// on any host.
func benchComposite(b *testing.B, overlays int) {
	size := clip.Size{W: 640, H: 360}
	children := []composite.CompositeChild{
		{Clip: solid(size.W, size.H, [3]byte{10, 10, 10}, time.Second)},
	}
	for i := 0; i < overlays; i++ {
		children = append(children, composite.CompositeChild{
			Clip:  solid(size.W/2, size.H/2, [3]byte{byte(i * 20), 100, 200}, time.Second),
			Pos:   composite.Position{X: float64(i * 8), Y: float64(i * 8)},
			Layer: i,
		})
	}
	n := composite.New(children, composite.Options{Size: size})
	dst := clip.NewFrame(size.W, size.H, clip.RGB24)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := n.RenderInto(ctx, 0, dst, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompositeStaticOverlay(b *testing.B) { benchComposite(b, 1) }
func BenchmarkCompositeMultiOverlay(b *testing.B)  { benchComposite(b, 8) }
