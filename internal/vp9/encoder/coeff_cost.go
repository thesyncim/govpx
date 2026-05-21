package encoder

import "github.com/thesyncim/govpx/internal/vp9/tables"

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
