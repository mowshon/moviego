# Timing

The contracts that determine which frames an export emits and where a subclip
begins and ends.

## Rate is rational, never float

`Rate{Num, Den}` is an exact ratio; `Float()` exists only for display and
probing. `30000/1001` cannot be a `float64` without drift over a long clip, so
all timing math stays in the rational/integer domain.

`FrameTime(i) = i * Den * Second / Num` — the exact timestamp of frame `i`.

## Reading: integer `TimeToFrame`

Mapping a timestamp back to a source frame index (only when reading an existing
file) reproduces MoviePy's `int(fps*t + 1e-5)` in integer math:

```
TimeToFrame(t) = trunc((t_ns*Num + Den*1e4) / (Den*1e9))
```

`trunc` (`big.Int.Quo`) truncates toward zero like Python's `int()`. For `t ≥ 0`
this is a floor; for `t < 0` it truncates upward rather than flooring, so e.g.
`-1ms @ 30fps → 0`, not `-1`. `FrameTime`/`TimeToFrame` round-trip (frame `i`'s
own timestamp maps back to `i`) and are monotonic — both fuzzed.

## Exporting: floor frame count

Export is index-driven, not `for t := range`. Frame `i` is emitted while
`FrameTime(i) < duration`; the schedule stops at the first `i` whose timestamp
reaches `duration`. This is a **floor** count and a deliberate divergence from
MoviePy's `int(duration*fps)` (documented in compatibility). The decoder's EOF
is the backstop for short or variable-rate files; whichever comes first wins,
and the actual delivered count is returned.

## Duration / End: a sentinel, not an ok flag

`Clip.Duration()` and `Clip.End()` return a single `Time`. A clip whose length is
unknown or unbounded (a generated `ColorNode` before `WithDuration`, an unbounded
`Loop`) returns `clip.Infinite` rather than a `(value, false)` pair. Use
`clip.Finite(d)` to test for a real duration; export requires a finite duration
and rejects `Infinite` with `ErrNoDuration`. `EndOr` guards the `start + dur`
arithmetic so an unbounded clip's `End()` is `Infinite`, never an overflow.

## Subclip boundaries

`Subclip(a, b)` maps output time `t` to source time `a + t` and has duration
`b - a`. On the facade the end is **optional** — `v.Subclip(start)` trims from
`start` to the end of the media; `v.Subclip(start, end)` trims to `end`. Audio
follows the same rules: `audio.Subclip` is end-based and resolves the open or
negative end against the audio's own duration (the dur-based `SubclipDur`
primitive serves the already-resolved sidecar trim from `video.Subclip`):

- A non-positive `b` is relative to the source duration: `b == 0` means "to the
  end", a negative `b` counts back from the end (`b = srcDur + b`).
- The end boundary tolerates **one rational tick** (`FrameTime(1)` =
  `Den*Second/Num`) of overshoot before being clamped to the source duration, so
  a `b` that rounds a hair past the end still yields the full tail rather than
  dropping the last frame. Overshoot beyond one tick is hard-clamped.

A `TimeTransformNode` carries the mapping; its source-access class is
`AccessLinear` for a forward subclip.
