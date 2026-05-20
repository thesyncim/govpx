package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func selectInterFrameSplitMotionMode(src vp8enc.SourceImage, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, partition int) (vp8enc.InterFrameMacroblockMode, bool) {
	return selectInterFrameSplitMotionModeWithContext(src, ref, refFrame, mbRow, mbCol, bestRefMV, qIndex, partition, nil, nil)
}

func selectInterFrameSplitMotionModeWithContext(src vp8enc.SourceImage, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, partition int, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode) (vp8enc.InterFrameMacroblockMode, bool) {
	return selectInterFrameSplitMotionModeWithSearch(src, ref, refFrame, mbRow, mbCol, bestRefMV, qIndex, partition, left, above, defaultInterAnalysisSearchConfig(), 0, nil, &vp8tables.DefaultMVContext)
}

func selectInterFrameSplitMotionModeWithSearch(src vp8enc.SourceImage, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, partition int, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, search interAnalysisSearchConfig, compressorSpeed int, seeds *splitMotionSearchSeeds, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.InterFrameMacroblockMode, bool) {
	return selectInterFrameSplitMotionModeWithSearchAndThreshold(src, ref, refFrame, mbRow, mbCol, bestRefMV, qIndex, partition, left, above, search, compressorSpeed, seeds, mvProbs, 0)
}

// selectInterFrameSplitMotionModeWithSearchAndThreshold mirrors libvpx's
// rd_check_segment per-label loop including its NEW4X4 gate. The mvthresh
// argument is the SPLITMV+NEW threshold for the current reference variant
// (THR_NEW1 for LAST, THR_NEW2 for GOLDEN, THR_NEW3 for ALTREF) plumbed
// through splitMVThresholdForRefSlot. Inside rd_check_segment the gate
// is computed as:
//
//	label_mv_thresh = 1 * bsi->mvthresh / label_count
//
// and inside the per-label loop the NEW4X4 motion search is short-circuited
// by `if (best_label_rd < label_mv_thresh) break;`. The subset helper
// compares an RDCOST-shaped per-label score against label_mv_thresh, which
// matches the libvpx rd_threshes scale.
//
// mvthresh == 0 disables the gate, which is the historical behavior used by
// callers that do not yet route the libvpx rd_threshes table through here.
func selectInterFrameSplitMotionModeWithSearchAndThreshold(src vp8enc.SourceImage, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, partition int, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, search interAnalysisSearchConfig, compressorSpeed int, seeds *splitMotionSearchSeeds, mvProbs *[2][vp8tables.MVPCount]uint8, mvthresh int) (vp8enc.InterFrameMacroblockMode, bool) {
	return selectInterFrameSplitMotionModeWithSearchThresholdAndLabelRD(src, ref, refFrame, mbRow, mbCol, bestRefMV, qIndex, partition, left, above, search, compressorSpeed, seeds, mvProbs, mvthresh, nil, nil, nil)
}

func selectInterFrameSplitMotionModeWithSearchThresholdAndLabelRD(src vp8enc.SourceImage, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, partition int, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, search interAnalysisSearchConfig, compressorSpeed int, seeds *splitMotionSearchSeeds, mvProbs *[2][vp8tables.MVPCount]uint8, mvthresh int, labelRD *splitMotionLabelRDEvaluator, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs) (vp8enc.InterFrameMacroblockMode, bool) {
	ctx := splitMotionShapeContext{
		src:           src,
		ref:           ref,
		refFrame:      refFrame,
		mbRow:         mbRow,
		mbCol:         mbCol,
		bestRefMV:     bestRefMV,
		qIndex:        qIndex,
		partition:     partition,
		left:          left,
		above:         above,
		search:        search,
		compressor:    compressorSpeed,
		seeds:         seeds,
		mvProbs:       mvProbs,
		mvthresh:      mvthresh,
		labelRD:       labelRD,
		quant:         quant,
		coefProbs:     coefProbs,
		subMVRefProbs: nil,
		segmentYRDCap: maxInt(),
	}
	res := ctx.selectShape()
	return res.Mode, res.OK
}

// splitMotionShapeResult mirrors the BEST_SEG_INFO commit produced by libvpx's
// rd_check_segment for a single segmentation shape. Mode is the per-block MV
// /sub-mode commit, SegmentYRD is the running cumulative RDCOST(rate,
// distortion) summed over the per-label best entries (matching
// `this_segment_rd` in rd_check_segment), Cutoff is true when the
// accumulator reached the SegmentYRDCap mid-shape and the per-label loop
// abandoned the shape early. OK is false only when the input arguments are
// invalid.
type splitMotionShapeResult struct {
	Mode              vp8enc.InterFrameMacroblockMode
	SegmentRate       int
	SegmentYRate      int
	SegmentDistortion int
	SegmentTTEOB      int
	SegmentYRD        int
	Cutoff            bool
	OK                bool
}

type splitMotionShapeContext struct {
	src       vp8enc.SourceImage
	ref       *vp8common.Image
	refFrame  vp8common.MVReferenceFrame
	mbRow     int
	mbCol     int
	bestRefMV vp8enc.MotionVector
	qIndex    int
	// errorPerBit is the activity-masked x->errorperbit libvpx
	// vp8_activity_masking publishes per MB before NEW4X4 motion searches
	// in vp8_rd_pick_best_mbsegmentation. Defaults to zero (no-activity);
	// callers that want the TuneSSIM lift populate this before calling
	// selectShape.
	errorPerBit         int
	partition           int
	left                *vp8enc.InterFrameMacroblockMode
	above               *vp8enc.InterFrameMacroblockMode
	search              interAnalysisSearchConfig
	compressor          int
	seeds               *splitMotionSearchSeeds
	mvProbs             *[2][vp8tables.MVPCount]uint8
	mvCosts             *vp8enc.MotionVectorCostTables
	subMVRefProbs       *[3]uint8
	mvthresh            int
	labelRD             *splitMotionLabelRDEvaluator
	quant               *vp8enc.MacroblockQuant
	coefProbs           *vp8tables.CoefficientProbs
	segmentYRDCap       int
	segmentOverheadRate int
	segmentOverheadRD   int
}

// selectShape ports rd_check_segment's
// per-label loop including its incremental segment_rd accumulator and the
//
//	if (this_segment_rd >= bsi->segment_rd) break;
//
// cutoff that abandons a partition shape mid-evaluation when the running
// Y-RD already exceeds the running-best across-shape Y-RD. segmentYRDCap
// is the running bsi->segment_rd carried from prior shape evaluations:
// callers that drive the inter-shape sweep
// (selectInterFrameSplitModeRDScore) initialize the cap to maxInt and
// tighten it after each completed shape's segmentYRD via
// `min(cap, shape.SegmentYRD)`, mirroring rd_check_segment's
// `bsi->segment_rd = this_segment_rd` commit. When the cap is hit
// mid-shape the returned Mode is still populated with the labels
// committed so far so the caller can inspect the partial commit; the
// Cutoff flag indicates the shape was abandoned. The non-cutoff entry
// points (selectInterFrameSplitMotionMode and friends) call this with
// cap=maxInt(), preserving the legacy independent-per-shape behavior
// used by their unit tests and by callers that have not yet routed an
// inter-shape cap.
func (ctx *splitMotionShapeContext) selectShape() splitMotionShapeResult {
	if ctx == nil || ctx.ref == nil || ctx.refFrame == vp8common.IntraFrame || uint(ctx.partition) >= uint(vp8tables.NumMBSplits) {
		return splitMotionShapeResult{}
	}
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  ctx.refFrame,
		Mode:      vp8common.SplitMV,
		Partition: uint8(ctx.partition),
	}
	width, height := splitMotionPartitionBlockSize(ctx.partition)
	// NumMBSplits=4 (pow2) and ctx.partition was just validated to
	// [0,4); mask with 3 elides the bounds check on MBSplitCount.
	labelCount := int(vp8tables.MBSplitCount[mode.Partition&3])
	labelMVThresh := splitMotionLabelMVThreshold(ctx.mvthresh, labelCount)
	// libvpx rd_check_segment seeds this_segment_rd with
	// RDCOST(mbsplit_tree + cost_mv_ref(SPLITMV), 0), then adds each label's
	// RD before comparing against bsi->segment_rd.
	segmentRate := ctx.segmentOverheadRate
	segmentYRate := 0
	segmentDistortion := 0
	segmentTTEOB := 0
	segmentYRD := ctx.segmentOverheadRD
	subsetCtx := splitMotionSubsetContext{
		src:                ctx.src,
		ref:                ctx.ref,
		mbRow:              ctx.mbRow,
		mbCol:              ctx.mbCol,
		mode:               &mode,
		width:              width,
		height:             height,
		bestRefMV:          ctx.bestRefMV,
		qIndex:             ctx.qIndex,
		errorPerBit:        ctx.errorPerBit,
		left:               ctx.left,
		above:              ctx.above,
		search:             ctx.search,
		mvProbs:            ctx.mvProbs,
		mvCosts:            ctx.mvCosts,
		subMVRefProbs:      ctx.subMVRefProbs,
		labelMVThresh:      labelMVThresh,
		labelRD:            ctx.labelRD,
		quant:              ctx.quant,
		coefProbs:          ctx.coefProbs,
		compressorSpeed:    ctx.compressor,
		fullSearchFallback: splitMotionSubsetFullSearchFallback(ctx.compressor),
	}
	for subset := range labelCount {
		subsetCtx.subset = subset
		subsetCtx.searchCenter = splitMotionSubsetSearchCenter(ctx.partition, subset, &mode, ctx.bestRefMV, ctx.compressor, ctx.seeds)
		subsetCtx.stepParam = splitMotionSubsetSearchStepParam(ctx.partition, subset, ctx.compressor, ctx.seeds)
		mv, bMode, labelBestRD, labelRate, labelYRate, labelDistortion, labelTTEOB := subsetCtx.selectMotion()
		fillInterFrameSplitSubsetWithMode(&mode, subset, mv, bMode)
		segmentRate += labelRate
		segmentYRate += labelYRate
		segmentDistortion += labelDistortion
		segmentTTEOB += labelTTEOB
		// libvpx vp8/encoder/rdopt.c:1163-1165
		//   this_segment_rd += best_label_rd;
		//   if (this_segment_rd >= bsi->segment_rd) break;
		// verbatim. segmentYRDCap is always positive at production callers
		// (selectInterFrameSplitModeRDScore seeds it from
		// best_mode.yrd / maxInt; selectInterFrameSplitMotionMode seeds it
		// at maxInt), so the legacy `> 0` precondition was dead and is
		// dropped here to keep the cutoff byte-faithful to libvpx in the
		// (theoretical) cap==0 edge case.
		segmentYRD = saturatingAddInt(segmentYRD, labelBestRD)
		if segmentYRD >= ctx.segmentYRDCap {
			mode.MV = mode.BlockMV[15]
			return splitMotionShapeResult{Mode: mode, SegmentRate: segmentRate, SegmentYRate: segmentYRate, SegmentDistortion: segmentDistortion, SegmentTTEOB: segmentTTEOB, SegmentYRD: segmentYRD, Cutoff: true, OK: true}
		}
	}
	mode.MV = mode.BlockMV[15]
	return splitMotionShapeResult{Mode: mode, SegmentRate: segmentRate, SegmentYRate: segmentYRate, SegmentDistortion: segmentDistortion, SegmentTTEOB: segmentTTEOB, SegmentYRD: segmentYRD, OK: true}
}

// saturatingAddInt avoids overflow when a per-label RDCOST returns
// MaxInt-sized sentinels (e.g. invalid mvProbs). Once saturated the cumulative
// segmentYRD compares ≥ any cap and the cutoff fires immediately.
func saturatingAddInt(a int, b int) int {
	if b <= 0 {
		return a + b
	}
	if a > maxInt()-b {
		return maxInt()
	}
	return a + b
}

// splitMotionLabelMVThreshold mirrors libvpx's
//
//	label_mv_thresh = 1 * bsi->mvthresh / label_count
//
// guard from rd_check_segment. mvthresh<=0 (no gating supplied) yields a
// label-MV threshold of zero, which never trips the NEW4X4 short-circuit and
// preserves the legacy unconditional NEW search.
func splitMotionLabelMVThreshold(mvthresh int, labelCount int) int {
	if min(mvthresh, labelCount) <= 0 {
		return 0
	}
	return mvthresh / labelCount
}

func selectInterFrameSplitSubsetMotionMode(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *vp8enc.InterFrameMacroblockMode, subset int, width int, height int, bestRefMV vp8enc.MotionVector, qIndex int, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode) (vp8enc.MotionVector, vp8common.BPredictionMode) {
	return selectInterFrameSplitSubsetMotionModeWithSearch(src, ref, mbRow, mbCol, mode, subset, width, height, bestRefMV, bestRefMV, 0, true, qIndex, left, above, defaultInterAnalysisSearchConfig(), &vp8tables.DefaultMVContext)
}

func selectInterFrameSplitSubsetMotionModeWithSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *vp8enc.InterFrameMacroblockMode, subset int, width int, height int, bestRefMV vp8enc.MotionVector, searchCenter vp8enc.MotionVector, stepParam int, fullSearchFallback bool, qIndex int, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, vp8common.BPredictionMode) {
	return selectInterFrameSplitSubsetMotionModeWithSearchAndThreshold(src, ref, mbRow, mbCol, mode, subset, width, height, bestRefMV, searchCenter, stepParam, fullSearchFallback, qIndex, left, above, search, mvProbs, 0)
}

// selectInterFrameSplitSubsetMotionModeWithSearchAndThreshold mirrors the
// per-label loop body in libvpx rd_check_segment, including its NEW4X4
// gate. labelMVThresh is the per-label MV threshold derived from
// bsi->mvthresh / label_count. When labelMVThresh > 0 and the running best
// label RD cost is already below it, the NEW4X4 motion search is skipped
// — matching `if (best_label_rd < label_mv_thresh) break;` in libvpx.
//
// Candidate ranking uses the same RDCOST(rate, distortion) shape as
// rd_check_segment. The default helper keeps the cheap historical SAD
// distortion proxy; the RD mode loop passes a splitMotionLabelRDEvaluator so
// each label candidate is ranked with transform-domain token RD like libvpx.
func selectInterFrameSplitSubsetMotionModeWithSearchAndThreshold(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *vp8enc.InterFrameMacroblockMode, subset int, width int, height int, bestRefMV vp8enc.MotionVector, searchCenter vp8enc.MotionVector, stepParam int, fullSearchFallback bool, qIndex int, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8, labelMVThresh int) (vp8enc.MotionVector, vp8common.BPredictionMode) {
	return selectInterFrameSplitSubsetMotionModeWithSearchThresholdAndLabelRD(src, ref, mbRow, mbCol, mode, subset, width, height, bestRefMV, searchCenter, stepParam, fullSearchFallback, qIndex, left, above, search, mvProbs, labelMVThresh, nil, nil, nil)
}

func selectInterFrameSplitSubsetMotionModeWithSearchThresholdAndLabelRD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *vp8enc.InterFrameMacroblockMode, subset int, width int, height int, bestRefMV vp8enc.MotionVector, searchCenter vp8enc.MotionVector, stepParam int, fullSearchFallback bool, qIndex int, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8, labelMVThresh int, labelRD *splitMotionLabelRDEvaluator, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs) (vp8enc.MotionVector, vp8common.BPredictionMode) {
	ctx := splitMotionSubsetContext{
		src:                src,
		ref:                ref,
		mbRow:              mbRow,
		mbCol:              mbCol,
		mode:               mode,
		subset:             subset,
		width:              width,
		height:             height,
		bestRefMV:          bestRefMV,
		searchCenter:       searchCenter,
		stepParam:          stepParam,
		fullSearchFallback: fullSearchFallback,
		qIndex:             qIndex,
		left:               left,
		above:              above,
		search:             search,
		mvProbs:            mvProbs,
		subMVRefProbs:      nil,
		labelMVThresh:      labelMVThresh,
		labelRD:            labelRD,
		quant:              quant,
		coefProbs:          coefProbs,
	}
	mv, bMode, _, _, _, _, _ := ctx.selectMotion()
	return mv, bMode
}

type splitMotionSubsetContext struct {
	src                vp8enc.SourceImage
	ref                *vp8common.Image
	mbRow              int
	mbCol              int
	mode               *vp8enc.InterFrameMacroblockMode
	subset             int
	width              int
	height             int
	bestRefMV          vp8enc.MotionVector
	searchCenter       vp8enc.MotionVector
	stepParam          int
	fullSearchFallback bool
	qIndex             int
	// compressorSpeed mirrors cpi->compressor_speed at the call site of
	// rd_check_segment. libvpx best-mode (==0) keeps the wide MB-scope UMV
	// window across all four segmentation shapes (rdopt.c:1220-1226); the
	// secondary [best_ref_mv ± MAX_FULL_PEL_VAL] intersection (rdopt.c:
	// 1245-1248) only fires inside the `else` (compressor_speed != 0)
	// branch. Plumbing this lets selectMotion pick the correct bounds for
	// the per-label diamond search depending on speed.
	compressorSpeed int
	// errorPerBit is the activity-masked x->errorperbit value libvpx
	// vp8_activity_masking computes per MB. Zero means the caller did not
	// thread the activity lift in; helpers default to vp8enc.ErrorPerBit
	// (qIndex), matching the PSNR-tuned baseline.
	errorPerBit   int
	left          *vp8enc.InterFrameMacroblockMode
	above         *vp8enc.InterFrameMacroblockMode
	search        interAnalysisSearchConfig
	mvProbs       *[2][vp8tables.MVPCount]uint8
	mvCosts       *vp8enc.MotionVectorCostTables
	subMVRefProbs *[3]uint8
	labelMVThresh int
	labelRD       *splitMotionLabelRDEvaluator
	quant         *vp8enc.MacroblockQuant
	coefProbs     *vp8tables.CoefficientProbs
}

// selectMotion is the per-label inner loop body of rd_check_segment. The
// returned bestLabelRD is the per-label RDCOST(rate, distortion) the picker
// chose, so the per-shape caller can accumulate this_segment_rd and apply the
// inter-shape early cutoff.
func (ctx *splitMotionSubsetContext) selectMotion() (vp8enc.MotionVector, vp8common.BPredictionMode, int, int, int, int, int) {
	// MBSplitOffset is [4][16]uint8: ctx.mode.Partition ∈ [0,4) and
	// ctx.subset ∈ [0,16) by upstream validation. Pow2 AND-masks
	// elide both bounds checks on this hot per-subset load.
	block := int(vp8tables.MBSplitOffset[ctx.mode.Partition&3][ctx.subset&15])
	leftMV := analysisSplitLeftMV(ctx.mode, ctx.left, block)
	aboveMV := analysisSplitAboveMV(ctx.mode, ctx.above, block)
	mbRows := (ctx.src.Height + 15) >> 4
	mbCols := (ctx.src.Width + 15) >> 4
	bestMV := vp8enc.MotionVector{}
	bestMode := vp8common.Zero4x4
	bestRD := maxInt()
	bestRate := 0
	bestYRate := 0
	bestDistortion := 0
	// lastTTEOB tracks the per-block-tteob from the LAST inner-loop iteration
	// that actually called vp8_encode_inter_mb_segment (i.e. wasn't UMV-
	// trapped and wasn't gated out before the call). libvpx returns this
	// side-effect via `xd->eobs[i]` because rd_check_segment never restores
	// the eobs after the winning mode is re-selected by `labels2mode`
	// (rdopt.c:1152-1158): only the entropy contexts (t_above_b/t_left_b)
	// are restored, the per-block eob registers retain the last call's
	// values, and bsi->eobs[i] = xd->eobs[i] at rdopt.c:1180 then captures
	// that stale snapshot for the segment_rd-wins path. calculate_final_rd_
	// costs reads tteob back through those stale eobs (rdopt.c:1689-1697),
	// so the SPLITMV skip-backout gate depends on the LAST-tested mode's
	// tteob, not the RD-winning mode's. Track it independently here.
	lastTTEOB := 0
	var bestAbove [4]uint8
	var bestLeft [4]uint8
	bestHasContexts := false

	tryCandidate := func(candidateMode vp8common.BPredictionMode, mv vp8enc.MotionVector) {
		// rd_check_segment applies the same UMV trap to inherited
		// LEFT/ABOVE/ZERO labels as it does to searched NEW labels.
		if !interFrameUMVFullPixelInRange(mv, ctx.mbRow, ctx.mbCol, mbRows, mbCols) {
			return
		}
		rate := splitSubMotionLabelRateWithProbs(candidateMode, ctx.subMVRefProbs)
		rd, labelRate, labelYRate, distortion, tteob, nextAbove, nextLeft, hasContexts := ctx.candidateRD(block, mv, rate)
		lastTTEOB = tteob
		if rd < bestRD {
			bestRD = rd
			bestRate = labelRate
			bestYRate = labelYRate
			bestDistortion = distortion
			bestMV = mv
			bestMode = candidateMode
			bestAbove = nextAbove
			bestLeft = nextLeft
			bestHasContexts = hasContexts
		}
	}

	tryCandidate(vp8common.Left4x4, leftMV)
	if aboveMV != leftMV {
		tryCandidate(vp8common.Above4x4, aboveMV)
	}
	tryCandidate(vp8common.Zero4x4, vp8enc.MotionVector{})

	// libvpx: `if (best_label_rd < label_mv_thresh) break;` — the running
	// best label score is already below the per-label MV threshold, skip
	// the NEW4X4 motion search (the most expensive trial). When the gate
	// is disabled (labelMVThresh == 0) we keep the legacy behavior of
	// always running the NEW4X4 search. We compare in RDCOST space so
	// the threshold matches the libvpx rd_threshes scale.
	//
	// libvpx breaks out of the inner mode loop here BEFORE calling
	// vp8_encode_inter_mb_segment for NEW4X4 (rdopt.c:1027 is inside the
	// `if (this_mode == NEW4X4)` block and precedes the segment-encode
	// call at 1135), so the lastTTEOB carry from ZERO4X4 (or the prior
	// non-trapped mode) is what bsi->eobs sees. lastTTEOB stays untouched
	// in this branch — _not_ assigned bestTTEOB — exactly matching that
	// side-effect.
	if ctx.labelMVThresh > 0 && bestRD < ctx.labelMVThresh {
		if ctx.labelRD != nil && bestHasContexts {
			ctx.labelRD.yAbove = bestAbove
			ctx.labelRD.yLeft = bestLeft
		}
		return bestMV, bestMode, bestRD, bestRate, bestYRate, bestDistortion, lastTTEOB
	}

	errorPerBit := ctx.errorPerBit
	if errorPerBit <= 0 {
		errorPerBit = vp8enc.ErrorPerBit(ctx.qIndex)
	}
	// libvpx vp8_rd_pick_best_mbsegmentation (vp8/encoder/rdopt.c:1199-
	// 1303) runs each rd_check_segment call inside one of two branches:
	//
	//   * Best mode (cpi->compressor_speed == 0, rdopt.c:1220-1226):
	//     all four segmentations (BLOCK_16X8/8X16/8X8/4X4) execute back-
	//     to-back with x->mv_col_min/x->mv_col_max untouched at their
	//     wide MB-scope UMV window. The [best_ref_mv ± MAX_FULL_PEL_VAL]
	//     intersection block (rdopt.c:1233-1248) lives in the `else`
	//     speed-mode branch and is never reached here.
	//
	//   * Speed mode (compressor_speed != 0, rdopt.c:1227-1302): the
	//     first BLOCK_8X8 call runs against the wide UMV window, then
	//     mv_col_min/max are tightened to the intersection with
	//     [best_ref_mv ± MAX_FULL_PEL_VAL] before BLOCK_8X16/16X8/4X4
	//     and restored afterwards (rdopt.c:1297-1301).
	//
	// govpx mirrors both shapes: in best mode every partition uses the
	// wide UMV window; in speed mode only partition 2 (BLOCK_8X8) does,
	// and the secondary partitions intersect with bestRefMV±MAX_FULL_PEL_
	// VAL. Prior to task #300 the intersection fired in best mode too,
	// truncating the SPLITMV per-label diamond search at MB(0,0) frame 1
	// for partitions 0/1/3 on the 1280x720 SSIM-best cohort and biasing
	// the picker away from SPLITMV.
	var bounds interFrameFullPixelBounds
	if ctx.compressorSpeed == 0 || (ctx.mode != nil && ctx.mode.Partition == 2) {
		bounds = interFrameUMVOnlyFullPixelSearchBounds(ctx.mbRow, ctx.mbCol, mbRows, mbCols)
	} else {
		bounds = interFrameFullPixelSearchBounds(ctx.bestRefMV, ctx.mbRow, ctx.mbCol, mbRows, mbCols)
	}
	newMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorWithBounds(ctx.src, ctx.ref, ctx.mbRow, ctx.mbCol, block, ctx.width, ctx.height, ctx.searchCenter, ctx.bestRefMV, ctx.qIndex, errorPerBit, ctx.stepParam, ctx.fullSearchFallback, ctx.mvProbs, ctx.mvCosts, bounds)
	if refinedMV, _, ok := refineInterFrameSplitBlockSubpixelMotionVectorWithErrorPerBitAndCostTables(ctx.src, ctx.ref, ctx.mbRow, ctx.mbCol, block, ctx.width, ctx.height, newMV, ctx.bestRefMV, ctx.qIndex, errorPerBit, ctx.search, ctx.mvProbs, ctx.mvCosts); ok {
		newMV = refinedMV
	}
	newRate := splitSubMotionLabelRateWithProbs(vp8common.New4x4, ctx.subMVRefProbs)
	delta := vp8enc.MotionVector{Row: int16(int(newMV.Row) - int(ctx.bestRefMV.Row)), Col: int16(int(newMV.Col) - int(ctx.bestRefMV.Col))}
	if ctx.mvCosts != nil {
		newRate += splitMotionVectorCostWithCostTables(delta, ctx.mvCosts)
	} else {
		newRate += splitMotionVectorCost(delta, ctx.mvProbs)
	}
	newRD, labelRate, labelYRate, distortion, tteob, nextAbove, nextLeft, hasContexts := ctx.candidateRD(block, newMV, newRate)
	lastTTEOB = tteob
	if newRD < bestRD {
		bestRD = newRD
		bestRate = labelRate
		bestYRate = labelYRate
		bestDistortion = distortion
		bestMV = newMV
		bestMode = vp8common.New4x4
		bestAbove = nextAbove
		bestLeft = nextLeft
		bestHasContexts = hasContexts
	}
	if ctx.labelRD != nil && bestHasContexts {
		ctx.labelRD.yAbove = bestAbove
		ctx.labelRD.yLeft = bestLeft
	}

	return bestMV, bestMode, bestRD, bestRate, bestYRate, bestDistortion, lastTTEOB
}

func (ctx *splitMotionSubsetContext) candidateRD(block int, mv vp8enc.MotionVector, rate int) (int, int, int, int, int, [4]uint8, [4]uint8, bool) {
	if ctx.labelRD != nil {
		if labelRate, labelYRate, labelDist, tteob, nextAbove, nextLeft, ok := ctx.labelRD.rateDistortion(ctx.src, ctx.ref, ctx.mbRow, ctx.mbCol, ctx.qIndex, ctx.quant, ctx.coefProbs, ctx.mode, ctx.subset, mv, rate); ok {
			return ctx.labelRD.score(ctx.qIndex, labelRate, labelDist), labelRate, labelYRate, labelDist, tteob, nextAbove, nextLeft, true
		}
	}
	sad := splitBlockSAD(ctx.src, ctx.ref, ctx.mbRow, ctx.mbCol, block, ctx.width, ctx.height, mv)
	return splitMotionLabelRDScore(ctx.qIndex, rate, sad), rate, 0, sad, 0, [4]uint8{}, [4]uint8{}, false
}

func splitMotionLabelRDScore(qIndex int, rate int, distortion int) int {
	return vp8enc.RDModeScoreWithZbin(qIndex, 0, rate, distortion)
}

type splitMotionLabelRDEvaluator struct {
	zbinOverQuant int
	actZbinAdj    int
	rdMult        int
	rdDiv         int
	fastQuant     bool
	optimize      bool
	yAbove        [4]uint8
	yLeft         [4]uint8
}

func (ev *splitMotionLabelRDEvaluator) init(zbinOverQuant int, actZbinAdj int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, fastQuant bool, optimize bool) bool {
	if ev == nil {
		return false
	}
	*ev = splitMotionLabelRDEvaluator{
		zbinOverQuant: zbinOverQuant,
		actZbinAdj:    actZbinAdj,
		fastQuant:     fastQuant,
		optimize:      optimize,
	}
	if aboveTok != nil {
		ev.yAbove = aboveTok.Y1
	}
	if leftTok != nil {
		ev.yLeft = leftTok.Y1
	}
	return true
}

// setRDConstants pins the macroblock-level RD constants used for SPLITMV label
// evaluation. TuneSSIM callers pass the activity-adjusted multiplier here.
func (ev *splitMotionLabelRDEvaluator) setRDConstants(rdMult int, rdDiv int) {
	if ev == nil {
		return
	}
	ev.rdMult = rdMult
	ev.rdDiv = rdDiv
}

// score prices a SPLITMV label candidate, falling back to the standard zbin
// RD path when no macroblock-specific constants were installed.
func (ev *splitMotionLabelRDEvaluator) score(qIndex int, rate int, distortion int) int {
	if ev == nil || min(ev.rdMult, ev.rdDiv) <= 0 {
		zbinOverQuant := 0
		if ev != nil {
			zbinOverQuant = ev.zbinOverQuant
		}
		return vp8enc.RDModeScoreWithZbin(qIndex, zbinOverQuant, rate, distortion)
	}
	return vp8enc.RDCost(ev.rdMult, ev.rdDiv, rate, distortion)
}

func (ev *splitMotionLabelRDEvaluator) rateDistortion(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, qIndex int, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, mode *vp8enc.InterFrameMacroblockMode, subset int, mv vp8enc.MotionVector, labelRate int) (int, int, int, int, [4]uint8, [4]uint8, bool) {
	if ev == nil || ref == nil || quant == nil || coefProbs == nil || mode == nil || mode.Partition >= vp8tables.NumMBSplits {
		return 0, 0, 0, 0, [4]uint8{}, [4]uint8{}, false
	}
	nextAbove := ev.yAbove
	nextLeft := ev.yLeft
	rate := labelRate
	yRate := 0
	distortion := 0
	tteob := 0
	for block := range 16 {
		// MBSplits is [4][16]uint8 indexed by validated Partition ∈ [0,4)
		// and block ∈ [0,16). Pow2 masks elide both bounds checks.
		if int(vp8tables.MBSplits[mode.Partition&3][block&15]) != subset {
			continue
		}
		var pred [16]byte
		if !predictSplitMotionBlock4x4(ref, mbRow, mbCol, block, mv, &pred) {
			return 0, 0, 0, 0, [4]uint8{}, [4]uint8{}, false
		}
		var input [16]int16
		fillSplitMotionResidual4x4(src, mbRow, mbCol, block, &pred, &input)
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(nextAbove[a] + nextLeft[l])
		eob := quantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, 3, ctx, 0, ev.zbinOverQuant, splitInterModeZbinBoost, ev.actZbinAdj, ev.zbinOverQuant, ev.rdMult, ev.rdDiv, false, ev.fastQuant, ev.optimize, &dct, &quant.Y1, &qcoeff, &dqcoeff)
		blockRate := vp8enc.CoefficientBlockTokenRate(coefProbs, 3, ctx, 0, &qcoeff, eob)
		rate += blockRate
		yRate += blockRate
		if eob > 0 {
			tteob++
		}
		distortion += transformBlockError(&dct, &dqcoeff)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		nextAbove[a] = hasCoeffs
		nextLeft[l] = hasCoeffs
	}
	return rate, yRate, distortion >> 2, tteob, nextAbove, nextLeft, true
}

func predictSplitMotionBlock4x4(ref *vp8common.Image, mbRow int, mbCol int, block int, mv vp8enc.MotionVector, out *[16]byte) bool {
	if ref == nil || out == nil {
		return false
	}
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		return predictSplitMotionSubpixelBlock4x4(ref, refBaseY, refBaseX, xOffset, yOffset, out)
	}
	// This predictor feeds the SPLITMV label-RD scorer. libvpx scores these
	// 4x4 labels against the coded-edge samples; final reconstruction still
	// goes through the decoder predictor path.
	if uint(refBaseY) <= uint(ref.CodedHeight-4) && uint(refBaseX) <= uint(ref.CodedWidth-4) {
		for row := range 4 {
			copy(out[row*4:row*4+4], ref.Y[(refBaseY+row)*ref.YStride+refBaseX:])
		}
		return true
	}
	gatherCodedClampedRefBlock(ref, refBaseY, refBaseX, 4, 4, out[:], 4)
	return true
}

func predictSplitMotionSubpixelBlock4x4(ref *vp8common.Image, refBaseY int, refBaseX int, xOffset int, yOffset int, out *[16]byte) bool {
	if ref == nil || out == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+7 > ref.CodedHeight+ref.YBorder ||
		refBaseX+7 > ref.CodedWidth+ref.YBorder {
		return false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+8*ref.YStride+9 > len(ref.YFull) {
		return false
	}
	if refBaseY-2 < 0 || refBaseX-2 < 0 || refBaseY+7 > ref.CodedHeight || refBaseX+7 > ref.CodedWidth {
		var scratch [(4 + 5) * (4 + 5)]byte
		gatherCodedClampedRefBlock(ref, refBaseY-2, refBaseX-2, 4+5, 4+5, scratch[:], 4+5)
		dsp.SixTapPredict4x4(scratch[:], 4+5, xOffset, yOffset, out[:], 4)
		return true
	}
	dsp.SixTapPredict4x4(ref.YFull[start:], ref.YStride, xOffset, yOffset, out[:], 4)
	return true
}

// gatherCodedClampedRefBlock copies a (height x width) Y-plane block from
// ref into dst at dstStride, clamping each source coordinate to the coded
// extent (ref.CodedWidth / ref.CodedHeight). libvpx's reference YV12 buffer
// is allocated with 16-aligned dimensions (alloccommon.c:56-65 rounds
// (width,height) up before vp8_yv12_alloc_frame_buffer), so y_crop_height
// == y_height == coded height. The post-LF vp8_yv12_extend_frame_borders
// therefore extends from coded-edge-1 (yv12extend.c:105-117), leaving the
// coded-but-invisible MB padding populated with the live LF
// reconstruction. SixTap/bilinear scratch fills here mirror that state by
// clamping to the coded extent — matching libvpx's effective bordered-Y
// buffer byte-for-byte.
func gatherCodedClampedRefBlock(ref *vp8common.Image, baseY int, baseX int, width int, height int, dst []byte, dstStride int) {
	for row := range height {
		refY := clampEncodeCoord(baseY+row, ref.CodedHeight)
		dstRow := row * dstStride
		srcRow := refY * ref.YStride
		for col := range width {
			refX := clampEncodeCoord(baseX+col, ref.CodedWidth)
			dst[dstRow+col] = ref.Y[srcRow+refX]
		}
	}
}

func fillSplitMotionResidual4x4(src vp8enc.SourceImage, mbRow int, mbCol int, block int, pred *[16]byte, out *[16]int16) {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	for row := range 4 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := range 4 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			out[row*4+col] = int16(int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*4+col]))
		}
	}
}

type splitMotionSearchSeeds struct {
	valid    bool
	mv       [4]vp8enc.MotionVector
	step8x16 [2]int8
	step16x8 [2]int8
}

func splitMotionSearchSeedsFrom8x8(mode *vp8enc.InterFrameMacroblockMode) splitMotionSearchSeeds {
	if mode == nil || mode.Mode != vp8common.SplitMV || mode.Partition != 2 {
		return splitMotionSearchSeeds{}
	}
	seeds := splitMotionSearchSeeds{
		valid: true,
		mv: [4]vp8enc.MotionVector{
			mode.BlockMV[0],
			mode.BlockMV[2],
			mode.BlockMV[8],
			mode.BlockMV[10],
		},
	}
	seeds.step8x16[0] = libvpxSplitMVStepParamFromSeedDistance(splitMotionSeedDistance(seeds.mv[0], seeds.mv[2]))
	seeds.step8x16[1] = libvpxSplitMVStepParamFromSeedDistance(splitMotionSeedDistance(seeds.mv[1], seeds.mv[3]))
	seeds.step16x8[0] = libvpxSplitMVStepParamFromSeedDistance(splitMotionSeedDistance(seeds.mv[0], seeds.mv[1]))
	seeds.step16x8[1] = libvpxSplitMVStepParamFromSeedDistance(splitMotionSeedDistance(seeds.mv[2], seeds.mv[3]))
	return seeds
}

func splitMotionSubsetSearchCenter(partition int, subset int, mode *vp8enc.InterFrameMacroblockMode, bestRefMV vp8enc.MotionVector, compressorSpeed int, seeds *splitMotionSearchSeeds) vp8enc.MotionVector {
	if compressorSpeed == 0 || mode == nil || uint(partition) >= uint(vp8tables.NumMBSplits) || uint(subset) >= uint(vp8tables.MBSplitCount[uint8(partition)]) {
		return bestRefMV
	}
	if seeds != nil && seeds.valid {
		switch partition {
		case 0:
			if subset == 0 {
				return seeds.mv[0]
			}
			if subset == 1 {
				return seeds.mv[2]
			}
		case 1:
			if subset == 0 {
				return seeds.mv[0]
			}
			if subset == 1 {
				return seeds.mv[1]
			}
		case 3:
			if subset == 0 {
				return seeds.mv[0]
			}
		}
	}
	if partition != 3 || subset == 0 {
		return bestRefMV
	}
	// MBSplitOffset is [4][16]uint8; partition is validated to [0,4)
	// at caller boundaries and subset comes from MBSplitCount[partition]
	// (≤16). Pow2 masks elide both per-call bounds checks.
	block := int(vp8tables.MBSplitOffset[uint8(partition)&3][subset&15])
	// block is in [0,15] by the table's contents (uint8 cells holding
	// 0..15). mode.BlockMV is [16]MotionVector; AND-mask with 15 elides
	// the bounds checks on both indexed loads here.
	if block&3 == 0 {
		if block >= 4 {
			return mode.BlockMV[(block-4)&15]
		}
		return bestRefMV
	}
	return mode.BlockMV[(block-1)&15]
}

func splitMotionSubsetSearchStepParam(partition int, subset int, compressorSpeed int, seeds *splitMotionSearchSeeds) int {
	if compressorSpeed == 0 {
		return 0
	}
	if seeds != nil && seeds.valid {
		switch partition {
		case 0:
			if uint(subset) < uint(len(seeds.step16x8)) {
				return int(seeds.step16x8[subset])
			}
		case 1:
			if uint(subset) < uint(len(seeds.step8x16)) {
				return int(seeds.step8x16[subset])
			}
		}
	}
	if partition == 3 && subset > 0 {
		return 2
	}
	return 0
}

func splitMotionSubsetFullSearchFallback(compressorSpeed int) bool {
	return compressorSpeed == 0
}

func splitMotionSeedDistance(a vp8enc.MotionVector, b vp8enc.MotionVector) int {
	row := int(a.Row) - int(b.Row)
	if row < 0 {
		row = -row
	}
	col := int(a.Col) - int(b.Col)
	if col < 0 {
		col = -col
	}
	if col > row {
		row = col
	}
	return row >> 3
}

func libvpxSplitMVStepParamFromSeedDistance(sr int) int8 {
	if sr > interFrameMaxFirstStep {
		sr = interFrameMaxFirstStep
	} else if sr < 1 {
		sr = 1
	}
	step := 0
	for sr >>= 1; sr > 0; sr >>= 1 {
		step++
	}
	return int8(interFrameMaxMVSearchSteps - 1 - step)
}

func splitSubMotionLabelSearchCost(mode vp8common.BPredictionMode, qIndex int) int {
	cost := splitSubMotionLabelRate(mode)
	return (cost*vp8enc.SADPerBit4(qIndex) + 128) >> 8
}

// interSplitMVRDDecision mirrors libvpx's RATE_DISTORTION accounting after a
// SPLITMV partition is chosen: vp8_rd_pick_best_mbsegmentation feeds the Y RD
// (rate_y/distortion) and rd_inter4x4_uv adds rate_uv/distortion_uv on top.
// Per-block EOBs let downstream packet writers reuse the chosen partition's
// quantized coefficients (libvpx stores these in MACROBLOCKD::eobs[0..23]).
//
// OtherCost / RefCost / TotalRate / Rate2 / RD / YRD mirror the
// other_cost / x->ref_frame_cost / RATE_DISTORTION::rate2 / this_rd /
// best_mode.yrd computed in vp8_rd_pick_inter_mode after the SPLITMV
// branch returns. Total rate decomposes as
//
//	TotalRate = YRate + UVRate + OtherCost + RefCost
//
// matching update_best_mode's
//
//	yrd = RDCOST(rdmult, rddiv, rate2 - rate_uv - other_cost, distortion2 - distortion_uv)
//
// breakdown where the inputs are the same Y-side / UV-side splits.
type interSplitMVRDDecision struct {
	Mode      vp8enc.InterFrameMacroblockMode
	YRate     int
	YDist     int
	UVRate    int
	UVDist    int
	OtherCost int
	RefCost   int
	TotalRate int
	Rate2     int
	RD        int
	YRD       int
	Coeffs    vp8enc.MacroblockCoefficients
}

// LumaEOB returns the per-4x4-block luma EOB stored after the chosen SPLITMV
// partition's transform/quantize pass. block must be 0..15.
func (d *interSplitMVRDDecision) LumaEOB(block int) int {
	if d == nil || uint(block) > 15 {
		return 0
	}
	return d.Coeffs.BlockEOB(block, 0)
}

// selectInterFrameSplitMotionDecisionRD ports rdopt.c's SPLITMV branch in
// vp8_rd_pick_inter_mode: after vp8_rd_pick_best_mbsegmentation commits the
// per-subblock luma MVs, we run macro_block_yrd over the 4x4 luma residual
// and rd_inter4x4_uv over the chroma residual using libvpx-style 8x8 UV MVs
// (average of the four covering 4x4 luma MVs, rounded to the nearest 1/8-pel
// chroma vector via vp8_build_inter4x4_predictors_mbuv). Per-block EOBs are
// stored on the returned decision so downstream callers can write the chosen
// partition's tokens without re-quantizing.
func selectInterFrameSplitMotionDecisionRD(src vp8enc.SourceImage, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, partition int, quant *vp8enc.MacroblockQuant, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, coefProbs *vp8tables.CoefficientProbs, pred *vp8common.Image, zbinOverQuant int, fastQuant bool, optimize bool) (interSplitMVRDDecision, bool) {
	return selectInterFrameSplitMotionDecisionRDWithThreshold(src, ref, refFrame, mbRow, mbCol, bestRefMV, qIndex, partition, quant, aboveTok, leftTok, coefProbs, pred, zbinOverQuant, fastQuant, optimize, 0, 0, 0)
}

// selectInterFrameSplitMotionDecisionRDWithThreshold mirrors the SPLITMV
// branch of vp8_rd_pick_inter_mode end-to-end. mvthresh is the SPLITMV+NEW
// rd_thresh for the current reference (THR_NEW1 for LAST, THR_NEW2 for
// GOLDEN, THR_NEW3 for ALTREF) used to gate the per-label NEW4X4 motion
// search inside rd_check_segment. otherCost / refCost are the
// other_cost / x->ref_frame_cost values libvpx accumulates around the
// segmentation call:
//
//	rd.rate2 += rate (label rate from vp8_rd_pick_best_mbsegmentation)
//	rd.rate2 += rd.rate_uv (rd_inter4x4_uv)
//	calculate_final_rd_costs adds default no-skip other_cost +
//	    x->ref_frame_cost[ref_frame] before computing this_rd.
//
// On return decision.TotalRate decomposes as
// YRate+UVRate+OtherCost+RefCost so callers can recover the same
// rate2/yrd breakdown update_best_mode would have written to BEST_MODE.
func selectInterFrameSplitMotionDecisionRDWithThreshold(src vp8enc.SourceImage, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, partition int, quant *vp8enc.MacroblockQuant, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, coefProbs *vp8tables.CoefficientProbs, pred *vp8common.Image, zbinOverQuant int, fastQuant bool, optimize bool, mvthresh int, otherCost int, refCost int) (interSplitMVRDDecision, bool) {
	if quant == nil || coefProbs == nil || pred == nil {
		return interSplitMVRDDecision{}, false
	}
	mode, ok := selectInterFrameSplitMotionModeWithSearchAndThreshold(src, ref, refFrame, mbRow, mbCol, bestRefMV, qIndex, partition, nil, nil, defaultInterAnalysisSearchConfig(), 0, nil, &vp8tables.DefaultMVContext, mvthresh)
	if !ok {
		return interSplitMVRDDecision{}, false
	}

	// Render the SPLITMV predictor into pred so we can reuse the same
	// per-4x4 transform/quantize path the whole-MB inter case takes through
	// buildPredictedMacroblockCoefficientsRD. With MBSkipCoeff=true the
	// reconstruction stops after vp8_build_inter*_predictors_mb{y,uv} so
	// pred holds the 16x16 luma + 8x8 chroma SPLITMV predictor.
	var decMode vp8dec.MacroblockMode
	vp8enc.ConvertInterFrameMode(&mode, &decMode)
	decMode.MBSkipCoeff = true
	yOff := mbRow*16*pred.YStride + mbCol*16
	uOff := mbRow*8*pred.UStride + mbCol*8
	vOff := mbRow*8*pred.VStride + mbCol*8
	var emptyTokens vp8dec.MacroblockTokens
	var residual vp8dec.MacroblockResidual
	if !vp8dec.ReconstructSplitMVInterMacroblock(&decMode, &emptyTokens, &vp8common.MacroblockDequant{}, ref, pred.Y[yOff:], pred.YStride, pred.U[uOff:], pred.UStride, pred.V[vOff:], pred.VStride, &residual, mbRow, mbCol, vp8dec.InterPredictionConfig{}) {
		return interSplitMVRDDecision{}, false
	}

	// is4x4=true, intra=false, zbinModeBoost=splitInterModeZbinBoost(0)
	// matches the SPLITMV branch of rdopt.c vp8_rd_pick_inter_mode where
	// macro_block_yrd reports rate_y/distortion via 16 4x4 token blocks
	// (block_type=3) and rd_inter4x4_uv reports rate_uv/distortion_uv via
	// 8 4x4 chroma blocks (block_type=2).
	decision := interSplitMVRDDecision{Mode: mode}
	stats := buildPredictedMacroblockCoefficientsInternal(&predictedMacroblockCoefficientArgs{
		coefProbs:           coefProbs,
		src:                 src,
		mbRow:               mbRow,
		mbCol:               mbCol,
		pred:                pred,
		aboveTok:            aboveTok,
		leftTok:             leftTok,
		quant:               quant,
		qIndex:              qIndex,
		zbinOverQuant:       zbinOverQuant,
		zbinModeBoost:       splitInterModeZbinBoost,
		is4x4:               true,
		splitPartitionValid: true,
		splitPartition:      mode.Partition,
		intra:               false,
		fastQuant:           fastQuant,
		optimize:            optimize,
		collectStats:        true,
		coeffs:              &decision.Coeffs,
	})
	decision.YRate = stats.rateY
	decision.YDist = stats.distortionY
	decision.UVRate = stats.rateUV
	decision.UVDist = stats.distortionUV

	// libvpx's vp8_rd_pick_inter_mode SPLITMV branch:
	//
	//   rd.rate2 += rate;          // label-tree + sub-MV-mode + MV cost
	//   rd.rate2 += rd.rate_uv;    // rd_inter4x4_uv chroma rate
	//   rd.rate2 += other_cost;    // no-skip cost / skip backout (calc_final_rd_costs)
	//   rd.rate2 += ref_frame_cost // (calc_final_rd_costs)
	//   this_rd = RDCOST(rdmult, rddiv, rd.rate2, rd.distortion2)
	//   yrd = RDCOST(rate2 - rate_uv - other_cost - ref_cost,
	//                distortion2 - distortion_uv)
	//
	// We expose all of these on the returned decision so callers (and
	// tests) can verify the breakdown without rerunning the picker.
	decision.OtherCost = otherCost
	decision.RefCost = refCost
	totalDist := decision.YDist + decision.UVDist
	decision.TotalRate = decision.YRate + decision.UVRate + otherCost + refCost
	decision.Rate2 = decision.TotalRate
	decision.RD = vp8enc.RDModeScoreWithZbin(qIndex, zbinOverQuant, decision.TotalRate, totalDist)
	decision.YRD = vp8enc.RDModeScoreWithZbin(qIndex, zbinOverQuant, decision.YRate, decision.YDist)
	return decision, true
}

func splitMotionPartitionLumaDistortionFromSums(labelErrors [16]int, partition uint8) int {
	if partition >= vp8tables.NumMBSplits {
		total := 0
		for _, err := range labelErrors {
			total += err
		}
		return total >> 2
	}
	total := 0
	labelCount := int(vp8tables.MBSplitCount[partition&3])
	for subset := range labelCount {
		total += labelErrors[subset&15] >> 2
	}
	return total
}

func splitMotionPartitionLumaDistortionFromBlocks(blockErrors [16]int, partition uint8) int {
	var labelErrors [16]int
	if partition >= vp8tables.NumMBSplits {
		total := 0
		for _, err := range blockErrors {
			total += err
		}
		return total >> 2
	}
	for block, err := range blockErrors {
		subset := int(vp8tables.MBSplits[partition&3][block&15])
		labelErrors[subset&15] += err
	}
	return splitMotionPartitionLumaDistortionFromSums(labelErrors, partition)
}

func splitMotionPartitionBlockSize(partition int) (int, int) {
	switch partition {
	case 0:
		return 16, 8
	case 1:
		return 8, 16
	case 2:
		return 8, 8
	default:
		return 4, 4
	}
}

func fillInterFrameSplitSubset(mode *vp8enc.InterFrameMacroblockMode, subset int, mv vp8enc.MotionVector) {
	fillInterFrameSplitSubsetWithMode(mode, subset, mv, vp8common.New4x4)
}

func fillInterFrameSplitSubsetWithMode(mode *vp8enc.InterFrameMacroblockMode, subset int, mv vp8enc.MotionVector, firstMode vp8common.BPredictionMode) {
	if mode == nil || mode.Partition >= vp8tables.NumMBSplits {
		return
	}
	for block := range 16 {
		// MBSplits is [4][16]uint8 indexed by validated Partition ∈ [0,4)
		// and block ∈ [0,16). Pow2 masks elide both bounds checks.
		if int(vp8tables.MBSplits[mode.Partition&3][block&15]) != subset {
			continue
		}
		bMode := firstMode
		// block-1 ∈ [0,15) and block-4 ∈ [-4,12); the guards above
		// (block&3 != 0, block>>2 != 0) ensure block-1 ≥ 0 and
		// block-4 ≥ 0 respectively. AND-mask with 15 elides bounds
		// checks on the [16]uint8 inner array.
		if block&3 != 0 && int(vp8tables.MBSplits[mode.Partition&3][(block-1)&15]) == subset {
			bMode = vp8common.Left4x4
		} else if block>>2 != 0 && int(vp8tables.MBSplits[mode.Partition&3][(block-4)&15]) == subset {
			bMode = vp8common.Above4x4
		}
		mode.BlockMV[block&15] = mv
		mode.BModes[block&15] = bMode
	}
}

func collectInterFrameMotionCandidates(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	mvProbs *[2][vp8tables.MVPCount]uint8,
	candidates *[interFrameMotionCandidateMax]interAnalysisMotionCandidate,
) int {
	return collectInterFrameMotionCandidatesWithSearch(src, refs, refCount, mbRow, mbCol, mbRows, mbCols, qIndex, above, left, aboveLeft, defaultInterAnalysisSearchConfig(), mvProbs, candidates)
}

func collectInterFrameMotionCandidatesWithSearch(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	search interAnalysisSearchConfig,
	mvProbs *[2][vp8tables.MVPCount]uint8,
	candidates *[interFrameMotionCandidateMax]interAnalysisMotionCandidate,
) int {
	return collectInterFrameMotionCandidatesWithEncoder(nil, src, refs, refCount, mbRow, mbCol, mbRows, mbCols, qIndex, above, left, aboveLeft, search, mvProbs, candidates)
}

func collectInterFrameMotionCandidatesWithEncoder(
	e *VP8Encoder, src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	search interAnalysisSearchConfig,
	mvProbs *[2][vp8tables.MVPCount]uint8,
	candidates *[interFrameMotionCandidateMax]interAnalysisMotionCandidate,
) int {
	if candidates == nil || mvProbs == nil {
		return 0
	}
	count := 0
	signBias := defaultInterFrameSignBias()
	var mvCosts *vp8enc.MotionVectorCostTables
	var localMVCosts vp8enc.MotionVectorCostTables
	if e != nil {
		signBias = e.interFrameSignBias()
		if mvProbs == &e.modeProbs.MV {
			mvCosts = e.currentMotionVectorCostTables()
		}
	}
	if mvCosts == nil && mvProbs != nil {
		localMVCosts.Build(mvProbs)
		mvCosts = &localMVCosts
	}
	// Hoist the min(refCount, len(refs)) bound out of the loop condition
	// so each iteration only does one compare instead of two.
	refLimit := min(refCount, len(refs))
	for refIndex := range refLimit {
		ref := refs[refIndex]
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, vp8enc.MotionVector{})
		nearest, near := interAnalysisReferenceMotionPredictorsWithSignBias(ref.Frame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, signBias)
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, nearest)
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, near)
		bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
		start := interFrameSearchStart{}
		if e != nil {
			start = e.improvedInterFrameSearchStart(src, ref.Frame, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, search)
		}
		var motionStats interFrameMotionSearchStats
		var stats *interFrameMotionSearchStats
		if e != nil && e.opts.PhaseStats != nil && !e.threadedRowsActive {
			motionStats.phase = e.opts.PhaseStats
			stats = &motionStats
		}
		fullMV, fullCost := selectInterFrameFullPixelMotionVectorWithSearchStartAndProbsAndStats(src, ref.Img, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, mvProbs, stats)
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, fullMV)
		if fullCost == 0 {
			continue
		}
		errorPerBit := 0
		if e != nil {
			errorPerBit = e.tunedErrorPerBit(qIndex, mbRow, mbCol)
		}
		subpel := interFrameSubpixelSearch{
			src:         src,
			ref:         ref.Img,
			mbRow:       mbRow,
			mbCol:       mbCol,
			best:        fullMV,
			bestRefMV:   bestRefMV,
			qIndex:      qIndex,
			errorPerBit: errorPerBit,
			search:      search,
			mvProbs:     mvProbs,
			mvCosts:     mvCosts,
		}
		var refinedMV vp8enc.MotionVector
		var ok bool
		if stats != nil {
			refinedMV, _, _, _, ok = subpel.refineWithStats(stats)
		} else {
			refinedMV, _, _, _, ok = subpel.refine()
		}
		if ok && refinedMV != fullMV {
			count = appendInterAnalysisMotionCandidate(candidates, count, ref, refinedMV)
		}
	}
	return count
}

func (e *VP8Encoder) interAnalysisReferenceMotionPredictors(refFrame vp8common.MVReferenceFrame, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) (vp8enc.MotionVector, vp8enc.MotionVector) {
	return interAnalysisReferenceMotionPredictorsWithSignBias(refFrame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, e.interFrameSignBias())
}

func interAnalysisReferenceMotionPredictorsWithSignBias(refFrame vp8common.MVReferenceFrame, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, signBias [vp8common.MaxRefFrames]bool) (vp8enc.MotionVector, vp8enc.MotionVector) {
	return vp8enc.InterFrameNearMotionVectorsAt(above, left, aboveLeft, refFrame, mbRow, mbCol, mbRows, mbCols, signBias)
}

func appendInterAnalysisMotionCandidate(candidates *[interFrameMotionCandidateMax]interAnalysisMotionCandidate, count int, ref interAnalysisReference, mv vp8enc.MotionVector) int {
	// uint range collapses (count<0)+(count>=len) and proves count is in
	// [0, len) for subsequent indexing.
	if candidates == nil || uint(count) >= uint(len(candidates)) {
		return count
	}
	// Sub-slice to [0, count] so the linear-scan loop body indexes a
	// statically-bounded slice instead of doing a per-iter bounds check
	// against the full array.
	existing := candidates[:count]
	for i := range existing {
		if existing[i].Ref.Frame == ref.Frame && existing[i].Ref.Img == ref.Img && existing[i].MV == mv {
			return count
		}
	}
	candidates[count] = interAnalysisMotionCandidate{Ref: ref, MV: mv}
	return count + 1
}
