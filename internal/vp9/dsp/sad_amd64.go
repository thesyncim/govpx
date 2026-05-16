//go:build amd64 && !purego

package dsp

import "unsafe"

// sad16x16SSE2 mirrors libvpx v1.16.0 vpx_sad16x16_sse2. The wrapper keeps
// the scalar panic/fallback behavior for malformed windows while valid VP9
// motion-search windows take the no-allocation assembly path.
//
//go:noescape
func sad16x16SSE2(src *byte, srcStride int, ref *byte, refStride int) uint32

func sad16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if srcOff >= 0 && refOff >= 0 && srcStride >= 0 && refStride >= 0 {
		srcLimit := srcOff + 15*srcStride + 16
		refLimit := refOff + 15*refStride + 16
		if srcLimit >= srcOff && refLimit >= refOff &&
			srcLimit <= len(src) && refLimit <= len(ref) {
			return sad16x16SSE2(
				unsafe.SliceData(src[srcOff:]),
				srcStride,
				unsafe.SliceData(ref[refOff:]),
				refStride)
		}
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 16)
}
