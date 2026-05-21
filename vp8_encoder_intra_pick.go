package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func predictBestKeyFrameIntraMode(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, fastQuant bool) (vp8enc.KeyFrameMacroblockMode, int, bool) {
	return predictBestKeyFrameIntraModeWithRDConstants(src, qIndex, 0, 0, mbRow, mbCol, above, left, aboveTok, leftTok, quant, pred, scratch, fastQuant, 0, 0)
}

func predictBestKeyFrameIntraModeWithRDConstants(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, actZbinAdj int, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, fastQuant bool, rdMult int, rdDiv int) (vp8enc.KeyFrameMacroblockMode, int, bool) {
	coefProbs := &vp8tables.DefaultCoefProbs
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
	}
	wholeY, wholeUV, wholeYRate, wholeYDist, wholeUVRate, wholeUVDist, ok := predictBestWholeBlockIntraModeRDWithProbsAndRDConstants(src, qIndex, zbinOverQuant, actZbinAdj, true, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, nil, nil, fastQuant, rdMult, rdDiv)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, 0, false
	}
	wholeRate := wholeYRate + wholeUVRate
	wholeDist := wholeYDist + wholeUVDist
	wholeCost := vp8enc.RDCost(rdMult, rdDiv, wholeRate, wholeDist)
	wholeYCost := vp8enc.RDCost(rdMult, rdDiv, wholeYRate, wholeYDist)
	best := vp8enc.KeyFrameMacroblockMode{YMode: wholeY, UVMode: wholeUV}
	bModes, bRate, bDist, ok := predictBestBPredLumaModeRDWithRDConstants(src, qIndex, zbinOverQuant, actZbinAdj, true, mbRow, mbCol, above, left, aboveTok, leftTok, quant, pred, scratch, wholeYCost, coefProbs, nil, fastQuant, rdMult, rdDiv)
	if !ok {
		return best, wholeRate, true
	}
	bUV, bUVRate, bUVDist, ok := predictBestIntraChromaModeRDWithProbsAndRDConstants(src, qIndex, zbinOverQuant, actZbinAdj, true, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, &vp8tables.DefaultCoefProbs, nil, fastQuant, rdMult, rdDiv)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, 0, false
	}
	bPredRate := bRate + bUVRate + intraYModeRate(true, vp8common.BPred)
	bPredCost := vp8enc.RDCost(rdMult, rdDiv, bPredRate, bDist+bUVDist)
	if bPredCost < wholeCost {
		best = vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred, UVMode: bUV, BModes: bModes}
		return best, bPredRate, true
	}
	return best, wholeRate, true
}

// predictBestKeyFrameIntraModeFastWithRDConstants mirrors libvpx pickinter.c
// vp8_pick_intra_mode (the fast keyframe intra picker libvpx selects when
// `cpi->sf.RD == 0` or `compressor_speed == 2 (realtime)`). Unlike the RD
// picker it scores Y MB-level and B_PRED sub-modes in the pixel domain
// instead of running DCT/quantize/token-cost per candidate, and B_PRED
// sub-blocks iterate only the four fast candidates {DC, TM, VE, HE} rather
// than all ten intra4x4 modes. The chroma mode is picked once independently
// (matching libvpx's pick_intra_mbuv_mode call before the Y loop).
func predictBestKeyFrameIntraModeFastWithRDConstants(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, fastQuant bool, rdMult int, rdDiv int) (vp8enc.KeyFrameMacroblockMode, int, bool) {
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
	}
	bestUVMode, _, ok := pickFastIntraChromaMode(src, mbRow, mbCol, pred, scratch)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, 0, false
	}
	if !predictAnalysisChroma(pred, mbRow, mbCol, bestUVMode, scratch) {
		return vp8enc.KeyFrameMacroblockMode{}, 0, false
	}

	bestYMode, bestYRate, bestY16RD, ok := pickFastWholeBlockIntraYModeWithRDConstants(src, qIndex, mbRow, mbCol, pred, scratch, rdMult, rdDiv)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, 0, false
	}

	whole := vp8enc.KeyFrameMacroblockMode{YMode: bestYMode, UVMode: bestUVMode}
	// vp8_pick_intra_mode calls pick_intra_mbuv_mode for the accepted UV
	// mode, but returns only the Y-mode/B_PRED rate through *rate.
	wholeRate := bestYRate

	bModes, bRate, bRD, ok := pickFastBPredLumaModeKFWithRDConstants(src, qIndex, zbinOverQuant, mbRow, mbCol, above, left, quant, pred, scratch, fastQuant, rdMult, rdDiv)
	if !ok {
		// pickFastBPredLumaModeKFWithRDConstants mutates pred.Y as it walks blocks; on
		// failure the analysis image may be partially overwritten. Fall back
		// to whole-block by re-running its prediction so the analysis frame
		// reflects the chosen mode for downstream coefficient construction.
		mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: bestYMode, UVMode: bestUVMode}
		predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch)
		return whole, wholeRate, true
	}
	if bRD < bestY16RD {
		return vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred, UVMode: bestUVMode, BModes: bModes}, bRate + intraYModeRate(true, vp8common.BPred), true
	}
	// BPred lost: walk back the analysis frame to whole-block prediction.
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: bestYMode, UVMode: bestUVMode}
	predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch)
	return whole, wholeRate, true
}

// pickFastWholeBlockIntraYModeWithRDConstants iterates wholeBlockIntraYModeCandidates and
// scores each via pixel-domain luma variance against the source. Mirrors the
// {DC,V,H,TM} loop in vp8_pick_intra_mode (pickinter.c). Returns the picked
// mode, its rate cost (mbmode_cost[KEY_FRAME][mode]), and the winning RDCOST
// — libvpx compares this RDCOST against the 4x4 BPred RDCOST when choosing
// between whole-block and split modes, so callers do the same.
func pickFastWholeBlockIntraYModeWithRDConstants(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, rdMult int, rdDiv int) (vp8common.MBPredictionMode, int, int, bool) {
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstants(qIndex)
	}
	bestMode := vp8common.DCPred
	bestRate := 0
	bestRD := 0
	for i, yMode := range wholeBlockIntraYModeCandidates {
		mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: yMode, UVMode: vp8common.DCPred}
		if !predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch) {
			return 0, 0, 0, false
		}
		dist, _ := vp8enc.MacroblockLumaVarianceSSE(src, pred, mbRow, mbCol)
		rate := intraYModeRate(true, yMode)
		cost := vp8enc.RDCost(rdMult, rdDiv, rate, dist)
		if i == 0 || cost < bestRD {
			bestMode = yMode
			bestRate = rate
			bestRD = cost
		}
	}
	return bestMode, bestRate, bestRD, true
}

// pickFastIntraChromaMode iterates wholeBlockIntraUVModeCandidates and scores
// each by pure SSE — libvpx's pick_intra_mbuv_mode (pickinter.c) intentionally
// drops the rate term and picks by pred_error alone (no RDCOST). The returned
// rate mirrors intraUVModeRate(picked) for callers that need it, but libvpx's
// fast keyframe picker does not include that UV rate in its returned MB rate.
func pickFastIntraChromaMode(src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (vp8common.MBPredictionMode, int, bool) {
	bestMode := vp8common.DCPred
	bestSSE := 0
	for i, uvMode := range wholeBlockIntraUVModeCandidates {
		if !predictAnalysisChroma(pred, mbRow, mbCol, uvMode, scratch) {
			return 0, 0, false
		}
		sse := vp8enc.MacroblockChromaSSE(src, pred, mbRow, mbCol)
		if i == 0 || sse < bestSSE {
			bestMode = uvMode
			bestSSE = sse
		}
	}
	return bestMode, intraUVModeRate(true, bestMode), true
}

// pickFastBPredLumaModeKFWithRDConstants mirrors libvpx pickinter.c pick_intra4x4mby_modes
// for keyframes: 16 sub-blocks, each scored via the four fast B-mode
// candidates {DC, TM, VE, HE} using pixel-domain 4x4 SSE. The mode rate uses
// libvpx's per-(A, L) keyframe table (mb->bmode_costs[A][L]) via
// bPredAnalysisAboveMode/LeftMode and bPredModeRate(keyFrame=true).
//
// After picking each block's mode the function performs the same
// DCT/quantize/dequantize/IDCT-add reconstruction libvpx executes via
// vp8_encode_intra4x4block (encodeintra.c), so subsequent blocks see
// reconstructed pixels (not raw predictor pixels) when they read their
// left/above-right neighbors. Without this step, govpx's predictor refs for
// blocks 1..15 would diverge from libvpx's because libvpx writes
// reconstructed pixels back into xd->dst.y_buffer between sub-blocks.
//
// Returns the picked sub-modes, the sum of bmode rates, and the BPred RDCOST
// (RDCOST(mbmode_cost[B_PRED]+sum_rates, sum_4x4_SSE)) — matching libvpx's
// `error4x4` return that the caller compares against `error16x16`.
func pickFastBPredLumaModeKFWithRDConstants(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, fastQuant bool, rdMult int, rdDiv int) ([16]vp8common.BPredictionMode, int, int, bool) {
	if quant == nil {
		return [16]vp8common.BPredictionMode{}, 0, 0, false
	}
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
	}
	refs := vp8dec.BuildIntraPredictorRefs(pred, mbRow, mbCol, &scratch.Refs)
	yOff := mbRow*16*pred.YStride + mbCol*16
	y := pred.Y[yOff:]
	var modes [16]vp8common.BPredictionMode
	totalRate := 0
	totalDist := 0
	for block := range 16 {
		bestMode := vp8common.BDCPred
		bestRate := 0
		bestDist := 0
		bestCost := 0
		var bestPred [16]byte
		aboveMode := bPredAnalysisAboveMode(true, above, modes, block)
		leftMode := bPredAnalysisLeftMode(true, left, modes, block)
		for i, candidate := range fastBPredIntraModeCandidates {
			var blockPred [16]byte
			if !predictAnalysisBPredBlock(candidate, blockPred[:], 4, y, pred.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
				return [16]vp8common.BPredictionMode{}, 0, 0, false
			}
			modeRate := bPredModeRate(true, candidate, aboveMode, leftMode)
			modeDist := vp8enc.BPredBlockSSE(src, mbRow, mbCol, block, blockPred[:], 4)
			cost := vp8enc.RDCost(rdMult, rdDiv, modeRate, modeDist)
			if i == 0 || cost < bestCost {
				bestMode = candidate
				bestRate = modeRate
				bestDist = modeDist
				bestCost = cost
				bestPred = blockPred
			}
		}
		modes[block] = bestMode

		// Mirror libvpx vp8_encode_intra4x4block: re-predict, residual,
		// DCT, quantize/dequant, IDCT-add into the analysis Y plane so the
		// next block's predictor neighbors come from reconstructed pixels.
		var input [16]int16
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		vp8enc.FillBPredResidual4x4(src, mbRow, mbCol, block, bestPred[:], &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		eob := vp8enc.QuantizeDecisionBlock(fastQuant, &dct, &quant.Y1, zbinOverQuant, &qcoeff, &dqcoeff)
		var recon [16]byte
		if eob > 1 {
			dsp.IDCT4x4Add(&dqcoeff, bestPred[:], 4, recon[:], 4)
		} else {
			dsp.DCOnlyIDCT4x4Add(dqcoeff[0], bestPred[:], 4, recon[:], 4)
		}
		vp8enc.CopyBPredBlock(recon[:], y, pred.YStride, block)

		totalRate += bestRate
		totalDist += bestDist
	}
	mbModeRate := intraYModeRate(true, vp8common.BPred)
	rd := vp8enc.RDCost(rdMult, rdDiv, mbModeRate+totalRate, totalDist)
	return modes, totalRate, rd, true
}

// predictBestWholeBlockIntraModeRD picks the best 16x16 intra Y mode using
// libvpx's transform-domain RD (rdopt.c macro_block_yrd) instead of pixel-SSE
// — the AC coefficients are quantized as Y_NO_DC and the 16 DC samples are
// lifted into the Y2 block, Walsh-transformed, and quantized; rate is the
// summed cost_coeffs and distortion is libvpx's
// (mbblock_error<<2 + y2_block_error) >> 4.
func predictBestWholeBlockIntraModeRD(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (vp8common.MBPredictionMode, vp8common.MBPredictionMode, int, int, int, int, bool) {
	return predictBestWholeBlockIntraModeRDWithProbs(src, qIndex, zbinOverQuant, keyFrame, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, nil, nil, fastQuant)
}

func predictBestWholeBlockIntraModeRDWithProbs(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, interYModeProbs []uint8, interUVModeProbs []uint8, fastQuant bool) (vp8common.MBPredictionMode, vp8common.MBPredictionMode, int, int, int, int, bool) {
	return predictBestWholeBlockIntraModeRDWithProbsAndRDConstants(src, qIndex, zbinOverQuant, 0, keyFrame, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, interYModeProbs, interUVModeProbs, fastQuant, 0, 0)
}

// predictBestWholeBlockIntraModeRDWithProbsAndRDConstants mirrors
// predictBestWholeBlockIntraModeRDWithProbs but accepts an explicit
// (rdMult, rdDiv) pair so callers can thread libvpx's activity-masked
// x->rdmult into the per-mode RDCOST comparison inside the whole-block
// intra Y picker. libvpx's vp8/encoder/rdopt.c rd_pick_intra_mbuv_mode
// and rd_pick_intra_mby_mode score every candidate with RDCOST using
// x->rdmult, which is activity-masked under --tune=ssim. Threading the
// tuned pair here makes the inter-frame intra mode picker (DC/V/H/TM/B)
// match libvpx's per-MB winner.
//
// When rdMult <= 0, vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant) is
// used (the PSNR / keyframe default path).
func predictBestWholeBlockIntraModeRDWithProbsAndRDConstants(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, actZbinAdj int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, interYModeProbs []uint8, interUVModeProbs []uint8, fastQuant bool, rdMult int, rdDiv int) (vp8common.MBPredictionMode, vp8common.MBPredictionMode, int, int, int, int, bool) {
	if quant == nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	if coefProbs == nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
	}
	bestYMode := vp8common.DCPred
	bestYRate := 0
	bestYDist := 0
	bestYCost := 0
	for i, yMode := range wholeBlockIntraYModeCandidates {
		mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: yMode, UVMode: vp8common.DCPred}
		if !predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch) {
			return 0, 0, 0, 0, 0, 0, false
		}
		yRate, yDist, _, _ := wholeBlockYTransformRD(src, pred, mbRow, mbCol, zbinOverQuant, actZbinAdj, aboveTok, leftTok, quant, coefProbs, fastQuant)
		rate := intraYModeRateWithProbs(keyFrame, yMode, interYModeProbs) + yRate
		cost := vp8enc.RDCost(rdMult, rdDiv, rate, yDist)
		if i == 0 || cost < bestYCost {
			bestYMode = yMode
			bestYRate = rate
			bestYDist = yDist
			bestYCost = cost
		}
	}

	bestUVMode, bestUVRate, bestUVDist, ok := predictBestIntraChromaModeRDWithProbsAndRDConstants(src, qIndex, zbinOverQuant, actZbinAdj, keyFrame, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, interUVModeProbs, fastQuant, rdMult, rdDiv)
	if !ok {
		return 0, 0, 0, 0, 0, 0, false
	}
	return bestYMode, bestUVMode, bestYRate, bestYDist, bestUVRate, bestUVDist, true
}

// wholeBlockYTransformRD ports libvpx rdopt.c macro_block_yrd. The selected
// yMode prediction is assumed to be present in pred at (mbRow, mbCol).
// aboveTok and leftTok seed the per-block token contexts; libvpx
// vp8_rdcost_mby reads them from `e_mbd.above_context` / `left_context`.
// Callers pass the coefficient probability base that the matching packet
// writer will use for token-rate costing.
func wholeBlockYTransformRD(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, zbinOverQuant int, actZbinAdj int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (int, int, uint8, [16]int16) {
	rate, dist, y2EOB, y2Q, _ := wholeBlockYTransformRDWithEOBs(src, pred, mbRow, mbCol, zbinOverQuant, actZbinAdj, aboveTok, leftTok, quant, coefProbs, fastQuant)
	return rate, dist, y2EOB, y2Q
}

// wholeBlockYTransformRDWithEOBs mirrors wholeBlockYTransformRD but also
// returns the per-Y-block AC-EOB indicator (libvpx's `eobs[i] > has_y2_block`
// term in vp8/encoder/rdopt.c calculate_final_rd_costs at lines 1690-1692).
// The returned yACEOBCount counts the number of Y blocks whose quantized
// coefficient EOB is strictly greater than 1 — i.e. blocks that carry at
// least one AC coefficient. The picker uses this to mirror libvpx's
// rate2 -= rate_y + rate_uv backout when tteob == 0 (calculate_final_rd_costs
// at lines 1700-1714). The legacy 4-return wholeBlockYTransformRD wraps
// this helper so non-intra-in-inter-loop callers keep their existing
// signature.
func wholeBlockYTransformRDWithEOBs(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, zbinOverQuant int, actZbinAdj int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (int, int, uint8, [16]int16, int) {
	if coefProbs == nil {
		return 0, 0, 0, [16]int16{}, 0
	}
	var dct [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	var y2Input [16]int16
	var y2Coeff [16]int16
	var y2Q [16]int16
	var y2DQ [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		y2Left = leftTok.Y2
	}

	rate := 0
	mbblockError := 0
	yACEOBCount := 0
	// Whole-MB residual+DCT batch — mirrors libvpx vp8_transform_intra_mby's
	// fdct8x4 chain. The per-block rate/distortion accumulation still runs
	// serially because token-context (yAbove/yLeft) and the regular-quantize
	// zbin-zerorun are block-sequential.
	var residuals [16 * 16]int16
	var dcts [16 * 16]int16
	vp8enc.GatherMacroblockYResiduals4x4(src.Y, src.YStride, src.Width, src.Height, pred.Y, pred.YStride, mbCol*16, mbRow*16, residuals[:])
	vp8enc.ForwardDCT4x4Batch(residuals[:], dcts[:], 16)
	for block := range 16 {
		copy(dct[:], dcts[block*16:block*16+16])
		y2Input[block] = dct[0]
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		// libvpx quantizes the Y1 DC coefficient before Y_NO_DC token
		// costing. The DC token itself is skipped because Y2 carries it,
		// but the quantized DC still affects zbin zero-run and EOB state.
		eob := vp8enc.QuantizeDecisionBlockWithActivity(fastQuant, &dct, &quant.Y1, zbinOverQuant, actZbinAdj, &qcoeff, &dqcoeff)
		dct[0] = 0
		dqcoeff[0] = 0
		rate += vp8enc.CoefficientBlockTokenRate(coefProbs, 0, ctx, 1, &qcoeff, eob)
		mbblockError += vp8enc.TransformBlockError(&dct, &dqcoeff)
		hasCoeffs := uint8(0)
		if eob > 1 {
			hasCoeffs = 1
			yACEOBCount++
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
	}
	vp8enc.ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
	y2Ctx := int(y2Above + y2Left)
	y2EOB := vp8enc.QuantizeDecisionBlockWithActivity(fastQuant, &y2Coeff, &quant.Y2, zbinOverQuant/2, actZbinAdj, &y2Q, &y2DQ)
	rate += vp8enc.CoefficientBlockTokenRate(coefProbs, 1, y2Ctx, 0, &y2Q, y2EOB)
	y2Error := vp8enc.TransformBlockError(&y2Coeff, &y2DQ)
	distortion := ((mbblockError << 2) + y2Error) >> 4
	return rate, distortion, uint8(y2EOB), y2Q, yACEOBCount
}

func predictBestIntraChromaModeRD(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (vp8common.MBPredictionMode, int, int, bool) {
	return predictBestIntraChromaModeRDWithProbs(src, qIndex, zbinOverQuant, keyFrame, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, nil, fastQuant)
}

func predictBestIntraChromaModeRDWithProbs(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, interUVModeProbs []uint8, fastQuant bool) (vp8common.MBPredictionMode, int, int, bool) {
	return predictBestIntraChromaModeRDWithProbsAndRDConstants(src, qIndex, zbinOverQuant, 0, keyFrame, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, interUVModeProbs, fastQuant, 0, 0)
}

// predictBestIntraChromaModeRDWithProbsAndRDConstants mirrors
// predictBestIntraChromaModeRDWithProbs but accepts an explicit
// (rdMult, rdDiv) pair so the activity-masked x->rdmult drives the UV
// mode RDCOST comparison. libvpx's vp8/encoder/rdopt.c
// rd_pick_intra_mbuv_mode uses x->rdmult for the per-UV-mode RDCOST
// (lines 1162-1190), so under --tune=ssim the activity-tuned multiplier
// changes which chroma mode wins on textured / flat macroblocks. When
// rdMult <= 0 the vp8enc.RDConstantsWithZbin defaults are used.
func predictBestIntraChromaModeRDWithProbsAndRDConstants(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, actZbinAdj int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, interUVModeProbs []uint8, fastQuant bool, rdMult int, rdDiv int) (vp8common.MBPredictionMode, int, int, bool) {
	mode, rate, dist, _, ok := predictBestIntraChromaModeRDWithProbsAndRDConstantsAndEOBs(src, qIndex, zbinOverQuant, actZbinAdj, keyFrame, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, interUVModeProbs, fastQuant, rdMult, rdDiv)
	return mode, rate, dist, ok
}

// predictBestIntraChromaModeRDWithProbsAndRDConstantsAndEOBs mirrors
// predictBestIntraChromaModeRDWithProbsAndRDConstants but also returns
// the live UV-block EOB sum left by libvpx's rd_pick_intra_mbuv_mode
// loop (uv_intra_tteob in vp8/encoder/rdopt.c). libvpx records the
// winning mode/rate/distortion but does not restore x->e_mbd.eobs for
// that winner before summing uv_intra_tteob; the EOB state is whatever
// the final UV trial left behind. Callers running inside the inter-RD
// picker use this to mirror libvpx's tteob==0 rate2 backout
// (calculate_final_rd_costs at lines 1700-1714); the 4-return wrapper
// above keeps the legacy keyframe-path callers unchanged.
func predictBestIntraChromaModeRDWithProbsAndRDConstantsAndEOBs(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, actZbinAdj int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, interUVModeProbs []uint8, fastQuant bool, rdMult int, rdDiv int) (vp8common.MBPredictionMode, int, int, int, bool) {
	if quant == nil || coefProbs == nil {
		return 0, 0, 0, 0, false
	}
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
	}
	bestUVMode := vp8common.DCPred
	bestUVRate := 0
	bestUVDist := 0
	bestUVCost := 0
	liveUVEOBSum := 0
	for i, uvMode := range wholeBlockIntraUVModeCandidates {
		if !predictAnalysisChroma(pred, mbRow, mbCol, uvMode, scratch) {
			return 0, 0, 0, 0, false
		}
		tokenRate, dist, uvEOBSum := wholeBlockChromaTransformRDWithEOBs(src, pred, mbRow, mbCol, zbinOverQuant, actZbinAdj, aboveTok, leftTok, quant, coefProbs, fastQuant)
		liveUVEOBSum = uvEOBSum
		rate := intraUVModeRateWithProbs(keyFrame, uvMode, interUVModeProbs) + tokenRate
		cost := vp8enc.RDCost(rdMult, rdDiv, rate, dist)
		if i == 0 || cost < bestUVCost {
			bestUVMode = uvMode
			bestUVRate = rate
			bestUVDist = dist
			bestUVCost = cost
		}
	}
	return bestUVMode, bestUVRate, bestUVDist, liveUVEOBSum, true
}

// wholeBlockChromaTransformRD mirrors libvpx rdopt.c rd_pick_intra_mbuv_mode:
// the predicted U/V blocks are transformed, quantized, token-costed, and
// measured with transform-domain reconstruction error divided by four.
func wholeBlockChromaTransformRD(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, zbinOverQuant int, actZbinAdj int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (int, int) {
	rate, dist, _ := wholeBlockChromaTransformRDWithEOBs(src, pred, mbRow, mbCol, zbinOverQuant, actZbinAdj, aboveTok, leftTok, quant, coefProbs, fastQuant)
	return rate, dist
}

// wholeBlockChromaTransformRDWithEOBs mirrors wholeBlockChromaTransformRD
// but also returns the sum of UV block EOBs (libvpx's `uv_intra_tteob`
// term in vp8/encoder/rdopt.c lines 1934-1943). The picker uses this to
// mirror libvpx's `tteob += uv_intra_tteob` aggregation when the MB's
// reference frame is INTRA_FRAME (line 1697).
func wholeBlockChromaTransformRDWithEOBs(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, zbinOverQuant int, actZbinAdj int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (int, int, int) {
	if pred == nil || quant == nil || coefProbs == nil {
		return maxInt() / 4, maxInt() / 4, 0
	}
	uvWidth, uvHeight := vp8enc.SourceImageUVDimensions(src)
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	if aboveTok != nil {
		uvAbove = vp8enc.TokenUVContextArray(aboveTok)
	}
	if leftTok != nil {
		uvLeft = vp8enc.TokenUVContextArray(leftTok)
	}

	rate := 0
	distortion := 0
	uvEOBSum := 0
	// Whole-UV residual+DCT batch — mirrors libvpx vp8_transform_mbuv's
	// pair of fdct8x4 calls. Token-context updates and the
	// regular-quantize zbin-zerorun keep the per-block accumulation
	// loop serial.
	var residuals [8 * 16]int16
	var dcts [8 * 16]int16
	vp8enc.GatherMacroblockUVResiduals4x4(src.U, src.UStride, uvWidth, uvHeight, pred.U, pred.UStride, mbCol*8, mbRow*8, residuals[0:64])
	vp8enc.GatherMacroblockUVResiduals4x4(src.V, src.VStride, uvWidth, uvHeight, pred.V, pred.VStride, mbCol*8, mbRow*8, residuals[64:128])
	vp8enc.ForwardDCT4x4Batch(residuals[:], dcts[:], 8)
	for slot := range 8 {
		block := 16 + slot
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		copy(dct[:], dcts[slot*16:slot*16+16])
		a, l := vp8enc.MacroblockCoefficientUVContextIndex(block)
		ctx := int(uvAbove[a] + uvLeft[l])
		eob := vp8enc.QuantizeDecisionBlockWithActivity(fastQuant, &dct, &quant.UV, zbinOverQuant, actZbinAdj, &qcoeff, &dqcoeff)
		rate += vp8enc.CoefficientBlockTokenRate(coefProbs, 2, ctx, 0, &qcoeff, eob)
		distortion += vp8enc.TransformBlockError(&dct, &dqcoeff)
		uvEOBSum += eob
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	return rate, distortion >> 2, uvEOBSum
}

// Ported from libvpx v1.16.0 vp8/encoder/rdopt.c rd_pick_intra4x4block (and
// the per-MB driver rd_pick_intra4x4mby_modes at lines 519-644). Audit notes
// (parity items confirmed against the reference):
//  1. Bmode cost source: keyframe path uses vp8tables.KeyFrameBModeProbs[A][L]
//     via bPredAnalysisAboveMode/LeftMode, matching mb->bmode_costs[A][L];
//     inter path uses vp8tables.DefaultBModeProbs (cf. mb->inter_bmode_costs).
//     Note libvpx's vp8_init_mode_costs overwrites inter_bmode_costs[0..3]
//     with sub_mv_ref-token costs after the bmode-token init — but mirroring
//     that quirk here regresses good-cpu3-vbr SPLITMV decisions, so the RD
//     picker keeps the bmode-token costs across all 10 slots. The fast
//     picker (estimateFastBPredIntraModeScore) honors the libvpx-stale
//     overwrite via libvpxInterFastBpredModeCost, where rt-cpu0/4/8 corner
//     MBs need it for B_PRED-vs-NEWMV tiebreak parity.
//  2. ENTROPY_CONTEXT: tokenAbove/tokenLeft are seeded once from the caller
//     and only committed using bestEOB after the candidate loop, mirroring
//     libvpx's "*a = tempa; *l = templ;" inside the if-best block.
//  3. Reconstruction: dsp.IDCT4x4Add is invoked inside the winning branch
//     and the resulting bestRecon is written via vp8enc.CopyBPredBlock at the end
//     of each block iteration, equivalent to libvpx's deferred
//     vp8_short_idct4x4llm(best_dqcoeff, best_predictor, ...) call.
//  4. Bailout: govpx returns ok=false when the running rate/dist already
//     exceeds bestRD; callers then fall back to the whole-block result, the
//     same role as libvpx's "return INT_MAX" when total_rd >= best_rd.
//  5. BPred container cost: callers add intraYModeRate(keyFrame, BPred)
//     before comparing with whole-block RD, matching libvpx's
//     "cost = mb->mbmode_cost[xd->frame_type][B_PRED];" seed.
//  6. intra_prediction_down_copy: predictAnalysisBPredBlock reads
//     refs.YAbove[16:20] for the bottom-right sub-block, replacing libvpx's
//     in-place predictor copy.
//
// libvpx picks each 4x4 block by per-block RDCOST, accumulates those chosen
// per-block RD values for the best_rd bailout, and returns the final MB-level
// RDCOST only if the per-block sum stays below best_rd.
func predictBestBPredLumaModeRD(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, bestRD int, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) ([16]vp8common.BPredictionMode, int, int, bool) {
	return predictBestBPredLumaModeRDWithRDConstants(src, qIndex, zbinOverQuant, 0, keyFrame, mbRow, mbCol, above, left, aboveTok, leftTok, quant, pred, scratch, bestRD, coefProbs, nil, fastQuant, 0, 0)
}

// predictBestBPredLumaModeRDWithRDConstants mirrors predictBestBPredLumaModeRD
// but accepts an explicit (rdMult, rdDiv) pair so callers can thread the
// activity-tuned RD constants used by libvpx's rd_pick_intra4x4block under
// --tune=ssim. When rdMult <= 0, the defaults derived from
// vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant) are used (the PSNR path).
//
// libvpx applies x->rdmult (activity-masked) inside the per-block 4x4 mode
// loop (vp8/encoder/rdopt.c rd_pick_intra4x4block: `this_rd = RDCOST(x->rdmult,
// x->rddiv, rate, distortion)`), so the tune-ssim B_PRED picker resolves to
// different per-sub-block intra modes than the default-rdmult path.
func predictBestBPredLumaModeRDWithRDConstants(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, actZbinAdj int, keyFrame bool, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, bestRD int, coefProbs *vp8tables.CoefficientProbs, interBModeProbs []uint8, fastQuant bool, rdMult int, rdDiv int) ([16]vp8common.BPredictionMode, int, int, bool) {
	modes, rate, dist, _, ok := predictBestBPredLumaModeRDWithRDConstantsAndEOBs(src, qIndex, zbinOverQuant, actZbinAdj, keyFrame, mbRow, mbCol, above, left, aboveTok, leftTok, quant, pred, scratch, bestRD, coefProbs, interBModeProbs, fastQuant, rdMult, rdDiv)
	return modes, rate, dist, ok
}

// predictBestBPredLumaModeRDWithRDConstantsAndEOBs mirrors
// predictBestBPredLumaModeRDWithRDConstants but additionally returns the
// count of 4x4 Y blocks whose final selected EOB is strictly greater than
// zero. This count is libvpx's `sum(eobs[0..15] > has_y2_block)` for the
// B_PRED (has_y2_block=0) branch of vp8/encoder/rdopt.c:1687-1714. Callers
// inside the inter-RD picker use it to compute tteob and trigger the
// tteob==0 rate2 backout for B_PRED intra-in-inter candidates.
func predictBestBPredLumaModeRDWithRDConstantsAndEOBs(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, actZbinAdj int, keyFrame bool, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, bestRD int, coefProbs *vp8tables.CoefficientProbs, interBModeProbs []uint8, fastQuant bool, rdMult int, rdDiv int) ([16]vp8common.BPredictionMode, int, int, int, bool) {
	if quant == nil {
		return [16]vp8common.BPredictionMode{}, 0, 0, 0, false
	}
	if coefProbs == nil {
		return [16]vp8common.BPredictionMode{}, 0, 0, 0, false
	}
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
	}
	refs := vp8dec.BuildIntraPredictorRefs(pred, mbRow, mbCol, &scratch.Refs)
	yOff := mbRow*16*pred.YStride + mbCol*16
	y := pred.Y[yOff:]
	var modes [16]vp8common.BPredictionMode
	var tokenAbove [4]uint8
	var tokenLeft [4]uint8
	if aboveTok != nil {
		tokenAbove = aboveTok.Y1
	}
	if leftTok != nil {
		tokenLeft = leftTok.Y1
	}
	totalRate := 0
	totalDist := 0
	totalBlockRD := int64(0)
	bPredEOBCount := 0
	for block := range 16 {
		bestMode := vp8common.BDCPred
		bestEOB := 0
		var bestRecon [16]byte
		bestRate := 0
		bestDist := 0
		bestCost := 0
		for i, candidate := range bPredIntraModeCandidates {
			var candidatePred [16]byte
			if !predictAnalysisBPredBlock(candidate, candidatePred[:], 4, y, pred.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
				return [16]vp8common.BPredictionMode{}, 0, 0, 0, false
			}
			var input [16]int16
			var dct [16]int16
			var qcoeff [16]int16
			var dqcoeff [16]int16
			vp8enc.FillBPredResidual4x4(src, mbRow, mbCol, block, candidatePred[:], &input)
			vp8enc.ForwardDCT4x4(input[:], 4, &dct)
			tokenCtx := int(tokenAbove[block&3] + tokenLeft[(block&0x0c)>>2])
			eob := vp8enc.QuantizeDecisionBlockWithActivity(fastQuant, &dct, &quant.Y1, zbinOverQuant, actZbinAdj, &qcoeff, &dqcoeff)
			coefRate := vp8enc.CoefficientBlockTokenRate(coefProbs, 3, tokenCtx, 0, &qcoeff, eob)
			aboveMode := bPredAnalysisAboveMode(keyFrame, above, modes, block)
			leftMode := bPredAnalysisLeftMode(keyFrame, left, modes, block)
			rate := bPredModeRateWithProbs(keyFrame, candidate, aboveMode, leftMode, interBModeProbs) + coefRate
			dist := vp8enc.TransformBlockError(&dct, &dqcoeff) >> 2
			cost := vp8enc.RDCost(rdMult, rdDiv, rate, dist)
			if i == 0 || cost < bestCost {
				var candidateRecon [16]byte
				bestMode = candidate
				if eob > 1 {
					dsp.IDCT4x4Add(&dqcoeff, candidatePred[:], 4, candidateRecon[:], 4)
				} else {
					dsp.DCOnlyIDCT4x4Add(dqcoeff[0], candidatePred[:], 4, candidateRecon[:], 4)
				}
				bestRecon = candidateRecon
				bestEOB = eob
				bestRate = rate
				bestDist = dist
				bestCost = cost
			}
		}
		modes[block] = bestMode
		vp8enc.CopyBPredBlock(bestRecon[:], y, pred.YStride, block)
		hasCoeffs := uint8(0)
		if bestEOB > 0 {
			hasCoeffs = 1
			bPredEOBCount++
		}
		tokenAbove[block&3] = hasCoeffs
		tokenLeft[(block&0x0c)>>2] = hasCoeffs
		totalRate += bestRate
		totalDist += bestDist
		// libvpx rd_pick_intra4x4mby_modes accumulates the already-rounded
		// RD of each selected 4x4 block and compares that sum to best_rd.
		// It does not compare RDCOST(sum(rate), sum(distortion)).
		totalBlockRD += int64(bestCost)
		if bestRD > 0 && totalBlockRD >= int64(bestRD) {
			return [16]vp8common.BPredictionMode{}, 0, 0, 0, false
		}
	}
	return modes, totalRate, totalDist, bPredEOBCount, true
}

func bPredAnalysisAboveMode(keyFrame bool, above *vp8enc.KeyFrameMacroblockMode, modes [16]vp8common.BPredictionMode, block int) vp8common.BPredictionMode {
	if !keyFrame {
		return vp8common.BDCPred
	}
	if block >= 4 {
		return modes[block-4]
	}
	if above == nil {
		return vp8common.BDCPred
	}
	if above.YMode == vp8common.BPred {
		return above.BModes[block+12]
	}
	return blockModeFromKeyFrameMacroblockMode(above.YMode)
}

func bPredAnalysisLeftMode(keyFrame bool, left *vp8enc.KeyFrameMacroblockMode, modes [16]vp8common.BPredictionMode, block int) vp8common.BPredictionMode {
	if !keyFrame {
		return vp8common.BDCPred
	}
	if block&3 != 0 {
		return modes[block-1]
	}
	if left == nil {
		return vp8common.BDCPred
	}
	if left.YMode == vp8common.BPred {
		return left.BModes[block+3]
	}
	return blockModeFromKeyFrameMacroblockMode(left.YMode)
}

func blockModeFromKeyFrameMacroblockMode(mode vp8common.MBPredictionMode) vp8common.BPredictionMode {
	switch mode {
	case vp8common.VPred:
		return vp8common.BVEPred
	case vp8common.HPred:
		return vp8common.BHEPred
	case vp8common.TMPred:
		return vp8common.BTMPred
	default:
		return vp8common.BDCPred
	}
}

func intraYModeRate(keyFrame bool, mode vp8common.MBPredictionMode) int {
	return intraYModeRateWithProbs(keyFrame, mode, nil)
}

func intraYModeRateWithProbs(keyFrame bool, mode vp8common.MBPredictionMode, interProbs []uint8) int {
	if keyFrame {
		return vp8enc.TreeTokenCost(vp8tables.KeyFrameYModeTree[:], vp8tables.KeyFrameYModeProbs[:], int(mode))
	}
	if len(interProbs) == vp8tables.YModeProbCount {
		return vp8enc.TreeTokenCost(vp8tables.YModeTree[:], interProbs, int(mode))
	}
	return vp8enc.TreeTokenCost(vp8tables.YModeTree[:], vp8tables.DefaultYModeProbs[:], int(mode))
}

func (e *VP8Encoder) interIntraYModeRate(mode vp8common.MBPredictionMode) int {
	return intraYModeRateWithProbs(false, mode, e.modeProbs.YMode[:])
}

func intraUVModeRate(keyFrame bool, mode vp8common.MBPredictionMode) int {
	return intraUVModeRateWithProbs(keyFrame, mode, nil)
}

func intraUVModeRateWithProbs(keyFrame bool, mode vp8common.MBPredictionMode, interProbs []uint8) int {
	if keyFrame {
		return vp8enc.TreeTokenCost(vp8tables.UVModeTree[:], vp8tables.KeyFrameUVModeProbs[:], int(mode))
	}
	if len(interProbs) == vp8tables.UVModeProbCount {
		return vp8enc.TreeTokenCost(vp8tables.UVModeTree[:], interProbs, int(mode))
	}
	return vp8enc.TreeTokenCost(vp8tables.UVModeTree[:], vp8tables.DefaultUVModeProbs[:], int(mode))
}

func bPredModeRate(keyFrame bool, mode vp8common.BPredictionMode, above vp8common.BPredictionMode, left vp8common.BPredictionMode) int {
	return bPredModeRateWithProbs(keyFrame, mode, above, left, nil)
}

func bPredModeRateWithProbs(keyFrame bool, mode vp8common.BPredictionMode, above vp8common.BPredictionMode, left vp8common.BPredictionMode, interProbs []uint8) int {
	if keyFrame {
		return vp8enc.TreeTokenCost(vp8tables.BModeTree[:], vp8tables.KeyFrameBModeProbs[int(above)][int(left)][:], int(mode))
	}
	if len(interProbs) == vp8tables.BModeProbCount {
		return vp8enc.TreeTokenCost(vp8tables.BModeTree[:], interProbs, int(mode))
	}
	return vp8enc.TreeTokenCost(vp8tables.BModeTree[:], vp8tables.DefaultBModeProbs[:], int(mode))
}

// libvpxInterFastBpredModeCost mirrors libvpx vp8/encoder/modecosts.c
// vp8_init_mode_costs's `inter_bmode_costs` table as read by the inter-frame
// non-RD fast picker (vp8/encoder/pickinter.c pick_intra4x4block).
//
// libvpx initializes the table in two steps:
//
//	vp8_cost_tokens(rd_costs->inter_bmode_costs, x->fc.bmode_prob, vp8_bmode_tree);
//	vp8_cost_tokens(rd_costs->inter_bmode_costs, x->fc.sub_mv_ref_prob, vp8_sub_mv_ref_tree);
//
// vp8_cost_tokens writes C[-leaf] for each negative leaf in the tree. The
// vp8_bmode_tree leaves are -B_DC_PRED..-B_HU_PRED (slots 0..9). The
// vp8_sub_mv_ref_tree leaves are -LEFT4X4..-NEW4X4 (slots 10..13). The
// second call therefore writes slots 10..13 ONLY — slots 0..3 retain the
// bmode-token costs from the first init. An off-by-tree-walk reading of
// vp8_cost_tokens suggests sub_mv_ref token costs for slots 0..3, but the
// actual tree-walk only touches the negated-leaf slots.
//
// pick_intra4x4block iterates `mode = B_DC_PRED..B_HE_PRED` (slots 0..3) and
// reads `mode_costs[mode]`, which therefore returns the bmode-token cost
// for that intra4x4 mode under the current frame's bmode_prob. Using the
// default bmode_prob at decode time matches libvpx's frame-1 state because
// fc.bmode_prob is reset to vp8_bmode_prob on every frame in
// vp8_default_coef_probs / start_encoded_frame.
func libvpxInterFastBpredModeCost(mode vp8common.BPredictionMode) int {
	return vp8enc.TreeTokenCost(vp8tables.BModeTree[:], vp8tables.DefaultBModeProbs[:], int(mode))
}

func libvpxInterFastBpredModeCostWithProbs(mode vp8common.BPredictionMode, probs []uint8) int {
	if len(probs) == vp8tables.BModeProbCount {
		return vp8enc.TreeTokenCost(vp8tables.BModeTree[:], probs, int(mode))
	}
	return libvpxInterFastBpredModeCost(mode)
}

// vp8enc.CoefficientBlockTokenRate ports libvpx's vp8/encoder/rdopt.c:cost_coeffs.
// It returns the entropy-coded token cost (in 1/256-bit units) of the given
// quantized coefficient block, including the implicit "skip_eob_node" elision
// libvpx applies when the previous token had prev_token_class == 0 (i.e. the
// previous coefficient was a ZERO_TOKEN) and the current coefficient is past
// the first band of the plane.
//
// Equivalent libvpx loop body:
//
//	for (; c < eob; ++c) {
//	    cost += token_costs[type][bands[c]][pt][token(qcoeff[zigzag[c]])];
//	    cost += dct_value_cost[v];
//	    pt = prev_token_class[token];
//	}
//	if (c < 16) cost += token_costs[type][bands[c]][pt][EOB];
//
// where token_costs[type][band][0][...] for band > (type == 0 ? 1 : 0) uses the
// non-EOB subtree only (matching skip_eob_node = (pt == 0) in tokenize.c). All
// other (type, band, pt) combinations include the EOB-vs-not bit.
