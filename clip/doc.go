// Package clip holds the foundational value types and interfaces shared by the
// whole library: timeline metadata, rational frame rates, packed pixel frames,
// audio buffers, and errors. It depends on nothing else in the module so that
// higher layers (video, audio, composite, render) can import it without cycles.
package clip
