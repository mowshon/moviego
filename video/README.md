# package video

The `video` package defines the `VideoClip` interface and all concrete video
graph nodes. It sits one layer above the foundation (`clip/`) and one layer
below the facade (`mgo/`). Its job is to describe the clip graph — decoders
are created at export time, not here.

## VideoClip interface

Every node in the graph implements `VideoClip`:

```go
type VideoClip interface {
    clip.Clip                                  // Start / Duration / End / With*

    RenderInto(ctx, t, rgbDst, alphaDst) (hasAlpha bool, err error)
    FrameInto(ctx, t, dst) error               // opaque-only convenience
    MaskInto(ctx, t, dst) (bool, error)        // alpha-only convenience

    Size() clip.Size
    Rate() (clip.Rate, bool)
    HasMask() bool
    Audio() audio.AudioClip

    ParallelSafe() bool       // safe to call RenderInto concurrently for different t?
    SourceAccess() AccessClass
}
```

**`RenderInto` contract** — fills `rgbDst` (always) and `alphaDst` (when the clip is
transparent and alphaDst is non-nil). Returns `hasAlpha=false` and leaves
alphaDst untouched when the frame is fully opaque. Callers own both
destination buffers; nodes must copy into them, never hand out internal
references.

**Scheduling hints** — `ParallelSafe()` and `SourceAccess()` let the render
planner choose between the parallel pipeline and the sequential oracle engine.

## AccessClass

```go
const (
    AccessStatic  // no time dependence (Image, Color)
    AccessLinear  // output i maps monotonically to a source frame (file passthrough, Subclip, Speed)
    AccessBounded // reserved
    AccessRandom  // arbitrary source reads (Loop, Reverse, negative TimeRemap segments)
)
```

AccessRandom forces the sequential engine. AccessLinear + all children
parallel-safe → pipeline engine.

## Source nodes

### VideoFileNode

Backed by a media file. Decoded lazily via an FFmpeg subprocess, one decoder
per node instance. Placement copies (`WithStart`/`WithDuration`/`WithEnd`)
each get their own independent decoder.

```go
n, err := video.OpenFile(ctx, "clip.mp4")
```

- **Not parallel-safe** — the decoder is sequential; the render pipeline
  drives it from a single goroutine.
- **`Size()` is post-rotation** — the display-matrix rotation is applied by
  the Go decoder; the fused FFmpeg path declines a rotated source and falls
  back to Go rendering.
- **Context-bound decoder** — if a different context is passed between calls,
  the decoder is closed and reopened under the new context. This ensures
  cancellation always stops the FFmpeg process.
- **Last-frame cache** — a repeated index-identical read is served from a
  one-entry buffer without re-reading the decoder.

Implements `FileSource` (for the render planner) and `FilterSource` (fusion
advertisement — declines when `rotation != 0`).

### ImageNode

Decodes an image file once at construction; serves every frame from the
in-memory buffer. Parallel-safe, `AccessStatic`.

```go
n, err := video.OpenImage("photo.png")     // disk
n  = video.NewImage(rgbFrame, alphaFrame)  // pre-decoded
```

`MapStatic` applies a pure geometry transform once and returns a new
`ImageNode`, so effects like Resize on a static source skip per-frame work
entirely.

### ColorNode

Solid-color fill, precomputed at construction. Parallel-safe, `AccessStatic`.

```go
n := video.NewColor(clip.Size{W: 1920, H: 1080}, [3]byte{0, 0, 0})
```

`MapStatic` applies a geometry transform once; the result is an `ImageNode`
because a transformed solid is not guaranteed to remain uniform.

### ImageSequenceNode

Ordered set of images played as a clip. The active image at time `t` is found
by binary search over start times (last-wins at a boundary). Parallel-safe,
`AccessStatic`.

```go
// Equal-duration frames at 24fps
n, err := video.NewImageSequence(paths, video.ImageSequenceOptions{
    FPS: clip.Rate{Num: 24, Den: 1},
})

// Per-image durations
n, err := video.NewImageSequence(paths, video.ImageSequenceOptions{
    Durations: []clip.Time{2 * time.Second, 1 * time.Second, 3 * time.Second},
})

// In-memory frames
n, err := video.NewImageSequenceFrames(frames, opts)
```

Disk-backed sequences decode through a bounded LRU (`CacheSize`, default 16)
so a cache hit skips the file read. All images must share the first frame's
size and pixel format; a mismatch surfaces as `ErrInconsistentSequence`.

## Transform nodes

### TimeTransformNode

Maps output time to source time without touching pixels. Constructors:

| Constructor | Access | Audio |
|---|---|---|
| `Subclip(inner, a, b)` | Linear | trimmed to same window |
| `MultiplySpeed(inner, factor)` | Linear | `audio.Speed` |
| `Loop(inner, n)` | Random | `audio.Loop` |
| `TimeRemap(inner, pts, ease)` | Linear or Random | `audio.TimeRemap` |
| `Freeze(inner, at, hold)` | Linear | `audio.TimeRemap` |
| `FreezeStart(inner, hold)` | Linear | same |
| `FreezeEnd(inner, hold)` | Linear | same |
| `Reverse(inner)` | Random | `audio.Reverse` |

**Subclip bounds** — negative values count back from the source end;
`b == 0` means "to the end". The window is clamped into `[0, srcDur]` and
one rational tick of overshoot is tolerated before clamping. When the source
duration is unknown, relative bounds fall back to passthrough (duration stays
unknown).

**`TimeTransformNode.Close()` is a no-op** — a trim wrapper does not own the
inner clip. The original source's `Close` is the caller's responsibility.

### TransformNode

Per-frame pixel transform; optionally changes the output size (resize, crop,
rotate) or only the colors (fade). The inner-size scratch buffer is
pool-managed so concurrent calls reuse buffers instead of allocating per-frame.

```go
n := video.NewTransform(inner, outSize, rgbFn, maskFn)
n  = n.WithFilter(filterFn) // optional FFmpeg fusion advertisement
```

`rgbFn(t, dst, src)` fills the output RGB frame from the inner-size source
frame. `maskFn` does the same for Gray8 alpha; when nil, the mask passes
through unchanged (valid only for size-preserving transforms). The node
inherits `ParallelSafe()` from the inner clip.

## Audio sidecar

`audioAttachNode` replaces a clip's audio sidecar without touching pixels:

```go
v = video.WithAudio(v, audioClip)   // attach / replace
v = video.WithoutAudio(v)           // strip
```

`With*` timeline mutations propagate to both the video inner clip and the
attached audio so placement stays in sync.

## Planner support

### FileSource

`VideoFileNode` implements `FileSource`; the render planner walks the graph
via `WalkSources` to enumerate distinct sources (deduped by `SourceKey`) and
installs one sequential decoder per source.

```go
video.WalkSources(root, func(fs video.FileSource) { /* ... */ })
```

### FrameProvider

During a pipelined render the planner installs a `FrameProvider` on the
context so file nodes read from the shared sequential decoder rather than
seeking their own:

```go
ctx = video.WithFrameProvider(ctx, provider)
```

Interactive callers never install a provider; each node uses its own decoder.

### Filterable / FilterSource

Opt-in FFmpeg fusion advertisement. A node returns `ok=false` from `Filter`
or `FilterInput` when it cannot be expressed as a filter (rotated source,
partial-timeline effect, Loop, Reverse). The planner falls back to Go
rendering for the whole graph.

## Frame iteration

`IterFrames` and `IterFramesAt` render a clip to a callback frame by frame,
managing pool buffers automatically:

```go
n, err := video.IterFrames(ctx, v, func(rf clip.RenderedFrame) error {
    // rf.RGB is a *clip.Frame (RGB24); rf.Alpha is non-nil when transparent
    // do not retain rf.RGB or rf.Alpha past this call
    return nil
})
```

`IterFramesAt` accepts an explicit output rate, overriding the clip's own rate.
Both functions stop early at EOF and return the number of frames delivered.

## video/io — FFmpeg subprocess boundary

### Decoder

Reads packed `rgb24` frames from a file in sequential order. Owned by one
goroutine; not concurrent-safe.

```go
dec, err := videoio.OpenDecoder(ctx, path, videoio.DecoderOptions{
    Size: clip.Size{W: 1280, H: 720},
    Rate: clip.Rate{Num: 25, Den: 1},
})
defer dec.Close()

dec.SeekToFrame(idx) // no-op / skip / restart per the state table
dec.ReadInto(dst)    // fills dst, advances pos
```

**Seek state machine** (pinned by golden tests in `video/io`):
- `idx == pos` → no-op
- `idx > pos` and `idx ≤ pos + 100` → skip frames into scratch
- otherwise → restart FFmpeg with a two-pass `-ss` (coarse before `-i`,
  precise after, 1 µs epsilon bias)

### Encoder

Feeds raw frames over stdin to FFmpeg; writes an encoded file.

```go
enc, err := videoio.OpenEncoder(ctx, "out.mp4", videoio.EncoderOptions{
    Size:  clip.Size{W: 1920, H: 1080},
    Rate:  clip.Rate{Num: 30, Den: 1},
    Codec: "libx264",
    CRF:   23,
})
defer enc.Close() // flushes and finalizes

enc.WriteFrame(frame)
```

**Alpha export** — set `Transparent: true` and let the encoder pick the codec
from the output extension (`.mov` → `qtrle`, `.mkv` → `ffv1`), or name an
explicit alpha codec. The encoder validates codec+pixfmt against the FFmpeg
build before launch — alpha is never silently dropped.

## Design invariants to preserve

- **Copy into caller's buffer** — `RenderInto`/`FrameInto`/`MaskInto` must
  never hand out a reference to an internal frame.
- **Placement copies are independent** — each `WithStart`/`WithDuration`/`WithEnd`
  on `VideoFileNode` creates a new reader pointer (independent decoder).
- **Mask sync** — geometry transforms must apply identically to the Gray8 mask
  (`Sidecars.Mask = true`), or transparency desyncs from picture.
- **Audio sync** — time transforms (`Subclip`, `MultiplySpeed`, `Loop`,
  `TimeRemap`, `Reverse`) must remap the audio sidecar by the same function.
- **`ParallelSafe()` must be conservative** — return false if the node has
  any shared mutable state that isn't protected by a concurrency-safe type.
  Silent non-determinism breaks the `parallel == sequential` CI invariant.
