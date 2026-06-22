# ease — easing curves for MovieGo

`ease` provides a small set of pure easing functions: they remap a normalized
progress value `p ∈ [0, 1]` to shape how an animation or transition
accelerates. Every built-in keeps the endpoints fixed (`f(0) = 0`, `f(1) = 1`)
so they compose without shifting a clip's start or end.

The package has no dependencies and is shared across the library:

- **Transitions** — `transition.Eased(t, e)` wraps any transition with an
  easing curve applied to the overlap progress.
- **Keyframe tracks** — `keyframe.New(keys, e)` shapes interpolation within
  each segment of a track.
- **Time remapping** — `mgo.TimeRemap(points, e)` eases the warp between
  control points.

## The `Func` type

```go
type Func func(float64) float64
```

An `ease.Func` is any function from progress to eased progress. The built-ins
cover the common cases; pass any `func(float64) float64` wherever a curve is
accepted.

## Built-in curves

| Name | Formula | Shape |
|---|---|---|
| `Linear` | `p` | constant rate, no easing |
| `EaseIn` | `p²` | slow start, fast finish |
| `EaseOut` | `p(2−p)` | fast start, slow finish |
| `EaseInOut` | `2p²` / `−1+(4−2p)p` | accelerate then decelerate |
| `EaseSmooth` | `3p²−2p³` | S-curve, zero slope at both ends |

### Notes

- `EaseIn` and `EaseOut` are quadratic mirrors of each other:
  `EaseOut(p) = 1 − EaseIn(1−p)`.
- `EaseInOut` is piecewise quadratic, C¹-continuous at the midpoint (slope 2
  on both sides), with zero slope at p=0 and p=1.
- `EaseSmooth` (classic smoothstep) has zero slope at both endpoints and is the
  preferred curve for camera moves or speed ramps where `EaseInOut`'s sharper
  midpoint is undesirable.
- All four non-linear curves are monotonically non-decreasing over [0, 1].

## Usage examples

### With keyframe animation

```go
import (
    "time"

    mgo "github.com/mowshon/moviego/v2"
)

// Zoom from 1.0x to 1.3x with a smooth S-curve.
v := src.Animate(mgo.PropScale, []mgo.Keyframe{
    {At: 0, Val: 1.0},
    {At: 4 * time.Second, Val: 1.3},
}, mgo.EaseSmooth)
```

### With transitions

```go
import "github.com/mowshon/moviego/v2/transition"

// A dissolve that accelerates in, then decelerates out.
t := transition.Eased(transition.Dissolve{}, ease.EaseInOut)
```

### With the keyframe package directly

```go
import (
    "github.com/mowshon/moviego/v2/ease"
    "github.com/mowshon/moviego/v2/keyframe"
)

track := keyframe.New([]keyframe.Key{
    {At: 0, Val: 0},
    {At: time.Second, Val: 1},
}, ease.EaseOut)

track.Eval(500 * time.Millisecond) // ≈ 0.75 (fast start, decelerating)
```

### Custom easing

`ease.Func` is just `func(float64) float64`. Keep `f(0) = 0` and `f(1) = 1`
so the curve composes without shifting the animation endpoints. Any function
that satisfies those constraints works:

```go
// A cubic "back" ease that overshoots slightly before settling.
back := func(p float64) float64 {
    const s = 1.70158
    p -= 1
    return p*p*((s+1)*p+s) + 1
}

v := src.Animate(mgo.PropScale, []mgo.Keyframe{
    {At: 0, Val: 0.8},
    {At: 400 * time.Millisecond, Val: 1.0},
}, back)
```

> **Note:** an overshooting curve may produce `p` values slightly outside
> `[0, 1]`. Effects like opacity clamp their input, but a time-remap with a
> rewinding segment may route the clip to the sequential engine.
