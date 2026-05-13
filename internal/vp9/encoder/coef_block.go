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

	// Fc carries the active per-frame coefficient probabilities.
	Fc *vp9dec.FrameCoefProbs
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
	dqShift := uint(0)
	if a.TxSize == common.Tx32x32 {
		dqShift = 1
	}

	// Find EOB position: one past the last non-zero coefficient.
	eob := 0
	for i := 0; i < maxEob; i++ {
		if a.Coeffs[a.Scan[i]] != 0 {
			eob = i + 1
		}
	}

	coefModel := &a.Fc[a.TxSize][a.PlaneType][a.IsInter]
	var tokenCache [1024]uint8
	ctx := 0
	bandIdx := 0

	c := 0
	for c < maxEob {
		band := int(bandTrans[bandIdx])
		bandIdx++
		probs := &coefModel[band][ctx]
		if c == eob {
			bw.Write(0, uint32(probs[0])) // EOB
			return nil
		}
		bw.Write(1, uint32(probs[0])) // not EOB

		// ZERO inner loop: mirror the decoder, which reads only the
		// ZERO bit (no fresh EOB) for each zero in a run.
		for a.Coeffs[a.Scan[c]] == 0 {
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
		}

		// Non-zero at c.
		bw.Write(1, uint32(probs[1])) // not ZERO

		raster := a.Scan[c]
		coeff := a.Coeffs[raster]
		dqv := dq[1]
		if c == 0 {
			dqv = dq[0]
		}
		absCoeff := coeff
		sign := 0
		if absCoeff < 0 {
			absCoeff = -absCoeff
			sign = 1
		}
		absVal := (int(absCoeff) << dqShift) / int(dqv)
		WriteTokenForCoeff(bw, probs[:], absVal, sign)

		switch {
		case absVal == 1:
			tokenCache[raster] = 1
		case absVal == 2:
			tokenCache[raster] = 2
		case absVal == 3 || absVal == 4:
			tokenCache[raster] = 3
		case absVal <= 6:
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
