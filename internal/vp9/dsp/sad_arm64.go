//go:build arm64 && !purego

package dsp

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/cpu"
)

// VP9 ARMv8 NEON SAD primitives. Ported from libvpx v1.16.0
// vpx_dsp/arm/sad_neon.c, with the vpx_dsp/arm/sad_neon_dotprod.c
// variants dispatched on cpu.HasARM64DotProd. Each kernel returns the
// full SAD for a (w, h) block; the wrappers verify the read windows lie
// inside the passed slices before entering the assembly path so the
// no-allocation kernels never read out of bounds.

//go:noescape
func sad16xNNEON(src *byte, srcStride int, ref *byte, refStride int, rows int) uint32

//go:noescape
func sad32xNNEON(src *byte, srcStride int, ref *byte, refStride int, rows int) uint32

//go:noescape
func sad64xNNEON(src *byte, srcStride int, ref *byte, refStride int, rows int) uint32

//go:noescape
func sadDot16xNNEON(src *byte, srcStride int, ref *byte, refStride int, rows int) uint32

//go:noescape
func sadDotWideNEON(src *byte, srcStride int, ref *byte, refStride int, rows int, groups int) uint32

//go:noescape
func sadDot4DNEON(src *byte, srcStride int, ref0 *byte, ref1 *byte,
	ref2 *byte, ref3 *byte, refStride int, rows int, chunks int, out *[4]uint32)

// sadWide dispatches a 32- or 64-wide single-reference SAD.
func sadWide(src *byte, srcStride int, ref *byte, refStride int, rows, width int) uint32 {
	if cpu.HasARM64DotProd {
		return sadDotWideNEON(src, srcStride, ref, refStride, rows, width/32)
	}
	if width == 64 {
		return sad64xNNEON(src, srcStride, ref, refStride, rows)
	}
	return sad32xNNEON(src, srcStride, ref, refStride, rows)
}

// sad16 dispatches a 16-wide single-reference SAD (rows is even for all
// VP9 16-wide block heights).
func sad16(src *byte, srcStride int, ref *byte, refStride int, rows int) uint32 {
	if cpu.HasARM64DotProd && rows&1 == 0 {
		return sadDot16xNNEON(src, srcStride, ref, refStride, rows)
	}
	return sad16xNNEON(src, srcStride, ref, refStride, rows)
}

//go:noescape
func sad16ChunksNEON(src *byte, srcStride int, ref *byte, refStride int, rows int, chunks int) uint32

//go:noescape
func sad16Chunksx4NEON(src *byte, srcStride int, ref0 *byte, ref1 *byte,
	ref2 *byte, ref3 *byte, refStride int, rows int, chunks int, out *[4]uint32)

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
		return sadWide(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 64, 64)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 64, 64)
}

func sad64x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 64, 32) &&
		sadWindowOK(ref, refOff, refStride, 64, 32) {
		return sadWide(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 32, 64)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 64, 32)
}

func sad32x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 32, 64) &&
		sadWindowOK(ref, refOff, refStride, 32, 64) {
		return sadWide(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 64, 32)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 64)
}

func sad32x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 32, 32) &&
		sadWindowOK(ref, refOff, refStride, 32, 32) {
		return sadWide(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 32, 32)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 32)
}

func sad32x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 32, 16) &&
		sadWindowOK(ref, refOff, refStride, 32, 16) {
		return sadWide(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 16, 32)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 32, 16)
}

func sad16x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 16, 32) &&
		sadWindowOK(ref, refOff, refStride, 16, 32) {
		return sad16(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 32)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 32)
}

func sad16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 16, 16) &&
		sadWindowOK(ref, refOff, refStride, 16, 16) {
		return sad16(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride, 16)
	}
	return sad(src, srcOff, srcStride, ref, refOff, refStride, 16, 16)
}

func sad16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	if sadWindowOK(src, srcOff, srcStride, 16, 8) &&
		sadWindowOK(ref, refOff, refStride, 16, 8) {
		return sad16(
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

func sad4D(src []uint8, srcOff, srcStride int,
	ref []uint8, refOff0, refOff1, refOff2, refOff3, refStride int,
	w, h int, out *[4]uint32,
) bool {
	if out == nil || w <= 0 || h <= 0 {
		return false
	}
	chunks := 0
	switch w {
	case 16:
		chunks = 1
	case 32:
		chunks = 2
	case 64:
		chunks = 4
	}
	if chunks != 0 &&
		sadWindowOK(src, srcOff, srcStride, w, h) &&
		sadWindowOK(ref, refOff0, refStride, w, h) &&
		sadWindowOK(ref, refOff1, refStride, w, h) &&
		sadWindowOK(ref, refOff2, refStride, w, h) &&
		sadWindowOK(ref, refOff3, refStride, w, h) {
		if cpu.HasARM64DotProd {
			sadDot4DNEON(
				unsafe.SliceData(src[srcOff:]), srcStride,
				unsafe.SliceData(ref[refOff0:]),
				unsafe.SliceData(ref[refOff1:]),
				unsafe.SliceData(ref[refOff2:]),
				unsafe.SliceData(ref[refOff3:]),
				refStride, h, chunks, out)
			return true
		}
		sad16Chunksx4NEON(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff0:]),
			unsafe.SliceData(ref[refOff1:]),
			unsafe.SliceData(ref[refOff2:]),
			unsafe.SliceData(ref[refOff3:]),
			refStride, h, chunks, out)
		return true
	}
	return sad4DScalar(src, srcOff, srcStride, ref, refOff0, refOff1, refOff2,
		refOff3, refStride, w, h, out)
}
