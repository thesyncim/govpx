package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestVP8UVResidualGatherParity pins task #294's negative finding
// for the ARNR audit pin-hold residual (BestQuality -5 bytes / GoodQuality
// -6 bytes on frame 1 inter, see
// vp8_kf_1280x720_ssim_best_arnr_parity_test.go and
// vp8_kf_1280x720_ssim_good_arnr_parity_test.go).
//
// HYPOTHESIS (final unexamined candidate per #292):
//
//	ResidualGather8x8PtrFast (internal/vp8/dsp/residual_gather_*).
//	may pack UV blocks in a different order than libvpx's
//	vp8_subtract_mbuv (vp8/encoder/encodemb.c:33-41), OR may slice
//	rows/cols differently within each 4x4 block. The downstream
//	fdct -> quantize -> tokens would produce different bytes for the
//	SAME predicted samples + SAME source if the residual array layout
//	differs between gather and DCT.
//
// AUDIT RESULT: the hypothesis is INCORRECT. govpx's UV residual gather
// is byte-faithful to libvpx v1.16.0 at the per-block sample level, even
// though the two implementations use STRUCTURALLY DIFFERENT in-memory
// layouts:
//
//   - libvpx vp8_subtract_mbuv (encodemb.c:33-41) writes the U residual
//     as one contiguous 8x8 plane at src_diff[256..320) with stride 8
//     (and V at src_diff[320..384)). vp8_setup_block_ptrs
//     (encodeframe.c:973-996) then aliases block[16+r*2+c].src_diff to
//     src_diff+256+r*4*8+c*4 for r,c in {0,1}, so each 4x4 sub-block
//     reads from the SAME 64-int16 8x8 plane with stride 8 (= 16-byte
//     pitch). vp8_transform_mbuv (encodemb.c:67-73) then runs
//     vp8_short_fdct8x4(block[i].src_diff, ..., pitch=16) for i in
//     {16, 18, 20, 22}, where short_fdct8x4 is two side-by-side
//     short_fdct4x4 calls each reading 4 rows of 4 cols at the supplied
//     stride.
//
//   - govpx ResidualGather8x8PtrFast
//     writes the U residual as 4 separate 4x4 blocks back-to-back
//     (block-major), 16 int16 per block, total 64. Each block is
//     row-major (col 0..3 across 4 rows). ForwardDCT4x4Batch
//     (internal/vp8/encoder/dct_batch.go:18) then runs
//     ForwardDCT4x4(block, stride=4, output) per block.
//
// The two structures are FUNCTIONALLY EQUIVALENT because the forward 4x4
// DCT depends only on the 16 input samples in their canonical (row, col)
// order, NOT on the in-memory stride between rows. Both pipelines feed
// the same 16 samples to vp8_short_fdct4x4_c in the same order:
//
//   - libvpx block 16: rows 0..3, cols 0..3 of U plane.
//     short_fdct4x4 reads ip[0..3] at ip += pitch/2 (= 8 shorts) per row.
//
//   - libvpx block 17: rows 0..3, cols 4..7 of U plane (input+4 of the
//     side-by-side short_fdct8x4 call).
//
//   - libvpx block 18: rows 4..7, cols 0..3 of U plane.
//
//   - libvpx block 19: rows 4..7, cols 4..7 of U plane.
//
//   - govpx block 0 (= U[16]): rows 0..3, cols 0..3 of U plane,
//     gathered into out[0..16] row-major with stride 4.
//     ForwardDCT4x4 reads input[0..3] at input += stride per row.
//
//   - govpx block 1 (= U[17]): rows 0..3, cols 4..7, into out[16..32].
//
//   - govpx block 2 (= U[18]): rows 4..7, cols 0..3, into out[32..48].
//
//   - govpx block 3 (= U[19]): rows 4..7, cols 4..7, into out[48..64].
//
// In ResidualGather8x8PtrFast (internal/vp8/dsp/residual_gather_other.go
// and the SIMD analogues for arm64/amd64), the block-iteration order is
// `for by in {0,1}: for bx in {0,1}: write block (by*2+bx) row-major`,
// which exactly maps to libvpx's block[16 + by*2 + bx] correspondence.
//
// Therefore the per-block 16 samples fed to vp8_short_fdct4x4_c are
// BYTE-IDENTICAL between govpx and libvpx for any (source, predictor)
// pair on the chroma planes. The downstream quantize + token-pack steps
// are deterministic functions of the per-block DCT output, so the
// pipelines are equivalent at every step.
//
// This audit asserts the equivalence by:
//
//	(1) Driving ResidualGather8x8PtrFast on a deterministic (src, pred)
//	    8x8 input and verifying that the 64-int16 output, viewed as 4
//	    contiguous 4x4 blocks, equals the 4 sub-blocks of a libvpx-style
//	    8x8 plane-diff written into a contiguous 8-stride buffer.
//
//	(2) Running ForwardDCT4x4 on each govpx block (stride=4) and
//	    ForwardDCT4x4 on the corresponding libvpx-layout 4x4 sub-region
//	    (stride=8) and verifying the 16-int16 outputs are byte-identical.
//	    This shows that the DCT is invariant under the layout change.
//
//	(3) Sweeping a non-trivial value range for both src and pred to
//	    cover positive, negative, and zero residual cells, plus the
//	    saturating boundary where (src - pred) lies in the full
//	    int8 range [-255, +255].
//
// CONCLUSION: the -5/-6 byte ARNR pin-hold is NOT explained by a UV
// residual gather slice-ordering divergence. The govpx gather + batched
// FDCT pipeline produces BYTE-IDENTICAL per-block DCT coefficients to
// the libvpx vp8_subtract_mbuv + vp8_transform_mbuv pipeline for any
// (source, predictor) chroma pair.
//
// This exhausts the static-inspection candidates for the ARNR pin-hold:
//
//	#282 optimize_b trellis              — byte-faithful (task #282)
//	#284 scalar paths                    — byte-faithful (task #284)
//	#286 NEON SIMD                       — byte-faithful (task #286)
//	#288 interRDCacheReusable UV-DCT     — byte-faithful (task #288)
//	#290 picker-vs-accepted act_zbin_adj — byte-faithful (task #290)
//	#292 chroma sub-pel predictor        — byte-faithful (task #292)
//	#294 UV residual gather              — byte-faithful (this task)
//
// NEXT INVESTIGATION STRATEGY: The remaining live divergence cannot be
// narrowed further by static inspection alone. The next step is to
// instrument a per-MB pre-trellis UV qcoeff trace via the C oracle
// (vpxenc-oracle) and govpx's oracle trace writer, then diff the two
// traces MB-by-MB on frame 1 of the Best/Good ARNR cohort. Concretely:
//
//	(a) Add a per-MB UV qcoeff dump to the oracle trace and to the
//	    vpxenc-oracle patched libvpx (the oracle SHA fingerprint needs
//	    to be rotated; see internal/coracle/build_vpxenc_oracle.sh and
//	    oracle_sha_test.go).
//	(b) Run TestVP8KF1280x720SSIMBestARNRParity with the per-MB
//	    trace enabled, sieve for the first MB where govpx UV qcoeff
//	    != libvpx UV qcoeff before trellis (so trellis-vs-fast-quant
//	    confusion is ruled out).
//	(c) Walk back from that MB through the candidate trail: the
//	    picker's selected mode, the chroma predictor sample buffer,
//	    and the actZbinAdj / zbin_mode_boost / zbinOverQuant inputs
//	    to the quantizer.
//
// Alternate strategies if (a)-(c) prove insufficient:
//
//   - Investigate float-precision arithmetic order in cost computation
//     (rdMult / rdDiv / RDCOST integer overflow boundaries that could
//     reorder candidate ranking on a near-tied RD score).
//   - Examine late state mutation between picker and accepted-mode
//     encode (any mutable global state on x->* or e->* read by the
//     picker AND mutated by the accepted-path before the picker's
//     coefficients land in coeffs.QCoeff).
//
// References:
//   - libvpx v1.16.0 vp8/encoder/encodemb.c:33-41 (vp8_subtract_mbuv).
//   - libvpx v1.16.0 vp8/encoder/encodemb.c:67-73 (vp8_transform_mbuv).
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:973-996
//     (vp8_setup_block_ptrs UV alias).
//   - libvpx v1.16.0 vp8/encoder/dct.c:15-58 (vp8_short_fdct4x4_c,
//     vp8_short_fdct8x4_c).
//   - libvpx v1.16.0 vpx_dsp/subtract.c:19-32 (vpx_subtract_block_c).
//   - libvpx v1.16.0 vp8/common/blockd.h (BLOCKD.src_diff aliasing).
//   - libvpx v1.16.0 vp8/encoder/rdopt.c:712-714, 731-733, 766-768
//     (vp8_subtract_mbuv inter / intra call sites).
//   - internal/vp8/encoder/residual_gather.go (GatherMacroblockUVResiduals4x4).
//   - vp8_encoder_inter_coefficients.go:476-492 (whole-MB UV gather + batch
//     FDCT dispatch).
//   - internal/vp8/dsp/residual_gather_other.go:39-61
//     (ResidualGather8x8PtrFast scalar reference).
//   - internal/vp8/dsp/residual_gather_arm64.go,
//     residual_gather_arm64.s (NEON kernel).
//   - internal/vp8/dsp/residual_gather_amd64.go,
//     residual_gather_amd64.s (SSE2 kernel).
//   - internal/vp8/encoder/dct.go:5-43 (ForwardDCT4x4, scalar reference).
//   - internal/vp8/encoder/dct_batch.go:18-20 (ForwardDCT4x4Batch).
//   - vp8_kf_1280x720_ssim_best_arnr_parity_test.go (BestQuality pin).
//   - vp8_kf_1280x720_ssim_good_arnr_parity_test.go (GoodQuality pin).
func TestVP8UVResidualGatherParity(t *testing.T) {
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
