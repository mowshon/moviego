// Command reframe demonstrates probing a file, describing the render plan
// without rendering, the built-in progress bar, the reframe effects (flip,
// pad/fit, blur, sharpen), single frame extraction, and PNG frame-sequence
// export.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/examples/internal/assets"
	"github.com/mowshon/moviego/v2/imagex"
)

func main() {
	ctx := context.Background()

	source, err := assets.Ensure(ctx, assets.SampleVideo)
	if err != nil {
		log.Fatal(err)
	}

	// Probe: metadata without building a graph.
	info, err := mgo.Probe(source)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("probe: %dx%d (raw %dx%d) %s %s %.2ffps dur=%s alpha=%v\n",
		info.Size.W, info.Size.H, info.RawSize.W, info.RawSize.H,
		info.Codec, info.PixFmt, info.Rate.Float(), info.Duration.Round(1e6), info.HasAlpha)

	// Build a reframed clip: trim, fit to a square with letterbox bars, mirror,
	// then soften. Pad/FitTo change size; FlipH/Blur are same-size PixelFn effects.
	v, err := mgo.OpenVideo(source)
	if err != nil {
		log.Fatal(err)
	}
	defer v.Close()
	clip := v.Subclip(mgo.Sec(0), mgo.Sec(2)).
		FitTo(360, 360).
		FlipH().
		Blur(2)

	// Describe: which engine will run, fused or not, how many frames.
	rep, err := mgo.Describe(clip, mgo.ExportOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("describe: engine=%s class=%s frames=%d size=%dx%d fused=%v\n",
		rep.Engine, rep.Class, rep.Frames, rep.Size.W, rep.Size.H, rep.Fused)

	// Extract a single still and write it as a PNG.
	still, err := mgo.ExtractFrame(ctx, source, mgo.Sec(1))
	if err != nil {
		log.Fatal(err)
	}
	stillPath, err := assets.OutputPath("reframe_still.png")
	if err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(stillPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := imagex.EncodePNG(f, imagex.FromImage(still)); err != nil {
		log.Fatal(err)
	}
	f.Close()
	log.Printf("wrote still %s", stillPath)

	// Export the reframed clip with the built-in progress bar.
	out, err := assets.OutputPath("reframe_out.mp4")
	if err != nil {
		log.Fatal(err)
	}
	if err := mgo.WriteVideo(ctx, clip, out, mgo.ExportOptions{
		Progress: &mgo.BarProgress{Label: "reframe"},
	}); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", out)

	// Export the first second as a numbered PNG sequence (2 fps).
	framesDir, err := assets.OutputPath("reframe_frames")
	if err != nil {
		log.Fatal(err)
	}
	count, err := mgo.WriteFrameSequence(ctx, clip.Subclip(0, mgo.Sec(1)), framesDir, mgo.FrameSequenceOptions{
		Rate: mgo.Rate{Num: 2, Den: 1},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d frames to %s", count, framesDir)
}
