// Command concat joins clips end to end.
//
// It downloads public video and photo media into examples/input on first run,
// then combines two video trims and one still-photo segment into one timeline.
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
	photoPath, err := assets.Ensure(ctx, assets.PhotoDesk)
	if err != nil {
		log.Fatal(err)
	}

	source, err := mgo.OpenVideo(videoPath)
	if err != nil {
		log.Fatal(err)
	}
	defer source.Close()

	photo, err := mgo.Image(photoPath)
	if err != nil {
		log.Fatal(err)
	}

	intro := source.Subclip(0, 2*time.Second).ResizeTo(640, 360).FadeOut(300 * time.Millisecond)
	still := photo.WithDuration(2*time.Second).ResizeTo(640, 360).FadeIn(300 * time.Millisecond)
	outro := source.Subclip(mgo.Sec(3), mgo.Sec(5)).ResizeTo(640, 360).FadeIn(300 * time.Millisecond)

	final := mgo.Concat(intro, still, outro)
	if final.Err() != nil {
		log.Fatal(final.Err())
	}

	out, err := assets.OutputPath("concat.mp4")
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
