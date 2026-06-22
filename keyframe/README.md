# Keyframes & Time Remapping

This documents moviego's keyframe feature set: keyframe animation of clip
properties (opacity, scale, position) and variable-speed time remapping (speed
ramps, slow-motion, freeze frames).

It is split into two layers:

- The **`keyframe` package** (this folder) is the small, dependency-free engine
  that interpolates a scalar value along time-stamped keyframes, reusing the
  [`ease`](../ease) package for the curve. It is what every animated property is
  built on.
- The **`mgo` facade** exposes the user-facing methods (`Animate`,
  `AnimatePosition`, `TimeRemap`, `Freeze*`) so common work needs only
  `import mgo "github.com/mowshon/moviego/v2"`.

The runnable demo lives in [`examples/keyframes`](../examples/keyframes).

---

## Table of contents

1. [Concepts](#concepts)
2. [Quick start](#quick-start)
3. [Property animation — `Animate`](#property-animation--animate)
4. [Position animation — `AnimatePosition`](#position-animation--animateposition)
5. [Time remapping — `TimeRemap`](#time-remapping--timeremap)
6. [Freeze frames — `Freeze` / `FreezeStart` / `FreezeEnd`](#freeze-frames)
7. [Using the `keyframe` package directly](#using-the-keyframe-package-directly)
8. [Writing custom animated effects](#writing-custom-animated-effects)
9. [Requirements & settings](#requirements--settings)
10. [Errors](#errors)
11. [Full example](#full-example)
12. [How it renders (performance)](#how-it-renders-performance)

---

## Concepts

### Keyframe

A **keyframe** pins a value to a moment in time:

```go
type Keyframe struct {
    At  Time    // clip-local time (a time.Duration; build with mgo.Sec / mgo.ParseTime)
    Val float64 // the property value at that time
}
```

### Track

A **track** is a sorted list of keyframes plus an easing curve. Between two
keyframes the value is linearly interpolated, with the normalized progress
shaped by the curve. Outside the keyframes the value is **clamped** (held at the
first/last value) — never extrapolated.

### Clip-local time

All keyframe times (`At`) are **relative to the clip's own playback**, not the
composition timeline. A clip placed at 2s in a `Composite` whose first keyframe
is `At: 0` begins animating when *that clip* starts, not at the composition's 2s
mark. This matches `PositionFunc` and the rest of moviego.

### Easing curves

Curves come from the [`ease`](../ease) package, re-exported on the facade:

| Curve | Shape |
|---|---|
| `mgo.Linear` | constant rate (identity) |
| `mgo.EaseIn` | slow start, fast finish (`p²`) |
| `mgo.EaseOut` | fast start, slow finish |
| `mgo.EaseInOut` | accelerate then decelerate |
| `mgo.EaseSmooth` | smoothstep S-curve, zero slope at both ends |

Passing `nil` as the easing argument defaults to `Linear`. Any
`func(float64) float64` works — see [custom easing](#custom-easing).

### Access class (why some remaps are slower)

Every node reports how it reads its source. A time remap whose source time only
ever moves **forward** (slow-mo, speed-up, freeze) stays `AccessLinear` and runs
on the fast **parallel pipeline**. A remap that **rewinds** the source (plays a
segment backward) is `AccessRandom` and drops to the **sequential engine** —
correct, but slower, because one forward decoder can't cheaply serve a backward
read. Use [`mgo.Describe`](../plan_api.go) to see which engine you got.

---

## Quick start

```go
package main

import (
    "context"
    "log"
    "time"

    mgo "github.com/mowshon/moviego/v2"
)

func main() {
    ctx := context.Background()

    src, err := mgo.OpenVideo("input.mp4")
    if err != nil {
        log.Fatal(err)
    }
    defer src.Close()

    clip := src.Subclip(0, 4*time.Second).
        // Ease into 2x slow motion over the first 2s, then run at normal speed.
        TimeRemap([]mgo.TimePoint{
            {Out: 0, In: 0},
            {Out: 2 * time.Second, In: time.Second},
            {Out: 3 * time.Second, In: 3 * time.Second},
        }, mgo.EaseInOut).
        // Hold the last frame for one second at the end.
        FreezeEnd(time.Second)

    if err := mgo.WriteVideo(ctx, clip, "out.mp4",
        mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}}); err != nil {
        log.Fatal(err)
    }
}
```

---

## Property animation — `Animate`

```go
func (v *Video) Animate(p Prop, keys []Keyframe, e EaseFunc) *Video
```

Keyframes a **scalar** clip property over clip-local time. `e` is the easing
curve (`nil` → `Linear`). An empty `keys` slice records an
[`ErrNoKeyframes`](#errors) build error.

| Property | Effect | Visible where |
|---|---|---|
| `mgo.PropOpacity` | clip alpha, `0` transparent → `1` opaque | only when **composited** (see note) |
| `mgo.PropScale` | uniform zoom about the frame center | standalone **and** composited |

### Opacity

```go
// Fade in over the first second, hold, then fade out over the last second.
overlay := logo.WithDuration(5 * time.Second).
    Animate(mgo.PropOpacity, []mgo.Keyframe{
        {At: 0, Val: 0},
        {At: time.Second, Val: 1},
        {At: 4 * time.Second, Val: 1},
        {At: 5 * time.Second, Val: 0},
    }, mgo.EaseInOut)

final := mgo.Composite(background, overlay)
```

`PropOpacity` attaches an alpha **mask** to the clip and scales any existing mask
by the keyframed value (clamped to `[0,1]`). Because a normal MP4 has no alpha
channel, the opacity is only *seen* when the clip is layered onto something with
[`Composite`](../composite_api.go). Animating opacity on a clip you export
directly is a no-op visually (the mask is discarded at encode) unless you export
with `Transparent: true`.

### Scale

```go
// A slow "Ken Burns" push from 1.0x to 1.25x across the whole clip.
hero := photo.WithDuration(6 * time.Second).
    Animate(mgo.PropScale, []mgo.Keyframe{
        {At: 0, Val: 1.0},
        {At: 6 * time.Second, Val: 1.25},
    }, mgo.EaseSmooth)
```

`PropScale` keeps the **frame size fixed** and resamples the content about the
center:

- `Val > 1` zooms **in** — content overflows and is cropped to the frame.
- `Val < 1` zooms **out** — content shrinks and is letterboxed (black for an
  opaque clip, transparent for one with a mask).
- `Val <= 0` yields an empty (cleared) frame.

Keeping the size fixed is deliberate: a per-frame-varying frame size cannot be
encoded and would break every downstream composite. For a one-shot static resize
use [`Resize`/`ResizeTo`](../video_api.go) instead.

---

## Position animation — `AnimatePosition`

```go
func (v *Video) AnimatePosition(keys []PositionKeyframe, e EaseFunc) *Video

type PositionKeyframe struct {
    At   Time
    X, Y float64 // absolute top-left pixel within the composite canvas
}
```

Animates a clip's **placement within a `Composite`** by interpolating `X` and
`Y` independently. It drives the same hook as
[`PositionFunc`](../video_api.go), so it is a **no-op for a standalone export** —
position only means something relative to a canvas.

```go
// Drift a caption diagonally across a 1920x1080 canvas.
caption := text.WithDuration(4 * time.Second).
    AnimatePosition([]mgo.PositionKeyframe{
        {At: 0, X: 100, Y: 100},
        {At: 4 * time.Second, X: 1400, Y: 800},
    }, mgo.EaseInOut)

final := mgo.Composite(video, caption)
```

An empty `keys` slice records an `ErrNoKeyframes` build error (symmetric with
`Animate`). Animations compose: a single clip can carry opacity **and** scale
**and** position at once — see the [full example](#full-example).

---

## Time remapping — `TimeRemap`

```go
func (v *Video) TimeRemap(points []TimePoint, e EaseFunc) *Video

type TimePoint struct {
    Out Time // time on the output (playback) timeline
    In  Time // source time shown at Out
}
```

Warps the clip onto a new timeline. Each `TimePoint` maps an output time to a
source time; between points the source advances (normal/slow/fast) or rewinds
(reverse), with the easing curve `e` applied inside each segment. The output
duration is the **last** point's `Out`.

```go
v.TimeRemap([]mgo.TimePoint{
    {Out: 0, In: 0},
    {Out: mgo.Sec(2), In: mgo.Sec(1)}, // first 2s of output = first 1s of source (2x slow-mo)
    {Out: mgo.Sec(3), In: mgo.Sec(5)}, // next 1s of output = source 1s→5s (4x fast-forward)
}, mgo.EaseSmooth)
```

**Rules** (violations record [`ErrTimeRemapPoints`](#errors)):

- at least **two** points,
- `Out` times **strictly increasing** and **non-negative** (points are sorted
  for you, so order in the slice does not matter),
- `In` times **non-negative**.

**Engine impact:** all-forward `In` keeps the clip on the parallel pipeline; any
segment where `In` decreases (a rewind) routes the whole clip to the sequential
engine. The audio sidecar is remapped by the same curve, so audio tracks the
video (a slowed segment stretches the audio, a held region sustains it).

> A reversing remap is *correct* but reads its source out of order, so it is
> slow on long clips: a backward step forces the decoder to re-seek.

---

## Freeze frames

```go
func (v *Video) Freeze(at, hold Time) *Video // hold the frame at `at` for `hold`, then resume
func (v *Video) FreezeStart(hold Time) *Video // hold frame 0 for `hold` before playing
func (v *Video) FreezeEnd(hold Time) *Video   // hold the last frame for `hold` after playing
```

Freezes are a special, common time remap and are **fast**: the held source time
never decreases, so the clip stays `AccessLinear` and pipeline-eligible, and the
forward decoder's last-frame cache serves the repeated reads for free.

```go
v.Freeze(mgo.Sec(4), mgo.Sec(2)) // play to 4s, hold that frame 2s, then continue
v.FreezeStart(mgo.Sec(1))        // 1s hold on frame 0, then play
v.FreezeEnd(mgo.Sec(1))          // play, then 1s hold on the last frame
```

Each adds `hold` to the clip's duration. They need a **known duration** and
return [`ErrNoDuration`](#errors) on an unbounded source. Audio is held with the
video across the freeze span (a sustained level); mute the audio separately if
that is undesired.

---

## Using the `keyframe` package directly

You rarely need this — the facade builds tracks for you — but the engine is
public and handy for custom effects or your own interpolation.

```go
import (
    "github.com/mowshon/moviego/v2/ease"
    "github.com/mowshon/moviego/v2/keyframe"
)

track := keyframe.New([]keyframe.Key{
    {At: 0, Val: 0},
    {At: time.Second, Val: 100},
    {At: 2 * time.Second, Val: 50},
}, ease.EaseInOut)

track.Eval(0)                    // 0   (clamped to first)
track.Eval(500 * time.Millisecond) // ~50 (eased toward 100)
track.Eval(3 * time.Second)      // 50  (clamped to last)
track.Empty()                    // false
```

API:

```go
type Key struct { At clip.Time; Val float64 }

func New(keys []Key, e ease.Func) Track  // copies + sorts the keys; nil ease → Linear
func (t Track) Eval(at clip.Time) float64 // clamps at the ends, eases between
func (t Track) Empty() bool
```

`New` copies and sorts the input, so the caller's slice is neither retained nor
required to be pre-sorted. A `Track` is immutable and `Eval` is a pure function,
so a `Track` is safe to share across the parallel render pipeline.

### Custom easing

`ease.Func` (aliased as `mgo.EaseFunc`) is just `func(float64) float64` mapping
progress `p∈[0,1]`. Keep `f(0)=0` and `f(1)=1` so it composes cleanly. Any
function works — pass it anywhere a curve is expected:

```go
// An overshooting "back" ease.
back := func(p float64) float64 {
    const s = 1.70158
    p -= 1
    return p*p*((s+1)*p+s) + 1
}

v.Animate(mgo.PropScale, []mgo.Keyframe{
    {At: 0, Val: 0.8},
    {At: 400 * time.Millisecond, Val: 1.0},
}, back)
```

---

## Writing custom animated effects

The built-in `effect.Opacity` and `effect.Scale` are ordinary `VideoEffect`
values driven by a `keyframe.Track`, so you can reach them through `Fx` with a
hand-built track:

```go
import "github.com/mowshon/moviego/v2/effect"

v.Fx(effect.Opacity{Track: keyframe.New([]keyframe.Key{
    {At: 0, Val: 0}, {At: time.Second, Val: 1},
}, ease.Linear)})
```

To animate a property moviego doesn't ship, write your own `VideoEffect` and let
a `keyframe.Track` supply the per-frame value. The pattern is: build a
`video.NewTransform` whose pixel function reads `track.Eval(t)`.

```go
package myfx

import (
    "github.com/mowshon/moviego/v2/clip"
    "github.com/mowshon/moviego/v2/effect"
    "github.com/mowshon/moviego/v2/keyframe"
    "github.com/mowshon/moviego/v2/video"
)

// AnimatedBrightness shifts every channel by a keyframed delta in [-1, 1].
type AnimatedBrightness struct {
    Track keyframe.Track
}

// Targets declares the sidecars touched (RGB only, here).
func (AnimatedBrightness) Targets() effect.EffectTargets {
    return effect.EffectTargets{Video: true}
}

func (b AnimatedBrightness) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
    if b.Track.Empty() {
        return nil, effect.ErrNoKeyframes
    }
    tr := b.Track
    rgbFn := func(t clip.Time, dst, src *clip.Frame) error {
        add := int(tr.Eval(t) * 255) // evaluate the track at this frame's time
        for y := 0; y < src.H; y++ {
            s := src.Pix[y*src.Stride:]
            d := dst.Pix[y*dst.Stride:]
            for x := 0; x < src.W*3; x++ {
                v := int(s[x]) + add
                if v < 0 {
                    v = 0
                } else if v > 255 {
                    v = 255
                }
                d[x] = byte(v)
            }
        }
        return nil
    }
    // Same-size transform: outSize == c.Size(); nil maskFn passes any mask through.
    return video.NewTransform(c, c.Size(), rgbFn, nil), nil
}
```

Apply it like any effect:

```go
v.Fx(myfx.AnimatedBrightness{Track: keyframe.New([]keyframe.Key{
    {At: 0, Val: -0.3},
    {At: 2 * time.Second, Val: 0.3},
}, ease.EaseInOut)})
```

Notes for custom effects:

- The `t` passed to the pixel function is **clip-local**, exactly what
  `track.Eval` expects.
- `dst` and `src` are RGB24 frames of the **same** size for a `NewTransform`
  with `outSize == c.Size()`; do not retain them past the call.
- To also animate the alpha sidecar, pass a non-nil `maskFn` (it receives Gray8
  frames). To *create* a mask on an opaque clip — as `PropOpacity` does — you
  need a node that reports `HasMask() == true`; see `effect/opacity.go` as a
  template.
- Set `Targets()` honestly: it is checked against the propagation matrix in
  `effect/propagation_test.go`.

---

## Requirements & settings

- **Export rate.** Generated/static sources (images, colors) and remaps need a
  frame rate at export time: `mgo.ExportOptions{Rate: mgo.Rate{Num: 30, Den: 1}}`.
- **Known duration.** `Freeze`, `FreezeStart`, and `FreezeEnd` need a clip with a
  known, finite duration. Give static sources one with `WithDuration` first.
- **Composite for opacity/position.** `PropOpacity` and `AnimatePosition` only
  affect a `Composite`; a standalone export ignores them (opacity needs an alpha
  channel — export with `Transparent: true` to keep it).
- **Build errors are deferred.** Like all moviego builders, these methods carry
  the first error on the handle. Check `v.Err()` or rely on `WriteVideo` /
  `mgo.Validate` to surface it; chains stay fluent.
- **Pre-flight with `Describe`.** `mgo.Describe(v, opts)` reports the chosen
  engine and access class without rendering — the quickest way to confirm a
  remap stayed on the pipeline.

---

## Errors

| Error | Cause |
|---|---|
| `effect.ErrNoKeyframes` | `Animate` / `AnimatePosition` with an empty keyframe list |
| `video.ErrTimeRemapPoints` | `TimeRemap` with < 2 points, non-increasing `Out`, or negative `Out`/`In` |
| `clip.ErrNoDuration` | `Freeze*` on a clip with unknown/unbounded duration |

All are returned via the handle's deferred error:

```go
v := src.Freeze(time.Second, time.Second)
if err := v.Err(); err != nil { /* handle */ }
```

---

## Full example

A complete program combining a speed ramp, a freeze, and a three-track animated
overlay (opacity + scale + position) lives in
[`examples/keyframes/main.go`](../examples/keyframes/main.go). Run it with:

```sh
go run ./examples/keyframes
```

It prints each clip's render report (engine/class/frames) via `mgo.Describe` and
writes `examples/output/keyframes.mp4`.

---

## How it renders (performance)

- **Keyframe `Animate`** changes pixels or the mask only; it does not touch
  timing, so opacity/scale/position stay pipeline-safe.
- **`TimeRemap`** and **`Freeze*`** reuse the existing `video.TimeTransformNode`.
  Forward maps are `AccessLinear` (parallel pipeline); a rewinding `TimeRemap` is
  `AccessRandom` (sequential engine). No remap advertises an FFmpeg filter, so
  they render on the Go path.
- **Freeze is cheap, reverse is not.** A held frame is served from the decoder's
  last-frame cache; a backward read re-seeks.

Confirm any of this for a given graph with `mgo.Describe(v, opts)`.
