//go:build arm64 && !purego

package encoder

import "unsafe"

// ARMv8 NEON dispatchers for the VP9 forward transforms and quantizer.
// Each entry point either routes to a hand-coded NEON kernel or falls
// back to the canonical scalar reference. Kernels still using the scalar
// fallback are listed inline so it is obvious which ports are pending.

func forwardDCT4x4Dispatch(input []int16, stride int, output []int16) {
	// NEON 4x4 forward DCT not yet ported — a faithful libvpx-style
	// port needs int32 widening through the column pass to preserve
	// byte-parity for the full ±1024 residual range. Falls back to
	// scalar for now.
	forwardDCT4x4Scalar(input, stride, output)
}

func forwardDCT8x8Dispatch(input []int16, stride int, output []int16) {
	// NEON 8x8 forward DCT not yet ported — falls back to scalar.
	forwardDCT8x8Scalar(input, stride, output)
}

func forwardDCT16x16Dispatch(input []int16, stride int, output []int16) {
	// NEON 16x16 forward DCT not yet ported — falls back to scalar.
	forwardDCT16x16Scalar(input, stride, output)
}

func forwardDCT32x32Dispatch(input []int16, stride int, output []int16) {
	// NEON 32x32 forward DCT not yet ported — falls back to scalar.
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
	// NEON quantize_fp port not yet ported. The naive 8-coef-at-a-time
	// NEON kernel hits a corner case in the eob calculation (q!=0 but
	// int16(q*dequant)==0 due to wraparound) that needs careful sentinel
	// tracking. Defer to scalar for now.
	return quantizeFPScalar(coeff, dequant, scan, dqcoeff)
}

//go:noescape
func forwardWHT4x4NEON(input *int16, stride int, output *int16)

// Test helper (unconditional SIMD entry):
func forwardWHT4x4NEONTest(input []int16, stride int, output []int16) {
	forwardWHT4x4NEON(unsafe.SliceData(input), stride, unsafe.SliceData(output))
}
