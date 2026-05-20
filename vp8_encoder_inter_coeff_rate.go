package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func rdBlockScore(qIndex int, planeMultiplier int, intra bool, rate int, distortion int) int {
	return rdBlockScoreWithZbin(qIndex, 0, planeMultiplier, intra, rate, distortion)
}

func rdBlockScoreWithZbin(qIndex int, zbinOverQuant int, planeMultiplier int, intra bool, rate int, distortion int) int {
	if planeMultiplier <= 0 {
		planeMultiplier = 1
	}
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	rdMult *= planeMultiplier
	if intra {
		rdMult = (rdMult * 9) >> 4
	}
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

func blockPlaneRDMultiplier(blockType int) int {
	switch blockType {
	case 1:
		return 16
	case 2:
		return 2
	default:
		return 4
	}
}

func macroblockCoefficientTokenRate(probs *vp8tables.CoefficientProbs, is4x4 bool, coeffs *vp8enc.MacroblockCoefficients) int {
	return macroblockCoefficientTokenRateWithContext(probs, is4x4, nil, nil, coeffs)
}

func macroblockCoefficientTokenRateWithContext(probs *vp8tables.CoefficientProbs, is4x4 bool, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, coeffs *vp8enc.MacroblockCoefficients) int {
	if probs == nil || coeffs == nil {
		return maxInt() / 4
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
		uvAbove = tokenUVContextArray(aboveTok)
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		uvLeft = tokenUVContextArray(leftTok)
		y2Left = leftTok.Y2
	}
	if !is4x4 {
		eob := coeffs.BlockEOB(24, 0)
		rate += vp8enc.CoefficientBlockTokenRate(probs, 1, int(y2Above+y2Left), 0, &coeffs.QCoeff[24], eob)
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
		rate += vp8enc.CoefficientBlockTokenRate(probs, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > skipDC {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
	}

	for block := 16; block < 24; block++ {
		eob := coeffs.BlockEOB(block, 0)
		a, l := macroblockCoefficientUVContextIndex(block)
		ctx := int(uvAbove[a] + uvLeft[l])
		rate += vp8enc.CoefficientBlockTokenRate(probs, 2, ctx, 0, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	return rate
}

func tokenUVContextArray(ctx *vp8enc.TokenContextPlanes) [4]uint8 {
	if ctx == nil {
		return [4]uint8{}
	}
	return [4]uint8{ctx.U[0], ctx.U[1], ctx.V[0], ctx.V[1]}
}

func macroblockCoefficientUVContextIndex(block int) (int, int) {
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
