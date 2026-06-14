//go:build amd64 && !purego

package dsp

import "unsafe"

// sad16x16SSE2 mirrors libvpx v1.16.0 vpx_sad16x16_sse2. The wrapper keeps
// the scalar panic/fallback behavior for malformed windows while valid VP9
// motion-search windows take the no-allocation assembly path.
//
//go:noescape
func sad16x16SSE2(src *byte, srcStride int, ref *byte, refStride int) uint32

//go:noescape
func sad16xNSSE2(src *byte, srcStride int, ref *byte, refStride int, rows int) uint32

//go:noescape
func sad8xNSSE2(src *byte, srcStride int, ref *byte, refStride int, rows int) uint32

//go:noescape
func sad16ChunksSSE2(src *byte, srcStride int, ref *byte, refStride int, rows int, chunks int) uint32

func sadWindowOK(buf []uint8, off, stride, w, h int) bool {
	return dspReadWindowOK(buf, off, stride, w, h)
}

func sad64x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 64, 64) &&
		sadWindowOK(ref, refOff, refStride, 64, 64) {
		return sad16ChunksSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 64, 4)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 64, 64)
}

func sad64x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 64, 32) &&
		sadWindowOK(ref, refOff, refStride, 64, 32) {
		return sad16ChunksSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 32, 4)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 64, 32)
}

func sad32x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 32, 64) &&
		sadWindowOK(ref, refOff, refStride, 32, 64) {
		return sad16ChunksSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 64, 2)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 64)
}

func sad32x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 32, 32) &&
		sadWindowOK(ref, refOff, refStride, 32, 32) {
		return sad16ChunksSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 32, 2)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 32)
}

func sad32x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 32, 16) &&
		sadWindowOK(ref, refOff, refStride, 32, 16) {
		return sad16ChunksSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 16, 2)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 16)
}

func sad16x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 16, 32) &&
		sadWindowOK(ref, refOff, refStride, 16, 32) {
		return sad16xNSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 32)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 32)
}

func sad16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 16, 16) &&
		sadWindowOK(ref, refOff, refStride, 16, 16) {
		return sad16x16SSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 16)
}

func sad16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 16, 8) &&
		sadWindowOK(ref, refOff, refStride, 16, 8) {
		return sad16xNSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 8)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 8)
}

func sad8x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 8, 16) &&
		sadWindowOK(ref, refOff, refStride, 8, 16) {
		return sad8xNSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 16)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 8, 16)
}

func sad8x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 8, 8) &&
		sadWindowOK(ref, refOff, refStride, 8, 8) {
		return sad8xNSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 8)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 8, 8)
}

func sad8x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 8, 4) &&
		sadWindowOK(ref, refOff, refStride, 8, 4) {
		return sad8xNSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 4)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 8, 4)
}

// 4-wide blocks remain on the scalar path on amd64; the SSE2 PSADBW
// pattern doesn't beat the scalar loop for 4-byte rows and the call
// is rare in VP9's motion search.
func sad4x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 4, 8)
}

func sad4x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 4, 4)
}
