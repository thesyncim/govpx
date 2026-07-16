package govpx

import (
	"unsafe"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func (e *VP8Encoder) estimateInterResidualRDScoreWithReferenceRateAndSkip(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, segmentID uint8, refRate int) (int, bool, bool) {
	acct, ok := e.estimateInterResidualRDAccounting(src, ref, mbRow, mbCol, mbRows, mbCols, mode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, refRate)
	return acct.rd, acct.rdLoopSkip, ok
}

type interResidualRDAccounting struct {
	rd           int
	yrd          int
	rate2        int
	rateY        int
	rateUV       int
	distortion2  int
	distortionUV int
	otherCost    int
	refCost      int
	rdLoopSkip   bool
	mbSkipCoeff  bool
	staleY2      staleY2Snapshot
}

type interResidualRDContext struct {
	aboveTok               *vp8enc.TokenContextPlanes
	quant                  *vp8enc.MacroblockQuant
	leftTok                *vp8enc.TokenContextPlanes
	ref                    *vp8common.Image
	mode                   *vp8enc.InterFrameMacroblockMode
	above                  *vp8enc.InterFrameMacroblockMode
	left                   *vp8enc.InterFrameMacroblockMode
	aboveLeft              *vp8enc.InterFrameMacroblockMode
	src                    vp8enc.SourceImage
	mbCol                  int
	mbRow                  int
	qIndex                 int
	refRate                int
	bestRefMV              vp8enc.MotionVector
	modeCounts             vp8enc.InterModeCounts
	segmentID              uint8
	suppressStaticBreakout bool
	denoiseActive          bool
}

func (e *VP8Encoder) estimateInterResidualRDAccounting(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, segmentID uint8, refRate int) (interResidualRDAccounting, bool) {
	if mode == nil {
		return interResidualRDAccounting{}, false
	}
	signBias := e.interFrameSignBias()
	modeCounts := vp8enc.InterFrameModeCounts(above, left, aboveLeft, mode.RefFrame, signBias)
	bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, mode.RefFrame, mbRow, mbCol, mbRows, mbCols, signBias)
	ctx := interResidualRDContext{
		src:        src,
		ref:        ref,
		mbRow:      mbRow,
		mbCol:      mbCol,
		mode:       mode,
		above:      above,
		left:       left,
		aboveLeft:  aboveLeft,
		aboveTok:   aboveTok,
		leftTok:    leftTok,
		quant:      quant,
		qIndex:     qIndex,
		segmentID:  segmentID,
		refRate:    refRate,
		modeCounts: modeCounts,
		bestRefMV:  bestRefMV,
	}
	return e.estimateInterResidualRDAccountingWithModeContext(&ctx)
}

func (e *VP8Encoder) estimateInterResidualRDAccountingWithModeContext(ctx *interResidualRDContext) (interResidualRDAccounting, bool) {
	if ctx == nil || ctx.ref == nil || ctx.mode == nil || ctx.quant == nil || ctx.segmentID >= vp8common.MaxMBSegments {
		return interResidualRDAccounting{}, false
	}
	zbinOverQuant := e.rc.currentZbinOverQuant
	actZbinAdj := 0
	if e.activityMapValid {
		if adjustment, ok := e.tunedZbinAdjustment(ctx.mbRow, ctx.mbCol); ok {
			actZbinAdj = adjustment
		}
	}
	var decMode vp8dec.MacroblockMode
	vp8enc.ConvertInterFrameMode(ctx.mode, &decMode)
	predMode := decMode
	predMode.MBSkipCoeff = true
	// segmentID is validated to [0, MaxMBSegments=4) at the caller
	// boundary; AND-mask with 3 (pow2 - 1) elides the bounds check on
	// e.dequants without changing semantics.
	if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ctx.ref, ctx.mbRow, ctx.mbCol, &predMode, nil, &e.dequants[ctx.segmentID&3], &e.reconstructScratch) {
		return interResidualRDAccounting{}, false
	}

	modeRate := e.interMotionModeRateWithReferenceRateAndModeContext(ctx.mode, ctx.left, ctx.above, ctx.refRate, ctx.modeCounts, ctx.bestRefMV, vp8enc.RDNewMVBitCostWeight)
	refCost := e.interInterReferenceRate(ctx.refRate)
	otherCost := e.interMacroblockSkipRate(false)
	if !ctx.suppressStaticBreakout {
		if breakout, predictionDist := vp8enc.StaticInterRDEncodeBreakoutDistortion(ctx.src, &e.analysis.Img, ctx.mbRow, ctx.mbCol, ctx.quant, e.interStaticThresholdForSegment(ctx.segmentID)); breakout {
			rd := e.rdModeScoreWithZbin(ctx.qIndex, zbinOverQuant, 500, predictionDist)
			if e.activityMapValid {
				rd = e.tunedRDModeScoreWithZbin(ctx.qIndex, zbinOverQuant, ctx.mbRow, ctx.mbCol, 500, predictionDist)
			}
			return interResidualRDAccounting{
				rd:          rd,
				yrd:         rd,
				rate2:       500,
				distortion2: predictionDist,
				otherCost:   otherCost,
				refCost:     refCost,
				rdLoopSkip:  true,
				mbSkipCoeff: true,
			}, true
		}
	}

	var coeffs vp8enc.MacroblockCoefficients
	is4x4 := vp8enc.InterFrameModeUses4x4Tokens(ctx.mode.Mode)
	// Plumb the encoder's scratch DCT cache (when an RD picker pass is
	// active) through to buildPredictedMacroblockCoefficients so each
	// candidate's post-FDCT DCT inputs are staged. The picker swaps the
	// winner / scratch slot indices when becameBest fires, leaving the
	// winning candidate's DCTs accessible to the accepted-mode coefficient
	// build without re-running predict + residual gather + FDCT.
	var trace predictedMacroblockCoefficientTrace
	if oracleTraceBuild {
		trace = newPickerUVQuantizeTrace(e, ctx.mode)
	}
	stats := buildPredictedMacroblockCoefficientsInternal(&predictedMacroblockCoefficientArgs{
		coefProbs:           e.pickerCoefProbs(),
		coefTokenCosts:      e.pickerCoefTokenCosts(),
		src:                 ctx.src,
		mbRow:               ctx.mbRow,
		mbCol:               ctx.mbCol,
		pred:                &e.analysis.Img,
		aboveTok:            ctx.aboveTok,
		leftTok:             ctx.leftTok,
		quant:               ctx.quant,
		qIndex:              ctx.qIndex,
		zbinOverQuant:       zbinOverQuant,
		zbinModeBoost:       vp8enc.InterZbinModeBoost(ctx.mode),
		actZbinAdj:          actZbinAdj,
		is4x4:               is4x4,
		splitPartitionValid: ctx.mode.Mode == vp8common.SplitMV,
		splitPartition:      ctx.mode.Partition,
		intra:               false,
		fastQuant:           e.libvpxUseFastQuantForPick(),
		optimize:            false,
		collectStats:        true,
		coeffs:              &coeffs,
		cacheOut:            e.interRDCoeffCacheScratchTarget,
		trace:               trace,
	})
	rateUV := stats.rateUV
	rate2 := modeRate + otherCost + stats.rateY + rateUV
	distortion2 := stats.distortionY + stats.distortionUV
	mbSkipCoeff := stats.tteob == 0
	var staleY2 staleY2Snapshot
	if oracleTraceBuild && !is4x4 {
		staleY2 = makeOracleStaleY2Snapshot(coeffs.EOB[24], coeffs.QCoeff[24])
	}
	if mbSkipCoeff {
		rate2 -= stats.rateY + stats.rateUV
		rateUV = 0
		skipBackout := e.interMacroblockSkipRate(true) - e.interMacroblockSkipRate(false)
		rate2 += skipBackout
		otherCost += skipBackout
	}
	rd := e.rdModeScoreWithZbin(ctx.qIndex, zbinOverQuant, rate2, distortion2)
	yrd := e.rdModeScoreWithZbin(ctx.qIndex, zbinOverQuant, rate2-rateUV-otherCost-refCost, distortion2-stats.distortionUV)
	if e.activityMapValid {
		rd = e.tunedRDModeScoreWithZbin(ctx.qIndex, zbinOverQuant, ctx.mbRow, ctx.mbCol, rate2, distortion2)
		yrd = e.tunedRDModeScoreWithZbin(ctx.qIndex, zbinOverQuant, ctx.mbRow, ctx.mbCol, rate2-rateUV-otherCost-refCost, distortion2-stats.distortionUV)
	}
	return interResidualRDAccounting{
		rd:           rd,
		yrd:          yrd,
		rate2:        rate2,
		rateY:        stats.rateY,
		rateUV:       rateUV,
		distortion2:  distortion2,
		distortionUV: stats.distortionUV,
		otherCost:    otherCost,
		refCost:      refCost,
		mbSkipCoeff:  mbSkipCoeff && !ctx.denoiseActive,
		staleY2:      staleY2,
	}, true
}

func (e *VP8Encoder) estimateFastInterModeScore(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int) (int, bool) {
	refRate := 1 << 30
	if mode != nil {
		refRate = e.interReferenceFrameRate(mode.RefFrame)
	}
	score, _, _, _, _, ok := e.estimateFastInterModeScoreWithReferenceRateAndSkip(src, ref, mbRow, mbCol, mbRows, mbCols, mode, above, left, aboveLeft, qIndex, refRate, nil)
	return score, ok
}

func (e *VP8Encoder) estimateFastInterModeScoreWithReferenceRateAndSkip(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, refRate int, quant *vp8enc.MacroblockQuant) (int, int, int, int, bool, bool) {
	return e.estimateFastInterModeScoreWithReferenceRateAndSkipCached(src, ref, mbRow, mbCol, mbRows, mbCols, mode, above, left, aboveLeft, qIndex, refRate, quant, nil)
}

func (e *VP8Encoder) estimateFastInterModeScoreWithReferenceRateAndSkipCached(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, refRate int, quant *vp8enc.MacroblockQuant, ctx *fastInterModeLoopContext) (int, int, int, int, bool, bool) {
	if ref == nil || mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode == vp8common.SplitMV {
		return 0, 0, 0, 0, false, false
	}
	var modeRate int
	if ctx != nil {
		modeRate = e.interMotionModeRateWithReferenceRateAndModeContextAndCosts(mode, left, above, refRate, ctx.modeMVs.counts, ctx.bestRefMV, ctx.mvCosts, vp8enc.FastNewMVBitCostWeight)
	} else {
		modeRate = e.fastInterMotionModeRateWithReferenceRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate)
	}
	variance, sse := macroblockLumaMotionVarianceSSECached(src, ref, mbRow, mbCol, mode.MV, ctx)
	zbinOverQuant := e.rc.currentZbinOverQuant
	score := e.rdModeScoreWithZbin(qIndex, zbinOverQuant, modeRate, variance)
	if e.activityMapValid {
		score = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, modeRate, variance)
	}
	if mode.RefFrame == vp8common.LastFrame && mode.Mode == vp8common.ZeroMV {
		// Mirror libvpx vp8/encoder/pickinter.c vp8_pick_inter_mode (lines
		// 780-794) followed by evaluate_inter_mode (lines 519-528). Order:
		//   1. rd_adjustment = 100
		//   2. calculate_zeromv_rd_adjustment may set 80/90 when
		//      lf_zeromv_pct > 40 and neighbors have small motion.
		//   3. multiply by pickmode_mv_bias/100 (CONFIG_TEMPORAL_DENOISING).
		//   4. dot_artifact_candidate OVERRIDES rd_adjustment to 150
		//      (so the pickmode_mv_bias scaling is discarded).
		//   5. evaluate_inter_mode: when ZEROMV+LAST and denoise_aggressive
		//      (or closest_reference_frame==LAST), x->is_skin OVERRIDES
		//      rd_adj to 100 (so BOTH the local-motion scaling AND the
		//      pickmode_mv_bias scaling are discarded for skin MBs).
		//
		// Surfaced by splitmv-realtime-cpu0-64x64-noise3 frame-2 MB(2,3):
		// govpx applied (adj=100)*(pickmodeMVBias=75)/100=0.75 ZEROMV-LAST
		// discount on a skin MB; libvpx applied no discount. govpx picked
		// ZEROMV-LAST, libvpx picked NEWMV-LAST mv=(2,-12). The cascading
		// mode-info divergence corrupted frame 2's entropy stream and the
		// rest of the clip.
		adj := 100
		pickmodeMVBias := e.denoiserPickmodeMVBias()
		if e.fastZeroMVLastAdjustmentEligible(mbRows, mbCols) {
			adj = fastZeroMVLastRDAdjustment(mbRow, mbCol, above, left, aboveLeft)
		}
		if e.checkDotArtifactCandidate(src, ref, mbRow, mbCol, mbRows, mbCols) {
			// libvpx dot_artifact override applies AFTER the pickmode_mv_bias
			// multiply, so the final multiplier is 1.5x — discard the bias.
			adj = 150
			pickmodeMVBias = 100
		}
		if e.macroblockIsSkin(mbRow, mbCol, mbCols) {
			// libvpx evaluate_inter_mode resets rd_adj to 100 for skin MBs
			// before applying the multiplier, discarding both the
			// local-motion adjustment AND the pickmode_mv_bias scaling.
			adj = 100
			pickmodeMVBias = 100
		}
		score = (score * adj * pickmodeMVBias) / 10000
	}
	breakoutSkip := vp8enc.StaticInterFastEncodeBreakout(src, ref, mbRow, mbCol, mode, quant, e.interStaticThresholdForSegment(mode.SegmentID), sse)
	return score, variance, sse, modeRate, breakoutSkip, true
}

func (e *VP8Encoder) estimateFastInterModeScoreHot(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, refFrame vp8common.MVReferenceFrame, mbMode vp8common.MBPredictionMode, mv vp8enc.MotionVector, segmentID uint8, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, refRate int, quant *vp8enc.MacroblockQuant, ctx *fastInterModeLoopContext) (int, int, int, int, bool, bool) {
	if ref == nil || refFrame == vp8common.IntraFrame || mbMode == vp8common.SplitMV {
		return 0, 0, 0, 0, false, false
	}
	// Reuse the loop context's candidate slot instead of rebuilding the
	// ~100-byte struct literal per candidate (libvpx mutates the shared
	// mbmi in place). Only the four fields the scoring callees read are
	// written; the rest stay zero from the per-MB context init.
	mode := &ctx.candMode
	mode.RefFrame = refFrame
	mode.Mode = mbMode
	mode.MV = mv
	mode.SegmentID = segmentID
	modeRate := e.interMotionModeRateWithReferenceRateAndModeContextAndCosts(mode, left, above, refRate, ctx.modeMVs.counts, ctx.bestRefMV, ctx.mvCosts, vp8enc.FastNewMVBitCostWeight)
	variance, sse := macroblockLumaMotionVarianceSSECached(src, ref, mbRow, mbCol, mv, ctx)
	zbinOverQuant := e.rc.currentZbinOverQuant
	// Per-MB cached RDCOST constants (see fastInterModeLoopContext.rdMult);
	// value-identical to rdModeScoreWithZbin / tunedRDModeScoreWithZbin.
	rdMult, rdDiv := ctx.rdConstants(e, qIndex, zbinOverQuant, mbRow, mbCol)
	score := vp8enc.RDCost(rdMult, rdDiv, modeRate, variance)
	if refFrame == vp8common.LastFrame && mbMode == vp8common.ZeroMV {
		adj := 100
		pickmodeMVBias := e.denoiserPickmodeMVBias()
		if e.fastZeroMVLastAdjustmentEligible(mbRows, mbCols) {
			adj = fastZeroMVLastRDAdjustment(mbRow, mbCol, above, left, aboveLeft)
		}
		if e.checkDotArtifactCandidate(src, ref, mbRow, mbCol, mbRows, mbCols) {
			adj = 150
			pickmodeMVBias = 100
		}
		if e.macroblockIsSkin(mbRow, mbCol, mbCols) {
			adj = 100
			pickmodeMVBias = 100
		}
		score = (score * adj * pickmodeMVBias) / 10000
	}
	breakoutSkip := vp8enc.StaticInterFastEncodeBreakout(src, ref, mbRow, mbCol, mode, quant, e.interStaticThresholdForSegment(segmentID), sse)
	return score, variance, sse, modeRate, breakoutSkip, true
}

func macroblockLumaMotionVarianceSSECached(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, ctx *fastInterModeLoopContext) (int, int) {
	if ctx == nil {
		return macroblockLumaMotionVarianceSSE(src, ref, mbRow, mbCol, mv)
	}
	index := fastInterVarianceCacheIndex(ref, mv)
	entry := &ctx.variance[index]
	if ctx.varianceSet&(uint16(1)<<uint(index)) != 0 && entry.ref == ref && entry.mv == mv {
		return int(entry.variance), int(entry.sse)
	}
	variance, sse := macroblockLumaMotionVarianceSSE(src, ref, mbRow, mbCol, mv)
	*entry = fastInterVarianceCacheEntry{ref: ref, mv: mv, variance: int32(variance), sse: int32(sse)}
	ctx.varianceSet |= uint16(1) << uint(index)
	return variance, sse
}

func (ctx *fastInterModeLoopContext) storeVariance(ref *vp8common.Image, mv vp8enc.MotionVector, variance int32, sse int32) {
	index := fastInterVarianceCacheIndex(ref, mv)
	entry := &ctx.variance[index]
	*entry = fastInterVarianceCacheEntry{ref: ref, mv: mv, variance: variance, sse: sse}
	ctx.varianceSet |= uint16(1) << uint(index)
}

func fastInterVarianceCacheIndex(ref *vp8common.Image, mv vp8enc.MotionVector) int {
	h := uintptr(unsafe.Pointer(ref)) >> 4
	h ^= uintptr(uint16(mv.Row))*17 + uintptr(uint16(mv.Col))*31
	return int(h & (fastInterVarianceCacheSize - 1))
}

func (e *VP8Encoder) macroblockIsSkin(mbRow int, mbCol int, mbCols int) bool {
	if len(e.skinMap) == 0 {
		return false
	}
	index := mbRow*mbCols + mbCol
	// Uint range check folds the (index < 0) and (index >= len) guards
	// into one branch and elides the bounds check on e.skinMap[index].
	if uint(index) >= uint(len(e.skinMap)) {
		return false
	}
	return e.skinMap[index] != 0
}

// fastZeroMVLastAdjustmentEligible mirrors libvpx vp8/encoder/pickinter.c
// vp8_pick_inter_mode (line 756: `if (cpi->Speed < 12) calculate_zeromv_rd_adjustment(...)`)
// guarded by the inner `if (cpi->lf_zeromv_pct > 40)` check at line 522. libvpx
// suppresses the local-motion ZEROMV-LAST RD-adjustment entirely once
// cpi->Speed >= 12 (RT autoSpeed evolves up to 16 per vp8_auto_select_speed
// rdopt.c line 312, so the gate fires once the auto-selected Speed crosses
// into the heavy-RT range where ZEROMV is already favored by rate-control).
//
// The Speed-conditioned half of this gate consults e.libvpxCPUUsed() --
// the same deterministic cpi->Speed model every other speed-feature gate
// reads (see the drop-parity audit note in vp8_encoder_config.go). The
// downstream effect on encode_breakout sensitivity: rd_adj scales
// this_rd, and the encode_breakout-skip path only commits when the
// scaled this_rd wins the best_rd comparison, so the gate indirectly
// controls how aggressive ZEROMV-LAST is in claiming the
// encode_breakout fast path at higher Speed levels.
func (e *VP8Encoder) fastZeroMVLastAdjustmentEligible(mbRows int, mbCols int) bool {
	if e.opts.ScreenContentMode != 0 {
		return false
	}
	if e.opts.Deadline == DeadlineRealtime && e.libvpxCPUUsed() >= 12 {
		return false
	}
	required := mbRows * mbCols
	return required > 0 && e.lastInterZeroMVCount*100 > 40*required
}

func fastZeroMVLastRDAdjustment(mbRow int, mbCol int, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode) int {
	localMotion := 0
	if interModeHasSmallMotion(left) {
		localMotion++
	}
	if interModeHasSmallMotion(aboveLeft) {
		localMotion++
	}
	if interModeHasSmallMotion(above) {
		localMotion++
	}
	if ((mbRow == 0 || mbCol == 0) && localMotion > 0) || localMotion > 2 {
		return 80
	}
	if localMotion > 0 {
		return 90
	}
	return 100
}

func interModeHasSmallMotion(mode *vp8enc.InterFrameMacroblockMode) bool {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame {
		return false
	}
	row := int(mode.MV.Row)
	if row < 0 {
		row = -row
	}
	col := int(mode.MV.Col)
	if col < 0 {
		col = -col
	}
	return row < 8 && col < 8
}
