package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 tx-size probability update writer. Ported from libvpx v1.16.0
// vp9/encoder/vp9_bitstream.c — encode_txfm_probs (the probabilities
// half, after the tx_mode literal). When tx_mode == TX_MODE_SELECT
// the writer emits per-context updates for the 8x8 / 16x16 / 32x32
// max-tx sub-tables. Each sub-table fans the per-(plane, ctx)
// tx-size counts into the 1- / 2- / 3-pair branch counts the
// matching tree-write expects, then runs the counts-driven
// cond-update per branch.
//
// The frame-level tx_mode literal already lands via writeTxMode in
// compressed_writer.go; this helper covers the prob-update half
// that's only emitted when tx_mode == TxModeSelect.

// TxModeCounts mirrors libvpx's FRAME_COUNTS.tx.p8x8/p16x16/p32x32.
// Per TxSizeContexts entry, the count is how many times each
// candidate tx_size was selected at that max-tx level.
type TxModeCounts struct {
	// P8x8 counts of (Tx4x4, Tx8x8) per context.
	P8x8 [vp9dec.TxSizeContexts][2]uint32
	// P16x16 counts of (Tx4x4, Tx8x8, Tx16x16) per context.
	P16x16 [vp9dec.TxSizeContexts][3]uint32
	// P32x32 counts of (Tx4x4, Tx8x8, Tx16x16, Tx32x32) per context.
	P32x32 [vp9dec.TxSizeContexts][4]uint32
}

// WriteTxModeProbsFromCounts mirrors the encode_txfm_probs
// per-context update block (gated on cm->tx_mode == TX_MODE_SELECT
// by the caller). Walks the 8x8 / 16x16 / 32x32 sub-tables; per
// context it derives the per-branch (left, right) count pairs the
// matching tree expects via the tx_counts_to_branch_counts_* fans
// (inlined here as direct count arithmetic for clarity), then
// invokes CondProbDiffUpdateFromCounts per pair.
func WriteTxModeProbsFromCounts(bw *bitstream.Writer,
	probs *vp9dec.TxProbs, counts *TxModeCounts,
) {
	// p8x8: one branch per context — Tx4x4 vs Tx8x8.
	for i := range vp9dec.TxSizeContexts {
		ct := [2]uint32{counts.P8x8[i][0], counts.P8x8[i][1]}
		CondProbDiffUpdateFromCounts(bw, &probs.P8x8[i][0], ct)
	}
	// p16x16: two branches per context — (Tx4x4, Tx8x8+Tx16x16) and
	// (Tx8x8, Tx16x16).
	for i := range vp9dec.TxSizeContexts {
		c := counts.P16x16[i]
		CondProbDiffUpdateFromCounts(bw, &probs.P16x16[i][0],
			[2]uint32{c[0], c[1] + c[2]})
		CondProbDiffUpdateFromCounts(bw, &probs.P16x16[i][1],
			[2]uint32{c[1], c[2]})
	}
	// p32x32: three branches per context.
	for i := range vp9dec.TxSizeContexts {
		c := counts.P32x32[i]
		CondProbDiffUpdateFromCounts(bw, &probs.P32x32[i][0],
			[2]uint32{c[0], c[1] + c[2] + c[3]})
		CondProbDiffUpdateFromCounts(bw, &probs.P32x32[i][1],
			[2]uint32{c[1], c[2] + c[3]})
		CondProbDiffUpdateFromCounts(bw, &probs.P32x32[i][2],
			[2]uint32{c[2], c[3]})
	}
}
