package encoder

import (
	"math/bits"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

const dctValueSignShift = bits.UintSize - 1

// DCTValueToken returns the libvpx v1.16.0 coefficient-token classification
// for value x (mirrors the dct_value_tokens table indexed by signed value).
func DCTValueToken(x int) int {
	mask := x >> dctValueSignShift
	abs := (x ^ mask) - mask
	if abs == 0 {
		return tables.ZeroToken
	}
	token, _, ok := dctCoefficientTokenMagnitude(abs)
	if !ok {
		return tables.ZeroToken
	}
	return token
}

// dctValueBaseCostLUT precomputes libvpx's dct_value_cost table for every
// signed coefficient in [-DCTMaxValue, DCTMaxValue]: extra-bits subtree cost
// plus sign-bit cost. Indexed by abs(x) * 2 + sign_bit so positive and
// negative values share the magnitude-dependent extra-bits cost but pick up the
// correct sign-cost (libvpx's vp8_cost_bit(vp8_prob_half, sign_bit) differs by
// two entropy-bit units between sign=0 and sign=1).
var dctValueBaseCostLUT = buildDCTValueBaseCostLUT()

func buildDCTValueBaseCostLUT() [2 * (tables.DCTMaxValue + 1)]int32 {
	var lut [2 * (tables.DCTMaxValue + 1)]int32
	signCostPositive := dctBoolBitCost(128, 0)
	signCostNegative := dctBoolBitCost(128, 1)
	for abs := 1; abs <= tables.DCTMaxValue; abs++ {
		token := int(dctCoefficientTokenLUT[abs])
		extra := dctCoefficientExtraBitsRate(token, abs)
		lut[abs*2+0] = int32(signCostPositive + extra)
		lut[abs*2+1] = int32(signCostNegative + extra)
	}
	return lut
}

// DCTValueBaseCost mirrors libvpx's dct_value_cost table: extra bits cost plus
// sign bit cost for value x. The token-tree cost is added separately by the
// trellis using band/context-specific token costs.
func DCTValueBaseCost(x int) int {
	mask := x >> dctValueSignShift
	abs := (x ^ mask) - mask
	if uint(abs) > uint(tables.DCTMaxValue) {
		return int(^uint(0)>>1) / 4
	}
	return int(dctValueBaseCostLUT[abs*2+(mask&1)])
}

// RDTrunc mirrors the libvpx v1.16.0 encodemb.c RDTRUNC macro used to break
// ties when two trellis paths have equal RDCOST.
func RDTrunc(rdMult int, rate int) int {
	return (128 + rate*rdMult) & 0xFF
}

var dctCoefficientTokenLUT = buildDCTCoefficientTokenLUT()

func buildDCTCoefficientTokenLUT() [tables.DCTMaxValue + 1]uint8 {
	var lut [tables.DCTMaxValue + 1]uint8
	lut[1] = tables.OneToken
	lut[2] = tables.TwoToken
	lut[3] = tables.ThreeToken
	lut[4] = tables.FourToken
	for i := 5; i <= 6; i++ {
		lut[i] = tables.DCTValCategory1
	}
	for i := 7; i <= 10; i++ {
		lut[i] = tables.DCTValCategory2
	}
	for i := 11; i <= 18; i++ {
		lut[i] = tables.DCTValCategory3
	}
	for i := 19; i <= 34; i++ {
		lut[i] = tables.DCTValCategory4
	}
	for i := 35; i <= 66; i++ {
		lut[i] = tables.DCTValCategory5
	}
	for i := 67; i <= tables.DCTMaxValue; i++ {
		lut[i] = tables.DCTValCategory6
	}
	return lut
}

func dctCoefficientTokenMagnitude(coeff int) (int, int, bool) {
	mask := coeff >> dctValueSignShift
	coeff = (coeff ^ mask) - mask
	if uint(coeff-1) >= uint(tables.DCTMaxValue) {
		return 0, 0, false
	}
	return int(dctCoefficientTokenLUT[coeff]), coeff, true
}

func dctCoefficientExtraBitsRate(token int, mag int) int {
	extra := tables.ExtraBitsTable[token]
	offset := mag - int(extra.BaseVal)
	cost := 0
	for i := 0; i < int(extra.Len); i++ {
		shift := int(extra.Len) - 1 - i
		bit := int((offset >> uint(shift)) & 1)
		cost += dctBoolBitCost(extra.Prob[i], bit)
	}
	return cost
}

func dctBoolBitCost(prob uint8, bit int) int {
	if bit != 0 {
		return tables.ProbCost[255-int(prob)]
	}
	return tables.ProbCost[prob]
}
