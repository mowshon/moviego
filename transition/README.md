# Transitions

A transition is the content shown during the **overlap window** where an
outgoing clip A hands over to an incoming clip B. It is a pure function of three
things — a normalized progress `p ∈ [0,1]`, the outgoing frame A, and the
incoming frame B — which is the entire interface:

```go
type Transition interface {
    // p == 0 is fully A, p == 1 is fully B. dst, a and b are RGB24 frames of the
    // same size; do not retain them past the call.
    Frame(p float64, dst, a, b *clip.Frame)
    Name() string
}
```

That single `Frame` method is the whole extension surface: write one function
and you have a working transition.

Each built-in lives in its own file (`crossfade.go`, `wipe.go`, …), mirroring
the `effect` package. The two-input render node, the timeline assembler, and the
audio crossfade live in the `composite` package; the facade builders
(`mgo.NewSequence`, `mgo.ConcatWith`, and the step helpers) live in the root
`mgo` package.

## Applying transitions

**Per-boundary, via the fluent builder** — the clearest API for mixed
transitions:

```go
v := mgo.NewSequence().
    Add(intro).
    Then(mgo.Crossfade(time.Second)).
    Add(body).
    Then(mgo.WipeLeft(500 * time.Millisecond)).
    Add(outro).
    Then(mgo.FadeThroughBlack(time.Second)).
    Add(credits).
    Video()                       // returns *Video (carries build errors)
```

**One uniform transition between every clip** — the common case:

```go
v := mgo.ConcatWith(mgo.ConcatOptions{
    Transition: transition.CrossFade{},
    Duration:   500 * time.Millisecond,
}, a, b, c)
```

Two clips added without a `Then` between them are a hard cut. An overlap longer
than the clip it consumes (or, for an interior clip, longer than its two
overlaps combined) is a build error carried on the returned `*Video`.

## Built-in transitions

| Constructor (struct)              | Facade step(s)                                              | What it does |
|-----------------------------------|-------------------------------------------------------------|--------------|
| `transition.CrossFade{}`          | `mgo.Crossfade(d)`                                          | Straight alpha dissolve A→B. Also blends the alpha sidecar (a `MaskTransition`), so it is exact for transparent clips. |
| `transition.FadeThroughColor{Color, Mid}` | `mgo.FadeThroughBlack(d)`, `mgo.FadeThroughColor(d, col)`   | A→color→B. Zero `Color` is black ("fade to black"); `Mid` (default 0.5) is where the frame is fully the color. |
| `transition.Dissolve{}`           | `mgo.Dissolve(d)`                                          | Film-style ordered-dither (Bayer) dissolve — a growing stipple of B, no mid-point luminance dip. |
| `transition.Wipe{Dir, Softness}`  | `mgo.WipeLeft/Right/Up/Down(d)`                            | A straight edge sweeps in `Dir`, revealing B. `Softness` (fraction of the swept dimension) feathers the edge; zero is a hard wipe. |
| `transition.Slide{Dir}`           | `mgo.SlideLeft/Right/Up/Down(d)`                          | B slides in over a stationary A, from the edge opposite `Dir`. |
| `transition.Push{Dir}`            | `mgo.PushLeft/Right/Up/Down(d)`                           | B slides in while pushing A out the far side; both move in `Dir`. |
| `transition.Iris{Shape, Out, CenterX, CenterY}` | `mgo.IrisOpen(d)`, `mgo.IrisClose(d)`                     | Reveal B through an expanding aperture (`Out=false`) or hide A through a contracting one (`Out=true`). `Shape` is `Circle` (default), `Diamond`, or `Rectangle`. |

`Dir` is `transition.Right` (the zero value), `Left`, `Up`, or `Down`. Every
struct has a useful zero value (`Wipe{}` is a hard left-to-right wipe,
`Iris{}` is a circular iris-in from the center).

Every built-in implements `MaskTransition`, so over transparent inputs the alpha
sidecar follows the same shape as the visible transition (the wipe edge, the iris
aperture, the dissolve stipple), not a uniform fade.

### Decorators

Any transition can be wrapped without a second implementation:

```go
transition.Eased(transition.Wipe{}, ease.EaseInOut) // ease the progress curve
transition.Reversed(transition.Wipe{Dir: transition.Left}) // flip A↔B and direction
```

`Eased` and `Reversed` preserve a wrapped transition's alpha blending
(`MaskTransition`) when it has it.

### Audio across the cut

The transition's overlap mixes A's tail (faded out) with B's head (faded in).
The curve is chosen on the step / `ConcatOptions` (`Curve` / `AudioCurve`):

- `mgo.CrossfadeEqualPower` (default) — `gain = √p` / `√(1-p)`, so the summed
  power stays flat; a linear pair dips ~3 dB at the midpoint.
- `mgo.CrossfadeLinear` — straight linear ramps.
- `mgo.CrossfadeNone` — no audio blend; A's tail plays through and B's audio
  resumes at the body boundary.

## Writing a custom transition

A custom transition lives entirely in **your own package** — it needs no moviego
changes and imports only `clip` (for the `*clip.Frame` type) and the `mgo` facade
to apply it. Implement the one-method interface and pass the value to
`mgo.UseTransition`, which accepts any `transition.Transition`. A runnable
`Diagonal` transition is defined and applied this way in `examples/transitions`.

There are three ways, in increasing weight.

**1. Inline, with `transition.Func`** — for a one-off, no type needed:

```go
fade := transition.Func("fade", func(p float64, dst, a, b *clip.Frame) {
    t := byte(p*255 + 0.5)
    for i := range dst.Pix {
        // dst = a*(1-p) + b*p, per byte
        dst.Pix[i] = byte((int(a.Pix[i])*(255-int(t)) + int(b.Pix[i])*int(t)) / 255)
    }
})

v := mgo.NewSequence().Add(a).Then(mgo.UseTransition(fade, time.Second)).Add(b).Video()
```

**2. A config struct** — the idiomatic form, with tunable exported fields and
useful zero-value defaults, exactly like a built-in:

```go
type Glitch struct{ Intensity float64 } // 0 is a sensible no-op default

func (g Glitch) Name() string { return "glitch" }

func (g Glitch) Frame(p float64, dst, a, b *clip.Frame) {
    // Mix A and B with a progress-driven horizontal tear sized by g.Intensity.
    // ... per-pixel work into dst ...
}

// Use it through the generic step:
seq.Then(mgo.UseTransition(Glitch{Intensity: 0.6}, 400*time.Millisecond))
```

**3. Add alpha** — if your transition should also blend transparency, implement
the optional `MaskTransition` extension. The engine type-asserts for it at
render time; a plain transition is treated as opaque.

```go
func (g Glitch) FrameMask(p float64, dst, a, b *clip.Frame) {
    // dst, a, b are Gray8 alpha planes of the same size.
}
```

### Rules every transition follows

- **`Frame` is pure.** Hold only immutable config; never retain `dst`, `a`, or
  `b` past the call. This is what makes a transition safe to share across the
  parallel render pipeline.
- **`dst`, `a`, `b` share dimensions** and are RGB24 (Gray8 for `FrameMask`).
  Index a pixel at `(x, y)` as `dst.Pix[y*dst.Stride + x*3]`.
- **Endpoints are the contract.** `p == 0` should be fully A and `p == 1` fully
  B, so the transition joins cleanly to the clip bodies on either side.

## Rendering notes

- Transitions render on the Go path; they advertise no FFmpeg filter, so a
  transition timeline never takes the FFmpeg-only fast path.
- An assembled timeline renders on the sequential engine whenever a clip keeps a
  straight-through body: that body and the clip's adjacent transition reach the
  same decoder over disjoint windows, which the planner's source-multiplicity
  guard treats as a conflict — and this holds even when the two clips are
  *distinct* files, since each is still reached by both its own body and the
  transition. A bare transition with no remaining bodies (e.g. a crossfade as
  long as both clips, so each source is read exactly once) stays
  pipeline-eligible. `mgo.Describe` reports the chosen engine.

See `examples/transitions` for a runnable demo.
