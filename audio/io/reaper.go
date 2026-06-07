package audioio

import (
	"context"
	"sync"
)

// Reaper collects the decoders opened during one render so the render driver can
// close (kill + reap) them when it finishes. A decoder is opened lazily inside a
// transient mix graph (a composite's child audio, repositioned with WithStart)
// that nothing else retains for Close: a child whose source never hits EOF during
// the render — typically the longest child, read to exactly its end — would be
// left as a running, then on context cancellation defunct, FFmpeg process until
// the parent process exits. The reaper gives that subtree a single owner without
// changing the node Close ownership model. It is used by one render and its
// decoders' Close is idempotent, so reaping never double-frees a node that is
// also closed through its own handle.
type Reaper struct {
	mu       sync.Mutex
	decoders []*Decoder
}

func (r *Reaper) add(d *Decoder) {
	r.mu.Lock()
	r.decoders = append(r.decoders, d)
	r.mu.Unlock()
}

// CloseAll closes every decoder registered under this reaper and returns the
// first error. It empties the set, so a second call is a no-op.
func (r *Reaper) CloseAll() error {
	r.mu.Lock()
	ds := r.decoders
	r.decoders = nil
	r.mu.Unlock()
	var first error
	for _, d := range ds {
		if err := d.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

type reaperKey struct{}

// WithReaper returns a context carrying r; decoders opened under it register
// themselves for reaping. A decoder opened without a reaper in its context falls
// back to its own Close lifecycle (the node that owns it closes it).
func WithReaper(ctx context.Context, r *Reaper) context.Context {
	return context.WithValue(ctx, reaperKey{}, r)
}

func reaperFrom(ctx context.Context) *Reaper {
	r, _ := ctx.Value(reaperKey{}).(*Reaper)
	return r
}
