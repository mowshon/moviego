// Command still-image turns a static image into a video clip.
//
// It downloads a public photo into examples/input on first run, then shows it
// for three seconds, scaled to 640x360 and fading out over the last second.
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

	imgPath, err := assets.Ensure(ctx, assets.PhotoLake)
	if err != nil {
		log.Fatal(err)
	}

	img, err := mgo.Image(imgPath)
	if err != nil {
		log.Fatal(err)
	}
	clip := img.WithDuration(3*time.Second).
		ResizeTo(640, 360).
		FadeOut(time.Second)
	if clip.Err() != nil {
		log.Fatal(clip.Err())
	}

	out, err := assets.OutputPath("still-image.mp4")
	if err != nil {
		log.Fatal(err)
	}
	if err := mgo.WriteVideo(ctx, clip, out, mgo.ExportOptions{
		Rate: mgo.Rate{Num: 30, Den: 1},
	}); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", out)
}
