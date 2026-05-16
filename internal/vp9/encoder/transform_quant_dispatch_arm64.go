//go:build arm64 && !purego

package encoder

import "unsafe"

// ARMv8 NEON dispatchers for the VP9 forward transforms and quantizer.
// Each entry point either routes to a hand-coded NEON kernel or falls
// back to the canonical scalar reference. Pending kernels are documented
// inline with pointers to the libvpx v1.16.0 source that must be ported
// verbatim and to the NEON encoder helpers (neon_encoder_test.go) that
// the port should use for instruction-word generation.

func forwardDCT4x4Dispatch(input []int16, stride int, output []int16) {
	// PENDING: port libvpx v1.16.0 vpx_fdct4x4_neon
	//   - kernel:  vpx_dsp/arm/fdct4x4_neon.c::vpx_fdct4x4_neon
	//   - helpers: vpx_dsp/arm/fdct4x4_neon.h (vpx_fdct4x4_pass1_neon,
	//              vpx_fdct4x4_pass2_neon) and vpx_dsp/arm/fdct_neon.h
	//              (butterfly_one_coeff_s16_fast_half,
	//              butterfly_one_coeff_s16_s32_fast_narrow_half,
	//              butterfly_two_coeff_half).
	// The kernel relies on int32-widening through pass2 (SADDL/SSUBL +
	// SQRDMULHQ on the cospi_16_64*1<<17 constant) which is what the
	// prior attempt missed, so reproduce that path exactly. The required
	// NEON instruction encoders are pre-built and self-tested in
	// neon_encoder_test.go (enc_saddl_4s_4h, enc_ssubl_4s_4h,
	// enc_smull_4s_4h_by_elt, enc_smlal_4s_4h_by_elt,
	// enc_smlsl_4s_4h_by_elt, enc_sqrshrn_4h_from_4s_imm,
	// enc_sqrdmulh_4h, enc_shl_4h_imm, enc_sshr_4h_imm,
	// enc_trn1_4h/2s, enc_trn2_4h/2s, enc_dup_4h_from_w,
	// enc_movz_w_imm16, enc_ldrsh_w, enc_cbz_w, enc_ins_h_from_w,
	// enc_ld1_4h, enc_st1_8h_post).
	forwardDCT4x4Scalar(input, stride, output)
}

func forwardDCT8x8Dispatch(input []int16, stride int, output []int16) {
	// PENDING: port libvpx v1.16.0 vpx_fdct8x8_neon
	//   - kernel:  vpx_dsp/arm/fdct8x8_neon.c::vpx_fdct8x8_neon
	//   - helpers: vpx_dsp/arm/fdct8x8_neon.h
	// The 8x8 kernel reuses butterfly_two_coeff and add_round_shift_s16
	// from vpx_dsp/arm/fdct_neon.h. Same encoders as 4x4 plus the .8h
	// (Q=1) variants of each NEON op.
	forwardDCT8x8Scalar(input, stride, output)
}

func forwardDCT16x16Dispatch(input []int16, stride int, output []int16) {
	// PENDING: port libvpx v1.16.0 vpx_fdct16x16_neon
	//   - kernel:  vpx_dsp/arm/fdct16x16_neon.c::vpx_fdct16x16_neon
	//   - helpers: vpx_dsp/arm/fdct16x16_neon.h
	// Two-pass (horizontal then vertical) with 8x8 sub-block reuse.
	forwardDCT16x16Scalar(input, stride, output)
}

func forwardDCT32x32Dispatch(input []int16, stride int, output []int16) {
	// PENDING: port libvpx v1.16.0 vpx_fdct32x32_neon
	//   - kernel:  vpx_dsp/arm/fdct32x32_neon.c::vpx_fdct32x32_neon
	//   - helpers: vpx_dsp/arm/fdct32x32_neon.h (~2900 LOC of macros)
	// The largest kernel; ~1500 LOC of NEON intrinsics expanding to
	// thousands of raw NEON instructions. Best ported via a generator
	// (compute every WORD by calling the enc_* helpers) rather than by
	// hand transcription.
	forwardDCT32x32Scalar(input, stride, output)
}

func forwardWHT4x4Dispatch(input []int16, stride int, output []int16) {
	if len(input) < 3*stride+4 || len(output) < 16 || stride < 4 {
		forwardWHT4x4Scalar(input, stride, output)
		return
	}
	forwardWHT4x4NEON(unsafe.SliceData(input), stride, unsafe.SliceData(output))
}

func quantizeFPDispatch(coeff []int16, dequant [2]int16, scan []int16, dqcoeff []int16) int {
	// PENDING: port libvpx v1.16.0 vp9_quantize_fp_neon
	//   - kernel:  vp9/encoder/arm/neon/vp9_quantize_neon.c::vp9_quantize_fp_neon
	//   - helpers: quantize_fp_8, get_max_lane_eob, get_max_eob,
	//              update_fp_values, calculate_dqcoeff_and_store
	// The libvpx-shaped scalar entry point is QuantizeFPLibvpx
	// (transform_quant.go); both this legacy dispatch and the NEON kernel
	// funnel through quantizeFPLibvpxScalar so byte parity is automatic
	// once the NEON kernel is plumbed in here. The required SIMD encoders
	// are pre-built in neon_encoder_test.go.
	return quantizeFPScalar(coeff, dequant, scan, dqcoeff)
}

//go:noescape
func forwardWHT4x4NEON(input *int16, stride int, output *int16)

// Test helper (unconditional SIMD entry):
func forwardWHT4x4NEONTest(input []int16, stride int, output []int16) {
	forwardWHT4x4NEON(unsafe.SliceData(input), stride, unsafe.SliceData(output))
}
