package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

const (
	lastFrameZeroMVZbinBoost  = 6
	goldenAltZeroMVZbinBoost  = 12
	nonZeroInterModeZbinBoost = 4
	splitInterModeZbinBoost   = 0
	intraInterFrameZbinBoost  = 0
)

func interZbinModeBoost(mode *vp8enc.InterFrameMacroblockMode) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode >= vp8common.DCPred && mode.Mode <= vp8common.BPred {
		return intraInterFrameZbinBoost
	}
	switch mode.Mode {
	case vp8common.ZeroMV:
		if mode.RefFrame == vp8common.LastFrame {
			return lastFrameZeroMVZbinBoost
		}
		return goldenAltZeroMVZbinBoost
	case vp8common.SplitMV:
		return splitInterModeZbinBoost
	default:
		return nonZeroInterModeZbinBoost
	}
}

func quantizeBlockWithZbin(coeff *[16]int16, quant *vp8enc.BlockQuant, zbinOverQuant int, zbinModeBoost int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if coeff == nil || quant == nil || qcoeff == nil || dqcoeff == nil {
		return 0
	}
	eob := -1
	zeroRun := 0
	for pos := range 16 {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		z := int(coeff[rc])
		if z == 0 {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			if zeroRun < len(quant.ZbinBoost)-1 {
				zeroRun++
			}
			continue
		}

		x := z
		if x < 0 {
			x = -x
		}
		zbin := int(quant.Zbin[rc])
		zbin += int(quant.ZbinBoost[zeroRun])
		zbin += (int(quant.Dequant[1]) * (zbinOverQuant + zbinModeBoost)) >> 7
		if x < zbin {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			if zeroRun < len(quant.ZbinBoost)-1 {
				zeroRun++
			}
			continue
		}

		x += int(quant.Round[rc])
		y := ((((x * int(quant.Quant[rc])) >> 16) + x) * int(quant.QuantShift[rc])) >> 16
		if z < 0 {
			y = -y
		}
		q := int16(y)
		qcoeff[rc] = q
		dqcoeff[rc] = q * quant.Dequant[rc]
		if y != 0 {
			eob = pos
			zeroRun = 0
		} else if zeroRun < len(quant.ZbinBoost)-1 {
			zeroRun++
		}
	}
	return eob + 1
}

func quantizeOptimizedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return quantizeOptimizedBlockWithRDZbin(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, zbinOverQuant, intra, coeff, quant, qcoeff, dqcoeff)
}

func quantizeOptimizedBlockWithRDZbin(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, rdZbinOverQuant int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	eob := quantizeBlockWithZbin(coeff, quant, zbinOverQuant, zbinModeBoost, qcoeff, dqcoeff)
	eob = optimizeQuantizedBlock(coefProbs, qIndex, blockType, ctx, skipDC, rdZbinOverQuant, intra, coeff, quant, qcoeff, eob)
	dequantizeQuantizedBlock(quant, qcoeff, dqcoeff)
	return eob
}

func quantizeEncodedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, intra bool, fastQuant bool, optimize bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return quantizeEncodedBlockWithRDZbin(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, zbinOverQuant, intra, fastQuant, optimize, coeff, quant, qcoeff, dqcoeff)
}

// quantizeEncodedBlockWithRDZbin keeps libvpx's Y2 split explicit: Y2 zbin
// thresholding uses zbin_over_quant/2, while the trellis optimizer scores with
// mb->rdmult computed from the full frame-level zbin_over_quant.
func quantizeEncodedBlockWithRDZbin(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, rdZbinOverQuant int, intra bool, fastQuant bool, optimize bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if fastQuant {
		return vp8enc.FastQuantizeBlock(coeff, quant, qcoeff, dqcoeff)
	}
	if optimize {
		eob := quantizeOptimizedBlockWithRDZbin(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, rdZbinOverQuant, intra, coeff, quant, qcoeff, dqcoeff)
		if blockType == 1 && skipDC == 0 {
			eob = resetLibvpxSmallSecondOrderCoefficients(quant, qcoeff, dqcoeff, eob)
		}
		return eob
	}
	return quantizeBlockWithZbin(coeff, quant, zbinOverQuant, zbinModeBoost, qcoeff, dqcoeff)
}

func quantizeDecisionBlock(fastQuant bool, coeff *[16]int16, quant *vp8enc.BlockQuant, zbinOverQuant int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if fastQuant {
		return vp8enc.FastQuantizeBlock(coeff, quant, qcoeff, dqcoeff)
	}
	return quantizeBlockWithZbin(coeff, quant, zbinOverQuant, 0, qcoeff, dqcoeff)
}

func dequantizeQuantizedBlock(quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) {
	if quant == nil || qcoeff == nil || dqcoeff == nil {
		return
	}
	for i := range 16 {
		dqcoeff[i] = qcoeff[i] * quant.Dequant[i]
	}
}

// optimizeQuantizedBlock ports libvpx v1.16.0 vp8/encoder/encodemb.c optimize_b.
// It walks the quantized block from eob-1 down to skipDC, builds a 2-state
// Viterbi trellis exploring (keep current value) vs (shift |x| toward 0 when
// the dequant boundary allows), scores transitions with libvpx's token_costs
// subtree elision, and applies the path that minimizes the libvpx RDCOST. Tied
// RDCOSTs use the libvpx RDTRUNC tie-break.
func optimizeQuantizedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, eob int) int {
	if coeff == nil || quant == nil || qcoeff == nil || eob <= skipDC {
		return eob
	}
	if blockType < 0 || blockType >= vp8tables.BlockTypes || ctx < 0 || ctx >= vp8tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return eob
	}
	if coefProbs == nil {
		return eob
	}
	if eob > 16 {
		eob = 16
	}

	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	rdMult *= blockPlaneRDMultiplier(blockType)
	if intra {
		rdMult = (rdMult * 9) >> 4
	}

	type tokenState struct {
		rate  int
		error int
		next  int8
		token int8
		qc    int16
	}
	var tokens [17][2]tokenState
	var bestMask [2]uint32

	tokens[eob][0] = tokenState{next: 16, token: int8(vp8tables.DCTEOBToken)}
	tokens[eob][1] = tokens[eob][0]
	next := eob

	for i := eob - 1; i >= skipDC; i-- {
		rc := int(vp8tables.DefaultZigZag1D[i])
		x := int(qcoeff[rc])
		if x != 0 {
			error0 := tokens[next][0].error
			error1 := tokens[next][1].error
			rate0 := tokens[next][0].rate
			rate1 := tokens[next][1].rate
			t0 := dctValueToken(x)

			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				pt := int(vp8tables.PrevTokenClass[t0])
				p := (*coefProbs)[blockType][band][pt]
				rate0 += coefficientTokenCost(p, int(tokens[next][0].token), blockType, band, pt)
				rate1 += coefficientTokenCost(p, int(tokens[next][1].token), blockType, band, pt)
			}

			rdCost0 := libvpxRDCost(rdMult, rdDiv, rate0, error0)
			rdCost1 := libvpxRDCost(rdMult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = libvpxRDTrunc(rdMult, rate0)
				rdCost1 = libvpxRDTrunc(rdMult, rate1)
			}
			best := 0
			if rdCost1 < rdCost0 {
				best = 1
			}

			baseBits := dctValueBaseCost(x)
			dq := int(quant.Dequant[rc])
			dx := x*dq - int(coeff[rc])
			d2 := dx * dx

			if best == 1 {
				tokens[i][0].rate = baseBits + rate1
				tokens[i][0].error = d2 + error1
			} else {
				tokens[i][0].rate = baseBits + rate0
				tokens[i][0].error = d2 + error0
			}
			tokens[i][0].next = int8(next)
			tokens[i][0].token = int8(t0)
			tokens[i][0].qc = int16(x)
			bestMask[0] |= uint32(best) << uint(i)

			rate0 = tokens[next][0].rate
			rate1 = tokens[next][1].rate

			absX := x
			if absX < 0 {
				absX = -absX
			}
			absC := int(coeff[rc])
			if absC < 0 {
				absC = -absC
			}
			shortcut := absX*dq > absC && absX*dq < absC+dq
			xs := x
			sz := 0
			if shortcut {
				if x < 0 {
					sz = -1
				}
				xs -= 2*sz + 1
			}

			var t1 int
			if xs == 0 {
				if int(tokens[next][0].token) == vp8tables.DCTEOBToken {
					t0 = vp8tables.DCTEOBToken
				} else {
					t0 = vp8tables.ZeroToken
				}
				if int(tokens[next][1].token) == vp8tables.DCTEOBToken {
					t1 = vp8tables.DCTEOBToken
				} else {
					t1 = vp8tables.ZeroToken
				}
			} else {
				t0 = dctValueToken(xs)
				t1 = t0
			}

			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				if t0 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t0])
					p := (*coefProbs)[blockType][band][pt]
					rate0 += coefficientTokenCost(p, int(tokens[next][0].token), blockType, band, pt)
				}
				if t1 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t1])
					p := (*coefProbs)[blockType][band][pt]
					rate1 += coefficientTokenCost(p, int(tokens[next][1].token), blockType, band, pt)
				}
			}

			rdCost0 = libvpxRDCost(rdMult, rdDiv, rate0, error0)
			rdCost1 = libvpxRDCost(rdMult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = libvpxRDTrunc(rdMult, rate0)
				rdCost1 = libvpxRDTrunc(rdMult, rate1)
			}
			best = 0
			if rdCost1 < rdCost0 {
				best = 1
			}

			baseBits = dctValueBaseCost(xs)

			d2s := d2
			if shortcut {
				dxs := dx - ((dq + sz) ^ sz)
				d2s = dxs * dxs
			}

			if best == 1 {
				tokens[i][1].rate = baseBits + rate1
				tokens[i][1].error = d2s + error1
				tokens[i][1].token = int8(t1)
			} else {
				tokens[i][1].rate = baseBits + rate0
				tokens[i][1].error = d2s + error0
				tokens[i][1].token = int8(t0)
			}
			tokens[i][1].next = int8(next)
			tokens[i][1].qc = int16(xs)
			bestMask[1] |= uint32(best) << uint(i)
			next = i
		} else {
			band := int(vp8tables.CoefBandsTable[i+1])
			p := (*coefProbs)[blockType][band][0]
			t0Tok := int(tokens[next][0].token)
			t1Tok := int(tokens[next][1].token)
			if t0Tok != vp8tables.DCTEOBToken {
				tokens[next][0].rate += coefficientTokenCost(p, t0Tok, blockType, band, 0)
				tokens[next][0].token = int8(vp8tables.ZeroToken)
			}
			if t1Tok != vp8tables.DCTEOBToken {
				tokens[next][1].rate += coefficientTokenCost(p, t1Tok, blockType, band, 0)
				tokens[next][1].token = int8(vp8tables.ZeroToken)
			}
		}
	}

	band := int(vp8tables.CoefBandsTable[skipDC])
	rate0 := tokens[next][0].rate
	rate1 := tokens[next][1].rate
	error0 := tokens[next][0].error
	error1 := tokens[next][1].error
	p := (*coefProbs)[blockType][band][ctx]
	rate0 += coefficientTokenCost(p, int(tokens[next][0].token), blockType, band, ctx)
	rate1 += coefficientTokenCost(p, int(tokens[next][1].token), blockType, band, ctx)
	rdCost0 := libvpxRDCost(rdMult, rdDiv, rate0, error0)
	rdCost1 := libvpxRDCost(rdMult, rdDiv, rate1, error1)
	if rdCost0 == rdCost1 {
		rdCost0 = libvpxRDTrunc(rdMult, rate0)
		rdCost1 = libvpxRDTrunc(rdMult, rate1)
	}
	best := 0
	if rdCost1 < rdCost0 {
		best = 1
	}

	finalEOB := skipDC - 1
	for i := next; i < eob; {
		x := tokens[i][best].qc
		if x != 0 {
			finalEOB = i
		}
		rc := int(vp8tables.DefaultZigZag1D[i])
		qcoeff[rc] = x
		nextI := int(tokens[i][best].next)
		best = int((bestMask[best] >> uint(i)) & 1)
		i = nextI
	}
	return finalEOB + 1
}

// libvpxRDTrunc mirrors the encodemb.c RDTRUNC macro used to break ties when
// two trellis paths have equal RDCOST.
func libvpxRDTrunc(rdMult int, rate int) int {
	return (128 + rate*rdMult) & 0xFF
}

// dctValueToken returns the libvpx coefficient-token classification for value x
// (mirrors the dct_value_tokens table indexed by signed value).
func dctValueToken(x int) int {
	abs := x
	if abs < 0 {
		abs = -abs
	}
	if abs == 0 {
		return vp8tables.ZeroToken
	}
	token, _, ok := coefficientTokenMagnitude(abs)
	if !ok {
		return vp8tables.ZeroToken
	}
	return token
}

// dctValueBaseCost mirrors libvpx's dct_value_cost table: extra bits cost plus
// sign bit cost for value x. The token-tree cost is added separately by the
// trellis using band/context-specific token costs.
func dctValueBaseCost(x int) int {
	if x == 0 {
		return 0
	}
	abs := x
	if abs < 0 {
		abs = -abs
	}
	token, _, ok := coefficientTokenMagnitude(abs)
	if !ok {
		return maxInt() / 4
	}
	cost := 0
	if x < 0 {
		cost += boolBitCost(128, 1)
	} else {
		cost += boolBitCost(128, 0)
	}
	cost += coefficientExtraBitsRate(token, abs)
	return cost
}

// Ported from libvpx v1.16.0 vp8/encoder/encodemb.c
// check_reset_2nd_coeffs. Very small Y2 residuals inverse-transform to a zero
// pixel delta, so libvpx drops the whole second-order block after optimization.
func resetLibvpxSmallSecondOrderCoefficients(quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16, eob int) int {
	if quant == nil || qcoeff == nil || eob <= 0 {
		return eob
	}
	if quant.Dequant[0] >= 35 && quant.Dequant[1] >= 35 {
		return eob
	}
	sum := 0
	for pos := 0; pos < eob && pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coef := int(qcoeff[rc]) * int(quant.Dequant[rc])
		if coef < 0 {
			coef = -coef
		}
		sum += coef
		if sum >= 35 {
			return eob
		}
	}
	for pos := 0; pos < eob && pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		qcoeff[rc] = 0
		if dqcoeff != nil {
			dqcoeff[rc] = 0
		}
	}
	return 0
}
