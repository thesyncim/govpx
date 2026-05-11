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
	src        vp8enc.SourceImage
	ref        *vp8common.Image
	mbRow      int
	mbCol      int
	mode       *vp8enc.InterFrameMacroblockMode
	above      *vp8enc.InterFrameMacroblockMode
	left       *vp8enc.InterFrameMacroblockMode
	aboveLeft  *vp8enc.InterFrameMacroblockMode
	aboveTok   *vp8enc.TokenContextPlanes
	leftTok    *vp8enc.TokenContextPlanes
	quant      *vp8enc.MacroblockQuant
	qIndex     int
	segmentID  uint8
	refRate    int
	modeCounts vp8enc.InterModeCounts
	bestRefMV  vp8enc.MotionVector
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
	var decMode vp8dec.MacroblockMode
	convertInterFrameMode(ctx.mode, &decMode)
	predMode := decMode
	predMode.MBSkipCoeff = true
	var zeroTokens vp8dec.MacroblockTokens
	if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ctx.ref, ctx.mbRow, ctx.mbCol, &predMode, &zeroTokens, &e.dequants[ctx.segmentID], &e.reconstructScratch) {
		return interResidualRDAccounting{}, false
	}

	modeRate := e.interMotionModeRateWithReferenceRateAndModeContext(ctx.mode, ctx.left, ctx.above, ctx.refRate, ctx.modeCounts, ctx.bestRefMV, libvpxRDNewMVBitCostWeight)
	refCost := boolBitCost(e.refProbIntra, 1) + ctx.refRate
	otherCost := e.interMacroblockSkipRate(false)
	if breakout, predictionDist := staticInterRDEncodeBreakoutDistortion(ctx.src, &e.analysis.Img, ctx.mbRow, ctx.mbCol, ctx.quant, e.opts.StaticThreshold); breakout {
		rd := rdModeScoreWithZbin(ctx.qIndex, zbinOverQuant, 500, predictionDist)
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

	var coeffs vp8enc.MacroblockCoefficients
	coeffDst := &coeffs
	if e.interRDCoeffCacheScratchTarget != nil {
		e.interRDCoeffCacheScratchTarget.coeffs = vp8enc.MacroblockCoefficients{}
		e.interRDCoeffCacheScratchTarget.coeffsValid = false
		coeffDst = &e.interRDCoeffCacheScratchTarget.coeffs
	}
	is4x4 := interFrameModeUses4x4Tokens(ctx.mode.Mode)
	// Plumb the encoder's scratch DCT cache (when an RD picker pass is
	// active) through to buildPredictedMacroblockCoefficients so each
	// candidate's post-FDCT DCT inputs are staged. The picker swaps the
	// winner / scratch slot indices when becameBest fires, leaving the
	// winning candidate's DCTs accessible to the accepted-mode coefficient
	// build without re-running predict + residual gather + FDCT.
	stats := buildPredictedMacroblockCoefficientsInternal(&predictedMacroblockCoefficientArgs{
		coefProbs:     e.pickerCoefProbs(),
		src:           ctx.src,
		mbRow:         ctx.mbRow,
		mbCol:         ctx.mbCol,
		pred:          &e.analysis.Img,
		aboveTok:      ctx.aboveTok,
		leftTok:       ctx.leftTok,
		quant:         ctx.quant,
		qIndex:        ctx.qIndex,
		zbinOverQuant: e.rc.currentZbinOverQuant,
		zbinModeBoost: interZbinModeBoost(ctx.mode),
		is4x4:         is4x4,
		intra:         false,
		fastQuant:     e.libvpxUseFastQuantForPick(),
		optimize:      false,
		collectStats:  true,
		coeffs:        coeffDst,
		cacheOut:      e.interRDCoeffCacheScratchTarget,
	})
	rateUV := stats.rateUV
	rate2 := modeRate + otherCost + stats.rateY + rateUV
	distortion2 := stats.distortionY + stats.distortionUV
	mbSkipCoeff := stats.tteob == 0
	var staleY2 staleY2Snapshot
	if oracleTraceBuild && !is4x4 {
		staleY2 = makeOracleStaleY2Snapshot(coeffDst.EOB[24], coeffDst.QCoeff[24])
	}
	if mbSkipCoeff {
		rate2 -= stats.rateY + stats.rateUV
		rateUV = 0
		skipBackout := e.interMacroblockSkipRate(true) - e.interMacroblockSkipRate(false)
		rate2 += skipBackout
		otherCost += skipBackout
	}
	rd := rdModeScoreWithZbin(ctx.qIndex, zbinOverQuant, rate2, distortion2)
	yrd := rdModeScoreWithZbin(ctx.qIndex, zbinOverQuant, rate2-rateUV-otherCost-refCost, distortion2-stats.distortionUV)
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
		mbSkipCoeff:  mbSkipCoeff,
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
		modeRate = e.interMotionModeRateWithReferenceRateAndModeContext(mode, left, above, refRate, ctx.modeMVs.counts, ctx.bestRefMV, libvpxFastNewMVBitCostWeight)
	} else {
		modeRate = e.fastInterMotionModeRateWithReferenceRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate)
	}
	variance, sse := macroblockLumaMotionVarianceSSECached(src, ref, mbRow, mbCol, mode.MV, ctx)
	zbinOverQuant := e.rc.currentZbinOverQuant
	score := rdModeScoreWithZbin(qIndex, zbinOverQuant, modeRate, variance)
	if mode.RefFrame == vp8common.LastFrame && mode.Mode == vp8common.ZeroMV {
		adj := 100
		if e.fastZeroMVLastAdjustmentEligible(mbRows, mbCols) {
			adj = fastZeroMVLastRDAdjustment(mbRow, mbCol, above, left, aboveLeft)
		}
		// Dot-artifact bias overrides the local-motion reduction with a 1.5x
		// penalty (libvpx pickinter.c). Skin macroblocks reset the multiplier
		// to 100 so face-coloured blocks aren't pushed off ZEROMV-LAST.
		if e.checkDotArtifactCandidateY(src, ref, mbRow, mbCol, mbRows, mbCols) {
			adj = 150
		}
		if e.macroblockIsSkin(mbRow, mbCol, mbCols) {
			adj = 100
		}
		// libvpx denoiser pickmode_mv_bias: aggressive denoise scales ZEROMV
		// down (multiplier=75) so ZEROMV-LAST is preferred for noisy areas.
		// Non-aggressive denoise leaves the multiplier at 100.
		score = (score * adj * e.denoiserPickmodeMVBias()) / 10000
	}
	breakoutSkip := staticInterFastEncodeBreakout(src, ref, mbRow, mbCol, mode, quant, e.opts.StaticThreshold, sse)
	return score, variance, sse, modeRate, breakoutSkip, true
}

func macroblockLumaMotionVarianceSSECached(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, ctx *fastInterModeLoopContext) (int, int) {
	if ctx == nil {
		return macroblockLumaMotionVarianceSSE(src, ref, mbRow, mbCol, mv)
	}
	entry := &ctx.variance[fastInterVarianceCacheIndex(ref, mv)]
	if entry.set && entry.ref == ref && entry.mv == mv {
		return entry.variance, entry.sse
	}
	variance, sse := macroblockLumaMotionVarianceSSE(src, ref, mbRow, mbCol, mv)
	*entry = fastInterVarianceCacheEntry{set: true, ref: ref, mv: mv, variance: variance, sse: sse}
	return variance, sse
}

func (ctx *fastInterModeLoopContext) storeVariance(ref *vp8common.Image, mv vp8enc.MotionVector, variance int, sse int) {
	entry := &ctx.variance[fastInterVarianceCacheIndex(ref, mv)]
	*entry = fastInterVarianceCacheEntry{set: true, ref: ref, mv: mv, variance: variance, sse: sse}
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
	if index < 0 || index >= len(e.skinMap) {
		return false
	}
	return e.skinMap[index] != 0
}

func (e *VP8Encoder) fastZeroMVLastAdjustmentEligible(mbRows int, mbCols int) bool {
	if e.opts.ScreenContentMode != 0 {
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
