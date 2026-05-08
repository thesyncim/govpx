//go:build amd64

package dsp

// amd64 dispatch for VP8 loop-filter apply paths (libvpx v1.16.0 baseline).
// SSE2 kernels handle the 16-wide (count=2) horizontal-edge cases for
// both inner and MB filters. Vertical-edge variants gather the 16x8
// row-window into a transposed 8x16 stack buffer, run the same SSE2
// horizontal kernel, then scatter the 4 (or 6) modified rows back.
// count=1 (chroma 8-wide) uses the libvpx-style scalar reference.

import (
	"encoding/binary"
	"unsafe"
)

func loopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 7*stride+16 {
		loopFilterEdgeH16SSE2(&s[0], stride, blimit, limit, thresh)
		return
	}
	loopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 15*stride+8 {
		var tmp [8 * 16]byte
		gatherV16x8AMD64(&tmp, s, stride)
		loopFilterEdgeH16SSE2((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV16x8AMD64(s, stride, &tmp, 2, 4)
		return
	}
	loopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 7*stride+16 {
		mbLoopFilterEdgeH16SSE2(&s[0], stride, blimit, limit, thresh)
		return
	}
	mbLoopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 15*stride+8 {
		var tmp [8 * 16]byte
		gatherV16x8AMD64(&tmp, s, stride)
		mbLoopFilterEdgeH16SSE2((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV16x8AMD64(s, stride, &tmp, 1, 6)
		return
	}
	mbLoopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

// loopFilterSimpleHorizontalEdgeDispatch routes the 16-wide simple-LF
// horizontal edge through the SSE2 kernel when the input window is
// large enough; otherwise falls back to the libvpx-style scalar.
func loopFilterSimpleHorizontalEdgeDispatch(s []byte, stride int, blimit byte) {
	if len(s) >= 3*stride+16 {
		loopFilterSimpleEdgeH16SSE2(&s[0], stride, blimit)
		return
	}
	loopFilterSimpleHorizontalEdgeScalar(s, stride, blimit)
}

// loopFilterSimpleVerticalEdgeDispatch gathers the 16x4 column window
// into a transposed 4x16 buffer, runs the SSE2 horizontal kernel, and
// scatters the modified p0 and q0 columns back.
func loopFilterSimpleVerticalEdgeDispatch(s []byte, stride int, blimit byte) {
	if len(s) >= 15*stride+4 {
		var tmp [4 * 16]byte
		gatherV16x4AMD64(&tmp, s, stride)
		loopFilterSimpleEdgeH16SSE2((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit)
		// p0 (slot 1) and q0 (slot 2) were modified by the kernel.
		for i := 0; i < 16; i++ {
			row := s[i*stride : i*stride+4]
			row[1] = tmp[1*16+i]
			row[2] = tmp[2*16+i]
		}
		return
	}
	loopFilterSimpleVerticalEdgeScalar(s, stride, blimit)
}

// gatherV16x4AMD64 reads 16 rows of 4 bytes each from s and packs them
// into tmp such that tmp[r*16+i] = s[i*stride+r] for r in 0..3.
func gatherV16x4AMD64(tmp *[4 * 16]byte, s []byte, stride int) {
	dst := tmp[:]
	for i := 0; i < 16; i++ {
		row := s[i*stride : i*stride+4]
		dst[0*16+i] = row[0]
		dst[1*16+i] = row[1]
		dst[2*16+i] = row[2]
		dst[3*16+i] = row[3]
	}
}

// gatherV16x8AMD64 reads 16 rows of 8 bytes each from s (row stride =
// stride) and packs them into tmp such that tmp[r*16+i] = s[i*stride+r].
// Same shape as gatherV16x8 in the arm64 dispatch.
func gatherV16x8AMD64(tmp *[8 * 16]byte, s []byte, stride int) {
	dst := tmp[:]
	for i := 0; i < 16; i++ {
		row := s[i*stride : i*stride+8]
		w := binary.LittleEndian.Uint64(row)
		dst[0*16+i] = byte(w)
		dst[1*16+i] = byte(w >> 8)
		dst[2*16+i] = byte(w >> 16)
		dst[3*16+i] = byte(w >> 24)
		dst[4*16+i] = byte(w >> 32)
		dst[5*16+i] = byte(w >> 40)
		dst[6*16+i] = byte(w >> 48)
		dst[7*16+i] = byte(w >> 56)
	}
}

// scatterV16x8AMD64 writes the modified rows [first..first+nrows-1] of
// tmp back to the corresponding column positions in s.
func scatterV16x8AMD64(s []byte, stride int, tmp *[8 * 16]byte, first int, nrows int) {
	src := tmp[:]
	for i := 0; i < 16; i++ {
		row := s[i*stride : i*stride+8]
		for r := 0; r < nrows; r++ {
			row[first+r] = src[(first+r)*16+i]
		}
	}
}
