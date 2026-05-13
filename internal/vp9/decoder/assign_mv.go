package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// VP9 per-block MV assignment. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodemv.c — assign_mv plus the four small helpers
// is_mv_valid / copy_mv_pair / zero_mv_pair / read_is_inter_block.
//
// assign_mv is the leaf that converts a decoded inter-mode (NEARESTMV /
// NEARMV / ZEROMV / NEWMV) and a pre-computed reference/best-near
// candidate set into the final motion-vector pair for the block:
//
//   - NEWMV  : ReadMv against ref_mv[i] for each ref half.
//   - NEAREST: copy from near_nearest_mv (the post-MV-ref-search slot).
//   - NEAR   : same as NEAREST — both use the (near, nearest) slot.
//   - ZEROMV : both halves zeroed.
//
// The validity check (MV_LOW < component < MV_UPP) gates whether
// libvpx accepts the block or marks the frame corrupt.

// IsMvValid mirrors libvpx's is_mv_valid. An MV component crosses
// into "corrupt" territory when it touches the saturation bound.
func IsMvValid(mv MV) bool {
	return int(mv.Row) > MvLow && int(mv.Row) < MvUpp &&
		int(mv.Col) > MvLow && int(mv.Col) < MvUpp
}

// CopyMvPair mirrors libvpx's copy_mv_pair memcpy.
func CopyMvPair(dst, src *[2]MV) { *dst = *src }

// ZeroMvPair mirrors libvpx's zero_mv_pair memset.
func ZeroMvPair(dst *[2]MV) { *dst = [2]MV{} }

// AssignMv mirrors libvpx's assign_mv. Writes the final MV pair
// into `mv`, picks based on `mode`, and returns 1 if every decoded
// component lands inside [MvLow+1, MvUpp-1] — matches the libvpx
// validity gate the frame-corrupt path consults.
//
// Inputs:
//   - mode      : decoded inter PREDICTION_MODE (NEARESTMV..NEWMV).
//   - mv        : output pair, 2 slots — both written for compound.
//   - refMv     : per-ref `best_ref_mvs[i]` from the MV-ref search.
//   - nearNearest: the (NEARESTMV/NEARMV) candidate output of the
//     ref-search; libvpx fills it in dec_find_mv_refs.
//   - isCompound: 1 when ref_frame[1] > INTRA_FRAME (i.e. two halves).
//   - allowHp   : frame-level allow-high-precision-MV flag.
//   - r         : boolean range coder; only consumed when mode==NEWMV.
//   - fc        : entropy context (Nmvc carries the MV PMF rows).
func AssignMv(
	mode common.PredictionMode,
	mv, refMv, nearNearest *[2]MV,
	isCompound int,
	allowHp bool,
	r *bitstream.Reader,
	fc *FrameContext,
) int {
	switch mode {
	case common.NewMv:
		ret := 1
		halves := 1 + isCompound
		for i := 0; i < halves; i++ {
			ReadMv(r, &mv[i], &refMv[i], &fc.Nmvc, allowHp)
			if !IsMvValid(mv[i]) {
				ret = 0
			}
		}
		return ret
	case common.NearMv, common.NearestMv:
		CopyMvPair(mv, nearNearest)
		return 1
	case common.ZeroMv:
		ZeroMvPair(mv)
		return 1
	}
	return 0
}

// ReadIsInterBlock mirrors libvpx's read_is_inter_block. The
// SEG_LVL_REF_FRAME feature overrides the per-block bit: a
// non-INTRA_FRAME segment ref-frame forces is_inter=1 (and zero
// forces intra). Without the override, the bit is read against
// fc.IntraInterProb[GetIntraInterContext()].
func ReadIsInterBlock(r *bitstream.Reader, seg *SegmentationParams, segID int,
	fc *FrameContext, above, left *NeighborMi,
) int {
	if SegFeatureActive(seg, segID, SegLvlRefFrame) {
		if int(GetSegData(seg, segID, SegLvlRefFrame)) != IntraFrame {
			return 1
		}
		return 0
	}
	ctx := GetIntraInterContext(above, left)
	return int(r.Read(uint32(fc.IntraInterProb[ctx])))
}
