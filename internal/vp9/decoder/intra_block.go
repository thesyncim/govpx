package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// VP9 per-block intra mode-info driver. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodemv.c — read_intra_frame_mode_info — and its
// transitive helpers in vp9/common/vp9_blockd.{h,c}: get_y_mode,
// vp9_above_block_mode, vp9_left_block_mode, get_y_mode_probs.
//
// The intra block driver decodes one MODE_INFO worth of intra Y +
// UV prediction modes plus the per-block side-channel signals
// (segment-id, skip, tx-size). Sub-8x8 partitions emit one mode per
// 4x4 sub-block; larger partitions write a single block-level mode.

// GetYMode mirrors libvpx's get_y_mode. For sub-8x8 partitions the
// `block` index selects one of the four bmi sub-modes; for ≥8x8
// partitions the single block-level mode applies.
func GetYMode(mi *NeighborMi, block int) common.PredictionMode {
	if mi.SbType < common.Block8x8 {
		return mi.Bmi[block].AsMode
	}
	return mi.Mode
}

// LeftBlockMode mirrors libvpx's vp9_left_block_mode. For sub-block
// indices 0 and 2, it looks one block to the left and reaches into
// that block's bmi quartet (or DC for inter-block / missing
// neighbors). For indices 1 and 3 it pulls the matching subblock out
// of the current MI.
func LeftBlockMode(curMi, leftMi *NeighborMi, b int) common.PredictionMode {
	if b == 0 || b == 2 {
		if leftMi == nil || isInterBlock(leftMi) {
			return common.DcPred
		}
		return GetYMode(leftMi, b+1)
	}
	return curMi.Bmi[b-1].AsMode
}

// AboveBlockMode mirrors libvpx's vp9_above_block_mode.
func AboveBlockMode(curMi, aboveMi *NeighborMi, b int) common.PredictionMode {
	if b == 0 || b == 1 {
		if aboveMi == nil || isInterBlock(aboveMi) {
			return common.DcPred
		}
		return GetYMode(aboveMi, b+2)
	}
	return curMi.Bmi[b-2].AsMode
}

// GetYModeProbs mirrors libvpx's get_y_mode_probs — keyframe Y-mode
// probability lookup keyed by (above_mode, left_mode). Returns the
// 9-element probability row that drives ReadIntraMode.
func GetYModeProbs(curMi, aboveMi, leftMi *NeighborMi, block int) []uint8 {
	above := AboveBlockMode(curMi, aboveMi, block)
	left := LeftBlockMode(curMi, leftMi, block)
	row := &tables.KfYModeProb[above][left]
	return row[:]
}

// ReadTxSize mirrors libvpx's read_tx_size. When the frame's TxMode
// is TxModeSelect AND the block is at least 8x8 AND the caller passed
// allowSelect=true, the per-block transform size is decoded from the
// boolean coder; otherwise it's clamped to the block's max via the
// frame-level TxMode-to-biggest-tx-size table.
func ReadTxSize(r *bitstream.Reader, fc *FrameContext, txMode common.TxMode,
	bsize common.BlockSize, above, left *NeighborMi, allowSelect bool,
) common.TxSize {
	maxTxSize := common.MaxTxsizeLookup[bsize]
	if allowSelect && txMode == common.TxModeSelect && bsize >= common.Block8x8 {
		ctx := GetTxSizeContext(above, left, maxTxSize)
		probs := getTxProbsRow(&fc.TxProbs, maxTxSize, ctx)
		return ReadSelectedTxSize(r, maxTxSize, probs)
	}
	cap := common.TxModeToBiggestTxSize[txMode]
	if maxTxSize < cap {
		return maxTxSize
	}
	return cap
}

// getTxProbsRow mirrors libvpx's get_tx_probs — selects the
// per-max-tx-size probability row out of TxProbs. Returns a slice
// aliasing the matching fixed-size sub-array (no allocation).
func getTxProbsRow(p *TxProbs, maxTxSize common.TxSize, ctx int) []uint8 {
	switch maxTxSize {
	case common.Tx8x8:
		return p.P8x8[ctx][:]
	case common.Tx16x16:
		return p.P16x16[ctx][:]
	case common.Tx32x32:
		return p.P32x32[ctx][:]
	}
	return nil
}

// ReadIntraBlockModeInfo mirrors libvpx's intra-block mode-info
// fragment from read_intra_frame_mode_info — the per-bsize Y-mode
// dispatch plus the UV-mode read. `out` is updated in place; callers
// pre-populate sb_type, ref_frame, and the side-channel signals.
//
// Layout per libvpx's switch:
//   - BLOCK_4X4: 4 sub-modes; mi->mode = bmi[3].
//   - BLOCK_4X8: bmi[0]=bmi[2] from block 0, bmi[1]=bmi[3]=mode from block 1.
//   - BLOCK_8X4: bmi[0]=bmi[1] from block 0, bmi[2]=bmi[3]=mode from block 2.
//   - else:      single mode from block 0.
//
// In every case `mi->uv_mode = read_intra_mode(r, kf_uv_mode_prob[mi->mode])`
// follows the Y dispatch.
func ReadIntraBlockModeInfo(r *bitstream.Reader, out *NeighborMi,
	aboveMi, leftMi *NeighborMi,
) (uvMode common.PredictionMode) {
	switch out.SbType {
	case common.Block4x4:
		for i := range 4 {
			out.Bmi[i].AsMode = ReadIntraMode(r, GetYModeProbs(out, aboveMi, leftMi, i))
		}
		out.Mode = out.Bmi[3].AsMode
	case common.Block4x8:
		out.Bmi[0].AsMode = ReadIntraMode(r, GetYModeProbs(out, aboveMi, leftMi, 0))
		out.Bmi[2].AsMode = out.Bmi[0].AsMode
		out.Bmi[1].AsMode = ReadIntraMode(r, GetYModeProbs(out, aboveMi, leftMi, 1))
		out.Bmi[3].AsMode = out.Bmi[1].AsMode
		out.Mode = out.Bmi[1].AsMode
	case common.Block8x4:
		out.Bmi[0].AsMode = ReadIntraMode(r, GetYModeProbs(out, aboveMi, leftMi, 0))
		out.Bmi[1].AsMode = out.Bmi[0].AsMode
		out.Bmi[2].AsMode = ReadIntraMode(r, GetYModeProbs(out, aboveMi, leftMi, 2))
		out.Bmi[3].AsMode = out.Bmi[2].AsMode
		out.Mode = out.Bmi[2].AsMode
	default:
		out.Mode = ReadIntraMode(r, GetYModeProbs(out, aboveMi, leftMi, 0))
	}
	uvRow := &tables.KfUvModeProb[out.Mode]
	uvMode = ReadIntraMode(r, uvRow[:])
	return uvMode
}

// VP9 keyframe + inter-frame intra block-mode drivers. Ported from
// libvpx v1.16.0 vp9/decoder/vp9_decodemv.c — read_intra_frame_mode_info
// (keyframe) and read_intra_block_mode_info (inter frame, intra block).
//
// These compose the per-block segment-id, skip, tx-size, and Y/UV
// mode reads that vp9_read_mode_info dispatches into when the block
// is an intra prediction. The Y/UV prob tables they consult differ:
//
//   - Keyframe path: probs are indexed by (above_mode, left_mode) via
//     KfYModeProb / KfUvModeProb (handled by ReadIntraBlockModeInfo
//     in intra_block.go).
//   - Inter-frame intra path: probs come from fc.YModeProb keyed by
//     size_group_lookup[bsize], and fc.UvModeProb (which lives in
//     ReadYModeProbs's table) keyed by the just-decoded Y mode.

// ReadIntraModeYInter mirrors libvpx's read_intra_mode_y. The
// size_group classification picks one of four probability rows.
func ReadIntraModeYInter(r *bitstream.Reader, fc *FrameContext, sizeGroup int) common.PredictionMode {
	row := &fc.YModeProb[sizeGroup]
	return ReadIntraMode(r, row[:])
}

// ReadIntraModeUvInter mirrors libvpx's read_intra_mode_uv. The UV
// probability row is keyed by the just-decoded Y mode. The fc keeps
// a 10x9 table; the keyframe path uses KfUvModeProb instead.
func ReadIntraModeUvInter(r *bitstream.Reader, fc *FrameContext, yMode common.PredictionMode) common.PredictionMode {
	row := &fc.UvModeProb[yMode]
	return ReadIntraMode(r, row[:])
}

// ReadIntraBlockModeInfoInter mirrors libvpx's read_intra_block_mode_info
// — the inter-frame intra-block path. Same per-BLOCK_SIZE switch as
// the keyframe driver but the size-group → fc.YModeProb lookup
// replaces the (above, left) → KfYModeProb pair, and the size_group
// for sub-8x8 partitions is forced to 0 to match libvpx.
//
// `out.InterpFilter` is set to SwitchableFilters as libvpx does so
// that subsequent get_pred_context_switchable_interp() against an
// intra neighbor reads the "no filter" sentinel.
func ReadIntraBlockModeInfoInter(r *bitstream.Reader, fc *FrameContext, out *NeighborMi) (uvMode common.PredictionMode) {
	switch out.SbType {
	case common.Block4x4:
		for i := range 4 {
			out.Bmi[i].AsMode = ReadIntraModeYInter(r, fc, 0)
		}
		out.Mode = out.Bmi[3].AsMode
	case common.Block4x8:
		out.Bmi[0].AsMode = ReadIntraModeYInter(r, fc, 0)
		out.Bmi[2].AsMode = out.Bmi[0].AsMode
		out.Bmi[1].AsMode = ReadIntraModeYInter(r, fc, 0)
		out.Bmi[3].AsMode = out.Bmi[1].AsMode
		out.Mode = out.Bmi[1].AsMode
	case common.Block8x4:
		out.Bmi[0].AsMode = ReadIntraModeYInter(r, fc, 0)
		out.Bmi[1].AsMode = out.Bmi[0].AsMode
		out.Bmi[2].AsMode = ReadIntraModeYInter(r, fc, 0)
		out.Bmi[3].AsMode = out.Bmi[2].AsMode
		out.Mode = out.Bmi[2].AsMode
	default:
		sg := int(common.SizeGroupLookup[out.SbType])
		out.Mode = ReadIntraModeYInter(r, fc, sg)
	}
	uvMode = ReadIntraModeUvInter(r, fc, out.Mode)
	out.InterpFilter = uint8(SwitchableFilters)
	return uvMode
}

// IntraFrameDriverArgs bundles the inputs to ReadIntraFrameModeInfo.
// `mi` holds the partially-populated MODE_INFO for the current block
// — SbType is read on entry; ref_frame / segment_id / skip / tx_size
// / Mode / Bmi / uv-mode are written out as the C source does.
type IntraFrameDriverArgs struct {
	Reader     *bitstream.Reader
	Fc         *FrameContext
	Seg        *SegmentationParams
	Maps       *IntraSegmentMaps
	TxMode     common.TxMode
	MiOffset   int
	XMis, YMis int
	Above      *NeighborMi
	Left       *NeighborMi
}

// IntraFrameModeInfoOut carries the per-block outputs of the
// keyframe intra-mode-info read.
type IntraFrameModeInfoOut struct {
	UvMode common.PredictionMode
}

// ReadIntraFrameModeInfo mirrors libvpx's read_intra_frame_mode_info
// — the full keyframe per-block driver. Decodes:
//
//  1. segment_id via ReadIntraSegmentId (gates on UpdateMap).
//  2. skip via ReadSkipWithSeg (gates on SEG_LVL_SKIP).
//  3. tx_size via ReadTxSize (gated by frame TxMode + sb_type).
//  4. ref_frame[0/1] forced to (IntraFrame, NoRefFrame).
//  5. Y/UV modes via ReadIntraBlockModeInfo (keyframe path, using
//     KfYModeProb keyed by neighbor modes and KfUvModeProb keyed by
//     the decoded Y mode).
//
// All side-effect writes land on `mi`; the returned struct surfaces
// signals (like uv_mode) that don't have a dedicated NeighborMi slot.
func ReadIntraFrameModeInfo(args IntraFrameDriverArgs, mi *NeighborMi) IntraFrameModeInfoOut {
	mi.RefFrame[0] = IntraFrame
	mi.RefFrame[1] = NoRefFrame

	segID := ReadIntraSegmentId(args.Reader, args.Seg, args.Maps,
		args.MiOffset, args.XMis, args.YMis)
	mi.SegmentID = uint8(segID)
	mi.SegIDPredicted = uint8(segID)

	mi.Skip = uint8(ReadSkipWithSeg(args.Reader, args.Seg, segID, args.Fc,
		args.Above, args.Left))

	mi.TxSize = ReadTxSize(args.Reader, args.Fc, args.TxMode, mi.SbType,
		args.Above, args.Left, true)

	uv := ReadIntraBlockModeInfo(args.Reader, mi, args.Above, args.Left)
	return IntraFrameModeInfoOut{UvMode: uv}
}

// VP9 intra-prediction predictor-need flags. Ported from libvpx
// v1.16.0 vp9/common/vp9_reconintra.c — the extend_modes[INTRA_MODES]
// table that drives whether the per-mode predictor reads the above
// row, the left column, or the above-right extension.
//
// The companion BuildIntraPredictors driver in vp9_predict_intra_block
// uses these flags to decide which border pixels to materialize before
// dispatching the DSP kernel.

const (
	// NeedLeft, NeedAbove, NeedAboveRight mirror libvpx's anonymous
	// enum at the top of vp9_reconintra.c.
	NeedLeft       = 1 << 1
	NeedAbove      = 1 << 2
	NeedAboveRight = 1 << 3
)

// ExtendModes mirrors libvpx's extend_modes[INTRA_MODES] table.
// Indexed by PredictionMode in the intra range (DcPred..TmPred).
var ExtendModes = [common.IntraModes]uint8{
	NeedAbove | NeedLeft, // DcPred
	NeedAbove,            // VPred
	NeedLeft,             // HPred
	NeedAboveRight,       // D45Pred
	NeedLeft | NeedAbove, // D135Pred
	NeedLeft | NeedAbove, // D117Pred
	NeedLeft | NeedAbove, // D153Pred
	NeedLeft,             // D207Pred
	NeedAboveRight,       // D63Pred
	NeedLeft | NeedAbove, // TmPred
}

// IntraNeedsLeft reports whether the predictor mode reads the left
// column of border pixels.
func IntraNeedsLeft(mode common.PredictionMode) bool {
	return ExtendModes[mode]&NeedLeft != 0
}

// IntraNeedsAbove reports whether the predictor reads the above row.
// True for DC/V/D135/D117/D153/TM; false for H/D207. The D45/D63
// modes go through the NeedAboveRight path which also implies above.
func IntraNeedsAbove(mode common.PredictionMode) bool {
	return ExtendModes[mode]&NeedAbove != 0
}

// IntraNeedsAboveRight reports whether the predictor reads the
// above-right row extension. True only for D45 and D63.
func IntraNeedsAboveRight(mode common.PredictionMode) bool {
	return ExtendModes[mode]&NeedAboveRight != 0
}
