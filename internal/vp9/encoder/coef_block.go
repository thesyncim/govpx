package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// WriteCoefBlockArgs bundles the inputs WriteCoefBlock consults.
//
// Layout mirrors libvpx's tokenize_b context: the coefficient
// stream is walked in scan order; band + ctx pick a probability
// row from `fc`; the per-position token cache feeds the next
// context.
type WriteCoefBlockArgs struct {
	TxSize    common.TxSize
	PlaneType int
	IsInter   int
	DequantDC int16
	DequantAC int16
	Scan      []int16
	Neighbors []int16

	// Coeffs are the dequantized residual coefficients in raster
	// order (indexed by scan[c]).
	Coeffs []int16

	// QCoeffs are the signed quantized coefficients in raster order.
	// When supplied, coefficient tokens are derived from qcoeff exactly
	// like libvpx's tokenize_b; Coeffs remain the reconstructed dqcoeff
	// plane consumed by inverse transforms and legacy callers.
	QCoeffs []int16

	// Fc carries the active per-frame coefficient probabilities.
	Fc *vp9dec.FrameCoefProbs

	// CoefBranchStats, when non-nil, receives the branch counts for
	// the coefficient token tree slots touched by this block. These
	// are the counts consumed by WriteCoefProbsFromCounts.
	CoefBranchStats *FrameCoefBranchStats

	// InitCtx is the band-0 coefficient context derived from the
	// above/left entropy-context cache via GetEntropyContext. Mirrors
	// libvpx's get_entropy_context result (0..2). Zero is correct
	// only when there's no neighbor residue (top-left of the SB or
	// directly after a skip block).
	InitCtx int

	// EOB, when non-nil, receives the computed end-of-block value so
	// callers that need residue presence can avoid rescanning coeffs.
	EOB *int
}

// WriteCoefBlock emits the wire fragment for one transform block's
// coefficient stream. Mirrors the t >= TWO_TOKEN branch of
// libvpx's tokenize_b inverted into the encoder side: walks
// `Coeffs` in scan order, emitting non-EOB / non-ZERO / ONE-or-tree
// + sign for each non-zero entry, ZERO inside runs, and EOB once
// the trailing zeros begin. Returns the boolean-coded byte count
// written.
func WriteCoefBlock(bw *bitstream.Writer, a WriteCoefBlockArgs) error {
	maxEob := vp9dec.MaxEobForTxSize(a.TxSize)
	bandTrans := vp9dec.BandTranslateForTxSize(a.TxSize)
	dq := [2]int16{a.DequantDC, a.DequantAC}
	qcoeffs := a.QCoeffs
	if len(qcoeffs) < maxEob {
		qcoeffs = nil
	}

	// Find EOB position: one past the last non-zero coefficient.
	eob := CoeffBlockEOB(a.Scan, maxEob, a.Coeffs, qcoeffs)
	if a.EOB != nil {
		*a.EOB = eob
	}

	coefModel := &a.Fc[a.TxSize][a.PlaneType][a.IsInter]
	var tokenCache [1024]uint8
	ctx := a.InitCtx
	bandIdx := 0

	c := 0
	for c < maxEob {
		band := int(bandTrans[bandIdx])
		bandIdx++
		probs := &coefModel[band][ctx]
		branchStats := coefBranchStatsSlot(a.CoefBranchStats, a.TxSize,
			a.PlaneType, a.IsInter, band, ctx)
		if c == eob {
			recordCoefBranch(branchStats, 0, 0)
			bw.Write(0, uint32(probs[0])) // EOB
			return nil
		}
		recordCoefBranch(branchStats, 0, 1)
		bw.Write(1, uint32(probs[0])) // not EOB

		// ZERO inner loop: mirror the decoder, which reads only the
		// ZERO bit (no fresh EOB) for each zero in a run.
		for !CoeffBlockHasCoeff(a.Scan, c, a.Coeffs, qcoeffs) {
			recordCoefBranch(branchStats, 1, 0)
			bw.Write(0, uint32(probs[1])) // ZERO
			tokenCache[a.Scan[c]] = 0
			c++
			if c >= maxEob {
				return nil
			}
			ctx = vp9dec.GetCoefContext(a.Neighbors, &tokenCache, c)
			band = int(bandTrans[bandIdx])
			bandIdx++
			probs = &coefModel[band][ctx]
			branchStats = coefBranchStatsSlot(a.CoefBranchStats, a.TxSize,
				a.PlaneType, a.IsInter, band, ctx)
		}

		// Non-zero at c.
		recordCoefBranch(branchStats, 1, 1)
		bw.Write(1, uint32(probs[1])) // not ZERO

		raster := a.Scan[c]
		dqv := dq[1]
		if c == 0 {
			dqv = dq[0]
		}
		absVal, sign := CoeffMagnitudeAndSign(qcoeffs, int(raster),
			a.Coeffs[raster], dqv, a.TxSize == common.Tx32x32)
		writeTokenForCoeff(bw, probs[:], absVal, sign, branchStats)

		switch {
		case absVal == 1:
			tokenCache[raster] = 1
		case absVal == 2:
			tokenCache[raster] = 2
		case absVal == 3 || absVal == 4:
			tokenCache[raster] = 3
		case absVal <= 10:
			tokenCache[raster] = 4
		default:
			tokenCache[raster] = 5
		}
		c++
		if c < maxEob {
			ctx = vp9dec.GetCoefContext(a.Neighbors, &tokenCache, c)
		}
	}
	return nil
}

func coefBranchStatsSlot(
	stats *FrameCoefBranchStats, tx common.TxSize, planeType, isInter, band, ctx int,
) *[EntropyNodes][2]uint32 {
	if stats == nil {
		return nil
	}
	return &stats[tx][planeType][isInter][band][ctx]
}
