// Command colorgrade demonstrates moviego's color-grading and image effects:
// brightness/contrast/saturation/gamma, HSL, color balance, a 3D .cube LUT, a
// separable Gaussian blur, a vignette, and an arbitrary-angle rotation.
//
// It downloads a sample video into examples/input on first run, writes a small
// "teal & orange" .cube LUT next to it, then builds a graded clip, prints the
// render plan with mgo.Describe, exports a still PNG and a short MP4.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/examples/internal/assets"
)

func main() {
	ctx := context.Background()

	source, err := assets.Ensure(ctx, assets.SampleVideo)
	if err != nil {
		log.Fatal(err)
	}

	lutPath, err := writeWarmCube()
	if err != nil {
		log.Fatal(err)
	}

	v, err := mgo.OpenVideo(source)
	if err != nil {
		log.Fatal(err)
	}
	defer v.Close()

	// A film-style grade: lift contrast and warmth, then a LUT look, a gentle
	// Gaussian softening and a vignette to draw the eye to the center. The color
	// ops, the LUT and GaussianBlur all advertise FFmpeg filters (eq/colorbalance/
	// hue/lut3d/gblur), so a straight chain can fuse; Vignette has no faithful
	// filter and renders on the Go path (and so keeps this chain Go-side).
	graded := v.Subclip(mgo.Sec(0), mgo.Sec(3)).
		Brightness(0.04).
		Contrast(1.15).
		Saturation(1.2).
		Gamma(1.1).
		Fx(effect.ColorBalance{Mids: [3]float64{0.05, 0, -0.06}}).
		LUT(lutPath).
		GaussianBlur(0.8).
		Vignette(0.5)

	rep, err := mgo.Describe(graded, mgo.ExportOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("describe: engine=%s class=%s frames=%d size=%dx%d fused=%v\n",
		rep.Engine, rep.Class, rep.Frames, rep.Size.W, rep.Size.H, rep.Fused)

	// Export the graded clip.
	out, err := assets.OutputPath("colorgrade_out.mp4")
	if err != nil {
		log.Fatal(err)
	}
	if err := mgo.WriteVideo(ctx, graded, out, mgo.ExportOptions{
		Progress: &mgo.BarProgress{Label: "colorgrade"},
	}); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", out)

	// Export a couple of graded stills as a numbered PNG sequence.
	framesDir, err := assets.OutputPath("colorgrade_frames")
	if err != nil {
		log.Fatal(err)
	}
	count, err := mgo.WriteFrameSequence(ctx, graded.Subclip(0, mgo.Sec(1)), framesDir, mgo.FrameSequenceOptions{
		Rate: mgo.Rate{Num: 2, Den: 1},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d graded stills to %s", count, framesDir)

	// A short clip showing arbitrary-angle rotation (canvas grows to fit).
	rotated := v.Subclip(mgo.Sec(1), mgo.Sec(2)).Rotate(12)
	rotOut, err := assets.OutputPath("colorgrade_rotated.mp4")
	if err != nil {
		log.Fatal(err)
	}
	if err := mgo.WriteVideo(ctx, rotated, rotOut, mgo.ExportOptions{}); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", rotOut)
}

// writeWarmCube writes a tiny 2³ "teal shadows / orange highlights" .cube LUT
// into the examples input directory and returns its path. A 2³ identity is the
// eight RGB cube corners; nudging them toward teal at the dark end and orange at
// the bright end yields a recognizable cinematic look under trilinear sampling.
func writeWarmCube() (string, error) {
	dir, err := assets.InputDir()
	if err != nil {
		return "", err
	}
	path := dir + "/teal_orange.cube"
	const body = `TITLE "teal-orange"
LUT_3D_SIZE 2
DOMAIN_MIN 0 0 0
DOMAIN_MAX 1 1 1
0.00 0.05 0.10
1.00 0.05 0.10
0.00 1.00 0.10
1.00 1.00 0.10
0.00 0.05 1.00
1.00 0.05 1.00
0.00 1.00 1.00
1.00 0.90 0.80
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
