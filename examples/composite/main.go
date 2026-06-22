// Command composite layers clips onto a canvas with positions and z-order.
//
// It downloads a public video and photo into examples/input on first run, then
// overlays the photo and a delayed picture-in-picture video on a canvas.
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

	videoPath, err := assets.Ensure(ctx, assets.SampleVideo)
	if err != nil {
		log.Fatal(err)
	}
	photoPath, err := assets.Ensure(ctx, assets.PhotoCity)
	if err != nil {
		log.Fatal(err)
	}

	bg, err := mgo.OpenVideo(videoPath)
	if err != nil {
		log.Fatal(err)
	}
	defer bg.Close()

	pip, err := mgo.OpenVideo(videoPath)
	if err != nil {
		log.Fatal(err)
	}
	defer pip.Close()

	card, err := mgo.Image(photoPath)
	if err != nil {
		log.Fatal(err)
	}

	const d = 5 * time.Second
	background := bg.Subclip(0, d).ResizeTo(640, 360)
	inset := pip.Subclip(mgo.Sec(1), mgo.Sec(4)).
		Resize(0.35).
		FadeIn(500 * time.Millisecond).
		WithStart(time.Second).
		Position(mgo.At(24, 24)).
		Layer(10)
	photo := card.WithDuration(d).
		ResizeTo(220, 124).
		FadeIn(500 * time.Millisecond).
		Position(mgo.At(396, 212)).
		Layer(20)

	final := mgo.CompositeWith(mgo.CompositeOptions{
		Size:    mgo.Size{W: 640, H: 360},
		BGColor: [3]byte{8, 12, 18},
	}, background, inset, photo)
	if final.Err() != nil {
		log.Fatal(final.Err())
	}

	out, err := assets.OutputPath("composite.mp4")
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
