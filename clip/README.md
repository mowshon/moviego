# clip — Foundation types

Package `clip` is the bottom of the MovieGo dependency graph. It defines the
vocabulary every other package shares and imports nothing else in the module,
so it can be imported freely without cycles.

## Types at a glance

| Type | Purpose |
|---|---|
| `Clip` | Timeline-metadata contract (start / duration / end + mutators) |
| `Time` | `time.Duration` alias — all timestamps and durations |
| `Infinite` | Sentinel for unbounded / unknown length |
| `Rate{Num, Den}` | Rational frame rate (never `float64`) |
| `Size{W, H}` | Pixel dimensions |
| `Frame` | Packed pixel buffer (RGB24 / RGBA / Gray8) |
| `MaskF32` | High-precision float32 alpha mask (upgrade from Gray8 when needed) |
| `AudioBuffer` | Interleaved float32 samples, reused across chunks |
| `FramePool` | Recycled frame buffers keyed by byte length |
| `RenderedFrame` | What the pipeline moves: RGB frame + optional alpha sidecar + index |

Sentinel errors: `ErrNoDuration`, `ErrNoRate`, `ErrClosed`, `ErrEOF`,
`ErrVideoCorrupted`. `Wrap(op, err)` annotates them while preserving the chain
for `errors.Is`.

---

## Clip interface

```go
type Clip interface {
    Start() Time
    Duration() Time  // Infinite when length is unknown or unbounded
    End() Time       // Infinite when length is unknown or unbounded

    WithStart(Time) Clip
    WithDuration(Time) Clip
    WithEnd(end Time, changeDuration bool) Clip

    Close() error    // idempotent
}
```

`Duration()` returns `Infinite` (not a separate `ok` bool) when the clip has
no fixed length. Use `Finite(d)` to test:

```go
if clip.Finite(v.Duration()) {
    fmt.Println("known length:", v.Duration())
}
```

`DurationOr` and `EndOr` collapse the common `(duration, hasDuration)` pattern
into the sentinel so node implementations stay concise:

```go
func (n *myNode) Duration() Time { return clip.DurationOr(n.dur, n.hasDur) }
func (n *myNode) End() Time      { return clip.EndOr(n.Start(), n.dur, n.hasDur) }
```

`EndOr` guards against `start + Infinite` overflow for unbounded clips.

---

## Time and ParseTime

`Time` is `time.Duration`, so any `time` constant works directly:

```go
start := 30 * time.Second
dur   := 2500 * time.Millisecond
```

`ParseTime` accepts the same formats as MoviePy's `convert_to_seconds`:

```
"33.5"          → 33.5 s
"1:33,5"        → 1 min 33.5 s   (comma decimal allowed)
"01:01:33.045"  → 1 h 1 min 33.045 s
```

---

## Rate — rational frame rate

**Never use `float64` fps inside the library.** `float64` cannot represent
30000/1001 exactly and drifts on long clips.

```go
ntsc := clip.Rate{Num: 30000, Den: 1001}  // 29.97 fps

t := ntsc.FrameTime(30)       // exact timestamp of frame 30 → 1.001 s
i := ntsc.TimeToFrame(t)      // back to frame index        → 30
```

`FrameTime` and `TimeToFrame` use `big.Int` internally when the intermediate
products would overflow int64 (roughly > 85 hours at 29.97 fps), so they stay
exact on any realistic clip length.

`TimeToFrame` mirrors MoviePy's `int(fps*t + 1e-5)` — the tiny epsilon prevents
a frame's own timestamp from rounding back to the previous frame index.

Converting from a probe or float source:

```go
r := clip.SnapNTSC(29.97)      // → Rate{30000, 1001}  (NTSC snap)
r  = clip.SnapNTSC(25.0)       // → Rate{25, 1}        (exact integer, no snap)
r  = clip.RateFromFloat(23.98) // → Rate{23980, 1000}  (thousandths approximation)
```

`MaxRate(a, b)` picks the higher rate by exact cross-multiplication.

**Precondition:** `Num > 0` and `Den > 0`. A zero or negative component causes a
division panic. Use `ErrNoRate` at clip-construction time to reject invalid input
before it reaches these functions.

---

## Frame and FramePool

`Frame` carries a packed pixel buffer plus metadata:

```go
f := clip.NewFrame(1920, 1080, clip.RGB24)
// f.Pix is len 1920*1080*3 = 6220800 bytes
// f.Stride = 1920*3
```

Supported formats:

| Constant | Bytes/px | Use |
|---|---|---|
| `RGB24` | 3 | Main video frame |
| `RGBA` | 4 | Transparent video (packed alpha) |
| `Gray8` | 1 | Default alpha mask (sidecar) |

**Single-owner contract.** A frame has exactly one owner at any time. The
pipeline moves frames through channels; the encode stage releases them after
writing. Never hand out a reference to an internal buffer — always copy into
the caller's `dst`.

`FramePool` recycles buffers by byte length so renders avoid repeated
allocation:

```go
pool := clip.NewFramePool()

f := pool.Get(1920, 1080, clip.RGB24)  // borrows or allocates
// ... fill f.Pix ...
f.Release()                             // returns to pool (idempotent)

g := pool.Get(1920, 1080, clip.RGB24)  // reuses f's buffer
```

`Release` is a no-op on unpooled frames and idempotent (double-release is safe
because the pool link is cleared on first return). `Put` and `Release` check
ownership: a frame from pool A cannot be accidentally returned to pool B.

---

## MaskF32

The common alpha path uses `Gray8` (1 byte per pixel, 0–255). Upgrade to
`MaskF32` only where 8-bit banding is visible:

```go
m := clip.NewMaskF32(1920, 1080)
// m.Val is []float32, stride = width
```

`MaskF32` is kept out of `Frame` so the common code path never pays for it.

---

## AudioBuffer

Interleaved float32 samples (`len = Count * Channels`). Reuse across chunks
by calling `Reset` rather than allocating:

```go
buf := clip.NewAudioBuffer(1024, 2)  // 1024 stereo frames

// on the next chunk:
buf.Reset(1024, 2)  // reuses backing slice when large enough
```

---

## SampleTime / SampleIndex

Exact inverse pair for mapping between a sample-frame index and a timestamp.
Designed so a chunked audio render can compute contiguous, gap-free chunk
boundaries:

```go
const sr = 44100
const chunkSize = 1024

for k := 0; ; k++ {
    startIdx := k * chunkSize
    startTime := clip.SampleTime(startIdx, sr)  // ceil(idx * 1e9 / sr)
    // request samples [startIdx, startIdx+chunkSize) from the audio clip
    // using startTime as the "at" parameter
}
```

`SampleIndex(SampleTime(idx, sr), sr) == idx` for all `idx >= 0` (the round-
trip guarantee). For negative `idx`, results are implementation-defined.

`SampleIndex` floors (toward −∞), not truncates, so a clip placed at a
fractional or sub-sample negative offset gates correctly:

```go
clip.SampleIndex(-1, 48000)  // → -1, not 0
```

---

## RenderedFrame

The struct the pipeline passes between stages:

```go
type RenderedFrame struct {
    RGB   *Frame  // always non-nil
    Alpha *Frame  // Gray8 sidecar; nil means fully opaque
    Index int     // frame index in the output sequence
}
```

`Alpha == nil` is the fast (opaque) path. Composite and encode stages check it
to decide between cheap and transparent blend paths.
