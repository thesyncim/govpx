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
	if stride < 4 || len(input) < 3*stride+4 || len(output) < 16 {
		forwardDCT4x4Scalar(input, stride, output)
		return
	}
	forwardDCT4x4NEON(unsafe.SliceData(input), unsafe.SliceData(output), stride)
}

func forwardDCT8x8Dispatch(input []int16, stride int, output []int16) {
	if stride < 8 || len(input) < 7*stride+8 || len(output) < 64 {
		forwardDCT8x8Scalar(input, stride, output)
		return
	}
	forwardDCT8x8NEON(unsafe.SliceData(input), unsafe.SliceData(output), stride)
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

func forwardDCT32x32RDDispatch(input []int16, stride int, output []int16) {
	// PENDING: port libvpx v1.16.0 vpx_fdct32x32_rd_neon
	//   - kernel:  vpx_dsp/arm/fdct32x32_neon.c::vpx_fdct32x32_rd_neon
	//   - helpers: vpx_dsp/arm/fdct32x32_neon.h (shared with vpx_fdct32x32_neon)
	// Shares the same column/row pipeline as the precision variant; the
	// row pass invokes the `1<<round` halving stage rather than the
	// (out + 1 + (out<0)) >> 2 final shift. Will land alongside the
	// precision variant in the eventual NEON port.
	forwardDCT32x32RDScalar(input, stride, output)
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

//go:noescape
func forwardDCT8x8NEON(input *int16, output *int16, stride int)

//go:noescape
func forwardDCT4x4NEON(input *int16, output *int16, stride int)
