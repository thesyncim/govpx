package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

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
	mi.SegIDPredicted = uint8(segID)

	mi.Skip = uint8(ReadSkipWithSeg(args.Reader, args.Seg, segID, args.Fc,
		args.Above, args.Left))

	mi.TxSize = ReadTxSize(args.Reader, args.Fc, args.TxMode, mi.SbType,
		args.Above, args.Left, true)

	uv := ReadIntraBlockModeInfo(args.Reader, mi, args.Above, args.Left)
	return IntraFrameModeInfoOut{UvMode: uv}
}
