//go:build arm64 && !purego

package dsp

import (
	"encoding/binary"

	vp8dsp "github.com/thesyncim/govpx/internal/vp8/dsp"
)

func vpxLpfHorizontal4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if vp9LpfHorizontal4Tap8NEON(plane, s-4*pitch, pitch, blimit, limit, thresh) {
		return
	}
	vpxLpfHorizontal4Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if vp9LpfVertical4Tap8NEON(plane, s-4, pitch, blimit, limit, thresh) {
		return
	}
	vpxLpfVertical4Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if canUseLoopfilter4TapNEON(blimit0) &&
		sameLoopfilterTriplet(blimit0, limit0, thresh0, blimit1, limit1, thresh1) {
		start := s - 4*pitch
		if hasHorizontalLfWindow(plane, start, pitch, 16) {
			vp8dsp.LoopFilterEdgeH16NEON(&plane[start], pitch, blimit0, limit0, thresh0)
			return
		}
	}
	vpxLpfHorizontal4(plane, s, pitch, blimit0, limit0, thresh0)
	vpxLpfHorizontal4(plane, s+8, pitch, blimit1, limit1, thresh1)
}

func vpxLpfVertical4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if canUseLoopfilter4TapNEON(blimit0) &&
		sameLoopfilterTriplet(blimit0, limit0, thresh0, blimit1, limit1, thresh1) &&
		hasVerticalLfWindow(plane, s-4, pitch, 16) {
		vp8dsp.LoopFilterEdgeV16NEON(&plane[s], pitch, blimit0, limit0, thresh0)
		return
	}
	vpxLpfVertical4(plane, s, pitch, blimit0, limit0, thresh0)
	vpxLpfVertical4(plane, s+8*pitch, pitch, blimit1, limit1, thresh1)
}

func vpxLpfHorizontal8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfHorizontal8Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfVertical8Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfHorizontal8DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfVertical8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfVertical8DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func sameLoopfilterTriplet(blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8) bool {
	return blimit0 == blimit1 && limit0 == limit1 && thresh0 == thresh1
}

func vp9LpfHorizontal4Tap8NEON(plane []uint8, start, pitch int, blimit, limit, thresh uint8) bool {
	if !canUseLoopfilter4TapNEON(blimit) || !hasHorizontalLfWindow(plane, start, pitch, 8) {
		return false
	}
	return vp9LpfHorizontal4Tap8NEONAt(plane, start, pitch, blimit, limit, thresh)
}

func vp9LpfHorizontal4Tap8NEONAt(plane []uint8, start, pitch int, blimit, limit, thresh uint8) bool {
	var tmp [8 * 16]byte
	gatherHorizontalLf8x8(&tmp, plane[start:], pitch)
	vp8dsp.LoopFilterEdgeH16NEON(&tmp[0], 16, blimit, limit, thresh)
	scatterHorizontalLf8x8(plane[start:], pitch, &tmp, 2, 4)
	return true
}

func vp9LpfVertical4Tap8NEON(plane []uint8, start, pitch int, blimit, limit, thresh uint8) bool {
	if !canUseLoopfilter4TapNEON(blimit) || !hasVerticalLfWindow(plane, start, pitch, 8) {
		return false
	}
	return vp9LpfVertical4Tap8NEONAt(plane, start, pitch, blimit, limit, thresh)
}

func vp9LpfVertical4Tap8NEONAt(plane []uint8, start, pitch int, blimit, limit, thresh uint8) bool {
	var tmp [8 * 16]byte
	gatherVerticalLf8x8(&tmp, plane[start:], pitch)
	vp8dsp.LoopFilterEdgeH16NEON(&tmp[0], 16, blimit, limit, thresh)
	scatterVerticalLf8x8(plane[start:], pitch, &tmp, 2, 4)
	return true
}

func hasHorizontalLfWindow(plane []uint8, start, pitch, width int) bool {
	return start >= 0 && pitch > 0 && width > 0 && len(plane) >= start+7*pitch+width
}

func canUseLoopfilter4TapNEON(blimit uint8) bool {
	return blimit != 255
}

func hasVerticalLfWindow(plane []uint8, start, pitch, rows int) bool {
	return start >= 0 && pitch > 0 && rows > 0 && len(plane) >= start+(rows-1)*pitch+8
}

func gatherHorizontalLf8x8(tmp *[8 * 16]byte, s []uint8, pitch int) {
	dst := tmp[:]
	for r := range 8 {
		w := binary.LittleEndian.Uint64(s[r*pitch : r*pitch+8])
		binary.LittleEndian.PutUint64(dst[r*16:r*16+8], w)
	}
}

func scatterHorizontalLf8x8(s []uint8, pitch int, tmp *[8 * 16]byte, first, nrows int) {
	src := tmp[:]
	for r := range nrows {
		w := binary.LittleEndian.Uint64(src[(first+r)*16 : (first+r)*16+8])
		binary.LittleEndian.PutUint64(s[(first+r)*pitch:(first+r)*pitch+8], w)
	}
}

func gatherVerticalLf8x8(tmp *[8 * 16]byte, s []uint8, pitch int) {
	dst := tmp[:]
	r0 := binary.LittleEndian.Uint64(s[0*pitch : 0*pitch+8])
	r1 := binary.LittleEndian.Uint64(s[1*pitch : 1*pitch+8])
	r2 := binary.LittleEndian.Uint64(s[2*pitch : 2*pitch+8])
	r3 := binary.LittleEndian.Uint64(s[3*pitch : 3*pitch+8])
	r4 := binary.LittleEndian.Uint64(s[4*pitch : 4*pitch+8])
	r5 := binary.LittleEndian.Uint64(s[5*pitch : 5*pitch+8])
	r6 := binary.LittleEndian.Uint64(s[6*pitch : 6*pitch+8])
	r7 := binary.LittleEndian.Uint64(s[7*pitch : 7*pitch+8])
	binary.LittleEndian.PutUint64(dst[0*16:0*16+8], packLfColumn8(r0, r1, r2, r3, r4, r5, r6, r7, 0))
	binary.LittleEndian.PutUint64(dst[1*16:1*16+8], packLfColumn8(r0, r1, r2, r3, r4, r5, r6, r7, 8))
	binary.LittleEndian.PutUint64(dst[2*16:2*16+8], packLfColumn8(r0, r1, r2, r3, r4, r5, r6, r7, 16))
	binary.LittleEndian.PutUint64(dst[3*16:3*16+8], packLfColumn8(r0, r1, r2, r3, r4, r5, r6, r7, 24))
	binary.LittleEndian.PutUint64(dst[4*16:4*16+8], packLfColumn8(r0, r1, r2, r3, r4, r5, r6, r7, 32))
	binary.LittleEndian.PutUint64(dst[5*16:5*16+8], packLfColumn8(r0, r1, r2, r3, r4, r5, r6, r7, 40))
	binary.LittleEndian.PutUint64(dst[6*16:6*16+8], packLfColumn8(r0, r1, r2, r3, r4, r5, r6, r7, 48))
	binary.LittleEndian.PutUint64(dst[7*16:7*16+8], packLfColumn8(r0, r1, r2, r3, r4, r5, r6, r7, 56))
}

func scatterVerticalLf8x8(s []uint8, pitch int, tmp *[8 * 16]byte, first, nrows int) {
	src := tmp[:]
	for i := range 8 {
		row := s[i*pitch : i*pitch+8]
		for r := range nrows {
			row[first+r] = src[(first+r)*16+i]
		}
	}
}

func packLfColumn8(r0, r1, r2, r3, r4, r5, r6, r7 uint64, shift uint) uint64 {
	return uint64(byte(r0>>shift)) |
		uint64(byte(r1>>shift))<<8 |
		uint64(byte(r2>>shift))<<16 |
		uint64(byte(r3>>shift))<<24 |
		uint64(byte(r4>>shift))<<32 |
		uint64(byte(r5>>shift))<<40 |
		uint64(byte(r6>>shift))<<48 |
		uint64(byte(r7>>shift))<<56
}
