// Command slideshow builds a video from an ordered set of images.
//
// It downloads three same-size public photos into examples/input on first run
// and plays each for one second via an image sequence.
package main

import (
	"context"
	"log"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/examples/internal/assets"
)

func main() {
	ctx := context.Background()

	paths, err := assets.EnsureAll(ctx,
		assets.PhotoLake,
		assets.PhotoDesk,
		assets.PhotoForest,
	)
	if err != nil {
		log.Fatal(err)
	}

	// Show each image for one second (1 image per second).
	clip, err := mgo.ImageSequence(paths, mgo.Rate{Num: 1, Den: 1})
	if err != nil {
		log.Fatal(err)
	}

	out, err := assets.OutputPath("slideshow.mp4")
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
