//go:build arm64 && !purego

package dsp

import "unsafe"

// ARMv8 NEON dispatchers for the VP9 inverse transforms. Each exported
// Idct/Iwht/Iht entry point either drops into a hand-coded NEON kernel
// or falls back to the canonical scalar reference defined in the
// idct*.go / iwht4.go / iadst*.go files. Kernels not yet ported use the
// scalar reference directly; they will be replaced one at a time as
// the NEON implementations land.

// Idct4x4_16Add applies the full 4x4 inverse DCT to a 16-coefficient
// block and adds the result onto dest. The hot per-pixel add+clip is
// done via a NEON kernel that operates one row at a time over an
// already-transformed residual buffer.
func Idct4x4_16Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 4, 4) {
		idct4x4_16AddScalar(input, dest, stride)
		return
	}
	var residual [16]int16
	idct4x4Residual(input, &residual)
	idctAddResidualRows4NEON(unsafe.SliceData(dest), stride, &residual[0], 4)
}

// Idct4x4_1Add is the DC-only fast path for the 4x4 inverse DCT. NEON
// kernel: computes a1 in scalar (two cospi_16_64 multiplies + a
// roundPowerOfTwo) then broadcasts a1 across a 4x4 saturating-add over
// dest.
func Idct4x4_1Add(input []int16, dest []uint8, stride int) {
	a1 := idctDcA1(int64(input[0]), 4)
	if !dcWindowOK(dest, stride, 4, 4) {
		// Slow path: stride too large for the read window. Defer to
		// the always-safe scalar reference.
		idct4x4_1AddScalar(input, dest, stride)
		return
	}
	idct4x4DcAddNEON(unsafe.SliceData(dest), stride, int16(a1))
}

// Idct8x8_64Add applies the full 8x8 inverse DCT.
func Idct8x8_64Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 8, 8) {
		idct8x8_64AddScalar(input, dest, stride)
		return
	}
	idct8x8_64AddNEON(unsafe.SliceData(input), unsafe.SliceData(dest), stride)
}

// Idct8x8_12Add is the sparse upper-left-4x4 fast path for the 8x8
// inverse DCT.
func Idct8x8_12Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 8, 8) {
		idct8x8_12AddScalar(input, dest, stride)
		return
	}
	idct8x8_12AddNEON(unsafe.SliceData(input), unsafe.SliceData(dest), stride)
}

// Idct8x8_1Add is the DC-only fast path. NEON kernel: scalar a1
// computation + 8x8 broadcast saturating-add.
func Idct8x8_1Add(input []int16, dest []uint8, stride int) {
	a1 := idctDcA1(int64(input[0]), 5)
	if !dcWindowOK(dest, stride, 8, 8) {
		idct8x8_1AddScalar(input, dest, stride)
		return
	}
	idct8x8DcAddNEON(unsafe.SliceData(dest), stride, int16(a1))
}

// Idct16x16_256Add applies the full 16x16 inverse DCT.
func Idct16x16_256Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 16, 16) {
		idct16x16_256AddScalar(input, dest, stride)
		return
	}
	var residual [256]int16
	idct16x16Residual(input, &residual, 16)
	idctAddResidualRows16NEON(unsafe.SliceData(dest), stride, &residual[0], 16)
}

// Idct16x16_38Add is the upper-left-8x8 sparse fast path.
func Idct16x16_38Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 16, 16) {
		idct16x16_38AddScalar(input, dest, stride)
		return
	}
	var residual [256]int16
	idct16x16Residual(input, &residual, 8)
	idctAddResidualRows16NEON(unsafe.SliceData(dest), stride, &residual[0], 16)
}

// Idct16x16_10Add is the upper-left-4x4 sparse fast path.
func Idct16x16_10Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 16, 16) {
		idct16x16_10AddScalar(input, dest, stride)
		return
	}
	var residual [256]int16
	idct16x16Residual(input, &residual, 4)
	idctAddResidualRows16NEON(unsafe.SliceData(dest), stride, &residual[0], 16)
}

// Idct16x16_1Add is the DC-only fast path. NEON kernel: scalar a1
// computation + 16x16 broadcast saturating-add.
func Idct16x16_1Add(input []int16, dest []uint8, stride int) {
	a1 := idctDcA1(int64(input[0]), 6)
	if !dcWindowOK(dest, stride, 16, 16) {
		idct16x16_1AddScalar(input, dest, stride)
		return
	}
	idct16x16DcAddNEON(unsafe.SliceData(dest), stride, int16(a1))
}

// Idct32x32_1024Add is the dense 32x32 inverse DCT.
func Idct32x32_1024Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 32, 32) {
		idct32x32_1024AddScalar(input, dest, stride)
		return
	}
	var residual [1024]int16
	idct32x32Residual(input, &residual, 32)
	idctAddResidualRows32NEON(unsafe.SliceData(dest), stride, &residual[0], 32)
}

// Idct32x32_135Add is the upper-left-16x16 sparse fast path.
func Idct32x32_135Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 32, 32) {
		idct32x32_135AddScalar(input, dest, stride)
		return
	}
	var residual [1024]int16
	idct32x32Residual(input, &residual, 16)
	idctAddResidualRows32NEON(unsafe.SliceData(dest), stride, &residual[0], 32)
}

// Idct32x32_34Add is the upper-left-8x8 sparse fast path.
func Idct32x32_34Add(input []int16, dest []uint8, stride int) {
	if !dcWindowOK(dest, stride, 32, 32) {
		idct32x32_34AddScalar(input, dest, stride)
		return
	}
	var residual [1024]int16
	idct32x32Residual(input, &residual, 8)
	idctAddResidualRows32NEON(unsafe.SliceData(dest), stride, &residual[0], 32)
}

// Idct32x32_1Add is the DC-only fast path. NEON kernel: scalar a1
// computation + 32x32 broadcast saturating-add. This is the most
// frequently invoked variant of the largest VP9 transform — almost
// every sb-aligned skip block in inter frames hits it.
func Idct32x32_1Add(input []int16, dest []uint8, stride int) {
	a1 := idctDcA1(int64(input[0]), 6)
	if !dcWindowOK(dest, stride, 32, 32) {
		idct32x32_1AddScalar(input, dest, stride)
		return
	}
	idct32x32DcAddNEON(unsafe.SliceData(dest), stride, int16(a1))
}

// Iwht4x4_16Add applies the inverse 4x4 Walsh-Hadamard (lossless).
func Iwht4x4_16Add(input []int16, dest []uint8, stride int) {
	iwht4x4_16AddScalar(input, dest, stride)
}

// Iwht4x4_1Add is the DC-only fast path for the lossless 4x4 IWHT.
// Same broadcast/add layout as Idct4x4_1Add, but the a1 prelude is
// the IWHT-specific arithmetic.
func Iwht4x4_1Add(input []int16, dest []uint8, stride int) {
	// Mirror vpx_iwht4x4_1_add_c's prelude: shift the DC by
	// UNIT_QUANT_SHIFT then run one row+column step of the IWHT,
	// arriving at a single broadcast residual.
	a1 := int64(input[0]) >> unitQuantShift
	e1 := a1 >> 1
	a1 -= e1
	// After the column pass, lane 0 of each row gets a1 and lanes 1..3
	// get e1. The libvpx kernel writes the row pattern (a1, e1, e1, e1)
	// for each of the 4 dest rows. NEON can't beat 4 byte stores here,
	// so route DC-only IWHT to the always-correct scalar fallback.
	_ = e1
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
	idctAddResidualRows4NEON(unsafe.SliceData(dest), stride, &residual[0], 4)
}

// Iht8x8_64Add dispatches the 2-D hybrid 8x8 inverse transform.
func Iht8x8_64Add(input []int16, dest []uint8, stride int, txType int) {
	if txType == 0 {
		Idct8x8_64Add(input, dest, stride)
		return
	}
	if txType >= 1 && txType <= 3 && dcWindowOK(dest, stride, 8, 8) {
		iht8x8_64AddNEON(unsafe.SliceData(input), unsafe.SliceData(dest), stride, txType)
		return
	}
	if !dcWindowOK(dest, stride, 8, 8) {
		iht8x8_64AddScalar(input, dest, stride, txType)
		return
	}
	var residual [64]int16
	iht8x8Residual(input, &residual, txType)
	idctAddResidualRows8NEON(unsafe.SliceData(dest), stride, &residual[0], 8)
}

// Iht16x16_256Add dispatches the 2-D hybrid 16x16 inverse transform.
func Iht16x16_256Add(input []int16, dest []uint8, stride int, txType int) {
	if txType == 0 {
		Idct16x16_256Add(input, dest, stride)
		return
	}
	if txType == 3 && dcWindowOK(dest, stride, 16, 16) {
		iht16x16_256AddAdstAdstNEON(unsafe.SliceData(input), unsafe.SliceData(dest), stride)
		return
	}
	if !dcWindowOK(dest, stride, 16, 16) {
		iht16x16_256AddScalar(input, dest, stride, txType)
		return
	}
	var residual [256]int16
	iht16x16Residual(input, &residual, txType)
	idctAddResidualRows16NEON(unsafe.SliceData(dest), stride, &residual[0], 16)
}

// idctDcA1 mirrors the scalar prelude shared by every Idct*_1Add
// variant: two cospi_16_64 multiplies through dctConstRoundShift,
// narrowed to int16 on each store, then ROUND_POWER_OF_TWO(., shift)
// where shift is the size-specific normalisation (4 for 4x4, 5 for
// 8x8, 6 for 16x16 and 32x32). Returns an int32 value safely
// representable in int16 — the [-2048, +2048] range that fits in 12
// bits.
func idctDcA1(dc int64, shift uint) int32 {
	out := int16(dctConstRoundShift(dc * cospi16_64))
	out = int16(dctConstRoundShift(int64(out) * cospi16_64))
	return roundPowerOfTwo(int32(out), shift)
}

// dcWindowOK validates that dest can be read/written as an NxN block
// of pixels at the given stride. Mirrors the SAD/variance window
// checks elsewhere in this package.
func dcWindowOK(dest []uint8, stride, w, h int) bool {
	if stride < w {
		return false
	}
	limit := (h-1)*stride + w
	return limit > 0 && limit <= len(dest)
}

//go:noescape
func idct4x4DcAddNEON(dest *byte, stride int, a1 int16)

//go:noescape
func idct8x8DcAddNEON(dest *byte, stride int, a1 int16)

//go:noescape
func idct16x16DcAddNEON(dest *byte, stride int, a1 int16)

//go:noescape
func idct32x32DcAddNEON(dest *byte, stride int, a1 int16)

//go:noescape
func idct8x8_64AddNEON(input *int16, dest *byte, stride int)

//go:noescape
func idct8x8_12AddNEON(input *int16, dest *byte, stride int)

//go:noescape
func iht8x8_64AddNEON(input *int16, dest *byte, stride int, txType int)

// idctAddResidualRowsNNEON adds N int16 residuals per row to dest with
// SRSHR (round-power-of-two) and signed-saturating narrow back to
// uint8. N is 4/8/16/32; the shift baked into each kernel is 4/5/6/6.
//
//go:noescape
func idctAddResidualRows4NEON(dest *byte, stride int, residual *int16, nRows int)

//go:noescape
func idctAddResidualRows8NEON(dest *byte, stride int, residual *int16, nRows int)

//go:noescape
func idctAddResidualRows16NEON(dest *byte, stride int, residual *int16, nRows int)

//go:noescape
func idctAddResidualRows32NEON(dest *byte, stride int, residual *int16, nRows int)
