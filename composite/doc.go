// Package composite layers and concatenates video clips. CompositeNode is the
// timeline compositor: it pulls each active child's frame at the child's local
// time, positions it, and blends it over the canvas using the four compose_on
// paths in the blend subpackage. ConcatChainNode and ConcatComposeNode join
// clips end to end.
//
// The compositor produces RGB and (for a transparent canvas) alpha in a single
// RenderInto pass with per-call scratch, so it carries no mutable node state and
// is safe to call concurrently for different times when its children are.
package composite
