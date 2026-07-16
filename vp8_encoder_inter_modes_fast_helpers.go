package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

type fastInterModeLoopContext struct {
	mvCosts     *vp8enc.MotionVectorCostTables
	variance    [fastInterVarianceCacheSize]fastInterVarianceCacheEntry
	modeMVs     interModeMVSlots
	nearSADs    improvedInterFrameNearSADCache
	bestRefMV   vp8enc.MotionVector
	varianceSet uint16
	search      interAnalysisSearchConfig
	signBias    [vp8common.MaxRefFrames]bool
	searchSet   bool
	// candMode is the per-candidate scoring slot reused across the mode
	// loop. libvpx's vp8_pick_inter_mode mutates the shared
	// x->e_mbd.mode_info_context->mbmi in place per candidate; rebuilding
	// the ~100-byte InterFrameMacroblockMode literal per candidate was a
	// measurable memset in the 720p realtime profile. Only RefFrame, Mode,
	// MV and SegmentID are written per candidate — the score/rate/breakout
	// callees never mutate the struct and never read the remaining fields
	// (BlockMV/BModes/UVMode/Partition/MBSkipCoeff stay zero from the
	// per-MB context zeroing).
	candMode vp8enc.InterFrameMacroblockMode
	// intraRefs caches the luma intra predictor neighbor stripes for the
	// whole-block intra candidates. libvpx builds the above/left pointers
	// once per MB (xd->dst.y_buffer - stride / -1) and reuses them for
	// every DC/V/H/TM candidate; the stripes only depend on pixels outside
	// the MB, which no intra candidate scoring write touches.
	intraRefs    vp8dec.IntraPredictorRefs
	intraRefsSet bool
	// intraPred is the contiguous 16x16 luma candidate predictor scratch
	// mirroring libvpx's x->e_mbd.predictor: pickinter.c scores DC/V/H/TM
	// candidates with vp8_build_intra_predictors_mby_s into that stride-16
	// buffer and runs vpx_variance16x16 against it, never touching the
	// reconstruction frame. Fully overwritten by each candidate before any
	// read, so it needs no per-MB reset.
	intraPred [256]byte
}

// lumaIntraRefs returns the cached per-MB luma intra predictor refs,
// building them on first use. Interior macroblocks (mbRow > 0 &&
// mbCol > 0) take a direct-alias fast path: the contiguous above row and
// the top-left corner alias the analysis frame, and the left column is
// gathered straight into the scratch stripe — the same pixels the full
// builder resolves through its edge-fill branch chain, without the
// synthetic-border and coded-size re-validation per call.
func (ctx *fastInterModeLoopContext) lumaIntraRefs(e *VP8Encoder, mbRow int, mbCol int) vp8dec.IntraPredictorRefs {
	if !ctx.intraRefsSet {
		ctx.intraRefs = fastPickerLumaIntraRefs(&e.analysis.Img, mbRow, mbCol, &e.reconstructScratch.Refs)
		ctx.intraRefsSet = true
	}
	return ctx.intraRefs
}

// fastPickerLumaIntraRefs resolves the luma intra neighbor stripes with a
// single-gate interior alias, deferring to the full edge-aware builder
// otherwise. Interior in-frame macroblocks always have their above stripe,
// left column, and top-left corner inside the coded plane, so the values
// are identical to BuildIntraPredictorRefsLuma's alias/copy resolution.
func fastPickerLumaIntraRefs(img *vp8common.Image, mbRow int, mbCol int, scratch *vp8dec.IntraPredictorScratch) vp8dec.IntraPredictorRefs {
	stride := img.YStride
	if mbRow > 0 && mbCol > 0 && stride > 0 {
		yRow := mbRow * 16
		yCol := mbCol * 16
		aboveStart := (yRow-1)*stride + yCol
		leftStart := yRow*stride + yCol - 1
		// The full builder hands out a 20-byte above stripe (the
		// B_PRED path reads the 4 above-right samples), so the alias
		// must stay within the coded plane for all 20 samples —
		// rightmost-column MBs need the extended border and fall back.
		codedW := img.CodedWidth
		if codedW <= 0 {
			codedW = img.Width
		}
		codedH := img.CodedHeight
		if codedH <= 0 {
			codedH = img.Height
		}
		const aboveLen = len(scratch.YAbove)
		if yCol+aboveLen <= codedW && yRow+16 <= codedH &&
			leftStart+15*stride+1 <= len(img.Y) && aboveStart+aboveLen <= len(img.Y) {
			left := scratch.YLeft[:16]
			src := img.Y[leftStart:]
			_ = src[15*stride]
			for i := range 16 {
				left[i] = src[i*stride]
			}
			return vp8dec.IntraPredictorRefs{
				YAbove:        img.Y[aboveStart : aboveStart+aboveLen],
				YLeft:         left,
				YTopLeft:      img.Y[aboveStart-1],
				UpAvailable:   true,
				LeftAvailable: true,
			}
		}
	}
	return vp8dec.BuildIntraPredictorRefsLuma(img, mbRow, mbCol, scratch)
}

const fastInterVarianceCacheSize = 16

type fastInterVarianceCacheEntry struct {
	ref      *vp8common.Image
	mv       vp8enc.MotionVector
	variance int32
	sse      int32
}

func (ctx *fastInterModeLoopContext) searchConfig(e *VP8Encoder) interAnalysisSearchConfig {
	if !ctx.searchSet {
		ctx.search = e.interAnalysisSearchConfig()
		ctx.searchSet = true
	}
	return ctx.search
}

func (e *VP8Encoder) estimateFastIntraModeScore(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, mbMode vp8common.MBPredictionMode, bestSSE int, quant *vp8enc.MacroblockQuant, ctx *fastInterModeLoopContext) (vp8enc.InterFrameMacroblockMode, int, int, int, int, bool) {
	if mbMode == vp8common.BPred {
		return e.estimateFastBPredIntraModeScore(src, mbRow, mbCol, qIndex, bestSSE, quant, ctx)
	}
	if mbMode < vp8common.DCPred || mbMode > vp8common.TMPred {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
	}
	// e is always non-nil on the picker hot path (selectFastInterFrameModeDecision
	// derefs e.interRDFrameActive before invoking us); the legacy nil-guarded
	// branch below was a no-op cost driver. Hoist the analysis image / zbin
	// loads into locals so the predict + variance calls share a single read.
	// The neighbor stripes are cached per MB in the loop context: libvpx's
	// vp8_pick_inter_mode scores every whole-block intra candidate with the
	// same above/left pointers, and nothing the candidate loop writes
	// touches the stripe pixels outside the MB.
	zbinOverQuant := e.rc.currentZbinOverQuant
	refs := ctx.lumaIntraRefs(e, mbRow, mbCol)
	// libvpx pickinter.c scores whole-MB intra candidates in the
	// contiguous x->e_mbd.predictor scratch (stride 16) and never writes
	// them into the reconstruction frame; the accepted path re-predicts
	// the winner via predictAnalysisMacroblock exactly like libvpx's
	// vp8_encode_intra16x16mby. Mirror that: predict into the loop
	// context's 256-byte scratch and take the variance against it.
	pred := &ctx.intraPred
	if !vp8dec.PredictIntraY16x16(mbMode, pred[:], 16, refs.YAbove, refs.YLeft, refs.YTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
	}
	variance, sse := macroblockLumaVarianceSSEAgainstBuffer(src, mbRow, mbCol, pred)
	rate := e.interIntraReferenceRate() + e.interIntraYModeRate(mbMode)
	resultMode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	score := e.rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, variance)
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
//     Y plane so subsequent sub-blocks read reconstructed pixels for their
//     above-/left-within-MB neighbors. libvpx's pick_intra4x4block tail call
//     mirrors the same path.
//  6. After all 16 sub-blocks: MB-level variance against e_mbd.predictor
//     (the saved raw predictor blocks, not the reconstructed analysis plane)
//     is the "distortion2" libvpx feeds into the outer RDCOST in
//     vp8_pick_inter_mode.
func (e *VP8Encoder) estimateFastBPredIntraModeScore(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, bestSSE int, quant *vp8enc.MacroblockQuant, ctx *fastInterModeLoopContext) (vp8enc.InterFrameMacroblockMode, int, int, int, int, bool) {
	if quant == nil {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
	}
	// e is always non-nil on the inter picker entry path; the prior nil
	// guard was dead code.
	zbinOverQuant := e.rc.currentZbinOverQuant
	actZbinAdj := 0
	if e.activityMapValid {
		if adjustment, ok := e.tunedZbinAdjustment(mbRow, mbCol); ok {
			actZbinAdj = adjustment
		}
	}
	fastQuant := e.libvpxUseFastQuantForPick()
	analysisImg := &e.analysis.Img
	refs := ctx.lumaIntraRefs(e, mbRow, mbCol)
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
	rdMult, rdDiv := e.libvpxRDConstantsWithZbinForFrame(qIndex, zbinOverQuant)
	if e.activityMapValid {
		rdMult = e.tunedRDMultiplier(rdMult, mbRow, mbCol)
	}
	quantY1 := &quant.Y1
	var modes [16]vp8common.BPredictionMode
	var predictor [256]byte
	rate := e.interIntraReferenceRate() + e.interIntraYModeRate(vp8common.BPred)
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
			modeRate := libvpxInterFastBpredModeCostWithProbs(bMode, e.modeProbs.BMode[:])
			modeDist := vp8enc.BPredBlockSSE(src, mbRow, mbCol, block, blockPred[:], 4)
			modeCost := vp8enc.RDCost(rdMult, rdDiv, modeRate, modeDist)
			if modeCost < bestCost {
				bestMode = bMode
				bestRate = modeRate
				bestDist = modeDist
				bestCost = modeCost
				bestPred = blockPred
			}
		}
		modes[block] = bestMode
		vp8enc.CopyBPredBlock(bestPred[:], predictor[:], 16, block)

		// Mirror libvpx vp8_encode_intra4x4block: re-predict, residual,
		// DCT, quantize/dequant, IDCT-add into the analysis Y plane so the
		// next sub-block's predictor neighbors come from reconstructed
		// pixels (not raw predictor). pick_intra4x4block calls this at
		// the end of each block iteration (encodeintra.c:45).
		var input [16]int16
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		vp8enc.FillBPredResidual4x4(src, mbRow, mbCol, block, bestPred[:], &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		eob := vp8enc.QuantizeDecisionBlockWithActivity(fastQuant, &dct, quantY1, zbinOverQuant, actZbinAdj, &qcoeff, &dqcoeff)
		var recon [16]byte
		if eob > 1 {
			dsp.IDCT4x4Add(&dqcoeff, bestPred[:], 4, recon[:], 4)
		} else {
			dsp.DCOnlyIDCT4x4Add(dqcoeff[0], bestPred[:], 4, recon[:], 4)
		}
		vp8enc.CopyBPredBlock(recon[:], y, yStride, block)

		rate += bestRate
		distortion += bestDist
		if distortion > bestSSE {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
		}
	}
	variance, sse := macroblockLumaVarianceSSEFromPredictor(src, mbRow, mbCol, &predictor)
	return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: vp8common.DCPred, BModes: modes}, vp8enc.RDCost(rdMult, rdDiv, rate, variance), variance, sse, rate, true
}

func macroblockLumaVarianceSSEFromPredictor(src vp8enc.SourceImage, mbRow int, mbCol int, pred *[256]byte) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	sum := 0
	sse := 0
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		for col := range 16 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*16+col])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}

// macroblockLumaVarianceSSEAgainstBuffer is the libvpx
// vpx_variance16x16(*(b->base_src), b->src_stride, x->e_mbd.predictor, 16)
// shape: source macroblock against a contiguous stride-16 predictor
// buffer. In-bounds macroblocks take the fused SIMD (sum, sse) kernel;
// partial-edge macroblocks fall back to the clamped scalar walk, which is
// value-identical to the frame-backed MacroblockLumaVarianceSSE because
// the 16x16 predictor region needs no clamping (analysis frames are
// macroblock-aligned, so its clamp was the identity).
func macroblockLumaVarianceSSEAgainstBuffer(src vp8enc.SourceImage, mbRow int, mbCol int, pred *[256]byte) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		sum, sse := dsp.VarianceBlock16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, &pred[0], 16)
		return sse - ((sum * sum) >> 8), sse
	}
	return macroblockLumaVarianceSSEFromPredictor(src, mbRow, mbCol, pred)
}
