package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// CoeffTokenRateCost returns the token-tree cost for a non-zero coefficient
// magnitude after the caller has already charged the not-EOB and not-ZERO
// outer bits.
func CoeffTokenRateCost(probs []uint8, absVal, sign int) int {
	if absVal <= 0 || len(probs) < UnconstrainedNodes {
		return 0
	}
	rate := 0
	token, extra := TokenForAbsCoeff(absVal)
	if token == OneToken {
		rate += VP9CostBit(probs[2], 0)
		rate += VP9CostBit(128, sign)
		return rate
	}
	rate += VP9CostBit(probs[2], 1)
	enc := CoefEncodings[token]
	pareto := tables.Pareto8Full[probs[2]-1]
	rate += TreedCost(CoefConTree[:], pareto[:],
		int(enc.Value), int(enc.Len)-UnconstrainedNodes)
	if token >= Category1Tok {
		eb := VP9ExtraBits[token]
		for i := eb.Len - 1; i >= 0; i-- {
			bit := (extra >> uint(i)) & 1
			rate += VP9CostBit(eb.Prob[eb.Len-1-i], bit)
		}
	}
	rate += VP9CostBit(128, sign)
	return rate
}

// CoeffTokenExtraCost returns the entropy token plus the extra-bit and sign
// cost for a coefficient magnitude.
func CoeffTokenExtraCost(absVal, sign int) (token int, cost int) {
	token, extra := TokenForAbsCoeff(absVal)
	if token >= Category1Tok {
		eb := VP9ExtraBits[token]
		for i := eb.Len - 1; i >= 0; i-- {
			bit := (extra >> uint(i)) & 1
			cost += VP9CostBit(eb.Prob[eb.Len-1-i], bit)
		}
	}
	if token != ZeroToken {
		cost += VP9CostBit(128, sign)
	}
	return token, cost
}

// CoeffTreeTokenCost returns the cost of one coefficient token for a compact
// three-node probability model.
func CoeffTreeTokenCost(model []uint8, skipEOB bool, token int) int {
	if len(model) < UnconstrainedNodes || token < 0 ||
		token >= EntropyTokens || model[2] == 0 {
		return 0
	}
	var full [EntropyNodes]uint8
	full[0] = model[0]
	full[1] = model[1]
	full[2] = model[2]
	tail := tables.Pareto8Full[model[2]-1]
	for i := range tail {
		full[3+i] = tail[i]
	}
	var costs [EntropyTokens]int
	if skipEOB {
		VP9CostTokensSkip(costs[:], full[:], CoefTree[:])
	} else {
		VP9CostTokens(costs[:], full[:], CoefTree[:])
	}
	return costs[token]
}

// CoeffTokenAbsValInt maps a dequantized coefficient magnitude back to the
// entropy-token absolute value used by libvpx's coefficient cost model.
func CoeffTokenAbsValInt(absCoeff, dqv int, tx32 bool) int {
	num := absCoeff
	den := dqv
	if den <= 0 {
		return 0
	}
	if tx32 {
		return (num*2 + den - 1) / den
	}
	return num / den
}

// CoeffMagnitudeAndSign returns the token magnitude and sign for a scanned
// coefficient position, preferring qcoeffs when the caller has them.
func CoeffMagnitudeAndSign(qcoeffs []int16, raster int, dqcoeff int16,
	dqv int16, tx32 bool,
) (absVal int, sign int) {
	if qcoeffs != nil && raster >= 0 && raster < len(qcoeffs) {
		q := int(qcoeffs[raster])
		if q < 0 {
			return -q, 1
		}
		return q, 0
	}
	coeff := int(dqcoeff)
	if coeff < 0 {
		coeff = -coeff
		sign = 1
	}
	return CoeffTokenAbsValInt(coeff, int(dqv), tx32), sign
}

// CoeffBlockEOB returns the last non-zero coefficient position plus one in
// scan order.
func CoeffBlockEOB(scan []int16, maxEob int, coeffs, qcoeffs []int16) int {
	eob := 0
	for i := range maxEob {
		if CoeffBlockHasCoeff(scan, i, coeffs, qcoeffs) {
			eob = i + 1
		}
	}
	return eob
}

// CoeffBlockHasCoeff reports whether scan[pos] points at a non-zero
// coefficient, preferring qcoeffs when present.
func CoeffBlockHasCoeff(scan []int16, pos int, coeffs, qcoeffs []int16) bool {
	if pos < 0 || pos >= len(scan) {
		return false
	}
	raster := int(scan[pos])
	if qcoeffs != nil && raster >= 0 && raster < len(qcoeffs) {
		return qcoeffs[raster] != 0
	}
	return raster >= 0 && raster < len(coeffs) && coeffs[raster] != 0
}

// TxSizeRateCost returns the transform-size signaling cost for txSize under
// the frame's maximum transform size.
func TxSizeRateCost(probs []uint8, txSize, maxTxSize common.TxSize) int {
	if len(probs) == 0 || txSize >= common.TxSizes {
		return 0
	}
	rate := 0
	if txSize == common.Tx4x4 {
		return VP9CostBit(probs[0], 0)
	}
	rate += VP9CostBit(probs[0], 1)
	if maxTxSize < common.Tx16x16 || len(probs) < 2 {
		return rate
	}
	if txSize == common.Tx8x8 {
		return rate + VP9CostBit(probs[1], 0)
	}
	rate += VP9CostBit(probs[1], 1)
	if maxTxSize < common.Tx32x32 || len(probs) < 3 {
		return rate
	}
	if txSize == common.Tx16x16 {
		return rate + VP9CostBit(probs[2], 0)
	}
	return rate + VP9CostBit(probs[2], 1)
}
