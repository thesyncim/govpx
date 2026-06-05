package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// fullrd_trellis.go ports libvpx v1.16.0 vp9_optimize_b
// (vp9/encoder/vp9_encodemb.c:66-329) — the backward/forward dynamic
// program over the scan order that, for each non-zero quantized coefficient,
// greedily picks between keeping the quantized level x and reducing it by one
// (toward zero) to minimise rate + lambda*distortion, while also choosing the
// best end-of-block position. It is the TRELLIS coefficient optimisation
// block_rd_txfm runs (vp9_rdopt.c:793, gated do_trellis_opt → ENABLE_TRELLIS_OPT
// for the RT full-RD mode-selection path) right after vp9_xform_quant and
// before dist_block / cost_coeffs.
//
// All RD/token primitives are reused verbatim from this package:
//   - the per-coefficient base cost (extra bits + sign) and its token come
//     from CoeffTokenExtraCost, the oracle-validated equivalent of libvpx
//     vp9_get_token_cost (vp9/encoder/vp9_tokenize.h:113, pinned by
//     TestVP9CoeffValueCostMatchesLibvpxOracle).
//   - the token-tree cost (*token_costs)[tree_sel][ctx][token] comes from
//     CoeffTreeTokenCost over the frame's coef-probs model — the same
//     expansion fill_token_costs performs (vp9_rd.c:135-152, pinned by
//     TestVP9CostTokensMatchesLibvpxOracle).
//   - PtEnergyClass mirrors vp9_pt_energy_class.
//   - GetCoefContext mirrors get_coef_context (vp9_scan.h:35).
//   - the RDCOST macro is expanded in int64 (rdTrellisCost), matching
//     vp9/encoder/vp9_rd.h:29-30 with VP9_PROB_COST_SHIFT=9, RDDIV_BITS=7.
//
// No new constants/tables are introduced; the magic numbers
// (plane_rd_mult, CAT6_MIN_VAL, the shifts) are taken verbatim from libvpx.

// VP9TrellisCoefModel is the per-(plane_type,ref) coefficient-probs model the
// trellis reads, i.e. cm->fc->coef_probs[tx_size][plane_type][ref], laid out
// as [band][ctx][UNCONSTRAINED_NODES] just like CoefProbsModel's leaf.
type VP9TrellisCoefModel = *[vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8

// trellisPlaneRDMult mirrors libvpx's plane_rd_mult[REF_TYPES][PLANE_TYPES]
// (vp9/encoder/vp9_encodemb.c:57-60). Index [ref][plane_type].
var trellisPlaneRDMult = [2][2]int64{
	{10, 6},
	{8, 5},
}

// trellisCat6MinVal mirrors CAT6_MIN_VAL (vp9/common/vp9_entropy.h:51).
const trellisCat6MinVal = 67

// rdTrellisCost expands libvpx's RDCOST macro (vp9/encoder/vp9_rd.h:29-30)
// fully in int64: ROUND_POWER_OF_TWO(R*RM, VP9_PROB_COST_SHIFT) + (D << DM).
// The trellis carries rate/distortion accumulators that exceed int32, so the
// computation must stay 64-bit (unlike the per-block RDCost helper).
func rdTrellisCost(rdmult int64, rddiv uint, rate, dist int64) int64 {
	const probCostShift = 9 // VP9_PROB_COST_SHIFT
	return ((rate*rdmult + (1 << (probCostShift - 1))) >> probCostShift) +
		(dist << rddiv)
}

// rightShiftPossiblyNegative mirrors libvpx's RIGHT_SHIFT_POSSIBLY_NEGATIVE
// (vp9/encoder/vp9_encodemb.c:62-64): an arithmetic-toward-zero right shift
// that, unlike Go's >> on negative ints (which rounds toward -inf), rounds
// toward zero exactly as the C macro does.
func rightShiftPossiblyNegative(num int64, shift uint) int64 {
	if num >= 0 {
		return num >> shift
	}
	return -((-num) >> shift)
}

// VP9OptimizeB ports vp9_optimize_b verbatim for the 8-bit profile. It mutates
// qcoeff/dqcoeff in place (raster order, indexed by scan[]) and returns the
// optimised final_eob. coeff is the pre-quant forward-transform output
// (tran_low_t in libvpx; govpx int16 scratch). eob is the pre-trellis eob from
// the quantizer. tokenCache is caller-owned 1024-byte scratch.
//
// Parameters mirror the libvpx call site block_rd_txfm → vp9_optimize_b:
//
//	plane       : 0 for Y (plane_type 0), 1/2 for UV (plane_type 1)
//	ref         : is_inter_block(mi) (1 inter, 0 intra)
//	txSize      : the transform size
//	ctx         : combine_entropy_contexts(t_left[blk_row], t_above[blk_col])
//	dequant     : pd->dequant ([DC, AC])
//	scan, nb    : so->scan, so->neighbors for (txSize, plane_type, block)
//	coefModel   : cm->fc->coef_probs[txSize][plane_type][ref]
//	rdmult      : mb->rdmult (x->rdmult)
//	rddiv       : mb->rddiv (== RDDIV_BITS == 7 on the RD path)
//	sharpness   : mb->sharpness (oxcf.sharpness)
//	segmentID   : mbmi->segment_id
//
// libvpx: vp9/encoder/vp9_encodemb.c:66-328.
func VP9OptimizeB(plane, ref int, txSize common.TxSize, ctx int,
	coeff, qcoeff, dqcoeff []int16, eob int, dequant [2]int16,
	scan, nb []int16, coefModel VP9TrellisCoefModel,
	rdmult int64, rddiv uint, sharpness, segmentID int,
	tokenCache *[1024]uint8,
) int {
	planeType := 0
	if plane != 0 {
		planeType = 1
	}
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if coefModel == nil || tokenCache == nil ||
		len(coeff) < maxEob || len(qcoeff) < maxEob || len(dqcoeff) < maxEob ||
		len(scan) < maxEob || len(nb) < common.MaxNeighbors*maxEob ||
		eob < 0 || eob > maxEob {
		return eob
	}

	// const int default_eob = 16 << (tx_size << 1); (== maxEob)
	defaultEob := maxEob
	// const int shift = (tx_size == TX_32X32);
	shift := uint(0)
	if txSize == common.Tx32x32 {
		shift = 1
	}
	bandTranslate := vp9dec.BandTranslateForTxSize(txSize)
	if len(bandTranslate) < defaultEob {
		return eob
	}

	// const int64_t rdadj = (int64_t)mb->rdmult * plane_rd_mult[ref][plane_type];
	rdadj := rdmult * trellisPlaneRDMult[ref&1][planeType]
	// const int64_t rdmult = (sharpness == 0 ? rdadj >> 1
	//                                        : (rdadj * (8 - sharpness + segment_id)) >> 4);
	var rdmultLocal int64
	if sharpness == 0 {
		rdmultLocal = rdadj >> 1
	} else {
		rdmultLocal = (rdadj * int64(8-sharpness+segmentID)) >> 4
	}

	// token_cache[rc] = vp9_pt_energy_class[vp9_get_token(qcoeff[rc])] for i<eob.
	for i := 0; i < eob; i++ {
		rc := int(scan[i])
		tokenCache[rc] = PtEnergyClass[trellisGetToken(int(qcoeff[rc]))]
	}
	finalEob := 0
	countHighValuesAfterEob := 0

	var accuRate int64 // accu_rate
	// accu_error initialised to the worst error for the largest transform so it
	// never goes negative (vp9_encodemb.c:109-111).
	accuError := int64(1) << 50
	var bestBlockRDCost int64
	xPrev := 1
	var beforeBestEobQC int
	var beforeBestEobDQC int

	// Initial RD cost. token_costs_cur = token_costs + band_translate[0].
	band0 := int(bandTranslate[0])
	rate0 := int64(trellisTokenCost(coefModel, band0, ctx, EobToken, false))
	bestBlockRDCost = rdTrellisCost(rdmultLocal, rddiv, rate0, accuError)

	for i := 0; i < eob; i++ {
		rc := int(scan[i])
		x := int(qcoeff[rc])
		bandCur := int(bandTranslate[i])
		ctxCur := ctx
		if i != 0 {
			ctxCur = vp9dec.GetCoefContext(nb, tokenCache, i)
		}
		tokenTreeSelCur := xPrev == 0

		if x == 0 { // No need to search.
			token := trellisGetToken(x)
			r0 := int64(trellisTokenCost(coefModel, bandCur, ctxCur, token,
				tokenTreeSelCur))
			accuRate += r0
			xPrev = 0
			// Note: accu_error does not change.
			continue
		}

		dqv := int(dequant[1])
		if rc == 0 {
			dqv = int(dequant[0])
		}
		// Distortion for quantizing to 0.
		diffForZero := int64(0-int(coeff[rc])) * int64(int(1)<<shift)
		distortionForZero := diffForZero * diffForZero

		// Distortion for the first candidate (keep x).
		diff0 := int64(int(dqcoeff[rc])-int(coeff[rc])) * int64(int(1)<<shift)
		distortion0 := diff0 * diff0

		// Second candidate: |x1| = |x| - 1.
		sign := 0
		if x < 0 {
			sign = -1
		}
		x1 := x - 2*sign - 1
		var distortion1 int64
		if x1 != 0 {
			dqvStep := int64(dqv)
			diffStep := (dqvStep + int64(sign)) ^ int64(sign)
			diff1 := diff0 - diffStep
			distortion1 = diff1 * diff1
		} else {
			distortion1 = distortionForZero
		}

		// Token-tree base costs for the two candidates.
		t0, baseBits0 := trellisGetTokenCost(x)
		t1, baseBits1 := trellisGetTokenCost(x1)
		r0 := int64(baseBits0) + int64(trellisTokenCost(coefModel, bandCur, ctxCur,
			t0, tokenTreeSelCur))
		r1 := int64(baseBits1) + int64(trellisTokenCost(coefModel, bandCur, ctxCur,
			t1, tokenTreeSelCur))

		// RD cost effect on the next coeff for the two candidates.
		var nextBits0, nextBits1 int64
		var nextEobBits0, nextEobBits1 int64
		if i < defaultEob-1 {
			bandNext := int(bandTranslate[i+1])
			tokenNext := EobToken
			if i+1 != eob {
				tokenNext = trellisGetToken(int(qcoeff[int(scan[i+1])]))
			}
			tokenCache[rc] = PtEnergyClass[t0]
			ctxNext := vp9dec.GetCoefContext(nb, tokenCache, i+1)
			tokenTreeSelNext := x == 0
			nextBits0 = int64(trellisTokenCost(coefModel, bandNext, ctxNext,
				tokenNext, tokenTreeSelNext))
			nextEobBits0 = int64(trellisTokenCost(coefModel, bandNext, ctxNext,
				EobToken, tokenTreeSelNext))
			tokenCache[rc] = PtEnergyClass[t1]
			ctxNext = vp9dec.GetCoefContext(nb, tokenCache, i+1)
			tokenTreeSelNext = x1 == 0
			nextBits1 = int64(trellisTokenCost(coefModel, bandNext, ctxNext,
				tokenNext, tokenTreeSelNext))
			if x1 != 0 {
				nextEobBits1 = int64(trellisTokenCost(coefModel, bandNext, ctxNext,
					EobToken, tokenTreeSelNext))
			}
		}

		// Compare total RD costs for the two candidates.
		rdCost0 := rdTrellisCost(rdmultLocal, rddiv, r0+nextBits0, distortion0)
		rdCost1 := rdTrellisCost(rdmultLocal, rddiv, r1+nextBits1, distortion1)
		rdcostBetterForX1 := 0
		if rdCost1 < rdCost0 {
			rdcostBetterForX1 = 1
		}
		eobCost0 := rdTrellisCost(rdmultLocal, rddiv,
			accuRate+r0+nextEobBits0, accuError+distortion0-distortionForZero)
		eobCost1 := eobCost0
		eobRdcostBetterForX1 := 0
		if x1 != 0 {
			eobCost1 = rdTrellisCost(rdmultLocal, rddiv,
				accuRate+r1+nextEobBits1,
				accuError+distortion1-distortionForZero)
			if eobCost1 < eobCost0 {
				eobRdcostBetterForX1 = 1
			}
		}

		// Two candidate de-quantized values.
		dqc0 := int(dqcoeff[rc])
		dqc1 := 0
		if rdcostBetterForX1+eobRdcostBetterForX1 != 0 {
			if x1 != 0 {
				dqc1 = int(rightShiftPossiblyNegative(int64(x1*dqv), shift))
			} else {
				dqc1 = 0
			}
		}

		// Pick and record the better quantized / de-quantized values.
		if rdcostBetterForX1 != 0 {
			qcoeff[rc] = int16(x1)
			dqcoeff[rc] = int16(dqc1)
			accuRate += r1
			accuError += distortion1 - distortionForZero
			tokenCache[rc] = PtEnergyClass[t1]
		} else {
			accuRate += r0
			accuError += distortion0 - distortionForZero
			tokenCache[rc] = PtEnergyClass[t0]
		}
		if sharpness > 0 && trellisAbs(int(qcoeff[rc])) > 1 {
			countHighValuesAfterEob++
		}
		xPrev = int(qcoeff[rc]) // Update based on selected quantized value.

		useX1 := x1 != 0 && eobRdcostBetterForX1 != 0
		bestEobCostCur := eobCost0
		if useX1 {
			bestEobCostCur = eobCost1
		}

		// Determine whether to move the eob position to i+1.
		if bestEobCostCur < bestBlockRDCost {
			bestBlockRDCost = bestEobCostCur
			finalEob = i + 1
			countHighValuesAfterEob = 0
			if useX1 {
				beforeBestEobQC = x1
				beforeBestEobDQC = dqc1
			} else {
				beforeBestEobQC = x
				beforeBestEobDQC = dqc0
			}
		}
	}

	if countHighValuesAfterEob > 0 {
		finalEob = eob - 1
		for ; finalEob >= 0; finalEob-- {
			rc := int(scan[finalEob])
			if qcoeff[rc] != 0 {
				break
			}
		}
		finalEob++
	} else {
		if finalEob > 0 {
			i := finalEob - 1
			rc := int(scan[i])
			qcoeff[rc] = int16(beforeBestEobQC)
			dqcoeff[rc] = int16(beforeBestEobDQC)
		}
		for i := finalEob; i < eob; i++ {
			rc := int(scan[i])
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
		}
	}
	return finalEob
}

// trellisGetToken mirrors vp9_get_token (vp9/encoder/vp9_tokenize.h:108-111):
// the token class for a signed value, with |v| >= CAT6_MIN_VAL → CATEGORY6.
func trellisGetToken(v int) int {
	if v >= trellisCat6MinVal || v <= -trellisCat6MinVal {
		return Category6Tok
	}
	tok, _ := CoeffTokenExtraCost(trellisAbs(v), 0)
	return tok
}

// trellisGetTokenCost mirrors vp9_get_token_cost
// (vp9/encoder/vp9_tokenize.h:113-124): returns (token, base_bits) for a
// signed value, where base_bits is the extra-bit + sign cost.
// CoeffTokenExtraCost is the oracle-validated equivalent for the 8-bit profile
// (TestVP9CoeffValueCostMatchesLibvpxOracle).
func trellisGetTokenCost(v int) (token int, baseBits int) {
	sign := 0
	a := v
	if a < 0 {
		a = -a
		sign = 1
	}
	return CoeffTokenExtraCost(a, sign)
}

// trellisTokenCost looks up (*token_costs)[tree_sel][ctx][token] for the given
// band/ctx, expanding the frame's coef-probs model via CoeffTreeTokenCost — the
// same value fill_token_costs precomputes (vp9_rd.c:135-152). tree_sel==true
// selects the skip-EOB variant (vp9_cost_tokens_skip).
func trellisTokenCost(coefModel VP9TrellisCoefModel, band, ctx, token int,
	skipEOB bool,
) int {
	if band < 0 || band >= vp9dec.CoefBands || ctx < 0 || ctx >= vp9dec.CoefContexts {
		return 0
	}
	return CoeffTreeTokenCost(coefModel[band][ctx][:], skipEOB, token)
}

// trellisAbs is a tiny int abs helper (avoids importing math for ints).
func trellisAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
