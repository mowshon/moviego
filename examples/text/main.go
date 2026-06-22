// Command text overlays a rendered title card on a video.
//
// It downloads a public video and the Inter font into examples/input on first
// run, then composites a stroked, centered title over the clip's first five
// seconds. The title is a static text clip (rasterized once) carrying an alpha
// mask, so it blends straight over the footage.
package main

import (
	"context"
	"image/color"
	"log"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/examples/internal/assets"
)

func main() {
	ctx := context.Background()

	videoPath, err := assets.Ensure(ctx, assets.SampleVideo)
	if err != nil {
		log.Fatal(err)
	}
	fontPath, err := assets.Ensure(ctx, assets.FontInter)
	if err != nil {
		log.Fatal(err)
	}

	bg, err := mgo.OpenVideo(videoPath)
	if err != nil {
		log.Fatal(err)
	}
	defer bg.Close()

	const d = 5 * time.Second
	background := bg.Subclip(0, d).ResizeTo(640, 360)

	// A centered title that fades in, plus a smaller caption pinned lower.
	title, err := mgo.Text("moviego", mgo.TextOptions{
		FontPath:    fontPath,
		FontSize:    72,
		Color:       color.White,
		Stroke:      color.RGBA{R: 8, G: 12, B: 18, A: 255},
		StrokeWidth: 3,
	})
	if err != nil {
		log.Fatal(err)
	}
	title = title.WithDuration(d).FadeIn(time.Second).Position(mgo.Center).Layer(10)

	tagline, err := mgo.Text("scripted video editing in Go", mgo.TextOptions{
		FontPath: fontPath,
		FontSize: 28,
		Color:    color.RGBA{R: 220, G: 230, B: 240, A: 255},
	})
	if err != nil {
		log.Fatal(err)
	}
	tagline = tagline.WithDuration(d).Position(mgo.RelPos(0.5, 0.78)).Layer(11)

	final := mgo.CompositeWith(mgo.CompositeOptions{
		Size: mgo.Size{W: 640, H: 360},
	}, background, title, tagline)
	if final.Err() != nil {
		log.Fatal(final.Err())
	}

	out, err := assets.OutputPath("text.mp4")
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
