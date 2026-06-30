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
	//
	// libvpx vp8/encoder/rdopt.c:1986-2006 (vp8_rd_pick_inter_mode SPLITMV
	// case) and rdopt.c:1199-1335 (vp8_rd_pick_best_mbsegmentation):
	//   bsi.segment_rd = best_rd            (line 1210)
	//   ...
	//   if (this_segment_rd >= bsi->segment_rd) break;   (line 1165)
	//   if (this_segment_rd <  bsi->segment_rd)          (line 1169)
	//       bsi->segment_rd = this_segment_rd;
	//   ...
	//   tmp_rd = bsi.segment_rd
	//   if (tmp_rd < best_mode.yrd) { /* accept */ }
	//   else { this_rd = INT_MAX; disable_skip = 1; }
	// govpx mirrors that ladder via ctx.bestYRD → bestSegmentYRD →
	// shapeCtx.segmentYRDCap and the shape.SegmentYRD < bestSegmentYRD
	// commit below. ctx.bestYRD is the outer-loop running best_mode.yrd
	// (seeded as maxInt() in selectRDInterFrameModeDecision); the libvpx
	// initial sentinel is INT_MAX so the empty-cap path is identical.
	//
	// Cap-semantic audit (rules out this SPLITMV cap as the
	// BestARNR/GoodARNR pin-hold root cause; relocates to upstream NEWMV
	// picker quantize):
	//
	// The BestARNR -5-byte and GoodARNR -6-byte ARNR pins localize
	// pin-holds to a SPLITMV picker dropout at MB(0,0) frame 1: govpx's
	// selectInterFrameSplitModeRDScore returns ok=false (no partition
	// shape commits) while libvpx's vp8_rd_pick_best_mbsegmentation
	// succeeds and SPLITMV wins the mode-loop. The proximate cap value is
	// govpx ctx.bestYRD = 73707 (NEWMV.yrd) vs
	// libvpx best_mode.yrd = 129509, the gap being a 27280-bit deficit
	// in govpx's NEWMV picker rate_y for the same MV (8,16) / same ref /
	// same source — govpx's picker quantize emitting all-zero Y qcoeff
	// while libvpx's emits enough non-zero Y to yield rate_y=34799.
	//
	// The original hypothesis was that govpx applies a tighter
	// per-partition cap than libvpx. Direct cross-reference of the cap
	// ladder against libvpx
	// rdopt.c:1199-1335 + 1726-1748 + 1974-2006 confirms the semantic
	// is already byte-faithful:
	//
	//   - Cap value source (libvpx best_mode.yrd, set in update_best_mode
	//     at rdopt.c:1734-1736): RDCOST(rdmult, rddiv, rate2 - rate_uv -
	//     other_cost, distortion2 - distortion_uv) — uses the FINAL
	//     accumulated rate2 from calculate_final_rd_costs (post skip-cost
	//     backout). govpx ctx.bestYRD threads through
	//     estimateInterResidualRDAccountingWithModeContext yrd field
	//     (vp8_encoder_inter_rd.go:171) which uses the IDENTICAL formula
	//     rdModeScoreWithZbin(qIndex, zbinOverQuant, rate2 - rateUV -
	//     otherCost - refCost, distortion2 - distortionUV).
	//
	//   - Cap propagation (libvpx bsi.segment_rd = best_rd at rdopt.c:1210
	//     seeded from best_mode.yrd, shrunk to this_segment_rd at line
	//     1173 across shapes): govpx bestSegmentYRD := ctx.bestYRD seeded
	//     from best_mode.yrd, shrunk to shape.SegmentYRD across shapes.
	//
	//   - Per-label cutoff (libvpx rdopt.c:1165 `this_segment_rd >=
	//     bsi->segment_rd break`): govpx vp8_encoder_inter_split.go:207
	//     `segmentYRD >= ctx.segmentYRDCap` returning Cutoff=true.
	//
	//   - Outer per-shape commit (libvpx rdopt.c:1169 `this_segment_rd <
	//     bsi->segment_rd` → bsi update): govpx line 155
	//     `shape.SegmentYRD < bestSegmentYRD` → bestSegmentYRD update +
	//     bestShape/bestMode commit.
	//
	//   - Outer SPLITMV acceptance (libvpx rdopt.c:1996 `tmp_rd <
	//     best_mode.yrd`): govpx returns ok=true when bestSet (== at
	//     least one shape commit beat the initial cap = ctx.bestYRD),
	//     ok=false otherwise. Libvpx's `else { this_rd = INT_MAX;
	//     disable_skip = 1; }` branch makes this_rd lose to best_mode.rd
	//     at the outer line-2235 best-mode update, functionally
	//     equivalent to govpx's ok=false drop.
	//
	// The cap VALUE divergence (73707 vs 129509) is the cap of
	// best_mode.yrd from the LATEST winning non-SPLITMV mode (NEWMV/LAST
	// in both engines). Same MV, same ref, same source, same FDCT, same
	// zbin/quant tables, same RDCOST formula — but different Y qcoeff
	// after quantize, yielding different rate_y, yielding different yrd.
	// That divergence is UPSTREAM of selectInterFrameSplitModeRDScore
	// and cannot be remedied by any change to the cap semantic here.
	//
	// Loosening the cap (seeding bestSegmentYRD = maxInt(), or replacing
	// the per-shape commit gate with a no-op) was empirically tested:
	// SPLITMV's full segment_yrd = 92730 still exceeds ctx.bestYRD =
	// 73707 (the running best whole-MB yrd from NEWMV), so any
	// libvpx-faithful outer acceptance gate `bestShape.SegmentYRD <
	// ctx.bestYRD` still drops SPLITMV. Dropping the outer gate too
	// would diverge from libvpx semantics AND let SPLITMV win solely
	// because its (lower) Y-rate gives a numerically smaller score
	// than NEWMV's (higher) score=102349, which is itself a downstream
	// echo of the same NEWMV picker quantize divergence.
	//
	// Conclusion: the cap semantic in this file is libvpx-faithful and
	// the BestARNR/GoodARNR pin-hold cannot be closed without fixing the
	// NEWMV picker Y quantize at MB(0,0) frame 1. The required next step is
	// the per-Y-block picker-side qcoeff oracle trace that localizes the
	// divergence to the residual or quantize layer.
	if ctx == nil {
		return interSplitModeRDResult{}, false
	}
	bestSet := false
	// libvpx seeds bsi.segment_rd = best_rd verbatim; no guard. ctx.bestYRD
	// is INT_MAX (maxInt) until a non-SPLITMV mode commits a positive yrd,
	// so the legacy `<= 0 → maxInt` guard is dead in practice and is
	// dropped here to keep the SPLITMV cutoff byte-faithful to libvpx for
	// the (theoretical) yrd==0 edge case.
	bestSegmentYRD := ctx.bestYRD
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
		labelRD.coefTokenCosts = e.pickerCoefTokenCosts()
		if e.activityMapValid {
			rdMult, rdDiv := e.libvpxRDConstantsWithZbinForFrame(ctx.qIndex, zbinOverQuant)
			rdMult = e.tunedRDMultiplier(rdMult, ctx.mbRow, ctx.mbCol)
			labelRD.setRDConstants(rdMult, rdDiv)
		}
		overheadRate := vp8enc.MBSplitPartitionRate(uint8(partition)) +
			vp8enc.InterPredictionModeRate(vp8common.SplitMV, ctx.modeCounts)
		overheadRD := e.rdModeScoreWithZbin(ctx.qIndex, zbinOverQuant, overheadRate, 0)
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
	vp8enc.ConvertInterFrameMode(mode, &decMode)
	decMode.MBSkipCoeff = true
	if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ctx.ref.Img, ctx.mbRow, ctx.mbCol, &decMode, nil, &e.dequants[ctx.segmentID&3], &e.reconstructScratch) {
		return interResidualRDAccounting{}, false
	}

	var coeffs vp8enc.MacroblockCoefficients
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
		zbinModeBoost:       vp8enc.SplitInterModeZbinBoost,
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
	//
	// libvpx vp8/encoder/rdopt.c:1700 calculate_final_rd_costs
	// fires the per-MB skip backout strictly on `tteob == 0`, where for
	// SPLITMV (has_y2_block=0) tteob counts Y blocks with eobs[i] > 0 plus
	// the sum of UV eob counts. govpx's previous gate appended
	// `&& stats.rateUV == 0`, which never holds because UV coefficient
	// token rate always includes the per-block EOB-token cost (non-zero
	// even for all-zero residuals — see vp8enc.CoefficientBlockTokenRate's
	// trailing EOB-token addition), silently suppressing the SPLITMV skip
	// backout in govpx's picker and inflating every SPLITMV candidate's
	// rate2 by `shape.SegmentYRate + stats.rateUV - skipBackout`. The
	// verbatim libvpx path checks only tteob == 0.
	mbSkipCoeff := shape.SegmentTTEOB+uvTTEOB == 0
	if mbSkipCoeff {
		rate2 -= shape.SegmentYRate + stats.rateUV
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
