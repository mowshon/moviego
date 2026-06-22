package render

import (
	"context"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// runSequential renders the plan one frame at a time on the calling goroutine
// and forwards each frame to sink in output order. It is the correctness oracle
// and the engine for random-access graphs. A random-access or bounded-access
// graph reads its sources non-monotonically, so it installs a backward-buffered
// frame provider: file nodes serve their reads from it through the
// frameProviderFrom seam instead of restarting their own decoder on every
// backward step. Linear and static graphs install nothing and keep the
// interactive own-decoder path.
func runSequential(ctx context.Context, root video.VideoClip, plan *Plan, sink frameSink) error {
	rctx := ctx
	if (plan.Class == ClassRandom || plan.Class == ClassBounded) && len(plan.sources) > 0 {
		prov, err := newReverseProvider(ctx, plan.sources)
		if err != nil {
			return err
		}
		defer prov.close()
		rctx = video.WithFrameProvider(ctx, prov)
	}
	_, err := video.IterFramesAt(rctx, root, plan.Rate, func(rf clip.RenderedFrame) error {
		return sink.writeFrame(rf)
	})
	return err
}
