package dsp

import (
	"math/rand/v2"
	"testing"
)

// TestSixTapPredictSIMDMatchesScalar verifies that the SIMD dispatch
// (NEON arm64, SSE2 amd64) produces byte-identical output to the
// scalar reference for every (xoffset, yoffset) ∈ [0..7]² across a
// random source corpus.
func TestSixTapPredictSIMDMatchesScalar(t *testing.T) {
	// strides are >= 32 because the SIMD horizontal loads read 32 bytes
	// per source row (VLD1 over [V0, V1] / MOVOU + 16). The pad bytes
	// past the active 16/8 lanes feed the VEXT taps and are otherwise
	// unused.
	cases := []struct {
		name   string
		w, h   int
		stride int
	}{
		{"16x16", 16, 16, 32},
		{"16x16-stride48", 16, 16, 48},
		{"16x8", 16, 8, 32},
		{"16x8-stride48", 16, 8, 48},
		{"8x16", 8, 16, 32},
		{"8x16-stride40", 8, 16, 40},
		{"8x8", 8, 8, 32},
		{"8x8-stride40", 8, 8, 40},
		{"8x4", 8, 4, 32},
		{"8x4-stride40", 8, 4, 40},
		{"4x4", 4, 4, 32},
		{"4x4-stride24", 4, 4, 24},
	}

	r := rand.New(rand.NewPCG(0xc0ffee, 0xdeadbeef))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := tc.h + 5
			src := make([]byte, tc.stride*rows)
			for i := range src {
				src[i] = byte(r.UintN(256))
			}

			dstSIMD := make([]byte, tc.w*tc.h)
			dstRef := make([]byte, tc.w*tc.h)

			for xoff := range 8 {
				for yoff := range 8 {
					for i := range dstSIMD {
						dstSIMD[i] = 0
						dstRef[i] = 0
					}

					switch tc.w*100 + tc.h {
					case 16*100 + 16:
						SixTapPredict16x16(src, tc.stride, xoff, yoff, dstSIMD, tc.w)
					case 16*100 + 8:
						SixTapPredict16x8(src, tc.stride, xoff, yoff, dstSIMD, tc.w)
					case 8*100 + 16:
						SixTapPredict8x16(src, tc.stride, xoff, yoff, dstSIMD, tc.w)
					case 8*100 + 8:
						SixTapPredict8x8(src, tc.stride, xoff, yoff, dstSIMD, tc.w)
					case 8*100 + 4:
						SixTapPredict8x4(src, tc.stride, xoff, yoff, dstSIMD, tc.w)
					case 4*100 + 4:
						SixTapPredict4x4(src, tc.stride, xoff, yoff, dstSIMD, tc.w)
					default:
						t.Fatalf("unexpected size %dx%d", tc.w, tc.h)
					}

					sixTapPredict(src, tc.stride, xoff, yoff, dstRef, tc.w, tc.w, tc.h)

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

func TestSixTapPredict8x8PairMatchesSeparateCalls(t *testing.T) {
	const stride = 40
	const rows = 13
	r := rand.New(rand.NewPCG(0x1234, 0x5678))
	src0 := make([]byte, stride*rows)
	src1 := make([]byte, stride*rows)
	for i := range src0 {
		src0[i] = byte(r.UintN(256))
		src1[i] = byte(r.UintN(256))
	}

	for xoff := range 8 {
		for yoff := range 8 {
			dst0Pair := make([]byte, 8*8)
			dst1Pair := make([]byte, 8*8)
			dst0Single := make([]byte, 8*8)
			dst1Single := make([]byte, 8*8)

			SixTapPredict8x8Pair(src0, stride, src1, stride, xoff, yoff, dst0Pair, 8, dst1Pair, 8)
			SixTapPredict8x8(src0, stride, xoff, yoff, dst0Single, 8)
			SixTapPredict8x8(src1, stride, xoff, yoff, dst1Single, 8)

			for i := range dst0Pair {
				if dst0Pair[i] != dst0Single[i] {
					t.Fatalf("plane 0 xoff=%d yoff=%d index=%d pair=%d single=%d", xoff, yoff, i, dst0Pair[i], dst0Single[i])
				}
				if dst1Pair[i] != dst1Single[i] {
					t.Fatalf("plane 1 xoff=%d yoff=%d index=%d pair=%d single=%d", xoff, yoff, i, dst1Pair[i], dst1Single[i])
				}
			}
		}
	}
}
