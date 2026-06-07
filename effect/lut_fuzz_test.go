package effect

import (
	"strings"
	"testing"
)

// FuzzParseCube checks the .cube parser never panics and never returns a
// non-nil LUT together with an error, on arbitrary input.
func FuzzParseCube(f *testing.F) {
	f.Add("LUT_3D_SIZE 2\n0 0 0\n1 0 0\n0 1 0\n1 1 0\n0 0 1\n1 0 1\n0 1 1\n1 1 1\n")
	f.Add("# comment\nTITLE \"x\"\nLUT_3D_SIZE 2\n")
	f.Add("LUT_3D_SIZE abc\n")
	f.Add("LUT_1D_SIZE 4\n")
	f.Add("DOMAIN_MIN 0 0\nLUT_3D_SIZE 2\n")
	// A degenerate domain on a non-red channel (green max == min) must be rejected,
	// not parsed into a table that samples to NaN grid coordinates.
	f.Add("LUT_3D_SIZE 2\nDOMAIN_MAX 1 0 1\n0 0 0\n1 0 0\n0 1 0\n1 1 0\n0 0 1\n1 0 1\n0 1 1\n1 1 1\n")
	f.Add("0 0 0\n1 1 1\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, data string) {
		cube, err := parseCube(strings.NewReader(data))
		if err != nil {
			if cube != nil {
				t.Fatalf("error %v returned with non-nil cube", err)
			}
			return
		}
		// A successful parse must be internally consistent and safe to sample.
		if cube.size < 2 || len(cube.data) != cube.size*cube.size*cube.size*3 {
			t.Fatalf("inconsistent cube: size=%d len=%d", cube.size, len(cube.data))
		}
		cube.sample(0.25, 0.5, 0.75)
		cube.sample(0, 0, 0)
		cube.sample(1, 1, 1)
	})
}
