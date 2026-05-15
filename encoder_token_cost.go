package govpx

import (
	"math"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func coefficientBlockTokenRate(probs *vp8tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) int {
	// Three uint range checks fold the (x < 0 || x >= max) pairs into
	// one branch each; nil/dual-bound guard is one short-circuit OR away.
	if probs == nil || qcoeff == nil ||
		uint(blockType) >= uint(vp8tables.BlockTypes) ||
		uint(ctx) >= uint(vp8tables.PrevCoefContexts) ||
		uint(skipDC) > 1 {
		return maxInt() / 4
	}
	eob = min(max(eob, skipDC), 16)

	pt := ctx
	cost := 0
	pos := skipDC
	// elidedThreshold mirrors libvpx's skip_eob_node firing condition: in
	// the type==0 (Y after Y2) plane the first encoded band is index 1, in
	// every other plane it is index 0. Read from the shared lookup table so
	// the per-block setup is a single branchless load.
	elidedThreshold := coefElisionBandThreshold[blockType&3]
	// signCostLookup[0] is the cost when coeff >= 0 (sign bit = 0),
	// signCostLookup[1] when coeff < 0 (sign bit = 1). Indexed off the
	// arithmetic-shift sign bit so the per-coefficient sign cost lookup
	// is branch-free.
	signCostLookup := [2]int{
		vp8tables.ProbCost[128],
		vp8tables.ProbCost[255-128],
	}
	for pos < eob {
		// pos ∈ [skipDC, 16) (eob is clamped above); CoefBandsTable and
		// DefaultZigZag1D are [16]-sized. blockType is in [0, 4) and
		// the CoefBandsTable cells are in [0, 8); mask both with their
		// pow2-1 to elide the bounds checks on the (*probs) load and
		// the per-coefficient table lookups.
		band := int(vp8tables.CoefBandsTable[pos&15])
		p := (*probs)[blockType&3][band&7][pt]
		rc := int(vp8tables.DefaultZigZag1D[pos&15]) & 15
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
		// Sign-cost lookup keyed off the sign bit: coeff>>shift is -1 for
		// negative, 0 for non-negative; masking with 1 selects between
		// signCost0 and signCost1 without a branch.
		cost += signCostLookup[(coeff>>mvKernelSignShift)&1]
		cost += coefficientExtraBitsRate(t, mag)
		pt = int(vp8tables.PrevTokenClass[t])
		pos++
	}
	if pos < 16 {
		// Same pow2 AND-mask pattern as the main loop body.
		band := int(vp8tables.CoefBandsTable[pos&15])
		p := (*probs)[blockType&3][band&7][pt]
		cost += coefEOBTokenCost(&p)
	}
	return cost
}

// coefTokenCostElided returns the token cost charged at one coefficient
// position. It mirrors libvpx's `token_costs` table layout: when the prior
// token's prev_token_class is 0 (a ZERO_TOKEN) and the current band is past
// the plane's first encoded band, libvpx fills the row via
// vp8_cost_tokens2(..., start=2), which writes only the non-EOB subtree
// terminals (TWO..DCT_CAT6) and leaves the EOB slot at the calloc'd zero
// seed (because cost_tokens2 starts past the EOB tree edge). Mirror that
// by returning 0 for DCT_EOB_TOKEN on elided rows so the trellis rate
// matches libvpx's mb->token_costs lookup exactly. The trellis shortcut
// path reaches the elided EOB slot when the current position rounds to
// zero (pt=0) and the next trellis state's token is DCT_EOB_TOKEN — in
// that case libvpx adds 0, not the full-minus-nonEOB value that govpx
// previously returned.
func coefTokenCostElided(probs [vp8tables.EntropyNodes]uint8, token int, blockType int, band int, pt int) int {
	// Single uint range check folds (token < 0) and (token >= len) so the
	// per-coefficient hot path only does one compare-and-branch.
	if uint(token) >= uint(len(coefTokenPaths)) {
		return maxInt() / 4
	}
	if pt == 0 && band > coefElisionBandThreshold[blockType&3] {
		if token == vp8tables.DCTEOBToken {
			// Libvpx's vp8_cost_tokens2(start=2) skips the EOB branch
			// entirely, so the cell stays at the calloc'd zero seed.
			return 0
		}
		full := coefTokenCostFromPath(&coefTokenPaths[token], &probs)
		// nonEOB == boolBitCost(probs[0], 1) == ProbCost[255-probs[0]].
		nonEOB := vp8tables.ProbCost[255-int(probs[0])]
		if full <= nonEOB {
			return maxInt() / 4
		}
		return full - nonEOB
	}
	return coefTokenCostFromPath(&coefTokenPaths[token], &probs)
}

// coefElisionBandThreshold maps blockType -> the first band that participates
// in libvpx's skip_eob_node elision. blockType 0 (Y-after-Y2 plane) starts
// elision at band > 1; every other plane starts at band > 0. Replaces a
// per-coefficient `if blockType == 0` branch with a bounded array load.
var coefElisionBandThreshold = [4]int{1, 0, 0, 0}

func coefficientTokenCost(probs [vp8tables.EntropyNodes]uint8, token int, blockType int, band int, pt int) int {
	return coefTokenCostElided(probs, token, blockType, band, pt)
}

func nonZeroCoeffTokenRate(probs [vp8tables.EntropyNodes]uint8, token int) int {
	if uint(token) >= uint(len(coefTokenPaths)) {
		return maxInt() / 4
	}
	cost := coefTokenCostFromPath(&coefTokenPaths[token], &probs)
	nonEOBRate := vp8tables.ProbCost[255-int(probs[0])]
	if cost <= nonEOBRate {
		return maxInt() / 4
	}
	return cost - nonEOBRate
}

// coefficientTokenLUT maps |coefficient| in [1, DCTMaxValue] to its libvpx
// token classification. Index 0 is unused (caller separately reports
// (0, 0, false) for coeff==0). Replaces the if-else ladder so the per-
// coefficient lookup is a single bounded array load.
var coefficientTokenLUT = buildCoefficientTokenLUT()

func buildCoefficientTokenLUT() [vp8tables.DCTMaxValue + 1]uint8 {
	var lut [vp8tables.DCTMaxValue + 1]uint8
	lut[1] = vp8tables.OneToken
	lut[2] = vp8tables.TwoToken
	lut[3] = vp8tables.ThreeToken
	lut[4] = vp8tables.FourToken
	for i := 5; i <= 6; i++ {
		lut[i] = vp8tables.DCTValCategory1
	}
	for i := 7; i <= 10; i++ {
		lut[i] = vp8tables.DCTValCategory2
	}
	for i := 11; i <= 18; i++ {
		lut[i] = vp8tables.DCTValCategory3
	}
	for i := 19; i <= 34; i++ {
		lut[i] = vp8tables.DCTValCategory4
	}
	for i := 35; i <= 66; i++ {
		lut[i] = vp8tables.DCTValCategory5
	}
	for i := 67; i <= vp8tables.DCTMaxValue; i++ {
		lut[i] = vp8tables.DCTValCategory6
	}
	return lut
}

func coefficientTokenMagnitude(coeff int) (int, int, bool) {
	mask := coeff >> mvKernelSignShift
	coeff = (coeff ^ mask) - mask
	// uint cast collapses (coeff <= 0) and (coeff > DCTMaxValue) into one
	// out-of-range check: coeff==0 wraps to ^uint(0); negative coeff cannot
	// arise post-abs but would also wrap.
	if uint(coeff-1) >= uint(vp8tables.DCTMaxValue) {
		return 0, 0, false
	}
	return int(coefficientTokenLUT[coeff]), coeff, true
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
		// Single uint compare folds the (token < 0) and (token >= len)
		// range check into one branch.
		if uint(token) >= uint(len(paths)) {
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
		// uint range folds the (probIndex < 0) and (>= len) bounds into
		// one compare; the tree bound is independent so kept separate.
		if uint(probIndex) >= uint(len(probs)) || int(node)+1 >= len(tree) {
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
	// Branchless sign-XOR: prob^uint8(-bit) flips prob to 255-prob
	// (== ^prob) when bit is set.
	return vp8tables.ProbCost[prob^uint8(-bit)]
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
	// vp8_initialize_rd_consts sets x->errorperbit from the raw RDMULT before
	// large multipliers are divided by 100 and paired with RDDIV=1.
	errorPerBit := libvpxRawRDMultiplierWithZbin(qIndex, zbinOverQuant) / 110
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

func libvpxRawRDMultiplierWithZbin(qIndex int, zbinOverQuant int) int {
	qValue := min(vp8common.DCQuant(qIndex, 0), 160)
	// zbinOverQuant=0 collapses oqFactor to 1.0 and modq to qValue, so
	// the no-zbin path produces the same rdMult as the modq formula.
	// Single straight-line computation drops the function below the
	// inliner budget.
	modq := int(float64(qValue) * (1.0 + 0.0015625*float64(zbinOverQuant)))
	return int(2.80 * float64(modq*modq))
}

func libvpxRDConstantsWithZbin(qIndex int, zbinOverQuant int) (int, int) {
	rdMult := libvpxRawRDMultiplierWithZbin(qIndex, zbinOverQuant)
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

var libvpxFullPelMVSADComponentCost16 [vp8common.QIndexRange][256]int

func init() {
	initLibvpxFullPelMVSADComponentCost16()
}

func initLibvpxFullPelMVSADComponentCost16() {
	for q := range libvpxFullPelMVSADComponentCost16 {
		sadPerBit := libvpxSADPerBit16LUT[q]
		for i := range libvpxFullPelMVSADComponentCost16[q] {
			cost := 300
			if i > 0 {
				cost = int(256 * (2 * (math.Log2(float64(8*i)) + 0.6)))
			}
			libvpxFullPelMVSADComponentCost16[q][i] = cost * sadPerBit
		}
	}
}

func libvpxFullPelMVSADCost16FromDeltas(mvRow8 int, mvCol8 int, refRow8 int, refCol8 int, qIndex int) int {
	// Clamp-to-[-255,255] then abs collapses to abs-then-clamp-to-255:
	// the original sign of the delta has no effect on the cost table
	// lookup (costs are symmetric in delta sign).
	rowDelta := mvRow8 - refRow8
	rowMask := rowDelta >> mvKernelSignShift
	rowDelta = min((rowDelta^rowMask)-rowMask, 255)
	colDelta := mvCol8 - refCol8
	colMask := colDelta >> mvKernelSignShift
	colDelta = min((colDelta^colMask)-colMask, 255)
	costs := &libvpxFullPelMVSADComponentCost16[vp8common.ClampQIndex(qIndex)]
	return (costs[rowDelta] + costs[colDelta] + 128) >> 8
}
