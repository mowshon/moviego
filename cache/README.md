# cache

Package `cache` provides a generic, bounded, goroutine-safe LRU cache used by
graph nodes that re-read the same data (notably decoded images in an image
sequence).

## Design contract

The cache is an **optimization only**. A miss always falls back to recomputing
or re-reading the value; the cache is never on the correctness path. This means:

- Disabling the cache (`capacity ≤ 0`) must never break correctness.
- Cache hits and misses must produce the same output.

## API

```go
import "github.com/mowshon/moviego/v2/cache"

// Create a cache with a fixed capacity.
c := cache.NewLRU[string, *MyValue](64)

// Store a value. No-op when capacity ≤ 0.
c.Put("key", value)

// Retrieve a value. Returns (zero, false) on miss.
v, ok := c.Get("key")

// Inspect.
c.Len() // current number of entries
c.Cap() // maximum entries (fixed at construction)
```

## Eviction

When `Put` causes the entry count to exceed `Cap`, the least-recently-used
entry is removed. Both `Get` and `Put` mark the touched entry as
most-recently-used.

## Zero / negative capacity

A cache created with `capacity ≤ 0` accepts no entries — every `Put` is a
no-op and every `Get` returns a miss. Callers can use this to disable caching
uniformly without special-casing.

## Concurrency

All methods are safe for concurrent use. The implementation uses a single
`sync.Mutex`; the lock is held only during the map/list update, never during
value construction.
