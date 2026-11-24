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
