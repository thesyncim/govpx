package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

type interSplitModeRDContext struct {
	src        vp8enc.SourceImage
	ref        interAnalysisReference
	mbRow      int
	mbCol      int
	mbCols     int
	bestRefMV  vp8enc.MotionVector
	modeCounts vp8enc.InterModeCounts
	qIndex     int
	segmentID  uint8
	mvthresh   int
	bestYRD    int
	above      *vp8enc.InterFrameMacroblockMode
	left       *vp8enc.InterFrameMacroblockMode
	aboveLeft  *vp8enc.InterFrameMacroblockMode
	aboveTok   *vp8enc.TokenContextPlanes
	leftTok    *vp8enc.TokenContextPlanes
	quant      *vp8enc.MacroblockQuant
}

type interSplitModeRDResult struct {
	mode         vp8enc.InterFrameMacroblockMode
	rd           int
	yrd          int
	rate         int
	rateY        int
	rateUV       int
	distortion   int
	distortionUV int
	rdLoopSkip   bool
	mbSkipCoeff  bool
}

func (e *VP8Encoder) selectInterFrameSplitModeRDScore(ctx *interSplitModeRDContext) (interSplitModeRDResult, bool) {
	// libvpx: vp8_rd_pick_inter_mode SPLITMV branch picks
	// x->rd_threshes[THR_NEW{1,2,3}] based on vp8_ref_frame_order[mode_index]
	// (1=LAST, 2=GOLDEN, 3=ALTREF) and feeds it into
	// vp8_rd_pick_best_mbsegmentation as bsi->mvthresh, which the per-label
	// loop divides by label_count to gate NEW4X4 motion searches.
	if ctx == nil {
		return interSplitModeRDResult{}, false
	}
	bestSet := false
	bestSegmentYRD := ctx.bestYRD
	if bestSegmentYRD <= 0 {
		bestSegmentYRD = maxInt()
	}
	var bestMode vp8enc.InterFrameMacroblockMode
	var bestShape splitMotionShapeResult
	var splitSeeds splitMotionSearchSeeds
	search := e.interAnalysisSearchConfig()
	compressor := e.interAnalysisCompressorSpeed()
	zbinOverQuant := e.rc.currentZbinOverQuant
	actZbinAdj := 0
	if e.activityMapValid {
		if adjustment, ok := e.tunedZbinAdjustment(ctx.mbRow, ctx.mbCol); ok {
			actZbinAdj = adjustment
		}
	}
	fastQuant := e.libvpxUseFastQuantForPick()
	coefProbs := e.pickerCoefProbs()
	mvCosts := e.currentMotionVectorCostTables()

	errorPerBit := 0
	if e.activityMapValid {
		errorPerBit = e.tunedErrorPerBit(ctx.qIndex, ctx.mbRow, ctx.mbCol)
	}
	tryPartition := func(partition int) {
		var labelRD splitMotionLabelRDEvaluator
		labelRD.init(zbinOverQuant, actZbinAdj, ctx.aboveTok, ctx.leftTok, fastQuant, false)
		if e.activityMapValid {
			rdMult, rdDiv := libvpxRDConstantsWithZbin(ctx.qIndex, zbinOverQuant)
			rdMult = e.tunedRDMultiplier(rdMult, ctx.mbRow, ctx.mbCol)
			labelRD.setRDConstants(rdMult, rdDiv)
		}
		overheadRate := mbSplitPartitionRate(uint8(partition)) + interPredictionModeRate(vp8common.SplitMV, ctx.modeCounts)
		overheadRD := rdModeScoreWithZbin(ctx.qIndex, zbinOverQuant, overheadRate, 0)
		if e.activityMapValid {
			overheadRD = e.tunedRDModeScoreWithZbin(ctx.qIndex, zbinOverQuant, ctx.mbRow, ctx.mbCol, overheadRate, 0)
		}
		shapeCtx := splitMotionShapeContext{
			src:                 ctx.src,
			ref:                 ctx.ref.Img,
			refFrame:            ctx.ref.Frame,
			mbRow:               ctx.mbRow,
			mbCol:               ctx.mbCol,
			bestRefMV:           ctx.bestRefMV,
			qIndex:              ctx.qIndex,
			errorPerBit:         errorPerBit,
			partition:           partition,
			left:                ctx.left,
			above:               ctx.above,
			search:              search,
			compressor:          compressor,
			seeds:               &splitSeeds,
			mvProbs:             &e.modeProbs.MV,
			mvCosts:             mvCosts,
			subMVRefProbs:       &e.subMVRefProbs,
			mvthresh:            ctx.mvthresh,
			labelRD:             &labelRD,
			quant:               ctx.quant,
			coefProbs:           coefProbs,
			segmentYRDCap:       bestSegmentYRD,
			segmentOverheadRate: overheadRate,
			segmentOverheadRD:   overheadRD,
		}
		shape := shapeCtx.selectShape()
		if !shape.OK {
			return
		}
		// libvpx: when this_segment_rd >= bsi->segment_rd at any label,
		// rd_check_segment returns without updating bsi (no bsi.r/bsi.d
		// commit). govpx mirrors that — the abandoned shape is not
		// considered for best mode and does not refresh bestSegmentYRD.
		if shape.Cutoff {
			return
		}
		mode := shape.Mode
		mode.SegmentID = ctx.segmentID
		if compressor != 0 && partition == 2 {
			splitSeeds = splitMotionSearchSeedsFrom8x8(&mode)
		}
		// libvpx:
		//
		//	if (this_segment_rd < bsi->segment_rd)
		//	    bsi->segment_rd = this_segment_rd;
		//
		if shape.SegmentYRD < bestSegmentYRD {
			bestSegmentYRD = shape.SegmentYRD
			bestSet = true
			bestMode = mode
			bestShape = shape
		}
	}

	if compressor != 0 {
		tryPartition(2)
		if bestSet {
			tryPartition(1)
			tryPartition(0)
			if e.interAnalysisNoSkipBlock4x4Search() || bestMode.Partition == 2 {
				tryPartition(3)
			}
		}
	} else {
		for _, partition := range e.interAnalysisSplitPartitionOrder() {
			tryPartition(partition)
		}
	}
	if !bestSet {
		return interSplitModeRDResult{}, false
	}
	acct, ok := e.estimateInterSplitResidualRDAccounting(ctx, &bestMode, &bestShape)
	if !ok {
		return interSplitModeRDResult{}, false
	}
	return interSplitModeRDResult{
		mode:         bestMode,
		rd:           acct.rd,
		yrd:          acct.yrd,
		rate:         acct.rate2,
		rateY:        acct.rateY,
		rateUV:       acct.rateUV,
		distortion:   acct.distortion2,
		distortionUV: acct.distortionUV,
		rdLoopSkip:   acct.rdLoopSkip,
		mbSkipCoeff:  acct.mbSkipCoeff,
	}, true
}

func (e *VP8Encoder) estimateInterSplitResidualRDAccounting(ctx *interSplitModeRDContext, mode *vp8enc.InterFrameMacroblockMode, shape *splitMotionShapeResult) (interResidualRDAccounting, bool) {
	if ctx == nil || mode == nil || shape == nil || ctx.ref.Img == nil || ctx.quant == nil || ctx.segmentID >= vp8common.MaxMBSegments {
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
	convertInterFrameMode(mode, &decMode)
	decMode.MBSkipCoeff = true
	if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ctx.ref.Img, ctx.mbRow, ctx.mbCol, &decMode, nil, &e.dequants[ctx.segmentID&3], &e.reconstructScratch) {
		return interResidualRDAccounting{}, false
	}

	var coeffs vp8enc.MacroblockCoefficients
	stats := buildPredictedMacroblockCoefficientsInternal(&predictedMacroblockCoefficientArgs{
		coefProbs:           e.pickerCoefProbs(),
		src:                 ctx.src,
		mbRow:               ctx.mbRow,
		mbCol:               ctx.mbCol,
		pred:                &e.analysis.Img,
		aboveTok:            ctx.aboveTok,
		leftTok:             ctx.leftTok,
		quant:               ctx.quant,
		qIndex:              ctx.qIndex,
		zbinOverQuant:       zbinOverQuant,
		zbinModeBoost:       splitInterModeZbinBoost,
		actZbinAdj:          actZbinAdj,
		is4x4:               true,
		splitPartitionValid: true,
		splitPartition:      mode.Partition,
		intra:               false,
		fastQuant:           e.libvpxUseFastQuantForPick(),
		optimize:            false,
		collectStats:        true,
		coeffs:              &coeffs,
		cacheOut:            e.interRDCoeffCacheScratchTarget,
	})

	refCost := e.interInterReferenceRate(e.interReferenceFrameRateForReference(ctx.ref))
	otherCost := e.interMacroblockSkipRate(false)
	rateUV := stats.rateUV
	rate2 := shape.SegmentRate + rateUV + otherCost + refCost
	distortion2 := shape.SegmentDistortion + stats.distortionUV
	uvTTEOB := 0
	for block := 16; block < 24; block++ {
		uvTTEOB += int(coeffs.EOB[block])
	}
	// SPLITMV returnrate is the RD picker's rate, not the later accepted
	// coefficient rebuild's packet skip state. Libvpx keeps rd_inter4x4_uv's
	// token rate here even when the accepted MB is ultimately packet-skipped.
	mbSkipCoeff := shape.SegmentTTEOB+uvTTEOB == 0 && stats.rateUV == 0
	if mbSkipCoeff {
		rate2 -= shape.SegmentYRate + stats.rateUV
		rateUV = 0
		skipBackout := e.interMacroblockSkipRate(true) - e.interMacroblockSkipRate(false)
		rate2 += skipBackout
		otherCost += skipBackout
	}
	rd := rdModeScoreWithZbin(ctx.qIndex, zbinOverQuant, rate2, distortion2)
	yrd := rdModeScoreWithZbin(ctx.qIndex, zbinOverQuant, rate2-rateUV-otherCost-refCost, distortion2-stats.distortionUV)
	if e.activityMapValid {
		rd = e.tunedRDModeScoreWithZbin(ctx.qIndex, zbinOverQuant, ctx.mbRow, ctx.mbCol, rate2, distortion2)
		yrd = e.tunedRDModeScoreWithZbin(ctx.qIndex, zbinOverQuant, ctx.mbRow, ctx.mbCol, rate2-rateUV-otherCost-refCost, distortion2-stats.distortionUV)
	}
	return interResidualRDAccounting{
		rd:           rd,
		yrd:          yrd,
		rate2:        rate2,
		rateY:        shape.SegmentYRate,
		rateUV:       rateUV,
		distortion2:  distortion2,
		distortionUV: stats.distortionUV,
		otherCost:    otherCost,
		refCost:      refCost,
		mbSkipCoeff:  mbSkipCoeff,
	}, true
}

func (e *VP8Encoder) splitMVSubsearchThresholdForSlot(qIndex int, refs []interAnalysisReference, refCount int, refSlot int) int {
	thresholds := e.interModeRDThresholdsForReferences(qIndex, refs, refCount)
	return splitMVThresholdForRefSlot(thresholds, refSlot)
}

func splitMVThresholdForRefSlot(thresholds [libvpxInterModeCount]int, refSlot int) int {
	switch refSlot {
	case 1:
		return thresholds[libvpxThrNew1]
	case 2:
		return thresholds[libvpxThrNew2]
	default:
		return thresholds[libvpxThrNew3]
	}
}
