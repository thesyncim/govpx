package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// Shared Idct/Iwht/Iht dispatchers use Go SIMD when available and
// retain the independent scalar transforms for fallback builds.

// Idct4x4_16Add applies the full 4x4 inverse DCT to a 16-coefficient
// block and adds the result onto dest.
func Idct4x4_16Add(input []int16, dest []uint8, stride int) {
	if inverseTransformGo(input, dest, stride, 4, 4, 0) {
		return
	}

	idct4x4_16AddScalar(input, dest, stride)
}

// Idct4x4_1Add is the DC-only fast path for the 4x4 inverse DCT.
func Idct4x4_1Add(input []int16, dest []uint8, stride int) {
	if pixel.Enabled {
		v := int16(dctConstRoundShift(int64(input[0]) * cospi16_64))
		v = int16(dctConstRoundShift(int64(v) * cospi16_64))
		pixel.AddConstant(dest, stride, dest, stride, 4, 4, int(roundPowerOfTwo(int32(v), 4)))
		return
	}

	idct4x4_1AddScalar(input, dest, stride)
}

// Idct8x8_64Add applies the full 8x8 inverse DCT.
func Idct8x8_64Add(input []int16, dest []uint8, stride int) {
	if inverseTransformGo(input, dest, stride, 8, 8, 0) {
		return
	}

	idct8x8_64AddScalar(input, dest, stride)
}

// Idct8x8_12Add is the sparse upper-left-4x4 fast path for the 8x8
// inverse DCT.
func Idct8x8_12Add(input []int16, dest []uint8, stride int) {
	if inverseTransformGo(input, dest, stride, 8, 4, 0) {
		return
	}

	idct8x8_12AddScalar(input, dest, stride)
}

// Idct8x8_1Add is the DC-only fast path for the 8x8 inverse DCT.
func Idct8x8_1Add(input []int16, dest []uint8, stride int) {
	if pixel.Enabled {
		v := int16(dctConstRoundShift(int64(input[0]) * cospi16_64))
		v = int16(dctConstRoundShift(int64(v) * cospi16_64))
		pixel.AddConstant(dest, stride, dest, stride, 8, 8, int(roundPowerOfTwo(int32(v), 5)))
		return
	}

	idct8x8_1AddScalar(input, dest, stride)
}

// Idct16x16_256Add applies the full 16x16 inverse DCT.
func Idct16x16_256Add(input []int16, dest []uint8, stride int) {
	if inverseTransformGo(input, dest, stride, 16, 16, 0) {
		return
	}

	idct16x16_256AddScalar(input, dest, stride)
}

// Idct16x16_38Add is the upper-left-8x8 sparse fast path.
func Idct16x16_38Add(input []int16, dest []uint8, stride int) {
	if inverseTransformGo(input, dest, stride, 16, 8, 0) {
		return
	}

	idct16x16_38AddScalar(input, dest, stride)
}

// Idct16x16_10Add is the upper-left-4x4 sparse fast path.
func Idct16x16_10Add(input []int16, dest []uint8, stride int) {
	if inverseTransformGo(input, dest, stride, 16, 4, 0) {
		return
	}

	idct16x16_10AddScalar(input, dest, stride)
}

// Idct16x16_1Add is the DC-only fast path for the 16x16 inverse DCT.
func Idct16x16_1Add(input []int16, dest []uint8, stride int) {
	if pixel.Enabled {
		v := int16(dctConstRoundShift(int64(input[0]) * cospi16_64))
		v = int16(dctConstRoundShift(int64(v) * cospi16_64))
		pixel.AddConstant(dest, stride, dest, stride, 16, 16, int(roundPowerOfTwo(int32(v), 6)))
		return
	}

	idct16x16_1AddScalar(input, dest, stride)
}

// Idct32x32_1024Add is the dense 32x32 inverse DCT.
func Idct32x32_1024Add(input []int16, dest []uint8, stride int) {
	if inverseTransformGo(input, dest, stride, 32, 32, 0) {
		return
	}

	idct32x32_1024AddScalar(input, dest, stride)
}

// Idct32x32_135Add is the upper-left-16x16 sparse fast path.
func Idct32x32_135Add(input []int16, dest []uint8, stride int) {
	if inverseTransformGo(input, dest, stride, 32, 16, 0) {
		return
	}

	idct32x32_135AddScalar(input, dest, stride)
}

// Idct32x32_34Add is the upper-left-8x8 sparse fast path.
func Idct32x32_34Add(input []int16, dest []uint8, stride int) {
	if inverseTransformGo(input, dest, stride, 32, 8, 0) {
		return
	}

	idct32x32_34AddScalar(input, dest, stride)
}

// Idct32x32_1Add is the DC-only fast path for the 32x32 inverse DCT.
func Idct32x32_1Add(input []int16, dest []uint8, stride int) {
	if pixel.Enabled {
		v := int16(dctConstRoundShift(int64(input[0]) * cospi16_64))
		v = int16(dctConstRoundShift(int64(v) * cospi16_64))
		pixel.AddConstant(dest, stride, dest, stride, 32, 32, int(roundPowerOfTwo(int32(v), 6)))
		return
	}

	idct32x32_1AddScalar(input, dest, stride)
}

// Iwht4x4_16Add applies the inverse 4x4 Walsh-Hadamard (lossless).
func Iwht4x4_16Add(input []int16, dest []uint8, stride int) {
	if !inverseWHTGo(input, dest, stride, false) {
		iwht4x4_16AddScalar(input, dest, stride)
	}
}

// Iwht4x4_1Add is the DC-only fast path for the lossless 4x4 IWHT.
func Iwht4x4_1Add(input []int16, dest []uint8, stride int) {
	if !inverseWHTGo(input, dest, stride, true) {
		iwht4x4_1AddScalar(input, dest, stride)
	}
}

// Iht4x4_16Add dispatches the 2-D hybrid 4x4 inverse transform.
func Iht4x4_16Add(input []int16, dest []uint8, stride int, txType int) {
	if txType >= 0 && txType <= 3 && inverseTransformGo(input, dest, stride, 4, 4, txType) {
		return
	}

	iht4x4_16AddScalar(input, dest, stride, txType)
}

// Iht8x8_64Add dispatches the 2-D hybrid 8x8 inverse transform.
func Iht8x8_64Add(input []int16, dest []uint8, stride int, txType int) {
	if txType >= 0 && txType <= 3 && inverseTransformGo(input, dest, stride, 8, 8, txType) {
		return
	}

	iht8x8_64AddScalar(input, dest, stride, txType)
}

// Iht16x16_256Add dispatches the 2-D hybrid 16x16 inverse transform.
func Iht16x16_256Add(input []int16, dest []uint8, stride int, txType int) {
	if txType >= 0 && txType <= 3 && inverseTransformGo(input, dest, stride, 16, 16, txType) {
		return
	}

	iht16x16_256AddScalar(input, dest, stride, txType)
}
