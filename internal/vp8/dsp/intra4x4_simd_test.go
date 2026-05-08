package dsp

import (
	"math/rand"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

// TestIntra4x4SIMDParity verifies that the dispatched (SIMD on amd64
// arm64; scalar elsewhere) Intra4x4Predict matches the scalar
// reference byte-for-byte across all 10 B_PRED modes, randomized
// inputs, multiple strides, and the full 0..255 byte range.
//
// All 10 modes consume some subset of {above[0..7], left[0..3],
// topLeft}; we randomise all eight above bytes, all four left bytes,
// and the topLeft byte each iteration so each mode sees fully
// arbitrary inputs.
func TestIntra4x4SIMDParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x4b_b9))
	modes := []common.BPredictionMode{
		common.BDCPred,
		common.BTMPred,
		common.BVEPred,
		common.BHEPred,
		common.BLDPred,
		common.BRDPred,
		common.BVRPred,
		common.BVLPred,
		common.BHDPred,
		common.BHUPred,
	}
	modeName := func(m common.BPredictionMode) string {
		switch m {
		case common.BDCPred:
			return "DC"
		case common.BTMPred:
			return "TM"
		case common.BVEPred:
			return "VE"
		case common.BHEPred:
			return "HE"
		case common.BLDPred:
			return "LD"
		case common.BRDPred:
			return "RD"
		case common.BVRPred:
			return "VR"
		case common.BVLPred:
			return "VL"
		case common.BHDPred:
			return "HD"
		case common.BHUPred:
			return "HU"
		}
		return "?"
	}

	for iter := 0; iter < 256; iter++ {
		var above [8]byte
		var left [4]byte
		for i := range above {
			above[i] = byte(rng.Intn(256))
		}
		for i := range left {
			left[i] = byte(rng.Intn(256))
		}
		topLeft := byte(rng.Intn(256))

		for _, stride := range []int{4, 8, 16, 32} {
			for _, mode := range modes {
				ref := make([]byte, stride*4+stride)
				got := make([]byte, stride*4+stride)
				for i := range ref {
					ref[i] = 0xab
					got[i] = 0xab
				}

				switch mode {
				case common.BDCPred:
					intra4x4DCPredictScalar(ref, stride, above[:], left[:])
				case common.BTMPred:
					intra4x4TMPredictScalar(ref, stride, above[:], left[:], topLeft)
				case common.BVEPred:
					intra4x4VEPredictScalar(ref, stride, above[:], topLeft)
				case common.BHEPred:
					intra4x4HEPredictScalar(ref, stride, left[:], topLeft)
				case common.BLDPred:
					intra4x4LDPredictScalar(ref, stride, above[:])
				case common.BRDPred:
					intra4x4RDPredictScalar(ref, stride, above[:], left[:], topLeft)
				case common.BVRPred:
					intra4x4VRPredictScalar(ref, stride, above[:], left[:], topLeft)
				case common.BVLPred:
					intra4x4VLPredictScalar(ref, stride, above[:])
				case common.BHDPred:
					intra4x4HDPredictScalar(ref, stride, above[:], left[:], topLeft)
				case common.BHUPred:
					intra4x4HUPredictScalar(ref, stride, left[:])
				}

				if !Intra4x4Predict(got, stride, mode, above[:], left[:], topLeft) {
					t.Fatalf("Intra4x4Predict %s returned false", modeName(mode))
				}

				for y := 0; y < 4; y++ {
					for x := 0; x < 4; x++ {
						o := y*stride + x
						if got[o] != ref[o] {
							t.Fatalf("mode=%s stride=%d iter=%d above=%v left=%v topLeft=%d [%d,%d]: got=%d want=%d",
								modeName(mode), stride, iter, above, left, topLeft, x, y, got[o], ref[o])
						}
					}
					// Bytes outside the 4-wide block must remain
					// untouched: SIMD kernels write exactly 4 bytes
					// per row.
					for x := 4; x < stride; x++ {
						o := y*stride + x
						if got[o] != 0xab {
							t.Fatalf("mode=%s stride=%d iter=%d touched outside block at [%d,%d]: got=%d",
								modeName(mode), stride, iter, x, y, got[o])
						}
					}
				}
			}
		}
	}
}

// BenchmarkIntra4x4PredictByMode reports per-mode dispatch performance
// so the SIMD path on amd64/arm64 can be compared against the scalar
// baseline (build with -tags noasm to bypass SIMD if needed).
func BenchmarkIntra4x4PredictByMode(b *testing.B) {
	above := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	left := []byte{90, 100, 110, 120}
	dst := make([]byte, 16)

	cases := []struct {
		name string
		mode common.BPredictionMode
	}{
		{"DC", common.BDCPred},
		{"TM", common.BTMPred},
		{"VE", common.BVEPred},
		{"HE", common.BHEPred},
		{"LD", common.BLDPred},
		{"RD", common.BRDPred},
		{"VR", common.BVRPred},
		{"VL", common.BVLPred},
		{"HD", common.BHDPred},
		{"HU", common.BHUPred},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				Intra4x4Predict(dst, 4, tc.mode, above, left, 5)
			}
		})
	}
}
