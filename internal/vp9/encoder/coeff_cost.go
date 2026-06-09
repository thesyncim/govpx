package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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

// CoeffBlockRateCostInput groups the state needed by libvpx's cost_coeffs
// path. TokenCache is caller-owned scratch and is cleared up to the active EOB
// range on every call.
type CoeffBlockRateCostInput struct {
	TxSize    common.TxSize
	CoefModel *[vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8
	// CostTable, when non-nil, is the per-frame precomputed token-cost
	// table for CoefModel (BuildCoeffTokenCostTable). The hot loops do O(1)
	// lookups against it instead of re-expanding CoefModel per coefficient.
	// It MUST have been built from the same CoefModel; when nil, the cost
	// path expands CoefModel directly (numerically identical, just slower).
	CostTable  *CoeffTokenCostTable
	ScanOrder  common.ScanOrder
	Dequant    [2]int16
	Coeffs     []int16
	QCoeffs    []int16
	InitCtx    int
	Fast       bool
	TokenCache *[1024]byte
}

// CoeffBlockRateCost ports libvpx cost_coeffs for VP9 encoder RD scoring.
func CoeffBlockRateCost(in CoeffBlockRateCostInput) int {
	maxEob := vp9dec.MaxEobForTxSize(in.TxSize)
	if in.TxSize >= common.TxSizes || in.CoefModel == nil ||
		in.Dequant[0] == 0 || in.Dequant[1] == 0 ||
		len(in.Coeffs) < maxEob || in.InitCtx < 0 || in.InitCtx > 2 ||
		in.TokenCache == nil {
		return 0
	}
	if in.QCoeffs != nil && len(in.QCoeffs) < maxEob {
		in.QCoeffs = nil
	}
	scan := in.ScanOrder.Scan
	neighbors := in.ScanOrder.Neighbors
	if len(scan) < maxEob || len(neighbors) < common.MaxNeighbors*maxEob {
		return 0
	}
	for i := range in.TokenCache[:maxEob] {
		in.TokenCache[i] = 0
	}
	// The token-tree costs depend only on CoefModel and are stable across
	// every coefficient in the frame, so libvpx precomputes them once
	// (fill_token_costs) and the hot loops do table lookups. Reuse the
	// caller's per-frame table when supplied; otherwise build a one-shot
	// table for this block — both yield byte-identical costs.
	costTable := in.CostTable
	if costTable == nil {
		costTable = BuildCoeffTokenCostTable(in.CoefModel)
	}
	if in.Fast {
		return coeffBlockRateCostFastQ(in, costTable, scan, maxEob)
	}
	eob := CoeffBlockEOB(scan, maxEob, in.Coeffs, in.QCoeffs)
	return coeffBlockRateCostSlowQ(in, costTable, scan, neighbors, maxEob, eob)
}

var coeffCostBandCounts = [common.TxSizes][8]int{
	{1, 2, 3, 4, 3, 16 - 13, 0},
	{1, 2, 3, 4, 11, 64 - 21, 0},
	{1, 2, 3, 4, 11, 256 - 21, 0},
	{1, 2, 3, 4, 11, 1024 - 21, 0},
}

func coeffBlockRateCostSlowQ(in CoeffBlockRateCostInput,
	costTable *CoeffTokenCostTable, scan, neighbors []int16, maxEob int, eob int,
) int {
	if eob <= 0 {
		return costTable.Lookup(0, in.InitCtx, EobToken, false)
	}
	if eob > maxEob {
		eob = maxEob
	}

	dcAbs, dcSign := CoeffMagnitudeAndSign(in.QCoeffs, 0, in.Coeffs[0],
		in.Dequant[0], in.TxSize == common.Tx32x32)
	prevToken, extraCost := CoeffTokenExtraCost(dcAbs, dcSign)
	rate := extraCost + costTable.Lookup(0, in.InitCtx, prevToken, false)
	in.TokenCache[0] = PtEnergyClass[prevToken]

	band := 1
	bandLeft := coeffCostBandCounts[in.TxSize][band]
	for c := 1; c < eob; c++ {
		if band >= vp9dec.CoefBands {
			return rate
		}
		raster := int(scan[c])
		absVal, sign := CoeffMagnitudeAndSign(in.QCoeffs, raster,
			in.Coeffs[raster], in.Dequant[1], in.TxSize == common.Tx32x32)
		token, extra := CoeffTokenExtraCost(absVal, sign)
		pt := vp9dec.GetCoefContext(neighbors, in.TokenCache, c)
		rate += extra + costTable.Lookup(band, pt, token, prevToken == ZeroToken)
		in.TokenCache[raster] = PtEnergyClass[token]
		if bandLeft > 0 {
			bandLeft--
			if bandLeft == 0 {
				band++
				if band < len(coeffCostBandCounts[in.TxSize]) {
					bandLeft = coeffCostBandCounts[in.TxSize][band]
				}
			}
		}
		prevToken = token
	}
	if bandLeft != 0 && band < vp9dec.CoefBands {
		pt := vp9dec.GetCoefContext(neighbors, in.TokenCache, eob)
		rate += costTable.Lookup(band, pt, EobToken, false)
	}
	return rate
}

func coeffBlockRateCostFastQ(in CoeffBlockRateCostInput,
	costTable *CoeffTokenCostTable, scan []int16, maxEob int,
) int {
	eob := CoeffBlockEOB(scan, maxEob, in.Coeffs, in.QCoeffs)
	if eob == 0 {
		return costTable.Lookup(0, in.InitCtx, EobToken, false)
	}

	rate := 0
	dcAbs, dcSign := CoeffMagnitudeAndSign(in.QCoeffs, 0, in.Coeffs[0],
		in.Dequant[0], in.TxSize == common.Tx32x32)
	prevToken, extraCost := CoeffTokenExtraCost(dcAbs, dcSign)
	rate += extraCost
	rate += costTable.Lookup(0, in.InitCtx, prevToken, false)

	bandIdx := 1
	bandLeft := coeffCostBandCounts[in.TxSize][bandIdx]
	for c := 1; c < eob; c++ {
		raster := int(scan[c])
		absVal, sign := CoeffMagnitudeAndSign(in.QCoeffs, raster,
			in.Coeffs[raster], in.Dequant[1], in.TxSize == common.Tx32x32)
		token, extra := CoeffTokenExtraCost(absVal, sign)
		ctx := 0
		skipEOB := false
		if prevToken == ZeroToken {
			ctx = 1
			skipEOB = true
		}
		rate += extra
		rate += costTable.Lookup(bandIdx, ctx, token, skipEOB)
		prevToken = token
		bandLeft--
		if bandLeft == 0 {
			bandIdx++
			if bandIdx >= len(coeffCostBandCounts[in.TxSize]) {
				break
			}
			bandLeft = coeffCostBandCounts[in.TxSize][bandIdx]
		}
	}
	if bandLeft != 0 {
		ctx := 0
		if prevToken == ZeroToken {
			ctx = 1
		}
		rate += costTable.Lookup(bandIdx, ctx, EobToken, false)
	}
	return rate
}
