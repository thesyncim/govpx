package decoder

// VP9 inter-prediction MV helpers. Ported from libvpx v1.16.0
// vp9/common/vp9_reconinter.c — average_split_mvs and
// clamp_mv_to_umv_border_sb plus the constants and rounding helpers
// they consult.

// Subpel + interpolation margins, mirroring libvpx's
// vpx_dsp/vpx_filter.h and vpx_scale/yv12config.h.
const (
	SubpelBitsConst  = 4
	SubpelShifts     = 1 << SubpelBitsConst
	VP9InterpExtend  = 4
	VP9EncBorderPels = 160
	// LeftTopMargin / RightBottomMargin gate the per-block clamp
	// region against the frame's reconstruction border. The shift by
	// 3 converts pixels into 1/8-pel MV units.
	LeftTopMargin     = (VP9EncBorderPels - VP9InterpExtend) << 3
	RightBottomMargin = (VP9EncBorderPels - VP9InterpExtend) << 3
)

// roundMvCompQ4 mirrors libvpx's round_mv_comp_q4 — rounds an MV
// component to the nearest multiple of 4, halfway-away-from-zero.
func roundMvCompQ4(v int) int {
	if v < 0 {
		return (v - 2) / 4
	}
	return (v + 2) / 4
}

// roundMvCompQ2 mirrors round_mv_comp_q2.
func roundMvCompQ2(v int) int {
	if v < 0 {
		return (v - 1) / 2
	}
	return (v + 1) / 2
}

// miMvPredQ4 mirrors libvpx's mi_mv_pred_q4 — average of the four
// sub-block MVs at the given ref index, rounded to the original
// 1/8-pel grid.
func miMvPredQ4(bmi *[4]Bmi, idx int) MV {
	rowSum := int(bmi[0].AsMv[idx].Row) + int(bmi[1].AsMv[idx].Row) +
		int(bmi[2].AsMv[idx].Row) + int(bmi[3].AsMv[idx].Row)
	colSum := int(bmi[0].AsMv[idx].Col) + int(bmi[1].AsMv[idx].Col) +
		int(bmi[2].AsMv[idx].Col) + int(bmi[3].AsMv[idx].Col)
	return MV{Row: int16(roundMvCompQ4(rowSum)), Col: int16(roundMvCompQ4(colSum))}
}

// miMvPredQ2 mirrors mi_mv_pred_q2 — half-block average between two
// adjacent sub-blocks.
func miMvPredQ2(bmi *[4]Bmi, idx, block0, block1 int) MV {
	rowSum := int(bmi[block0].AsMv[idx].Row) + int(bmi[block1].AsMv[idx].Row)
	colSum := int(bmi[block0].AsMv[idx].Col) + int(bmi[block1].AsMv[idx].Col)
	return MV{Row: int16(roundMvCompQ2(rowSum)), Col: int16(roundMvCompQ2(colSum))}
}

// AverageSplitMvs mirrors libvpx's average_split_mvs. For a sub-8x8
// inter partition, depending on the chroma subsampling pair (ss_x,
// ss_y) the matching MV is either the per-subblock value (4:4:4),
// the vertical pair average (4:4:2), the horizontal pair average
// (4:2:2), or the 4-way average (4:2:0).
func AverageSplitMvs(bmi *[4]Bmi, ref, block int, ssX, ssY int) MV {
	idx := 0
	if ssX > 0 {
		idx |= 2
	}
	if ssY > 0 {
		idx |= 1
	}
	switch idx {
	case 0:
		return bmi[block].AsMv[ref]
	case 1:
		return miMvPredQ2(bmi, ref, block, block+2)
	case 2:
		return miMvPredQ2(bmi, ref, block, block+1)
	default:
		return miMvPredQ4(bmi, ref)
	}
}

// BlockBoundsEdges carries the per-block edge offsets that
// clamp_mv_to_umv_border_sb consults. Mirrors libvpx's xd->mb_to_*_edge
// quartet (signed 1/8-pel offsets to the frame margin).
type BlockBoundsEdges struct {
	MbToLeftEdge   int
	MbToRightEdge  int
	MbToTopEdge    int
	MbToBottomEdge int
}

// ClampMvToUmvBorderSb mirrors libvpx's clamp_mv_to_umv_border_sb.
// Takes a source MV in 1/8-pel units plus the block dimensions
// (bw, bh) in source-plane pixels and the chroma subsampling pair
// (ssX, ssY) — returns the MV scaled to 1/8-pel * 2^(1-ss) and
// saturated to keep the projected reference window inside the
// unrestricted motion-vector border.
func ClampMvToUmvBorderSb(edges BlockBoundsEdges, srcMv MV, bw, bh, ssX, ssY int) MV {
	spelLeft := (VP9InterpExtend + bw) << SubpelBitsConst
	spelRight := spelLeft - SubpelShifts
	spelTop := (VP9InterpExtend + bh) << SubpelBitsConst
	spelBottom := spelTop - SubpelShifts
	shiftY := 1 << uint(1-ssY)
	shiftX := 1 << uint(1-ssX)
	cmv := MV{
		Row: int16(int(srcMv.Row) * shiftY),
		Col: int16(int(srcMv.Col) * shiftX),
	}
	minCol := int32(edges.MbToLeftEdge*shiftX - spelLeft)
	maxCol := int32(edges.MbToRightEdge*shiftX + spelRight)
	minRow := int32(edges.MbToTopEdge*shiftY - spelTop)
	maxRow := int32(edges.MbToBottomEdge*shiftY + spelBottom)
	ClampMv(&cmv, minCol, maxCol, minRow, maxRow)
	return cmv
}
