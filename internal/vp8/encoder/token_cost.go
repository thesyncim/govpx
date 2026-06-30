package encoder

import (
	"math/bits"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/rdopt.c token-cost helpers and
// vp8/encoder/onyx_if.c default sub-MV probabilities.

const tokenCostSignShift = bits.UintSize - 1

var coefficientSignCost = [2]int{
	tables.ProbCost[128],
	tables.ProbCost[255-128],
}

// DefaultSubMVRefProbs mirrors libvpx's default sub-MV reference
// probabilities for split-motion mode decisions.
var DefaultSubMVRefProbs = [3]uint8{180, 162, 25}

func tokenCostMaxInt() int {
	return int(^uint(0) >> 1)
}

// CoefficientTokenCostTable caches libvpx-compatible coefficient token costs
// for a fixed coefficient-probability table. Token costs honor skip_eob_node
// elision; terminating EOB costs are stored separately because block-rate
// scoring always charges the full EOB tree cost.
type CoefficientTokenCostTable struct {
	Token [tables.BlockTypes][tables.CoefBands][tables.PrevCoefContexts][tables.MaxEntropyTokens]int
	EOB   [tables.BlockTypes][tables.CoefBands][tables.PrevCoefContexts]int
}

// FillCoefficientTokenCostTable builds the per-frame coefficient token-cost
// table used by VP8 RD scoring.
func FillCoefficientTokenCostTable(probs *tables.CoefficientProbs, costs *CoefficientTokenCostTable) bool {
	if probs == nil || costs == nil {
		return false
	}
	for blockType := range tables.BlockTypes {
		for band := range tables.CoefBands {
			for ctx := range tables.PrevCoefContexts {
				p := (*probs)[blockType][band][ctx]
				for token := range tables.MaxEntropyTokens {
					costs.Token[blockType][band][ctx][token] = coefTokenCostElided(p, token, blockType, band, ctx)
				}
				costs.EOB[blockType][band][ctx] = coefEOBTokenCost(&p)
			}
		}
	}
	return true
}

// CoefficientBlockTokenRate returns the libvpx-compatible coefficient token
// rate for one 4x4/Y2 block.
func CoefficientBlockTokenRate(probs *tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) int {
	// Three uint range checks fold the (x < 0 || x >= max) pairs into
	// one branch each; nil/dual-bound guard is one short-circuit OR away.
	if probs == nil || qcoeff == nil ||
		uint(blockType) >= uint(tables.BlockTypes) ||
		uint(ctx) >= uint(tables.PrevCoefContexts) ||
		uint(skipDC) > 1 {
		return tokenCostMaxInt() / 4
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
	for pos < eob {
		// pos ∈ [skipDC, 16) (eob is clamped above); CoefBandsTable and
		// DefaultZigZag1D are [16]-sized. blockType is in [0, 4) and
		// the CoefBandsTable cells are in [0, 8); mask both with their
		// pow2-1 to elide the bounds checks on the (*probs) load and
		// the per-coefficient table lookups.
		band := int(tables.CoefBandsTable[pos&15])
		p := (*probs)[blockType&3][band&7][pt]
		rc := int(tables.DefaultZigZag1D[pos&15]) & 15
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
			pt = int(tables.PrevTokenClass[tables.ZeroToken])
			pos++
			continue
		}
		mask := coeff >> tokenCostSignShift
		sign := mask & 1
		mag := (coeff ^ mask) - mask
		if uint(mag-1) >= uint(tables.DCTMaxValue) {
			return tokenCostMaxInt() / 4
		}
		entry := coefficientTokenRateLUT[mag]
		t := int(entry.token)
		cost += coefTokenCostElided(p, t, blockType, band, pt)
		cost += coefficientSignCost[sign]
		cost += int(entry.extra)
		pt = int(tables.PrevTokenClass[t])
		pos++
	}
	if pos < 16 {
		// Same pow2 AND-mask pattern as the main loop body.
		band := int(tables.CoefBandsTable[pos&15])
		p := (*probs)[blockType&3][band&7][pt]
		cost += coefEOBTokenCost(&p)
	}
	return cost
}

// CoefficientBlockTokenRateWithTable returns the same block rate as
// CoefficientBlockTokenRate using a precomputed token-cost table.
func CoefficientBlockTokenRateWithTable(costs *CoefficientTokenCostTable, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) int {
	if costs == nil || qcoeff == nil ||
		uint(blockType) >= uint(tables.BlockTypes) ||
		uint(ctx) >= uint(tables.PrevCoefContexts) ||
		uint(skipDC) > 1 {
		return tokenCostMaxInt() / 4
	}
	eob = min(max(eob, skipDC), 16)
	bt := blockType & 3

	pt := ctx
	cost := 0
	pos := skipDC
	if pos == eob {
		if pos < 16 {
			band := int(tables.CoefBandsTable[pos&15])
			return costs.EOB[bt][band&7][pt]
		}
		return 0
	}
	for pos < eob {
		band := int(tables.CoefBandsTable[pos&15])
		rc := int(tables.DefaultZigZag1D[pos&15]) & 15
		coeff := int(qcoeff[rc])
		if coeff == 0 {
			cost += costs.Token[bt][band&7][pt][tables.ZeroToken]
			pt = int(tables.PrevTokenClass[tables.ZeroToken])
			pos++
			continue
		}
		mask := coeff >> tokenCostSignShift
		sign := mask & 1
		mag := (coeff ^ mask) - mask
		if uint(mag-1) >= uint(tables.DCTMaxValue) {
			return tokenCostMaxInt() / 4
		}
		entry := coefficientTokenRateLUT[mag]
		t := int(entry.token)
		cost += costs.Token[bt][band&7][pt][t]
		cost += coefficientSignCost[sign]
		cost += int(entry.extra)
		pt = int(tables.PrevTokenClass[t])
		pos++
	}
	if pos < 16 {
		band := int(tables.CoefBandsTable[pos&15])
		cost += costs.EOB[bt][band&7][pt]
	}
	return cost
}

// CoefficientBlockTokenRateWithTableTrusted is the hot RD scorer for callers
// that already hold valid encoder-internal state. The caller must pass non-nil
// costs/qcoeff, blockType in [0, BlockTypes), ctx in [0, PrevCoefContexts),
// skipDC in {0,1}, eob in [skipDC,16], and quantized coefficients whose
// magnitudes fit DCTMaxValue. It mirrors libvpx's cost_coeffs table walk while
// leaving defensive validation to CoefficientBlockTokenRateWithTable.
func CoefficientBlockTokenRateWithTableTrusted(costs *CoefficientTokenCostTable,
	blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int,
) int {
	bt := blockType & 3
	pt := ctx
	pos := skipDC
	if pos == eob {
		if pos < 16 {
			band := int(tables.CoefBandsTable[pos&15])
			return costs.EOB[bt][band&7][pt]
		}
		return 0
	}

	cost := 0
	for pos < eob {
		band := int(tables.CoefBandsTable[pos&15])
		rc := int(tables.DefaultZigZag1D[pos&15]) & 15
		coeff := int(qcoeff[rc])
		if coeff == 0 {
			cost += costs.Token[bt][band&7][pt][tables.ZeroToken]
			pt = int(tables.PrevTokenClass[tables.ZeroToken])
			pos++
			continue
		}
		mask := coeff >> tokenCostSignShift
		sign := mask & 1
		mag := (coeff ^ mask) - mask
		entry := coefficientTokenRateLUT[mag]
		t := int(entry.token)
		cost += costs.Token[bt][band&7][pt][t]
		cost += coefficientSignCost[sign]
		cost += int(entry.extra)
		pt = int(tables.PrevTokenClass[t])
		pos++
	}
	if pos < 16 {
		band := int(tables.CoefBandsTable[pos&15])
		cost += costs.EOB[bt][band&7][pt]
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
func coefTokenCostElided(probs [tables.EntropyNodes]uint8, token int, blockType int, band int, pt int) int {
	// Single uint range check folds (token < 0) and (token >= len) so the
	// per-coefficient hot path only does one compare-and-branch.
	if uint(token) >= uint(len(coefTokenPaths)) {
		return tokenCostMaxInt() / 4
	}
	if pt == 0 && band > coefElisionBandThreshold[blockType&3] {
		if token == tables.DCTEOBToken {
			// Libvpx's vp8_cost_tokens2(start=2) skips the EOB branch
			// entirely, so the cell stays at the calloc'd zero seed.
			return 0
		}
		full := coefTokenCostFromPath(&coefTokenPaths[token], &probs)
		// nonEOB == BoolBitCost(probs[0], 1) == ProbCost[255-probs[0]].
		nonEOB := tables.ProbCost[255-int(probs[0])]
		if full <= nonEOB {
			return tokenCostMaxInt() / 4
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

// CoefElisionBandThreshold returns the first band threshold that participates
// in libvpx's skip_eob_node token-cost elision for blockType.
func CoefElisionBandThreshold(blockType int) int {
	return coefElisionBandThreshold[blockType&3]
}

// CoefElisionBandThresholds returns the libvpx skip_eob_node elision table.
func CoefElisionBandThresholds() [4]int {
	return coefElisionBandThreshold
}

// CoefficientTokenCost returns the coefficient token cost at one trellis
// position, including libvpx's skip_eob_node elision rules.
func CoefficientTokenCost(probs [tables.EntropyNodes]uint8, token int, blockType int, band int, pt int) int {
	return coefTokenCostElided(probs, token, blockType, band, pt)
}

// NonZeroCoeffTokenRate returns the coefficient token rate after removing the
// non-EOB branch that callers account separately.
func NonZeroCoeffTokenRate(probs [tables.EntropyNodes]uint8, token int) int {
	if uint(token) >= uint(len(coefTokenPaths)) {
		return tokenCostMaxInt() / 4
	}
	cost := coefTokenCostFromPath(&coefTokenPaths[token], &probs)
	nonEOBRate := tables.ProbCost[255-int(probs[0])]
	if cost <= nonEOBRate {
		return tokenCostMaxInt() / 4
	}
	return cost - nonEOBRate
}

// coefficientTokenLUT maps |coefficient| in [1, DCTMaxValue] to its libvpx
// token classification. Index 0 is unused (caller separately reports
// (0, 0, false) for coeff==0). Replaces the if-else ladder so the per-
// coefficient lookup is a single bounded array load.
var coefficientTokenLUT = buildCoefficientTokenLUT()

type coefficientTokenRateEntry struct {
	token uint8
	extra uint16
}

var coefficientTokenRateLUT = buildCoefficientTokenRateLUT()

func buildCoefficientTokenLUT() [tables.DCTMaxValue + 1]uint8 {
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

func buildCoefficientTokenRateLUT() [tables.DCTMaxValue + 1]coefficientTokenRateEntry {
	var lut [tables.DCTMaxValue + 1]coefficientTokenRateEntry
	for mag := 1; mag <= tables.DCTMaxValue; mag++ {
		token := coefficientTokenLUT[mag]
		lut[mag] = coefficientTokenRateEntry{
			token: token,
			extra: uint16(CoefficientExtraBitsRate(int(token), mag)),
		}
	}
	return lut
}

// CoefficientTokenMagnitude maps a signed coefficient to its VP8 entropy token
// and absolute magnitude.
func CoefficientTokenMagnitude(coeff int) (int, int, bool) {
	mask := coeff >> tokenCostSignShift
	coeff = (coeff ^ mask) - mask
	// uint cast collapses (coeff <= 0) and (coeff > DCTMaxValue) into one
	// out-of-range check: coeff==0 wraps to ^uint(0); negative coeff cannot
	// arise post-abs but would also wrap.
	if uint(coeff-1) >= uint(tables.DCTMaxValue) {
		return 0, 0, false
	}
	return int(coefficientTokenLUT[coeff]), coeff, true
}

// CoefficientExtraBitsRate returns the category extra-bit cost for a
// coefficient token and magnitude.
func CoefficientExtraBitsRate(token int, mag int) int {
	extra := tables.ExtraBitsTable[token]
	offset := mag - int(extra.BaseVal)
	cost := 0
	for i := 0; i < int(extra.Len); i++ {
		shift := int(extra.Len) - 1 - i
		bit := int((offset >> uint(shift)) & 1)
		cost += BoolBitCost(extra.Prob[i], bit)
	}
	return cost
}

// TreeTokenCost returns the bool-coder cost for a token in a VP8 token tree.
func TreeTokenCost(tree []int16, probs []uint8, token int) int {
	if paths := lookupTreeTokenPaths(tree); paths != nil {
		// Single uint compare folds the (token < 0) and (token >= len)
		// range check into one branch.
		if uint(token) >= uint(len(paths)) {
			return tokenCostMaxInt() / 4
		}
		return treeTokenCostFromPath(&paths[token], probs)
	}
	return treeTokenCostSlow(tree, probs, token)
}

// treeTokenCostSlow is the fallback walker for trees that do not have a
// precomputed path table (e.g. ad-hoc trees in tests). It mirrors the
// historical implementation byte-for-byte.
func treeTokenCostSlow(tree []int16, probs []uint8, token int) int {
	var encoded TreeToken
	if !BuildTreeToken(tree, token, &encoded) {
		return tokenCostMaxInt() / 4
	}
	node := int16(0)
	cost := 0
	for bitIndex := int(encoded.Len) - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		// uint range folds the (probIndex < 0) and (>= len) bounds into
		// one compare; the tree bound is independent so kept separate.
		if uint(probIndex) >= uint(len(probs)) || int(node)+1 >= len(tree) {
			return tokenCostMaxInt() / 4
		}
		prob := probs[probIndex]
		bit := int((encoded.Value >> uint(bitIndex)) & 1)
		cost += BoolBitCost(prob, bit)
		next := tree[int(node)+bit]
		if next <= 0 {
			if bitIndex == 0 {
				return cost
			}
			return tokenCostMaxInt() / 4
		}
		node = next
	}
	return tokenCostMaxInt() / 4
}

// BoolBitCost returns libvpx's vp8_cost_bit result for prob and bit.
func BoolBitCost(prob uint8, bit int) int {
	// Branchless sign-XOR: prob^uint8(-bit) flips prob to 255-prob
	// (== ^prob) when bit is set.
	return tables.ProbCost[prob^uint8(-bit)]
}
