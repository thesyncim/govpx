//go:build arm64 && !purego

package dsp

import (
	"encoding/binary"
	"unsafe"
)

// arm64 dispatch for VP8 loop-filter apply paths (libvpx v1.16.0 baseline).
// NEON kernels handle the horizontal-edge cases for both inner and MB
// filters; the kernel itself is 16-wide. count=2 (luma, 16 columns)
// feeds it directly. count=1 (chroma, 8 columns) feeds the same H16
// kernel after gathering 8x8 into a [8*16]byte buffer with the high 8
// lanes padded — every NEON op is per-byte (UABD / UMAX / SQADD /
// SQSUB / SADDW / SQXTN ...) so lanes are independent and the padding
// bytes don't affect the active 8 lanes' output.
// Vertical edges use the direct TRN1/TRN2-cascade kernels for count=2;
// count=1 falls back to the gather-then-H16 pattern.

func loopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 7*stride+16 {
		// len check already guarantees s is non-empty, so SliceData
		// folds away the &s[0] bounds-check + stack frame.
		loopFilterEdgeH16NEON(unsafe.SliceData(s), stride, blimit, limit, thresh)
		return
	}
	if count == 1 && len(s) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherH8x8ARM64(&tmp, s, stride)
		loopFilterEdgeH16NEON((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterH8x8ARM64(s, stride, &tmp, 2, 4)
		return
	}
	loopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterHorizontalEdgesYDispatch(s []byte, stride int, blimit, limit, thresh byte) {
	if len(s) >= 15*stride+16 {
		loopFilterHorizontalEdgesYSharedNEON(unsafe.SliceData(s), stride, blimit, limit, thresh)
		return
	}
	loopFilterHorizontalEdgeDispatch(s, stride, blimit, limit, thresh, 2)
	loopFilterHorizontalEdgeDispatch(s[4*stride:], stride, blimit, limit, thresh, 2)
	loopFilterHorizontalEdgeDispatch(s[8*stride:], stride, blimit, limit, thresh, 2)
}

func loopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 15*stride+8 {
		// Kernel follows libvpx: caller passes a pointer at the q0
		// column (the byte at offset +4 from the leftmost p3); the
		// kernel reads 8 bytes/row at src-4 across 16 rows.
		loopFilterEdgeV16NEON((*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(s)), 4)), stride, blimit, limit, thresh)
		return
	}
	if count == 1 && len(s) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherV8x8ARM64(&tmp, s, stride)
		loopFilterEdgeH16NEON((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV8x8ARM64(s, stride, &tmp, 2, 4)
		return
	}
	loopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterVerticalEdgesYDispatch(s []byte, stride int, blimit, limit, thresh byte) {
	if len(s) >= 15*stride+16 {
		loopFilterVerticalEdgesYSharedNEON(unsafe.SliceData(s), stride, blimit, limit, thresh)
		return
	}
	loopFilterVerticalEdgeDispatch(s, stride, blimit, limit, thresh, 2)
	loopFilterVerticalEdgeDispatch(s[4:], stride, blimit, limit, thresh, 2)
	loopFilterVerticalEdgeDispatch(s[8:], stride, blimit, limit, thresh, 2)
}

func loopFilterHorizontalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if len(u) >= 7*stride+8 && len(v) >= 7*stride+8 {
		loopFilterEdgeH8x8PairNEON(unsafe.SliceData(u), unsafe.SliceData(v), stride, blimit, limit, thresh)
		return
	}
	loopFilterHorizontalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	loopFilterHorizontalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func loopFilterVerticalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if len(u) >= 7*stride+8 && len(v) >= 7*stride+8 {
		baseU := unsafe.Pointer(unsafe.SliceData(u))
		baseV := unsafe.Pointer(unsafe.SliceData(v))
		loopFilterEdgeV8x8PairNEON((*byte)(unsafe.Add(baseU, 4)), (*byte)(unsafe.Add(baseV, 4)), stride, blimit, limit, thresh)
		return
	}
	loopFilterVerticalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	loopFilterVerticalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func mbLoopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 7*stride+16 {
		mbLoopFilterEdgeH16NEON(unsafe.SliceData(s), stride, blimit, limit, thresh)
		return
	}
	if count == 1 && len(s) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherH8x8ARM64(&tmp, s, stride)
		mbLoopFilterEdgeH16NEON((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterH8x8ARM64(s, stride, &tmp, 1, 6)
		return
	}
	mbLoopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 15*stride+8 {
		mbLoopFilterEdgeV16NEON((*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(s)), 4)), stride, blimit, limit, thresh)
		return
	}
	if count == 1 && len(s) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherV8x8ARM64(&tmp, s, stride)
		mbLoopFilterEdgeH16NEON((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV8x8ARM64(s, stride, &tmp, 1, 6)
		return
	}
	mbLoopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterHorizontalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if len(u) >= 7*stride+8 && len(v) >= 7*stride+8 {
		mbLoopFilterEdgeH8x8PairNEON(unsafe.SliceData(u), unsafe.SliceData(v), stride, blimit, limit, thresh)
		return
	}
	mbLoopFilterHorizontalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	mbLoopFilterHorizontalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func mbLoopFilterVerticalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if len(u) >= 7*stride+8 && len(v) >= 7*stride+8 {
		baseU := unsafe.Pointer(unsafe.SliceData(u))
		baseV := unsafe.Pointer(unsafe.SliceData(v))
		mbLoopFilterEdgeV8x8PairNEON((*byte)(unsafe.Add(baseU, 4)), (*byte)(unsafe.Add(baseV, 4)), stride, blimit, limit, thresh)
		return
	}
	mbLoopFilterVerticalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	mbLoopFilterVerticalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

// gatherH8x8ARM64 copies 8 rows of 8 bytes from s into a [8*16]byte
// stack buffer at row stride 16. The H16 kernel filters all 16 lanes,
// but lanes 8..15 are inactive downstream because only the first 8 lanes
// are scattered back.
func gatherH8x8ARM64(tmp *[8 * 16]byte, s []byte, stride int) {
	dst := tmp[:]
	for r := range 8 {
		base := r * 16
		w := binary.LittleEndian.Uint64(s[r*stride : r*stride+8])
		binary.LittleEndian.PutUint64(dst[base:base+8], w)
	}
}

// scatterH8x8ARM64 writes the modified rows [first..first+nrows-1] of
// tmp back to the corresponding source rows of s, copying only the
// first 8 lanes (the chroma 8-wide window).
func scatterH8x8ARM64(s []byte, stride int, tmp *[8 * 16]byte, first int, nrows int) {
	src := tmp[:]
	for r := range nrows {
		w := binary.LittleEndian.Uint64(src[(first+r)*16 : (first+r)*16+8])
		binary.LittleEndian.PutUint64(s[(first+r)*stride:(first+r)*stride+8], w)
	}
}

// gatherV8x8ARM64 reads 8 rows of 8 bytes each from s and packs them
// into tmp such that tmp[r*16+i] = s[i*stride+r] for i in 0..7,
// r in 0..7 — the same column-major transpose used for the count=2
// vertical-edge fallback. Lanes 8..15 are inactive on writeback.
func gatherV8x8ARM64(tmp *[8 * 16]byte, s []byte, stride int) {
	dst := tmp[:]
	r0 := binary.LittleEndian.Uint64(s[0*stride : 0*stride+8])
	r1 := binary.LittleEndian.Uint64(s[1*stride : 1*stride+8])
	r2 := binary.LittleEndian.Uint64(s[2*stride : 2*stride+8])
	r3 := binary.LittleEndian.Uint64(s[3*stride : 3*stride+8])
	r4 := binary.LittleEndian.Uint64(s[4*stride : 4*stride+8])
	r5 := binary.LittleEndian.Uint64(s[5*stride : 5*stride+8])
	r6 := binary.LittleEndian.Uint64(s[6*stride : 6*stride+8])
	r7 := binary.LittleEndian.Uint64(s[7*stride : 7*stride+8])
	binary.LittleEndian.PutUint64(dst[0*16:0*16+8], packColumn8ARM64(r0, r1, r2, r3, r4, r5, r6, r7, 0))
	binary.LittleEndian.PutUint64(dst[1*16:1*16+8], packColumn8ARM64(r0, r1, r2, r3, r4, r5, r6, r7, 8))
	binary.LittleEndian.PutUint64(dst[2*16:2*16+8], packColumn8ARM64(r0, r1, r2, r3, r4, r5, r6, r7, 16))
	binary.LittleEndian.PutUint64(dst[3*16:3*16+8], packColumn8ARM64(r0, r1, r2, r3, r4, r5, r6, r7, 24))
	binary.LittleEndian.PutUint64(dst[4*16:4*16+8], packColumn8ARM64(r0, r1, r2, r3, r4, r5, r6, r7, 32))
	binary.LittleEndian.PutUint64(dst[5*16:5*16+8], packColumn8ARM64(r0, r1, r2, r3, r4, r5, r6, r7, 40))
	binary.LittleEndian.PutUint64(dst[6*16:6*16+8], packColumn8ARM64(r0, r1, r2, r3, r4, r5, r6, r7, 48))
	binary.LittleEndian.PutUint64(dst[7*16:7*16+8], packColumn8ARM64(r0, r1, r2, r3, r4, r5, r6, r7, 56))
}

// scatterV8x8ARM64 writes the modified rows [first..first+nrows-1] of
// tmp back to the corresponding column positions in s, scattering only
// the first 8 lanes of each tmp row (the active chroma rows).
func scatterV8x8ARM64(s []byte, stride int, tmp *[8 * 16]byte, first int, nrows int) {
	src := tmp[:]
	for i := range 8 {
		row := s[i*stride : i*stride+8]
		for r := range nrows {
			row[first+r] = src[(first+r)*16+i]
		}
	}
}

func packColumn8ARM64(r0, r1, r2, r3, r4, r5, r6, r7 uint64, shift uint) uint64 {
	return uint64(byte(r0>>shift)) |
		uint64(byte(r1>>shift))<<8 |
		uint64(byte(r2>>shift))<<16 |
		uint64(byte(r3>>shift))<<24 |
		uint64(byte(r4>>shift))<<32 |
		uint64(byte(r5>>shift))<<40 |
		uint64(byte(r6>>shift))<<48 |
		uint64(byte(r7>>shift))<<56
}

// loopFilterSimpleHorizontalEdgeDispatch routes the 16-wide simple-LF
// horizontal edge through the NEON kernel when the input window is
// large enough; otherwise falls back to the libvpx-style scalar.
func loopFilterSimpleHorizontalEdgeDispatch(s []byte, stride int, blimit byte) {
	if len(s) >= 3*stride+16 {
		loopFilterSimpleEdgeH16NEON(unsafe.SliceData(s), stride, blimit)
		return
	}
	loopFilterSimpleHorizontalEdgeScalar(s, stride, blimit)
}

// loopFilterSimpleVerticalEdgeDispatch invokes the direct vertical-edge
// NEON kernel: caller passes the slice base; the kernel reads 4 bytes
// per row at &s[2]-2 = &s[0] across 16 rows and writes 2 modified bytes
// per row at &s[2]-1 = &s[1].
func loopFilterSimpleVerticalEdgeDispatch(s []byte, stride int, blimit byte) {
	if len(s) >= 15*stride+4 {
		loopFilterSimpleEdgeV16NEON((*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(s)), 2)), stride, blimit)
		return
	}
	loopFilterSimpleVerticalEdgeScalar(s, stride, blimit)
}
