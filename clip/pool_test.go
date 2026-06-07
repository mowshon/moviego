package clip

import "testing"

func TestFramePoolReuse(t *testing.T) {
	p := NewFramePool()

	f1 := p.Get(4, 4, RGB24)
	if f1.Bytes() != 4*4*3 {
		t.Fatalf("Bytes = %d, want %d", f1.Bytes(), 48)
	}
	f1.Release()

	f2 := p.Get(4, 4, RGB24)
	if f2 != f1 {
		t.Errorf("expected released frame to be reused (same pointer)")
	}
	if f2.pool != p {
		t.Errorf("reused frame not re-attached to pool")
	}
}

func TestFramePoolBucketsByByteLength(t *testing.T) {
	p := NewFramePool()

	// 4x4 RGB24 (48 bytes) and 12x4 Gray8 (48 bytes) share a bucket.
	a := p.Get(4, 4, RGB24)
	a.Release()
	b := p.Get(12, 4, Gray8)
	if b != a {
		t.Errorf("equal byte length should reuse the same buffer")
	}
	if b.Format != Gray8 || b.W != 12 || b.Stride != 12 {
		t.Errorf("reused frame metadata not reset: %+v", b)
	}
}

func TestReleaseIdempotent(t *testing.T) {
	p := NewFramePool()
	f := p.Get(2, 2, RGB24)
	f.Release()
	f.Release() // must not double-add to the pool

	g := p.Get(2, 2, RGB24)
	if g != f {
		t.Fatal("first Get should return the single released frame")
	}
	// A second Get must allocate fresh, not hand back the same pointer again.
	if h := p.Get(2, 2, RGB24); h == g {
		t.Error("double Release added the frame twice")
	}
}

func TestUnpooledReleaseNoop(t *testing.T) {
	f := NewFrame(2, 2, RGB24)
	f.Release() // no pool; must not panic
}

func TestFramePoolDoublePutNoop(t *testing.T) {
	p := NewFramePool()
	f := p.Get(2, 2, RGB24)
	p.Put(f)
	p.Put(f)    // direct double Put must not re-enqueue
	f.Release() // and a following Release is a no-op too

	g := p.Get(2, 2, RGB24)
	if g != f {
		t.Fatal("first Get should reuse the single Put frame")
	}
	if h := p.Get(2, 2, RGB24); h == g {
		t.Error("double Put added the frame twice")
	}
}

func TestFramePoolPutForeignFrame(t *testing.T) {
	p := NewFramePool()
	foreign := NewFrame(2, 2, RGB24) // never checked out from p
	p.Put(foreign)                   // must be ignored, not enqueued

	if f := p.Get(2, 2, RGB24); f == foreign {
		t.Error("pool handed out a frame it never owned")
	}
}
