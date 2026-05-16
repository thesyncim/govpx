//go:build !amd64 || purego

package dsp

func sad64x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 64, 64)
}

func sad64x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 64, 32)
}

func sad32x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 64)
}

func sad32x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 32)
}

func sad32x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 16)
}

func sad16x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 32)
}

func sad16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 16)
}

func sad16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 8)
}

func sad8x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 8, 16)
}

func sad8x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 8, 8)
}

func sad8x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 8, 4)
}
