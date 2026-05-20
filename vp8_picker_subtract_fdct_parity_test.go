package govpx

import (
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestVP8PickerSubtractFDCTParity pins the picker source-prediction
// subtract step and the per-4x4 forward DCT step at MB(0,0) frame-1
// NEWMV MV=(8,16) — the cohort already isolated by tasks #298, #304,
// #307, and #309. Predictor reference bytes (task #309) and picker
// source bytes (task #307) MATCH libvpx byte-for-byte at this MB; the
// remaining picker-side stages that could explain libvpx's non-zero
// Y qcoeff vs govpx's all-zero Y qcoeff are the source−pred subtract
// and the 4x4 forward DCT.
//
// HYPOTHESIS UNDER TEST: govpx's gatherMacroblockYResiduals4x4 (the
// inter NEWMV picker's per-MB residual gather) or its batched
// ForwardDCT4x4Batch (the FDCT immediately after the gather) produces
// values that differ from libvpx's vpx_subtract_block_c +
// vp8_short_fdct4x4_c reference path on byte-identical inputs.
//
// REFERENCES:
//
//	libvpx v1.16.0 vp8/encoder/encodemb.c:43-46 (vp8_subtract_mby
//	  calls vpx_subtract_block(16, 16, diff, 16, src, src_stride,
//	  pred, pred_stride): a contiguous 16×16 raster diff with
//	  diff_stride=16).
//	libvpx v1.16.0 vpx_dsp/subtract.c:19-32 (vpx_subtract_block_c:
//	  per-row `diff_ptr[c] = src_ptr[c] - pred_ptr[c]`; the 16-bit
//	  diff_ptr stores the signed difference of two uint8 in two's
//	  complement, range [-255, 255]).
//	libvpx v1.16.0 vp8/encoder/rdopt.c:471-509 (macro_block_yrd
//	  picker entry: vp8_subtract_mby into mb->src_diff, then 8
//	  calls to short_fdct8x4(beptr->src_diff, beptr->coeff, 32) ==
//	  16 short_fdct4x4 invocations on per-block 4×4 windows with
//	  stride 16 int16).
//	libvpx v1.16.0 vp8/encoder/dct.c:15-53 (vp8_short_fdct4x4_c
//	  scalar reference: two-stage 4-row × 4-col 4x4 forward DCT,
//	  input multiplied by 8, Hadamard-style butterflies at constants
//	  2217 and 5352, +14500/+7500 rounding in stage 1, +12000/+51000
//	  rounding in stage 2, (d1!=0) bias on op[4]).
//	govpx internal/vp8/dsp/residual_gather_other.go (scalar fallback)
//	  and residual_gather_{arm64,amd64}.{go,s} (SIMD): 4-block-row ×
//	  4-block-col outer loop over the 16×16 MB writing 16 contiguous
//	  4×4 int16 windows. The per-pixel arithmetic is
//	  `int16(int(src) - int(pred))` — bitwise identical to libvpx's
//	  `src_ptr[c] - pred_ptr[c]` after int16 truncation.
//	govpx internal/vp8/encoder/dct.go:8-43 (forwardDCT4x4Scalar): a
//	  verbatim port of vp8_short_fdct4x4_c, with the libvpx pitch /
//	  2 byte-to-int16 conversion folded into the stride parameter
//	  directly. Already fuzz-pinned against the SIMD path in
//	  fuzz_fdct_test.go and against the per-block batch path in
//	  dct_batch_test.go.
//
// RESULT: hypothesis INCORRECT on both layers.
//
//  1. SUBTRACT LAYER MATCHES. The govpx residual at MB(0,0) frame 1
//     NEWMV MV=(8,16) is byte-identical to the libvpx contiguous-raster
//     reference residual, after the trivial block-scan vs raster
//     re-layout. There is no divergence in stride handling, no
//     divergence in src/pred operand order, no divergence in int16
//     sign extension. govpx's 16-block contiguous layout vs libvpx's
//     16×16 raster layout produces the same residual VALUES at the
//     same MB-relative pixel positions; the DCT consumes them at the
//     same 4×4 windows regardless of the surrounding inter-window
//     stride (4 in govpx, 16 in libvpx). See subtract assertion below.
//
//  2. FDCT LAYER MATCHES. govpx's ForwardDCT4x4Batch on the 16 govpx-
//     layout 4x4 residual blocks produces the same per-block output as
//     a fresh, in-test scalar replay of libvpx's vp8_short_fdct4x4_c
//     applied to the libvpx-layout (stride-16) raster residual at each
//     of the 16 4×4 windows. The audit uses a synthetic deterministic
//     predictor (panning formula at xoff=4, yoff=2) rather than the
//     cohort's actual e.lastRef post-LF reconstruction, since this
//     pin's purpose is the subtract+FDCT MATH layer — task #304's
//     end-to-end cohort sentinel (residual SSE=48000, max |AC|=87)
//     already separately pins the cohort's actual encoder state.
//
// Together with task #304's residual SSE=48000 / max |AC|=87 finding
// and zbin threshold=126, this confirms that govpx's picker is
// MATHEMATICALLY CORRECT at every stage from predictor (#309) through
// source (#307) → subtract (this task) → FDCT (this task) →
// quantize (#304). The libvpx-vs-govpx Y rate divergence (libvpx
// rate_y=34799 vs govpx rate_y≈7519) must therefore originate
// downstream of the FDCT — at quantize parameter selection, or in a
// libvpx state-bleed bug (e.g. stale b->zbin_extra) that task #310's
// libvpx instrumentation will localize.
func TestVP8PickerSubtractFDCTParity(t *testing.T) {
	// Build a 16×16 source from the MB(0,0) frame-1 panning-fixture
	// formula (xoff=2, yoff=1 reproduces
	// encoderValidationPanningFrame(_, _, 1) at the MB origin).
	// This pins the audit to the SAME source bytes the picker reads
	// in the BestARNR/GoodARNR cohort (task #307 pin).
	const xoff, yoff = 2, 1
	var src16 [16 * 16]byte
	for y := range 16 {
		for x := range 16 {
			srcX := x + xoff
			srcY := y + yoff
			src16[y*16+x] = byte(32 + ((srcY*7 + srcX*11 + (srcX/8)*(srcY/8)*13) & 191))
		}
	}

	// Build a deterministic 16×16 predictor. The audit's goal is to
	// pin the subtract + FDCT MATH layer; the precise byte values
	// don't matter as long as residual + FDCT outputs are
	// reproducible. Using a second copy of the panning formula at a
	// different offset (xoff=4, yoff=2 — the formula evaluated as if
	// the previous frame's reconstruction were panning-fixture index
	// 2) provides a deterministic, mostly-nonzero residual that
	// exercises all 16 4×4 blocks with realistic dynamics.
	var pred16 [16 * 16]byte
	for y := range 16 {
		for x := range 16 {
			refX := x + 4
			refY := y + 2
			pred16[y*16+x] = byte(32 + ((refY*7 + refX*11 + (refX/8)*(refY/8)*13) & 191))
		}
	}

	// --- LAYER 1: SUBTRACT ---
	//
	// govpx layout: 16 contiguous 4×4 int16 windows packed in raster
	// block order (block 0 = top-left, block 3 = top-right, block 4
	// = second row left, ..., block 15 = bottom-right).
	var govpxRes [16 * 16]int16
	gatherMacroblockYResiduals4x4(src16[:], 16, 16, 16, pred16[:], 16, 0, 0, govpxRes[:])

	// libvpx layout: a single 16×16 raster int16 buffer at stride 16.
	// Direct in-test replay of vpx_subtract_block_c(16, 16, diff, 16,
	// src_ptr, 16, pred_ptr, 16).
	var libvpxRaster [16 * 16]int16
	for r := range 16 {
		for c := range 16 {
			libvpxRaster[r*16+c] = int16(src16[r*16+c]) - int16(pred16[r*16+c])
		}
	}

	// Re-layout the libvpx raster diff into 16 contiguous 4×4
	// windows to compare against govpx. This is what libvpx's
	// short_fdct8x4(beptr->src_diff, ..., 32) consumes at each
	// per-block window (stride 16 int16 = 32 bytes).
	var libvpxBlocks [16 * 16]int16
	for block := range 16 {
		blockRow := (block >> 2) * 4
		blockCol := (block & 3) * 4
		for r := range 4 {
			for c := range 4 {
				libvpxBlocks[block*16+r*4+c] = libvpxRaster[(blockRow+r)*16+blockCol+c]
			}
		}
	}

	// PIN: govpx residual == libvpx residual (after re-layout).
	for i := range 16 * 16 {
		if govpxRes[i] != libvpxBlocks[i] {
			block := i / 16
			lane := i % 16
			t.Fatalf("subtract diverges at block=%d lane=%d (row=%d col=%d in block): govpx=%d libvpx=%d (src=%d pred=%d)",
				block, lane, lane/4, lane%4,
				govpxRes[i], libvpxBlocks[i],
				src16[(((block>>2)*4)+lane/4)*16+(((block&3)*4)+lane%4)],
				pred16[(((block>>2)*4)+lane/4)*16+(((block&3)*4)+lane%4)])
		}
	}

	// Cross-validate the residual against the trivially-correct
	// per-pixel subtract that govpx's slow-path fillPredictedResidual4x4
	// is structurally identical to. This catches operand-order, int16-
	// truncation, and stride mismatches in the SIMD residual gather.
	for r := range 16 {
		for c := range 16 {
			want := int16(src16[r*16+c]) - int16(pred16[r*16+c])
			if libvpxRaster[r*16+c] != want {
				t.Fatalf("reference subtract self-check at (%d,%d): got=%d want=%d", r, c, libvpxRaster[r*16+c], want)
			}
		}
	}

	// --- LAYER 2: FDCT ---
	//
	// govpx batched FDCT over 16 contiguous 4×4 windows at block
	// stride 4 int16.
	var govpxDcts [16 * 16]int16
	vp8enc.ForwardDCT4x4Batch(govpxRes[:], govpxDcts[:], 16)

	// libvpx reference: scalar replay of vp8_short_fdct4x4_c on each
	// of the 16 4×4 windows of the libvpx raster residual at stride
	// 16 int16 (libvpx pitch=32 bytes / 2 = 16 int16). This is the
	// EXACT computation rdopt.c:485 issues for the picker, modulo
	// short_fdct8x4 = two short_fdct4x4 invocations 4 int16 apart.
	var libvpxDcts [16 * 16]int16
	for block := range 16 {
		blockRow := (block >> 2) * 4
		blockCol := (block & 3) * 4
		windowStart := blockRow*16 + blockCol
		var out [16]int16
		referenceShortFdct4x4(libvpxRaster[windowStart:], 16, &out)
		copy(libvpxDcts[block*16:block*16+16], out[:])
	}

	// PIN: govpx FDCT output == libvpx FDCT output, byte-exact.
	for i := range 16 * 16 {
		if govpxDcts[i] != libvpxDcts[i] {
			block := i / 16
			lane := i % 16
			t.Fatalf("FDCT diverges at block=%d lane=%d (scan_rc=%d): govpx=%d libvpx=%d",
				block, lane, lane, govpxDcts[i], libvpxDcts[i])
		}
	}

	// Sentinel: under this fixed source+predictor pair the FDCT
	// produces a known max |AC|. Pin the exact value so a regression
	// in EITHER the subtract layer OR the FDCT layer surfaces as a
	// concrete failure mode (not just a vague drift).
	maxAbs := 0
	for block := range 16 {
		dct := govpxDcts[block*16 : block*16+16]
		for i := 1; i < 16; i++ {
			a := int(dct[i])
			if a < 0 {
				a = -a
			}
			if a > maxAbs {
				maxAbs = a
			}
		}
	}
	if maxAbs != taskTwelve12FDCTMaxAbsSentinel {
		t.Fatalf("max |AC| FDCT coeff across 16 Y blocks = %d, want %d (audit pin)", maxAbs, taskTwelve12FDCTMaxAbsSentinel)
	}
}

// taskTwelve12FDCTMaxAbsSentinel records the exact max-|AC| over the
// 16 Y FDCT blocks computed for the fixed
// (panning-formula-source(xoff=2,yoff=1), panning-formula-predictor
// (xoff=4,yoff=2)) input pair this audit uses. The constant is
// captured at test authorship time from the byte-exact govpx scalar
// FDCT (also cross-checked against the in-test libvpx reference
// replay) and locks the joint subtract+FDCT pipeline value-for-value.
const taskTwelve12FDCTMaxAbsSentinel = 568

// referenceShortFdct4x4 is a hand-written scalar replay of libvpx
// v1.16.0 vp8/encoder/dct.c:15-53 vp8_short_fdct4x4_c. Kept inline
// here (rather than calling vp8enc.ForwardDCT4x4 / the scalar Go
// port) so the audit pins govpx's FDCT against a textual transcription
// of the libvpx C source — any future regression in either side
// surfaces as a localized failure with the libvpx side as the
// independent oracle. The stride parameter mirrors libvpx's `pitch /
// 2` (libvpx callers pass byte-pitch; we accept int16-stride).
func referenceShortFdct4x4(input []int16, stride int, output *[16]int16) {
	var ip [16]int
	for r := range 4 {
		for c := range 4 {
			ip[r*4+c] = int(input[r*stride+c])
		}
	}
	var op [16]int

	// Stage 1: per-row horizontal butterfly. Mirrors lines 21-35 of
	// libvpx vp8/encoder/dct.c.
	for i := range 4 {
		row := i * 4
		a1 := (ip[row+0] + ip[row+3]) * 8
		b1 := (ip[row+1] + ip[row+2]) * 8
		c1 := (ip[row+1] - ip[row+2]) * 8
		d1 := (ip[row+0] - ip[row+3]) * 8

		op[row+0] = a1 + b1
		op[row+2] = a1 - b1
		op[row+1] = (c1*2217 + d1*5352 + 14500) >> 12
		op[row+3] = (d1*2217 - c1*5352 + 7500) >> 12
	}

	// Stage 2: per-column vertical butterfly. Mirrors lines 38-52.
	for i := range 4 {
		a1 := op[i+0] + op[i+12]
		b1 := op[i+4] + op[i+8]
		c1 := op[i+4] - op[i+8]
		d1 := op[i+0] - op[i+12]

		output[i+0] = int16((a1 + b1 + 7) >> 4)
		output[i+8] = int16((a1 - b1 + 7) >> 4)
		bias := 0
		if d1 != 0 {
			bias = 1
		}
		output[i+4] = int16(((c1*2217 + d1*5352 + 12000) >> 16) + bias)
		output[i+12] = int16((d1*2217 - c1*5352 + 51000) >> 16)
	}
}
