//go:build arm64

package dsp

// arm64 dispatch for VP8 loop-filter apply paths (libvpx v1.16.0 baseline).
// NEON kernels handle the 16-wide (count=2) horizontal-edge cases for
// both inner and MB filters. Vertical-edge variants gather the 16x8
// row-window into a transposed 8x16 stack buffer, run the same NEON
// horizontal kernel, then scatter the 4 (or 6) modified rows back.
// count=1 (chroma 8-wide) uses the libvpx-style scalar reference.

import (
	"unsafe"
)

func loopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 7*stride+16 {
		loopFilterEdgeH16NEON(&s[0], stride, blimit, limit, thresh)
		return
	}
	loopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 15*stride+8 {
		var tmp [8 * 16]byte
		gatherV16x8(&tmp, s, stride)
		loopFilterEdgeH16NEON((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV16x8(s, stride, &tmp, 2, 4)
		return
	}
	loopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 7*stride+16 {
		mbLoopFilterEdgeH16NEON(&s[0], stride, blimit, limit, thresh)
		return
	}
	mbLoopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 15*stride+8 {
		var tmp [8 * 16]byte
		gatherV16x8(&tmp, s, stride)
		mbLoopFilterEdgeH16NEON((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV16x8(s, stride, &tmp, 1, 6)
		return
	}
	mbLoopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

// gatherV16x8 reads 16 rows of 8 bytes each from s (row stride = stride)
// and packs them into tmp such that tmp[r*16+i] = s[i*stride+r].
// Equivalently: rows [0..7] of tmp each contain a single byte from
// columns 0..7 of every input row, lined up across the 16 lanes.
//
// Implementation: read each input row as a uint64 (8 bytes little-endian
// at byte 0..7) and scatter each byte at the right lane position.
func gatherV16x8(tmp *[8 * 16]byte, s []byte, stride int) {
	_ = s[15*stride+7]
	srcBase := unsafe.Pointer(&s[0])
	dstBase := unsafe.Pointer(&tmp[0])
	for i := 0; i < 16; i++ {
		w := *(*uint64)(unsafe.Add(srcBase, i*stride))
		*(*byte)(unsafe.Add(dstBase, 0*16+i)) = byte(w)
		*(*byte)(unsafe.Add(dstBase, 1*16+i)) = byte(w >> 8)
		*(*byte)(unsafe.Add(dstBase, 2*16+i)) = byte(w >> 16)
		*(*byte)(unsafe.Add(dstBase, 3*16+i)) = byte(w >> 24)
		*(*byte)(unsafe.Add(dstBase, 4*16+i)) = byte(w >> 32)
		*(*byte)(unsafe.Add(dstBase, 5*16+i)) = byte(w >> 40)
		*(*byte)(unsafe.Add(dstBase, 6*16+i)) = byte(w >> 48)
		*(*byte)(unsafe.Add(dstBase, 7*16+i)) = byte(w >> 56)
	}
}

// scatterV16x8 writes the modified rows [first..first+nrows-1] of tmp
// back to the corresponding column positions in s.
func scatterV16x8(s []byte, stride int, tmp *[8 * 16]byte, first int, nrows int) {
	_ = s[15*stride+7]
	if first < 0 || nrows < 0 || first+nrows > 8 {
		return
	}
	srcBase := unsafe.Pointer(&tmp[0])
	dstBase := unsafe.Pointer(&s[0])
	for i := 0; i < 16; i++ {
		row := unsafe.Add(dstBase, i*stride)
		for r := 0; r < nrows; r++ {
			*(*byte)(unsafe.Add(row, first+r)) = *(*byte)(unsafe.Add(srcBase, (first+r)*16+i))
		}
	}
}
