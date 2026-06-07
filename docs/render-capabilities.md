# Render capabilities

How the planner classifies a graph and chooses an execution engine. Every node
advertises three orthogonal capabilities; the planner combines them with the
export settings to pick exactly one engine.

## The three capabilities a node advertises

- **`SourceAccess() AccessClass`** — how the node maps output frames to source
  reads, *not* a threading hint. One of:
  - `AccessStatic` — no time dependence (image, color).
  - `AccessLinear` — output `i` maps to a monotonically increasing source frame
    (file passthrough, forward `Subclip`, positive `Speed`).
  - `AccessBounded` — reorders within a bounded window (no node reports this yet;
    it is a reserved contract).
  - `AccessRandom` — arbitrary source times (`Loop`, reverse, negative speed, or a
    source reached by more than one path).

  A parent derives its class from its children: a composite/concat is the
  *least-linear* of its children, a forward time transform stays `AccessLinear`,
  and `Loop`/reverse report `AccessRandom`.

- **`ParallelSafe() bool`** — whether `RenderInto` may be *called concurrently*
  for different `t`. It describes the transform/compositor only; it is never a
  license to seek a file in parallel. A composite is parallel-safe iff all
  children are; a pure transform iff its inner clip is; a file source is not
  (its decoder is single-owner), but the pipeline neutralizes that by replacing
  the decoder with a shared sequential provider.

- **`Filter(ffmpeg.FilterContext)` / `FilterInput(...)`** *(optional interfaces
  `video.Filterable` / `video.FilterSource`)* — the FFmpeg-expressibility
  advertisement. A node returns a structured `FilterFragment` (or declines), and
  a leaf returns an `-i` input description. Most nodes do not implement these and
  are simply not fusible.

## Graph classes → decode strategy

| Class | Source reads | Strategy |
| --- | --- | --- |
| static | none | full fan-out; no decoder |
| linear-streamable | monotonic per source | one sequential decoder per source + parallel pixel fan-out |
| bounded-cache | reorder within a window | sequential decode + index-addressed slot table (reserved; routes sequential today) |
| random-access | arbitrary source times | sequential render with a backward-buffered reader (see below) |

A random-access graph (reverse, boomerang, loop, negative speed) reads its
sources non-monotonically. On the sequential engine each source is served by a
**backward-buffered reader** (`render/reverse.go`): it keeps a sliding window of
recently decoded frames, so a step inside the window is a copy and a step past
its start refills the window in one forward pass. This turns a reverse walk from
`O(N²)` FFmpeg restart-and-skips (a plain decoder restarts on every backward
seek) into `O(N)` decodes with `O(N/window)` restarts — a clip short enough to
fit the window budget is decoded exactly once. It plugs in through the same
`FrameProvider` seam the pipeline uses, so neither the file node nor engine
selection changes.

## Engine selection (`render/plan.go`)

The planner picks, in order:

1. **FFmpeg-only** (`render/fuse.go`) — **opt-in** via
   `ExportOptions.EnableFusion`. When enabled and the whole graph is
   FFmpeg-expressible and not transparent, run a single `-filter_complex`
   invocation and skip Go pixels entirely. It is **off by default** so a plain
   export always uses a Go engine (the correctness oracle), avoiding the fused
   path's documented divergences. See
   [ffmpeg-policy.md](ffmpeg-policy.md#filtergraph-fusion-ffmpeg-only-export) for
   the v1 scope and [compatibility.md](compatibility.md#filtergraph-fusion-ffmpeg-only-export)
   for divergences. Also skipped when a test pins a Go engine.
2. **Pipeline** (the 3-stage parallel engine) — for `static`/`linear` graphs that
   are pipeline-eligible: every source is reached by a single path (no source
   used twice) and every non-file leaf is parallel-safe. The decode stage runs
   one sequential decoder per distinct source; a worker pool fans out the
   per-frame pixel work; a single goroutine reorders by index and feeds the
   encoder.
3. **Sequential** — the correctness oracle and the engine for `random-access`
   graphs, reused-source graphs, single-worker requests, and any graph that fails
   pipeline eligibility. CI asserts `parallel == sequential` under `-race`.

The chosen class, engine, worker count, and fusion reason are emitted through
`ExportOptions.Debugf` so an export can always explain *why* it ran the way it
did.

## Why fusion is advertised but decided centrally

A bare `frag string` cannot express stream labels, input pads, pixel-format or
rate needs. Nodes advertise structured fragments and the planner stitches the
labeled graph, which keeps fusion a single, testable decision: a graph is fused
only when *every* node on the spine is understood, and anything unknown falls
back to the Go engine that is already the correctness baseline.
