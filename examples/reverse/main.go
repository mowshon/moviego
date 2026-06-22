// Command reverse demonstrates backward playback and the boomerang loop.
//
// It downloads a sample video on first run, then builds two clips: a plain
// reverse of a short cut, and a boomerang (forward then backward) of another.
// Both read their source non-monotonically, so mgo.Describe reports the
// sequential engine — the export installs a backward-buffered reader so each
// reverse step stays cheap. It prints each report and writes two MP4s.
package main

import (
	"context"
	"fmt"
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

	source, err := mgo.OpenVideo(videoPath)
	if err != nil {
		log.Fatal(err)
	}
	defer source.Close()

	// Play a two-second cut backward, audio included.
	reversed := source.Subclip(0, 2*time.Second).Reverse()

	// A one-second cut that plays forward then backward as a seamless loop.
	boomerang := source.Subclip(2*time.Second, 3*time.Second).Boomerang()

	clips := []struct {
		name string
		v    *mgo.Video
	}{
		{"reverse", reversed},
		{"boomerang", boomerang},
	}
	for _, c := range clips {
		if c.v.Err() != nil {
			log.Fatal(c.v.Err())
		}
		rep, err := mgo.Describe(c.v, mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("describe %-9s: engine=%s class=%s frames=%d duration=%v\n",
			c.name, rep.Engine, rep.Class, rep.Frames, c.v.Duration())
	}

	for _, c := range clips {
		out, err := assets.OutputPath(c.name + ".mp4")
		if err != nil {
			log.Fatal(err)
		}
		if err := mgo.WriteVideo(ctx, c.v, out, mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}}); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", out)
	}
}
