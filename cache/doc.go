// Package cache provides bounded caches with a visible eviction policy, used by
// graph nodes that re-read the same data (notably decoded images in an image
// sequence). Caches are a fast path, never a correctness requirement: a miss
// always falls back to recomputing or re-reading the value.
package cache
