package govpx

import (
	"math"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func coefficientBlockTokenRate(probs *vp8tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) int {
	if probs == nil || qcoeff == nil || blockType < 0 || blockType >= vp8tables.BlockTypes || ctx < 0 || ctx >= vp8tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return maxInt() / 4
	}
	if eob < skipDC {
		eob = skipDC
	}
	if eob > 16 {
		eob = 16
	}

	pt := ctx
	cost := 0
	pos := skipDC
	// elidedThreshold mirrors libvpx's skip_eob_node firing condition: in
	// the type==0 (Y after Y2) plane the first encoded band is index 1, in
	// every other plane it is index 0. Hoisted out of the inner loop so the
	// per-position elision check is a single int compare.
	elidedThreshold := 0
	if blockType == 0 {
		elidedThreshold = 1
	}
	signCost0 := vp8tables.ProbCost[128]
	signCost1 := vp8tables.ProbCost[255-128]
	for pos < eob {
		band := int(vp8tables.CoefBandsTable[pos])
		p := (*probs)[blockType][band][pt]
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coeff := int(qcoeff[rc])
		if coeff == 0 {
			// ZeroToken sits two edges deep in CoefTree (root, then the
			// non-EOB branch). The libvpx elision drops the EOB bit when
			// pt==0 and band > elidedThreshold, leaving only the second
			// edge at probs[1].
			if pt == 0 && band > elidedThreshold {
				cost += coefZeroTokenCostElided(&p)
			} else {
				cost += coefZeroTokenCost(&p)
			}
			pt = int(vp8tables.PrevTokenClass[vp8tables.ZeroToken])
			pos++
			continue
		}
		t, mag, ok := coefficientTokenMagnitude(coeff)
		if !ok {
			return maxInt() / 4
		}
		cost += coefTokenCostElided(p, t, blockType, band, pt)
		if coeff < 0 {
			cost += signCost1
		} else {
			cost += signCost0
		}
		cost += coefficientExtraBitsRate(t, mag)
		pt = int(vp8tables.PrevTokenClass[t])
		pos++
	}
	if pos < 16 {
		band := int(vp8tables.CoefBandsTable[pos])
		p := (*probs)[blockType][band][pt]
		cost += coefEOBTokenCost(&p)
	}
	return cost
}

// coefTokenCostElided returns the token cost charged at one coefficient
// position. It mirrors libvpx's `token_costs` table: when the prior token's
// prev_token_class is 0 (a ZERO_TOKEN) and the current band is past the
// plane's first encoded band, the EOB-vs-not bit is elided and only the
// non-EOB subtree cost is charged. Otherwise the full tree cost is charged.
func coefTokenCostElided(probs [vp8tables.EntropyNodes]uint8, token int, blockType int, band int, pt int) int {
	if token < 0 || token >= len(coefTokenPaths) {
		return maxInt() / 4
	}
	threshold := 0
	if blockType == 0 {
		threshold = 1
	}
	full := coefTokenCostFromPath(&coefTokenPaths[token], &probs)
	if pt == 0 && band > threshold {
		// nonEOB == boolBitCost(probs[0], 1) == ProbCost[255-probs[0]].
		nonEOB := vp8tables.ProbCost[255-int(probs[0])]
		if full <= nonEOB {
			return maxInt() / 4
		}
		return full - nonEOB
	}
	return full
}

func coefficientTokenCost(probs [vp8tables.EntropyNodes]uint8, token int, blockType int, band int, pt int) int {
	return coefTokenCostElided(probs, token, blockType, band, pt)
}

func nonZeroCoeffTokenRate(probs [vp8tables.EntropyNodes]uint8, token int) int {
	if token < 0 || token >= len(coefTokenPaths) {
		return maxInt() / 4
	}
	cost := coefTokenCostFromPath(&coefTokenPaths[token], &probs)
	nonEOBRate := vp8tables.ProbCost[255-int(probs[0])]
	if cost <= nonEOBRate {
		return maxInt() / 4
	}
	return cost - nonEOBRate
}

func coefficientTokenMagnitude(coeff int) (int, int, bool) {
	if coeff < 0 {
		coeff = -coeff
	}
	switch {
	case coeff <= 0:
		return 0, 0, false
	case coeff == 1:
		return vp8tables.OneToken, coeff, true
	case coeff == 2:
		return vp8tables.TwoToken, coeff, true
	case coeff == 3:
		return vp8tables.ThreeToken, coeff, true
	case coeff == 4:
		return vp8tables.FourToken, coeff, true
	case coeff <= 6:
		return vp8tables.DCTValCategory1, coeff, true
	case coeff <= 10:
		return vp8tables.DCTValCategory2, coeff, true
	case coeff <= 18:
		return vp8tables.DCTValCategory3, coeff, true
	case coeff <= 34:
		return vp8tables.DCTValCategory4, coeff, true
	case coeff <= 66:
		return vp8tables.DCTValCategory5, coeff, true
	case coeff <= vp8tables.DCTMaxValue:
		return vp8tables.DCTValCategory6, coeff, true
	default:
		return 0, 0, false
	}
}

func coefficientExtraBitsRate(token int, mag int) int {
	extra := vp8tables.ExtraBitsTable[token]
	offset := mag - int(extra.BaseVal)
	cost := 0
	for i := 0; i < int(extra.Len); i++ {
		shift := int(extra.Len) - 1 - i
		bit := int((offset >> uint(shift)) & 1)
		cost += boolBitCost(extra.Prob[i], bit)
	}
	return cost
}

func treeTokenCost(tree []int16, probs []uint8, token int) int {
	if paths := lookupTreeTokenPaths(tree); paths != nil {
		if token < 0 || token >= len(paths) {
			return maxInt() / 4
		}
		return treeTokenCostFromPath(&paths[token], probs)
	}
	return treeTokenCostSlow(tree, probs, token)
}

// treeTokenCostSlow is the fallback walker for trees that do not have a
// precomputed path table (e.g. ad-hoc trees in tests). It mirrors the
// historical implementation byte-for-byte.
func treeTokenCostSlow(tree []int16, probs []uint8, token int) int {
	var encoded vp8enc.TreeToken
	if !vp8enc.BuildTreeToken(tree, token, &encoded) {
		return maxInt() / 4
	}
	node := int16(0)
	cost := 0
	for bitIndex := int(encoded.Len) - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		if probIndex < 0 || probIndex >= len(probs) || int(node)+1 >= len(tree) {
			return maxInt() / 4
		}
		prob := probs[probIndex]
		bit := int((encoded.Value >> uint(bitIndex)) & 1)
		cost += boolBitCost(prob, bit)
		next := tree[int(node)+bit]
		if next <= 0 {
			if bitIndex == 0 {
				return cost
			}
			return maxInt() / 4
		}
		node = next
	}
	return maxInt() / 4
}

func boolBitCost(prob uint8, bit int) int {
	if bit == 0 {
		return vp8tables.ProbCost[prob]
	}
	return vp8tables.ProbCost[255-int(prob)]
}

func rdModeScore(qIndex int, rate int, distortion int) int {
	return rdModeScoreWithZbin(qIndex, 0, rate, distortion)
}

func rdModeScoreWithZbin(qIndex int, zbinOverQuant int, rate int, distortion int) int {
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

func libvpxInterIntraRDPenalty(qIndex int) int {
	return 10 * vp8common.DCQuant(qIndex, 0)
}

// libvpxErrorPerBit ports the encodeframe.c errorperbit derivation used by
// libvpx fractional motion searches.
func libvpxErrorPerBit(qIndex int) int {
	return libvpxErrorPerBitWithZbin(qIndex, 0)
}

func libvpxErrorPerBitWithZbin(qIndex int, zbinOverQuant int) int {
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	errorPerBit := rdMult * 100 / (110 * rdDiv)
	if errorPerBit == 0 {
		return 1
	}
	return errorPerBit
}

// libvpxSADPerBit16 ports sad_per_bit16lut from
// vp8/encoder/rdopt.c vp8cx_initialize_me_consts.
func libvpxSADPerBit16(qIndex int) int {
	return libvpxSADPerBit16LUT[vp8common.ClampQIndex(qIndex)]
}

// libvpxSADPerBit4 ports sad_per_bit4lut from
// vp8/encoder/rdopt.c vp8cx_initialize_me_consts for SPLITMV block search.
func libvpxSADPerBit4(qIndex int) int {
	return libvpxSADPerBit4LUT[vp8common.ClampQIndex(qIndex)]
}

// libvpxRDConstants ports vp8_initialize_rd_consts for the single-pass path.
func libvpxRDConstants(qIndex int) (int, int) {
	return libvpxRDConstantsWithZbin(qIndex, 0)
}

func libvpxRDConstantsWithZbin(qIndex int, zbinOverQuant int) (int, int) {
	qValue := min(vp8common.DCQuant(qIndex, 0), 160)
	rdMult := int(2.80 * float64(qValue*qValue))
	if zbinOverQuant > 0 {
		oqFactor := 1.0 + 0.0015625*float64(zbinOverQuant)
		modq := int(float64(qValue) * oqFactor)
		rdMult = int(2.80 * float64(modq*modq))
	}
	rdDiv := 100
	if rdMult > 1000 {
		rdDiv = 1
		rdMult /= 100
	}
	return rdMult, rdDiv
}

func libvpxRDCost(rdMult int, rdDiv int, rate int, distortion int) int {
	return ((128 + rate*rdMult) >> 8) + rdDiv*distortion
}

var libvpxSADPerBit16LUT = [vp8common.QIndexRange]int{
	2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2,
	3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 4, 4,
	4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6,
	6, 6, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 9, 9, 9, 9, 9, 9,
	9, 9, 9, 9, 9, 9, 10, 10,
	10, 10, 10, 10, 10, 10, 11, 11,
	11, 11, 11, 11, 12, 12, 12, 12,
	12, 12, 13, 13, 13, 13, 14, 14,
}

var libvpxSADPerBit4LUT = [vp8common.QIndexRange]int{
	2, 2, 2, 2, 2, 2, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 4, 4, 4, 4,
	4, 4, 4, 4, 4, 4, 5, 5,
	5, 5, 5, 5, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6,
	7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 8, 8, 8,
	8, 8, 9, 9, 9, 9, 9, 9,
	10, 10, 10, 10, 10, 10, 10, 10,
	11, 11, 11, 11, 11, 11, 11, 11,
	12, 12, 12, 12, 12, 12, 12, 12,
	13, 13, 13, 13, 13, 13, 13, 14,
	14, 14, 14, 14, 15, 15, 15, 15,
	16, 16, 16, 16, 17, 17, 17, 18,
	18, 18, 19, 19, 19, 20, 20, 20,
}

var libvpxFullPelMVSADComponentCost16 = buildLibvpxFullPelMVSADComponentCost16()

func buildLibvpxFullPelMVSADComponentCost16() [vp8common.QIndexRange][256]int {
	var out [vp8common.QIndexRange][256]int
	for q := range out {
		sadPerBit := libvpxSADPerBit16LUT[q]
		for i := range out[q] {
			cost := 300
			if i > 0 {
				cost = int(256 * (2 * (math.Log2(float64(8*i)) + 0.6)))
			}
			out[q][i] = cost * sadPerBit
		}
	}
	return out
}

func libvpxFullPelMVSADCost16FromDeltas(mvRow8 int, mvCol8 int, refRow8 int, refCol8 int, qIndex int) int {
	rowDelta := mvRow8 - refRow8
	if rowDelta > 255 {
		rowDelta = 255
	} else if rowDelta < -255 {
		rowDelta = -255
	}
	if rowDelta < 0 {
		rowDelta = -rowDelta
	}
	colDelta := mvCol8 - refCol8
	if colDelta > 255 {
		colDelta = 255
	} else if colDelta < -255 {
		colDelta = -255
	}
	if colDelta < 0 {
		colDelta = -colDelta
	}
	costs := &libvpxFullPelMVSADComponentCost16[vp8common.ClampQIndex(qIndex)]
	return (costs[rowDelta] + costs[colDelta] + 128) >> 8
}
