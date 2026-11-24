package encoder

import vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"

// Coefficient rate and block RD helpers mirror libvpx v1.16.0 VP8
// vp8/encoder/rdopt.c and vp8/encoder/encodemb.c mechanics.

// RDBlockScore returns libvpx VP8's block RDCOST after applying the plane and
// intra multipliers.
func RDBlockScore(qIndex int, planeMultiplier int, intra bool, rate int, distortion int) int {
	return RDBlockScoreWithZbin(qIndex, 0, planeMultiplier, intra, rate, distortion)
}

// RDBlockScoreWithZbin returns libvpx VP8's zbin-adjusted block RDCOST after
// applying the plane and intra multipliers.
func RDBlockScoreWithZbin(qIndex int, zbinOverQuant int, planeMultiplier int, intra bool, rate int, distortion int) int {
	if planeMultiplier <= 0 {
		planeMultiplier = 1
	}
	rdMult, rdDiv := RDConstantsWithZbin(qIndex, zbinOverQuant)
	rdMult *= planeMultiplier
	if intra {
		rdMult = (rdMult * 9) >> 4
	}
	return RDCost(rdMult, rdDiv, rate, distortion)
}

// BlockPlaneRDMultiplier maps VP8 coefficient block types to libvpx's
// plane_rd_mult table.
func BlockPlaneRDMultiplier(blockType int) int {
	switch blockType {
	case 1:
		return 16
	case 2:
		return 2
	default:
		return 4
	}
}

// MacroblockCoefficientTokenRate returns the coefficient-token rate for a VP8
// macroblock using fresh zero token contexts.
func MacroblockCoefficientTokenRate(probs *vp8tables.CoefficientProbs, is4x4 bool, coeffs *MacroblockCoefficients) int {
	return MacroblockCoefficientTokenRateWithContext(probs, is4x4, nil, nil, coeffs)
}

// MacroblockCoefficientTokenRateWithContext returns the coefficient-token rate
// for a VP8 macroblock using the supplied above/left token contexts.
func MacroblockCoefficientTokenRateWithContext(probs *vp8tables.CoefficientProbs, is4x4 bool, aboveTok *TokenContextPlanes, leftTok *TokenContextPlanes, coeffs *MacroblockCoefficients) int {
	if probs == nil || coeffs == nil {
		return int(^uint(0)>>1) / 4
	}

	rate := 0
	blockType := 0
	skipDC := 0
	var yAbove [4]uint8
	var yLeft [4]uint8
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		uvAbove = TokenUVContextArray(aboveTok)
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		uvLeft = TokenUVContextArray(leftTok)
		y2Left = leftTok.Y2
	}
	if !is4x4 {
		eob := coeffs.BlockEOB(24, 0)
		rate += CoefficientBlockTokenRate(probs, 1, int(y2Above+y2Left), 0, &coeffs.QCoeff[24], eob)
		blockType = 0
		skipDC = 1
	} else {
		blockType = 3
	}

	for block := range 16 {
		eob := coeffs.BlockEOB(block, skipDC)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		rate += CoefficientBlockTokenRate(probs, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > skipDC {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
	}

	for block := 16; block < 24; block++ {
		eob := coeffs.BlockEOB(block, 0)
		a, l := MacroblockCoefficientUVContextIndex(block)
		ctx := int(uvAbove[a] + uvLeft[l])
		rate += CoefficientBlockTokenRate(probs, 2, ctx, 0, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	return rate
}

// TokenUVContextArray converts VP8's split U/V token contexts into the
// four-entry UV array used by libvpx's block2above/block2left chroma mapping.
func TokenUVContextArray(ctx *TokenContextPlanes) [4]uint8 {
	if ctx == nil {
		return [4]uint8{}
	}
	return [4]uint8{ctx.U[0], ctx.U[1], ctx.V[0], ctx.V[1]}
}

// MacroblockCoefficientUVContextIndex maps coefficient blocks 16..23 to the
// UV above/left context indices used by libvpx VP8's chroma token writer.
func MacroblockCoefficientUVContextIndex(block int) (int, int) {
	base := 0
	if block > 19 {
		base = 2
	}
	a := base + (block & 1)
	l := base
	if block&3 > 1 {
		l++
	}
	return a, l
}
