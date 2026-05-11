package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

type fastInterModeLoopContext struct {
	modeMVs   interModeMVSlots
	signBias  [vp8common.MaxRefFrames]bool
	bestRefMV vp8enc.MotionVector
	search    interAnalysisSearchConfig
	searchSet bool
	mvCosts   *vp8enc.MotionVectorCostTables
	variance  [fastInterVarianceCacheSize]fastInterVarianceCacheEntry
}

const fastInterVarianceCacheSize = 16

type fastInterVarianceCacheEntry struct {
	set      bool
	ref      *vp8common.Image
	mv       vp8enc.MotionVector
	variance int
	sse      int
}

func (ctx *fastInterModeLoopContext) searchConfig(e *VP8Encoder) interAnalysisSearchConfig {
	if !ctx.searchSet {
		ctx.search = e.interAnalysisSearchConfig()
		ctx.searchSet = true
	}
	return ctx.search
}

func (e *VP8Encoder) estimateFastIntraModeScore(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, mbMode vp8common.MBPredictionMode, bestSSE int, quant *vp8enc.MacroblockQuant) (vp8enc.InterFrameMacroblockMode, int, int, int, int, bool) {
	if mbMode == vp8common.BPred {
		return e.estimateFastBPredIntraModeScore(src, mbRow, mbCol, qIndex, bestSSE, quant)
	}
	if mbMode < vp8common.DCPred || mbMode > vp8common.TMPred {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
	}
	// e is always non-nil on the picker hot path (selectFastInterFrameModeDecision
	// derefs e.interRDFrameActive before invoking us); the legacy nil-guarded
	// branch below was a no-op cost driver. Hoist the analysis image / zbin
	// loads into locals so the predict + variance calls share a single read.
	zbinOverQuant := e.rc.currentZbinOverQuant
	if e.activityMapValid {
		zbinOverQuant = e.tunedZbinOverQuant(zbinOverQuant, mbRow, mbCol)
	}
	analysisImg := &e.analysis.Img
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(analysisImg, mbRow, mbCol, &mode, &e.reconstructScratch) {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
	}
	variance, sse := macroblockLumaVarianceSSE(src, analysisImg, mbRow, mbCol)
	rate := boolBitCost(e.refProbIntra, 0) + e.interIntraYModeRate(mbMode)
	resultMode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, variance)
	if e.activityMapValid {
		score = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, rate, variance)
	}
	return resultMode, score, variance, sse, rate, true
}

// estimateFastBPredIntraModeScore mirrors libvpx pickinter.c
// pick_intra4x4mby_modes (the fast non-RD picker invoked from
// vp8_pick_inter_mode's B_PRED case for inter frames). Per-block scoring:
//
//  1. Iterate {BDC, BTM, BVE, BHE} (matches libvpx mode = B_DC_PRED..B_HE_PRED).
//  2. rate = inter_bmode_costs[mode] (libvpx's two-step init leaves slots
//     0..3 holding sub_mv_ref token costs after the bmode-token init is
//     overwritten — see libvpxInterBpredModeCost).
//  3. distortion = pixel-domain SSE between source and predictor.
//  4. RDCOST(rdmult, rddiv, rate, distortion); pick min.
//  5. After the per-block winner is chosen, run vp8_encode_intra4x4block
//     equivalent: DCT residual, quantize/dequant, IDCT-add into the analysis
//     Y plane so subsequent sub-blocks read reconstructed pixels (not raw
//     predictor) for their above-/left-within-MB neighbors. libvpx's
//     pick_intra4x4block tail call mirrors the same path.
//  6. After all 16 sub-blocks: MB-level variance against e_mbd.predictor
//     (here the analysis Y plane post-reconstruction) is the "distortion2"
//     libvpx feeds into the outer RDCOST in vp8_pick_inter_mode.
func (e *VP8Encoder) estimateFastBPredIntraModeScore(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, bestSSE int, quant *vp8enc.MacroblockQuant) (vp8enc.InterFrameMacroblockMode, int, int, int, int, bool) {
	if quant == nil {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
	}
	// e is always non-nil on the inter picker entry path; the prior nil
	// guard was dead code.
	zbinOverQuant := e.rc.currentZbinOverQuant
	if e.activityMapValid {
		zbinOverQuant = e.tunedZbinOverQuant(zbinOverQuant, mbRow, mbCol)
	}
	fastQuant := e.libvpxUseFastQuantForPick()
	analysisImg := &e.analysis.Img
	refs := vp8dec.BuildIntraPredictorRefs(analysisImg, mbRow, mbCol, &e.reconstructScratch.Refs)
	yStride := analysisImg.YStride
	yOff := mbRow*16*yStride + mbCol*16
	y := analysisImg.Y[yOff:]
	// Hoist refs slices once: predictAnalysisBPredBlock reads YAbove/YLeft/
	// YTopLeft on every sub-block iteration, but they are derived from the
	// MB's neighbor stripes and never mutated across the 16-block walk.
	refsYAbove := refs.YAbove
	refsYLeft := refs.YLeft
	refsYTopLeft := refs.YTopLeft
	// Hoist RD constants once: rdModeScoreWithZbin recomputes (rdMult, rdDiv)
	// from qIndex/zbinOverQuant, both invariant across the 64-iteration
	// {16 blocks} x {4 modes} inner cost loop.
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	if e.activityMapValid {
		rdMult = e.tunedRDMultiplier(rdMult, mbRow, mbCol)
	}
	quantY1 := &quant.Y1
	var modes [16]vp8common.BPredictionMode
	rate := boolBitCost(e.refProbIntra, 0) + e.interIntraYModeRate(vp8common.BPred)
	distortion := 0
	for block := range 16 {
		bestMode := vp8common.BModeCount
		bestRate := 0
		bestDist := 0
		bestCost := maxInt()
		var bestPred [16]byte
		for _, bMode := range fastBPredIntraModeCandidates {
			var blockPred [16]byte
			if !predictAnalysisBPredBlock(bMode, blockPred[:], 4, y, yStride, refsYAbove, refsYLeft, refsYTopLeft, block) {
				return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
			}
			modeRate := libvpxInterFastBpredModeCost(bMode)
			modeDist := bPredBlockSSE(src, mbRow, mbCol, block, blockPred[:], 4)
			modeCost := libvpxRDCost(rdMult, rdDiv, modeRate, modeDist)
			if modeCost < bestCost {
				bestMode = bMode
				bestRate = modeRate
				bestDist = modeDist
				bestCost = modeCost
				bestPred = blockPred
			}
		}
		modes[block] = bestMode

		// Mirror libvpx vp8_encode_intra4x4block: re-predict, residual,
		// DCT, quantize/dequant, IDCT-add into the analysis Y plane so the
		// next sub-block's predictor neighbors come from reconstructed
		// pixels (not raw predictor). pick_intra4x4block calls this at
		// the end of each block iteration (encodeintra.c:45).
		var input [16]int16
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		fillBPredResidual4x4(src, mbRow, mbCol, block, bestPred[:], &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		eob := quantizeDecisionBlock(fastQuant, &dct, quantY1, zbinOverQuant, &qcoeff, &dqcoeff)
		var recon [16]byte
		if eob > 1 {
			dsp.IDCT4x4Add(&dqcoeff, bestPred[:], 4, recon[:], 4)
		} else {
			dsp.DCOnlyIDCT4x4Add(dqcoeff[0], bestPred[:], 4, recon[:], 4)
		}
		copyBPredBlock(recon[:], y, yStride, block)

		rate += bestRate
		distortion += bestDist
		if distortion > bestSSE {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
		}
	}
	variance, sse := macroblockLumaVarianceSSE(src, analysisImg, mbRow, mbCol)
	return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: vp8common.DCPred, BModes: modes}, libvpxRDCost(rdMult, rdDiv, rate, variance), variance, sse, rate, true
}
