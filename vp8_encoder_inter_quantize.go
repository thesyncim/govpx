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
	return quantizeBlockWithZbinAndActivity(coeff, quant, zbinOverQuant, zbinModeBoost, 0, qcoeff, dqcoeff)
}

func quantizeBlockWithZbinAndActivity(coeff *[16]int16, quant *vp8enc.BlockQuant, zbinOverQuant int, zbinModeBoost int, actZbinAdj int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if coeff == nil || quant == nil || qcoeff == nil || dqcoeff == nil {
		return 0
	}
	eob := -1
	zeroRun := 0
	for pos := range 16 {
		// DefaultZigZag1D is [16]uint8 with values 0..15; coeff/qcoeff/
		// dqcoeff/quant.Zbin/ZbinBoost are all [16]-sized. Mask rc and
		// zeroRun with 15 (pow2-1) so the compiler can elide the per-iter
		// bounds checks on the indexed loads/stores. zeroRun is clamped
		// to ≤ 15 by min(zeroRun+1, 15) below so the mask is a no-op.
		rc := int(vp8tables.DefaultZigZag1D[pos]) & 15
		z := int(coeff[rc])
		if z == 0 {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			zeroRun = min(zeroRun+1, 15)
			continue
		}

		// Branchless |z| via sign mask: sign is -1 when z<0, 0 otherwise.
		sign := z >> mvKernelSignShift
		x := (z ^ sign) - sign
		zbin := int(quant.Zbin[rc])
		zbin += int(quant.ZbinBoost[zeroRun&15])
		zbin += (int(quant.Dequant[1]) * (zbinOverQuant + zbinModeBoost + actZbinAdj)) >> 7
		if x < zbin {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			zeroRun = min(zeroRun+1, 15)
			continue
		}

		x += int(quant.Round[rc])
		y := ((((x * int(quant.Quant[rc])) >> 16) + x) * int(quant.QuantShift[rc])) >> 16
		y = (y ^ sign) - sign
		q := int16(y)
		qcoeff[rc] = q
		dqcoeff[rc] = q * quant.Dequant[rc]
		if y != 0 {
			eob = pos
			zeroRun = 0
		} else {
			zeroRun = min(zeroRun+1, 15)
		}
	}
	return eob + 1
}

func quantizeOptimizedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return quantizeOptimizedBlockWithRDZbinAndActivity(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, 0, zbinOverQuant, 0, 0, intra, coeff, quant, qcoeff, dqcoeff)
}

func quantizeOptimizedBlockWithRDZbinAndActivity(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, actZbinAdj int, rdZbinOverQuant int, rdMult int, rdDiv int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	eob := quantizeBlockWithZbinAndActivity(coeff, quant, zbinOverQuant, zbinModeBoost, actZbinAdj, qcoeff, dqcoeff)
	eob = optimizeQuantizedBlockWithRDConstants(coefProbs, qIndex, blockType, ctx, skipDC, rdZbinOverQuant, rdMult, rdDiv, intra, coeff, quant, qcoeff, eob)
	dequantizeQuantizedBlock(quant, qcoeff, dqcoeff)
	return eob
}

func quantizeEncodedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, intra bool, fastQuant bool, optimize bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return quantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, 0, zbinOverQuant, 0, 0, intra, fastQuant, optimize, coeff, quant, qcoeff, dqcoeff)
}

// quantizeEncodedBlockWithRDZbin keeps libvpx's Y2 split explicit: Y2 zbin
// thresholding uses zbin_over_quant/2, while the trellis optimizer scores with
// mb->rdmult computed from the full frame-level zbin_over_quant.
func quantizeEncodedBlockWithRDZbin(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, rdZbinOverQuant int, intra bool, fastQuant bool, optimize bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return quantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, 0, rdZbinOverQuant, 0, 0, intra, fastQuant, optimize, coeff, quant, qcoeff, dqcoeff)
}

func quantizeEncodedBlockWithRDZbinAndActivity(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, actZbinAdj int, rdZbinOverQuant int, rdMult int, rdDiv int, intra bool, fastQuant bool, optimize bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if fastQuant {
		return vp8enc.FastQuantizeBlock(coeff, quant, qcoeff, dqcoeff)
	}
	if optimize {
		eob := quantizeOptimizedBlockWithRDZbinAndActivity(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, actZbinAdj, rdZbinOverQuant, rdMult, rdDiv, intra, coeff, quant, qcoeff, dqcoeff)
		if blockType == 1 && skipDC == 0 {
			eob = resetLibvpxSmallSecondOrderCoefficients(quant, qcoeff, dqcoeff, eob)
		}
		return eob
	}
	return quantizeBlockWithZbinAndActivity(coeff, quant, zbinOverQuant, zbinModeBoost, actZbinAdj, qcoeff, dqcoeff)
}

func quantizeDecisionBlock(fastQuant bool, coeff *[16]int16, quant *vp8enc.BlockQuant, zbinOverQuant int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return quantizeDecisionBlockWithActivity(fastQuant, coeff, quant, zbinOverQuant, 0, qcoeff, dqcoeff)
}

func quantizeDecisionBlockWithActivity(fastQuant bool, coeff *[16]int16, quant *vp8enc.BlockQuant, zbinOverQuant int, actZbinAdj int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if fastQuant {
		return vp8enc.FastQuantizeBlock(coeff, quant, qcoeff, dqcoeff)
	}
	return quantizeBlockWithZbinAndActivity(coeff, quant, zbinOverQuant, 0, actZbinAdj, qcoeff, dqcoeff)
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
	return optimizeQuantizedBlockWithRDConstants(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, 0, 0, intra, coeff, quant, qcoeff, eob)
}

func optimizeQuantizedBlockWithRDConstants(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, rdMult int, rdDiv int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, eob int) int {
	if coeff == nil || quant == nil || qcoeff == nil || eob <= skipDC {
		return eob
	}
	// Three uint range checks fold the (x < 0 || x >= max) pairs into
	// one branch each; matches the form in vp8enc.CoefficientBlockTokenRate.
	if uint(blockType) >= uint(vp8tables.BlockTypes) ||
		uint(ctx) >= uint(vp8tables.PrevCoefContexts) ||
		uint(skipDC) > 1 {
		return eob
	}
	if coefProbs == nil {
		return eob
	}
	if eob > 16 {
		eob = 16
	}

	if rdMult <= 0 || rdDiv <= 0 {
		rdMult, rdDiv = libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	}
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
		// DefaultZigZag1D is [16]uint8 with cells in [0,16); qcoeff is
		// [16]int16. Mask rc with 15 (pow2-1) to elide the per-iter
		// bounds checks on both indexed loads.
		rc := int(vp8tables.DefaultZigZag1D[i&15]) & 15
		x := int(qcoeff[rc])
		if x != 0 {
			error0 := tokens[next][0].error
			error1 := tokens[next][1].error
			rate0 := tokens[next][0].rate
			rate1 := tokens[next][1].rate
			t0 := vp8enc.DCTValueToken(x)

			if next < 16 {
				// i+1 ∈ [1, 16) given i ≤ 14 from the loop range and
				// next < 16 guard. CoefBandsTable cells are ≤ 7;
				// (*coefProbs) outer dim is [4]. Pow2 masks elide BCE.
				band := int(vp8tables.CoefBandsTable[(i+1)&15])
				pt := int(vp8tables.PrevTokenClass[t0])
				p := (*coefProbs)[blockType&3][band&7][pt]
				rate0 += vp8enc.CoefficientTokenCost(p, int(tokens[next][0].token), blockType, band, pt)
				rate1 += vp8enc.CoefficientTokenCost(p, int(tokens[next][1].token), blockType, band, pt)
			}

			rdCost0 := libvpxRDCost(rdMult, rdDiv, rate0, error0)
			rdCost1 := libvpxRDCost(rdMult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = vp8enc.RDTrunc(rdMult, rate0)
				rdCost1 = vp8enc.RDTrunc(rdMult, rate1)
			}
			best := 0
			if rdCost1 < rdCost0 {
				best = 1
			}

			baseBits := vp8enc.DCTValueBaseCost(x)
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

			// Branchless |x| and |coeff[rc]|.
			xMask := x >> mvKernelSignShift
			absX := (x ^ xMask) - xMask
			cInt := int(coeff[rc])
			cMask := cInt >> mvKernelSignShift
			absC := (cInt ^ cMask) - cMask
			shortcut := absX*dq > absC && absX*dq < absC+dq
			xs := x
			sz := 0
			if shortcut {
				sz = x >> mvKernelSignShift // -1 if x<0, 0 otherwise
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
				t0 = vp8enc.DCTValueToken(xs)
				t1 = t0
			}

			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				if t0 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t0])
					p := (*coefProbs)[blockType][band][pt]
					rate0 += vp8enc.CoefficientTokenCost(p, int(tokens[next][0].token), blockType, band, pt)
				}
				if t1 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t1])
					p := (*coefProbs)[blockType][band][pt]
					rate1 += vp8enc.CoefficientTokenCost(p, int(tokens[next][1].token), blockType, band, pt)
				}
			}

			rdCost0 = libvpxRDCost(rdMult, rdDiv, rate0, error0)
			rdCost1 = libvpxRDCost(rdMult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = vp8enc.RDTrunc(rdMult, rate0)
				rdCost1 = vp8enc.RDTrunc(rdMult, rate1)
			}
			best = 0
			if rdCost1 < rdCost0 {
				best = 1
			}

			baseBits = vp8enc.DCTValueBaseCost(xs)

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
				tokens[next][0].rate += vp8enc.CoefficientTokenCost(p, t0Tok, blockType, band, 0)
				tokens[next][0].token = int8(vp8tables.ZeroToken)
			}
			if t1Tok != vp8tables.DCTEOBToken {
				tokens[next][1].rate += vp8enc.CoefficientTokenCost(p, t1Tok, blockType, band, 0)
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
	rate0 += vp8enc.CoefficientTokenCost(p, int(tokens[next][0].token), blockType, band, ctx)
	rate1 += vp8enc.CoefficientTokenCost(p, int(tokens[next][1].token), blockType, band, ctx)
	rdCost0 := libvpxRDCost(rdMult, rdDiv, rate0, error0)
	rdCost1 := libvpxRDCost(rdMult, rdDiv, rate1, error1)
	if rdCost0 == rdCost1 {
		rdCost0 = vp8enc.RDTrunc(rdMult, rate0)
		rdCost1 = vp8enc.RDTrunc(rdMult, rate1)
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
	// Hoist min(eob, 16) outside the loops so each iteration only has
	// one compare instead of two.
	limit := min(eob, 16)
	sum := 0
	for pos := range limit {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coef := int(qcoeff[rc]) * int(quant.Dequant[rc])
		// Branchless |coef| via sign-mask XOR.
		mask := coef >> mvKernelSignShift
		coef = (coef ^ mask) - mask
		sum += coef
		if sum >= 35 {
			return eob
		}
	}
	for pos := range limit {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		qcoeff[rc] = 0
		if dqcoeff != nil {
			dqcoeff[rc] = 0
		}
	}
	return 0
}
