// Command transitions demonstrates moviego's transition system: one timeline
// whose clips are joined by a different transition on every boundary.
//
// It downloads a sample video and a few public photos into examples/input on
// first run, then stitches them with the fluent mgo.NewSequence builder, walking
// through every built-in transition (crossfade, dissolve, wipe, slide, push,
// fade-through-black, iris in/out), a decorator (an eased wipe), and a custom
// transition (Diagonal, defined in this file) applied through mgo.UseTransition.
//
// The same transition can also be applied to every boundary at once with
// mgo.ConcatWith, the other entry point:
//
//	v := mgo.ConcatWith(mgo.ConcatOptions{
//	    Transition: transition.CrossFade{},
//	    Duration:   xd,
//	    AudioCurve: mgo.CrossfadeEqualPower,
//	}, a, b, c)
package main

import (
	"context"
	"log"
	"time"

	mgo "github.com/mowshon/moviego/v2"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ease"
	"github.com/mowshon/moviego/v2/examples/internal/assets"
	"github.com/mowshon/moviego/v2/transition"
)

const (
	w   = 640
	h   = 360
	seg = 2 * time.Second
	xd  = 600 * time.Millisecond // transition (overlap) duration
)

func main() {
	ctx := context.Background()

	clips, closeAll, err := loadClips(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer closeAll()

	// One step per boundary, in order — every built-in transition, a decorator,
	// and a custom one. len(steps) is len(clips)-1.
	steps := []mgo.TransitionStep{
		mgo.Crossfade(xd),
		mgo.Dissolve(xd),
		mgo.WipeLeft(xd),
		mgo.SlideRight(xd),
		mgo.PushDown(xd),
		mgo.FadeThroughBlack(xd),
		mgo.IrisOpen(xd),
		mgo.IrisClose(xd),
		// Decorator: any transition can be eased without a second implementation.
		mgo.UseTransition(transition.Eased(transition.Wipe{Dir: transition.Up}, ease.EaseInOut), xd),
		// Custom transition, defined in this program (see Diagonal below) and
		// applied through the generic step — no moviego changes required.
		mgo.UseTransition(Diagonal{Softness: 0.15}, xd),
	}

	seq := mgo.NewSequence().Add(clips[0])
	for i, step := range steps {
		seq = seq.Then(step).Add(clips[i+1])
	}
	final := seq.Video()
	if final.Err() != nil {
		log.Fatal(final.Err())
	}

	// Describe reports the engine the planner chose. A transition timeline whose
	// clips keep straight-through bodies renders on the sequential engine: a
	// clip's body and its transition read disjoint windows of the same decoder.
	if report, err := mgo.Describe(final, mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}}); err == nil {
		log.Printf("engine=%v frames=%d size=%dx%d", report.Engine, report.Frames, report.Size.W, report.Size.H)
	}

	out, err := assets.OutputPath("transitions.mp4")
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

// loadClips fetches the demo media and returns 11 same-size clips, alternating
// between video segments and stills so adjacent clips differ visually. The
// returned closer releases the opened video sources.
func loadClips(ctx context.Context) ([]*mgo.Video, func(), error) {
	videoPath, err := assets.Ensure(ctx, assets.SampleVideo)
	if err != nil {
		return nil, nil, err
	}
	source, err := mgo.OpenVideo(videoPath)
	if err != nil {
		return nil, nil, err
	}

	photo := func(a assets.Asset) (*mgo.Video, error) {
		p, err := assets.Ensure(ctx, a)
		if err != nil {
			return nil, err
		}
		return mgo.Image(p)
	}
	lake, err := photo(assets.PhotoLake)
	if err != nil {
		source.Close()
		return nil, nil, err
	}
	forest, err := photo(assets.PhotoForest)
	if err != nil {
		source.Close()
		return nil, nil, err
	}
	city, err := photo(assets.PhotoCity)
	if err != nil {
		source.Close()
		return nil, nil, err
	}
	desk, err := photo(assets.PhotoDesk)
	if err != nil {
		source.Close()
		return nil, nil, err
	}

	// Each still is reused with an independent handle; the video is sliced into
	// two windows within its first 5s (the sample's known length). Every clip is
	// resized to the common canvas — the assembler requires a uniform size.
	vid := func(start, end time.Duration) *mgo.Video {
		return source.Subclip(mgo.Time(start), mgo.Time(end)).ResizeTo(w, h)
	}
	still := func(v *mgo.Video) *mgo.Video { return v.WithDuration(seg).ResizeTo(w, h) }

	// Alternate across five distinct sources so no two adjacent clips repeat.
	clips := []*mgo.Video{
		vid(0, seg),
		still(lake),
		still(forest),
		still(city),
		still(desk),
		vid(3*time.Second, 5*time.Second),
		still(lake),
		still(forest),
		still(city),
		still(desk),
		vid(0, seg),
	}
	return clips, func() { source.Close() }, nil
}

// Diagonal is a custom transition that lives in this program, not in moviego: it
// reveals B behind a 45° edge sweeping from the top-left corner. Implementing the
// one-method transition.Transition interface is all it takes, and mgo.UseTransition
// applies it exactly like a built-in. It also implements the optional
// transition.MaskTransition, so it is exact over transparent clips too.
type Diagonal struct {
	// Softness feathers the edge as a fraction of the diagonal; 0 is a hard edge.
	Softness float64
}

func (Diagonal) Name() string { return "diagonal" }

func (d Diagonal) Frame(p float64, dst, a, b *clip.Frame)     { d.blend(p, dst, a, b, 3) }
func (d Diagonal) FrameMask(p float64, dst, a, b *clip.Frame) { d.blend(p, dst, a, b, 1) }

// blend mixes A→B along the top-left→bottom-right diagonal. bpp is 3 for the RGB
// frame and 1 for the alpha sidecar, so both follow the identical edge.
func (d Diagonal) blend(p float64, dst, a, b *clip.Frame, bpp int) {
	soft := d.Softness
	if soft < 0 {
		soft = 0
	}
	// Expand the sweep by the feather width so p=0 is fully A and p=1 fully B.
	edge := p*(1+soft) - soft/2
	for y := 0; y < dst.H; y++ {
		drow := dst.Pix[y*dst.Stride:]
		arow := a.Pix[y*a.Stride:]
		brow := b.Pix[y*b.Stride:]
		fy := (float64(y) + 0.5) / float64(dst.H)
		for x := 0; x < dst.W; x++ {
			pos := ((float64(x)+0.5)/float64(dst.W) + fy) / 2 // diagonal coord in [0,1]
			t := 1.0
			if soft <= 0 {
				if edge < pos {
					t = 0
				}
			} else {
				t = clamp01((edge-pos)/soft + 0.5)
			}
			o := x * bpp
			for c := 0; c < bpp; c++ {
				drow[o+c] = byte(float64(brow[o+c])*t + float64(arow[o+c])*(1-t) + 0.5)
			}
		}
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
