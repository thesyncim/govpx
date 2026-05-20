package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// VP9 block-level mode decoders. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodemv.c — the small leaf-functions that compose
// on top of Reader.ReadTree and the canonical token trees.

// ReadIntraMode mirrors read_intra_mode. Picks one of 10 intra
// prediction modes from the supplied probability row.
func ReadIntraMode(r *bitstream.Reader, probs []uint8) common.PredictionMode {
	return common.PredictionMode(r.ReadTree(common.IntraModeTree[:], probs))
}

// ReadInterMode mirrors read_inter_mode. Returns the absolute
// PredictionMode (NEARESTMV..NEWMV), having added the NEARESTMV
// offset that the InterModeTree leaves omit.
func ReadInterMode(r *bitstream.Reader, interModeProbs [common.InterModes - 1]uint8) common.PredictionMode {
	off := r.ReadTree(common.InterModeTree[:], interModeProbs[:])
	return common.NearestMv + common.PredictionMode(off)
}

// ReadSegmentId mirrors read_segment_id. The supplied probability
// triplet drives the SegmentTree decode.
func ReadSegmentId(r *bitstream.Reader, treeProbs [SegTreeProbs]uint8) int {
	return r.ReadTree(common.SegmentTree[:], treeProbs[:])
}

// ReadSkip mirrors read_skip. The single bit gates whether the block
// has any non-zero residual.
func ReadSkip(r *bitstream.Reader, skipProb uint8) int {
	return int(r.Read(uint32(skipProb)))
}

// ReadSelectedTxSize mirrors read_selected_tx_size — the multi-bit
// transform-size selector used when TxMode = TxModeSelect and the
// block is at least 8x8. `txProbs` is the 3-element probability row
// selected by (max_tx_size, ctx).
func ReadSelectedTxSize(r *bitstream.Reader, maxTxSize common.TxSize, txProbs []uint8) common.TxSize {
	tx := common.TxSize(r.Read(uint32(txProbs[0])))
	if tx != common.Tx4x4 && maxTxSize >= common.Tx16x16 {
		tx += common.TxSize(r.Read(uint32(txProbs[1])))
		if tx != common.Tx8x8 && maxTxSize >= common.Tx32x32 {
			tx += common.TxSize(r.Read(uint32(txProbs[2])))
		}
	}
	return tx
}
