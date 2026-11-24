package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// Coefficient probability updates from the VP9 compressed header.
// Ported from libvpx v1.16.0 vp9/decoder/vp9_decodeframe.c
// (read_coef_probs / read_coef_probs_common) plus the table sizing
// constants in vp9/common/vp9_entropy.h.

const (
	// CoefPlaneTypes — VP9 keeps separate coefficient probs for luma
	// and chroma (libvpx's PLANE_TYPES = 2).
	CoefPlaneTypes = 2
	// CoefRefTypes — intra vs inter reference frame (REF_TYPES = 2).
	CoefRefTypes = 2
	// CoefBands — six bands per (plane, ref, tx_size) entry.
	CoefBands = 6
	// CoefContexts — full per-band context count outside band 0.
	CoefContexts = 6
	// CoefBand0Contexts — band 0 uses a reduced context space.
	CoefBand0Contexts = 3
	// UnconstrainedNodes — three of the four token-tree nodes are
	// unconstrained and carry their own probabilities; the fourth is
	// implied from the others.
	UnconstrainedNodes = 3
)

// BandCoefContexts mirrors libvpx's BAND_COEFF_CONTEXTS(band) macro —
// band 0 uses 3 contexts, every other band uses 6.
func BandCoefContexts(band int) int {
	if band == 0 {
		return CoefBand0Contexts
	}
	return CoefContexts
}

// CoefProbsModel matches libvpx's vp9_coeff_probs_model layout. The
// per-tx-size table is a 4-D array indexed as
//
//	[PLANE_TYPES][REF_TYPES][COEF_BANDS][COEF_CONTEXTS][UNCONSTRAINED_NODES]
//
// Band 0 only actually uses 3 of the COEF_CONTEXTS slots; the tail
// slots are unused but the storage is square so the surrounding code
// can index uniformly.
type CoefProbsModel [CoefPlaneTypes][CoefRefTypes][CoefBands][CoefContexts][UnconstrainedNodes]uint8

// FrameCoefProbs aggregates the per-tx-size coefficient probability
// tables the compressed header may update — one model per TxSize
// (4x4 / 8x8 / 16x16 / 32x32).
type FrameCoefProbs [common.TxSizes]CoefProbsModel

// readCoefProbsCommon mirrors read_coef_probs_common — a single
// leading bit gates the full nested loop over (planes × refs × bands ×
// per-band contexts × unconstrained nodes).
func readCoefProbsCommon(r *bitstream.Reader, model *CoefProbsModel) {
	if r.ReadBit() == 0 {
		return
	}
	for i := range CoefPlaneTypes {
		for j := range CoefRefTypes {
			for k := range CoefBands {
				for l := range BandCoefContexts(k) {
					for m := range UnconstrainedNodes {
						VpxDiffUpdateProb(r, &model[i][j][k][l][m])
					}
				}
			}
		}
	}
}

// ReadCoefProbs mirrors read_coef_probs. It walks every TxSize up to
// the cap that tx_mode_to_biggest_tx_size selects from the frame
// header's TxMode and invokes the per-model update routine on each.
func ReadCoefProbs(r *bitstream.Reader, fc *FrameCoefProbs, txMode common.TxMode) {
	max := common.TxModeToBiggestTxSize[txMode]
	for tx := common.TxSize(0); tx <= max; tx++ {
		readCoefProbsCommon(r, &fc[tx])
	}
}
