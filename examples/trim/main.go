// Command trim demonstrates the core read -> trim -> transform -> write flow.
//
// It downloads a small public sample video into examples/input on first run,
// trims a window, scales it down, and fades it in.
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

	source, err := assets.Ensure(ctx, assets.SampleVideo)
	if err != nil {
		log.Fatal(err)
	}

	v, err := mgo.OpenVideo(source)
	if err != nil {
		log.Fatal(err)
	}
	defer v.Close()

	// Keep the 1s-4s window, halve the size, and fade in over 1s. Subclip's end
	// is optional: v.Subclip(mgo.Sec(1)) would run from 1s to the end of the clip.
	clip := v.Subclip(mgo.Sec(1), mgo.Sec(4)).
		Resize(0.5).
		FadeIn(time.Second)
	if clip.Err() != nil {
		log.Fatal(clip.Err())
	}

	out, err := assets.OutputPath("trim.mp4")
	if err != nil {
		log.Fatal(err)
	}
	if err := mgo.WriteVideo(ctx, clip, out, mgo.ExportOptions{}); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", out)
}
