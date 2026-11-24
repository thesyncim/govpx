package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// VP9 compressed header foundations. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodeframe.c — the boolean-coder-driven
// probability updates that follow the uncompressed header.

// TxSizeContexts is libvpx's TX_SIZE_CONTEXTS — 2 contexts per
// transform-size probability bucket.
const TxSizeContexts = 2

// TxProbs mirrors libvpx's struct tx_probs. The dimensions follow the
// fixed `TX_SIZES - {1, 2, 3}` triangular layout in vp9_entropymode.h.
type TxProbs struct {
	P32x32 [TxSizeContexts][common.TxSizes - 1]uint8
	P16x16 [TxSizeContexts][common.TxSizes - 2]uint8
	P8x8   [TxSizeContexts][common.TxSizes - 3]uint8
}

// ReadTxMode mirrors read_tx_mode. Two bits select Only4x4 / Allow8x8
// / Allow16x16 / Allow32x32; a third bit is read only when the first
// two say "Allow32x32", promoting to TxModeSelect.
func ReadTxMode(r *bitstream.Reader) common.TxMode {
	tm := common.TxMode(r.ReadLiteral(2))
	if tm == common.Allow32x32 {
		tm += common.TxMode(r.ReadBit())
	}
	return tm
}

// ReadTxModeProbs mirrors read_tx_mode_probs. Three nested loops over
// the (TxSizeContexts × {TxSizes-3, TxSizes-2, TxSizes-1}) probability
// slots; each slot is conditionally updated via vp9_diff_update_prob.
func ReadTxModeProbs(r *bitstream.Reader, tp *TxProbs) {
	for i := range TxSizeContexts {
		for j := range int(common.TxSizes - 3) {
			VpxDiffUpdateProb(r, &tp.P8x8[i][j])
		}
	}
	for i := range TxSizeContexts {
		for j := range int(common.TxSizes - 2) {
			VpxDiffUpdateProb(r, &tp.P16x16[i][j])
		}
	}
	for i := range TxSizeContexts {
		for j := range int(common.TxSizes - 1) {
			VpxDiffUpdateProb(r, &tp.P32x32[i][j])
		}
	}
}

// SkipContexts mirrors libvpx's SKIP_CONTEXTS (= 3).
const SkipContexts = common.SkipContexts

// ReadSkipProbs runs vp9_diff_update_prob across the 3 skip-context
// probability slots. Mirrors the corresponding fragment of
// read_compressed_header.
func ReadSkipProbs(r *bitstream.Reader, probs *[SkipContexts]uint8) {
	for k := range SkipContexts {
		VpxDiffUpdateProb(r, &probs[k])
	}
}
