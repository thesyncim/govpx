//go:build arm64 && !purego

package dsp

import "unsafe"

// VP9 ARMv8 NEON SAD primitives. Ported from libvpx v1.16.0
// vpx_dsp/arm/sad_neon.c. Each kernel returns the full SAD for a
// (w, h) block; the wrappers verify the read windows lie inside the
// passed slices before entering the assembly path so the no-allocation
// kernels never read out of bounds.
//
// Three asm kernels cover all VP9 sizes:
//
//   sad16xNNEON(rows)          - 16-wide, rows in [4, 64]
//   sad16ChunksNEON(rows, chk) - chk*16 wide, rows in [16, 64]
//   sad8xNNEON(rows)           - 8-wide, rows in [4, 16]
//   sad4xNNEON(rows)           - 4-wide, rows in {4, 8}; rows must be even
//
// All return uint32 to match the libvpx/SSE2 signature used elsewhere
// in the VP9 DSP package.

//go:noescape
func sad16xNNEON(src *byte, srcStride int, ref *byte, refStride int, rows int) uint32

//go:noescape
func sad16ChunksNEON(src *byte, srcStride int, ref *byte, refStride int, rows int, chunks int) uint32

//go:noescape
func sad8xNNEON(src *byte, srcStride int, ref *byte, refStride int, rows int) uint32

//go:noescape
func sad4xNNEON(src *byte, srcStride int, ref *byte, refStride int, rows int) uint32

// sadWindowOK validates that the [off, off+(h-1)*stride+w) byte window
// is in-bounds and non-overflowing so the NEON kernels can read it
// safely. Mirrors the AMD64 wrapper's check.
func sadWindowOK(buf []uint8, off, stride, w, h int) bool {
	if off < 0 || stride < 0 {
		return false
	}
	limit := off + (h-1)*stride + w
	return limit >= off && limit <= len(buf)
}

func sad64x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 64, 64) &&
		sadWindowOK(ref, refOff, refStride, 64, 64) {
		return sad16ChunksNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 64, 4)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 64, 64)
}

func sad64x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 64, 32) &&
		sadWindowOK(ref, refOff, refStride, 64, 32) {
		return sad16ChunksNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 32, 4)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 64, 32)
}

func sad32x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 32, 64) &&
		sadWindowOK(ref, refOff, refStride, 32, 64) {
		return sad16ChunksNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 64, 2)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 64)
}

func sad32x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 32, 32) &&
		sadWindowOK(ref, refOff, refStride, 32, 32) {
		return sad16ChunksNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 32, 2)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 32)
}

func sad32x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 32, 16) &&
		sadWindowOK(ref, refOff, refStride, 32, 16) {
		return sad16ChunksNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 16, 2)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 16)
}

func sad16x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 16, 32) &&
		sadWindowOK(ref, refOff, refStride, 16, 32) {
		return sad16xNNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 32)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 32)
}

func sad16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 16, 16) &&
		sadWindowOK(ref, refOff, refStride, 16, 16) {
		return sad16xNNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 16)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 16)
}

func sad16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 16, 8) &&
		sadWindowOK(ref, refOff, refStride, 16, 8) {
		return sad16xNNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 8)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 8)
}

func sad8x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 8, 16) &&
		sadWindowOK(ref, refOff, refStride, 8, 16) {
		return sad8xNNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 16)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 8, 16)
}

func sad8x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 8, 8) &&
		sadWindowOK(ref, refOff, refStride, 8, 8) {
		return sad8xNNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 8)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 8, 8)
}

func sad8x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 8, 4) &&
		sadWindowOK(ref, refOff, refStride, 8, 4) {
		return sad8xNNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 4)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 8, 4)
}

func sad4x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 4, 8) &&
		sadWindowOK(ref, refOff, refStride, 4, 8) {
		return sad4xNNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 8)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 4, 8)
}

func sad4x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 4, 4) &&
		sadWindowOK(ref, refOff, refStride, 4, 4) {
		return sad4xNNEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 4)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 4, 4)
}
