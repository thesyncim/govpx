package encoder_test

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestUVResidualGatherMatchesLibvpxBlockLayout proves that the Go UV residual
// gather layout feeds the same 4x4 samples to the FDCT as libvpx's
// vp8_subtract_mbuv plus vp8_transform_mbuv path.
//
// libvpx stores each chroma plane as one 8x8 residual buffer with stride 8 and
// points blocks 16..19 at 4x4 windows within that buffer. The Go encoder stores
// the same four windows as contiguous 4x4 blocks. The layouts differ in memory,
// but each block must expose identical row-major samples and identical FDCT
// output. This test keeps that contract local to the encoder package instead of
// leaving it in the public facade test suite.
func TestUVResidualGatherMatchesLibvpxBlockLayout(t *testing.T) {
	// Deterministic 8x8 source and predictor planes covering positive,
	// negative, and zero residual cells. We allocate slightly larger
	// strides than 8 so the test catches any out-of-row read.
	const srcStride = 24
	const predStride = 17
	src := make([]byte, srcStride*8)
	pred := make([]byte, predStride*8)
	for r := range 8 {
		for c := range 8 {
			// src ramps 0..255 across the 8x8; pred is a deterministic
			// pattern that yields positive, negative, and zero residual
			// cells across the block.
			src[r*srcStride+c] = byte((r*32 + c*4) & 0xFF)
			pred[r*predStride+c] = byte(((r*8 + c*16) ^ 0x55) & 0xFF)
		}
	}

	// (1) Run the production fast-path gather.
	var got [4 * 16]int16
	dsp.ResidualGather8x8PtrFast(&src[0], srcStride, &pred[0], predStride, &got[0])

	// (1') Reference: build the libvpx-style 8x8 plane-diff buffer with
	// stride 8 (vpx_subtract_block_c semantics), then read out the 4
	// sub-blocks at libvpx's block[16+r*2+c].src_diff offsets:
	//
	//	block[16]: rows 0..3, cols 0..3 -> plane[ 0.. 4)+stride*0..3
	//	block[17]: rows 0..3, cols 4..7 -> plane[ 4.. 8)+stride*0..3
	//	block[18]: rows 4..7, cols 0..3 -> plane[ 0.. 4)+stride*4..7
	//	block[19]: rows 4..7, cols 4..7 -> plane[ 4.. 8)+stride*4..7
	var plane [64]int16
	for r := range 8 {
		for c := range 8 {
			plane[r*8+c] = int16(int(src[r*srcStride+c]) - int(pred[r*predStride+c]))
		}
	}
	for by := range 2 {
		for bx := range 2 {
			blockIdx := by*2 + bx
			for r := range 4 {
				for c := range 4 {
					libvpxSample := plane[(by*4+r)*8+(bx*4+c)]
					govpxSample := got[blockIdx*16+r*4+c]
					if libvpxSample != govpxSample {
						t.Fatalf("UV residual sample skew at block=%d r=%d c=%d: govpx=%d libvpx=%d",
							blockIdx, r, c, govpxSample, libvpxSample)
					}
				}
			}
		}
	}

	// (2) FDCT invariance under input layout. Run ForwardDCT4x4 on each
	// govpx block (stride=4, contiguous) and on the corresponding 4x4
	// sub-region of the libvpx-style 8x8 plane (stride=8) and verify
	// byte-identical DCT outputs.
	for by := range 2 {
		for bx := range 2 {
			blockIdx := by*2 + bx
			// govpx layout: block at got[blockIdx*16..blockIdx*16+16],
			// stride 4 (contiguous 4x4).
			var govpxDCT [16]int16
			vp8enc.ForwardDCT4x4(got[blockIdx*16:blockIdx*16+16], 4, &govpxDCT)
			// libvpx layout: block at plane[(by*4)*8 + (bx*4)..],
			// stride 8 (8x8 plane).
			var libvpxDCT [16]int16
			libvpxInput := plane[(by*4)*8+(bx*4):]
			vp8enc.ForwardDCT4x4(libvpxInput, 8, &libvpxDCT)
			for i := range 16 {
				if govpxDCT[i] != libvpxDCT[i] {
					t.Fatalf("UV FDCT4x4 layout-invariance skew at block=%d coeff=%d: govpx=%d libvpx=%d",
						blockIdx, i, govpxDCT[i], libvpxDCT[i])
				}
			}
		}
	}

	// (3) Per-block sample-order pin: within each 4x4 block, the gather
	// output's i-th int16 (i in 0..16) must be (src[r][c] - pred[r][c])
	// where r = i / 4 and c = i % 4 of THAT block. This catches any
	// future regression that transposes rows<->cols inside a block.
	for by := range 2 {
		for bx := range 2 {
			blockIdx := by*2 + bx
			for i := range 16 {
				r := i / 4
				c := i % 4
				wantRow := by*4 + r
				wantCol := bx*4 + c
				want := int16(int(src[wantRow*srcStride+wantCol]) - int(pred[wantRow*predStride+wantCol]))
				if got[blockIdx*16+i] != want {
					t.Fatalf("UV gather row-major order skew at block=%d i=%d (r=%d c=%d): got=%d want=%d",
						blockIdx, i, r, c, got[blockIdx*16+i], want)
				}
			}
		}
	}

	// (4) Bordering: zero src and pred (full-zero residual). All 64
	// residual samples must be zero, regardless of stride.
	for i := range src {
		src[i] = 0
	}
	for i := range pred {
		pred[i] = 0
	}
	var zeros [4 * 16]int16
	dsp.ResidualGather8x8PtrFast(&src[0], srcStride, &pred[0], predStride, &zeros[0])
	for i := range zeros {
		if zeros[i] != 0 {
			t.Fatalf("UV gather zero-input skew at i=%d: got=%d want=0", i, zeros[i])
		}
	}

	// (5) Full-saturating residual: src=255, pred=0 -> every cell = +255.
	for i := range src {
		src[i] = 0xFF
	}
	for i := range pred {
		pred[i] = 0
	}
	var posSat [4 * 16]int16
	dsp.ResidualGather8x8PtrFast(&src[0], srcStride, &pred[0], predStride, &posSat[0])
	for i := range posSat {
		if posSat[i] != 255 {
			t.Fatalf("UV gather +sat skew at i=%d: got=%d want=255", i, posSat[i])
		}
	}

	// (6) Negative-saturating residual: src=0, pred=255 -> every cell = -255.
	for i := range src {
		src[i] = 0
	}
	for i := range pred {
		pred[i] = 0xFF
	}
	var negSat [4 * 16]int16
	dsp.ResidualGather8x8PtrFast(&src[0], srcStride, &pred[0], predStride, &negSat[0])
	for i := range negSat {
		if negSat[i] != -255 {
			t.Fatalf("UV gather -sat skew at i=%d: got=%d want=-255", i, negSat[i])
		}
	}
}
