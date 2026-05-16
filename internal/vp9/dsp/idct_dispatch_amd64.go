//go:build amd64 && !purego

package dsp

import "unsafe"

// AMD64 SSE2 dispatchers for the VP9 inverse transforms. Each exported
// Idct/Iwht/Iht entry point runs the same scalar 1-D butterflies as
// the !arm64 path (since byte parity with libvpx hinges on the
// stage-by-stage int16 truncation in those kernels) but pushes the
// per-row column add+clip into SSE2 via PADDW/PSRAW + PACKUSWB.

// Idct4x4_16Add applies the full 4x4 inverse DCT to a 16-coefficient
// block and adds the result onto dest.
func Idct4x4_16Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 4, 4) {
		idct4x4_16AddScalar(input, dest, stride)
		return
	}
	var residual [16]int16
	idct4x4Residual(input, &residual)
	idctAddResidualRows4SSE2(unsafe.SliceData(dest), stride, &residual[0], 4)
}

// Idct4x4_1Add is the DC-only fast path. Defers to scalar; the SSE2
// agent intentionally leaves this with the scalar reference because
// the broadcast-add kernel is dominated by the 16-byte store cost
// rather than the math.
func Idct4x4_1Add(input []int16, dest []uint8, stride int) {
	idct4x4_1AddScalar(input, dest, stride)
}

// Idct8x8_64Add applies the full 8x8 inverse DCT.
func Idct8x8_64Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 8, 8) {
		idct8x8_64AddScalar(input, dest, stride)
		return
	}
	var residual [64]int16
	idct8x8Residual(input, &residual, 8)
	idctAddResidualRows8SSE2(unsafe.SliceData(dest), stride, &residual[0], 8)
}

// Idct8x8_12Add is the sparse upper-left-4x4 fast path.
func Idct8x8_12Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 8, 8) {
		idct8x8_12AddScalar(input, dest, stride)
		return
	}
	var residual [64]int16
	idct8x8Residual(input, &residual, 4)
	idctAddResidualRows8SSE2(unsafe.SliceData(dest), stride, &residual[0], 8)
}

// Idct8x8_1Add is the DC-only fast path.
func Idct8x8_1Add(input []int16, dest []uint8, stride int) {
	idct8x8_1AddScalar(input, dest, stride)
}

// Idct16x16_256Add applies the full 16x16 inverse DCT.
func Idct16x16_256Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 16, 16) {
		idct16x16_256AddScalar(input, dest, stride)
		return
	}
	var residual [256]int16
	idct16x16Residual(input, &residual, 16)
	idctAddResidualRows16SSE2(unsafe.SliceData(dest), stride, &residual[0], 16)
}

// Idct16x16_38Add is the upper-left-8x8 sparse fast path.
func Idct16x16_38Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 16, 16) {
		idct16x16_38AddScalar(input, dest, stride)
		return
	}
	var residual [256]int16
	idct16x16Residual(input, &residual, 8)
	idctAddResidualRows16SSE2(unsafe.SliceData(dest), stride, &residual[0], 16)
}

// Idct16x16_10Add is the upper-left-4x4 sparse fast path.
func Idct16x16_10Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 16, 16) {
		idct16x16_10AddScalar(input, dest, stride)
		return
	}
	var residual [256]int16
	idct16x16Residual(input, &residual, 4)
	idctAddResidualRows16SSE2(unsafe.SliceData(dest), stride, &residual[0], 16)
}

// Idct16x16_1Add is the DC-only fast path.
func Idct16x16_1Add(input []int16, dest []uint8, stride int) {
	idct16x16_1AddScalar(input, dest, stride)
}

// Idct32x32_1024Add is the dense 32x32 inverse DCT.
func Idct32x32_1024Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 32, 32) {
		idct32x32_1024AddScalar(input, dest, stride)
		return
	}
	var residual [1024]int16
	idct32x32Residual(input, &residual, 32)
	idctAddResidualRows32SSE2(unsafe.SliceData(dest), stride, &residual[0], 32)
}

// Idct32x32_135Add is the upper-left-16x16 sparse fast path.
func Idct32x32_135Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 32, 32) {
		idct32x32_135AddScalar(input, dest, stride)
		return
	}
	var residual [1024]int16
	idct32x32Residual(input, &residual, 16)
	idctAddResidualRows32SSE2(unsafe.SliceData(dest), stride, &residual[0], 32)
}

// Idct32x32_34Add is the upper-left-8x8 sparse fast path.
func Idct32x32_34Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 32, 32) {
		idct32x32_34AddScalar(input, dest, stride)
		return
	}
	var residual [1024]int16
	idct32x32Residual(input, &residual, 8)
	idctAddResidualRows32SSE2(unsafe.SliceData(dest), stride, &residual[0], 32)
}

// Idct32x32_1Add is the DC-only fast path.
func Idct32x32_1Add(input []int16, dest []uint8, stride int) {
	idct32x32_1AddScalar(input, dest, stride)
}

// Iwht4x4_16Add applies the lossless inverse 4x4 Walsh-Hadamard.
func Iwht4x4_16Add(input []int16, dest []uint8, stride int) {
	// The IWHT residual range exceeds what dctAddResidualRows4SSE2's
	// >>4 shift+saturate path expects; defer to scalar so the lossless
	// mode stays byte-exact.
	iwht4x4_16AddScalar(input, dest, stride)
}

// Iwht4x4_1Add is the DC-only fast path for the lossless 4x4 IWHT.
func Iwht4x4_1Add(input []int16, dest []uint8, stride int) {
	iwht4x4_1AddScalar(input, dest, stride)
}

// Iht4x4_16Add dispatches the 2-D hybrid 4x4 inverse transform.
func Iht4x4_16Add(input []int16, dest []uint8, stride int, txType int) {
	if txType == 0 {
		Idct4x4_16Add(input, dest, stride)
		return
	}
	if !dcWindowOK(dest, stride, 4, 4) {
		iht4x4_16AddScalar(input, dest, stride, txType)
		return
	}
	var residual [16]int16
	iht4x4Residual(input, &residual, txType)
	idctAddResidualRows4SSE2(unsafe.SliceData(dest), stride, &residual[0], 4)
}

// Iht8x8_64Add dispatches the 2-D hybrid 8x8 inverse transform.
func Iht8x8_64Add(input []int16, dest []uint8, stride int, txType int) {
	if txType == 0 {
		Idct8x8_64Add(input, dest, stride)
		return
	}
	if !dcWindowOK(dest, stride, 8, 8) {
		iht8x8_64AddScalar(input, dest, stride, txType)
		return
	}
	var residual [64]int16
	iht8x8Residual(input, &residual, txType)
	idctAddResidualRows8SSE2(unsafe.SliceData(dest), stride, &residual[0], 8)
}

// Iht16x16_256Add dispatches the 2-D hybrid 16x16 inverse transform.
func Iht16x16_256Add(input []int16, dest []uint8, stride int, txType int) {
	if txType == 0 {
		Idct16x16_256Add(input, dest, stride)
		return
	}
	if !dcWindowOK(dest, stride, 16, 16) {
		iht16x16_256AddScalar(input, dest, stride, txType)
		return
	}
	var residual [256]int16
	iht16x16Residual(input, &residual, txType)
	idctAddResidualRows16SSE2(unsafe.SliceData(dest), stride, &residual[0], 16)
}

// dcWindowOK validates that dest can be read/written as an NxN block
// at the given stride. Mirrors the helper in idct_dispatch_arm64.go.
func dcWindowOK(dest []uint8, stride, w, h int) bool {
	if stride < w {
		return false
	}
	limit := (h-1)*stride + w
	return limit > 0 && limit <= len(dest)
}

// SSE2 column-add kernels — see idct_amd64.s.

//go:noescape
func idctAddResidualRows4SSE2(dest *byte, stride int, residual *int16, nRows int)

//go:noescape
func idctAddResidualRows8SSE2(dest *byte, stride int, residual *int16, nRows int)

//go:noescape
func idctAddResidualRows16SSE2(dest *byte, stride int, residual *int16, nRows int)

//go:noescape
func idctAddResidualRows32SSE2(dest *byte, stride int, residual *int16, nRows int)
