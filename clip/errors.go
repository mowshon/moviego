package clip

import (
	"errors"
	"fmt"
)

// Sentinel errors surfaced across the library; wrap them with %w via Wrap.
var (
	ErrNoDuration     = errors.New("duration unknown or infinite")
	ErrNoRate         = errors.New("frame rate unknown")
	ErrClosed         = errors.New("resource already closed")
	ErrEOF            = errors.New("end of stream")
	ErrVideoCorrupted = errors.New("video corrupted")
)

// Wrap annotates err with an operation name, preserving the chain for errors.Is.
func Wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
