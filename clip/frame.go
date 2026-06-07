package clip

// Size is width/height in pixels (note: opposite of a NumPy frame's shape order).
type Size struct{ W, H int }

// PixelFormat identifies a packed pixel layout.
type PixelFormat int

const (
	RGB24 PixelFormat = iota // 3 bytes/px, packed
	RGBA                     // 4 bytes/px, packed
	Gray8                    // 1 byte/px — default mask representation
)

// BytesPerPixel returns the packed byte size of one pixel.
func (f PixelFormat) BytesPerPixel() int {
	switch f {
	case RGB24:
		return 3
	case RGBA:
		return 4
	case Gray8:
		return 1
	default:
		return 0
	}
}

// String returns the format name.
func (f PixelFormat) String() string {
	switch f {
	case RGB24:
		return "rgb24"
	case RGBA:
		return "rgba"
	case Gray8:
		return "gray8"
	default:
		return "unknown"
	}
}

// Frame is a packed pixel buffer. Masks are Gray8 by default; high-precision
// alpha uses the separate MaskF32 path. pool is non-nil only when borrowed from
// a FramePool (see the ownership contract).
type Frame struct {
	Pix    []byte
	W, H   int
	Stride int
	Format PixelFormat

	pool *FramePool
}

// NewFrame allocates a packed frame of the given size and format.
func NewFrame(w, h int, f PixelFormat) *Frame {
	stride := w * f.BytesPerPixel()
	return &Frame{
		Pix:    make([]byte, stride*h),
		W:      w,
		H:      h,
		Stride: stride,
		Format: f,
	}
}

// Bytes returns the length of the backing pixel buffer.
func (f *Frame) Bytes() int { return len(f.Pix) }

// Release returns a pooled frame to its FramePool; it is a no-op for unpooled
// frames and idempotent. Put clears the pool link, so repeated Release or a
// mix of Release and Put never re-enqueues the same buffer.
func (f *Frame) Release() {
	if f.pool != nil {
		f.pool.Put(f)
	}
}

// MaskF32 is the explicit high-precision mask, used only where Gray8 banding is
// visible. Kept out of Frame so the common path never pays for it.
type MaskF32 struct {
	Val    []float32 // 0..1
	W, H   int
	Stride int
}

// NewMaskF32 allocates a high-precision mask of the given size.
func NewMaskF32(w, h int) *MaskF32 {
	return &MaskF32{Val: make([]float32, w*h), W: w, H: h, Stride: w}
}
