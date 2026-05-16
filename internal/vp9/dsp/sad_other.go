//go:build !amd64 || purego

package dsp

func sad16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 16)
}
