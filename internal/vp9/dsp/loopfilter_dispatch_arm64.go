//go:build arm64 && !purego

package dsp

import (
	"encoding/binary"

	vp8dsp "github.com/thesyncim/govpx/internal/vp8/dsp"
)

//go:noescape
func lpfHorizontal8NEON(s *byte, pitch int, blimit, limit, thresh byte)

//go:noescape
func lpfVertical8NEON(s *byte, pitch int, blimit, limit, thresh byte)

//go:noescape
func lpfHorizontal8DualNEON(s *byte, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 *byte)

//go:noescape
func lpfVertical8DualNEON(s *byte, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 *byte)

//go:noescape
func lpfHorizontal4NEON(s *byte, pitch int, blimit, limit, thresh *byte)

//go:noescape
func lpfVertical4NEON(s *byte, pitch int, blimit, limit, thresh *byte)

//go:noescape
func lpfHorizontal4DualNEON(s *byte, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 *byte)

//go:noescape
func lpfVertical4DualNEON(s *byte, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 *byte)

//go:noescape
func lpfHorizontal16NEON(s *byte, pitch int, blimit, limit, thresh *byte)

//go:noescape
func lpfVertical16NEON(s *byte, pitch int, blimit, limit, thresh *byte)

// vpxLpfHorizontal16Neon dispatches vpx_lpf_horizontal_16_neon: 16 rows
// read around the edge (s-8*pitch..s+7*pitch), 8 columns.
func vpxLpfHorizontal16Neon(plane []uint8, s, pitch int, blimit, limit, thresh uint8) bool {
	start := s - 8*pitch
	if !canUseLoopfilter4TapNEON(blimit) || start < 0 || pitch <= 0 ||
		len(plane) < start+15*pitch+8 {
		return false
	}
	thr := [3]byte{blimit, limit, thresh}
	lpfHorizontal16NEON(&plane[s], pitch, &thr[0], &thr[1], &thr[2])
	return true
}

// vpxLpfVertical16Neon dispatches vpx_lpf_vertical_16_neon: 8 rows x
// 16 columns read around the edge (s-8..s+7).
func vpxLpfVertical16Neon(plane []uint8, s, pitch int, blimit, limit, thresh uint8) bool {
	start := s - 8
	if !canUseLoopfilter4TapNEON(blimit) || start < 0 || pitch <= 0 ||
		len(plane) < start+7*pitch+16 {
		return false
	}
	thr := [3]byte{blimit, limit, thresh}
	lpfVertical16NEON(&plane[s], pitch, &thr[0], &thr[1], &thr[2])
	return true
}

func vpxLpfHorizontal4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if canUseLoopfilter4TapNEON(blimit) &&
		hasHorizontalLfWindow(plane, s-4*pitch, pitch, 8) {
		thr := [3]byte{blimit, limit, thresh}
		lpfHorizontal4NEON(&plane[s], pitch, &thr[0], &thr[1], &thr[2])
		return
	}
	vpxLpfHorizontal4Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if canUseLoopfilter4TapNEON(blimit) &&
		hasVerticalLfWindow(plane, s-4, pitch, 8) {
		thr := [3]byte{blimit, limit, thresh}
		lpfVertical4NEON(&plane[s], pitch, &thr[0], &thr[1], &thr[2])
		return
	}
	vpxLpfVertical4Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if canUseLoopfilter4TapNEON(blimit0) && canUseLoopfilter4TapNEON(blimit1) &&
		hasHorizontalLfWindow(plane, s-4*pitch, pitch, 16) {
		thr := [6]byte{blimit0, limit0, thresh0, blimit1, limit1, thresh1}
		lpfHorizontal4DualNEON(&plane[s], pitch,
			&thr[0], &thr[1], &thr[2], &thr[3], &thr[4], &thr[5])
		return
	}
	vpxLpfHorizontal4(plane, s, pitch, blimit0, limit0, thresh0)
	vpxLpfHorizontal4(plane, s+8, pitch, blimit1, limit1, thresh1)
}

func vpxLpfVertical4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if canUseLoopfilter4TapNEON(blimit0) && canUseLoopfilter4TapNEON(blimit1) &&
		hasVerticalLfWindow(plane, s-4, pitch, 16) {
		thr := [6]byte{blimit0, limit0, thresh0, blimit1, limit1, thresh1}
		lpfVertical4DualNEON(&plane[s], pitch,
			&thr[0], &thr[1], &thr[2], &thr[3], &thr[4], &thr[5])
		return
	}
	vpxLpfVertical4(plane, s, pitch, blimit0, limit0, thresh0)
	vpxLpfVertical4(plane, s+8*pitch, pitch, blimit1, limit1, thresh1)
}

func vpxLpfHorizontal8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfHorizontal8Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal8NEON(plane []uint8, s, pitch int, blimit, limit, thresh uint8) bool {
	if canUseLoopfilter4TapNEON(blimit) && hasHorizontalLfWindow(plane, s-4*pitch, pitch, 8) {
		lpfHorizontal8NEON(&plane[s], pitch, blimit, limit, thresh)
		return true
	}
	return false
}

func vpxLpfVertical8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfVertical8Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical8NEON(plane []uint8, s, pitch int, blimit, limit, thresh uint8) bool {
	if canUseLoopfilter4TapNEON(blimit) && hasVerticalLfWindow(plane, s-4, pitch, 8) {
		lpfVertical8NEON(&plane[s], pitch, blimit, limit, thresh)
		return true
	}
	return false
}

func vpxLpfHorizontal8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	// The 16-lane dual kernel reads and writes rows s-4*pitch..s+3*pitch
	// x 16 columns in one pass, mirroring vpx_lpf_horizontal_8_dual_neon.
	// blimit==255 falls back: the kernel's saturating mask sum diverges
	// from the scalar int reference there (unreachable in-spec, where
	// mblim <= 2*(63+2)+63).
	if canUseLoopfilter4TapNEON(blimit0) && canUseLoopfilter4TapNEON(blimit1) &&
		hasHorizontalLfWindow(plane, s-4*pitch, pitch, 16) {
		thr := [6]byte{blimit0, limit0, thresh0, blimit1, limit1, thresh1}
		lpfHorizontal8DualNEON(&plane[s], pitch,
			&thr[0], &thr[1], &thr[2], &thr[3], &thr[4], &thr[5])
		return
	}
	if !vpxLpfHorizontal8NEON(plane, s, pitch, blimit0, limit0, thresh0) {
		vpxLpfHorizontal8Scalar(plane, s, pitch, blimit0, limit0, thresh0)
	}
	if !vpxLpfHorizontal8NEON(plane, s+8, pitch, blimit1, limit1, thresh1) {
		vpxLpfHorizontal8Scalar(plane, s+8, pitch, blimit1, limit1, thresh1)
	}
}

func vpxLpfVertical8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	// The 16-lane dual kernel reads and writes rows s..s+15*pitch x
	// columns s-4..s+3 in one pass, mirroring vpx_lpf_vertical_8_dual_neon.
	// blimit==255 falls back (see horizontal note).
	if canUseLoopfilter4TapNEON(blimit0) && canUseLoopfilter4TapNEON(blimit1) &&
		hasVerticalLfWindow(plane, s-4, pitch, 16) {
		thr := [6]byte{blimit0, limit0, thresh0, blimit1, limit1, thresh1}
		lpfVertical8DualNEON(&plane[s], pitch,
			&thr[0], &thr[1], &thr[2], &thr[3], &thr[4], &thr[5])
		return
	}
	if !vpxLpfVertical8NEON(plane, s, pitch, blimit0, limit0, thresh0) {
		vpxLpfVertical8Scalar(plane, s, pitch, blimit0, limit0, thresh0)
	}
	if !vpxLpfVertical8NEON(plane, s+8*pitch, pitch, blimit1, limit1, thresh1) {
		vpxLpfVertical8Scalar(plane, s+8*pitch, pitch, blimit1, limit1, thresh1)
	}
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

func vpxLpfHorizontal16(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if vpxLpfHorizontal16Neon(plane, s, pitch, blimit, limit, thresh) {
		return
	}
	mbLpfHorizontalEdgeW(plane, s, pitch, blimit, limit, thresh, 1)
}

// vpxLpfHorizontal16Dual mirrors vpx_lpf_horizontal_16_dual_neon's
// count=2 semantics as two single-edge kernel passes. Both windows are
// validated up front so a fallback never refilters half-written pixels.
func vpxLpfHorizontal16Dual(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	start := s - 8*pitch
	if canUseLoopfilter4TapNEON(blimit) && start >= 0 && pitch > 0 &&
		len(plane) >= start+15*pitch+16 {
		thr := [3]byte{blimit, limit, thresh}
		lpfHorizontal16NEON(&plane[s], pitch, &thr[0], &thr[1], &thr[2])
		lpfHorizontal16NEON(&plane[s+8], pitch, &thr[0], &thr[1], &thr[2])
		return
	}
	mbLpfHorizontalEdgeW(plane, s, pitch, blimit, limit, thresh, 2)
}

func vpxLpfVertical16(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if vpxLpfVertical16Neon(plane, s, pitch, blimit, limit, thresh) {
		return
	}
	mbLpfVerticalEdgeW(plane, s, pitch, blimit, limit, thresh, 8)
}

// vpxLpfVertical16Dual mirrors vpx_lpf_vertical_16_dual_neon's count=16
// semantics as two single-edge kernel passes. Both windows are validated
// up front so a fallback never refilters half-written pixels.
func vpxLpfVertical16Dual(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	start := s - 8
	if canUseLoopfilter4TapNEON(blimit) && start >= 0 && pitch > 0 &&
		len(plane) >= start+15*pitch+16 {
		thr := [3]byte{blimit, limit, thresh}
		lpfVertical16NEON(&plane[s], pitch, &thr[0], &thr[1], &thr[2])
		lpfVertical16NEON(&plane[s+8*pitch], pitch, &thr[0], &thr[1], &thr[2])
		return
	}
	mbLpfVerticalEdgeW(plane, s, pitch, blimit, limit, thresh, 16)
}
