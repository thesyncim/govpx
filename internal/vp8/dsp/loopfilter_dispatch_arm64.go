//go:build arm64

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
		base := unsafe.Pointer(unsafe.SliceData(s))
		loopFilterEdgeH16NEON((*byte)(base), stride, blimit, limit, thresh)
		loopFilterEdgeH16NEON((*byte)(unsafe.Add(base, 4*stride)), stride, blimit, limit, thresh)
		loopFilterEdgeH16NEON((*byte)(unsafe.Add(base, 8*stride)), stride, blimit, limit, thresh)
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
		base := unsafe.Pointer(unsafe.SliceData(s))
		loopFilterEdgeV16NEON((*byte)(unsafe.Add(base, 4)), stride, blimit, limit, thresh)
		loopFilterEdgeV16NEON((*byte)(unsafe.Add(base, 8)), stride, blimit, limit, thresh)
		loopFilterEdgeV16NEON((*byte)(unsafe.Add(base, 12)), stride, blimit, limit, thresh)
		return
	}
	loopFilterVerticalEdgeDispatch(s, stride, blimit, limit, thresh, 2)
	loopFilterVerticalEdgeDispatch(s[4:], stride, blimit, limit, thresh, 2)
	loopFilterVerticalEdgeDispatch(s[8:], stride, blimit, limit, thresh, 2)
}

func loopFilterHorizontalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if len(u) >= 7*stride+8 && len(v) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherH8x8PairARM64(&tmp, u, v, stride)
		loopFilterEdgeH16NEON((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterH8x8PairARM64(u, v, stride, &tmp, 2, 4)
		return
	}
	loopFilterHorizontalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	loopFilterHorizontalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func loopFilterVerticalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if len(u) >= 7*stride+8 && len(v) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherV8x8PairARM64(&tmp, u, v, stride)
		loopFilterEdgeH16NEON((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV8x8PairARM64(u, v, stride, &tmp, 2, 4)
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
		var tmp [8 * 16]byte
		gatherH8x8PairARM64(&tmp, u, v, stride)
		mbLoopFilterEdgeH16NEON((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterH8x8PairARM64(u, v, stride, &tmp, 1, 6)
		return
	}
	mbLoopFilterHorizontalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	mbLoopFilterHorizontalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func mbLoopFilterVerticalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if len(u) >= 7*stride+8 && len(v) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherV8x8PairARM64(&tmp, u, v, stride)
		mbLoopFilterEdgeH16NEON((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV8x8PairARM64(u, v, stride, &tmp, 1, 6)
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

func gatherH8x8PairARM64(tmp *[8 * 16]byte, u []byte, v []byte, stride int) {
	dst := tmp[:]
	for r := range 8 {
		base := r * 16
		uw := binary.LittleEndian.Uint64(u[r*stride : r*stride+8])
		vw := binary.LittleEndian.Uint64(v[r*stride : r*stride+8])
		binary.LittleEndian.PutUint64(dst[base:base+8], uw)
		binary.LittleEndian.PutUint64(dst[base+8:base+16], vw)
	}
}

func scatterH8x8PairARM64(u []byte, v []byte, stride int, tmp *[8 * 16]byte, first int, nrows int) {
	src := tmp[:]
	for r := range nrows {
		base := (first + r) * 16
		uw := binary.LittleEndian.Uint64(src[base : base+8])
		vw := binary.LittleEndian.Uint64(src[base+8 : base+16])
		binary.LittleEndian.PutUint64(u[(first+r)*stride:(first+r)*stride+8], uw)
		binary.LittleEndian.PutUint64(v[(first+r)*stride:(first+r)*stride+8], vw)
	}
}

// gatherV8x8ARM64 reads 8 rows of 8 bytes each from s and packs them
// into tmp such that tmp[r*16+i] = s[i*stride+r] for i in 0..7,
// r in 0..7 — the same column-major transpose used for the count=2
// vertical-edge fallback. Lanes 8..15 are inactive on writeback.
func gatherV8x8ARM64(tmp *[8 * 16]byte, s []byte, stride int) {
	dst := tmp[:]
	for i := range 8 {
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

func gatherV8x8PairARM64(tmp *[8 * 16]byte, u []byte, v []byte, stride int) {
	dst := tmp[:]
	for i := range 8 {
		uw := binary.LittleEndian.Uint64(u[i*stride : i*stride+8])
		vw := binary.LittleEndian.Uint64(v[i*stride : i*stride+8])
		dst[0*16+i] = byte(uw)
		dst[1*16+i] = byte(uw >> 8)
		dst[2*16+i] = byte(uw >> 16)
		dst[3*16+i] = byte(uw >> 24)
		dst[4*16+i] = byte(uw >> 32)
		dst[5*16+i] = byte(uw >> 40)
		dst[6*16+i] = byte(uw >> 48)
		dst[7*16+i] = byte(uw >> 56)
		dst[0*16+8+i] = byte(vw)
		dst[1*16+8+i] = byte(vw >> 8)
		dst[2*16+8+i] = byte(vw >> 16)
		dst[3*16+8+i] = byte(vw >> 24)
		dst[4*16+8+i] = byte(vw >> 32)
		dst[5*16+8+i] = byte(vw >> 40)
		dst[6*16+8+i] = byte(vw >> 48)
		dst[7*16+8+i] = byte(vw >> 56)
	}
}

func scatterV8x8PairARM64(u []byte, v []byte, stride int, tmp *[8 * 16]byte, first int, nrows int) {
	src := tmp[:]
	for i := range 8 {
		urow := u[i*stride : i*stride+8]
		vrow := v[i*stride : i*stride+8]
		for r := range nrows {
			urow[first+r] = src[(first+r)*16+i]
			vrow[first+r] = src[(first+r)*16+8+i]
		}
	}
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
