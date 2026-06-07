package testmedia

import (
	"errors"
	"fmt"
)

var (
	errEmpty  = errors.New("no rows")
	errRagged = errors.New("rows have unequal width")
)

func errUnknownCode(r rune) error {
	return fmt.Errorf("unknown color code %q", r)
}
