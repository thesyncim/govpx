//go:build arm64 && !purego

package dsp

import "unsafe"

// ARMv8 NEON subtract helpers port libvpx v1.16.0
// vpx_dsp/arm/subtract_neon.c. The assembly path is used for the standard VP9
// transform widths when the output rows are contiguous; other layouts fall
// back to the scalar reference above.

//go:noescape
func subtractBlock16xNNEON(src *byte, srcStride int, pred *byte, predStride int, out *int16, rows int) uint64

//go:noescape
func subtractBlock16ChunksNEON(src *byte, srcStride int, pred *byte, predStride int, out *int16, rows int, chunks int) uint64

//go:noescape
func subtractBlock8xNNEON(src *byte, srcStride int, pred *byte, predStride int, out *int16, rows int) uint64

//go:noescape
func subtractBlock4xNNEON(src *byte, srcStride int, pred *byte, predStride int, out *int16, rows int) uint64

func subtractBlockNonZeroFast(src []uint8, srcOff, srcStride int,
	pred []uint8, predOff, predStride int,
	out []int16, outOff, outStride int,
	w, h int,
) (bool, bool) {
	if outStride != w {
		return false, false
	}
	srcPtr := unsafe.SliceData(src[srcOff:])
	predPtr := unsafe.SliceData(pred[predOff:])
	outPtr := unsafe.SliceData(out[outOff:])
	switch w {
	case 32:
		return subtractBlock16ChunksNEON(srcPtr, srcStride, predPtr, predStride,
			outPtr, h, 2) != 0, true
	case 16:
		return subtractBlock16xNNEON(srcPtr, srcStride, predPtr, predStride,
			outPtr, h) != 0, true
	case 8:
		return subtractBlock8xNNEON(srcPtr, srcStride, predPtr, predStride,
			outPtr, h) != 0, true
	case 4:
		return subtractBlock4xNNEON(srcPtr, srcStride, predPtr, predStride,
			outPtr, h) != 0, true
	default:
		return false, false
	}
}
