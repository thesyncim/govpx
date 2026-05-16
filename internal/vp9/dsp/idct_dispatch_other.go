//go:build !arm64 || purego

package dsp

// Scalar dispatchers for the non-arm64 / purego build path. These
// expose the canonical Idct/Iwht/Iht entry points used by the VP9
// decoder. On arm64 (non-purego) builds, idct_dispatch_arm64.go
// replaces these with NEON-accelerated implementations.

// Idct4x4_16Add applies the full 4x4 inverse DCT to a 16-coefficient
// block and adds the result onto dest.
func Idct4x4_16Add(input []int16, dest []uint8, stride int) {
	idct4x4_16AddScalar(input, dest, stride)
}

// Idct4x4_1Add is the DC-only fast path for the 4x4 inverse DCT.
func Idct4x4_1Add(input []int16, dest []uint8, stride int) {
	idct4x4_1AddScalar(input, dest, stride)
}

// Idct8x8_64Add applies the full 8x8 inverse DCT.
func Idct8x8_64Add(input []int16, dest []uint8, stride int) {
	idct8x8_64AddScalar(input, dest, stride)
}

// Idct8x8_12Add is the sparse upper-left-4x4 fast path for the 8x8
// inverse DCT.
func Idct8x8_12Add(input []int16, dest []uint8, stride int) {
	idct8x8_12AddScalar(input, dest, stride)
}

// Idct8x8_1Add is the DC-only fast path for the 8x8 inverse DCT.
func Idct8x8_1Add(input []int16, dest []uint8, stride int) {
	idct8x8_1AddScalar(input, dest, stride)
}

// Idct16x16_256Add applies the full 16x16 inverse DCT.
func Idct16x16_256Add(input []int16, dest []uint8, stride int) {
	idct16x16_256AddScalar(input, dest, stride)
}

// Idct16x16_38Add is the upper-left-8x8 sparse fast path.
func Idct16x16_38Add(input []int16, dest []uint8, stride int) {
	idct16x16_38AddScalar(input, dest, stride)
}

// Idct16x16_10Add is the upper-left-4x4 sparse fast path.
func Idct16x16_10Add(input []int16, dest []uint8, stride int) {
	idct16x16_10AddScalar(input, dest, stride)
}

// Idct16x16_1Add is the DC-only fast path for the 16x16 inverse DCT.
func Idct16x16_1Add(input []int16, dest []uint8, stride int) {
	idct16x16_1AddScalar(input, dest, stride)
}

// Idct32x32_1024Add is the dense 32x32 inverse DCT.
func Idct32x32_1024Add(input []int16, dest []uint8, stride int) {
	idct32x32_1024AddScalar(input, dest, stride)
}

// Idct32x32_135Add is the upper-left-16x16 sparse fast path.
func Idct32x32_135Add(input []int16, dest []uint8, stride int) {
	idct32x32_135AddScalar(input, dest, stride)
}

// Idct32x32_34Add is the upper-left-8x8 sparse fast path.
func Idct32x32_34Add(input []int16, dest []uint8, stride int) {
	idct32x32_34AddScalar(input, dest, stride)
}

// Idct32x32_1Add is the DC-only fast path for the 32x32 inverse DCT.
func Idct32x32_1Add(input []int16, dest []uint8, stride int) {
	idct32x32_1AddScalar(input, dest, stride)
}

// Iwht4x4_16Add applies the inverse 4x4 Walsh-Hadamard (lossless).
func Iwht4x4_16Add(input []int16, dest []uint8, stride int) {
	iwht4x4_16AddScalar(input, dest, stride)
}

// Iwht4x4_1Add is the DC-only fast path for the lossless 4x4 IWHT.
func Iwht4x4_1Add(input []int16, dest []uint8, stride int) {
	iwht4x4_1AddScalar(input, dest, stride)
}

// Iht4x4_16Add dispatches the 2-D hybrid 4x4 inverse transform.
func Iht4x4_16Add(input []int16, dest []uint8, stride int, txType int) {
	iht4x4_16AddScalar(input, dest, stride, txType)
}

// Iht8x8_64Add dispatches the 2-D hybrid 8x8 inverse transform.
func Iht8x8_64Add(input []int16, dest []uint8, stride int, txType int) {
	iht8x8_64AddScalar(input, dest, stride, txType)
}

// Iht16x16_256Add dispatches the 2-D hybrid 16x16 inverse transform.
func Iht16x16_256Add(input []int16, dest []uint8, stride int, txType int) {
	iht16x16_256AddScalar(input, dest, stride, txType)
}
