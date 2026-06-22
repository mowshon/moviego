// Command transparent exports a composite with a real alpha channel.
//
// It downloads a public transparent PNG into examples/input on first run,
// composites it over a transparent canvas, and writes a QuickTime .mov
// (lossless qtrle), the portable default alpha container.
//
// For a web-friendly transparent .mp4 on macOS, swap the ExportOptions for
// `mgo.ExportOptions{Codec: mgo.CodecHEVCAlpha}` and a ".mp4" output path.
package main

import (
	"context"
	"log"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/examples/internal/assets"
)

func main() {
	ctx := context.Background()

	overlayPath, err := assets.Ensure(ctx, assets.TransparentPNG)
	if err != nil {
		log.Fatal(err)
	}

	overlay, err := mgo.Image(overlayPath)
	if err != nil {
		log.Fatal(err)
	}
	overlay = overlay.WithDuration(3*time.Second).
		ResizeTo(220, 165).
		Position(mgo.Center)

	// Transparent keeps the PNG alpha through the composite and export.
	final := mgo.CompositeWith(mgo.CompositeOptions{
		Size:        mgo.Size{W: 640, H: 360},
		Transparent: true,
	}, overlay)
	if final.Err() != nil {
		log.Fatal(final.Err())
	}

	// Export to .mov; qtrle is chosen automatically for the alpha container.
	out, err := assets.OutputPath("transparent.mov")
	if err != nil {
		log.Fatal(err)
	}
	if err := mgo.WriteVideo(ctx, final, out, mgo.ExportOptions{
		Rate: mgo.Rate{Num: 30, Den: 1},
	}); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", out)
}
