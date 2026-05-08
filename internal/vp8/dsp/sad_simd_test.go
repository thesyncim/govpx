package dsp

import (
	"math/rand"
	"testing"
)

// TestSADSIMDMatchesScalar exhaustively cross-checks the SIMD SAD
// primitives against an independent scalar reference for every block size
// govpx uses. Stride/offset combinations stress unaligned loads and
// non-contiguous source layouts, mirroring the strides hit by motion
// search.
func TestSADSIMDMatchesScalar(t *testing.T) {
	const planeStride = 64
	const planeRows = 64
	plane := make([]byte, planeStride*planeRows)
	ref := make([]byte, planeStride*planeRows)

	r := rand.New(rand.NewSource(0xC0DEFACE))
	for i := range plane {
		plane[i] = byte(r.Intn(256))
		ref[i] = byte(r.Intn(256))
	}

	cases := []struct {
		name string
		fn   func(src []byte, srcStride int, ref []byte, refStride int) int
		w, h int
	}{
		{"16x16", SAD16x16, 16, 16},
		{"16x8", SAD16x8, 16, 8},
		{"8x16", SAD8x16, 8, 16},
		{"8x8", SAD8x8, 8, 8},
		{"4x4", SAD4x4, 4, 4},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			for srcOff := 0; srcOff < 8; srcOff++ {
				for refOff := 0; refOff < 8; refOff++ {
					srcSlice := plane[srcOff*planeStride+srcOff:]
					refSlice := ref[refOff*planeStride+refOff:]
					got := c.fn(srcSlice, planeStride, refSlice, planeStride)
					want := scalarSAD(srcSlice, planeStride, refSlice, planeStride, c.w, c.h)
					if got != want {
						t.Fatalf("%s offsets (src=%d ref=%d): got %d want %d", c.name, srcOff, refOff, got, want)
					}
				}
			}
		})
	}
}

// TestSAD16x16LimitSIMDMatchesScalar covers the limit-aware 16x16 SIMD
// kernel against the scalar sadBlockLimit on a sweep of limits including
// edge cases (0, exact match, slightly under, well above).
func TestSAD16x16LimitSIMDMatchesScalar(t *testing.T) {
	const planeStride = 32
	plane := make([]byte, planeStride*32)
	ref := make([]byte, planeStride*32)
	r := rand.New(rand.NewSource(0xC0FFEE))
	for i := range plane {
		plane[i] = byte(r.Intn(256))
		ref[i] = byte(r.Intn(256))
	}

	full := scalarSADLimit(plane, planeStride, ref, planeStride, 16, 16, 1<<30)

	limits := []int{0, 1, 100, 1000, full / 4, full / 2, full - 1, full, full + 1, full * 2, 1 << 30}
	for _, lim := range limits {
		got := SAD16x16Limit(plane, planeStride, ref, planeStride, lim)
		want := scalarSADLimit(plane, planeStride, ref, planeStride, 16, 16, lim)
		if got != want {
			t.Fatalf("limit=%d: got %d want %d (full=%d)", lim, got, want, full)
		}
	}
}

// TestSAD16x16LimitSIMDEarlyExit verifies the limit kernel returns the
// running sum at row boundaries, not the final 16-row sum, when a tight
// limit is hit early. Behaviour must match the scalar reference exactly so
// the encoder's best-so-far pruning sees the same comparisons.
func TestSAD16x16LimitSIMDEarlyExit(t *testing.T) {
	const planeStride = 32
	plane := make([]byte, planeStride*32)
	ref := make([]byte, planeStride*32)
	for i := range ref {
		ref[i] = 255
	}
	got := SAD16x16Limit(plane, planeStride, ref, planeStride, 256)
	want := scalarSADLimit(plane, planeStride, ref, planeStride, 16, 16, 256)
	if got != want {
		t.Fatalf("early-exit: got %d want %d", got, want)
	}
}

func scalarSADLimit(src []byte, srcStride int, ref []byte, refStride int, w, h, limit int) int {
	sad := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := int(src[y*srcStride+x]) - int(ref[y*refStride+x])
			if d < 0 {
				d = -d
			}
			sad += d
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}
