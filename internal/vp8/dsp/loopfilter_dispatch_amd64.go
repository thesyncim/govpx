//go:build amd64 && !purego

package dsp

// amd64 dispatch for VP8 loop-filter apply paths (libvpx v1.16.0 baseline).
// SSE2 kernels handle the horizontal-edge cases for both inner and MB
// filters. The kernel itself is 16-wide; count=2 (luma, 16 columns)
// feeds it directly. count=1 (chroma, 8 columns) feeds the same kernel
// after gathering 8x8 into a [8*16]byte buffer with the high 8 lanes
// padded — every op in the kernel is per-byte (PSUBUSB / PSUBSB / PMAXUB
// / PADDSB / PSRAW / PACKSSWB ...) so lanes are independent and the
// padding bytes don't affect the active 8 lanes' output.
// Vertical-edge variants gather the row-window into a transposed
// stack buffer (16x8 for count=2, 8x8 for count=1 with high lanes
// padded), run the same SSE2 horizontal kernel, then scatter the
// modified rows back.

import (
	"encoding/binary"
	"unsafe"

	"github.com/thesyncim/govpx/internal/cpu"
)

// loopFilterEdgeH16 routes to the AVX2 VEX-encoded kernel when
// available; otherwise falls back to SSE2. Both kernels operate on
// the same 16-wide horizontal-edge window and produce byte-identical
// output (the AVX2 kernel mirrors the SSE2 schedule with 3-op form
// and zero functional changes).
func loopFilterEdgeH16(src *byte, pitch int, blimit, limit, thresh byte) {
	if cpu.HasAVX2 {
		loopFilterEdgeH16AVX2(src, pitch, blimit, limit, thresh)
		return
	}
	loopFilterEdgeH16SSE2(src, pitch, blimit, limit, thresh)
}

func mbLoopFilterEdgeH16(src *byte, pitch int, blimit, limit, thresh byte) {
	if cpu.HasAVX2 {
		mbLoopFilterEdgeH16AVX2(src, pitch, blimit, limit, thresh)
		return
	}
	mbLoopFilterEdgeH16SSE2(src, pitch, blimit, limit, thresh)
}

func loopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 7*stride+16 {
		// len check already guarantees s is non-empty, so SliceData
		// folds away the &s[0] bounds-check + stack frame.
		loopFilterEdgeH16(unsafe.SliceData(s), stride, blimit, limit, thresh)
		return
	}
	if count == 1 && len(s) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherH8x8AMD64(&tmp, s, stride)
		loopFilterEdgeH16((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterH8x8AMD64(s, stride, &tmp, 2, 4)
		return
	}
	loopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterHorizontalEdgesYDispatch(s []byte, stride int, blimit, limit, thresh byte) {
	if len(s) >= 15*stride+16 {
		base := unsafe.Pointer(unsafe.SliceData(s))
		loopFilterEdgeH16((*byte)(base), stride, blimit, limit, thresh)
		loopFilterEdgeH16((*byte)(unsafe.Add(base, 4*stride)), stride, blimit, limit, thresh)
		loopFilterEdgeH16((*byte)(unsafe.Add(base, 8*stride)), stride, blimit, limit, thresh)
		return
	}
	loopFilterHorizontalEdgeDispatch(s, stride, blimit, limit, thresh, 2)
	loopFilterHorizontalEdgeDispatch(s[4*stride:], stride, blimit, limit, thresh, 2)
	loopFilterHorizontalEdgeDispatch(s[8*stride:], stride, blimit, limit, thresh, 2)
}

func loopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 15*stride+8 {
		var tmp [8 * 16]byte
		base := unsafe.SliceData(s)
		gatherV16x8AMD64SSE2(&tmp, base, stride)
		loopFilterEdgeH16((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV16x8AMD64SSE2(base, stride, &tmp)
		return
	}
	if count == 1 && len(s) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherV8x8AMD64(&tmp, s, stride)
		loopFilterEdgeH16((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV8x8AMD64(s, stride, &tmp, 2, 4)
		return
	}
	loopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterVerticalEdgesYDispatch(s []byte, stride int, blimit, limit, thresh byte) {
	loopFilterVerticalEdgeDispatch(s, stride, blimit, limit, thresh, 2)
	loopFilterVerticalEdgeDispatch(s[4:], stride, blimit, limit, thresh, 2)
	loopFilterVerticalEdgeDispatch(s[8:], stride, blimit, limit, thresh, 2)
}

func loopFilterHorizontalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if len(u) >= 7*stride+8 && len(v) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherH8x8PairAMD64(&tmp, u, v, stride)
		loopFilterEdgeH16((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterH8x8PairAMD64(u, v, stride, &tmp, 2, 4)
		return
	}
	loopFilterHorizontalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	loopFilterHorizontalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func loopFilterVerticalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if len(u) >= 7*stride+8 && len(v) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherV8x8PairAMD64(&tmp, u, v, stride)
		loopFilterEdgeH16((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV8x8PairAMD64(u, v, stride, &tmp, 2, 4)
		return
	}
	loopFilterVerticalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	loopFilterVerticalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func mbLoopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 7*stride+16 {
		mbLoopFilterEdgeH16(unsafe.SliceData(s), stride, blimit, limit, thresh)
		return
	}
	if count == 1 && len(s) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherH8x8AMD64(&tmp, s, stride)
		mbLoopFilterEdgeH16((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterH8x8AMD64(s, stride, &tmp, 1, 6)
		return
	}
	mbLoopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 15*stride+8 {
		var tmp [8 * 16]byte
		base := unsafe.SliceData(s)
		gatherV16x8AMD64SSE2(&tmp, base, stride)
		mbLoopFilterEdgeH16((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV16x8AMD64SSE2(base, stride, &tmp)
		return
	}
	if count == 1 && len(s) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherV8x8AMD64(&tmp, s, stride)
		mbLoopFilterEdgeH16((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV8x8AMD64(s, stride, &tmp, 1, 6)
		return
	}
	mbLoopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterHorizontalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if len(u) >= 7*stride+8 && len(v) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherH8x8PairAMD64(&tmp, u, v, stride)
		mbLoopFilterEdgeH16((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterH8x8PairAMD64(u, v, stride, &tmp, 1, 6)
		return
	}
	mbLoopFilterHorizontalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	mbLoopFilterHorizontalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func mbLoopFilterVerticalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if len(u) >= 7*stride+8 && len(v) >= 7*stride+8 {
		var tmp [8 * 16]byte
		gatherV8x8PairAMD64(&tmp, u, v, stride)
		mbLoopFilterEdgeH16((*byte)(unsafe.Pointer(&tmp[0])), 16, blimit, limit, thresh)
		scatterV8x8PairAMD64(u, v, stride, &tmp, 1, 6)
		return
	}
	mbLoopFilterVerticalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	mbLoopFilterVerticalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

// loopFilterSimpleHorizontalEdgeDispatch routes the 16-wide simple-LF
// horizontal edge through the SSE2 kernel when the input window is
// large enough; otherwise falls back to the libvpx-style scalar.
func loopFilterSimpleHorizontalEdgeDispatch(s []byte, stride int, blimit byte) {
	if len(s) >= 3*stride+16 {
		loopFilterSimpleEdgeH16SSE2(unsafe.SliceData(s), stride, blimit)
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
		for i := range 16 {
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
	for i := range 16 {
		row := s[i*stride : i*stride+4]
		dst[0*16+i] = row[0]
		dst[1*16+i] = row[1]
		dst[2*16+i] = row[2]
		dst[3*16+i] = row[3]
	}
}

// gatherH8x8AMD64 copies 8 rows of 8 bytes into a [8*16]byte stack
// buffer at row stride 16. The high 8 lanes of each row are zeroed —
// the H16 kernel filters all 16 lanes but lanes 8..15 are inactive
// downstream because we only scatter back the first 8 lanes per row.
func gatherH8x8AMD64(tmp *[8 * 16]byte, s []byte, stride int) {
	dst := tmp[:]
	for r := range 8 {
		base := r * 16
		w := binary.LittleEndian.Uint64(s[r*stride : r*stride+8])
		binary.LittleEndian.PutUint64(dst[base:base+8], w)
		// Padding lanes 8..15 — zero is fine; they're just dummy
		// inputs that the kernel filters but we never read back.
		binary.LittleEndian.PutUint64(dst[base+8:base+16], 0)
	}
}

// scatterH8x8AMD64 writes the modified rows [first..first+nrows-1] of
// tmp back to the corresponding source rows of s, copying only the
// first 8 lanes (the chroma 8-wide window).
func scatterH8x8AMD64(s []byte, stride int, tmp *[8 * 16]byte, first int, nrows int) {
	src := tmp[:]
	for r := range nrows {
		w := binary.LittleEndian.Uint64(src[(first+r)*16 : (first+r)*16+8])
		binary.LittleEndian.PutUint64(s[(first+r)*stride:(first+r)*stride+8], w)
	}
}

func gatherH8x8PairAMD64(tmp *[8 * 16]byte, u []byte, v []byte, stride int) {
	dst := tmp[:]
	for r := range 8 {
		base := r * 16
		uw := binary.LittleEndian.Uint64(u[r*stride : r*stride+8])
		vw := binary.LittleEndian.Uint64(v[r*stride : r*stride+8])
		binary.LittleEndian.PutUint64(dst[base:base+8], uw)
		binary.LittleEndian.PutUint64(dst[base+8:base+16], vw)
	}
}

func scatterH8x8PairAMD64(u []byte, v []byte, stride int, tmp *[8 * 16]byte, first int, nrows int) {
	src := tmp[:]
	for r := range nrows {
		base := (first + r) * 16
		uw := binary.LittleEndian.Uint64(src[base : base+8])
		vw := binary.LittleEndian.Uint64(src[base+8 : base+16])
		binary.LittleEndian.PutUint64(u[(first+r)*stride:(first+r)*stride+8], uw)
		binary.LittleEndian.PutUint64(v[(first+r)*stride:(first+r)*stride+8], vw)
	}
}

// gatherV8x8AMD64 reads 8 rows of 8 bytes each from s and packs them
// into tmp at the count=2 vertical-edge transpose layout, but only
// fills lanes 0..7. tmp[r*16+i] = s[i*stride+r] for i in 0..7,
// r in 0..7. Lanes 8..15 are zeroed — they're inactive on writeback
// (only lanes 0..7 are scattered).
func gatherV8x8AMD64(tmp *[8 * 16]byte, s []byte, stride int) {
	dst := tmp[:]
	// Zero the whole 128-byte buffer first so the high 8 lanes of
	// every row carry deterministic padding bytes.
	for i := range dst {
		dst[i] = 0
	}
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

// scatterV8x8AMD64 writes the modified rows [first..first+nrows-1] of
// tmp back to the corresponding column positions in s, scattering
// only the first 8 lanes of each tmp row (the active chroma rows).
func scatterV8x8AMD64(s []byte, stride int, tmp *[8 * 16]byte, first int, nrows int) {
	src := tmp[:]
	for i := range 8 {
		row := s[i*stride : i*stride+8]
		for r := range nrows {
			row[first+r] = src[(first+r)*16+i]
		}
	}
}

func gatherV8x8PairAMD64(tmp *[8 * 16]byte, u []byte, v []byte, stride int) {
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

func scatterV8x8PairAMD64(u []byte, v []byte, stride int, tmp *[8 * 16]byte, first int, nrows int) {
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
