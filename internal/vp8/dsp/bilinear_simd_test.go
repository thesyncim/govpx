package dsp

import (
	"math/rand/v2"
	"testing"
)

// TestBilinearPredictSIMDMatchesScalar verifies that the bilinear SIMD
// dispatch produces byte-identical output to the scalar reference for
// every (xoffset, yoffset) in [0..7]^2 across a random source corpus.
func TestBilinearPredictSIMDMatchesScalar(t *testing.T) {
	// Bilinear horizontal loads need 32 bytes per row for 16x16 (covers
	// the [0..16] tap window via VEXT), and 16 bytes per row for 8x8.
	cases := []struct {
		name   string
		w, h   int
		stride int
	}{
		{"16x16", 16, 16, 32},
		{"16x16-stride48", 16, 16, 48},
		{"8x8", 8, 8, 16},
		{"8x8-stride32", 8, 8, 32},
	}

	r := rand.New(rand.NewPCG(0xb1f10ed, 0xface5eed))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := tc.h + 1
			src := make([]byte, tc.stride*rows)
			for i := range src {
				src[i] = byte(r.UintN(256))
			}

			dstSIMD := make([]byte, tc.w*tc.h)
			dstRef := make([]byte, tc.w*tc.h)

			for xoff := 0; xoff < 8; xoff++ {
				for yoff := 0; yoff < 8; yoff++ {
					for i := range dstSIMD {
						dstSIMD[i] = 0
						dstRef[i] = 0
					}

					switch tc.w*100 + tc.h {
					case 16*100 + 16:
						BilinearPredict16x16(src, tc.stride, xoff, yoff, dstSIMD, tc.w)
					case 8*100 + 8:
						BilinearPredict8x8(src, tc.stride, xoff, yoff, dstSIMD, tc.w)
					default:
						t.Fatalf("unexpected size %dx%d", tc.w, tc.h)
					}

					bilinearPredict(src, tc.stride, xoff, yoff, dstRef, tc.w, tc.w, tc.h)

					for i := 0; i < tc.w*tc.h; i++ {
						if dstSIMD[i] != dstRef[i] {
							y := i / tc.w
							x := i % tc.w
							t.Fatalf("xoff=%d yoff=%d size=%dx%d stride=%d: dst[%d,%d] simd=%d scalar=%d",
								xoff, yoff, tc.w, tc.h, tc.stride, x, y, dstSIMD[i], dstRef[i])
						}
					}
				}
			}
		})
	}
}
