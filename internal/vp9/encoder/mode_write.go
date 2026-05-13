package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// VP9 per-block mode-info writers. Ported from libvpx v1.16.0
// vp9/encoder/vp9_bitstream.c — write_intra_mode, write_inter_mode,
// write_selected_tx_size, write_skip, write_segment_id (already
// done), and the small tree-encoding tables intra/inter/partition/
// switchable_interp share.

// IntraModeEncodings mirrors intra_mode_encodings[INTRA_MODES]
// from libvpx. Indexed by PredictionMode in [DcPred, TmPred].
var IntraModeEncodings = [common.IntraModes]valLen{
	{0, 1},   // DcPred  "0"
	{6, 3},   // VPred   "110"
	{28, 5},  // HPred   "11100"
	{30, 5},  // D45Pred "11110"
	{58, 6},  // D135Pred"111010"
	{59, 6},  // D117Pred"111011"
	{126, 7}, // D153Pred"1111110"
	{127, 7}, // D207Pred"1111111"
	{62, 6},  // D63Pred "111110"
	{2, 2},   // TmPred  "10"
}

// InterModeEncodings mirrors inter_mode_encodings[INTER_MODES] —
// the sub-mode patterns walking InterModeTree to NEARESTMV/NEARMV/
// ZEROMV/NEWMV. The libvpx caller uses INTER_OFFSET(mode) to
// translate the absolute PredictionMode into the 0..3 sub-mode
// index before indexing here.
var InterModeEncodings = [common.InterModes]valLen{
	{2, 2}, // NEARESTMV  "10"
	{6, 3}, // NEARMV     "110"
	{0, 1}, // ZEROMV     "0"
	{7, 3}, // NEWMV      "111"
}

// PartitionEncodings mirrors partition_encodings[PARTITION_TYPES] —
// patterns walking PartitionTree to None / Horz / Vert / Split.
var PartitionEncodings = [common.PartitionTypes]valLen{
	{0, 1}, // PartitionNone  "0"
	{2, 2}, // PartitionHorz  "10"
	{6, 3}, // PartitionVert  "110"
	{7, 3}, // PartitionSplit "111"
}

// SwitchableInterpEncodings mirrors switchable_interp_encodings[3]
// — the three patterns walking SwitchableInterpTree to
// EIGHTTAP / EIGHTTAP_SMOOTH / EIGHTTAP_SHARP.
var SwitchableInterpEncodings = [3]valLen{
	{0, 1}, // EIGHTTAP        "0"
	{2, 2}, // EIGHTTAP_SMOOTH "10"
	{3, 2}, // EIGHTTAP_SHARP  "11"
}

// WriteIntraMode mirrors libvpx's write_intra_mode — walks
// IntraModeTree against the supplied 9-element probability row
// (size group's Y-mode probs for an inter-frame intra block, or
// kfYModeProb[above][left] for keyframes).
func WriteIntraMode(bw *bitstream.Writer, mode common.PredictionMode, probs []uint8) {
	enc := IntraModeEncodings[mode]
	writeTreeBits(bw, common.IntraModeTree[:], probs, enc.value, enc.length)
}

// WriteInterMode mirrors libvpx's write_inter_mode — takes the
// absolute PredictionMode (NearestMv..NewMv) and emits the
// matching sub-mode pattern against the 7-context probability row.
func WriteInterMode(bw *bitstream.Writer, mode common.PredictionMode, probs []uint8) {
	sub := int(mode) - int(common.NearestMv)
	enc := InterModeEncodings[sub]
	writeTreeBits(bw, common.InterModeTree[:], probs, enc.value, enc.length)
}

// WritePartition mirrors libvpx's write_partition_tree-and-encode
// pattern. When both (hasRows, hasCols) are true the full 4-way
// tree walks against `probs[0..2]`; the boundary forms emit a
// single bit (rows-only / cols-only) or no bit at all (neither).
func WritePartition(bw *bitstream.Writer, p common.PartitionType, probs []uint8, hasRows, hasCols bool) {
	switch {
	case hasRows && hasCols:
		enc := PartitionEncodings[p]
		writeTreeBits(bw, common.PartitionTree[:], probs, enc.value, enc.length)
	case !hasRows && hasCols:
		// 1-bit choice between Horz (0) and Split (1).
		if p == common.PartitionSplit {
			bw.Write(1, uint32(probs[1]))
		} else {
			bw.Write(0, uint32(probs[1]))
		}
	case hasRows && !hasCols:
		if p == common.PartitionSplit {
			bw.Write(1, uint32(probs[2]))
		} else {
			bw.Write(0, uint32(probs[2]))
		}
	}
}

// WriteSwitchableInterpFilter mirrors the libvpx write call that
// emits the per-block interp filter — assumes a non-Switchable
// frame-level filter has already been gated out.
func WriteSwitchableInterpFilter(bw *bitstream.Writer, f int, probs []uint8) {
	enc := SwitchableInterpEncodings[f]
	writeTreeBits(bw, common.SwitchableInterpTree[:], probs, enc.value, enc.length)
}

// WriteSelectedTxSize mirrors write_selected_tx_size — emits the
// cascade libvpx uses when TxModeSelect is in effect: a 1-bit
// "not Tx4x4", then if max >= Tx16x16 another "not Tx8x8", then
// if max >= Tx32x32 another "is Tx32x32" bit.
//
// `txProbs` is the 3-element row picked by (maxTxSize, ctx) from
// fc.TxProbs.
func WriteSelectedTxSize(bw *bitstream.Writer, txSize, maxTxSize common.TxSize, txProbs []uint8) {
	// First bit: 0 = Tx4x4, 1 = not Tx4x4.
	if txSize == common.Tx4x4 {
		bw.Write(0, uint32(txProbs[0]))
		return
	}
	bw.Write(1, uint32(txProbs[0]))
	if maxTxSize < common.Tx16x16 {
		return
	}
	// Second bit: 0 = stop at Tx8x8, 1 = not Tx8x8.
	if txSize == common.Tx8x8 {
		bw.Write(0, uint32(txProbs[1]))
		return
	}
	bw.Write(1, uint32(txProbs[1]))
	if maxTxSize < common.Tx32x32 {
		return
	}
	// Third bit: 0 = Tx16x16, 1 = Tx32x32.
	if txSize == common.Tx16x16 {
		bw.Write(0, uint32(txProbs[2]))
		return
	}
	bw.Write(1, uint32(txProbs[2]))
}
