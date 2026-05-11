package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
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
	mode       vp8enc.InterFrameMacroblockMode
	rd         int
	yrd        int
	rate       int
	distortion int
	rdLoopSkip bool
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
	var splitSeeds splitMotionSearchSeeds

	tryPartition := func(partition int) {
		var labelRD splitMotionLabelRDEvaluator
		initSplitMotionLabelRDEvaluator(&labelRD, e.rc.currentZbinOverQuant, ctx.aboveTok, ctx.leftTok, e.libvpxUseFastQuantForPick(), false)
		overheadRate := mbSplitPartitionRate(uint8(partition)) + interPredictionModeRate(vp8common.SplitMV, ctx.modeCounts)
		overheadRD := rdModeScoreWithZbin(ctx.qIndex, e.rc.currentZbinOverQuant, overheadRate, 0)
		shape := selectInterFrameSplitMotionModeWithSegmentCutoff(ctx.src, ctx.ref.Img, ctx.ref.Frame, ctx.mbRow, ctx.mbCol, ctx.bestRefMV, ctx.qIndex, partition, ctx.left, ctx.above, e.interAnalysisSearchConfig(), e.interAnalysisCompressorSpeed(), &splitSeeds, &e.modeProbs.MV, ctx.mvthresh, &labelRD, ctx.quant, e.pickerCoefProbs(), bestSegmentYRD, overheadRD)
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
		if e.interAnalysisCompressorSpeed() != 0 && partition == 2 {
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
		}
	}

	if e.interAnalysisCompressorSpeed() != 0 {
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
	rdCtx := interResidualRDContext{
		src:        ctx.src,
		ref:        ctx.ref.Img,
		mbRow:      ctx.mbRow,
		mbCol:      ctx.mbCol,
		mode:       &bestMode,
		above:      ctx.above,
		left:       ctx.left,
		aboveLeft:  ctx.aboveLeft,
		aboveTok:   ctx.aboveTok,
		leftTok:    ctx.leftTok,
		quant:      ctx.quant,
		qIndex:     ctx.qIndex,
		segmentID:  ctx.segmentID,
		refRate:    e.interReferenceFrameRateForReference(ctx.ref),
		modeCounts: ctx.modeCounts,
		bestRefMV:  ctx.bestRefMV,
	}
	acct, ok := e.estimateInterResidualRDAccountingWithModeContext(&rdCtx)
	if !ok {
		return interSplitModeRDResult{}, false
	}
	return interSplitModeRDResult{
		mode:       bestMode,
		rd:         acct.rd,
		yrd:        acct.yrd,
		rate:       acct.rate2,
		distortion: acct.distortion2,
		rdLoopSkip: acct.rdLoopSkip,
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
