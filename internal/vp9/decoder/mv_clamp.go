package decoder

// VP9 motion-vector clamping + precision helpers. Ported from libvpx
// v1.16.0 vp9/common/vp9_mvref_common.h and vp9/common/vp9_entropymv.h.

// MV magnitude limits from vp9_entropymv.h. The boolean coder
// represents an MV component as int16; the bounds keep the magnitude
// inside ±MV_UPP so encoder + decoder paths stay in lock-step on
// what range gets emitted.
const (
	MvInUseBits = 14
	MvUpp       = (1 << MvInUseBits) - 1
	MvLow       = -(1 << MvInUseBits)
)

// LowerMvPrecision mirrors lower_mv_precision in vp9_mvref_common.h.
// When the high-precision MV gate is closed (either by the frame
// flag OR by use_mv_hp(ref) on the candidate) any odd low bit gets
// stripped, rounding toward zero. The libvpx implementation lives
// on the candidate MV before assign_mv consults it.
func LowerMvPrecision(mv *MV, allowHp bool) {
	useHp := allowHp && useMvHp(mv)
	if useHp {
		return
	}
	if mv.Row&1 != 0 {
		if mv.Row > 0 {
			mv.Row -= 1
		} else {
			mv.Row += 1
		}
	}
	if mv.Col&1 != 0 {
		if mv.Col > 0 {
			mv.Col -= 1
		} else {
			mv.Col += 1
		}
	}
}

// ClampMv mirrors libvpx's clamp_mv. Saturates the (row,col) pair
// into the supplied bounding box, expressed in 1/8-pel units like
// the rest of the MV pipeline.
func ClampMv(mv *MV, minCol, maxCol, minRow, maxRow int32) {
	r := int32(mv.Row)
	c := int32(mv.Col)
	if r < minRow {
		r = minRow
	} else if r > maxRow {
		r = maxRow
	}
	if c < minCol {
		c = minCol
	} else if c > maxCol {
		c = maxCol
	}
	mv.Row = int16(r)
	mv.Col = int16(c)
}
