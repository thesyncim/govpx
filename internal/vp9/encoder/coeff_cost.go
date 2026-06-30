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
	rate += int(coefConTreeTokenCostTable[probs[2]][token])
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
	if uint(absVal) < coeffTokenExtraCostTableSize {
		entry := coeffTokenExtraCostTable[absVal]
		return int(entry.token), int(entry.cost)
	}
	return coeffTokenExtraCostSlow(absVal, sign)
}

const coeffTokenExtraCostTableSize = 4096

type coeffTokenExtraCostEntry struct {
	cost  uint16
	token uint8
}

var coeffTokenExtraCostTable = func() [coeffTokenExtraCostTableSize]coeffTokenExtraCostEntry {
	var table [coeffTokenExtraCostTableSize]coeffTokenExtraCostEntry
	for absVal := range table {
		token, cost := coeffTokenExtraCostSlow(absVal, 0)
		table[absVal] = coeffTokenExtraCostEntry{token: uint8(token), cost: uint16(cost)}
	}
	return table
}()

func coeffTokenExtraCostSlow(absVal, sign int) (token int, cost int) {
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

const coeffQCoeffTokenExtraCostTableSize = 1 << 16

var coeffQCoeffTokenExtraCostTable = func() [coeffQCoeffTokenExtraCostTableSize]coeffTokenExtraCostEntry {
	var table [coeffQCoeffTokenExtraCostTableSize]coeffTokenExtraCostEntry
	for bits := range table {
		q := int(int16(uint16(bits)))
		sign := 0
		if q < 0 {
			q = -q
			sign = 1
		}
		token, cost := coeffTokenExtraCostSlow(q, sign)
		table[bits] = coeffTokenExtraCostEntry{token: uint8(token), cost: uint16(cost)}
	}
	return table
}()

// CoeffTreeTokenCost returns the cost of one coefficient token for a compact
// three-node probability model.
func CoeffTreeTokenCost(model []uint8, skipEOB bool, token int) int {
	if len(model) < UnconstrainedNodes || token < 0 ||
		token >= EntropyTokens || model[2] == 0 {
		return 0
	}
	if token == EobToken {
		return VP9CostZero(model[0])
	}
	cost := 0
	if !skipEOB {
		cost += VP9CostOne(model[0])
	}
	if token == ZeroToken {
		return cost + VP9CostZero(model[1])
	}
	cost += VP9CostOne(model[1])
	if token == OneToken {
		return cost + VP9CostZero(model[2])
	}
	cost += VP9CostOne(model[2])
	return cost + int(coefConTreeTokenCostTable[model[2]][token])
}

type CoeffTreeTokenCostTable [vp9dec.CoefBands][vp9dec.CoefContexts][2][EntropyTokens]uint16

type FrameCoeffTokenCostTable [common.TxSizes][vp9dec.CoefPlaneTypes][vp9dec.CoefRefTypes]CoeffTreeTokenCostTable

func FillCoeffTreeTokenCostTable(
	model *[vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8,
	out *CoeffTreeTokenCostTable,
) bool {
	if model == nil || out == nil {
		return false
	}
	for band := range vp9dec.CoefBands {
		for ctx := range vp9dec.CoefContexts {
			for token := range EntropyTokens {
				(*out)[band][ctx][0][token] = uint16(CoeffTreeTokenCost(
					(*model)[band][ctx][:], false, token))
				(*out)[band][ctx][1][token] = uint16(CoeffTreeTokenCost(
					(*model)[band][ctx][:], true, token))
			}
		}
	}
	return true
}

func FillFrameCoeffTokenCostTable(fc *vp9dec.FrameCoefProbs,
	out *FrameCoeffTokenCostTable,
) bool {
	if fc == nil || out == nil {
		return false
	}
	for tx := range common.TxSizes {
		for plane := range vp9dec.CoefPlaneTypes {
			for ref := range vp9dec.CoefRefTypes {
				FillCoeffTreeTokenCostTable(&(*fc)[tx][plane][ref],
					&(*out)[tx][plane][ref])
			}
		}
	}
	return true
}

func (t *CoeffTreeTokenCostTable) TokenCost(band, ctx int,
	skipEOB bool, token int,
) int {
	if t == nil || band < 0 || band >= vp9dec.CoefBands ||
		ctx < 0 || ctx >= vp9dec.CoefContexts ||
		token < 0 || token >= EntropyTokens {
		return 0
	}
	skipIdx := 0
	if skipEOB {
		skipIdx = 1
	}
	return int((*t)[band][ctx][skipIdx][token])
}

var coefConTreeTokenCostTable = func() [256][EntropyTokens]uint16 {
	var table [256][EntropyTokens]uint16
	for pivot := 1; pivot < 256; pivot++ {
		probs := &tables.Pareto8Full[pivot-1]
		for token := range EntropyTokens {
			table[pivot][token] = uint16(coefConTreeTokenCost(probs, token))
		}
	}
	return table
}()

func coefConTreeTokenCost(probs *[8]uint8, token int) int {
	switch token {
	case TwoToken:
		return VP9CostZero(probs[0]) + VP9CostZero(probs[1])
	case ThreeToken:
		return VP9CostZero(probs[0]) + VP9CostOne(probs[1]) +
			VP9CostZero(probs[2])
	case FourToken:
		return VP9CostZero(probs[0]) + VP9CostOne(probs[1]) +
			VP9CostOne(probs[2])
	case Category1Tok:
		return VP9CostOne(probs[0]) + VP9CostZero(probs[3]) +
			VP9CostZero(probs[4])
	case Category2Tok:
		return VP9CostOne(probs[0]) + VP9CostZero(probs[3]) +
			VP9CostOne(probs[4])
	case Category3Tok:
		return VP9CostOne(probs[0]) + VP9CostOne(probs[3]) +
			VP9CostZero(probs[5]) + VP9CostZero(probs[6])
	case Category4Tok:
		return VP9CostOne(probs[0]) + VP9CostOne(probs[3]) +
			VP9CostZero(probs[5]) + VP9CostOne(probs[6])
	case Category5Tok:
		return VP9CostOne(probs[0]) + VP9CostOne(probs[3]) +
			VP9CostOne(probs[5]) + VP9CostZero(probs[7])
	case Category6Tok:
		return VP9CostOne(probs[0]) + VP9CostOne(probs[3]) +
			VP9CostOne(probs[5]) + VP9CostOne(probs[7])
	default:
		return 0
	}
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
	if maxEob > len(scan) {
		maxEob = len(scan)
	}
	for i := maxEob - 1; i >= 0; i-- {
		if coeffBlockHasCoeffAtRaster(int(scan[i]), coeffs, qcoeffs) {
			return i + 1
		}
	}
	return 0
}

func coeffBlockEOBCompleteQCoeff(scan []int16, maxEob int, qcoeffs []int16) int {
	if maxEob > len(scan) {
		maxEob = len(scan)
	}
	for i := maxEob - 1; i >= 0; i-- {
		raster := int(scan[i])
		if raster >= 0 && raster < len(qcoeffs) && qcoeffs[raster] != 0 {
			return i + 1
		}
	}
	return 0
}

func coeffBlockEOBEncode(scan []int16, maxEob int, coeffs, qcoeffs []int16) int {
	if maxEob > len(scan) {
		maxEob = len(scan)
	}
	if qcoeffs != nil && len(qcoeffs) >= maxEob {
		return coeffBlockEOBCompleteQCoeff(scan, maxEob, qcoeffs)
	}
	return CoeffBlockEOB(scan, maxEob, coeffs, qcoeffs)
}

// CoeffBlockHasCoeff reports whether scan[pos] points at a non-zero
// coefficient, preferring qcoeffs when present.
func CoeffBlockHasCoeff(scan []int16, pos int, coeffs, qcoeffs []int16) bool {
	if pos < 0 || pos >= len(scan) {
		return false
	}
	return coeffBlockHasCoeffAtRaster(int(scan[pos]), coeffs, qcoeffs)
}

func coeffBlockHasCoeffAtRaster(raster int, coeffs, qcoeffs []int16) bool {
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
	TxSize     common.TxSize
	CoefModel  *[vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8
	ScanOrder  common.ScanOrder
	Dequant    [2]int16
	Coeffs     []int16
	QCoeffs    []int16
	InitCtx    int
	Fast       bool
	TokenCache *[1024]byte
	CostTable  *CoeffTreeTokenCostTable
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
	if len(scan) < maxEob {
		return 0
	}
	if in.Fast {
		return coeffBlockRateCostFastQ(in, scan, maxEob)
	}
	neighbors := in.ScanOrder.Neighbors
	if len(neighbors) < common.MaxNeighbors*maxEob {
		return 0
	}
	for i := range in.TokenCache[:maxEob] {
		in.TokenCache[i] = 0
	}
	eob := coeffBlockEOBEncode(scan, maxEob, in.Coeffs, in.QCoeffs)
	return coeffBlockRateCostSlowQ(in, scan, neighbors, maxEob, eob)
}

var coeffCostBandCounts = [common.TxSizes][8]int{
	{1, 2, 3, 4, 3, 16 - 13, 0},
	{1, 2, 3, 4, 11, 64 - 21, 0},
	{1, 2, 3, 4, 11, 256 - 21, 0},
	{1, 2, 3, 4, 11, 1024 - 21, 0},
}

func coeffBlockRateCostSlowQ(in CoeffBlockRateCostInput, scan, neighbors []int16,
	maxEob int, eob int,
) int {
	if eob <= 0 {
		return coeffBlockTreeTokenCost(in.CostTable, in.CoefModel, 0,
			in.InitCtx, false, EobToken)
	}
	if eob > maxEob {
		eob = maxEob
	}

	dcAbs, dcSign := CoeffMagnitudeAndSign(in.QCoeffs, 0, in.Coeffs[0],
		in.Dequant[0], in.TxSize == common.Tx32x32)
	prevToken, extraCost := CoeffTokenExtraCost(dcAbs, dcSign)
	rate := extraCost + coeffBlockTreeTokenCost(in.CostTable, in.CoefModel,
		0, in.InitCtx, false, prevToken)
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
		rate += extra + coeffBlockTreeTokenCost(in.CostTable, in.CoefModel, band, pt,
			prevToken == ZeroToken, token)
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
		rate += coeffBlockTreeTokenCost(in.CostTable, in.CoefModel, band, pt,
			false, EobToken)
	}
	return rate
}

func coeffBlockRateCostFastQ(in CoeffBlockRateCostInput, scan []int16,
	maxEob int,
) int {
	if in.QCoeffs != nil {
		return coeffBlockRateCostFastCompleteQCoeff(in, scan, maxEob)
	}
	eob := coeffBlockEOBEncode(scan, maxEob, in.Coeffs, in.QCoeffs)
	if eob == 0 {
		return coeffBlockTreeTokenCost(in.CostTable, in.CoefModel, 0,
			in.InitCtx, false, EobToken)
	}

	rate := 0
	dcAbs, dcSign := CoeffMagnitudeAndSign(in.QCoeffs, 0, in.Coeffs[0],
		in.Dequant[0], in.TxSize == common.Tx32x32)
	prevToken, extraCost := CoeffTokenExtraCost(dcAbs, dcSign)
	rate += extraCost
	rate += coeffBlockTreeTokenCost(in.CostTable, in.CoefModel, 0, in.InitCtx,
		false, prevToken)

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
		rate += coeffBlockTreeTokenCost(in.CostTable, in.CoefModel, bandIdx,
			ctx, skipEOB, token)
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
		rate += coeffBlockTreeTokenCost(in.CostTable, in.CoefModel, bandIdx,
			ctx, false, EobToken)
	}
	return rate
}

func coeffBlockRateCostFastCompleteQCoeff(in CoeffBlockRateCostInput,
	scan []int16, maxEob int,
) int {
	if in.CostTable != nil {
		return coeffBlockRateCostFastCompleteQCoeffTable(in, scan, maxEob)
	}
	eob := coeffBlockEOBCompleteQCoeff(scan, maxEob, in.QCoeffs)
	if eob == 0 {
		return coeffBlockTreeTokenCost(in.CostTable, in.CoefModel, 0,
			in.InitCtx, false, EobToken)
	}

	rate := 0
	prevToken, extraCost := coeffTokenExtraCostQCoeff(in.QCoeffs[0])
	rate += extraCost
	rate += coeffBlockTreeTokenCost(in.CostTable, in.CoefModel, 0, in.InitCtx,
		false, prevToken)

	bandIdx := 1
	bandLeft := coeffCostBandCounts[in.TxSize][bandIdx]
	for c := 1; c < eob; c++ {
		raster := int(scan[c])
		token, extra := coeffTokenExtraCostQCoeff(in.QCoeffs[raster])
		ctx := 0
		skipEOB := false
		if prevToken == ZeroToken {
			ctx = 1
			skipEOB = true
		}
		rate += extra
		rate += coeffBlockTreeTokenCost(in.CostTable, in.CoefModel, bandIdx,
			ctx, skipEOB, token)
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
		rate += coeffBlockTreeTokenCost(in.CostTable, in.CoefModel, bandIdx,
			ctx, false, EobToken)
	}
	return rate
}

func coeffBlockRateCostFastCompleteQCoeffTable(in CoeffBlockRateCostInput,
	scan []int16, maxEob int,
) int {
	eob := coeffBlockEOBCompleteQCoeff(scan, maxEob, in.QCoeffs)
	costs := in.CostTable
	if eob == 0 {
		return int((*costs)[0][in.InitCtx][0][EobToken])
	}

	rate := 0
	prevToken, extraCost := coeffTokenExtraCostQCoeff(in.QCoeffs[0])
	rate += extraCost
	rate += int((*costs)[0][in.InitCtx][0][prevToken])

	bandIdx := 1
	bandLeft := coeffCostBandCounts[in.TxSize][bandIdx]
	for c := 1; c < eob; c++ {
		raster := int(scan[c])
		token, extra := coeffTokenExtraCostQCoeff(in.QCoeffs[raster])
		ctx := 0
		skipIdx := 0
		if prevToken == ZeroToken {
			ctx = 1
			skipIdx = 1
		}
		rate += extra
		rate += int((*costs)[bandIdx][ctx][skipIdx][token])
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
		rate += int((*costs)[bandIdx][ctx][0][EobToken])
	}
	return rate
}

func coeffTokenExtraCostQCoeff(q int16) (token int, cost int) {
	entry := coeffQCoeffTokenExtraCostTable[uint16(q)]
	return int(entry.token), int(entry.cost)
}

func coeffBlockTreeTokenCost(costTable *CoeffTreeTokenCostTable,
	coefModel *[vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8,
	band, ctx int, skipEOB bool, token int,
) int {
	if costTable != nil {
		return costTable.TokenCost(band, ctx, skipEOB, token)
	}
	if band < 0 || band >= vp9dec.CoefBands ||
		ctx < 0 || ctx >= vp9dec.CoefContexts || coefModel == nil {
		return 0
	}
	return CoeffTreeTokenCost((*coefModel)[band][ctx][:], skipEOB, token)
}
