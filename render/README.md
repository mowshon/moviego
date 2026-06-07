# render

The render package orchestrates video export: it classifies a clip graph,
picks an execution engine, and drives the graph into an encoder frame by frame.

---

## Engines

Three engines are available. The planner selects one automatically; callers can
observe the decision via `Describe` or force it in tests via `planOptions.engineOverride`.

| Engine | When used | Notes |
|---|---|---|
| `EngineSequential` | Random/bounded access, single worker, or pipeline-ineligible graph | Correctness oracle; frames rendered one at a time on the calling goroutine |
| `EnginePipeline` | Static/linear graph with multiple workers and a single forward pass per source | 3-stage: sequential decoder per source → worker pool → reorder+encode-feed |
| `EngineFFmpegOnly` | Opt-in (`EnableFusion`), fully FFmpeg-expressible single-source chain | One FFmpeg filtergraph invocation; no Go pixels |

The keystone invariant: **parallel == sequential, byte-for-byte**, enforced by
CI tests under `-race`.

---

## Graph classification

`BuildPlan` inspects the root's `SourceAccess()` (the least-linear leaf) and
maps it to a class:

| Class | Source access | Engine |
|---|---|---|
| `ClassStatic` | Image / color | Pipeline (if workers > 1) |
| `ClassLinear` | File source, forward subclip | Pipeline (if workers > 1 and pipeline-eligible) |
| `ClassBounded` | Bounded window (no node emits this yet) | Sequential (no window model yet) |
| `ClassRandom` | Reverse, loop, negative speed, backward remap | Sequential |

### Pipeline eligibility

The pipeline requires two properties:

1. **Single forward pass per source.** `maxSourceMultiplicity` counts how many
   distinct graph paths reach each source key. If any source is reachable by
   more than one path (e.g. the same file in two composite layers, or a clip
   body plus an adjacent transition), the planner falls back to sequential.

2. **Concurrent-safe pixel work.** Every non-file leaf must report
   `ParallelSafe() == true`. File leaves are exempt because the pipeline
   replaces their decoder with a shared `sourceProvider`.

---

## The 3-stage pipeline

```
 Dispatcher ──────────► jobs (buffered)
                              │
              ┌───────────────┼────────────────┐
              │               │                │
          Worker 0        Worker 1  ...    Worker N
        RenderInto       RenderInto         RenderInto
              │               │                │
              └───────────────┼────────────────┘
                              ▼
                    results (buffered, MaxInflight)
                              │
                       Encode-feed
                    reorder by index
                    sink.writeFrame (in order)
```

**Dispatcher** schedules indices 0 … Frames-1 in order. Once any worker
reports EOF (`atomicMin(&minEOF, i)`), the dispatcher stops issuing indices
at or past the EOF boundary.

**Workers** allocate RGB (and optionally Gray8 alpha) frames from a shared
`FramePool`, call `root.RenderInto`, and send the result (or an EOF marker)
on the results channel. On cancellation or non-EOF error they release frames
immediately and exit.

**Encode-feed** is a single goroutine that buffers out-of-order results in a
`pending` map (bounded by `ReorderDepth`) and writes contiguous indices in
order to the `encoderSink`. On EOF or write error it sets a stop flag, calls
`fail(err)` to cancel the pipeline context, and drains the channel until
workers exit; all buffered frames are then released.

### Source provider

`sourceProvider` owns one `videoio.Decoder` per distinct file source. Each
worker that calls `SourceFrameInto(key, idx, dst)` cooperates with the others:

- One worker at a time drives the decoder forward (`decoding = true`).
- Others wait on a `sync.Cond` until their index lands in the decoded cache.
- Decoded frames are evicted once no worker needs them (`evictLocked`).
- `watchCancel` unblocks all waiting workers immediately on context cancellation.

---

## Reverse provider

For random-access graphs (reverse playback, non-monotonic time remaps), the
sequential engine installs a `reverseProvider` instead of the plain file-node
decoder. This turns O(N²) seek-and-restart into O(N) decodes:

- Each source keeps a sliding window of `≤ max` decoded frames.
- A request inside the window is a copy (no seek).
- A request before the window start refills the window in one forward pass.
- Short clips (total frame count ≤ window) are fully buffered on first access
  with a single forward pass; the entire backward walk is served from cache.

Window size is derived from `reverseWindowBudget` (256 MiB) and the frame
size, so a large-resolution source gets a smaller frame count per window.

---

## FFmpeg-only engine

When `EnableFusion` is set and the whole graph is expressible as a single
FFmpeg filtergraph (`fuse` returns ok), `runFFmpegOnly` launches one FFmpeg
process with:

- The planner's filtergraph as `-filter_complex`
- `-frames:v <scheduled_count>` so the output length matches the Go engines
- The pre-rendered audio file muxed with `-c:a copy -shortest`

FFmpeg's `-progress pipe:1` output is read by a background goroutine to drive
the `Progress` callbacks at the same per-frame granularity as the Go engines.

Fused output is YUV-domain and **not** byte-identical to the Go path (a
documented divergence). Never assert pixel equality across the fusion boundary.

---

## Progress

`Progress` is driven exclusively by the encode-feed goroutine (sequential by
construction). `SetTotal` is called once with the planned frame count and
optionally revised after the run when the source ends early. `Step` is called
once per frame written, in output order.

`BarProgress` is a built-in stderr bar; `NopProgress` ignores every callback.

---

## Memory budget

`Plan.Budget` is a conservative memory ceiling in bytes:

```
(workers + reorder) × frameBytes × sourceCount   // source frames in flight
+ reorder × frameBytes                            // reorder buffer (output frames)
+ RGBA scratch (transparent only)                 // encoder interleave buffer
+ workers × 2 × rgbBytes                          // per-worker transform scratch
```

`Budget` is advisory only — it is exposed for observability and tests and is
never enforced as a hard cap.

---

## Critical invariants

1. **parallel == sequential.** Adding a node that cannot be made
   concurrency-safe must report `ParallelSafe() == false` (or a non-linear
   `SourceAccess`), or the pipeline will race it.

2. **Single-owner frames.** Frames come from a `FramePool` and must be
   released exactly once. Workers release on send failure; the encode-feed
   releases after writing or on stop. The source provider releases cached
   frames on close.

3. **Context cancellation kills FFmpeg.** Any new subprocess must be bound to
   the export context and reaped. On Unix the whole process group is SIGKILL'd;
   on other platforms only the direct child is killed (a known gap).

4. **EOF is graceful.** Both engines stop at the same `minEOF` boundary. The
   source ending before its declared duration is not an error; the final
   `SetTotal` revision corrects the progress denominator to the real count.
