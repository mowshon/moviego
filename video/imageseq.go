package video

import (
	"context"
	"errors"
	"sort"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/cache"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/imagex"
)

// ErrEmptySequence reports an image sequence built from no images.
var ErrEmptySequence = errors.New("image sequence: no images")

// ErrDurationCount reports that the per-image Durations slice does not have one
// entry per image, which would leave some images without a defined interval.
var ErrDurationCount = errors.New("image sequence: Durations length must match image count")

// ErrInconsistentSequence reports an image whose size or pixel format differs
// from the sequence's first image. Every image must share one shape and one
// transparency, so the mask/RGB split is well-defined for every frame.
var ErrInconsistentSequence = errors.New("image sequence: images must share size and format")

// defaultSeqCache bounds the decoded-image LRU for disk-backed sequences.
const defaultSeqCache = 16

// ImageSequenceOptions configures the timing of an image sequence. Set FPS for
// equal-duration frames (each image shows for 1/FPS), or Durations for a custom
// per-image duration. CacheSize bounds the decoded-image LRU (disk mode only).
type ImageSequenceOptions struct {
	FPS       clip.Rate
	Durations []clip.Time
	CacheSize int
}

// ImageSequenceNode plays an ordered set of images as a video clip. The active
// image for an output time is found by binary search over rational start times,
// with the boundary belonging to the next image. Disk-backed sequences decode
// through a bounded LRU instead of re-reading the file on every frame (MoviePy
// has no such cache). It is parallel-safe and AccessStatic: every frame is an
// independent read.
type ImageSequenceNode struct {
	paths  []string      // disk mode (nil in memory mode)
	frames []*clip.Frame // memory mode (nil in disk mode)
	load   func(string) (*clip.Frame, error)
	cache  *cache.LRU[int, *clip.Frame]

	starts []clip.Time // start time of each image; len == image count
	total  clip.Time
	size   clip.Size
	format clip.PixelFormat // every image must match this (RGBA when mask, else RGB24)
	mask   bool

	start   clip.Time
	dur     clip.Time
	rate    clip.Rate
	hasRate bool
}

// NewImageSequence builds a disk-backed sequence from image file paths, probing
// the first image for the common size and transparency.
func NewImageSequence(paths []string, opts ImageSequenceOptions) (*ImageSequenceNode, error) {
	return newImageSequence(imagex.DecodeFile, paths, opts)
}

// newImageSequence is the injectable-loader form used by tests to count reads.
func newImageSequence(load func(string) (*clip.Frame, error), paths []string, opts ImageSequenceOptions) (*ImageSequenceNode, error) {
	if len(paths) == 0 {
		return nil, clip.Wrap("image sequence", ErrEmptySequence)
	}
	if err := validateTiming(len(paths), opts); err != nil {
		return nil, err
	}
	first, err := load(paths[0])
	if err != nil {
		return nil, err
	}
	capacity := opts.CacheSize
	if capacity <= 0 {
		capacity = defaultSeqCache
	}
	n := &ImageSequenceNode{
		paths:  paths,
		load:   load,
		cache:  cache.NewLRU[int, *clip.Frame](capacity),
		size:   clip.Size{W: first.W, H: first.H},
		format: first.Format,
		mask:   first.Format == clip.RGBA,
	}
	n.cache.Put(0, first)
	n.applyTiming(len(paths), opts)
	return n, nil
}

// NewImageSequenceFrames builds an in-memory sequence from decoded frames. All
// frames must share the first frame's size and pixel format.
func NewImageSequenceFrames(frames []*clip.Frame, opts ImageSequenceOptions) (*ImageSequenceNode, error) {
	if len(frames) == 0 {
		return nil, clip.Wrap("image sequence", ErrEmptySequence)
	}
	if err := validateTiming(len(frames), opts); err != nil {
		return nil, err
	}
	size := clip.Size{W: frames[0].W, H: frames[0].H}
	format := frames[0].Format
	for _, f := range frames {
		if f.W != size.W || f.H != size.H || f.Format != format {
			return nil, clip.Wrap("image sequence", ErrInconsistentSequence)
		}
	}
	n := &ImageSequenceNode{
		frames: frames,
		size:   size,
		format: format,
		mask:   format == clip.RGBA,
	}
	n.applyTiming(len(frames), opts)
	return n, nil
}

// validateTiming rejects a Durations slice (the active timing mode when FPS is
// unset) whose length does not match the image count.
func validateTiming(count int, opts ImageSequenceOptions) error {
	if opts.FPS.Num <= 0 && len(opts.Durations) > 0 && len(opts.Durations) != count {
		return clip.Wrap("image sequence", ErrDurationCount)
	}
	return nil
}

// applyTiming computes per-image start times and the total duration. FPS mode
// gives each image FrameTime(1) and sets the clip rate; Durations mode uses the
// cumulative sums. With neither, images each last one second.
func (n *ImageSequenceNode) applyTiming(count int, opts ImageSequenceOptions) {
	n.starts = make([]clip.Time, count)
	switch {
	case opts.FPS.Num > 0:
		for i := 0; i < count; i++ {
			n.starts[i] = opts.FPS.FrameTime(i)
		}
		n.total = opts.FPS.FrameTime(count)
		n.rate, n.hasRate = opts.FPS, true
	case len(opts.Durations) > 0:
		var acc clip.Time
		for i := 0; i < count; i++ {
			n.starts[i] = acc
			if i < len(opts.Durations) {
				acc += opts.Durations[i]
			}
		}
		n.total = acc
	default:
		for i := 0; i < count; i++ {
			n.starts[i] = clip.Time(i) * clip.Time(1e9)
		}
		n.total = clip.Time(count) * clip.Time(1e9)
	}
	n.dur = n.total
}

// indexAt returns the image index active at local time t: the last image whose
// start is <= t, with the boundary belonging to the next image.
func (n *ImageSequenceNode) indexAt(t clip.Time) int {
	// First index whose start is strictly greater than t, minus one.
	i := sort.Search(len(n.starts), func(i int) bool { return n.starts[i] > t }) - 1
	if i < 0 {
		return 0
	}
	if i >= len(n.starts) {
		return len(n.starts) - 1
	}
	return i
}

// imageAt returns the decoded frame for index idx, reading through the LRU in
// disk mode. The returned frame is owned by the node/cache; callers copy out of
// it and never mutate it.
func (n *ImageSequenceNode) imageAt(idx int) (*clip.Frame, error) {
	if n.frames != nil {
		return n.frames[idx], nil
	}
	if f, ok := n.cache.Get(idx); ok {
		return f, nil
	}
	f, err := n.load(n.paths[idx])
	if err != nil {
		return nil, err
	}
	// Every image must match the first image's size and format so the RGB/mask
	// split is well-defined; a heterogeneous frame is surfaced, never copied
	// through a mismatched buffer.
	if f.W != n.size.W || f.H != n.size.H || f.Format != n.format {
		return nil, clip.Wrap("image sequence", ErrInconsistentSequence)
	}
	n.cache.Put(idx, f)
	return f, nil
}

// Timeline metadata.

func (n *ImageSequenceNode) Start() clip.Time    { return n.start }
func (n *ImageSequenceNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.dur > 0) }
func (n *ImageSequenceNode) End() clip.Time      { return clip.EndOr(n.start, n.dur, n.dur > 0) }

func (n *ImageSequenceNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	return &c
}

func (n *ImageSequenceNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur = d
	return &c
}

func (n *ImageSequenceNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur = end - c.start
	} else if c.dur > 0 {
		c.start = end - c.dur
	}
	return &c
}

// Video metadata.

func (n *ImageSequenceNode) Size() clip.Size           { return n.size }
func (n *ImageSequenceNode) Rate() (clip.Rate, bool)   { return n.rate, n.hasRate }
func (n *ImageSequenceNode) HasMask() bool             { return n.mask }
func (n *ImageSequenceNode) Audio() audio.AudioClip    { return nil }
func (n *ImageSequenceNode) ParallelSafe() bool        { return true }
func (n *ImageSequenceNode) SourceAccess() AccessClass { return AccessStatic }

// Rendering copies the active image into the caller's destination, splitting
// alpha into the mask sidecar when the sequence is transparent.

func (n *ImageSequenceNode) RenderInto(_ context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	f, err := n.imageAt(n.indexAt(t))
	if err != nil {
		return false, err
	}
	if f.Format == clip.RGBA {
		if alphaDst != nil {
			if err := imagex.SplitAlphaInto(rgbDst, alphaDst, f); err != nil {
				return false, err
			}
			return true, nil
		}
		// Caller wants RGB only: drop the alpha channel.
		return false, dropAlphaInto(rgbDst, f)
	}
	copyPixels(rgbDst, f)
	return false, nil
}

func (n *ImageSequenceNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	_, err := n.RenderInto(ctx, t, dst, nil)
	return err
}

func (n *ImageSequenceNode) MaskInto(_ context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	if !n.mask {
		return false, nil
	}
	f, err := n.imageAt(n.indexAt(t))
	if err != nil || f.Format != clip.RGBA {
		return false, err
	}
	for y := 0; y < f.H; y++ {
		s := f.Pix[y*f.Stride:]
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < f.W; x++ {
			d[x] = s[x*4+3]
		}
	}
	return true, nil
}

// Close releases nothing: an image sequence owns only decoded buffers.
func (n *ImageSequenceNode) Close() error { return nil }

// dropAlphaInto copies the color channels of an RGBA frame into an RGB24
// destination, discarding alpha.
func dropAlphaInto(rgbDst, rgba *clip.Frame) error {
	if rgbDst.Format != clip.RGB24 || rgbDst.W != rgba.W || rgbDst.H != rgba.H {
		return clip.Wrap("image sequence", clip.ErrVideoCorrupted)
	}
	for y := 0; y < rgba.H; y++ {
		s := rgba.Pix[y*rgba.Stride:]
		d := rgbDst.Pix[y*rgbDst.Stride:]
		for x := 0; x < rgba.W; x++ {
			d[x*3], d[x*3+1], d[x*3+2] = s[x*4], s[x*4+1], s[x*4+2]
		}
	}
	return nil
}
