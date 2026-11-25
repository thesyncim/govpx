package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
)

func defaultInterFrameSignBias() [vp8common.MaxRefFrames]bool {
	return [vp8common.MaxRefFrames]bool{}
}

func selectInterFrameReferenceMotionVector(src vp8enc.SourceImage, refs []interAnalysisReference, refCount int, mbRow int, mbCol int, mbRows int, mbCols int, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (interAnalysisReference, vp8enc.MotionVector) {
	return selectInterFrameReferenceMotionVectorWithSearch(src, refs, refCount, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, qIndex, defaultInterAnalysisSearchConfig(), mvProbs)
}

func selectInterFrameReferenceMotionVectorWithSearch(src vp8enc.SourceImage, refs []interAnalysisReference, refCount int, mbRow int, mbCol int, mbRows int, mbCols int, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (interAnalysisReference, vp8enc.MotionVector) {
	bestRef := refs[0]
	signBias := defaultInterFrameSignBias()
	bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, bestRef.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
	best, bestCost := selectInterFrameMotionVectorWithSearch(src, bestRef.Img, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, mvProbs)
	if bestCost == 0 {
		return bestRef, best
	}
	// Hoist the min(refCount, len(refs)) bound out of the loop condition
	// so the compiler can prove refs[refIndex] in range without re-doing
	// the len(refs) check inside the loop.
	refLimit := min(refCount, len(refs))
	for refIndex := 1; refIndex < refLimit; refIndex++ {
		ref := refs[refIndex]
		refMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
		mv, cost := selectInterFrameMotionVectorWithSearch(src, ref.Img, mbRow, mbCol, mbRows, mbCols, refMV, qIndex, search, mvProbs)
		if cost < bestCost {
			bestRef = ref
			best = mv
			bestCost = cost
			if bestCost == 0 {
				return bestRef, best
			}
		}
	}
	return bestRef, best
}

type interFrameModeDecision struct {
	interMode       vp8enc.InterFrameMacroblockMode
	intraMode       vp8enc.InterFrameMacroblockMode
	ref             interAnalysisReference
	projectedRate   int32
	denoise         denoiserMacroblockDecision
	improvedMVStart interFrameSearchStart
	staleY2         staleY2Snapshot
	// predictionError is the picker `distortion` scalar returned through
	// vp8_encode_inter_macroblock and accumulated into mb.prediction_error.
	predictionError int32
	lumaSSE         int32
	lumaSSEValid    bool
	useIntra        bool
}

func (d interFrameModeDecision) cyclicRefreshEligible() bool {
	return !d.useIntra && d.interMode.RefFrame == vp8common.LastFrame && d.interMode.Mode == vp8common.ZeroMV
}

func addProjectedMacroblockRate(total int, rate int) int {
	if rate <= 0 {
		return total
	}
	if total > maxInt()-rate {
		return maxInt()
	}
	return total + rate
}

func (e *VP8Encoder) selectInterFrameModeDecision(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int,
	baseQIndex int, segmentation vp8enc.SegmentationConfig, segmentID uint8,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes,
	quant *vp8enc.MacroblockQuant,
	sourceAltRefZeroMVOnly bool,
) (interFrameModeDecision, bool) {
	if !e.interAnalysisUsesRDModeDecision() {
		// Libvpx encodeframe.c resets x->rdmult/x->rddiv from the
		// frame-level cpi->RDMULT/RDDIV before vp8cx_mb_init_quantizer()
		// applies per-segment quant tables. The fast picker therefore uses
		// base_qindex for RD-cost and motion-search rate scaling, while the
		// supplied quant still reflects the candidate segment for breakout
		// and final residual coding.
		return e.selectFastInterFrameModeDecision(
			src, refs, refCount,
			mbRow, mbCol, mbRows, mbCols,
			baseQIndex, segmentID,
			above, left, aboveLeft,
			quant,
			sourceAltRefZeroMVOnly,
		)
	}
	// The RD picker has the same frame-level RD-constant rule as the fast
	// picker above. Segment Q still feeds residual quantization through quant,
	// but candidate scoring, thresholds, motion costs, and intra penalties use
	// cm->base_qindex via cpi->RDMULT/RDDIV.
	return e.selectRDInterFrameModeDecision(
		src, refs, refCount,
		mbRow, mbCol, mbRows, mbCols,
		baseQIndex, segmentID,
		above, left, aboveLeft,
		aboveTok, leftTok,
		quant,
		sourceAltRefZeroMVOnly,
	)
}

func (e *VP8Encoder) sourceAltRefZeroMVOnly(flags EncodeFlags) bool {
	return flags&EncodeInvisibleFrame == 0 &&
		e.opts.ARNRMaxFrames == 0 &&
		e.isSrcFrameAltRef(e.currentSourcePTS)
}

func (e *VP8Encoder) interMacroblockInactive(mbRow int, mbCol int, mbCols int) bool {
	if !e.activeMapEnabled || mbCols <= 0 {
		return false
	}
	index := mbRow*mbCols + mbCol
	return index >= 0 && index < len(e.activeMap) && e.activeMap[index] == 0
}

func sourceAltRefCandidateAllowed(onlyAltRefZeroMV bool, refFrame vp8common.MVReferenceFrame, mode vp8common.MBPredictionMode) bool {
	return !onlyAltRefZeroMV || (mode == vp8common.ZeroMV && refFrame == vp8common.AltRefFrame)
}

type interIntraModeRDResult struct {
	mode         vp8enc.InterFrameMacroblockMode
	score        int
	yrd          int
	rate         int
	rateY        int
	rateUV       int
	distortion   int
	distortionUV int
	staleY2      staleY2Snapshot
}

type interIntraChromaRDCache struct {
	mode   vp8common.MBPredictionMode
	rate   int
	dist   int
	eobSum int
	ok     bool
	valid  bool
}

func (e *VP8Encoder) estimateInterIntraModeRDScore(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, mbMode vp8common.MBPredictionMode, bestRD int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant) (interIntraModeRDResult, bool) {
	return e.estimateInterIntraModeRDScoreWithChromaCache(src, qIndex, mbRow, mbCol, mbMode, bestRD, aboveTok, leftTok, quant, nil)
}

func (e *VP8Encoder) estimateInterIntraModeRDScoreWithChromaCache(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, mbMode vp8common.MBPredictionMode, bestRD int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, chromaCache *interIntraChromaRDCache) (interIntraModeRDResult, bool) {
	zbinOverQuant := e.rc.currentZbinOverQuant
	actZbinAdj := 0
	if e.activityMapValid {
		if adjustment, ok := e.tunedZbinAdjustment(mbRow, mbCol); ok {
			actZbinAdj = adjustment
		}
	}
	fastQuant := e.libvpxUseFastQuantForPick()
	pickerProbs := e.pickerCoefProbs()
	pickerTokenCosts := e.pickerCoefTokenCosts()
	// libvpx propagates x->rdmult (activity-masked under --tune=ssim) into
	// every per-block RDCOST inside vp8/encoder/rdopt.c
	// rd_pick_intra4x4block, rd_pick_intra_mbuv_mode, and the whole-block
	// intra Y picker. Pre-compute the activity-tuned (rdMult, rdDiv) pair
	// once for this MB and thread it through the per-sub-block 4x4 picker
	// AND the UV picker so all the intra mode comparisons agree with
	// libvpx's tuned-rdmult winners.
	rdMult, rdDiv := 0, 0
	if e.activityMapValid {
		rdMult, rdDiv = e.libvpxRDConstantsWithZbinForFrame(qIndex, zbinOverQuant)
		rdMult = e.tunedRDMultiplier(rdMult, mbRow, mbCol)
	}
	chromaRD := func() (vp8common.MBPredictionMode, int, int, int, bool) {
		if chromaCache != nil && chromaCache.valid {
			return chromaCache.mode, chromaCache.rate, chromaCache.dist, chromaCache.eobSum, chromaCache.ok
		}
		uvMode, uvRate, uvDist, uvEOBSum, ok := predictBestIntraChromaModeRDWithProbsAndRDConstantsAndEOBs(src, qIndex, zbinOverQuant, actZbinAdj, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant, rdMult, rdDiv, pickerTokenCosts)
		if chromaCache != nil {
			chromaCache.mode = uvMode
			chromaCache.rate = uvRate
			chromaCache.dist = uvDist
			chromaCache.eobSum = uvEOBSum
			chromaCache.ok = ok
			chromaCache.valid = true
		}
		return uvMode, uvRate, uvDist, uvEOBSum, ok
	}
	if mbMode == vp8common.BPred {
		bModes, bRate, bDist, bPredEOBCount, ok := predictBestBPredLumaModeRDWithRDConstantsAndEOBs(src, qIndex, zbinOverQuant, actZbinAdj, false, mbRow, mbCol, nil, nil, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, bestRD, pickerProbs, e.modeProbs.BMode[:], fastQuant, rdMult, rdDiv, pickerTokenCosts)
		if !ok {
			return interIntraModeRDResult{}, false
		}
		yRate := bRate + e.interIntraYModeRate(vp8common.BPred)
		// Mirror libvpx vp8/encoder/rdopt.c rd_pick_intra4x4mby_modes lines
		// 591/634/637: libvpx starts the cumulative cost with
		// mbmode_cost[B_PRED] and rejects (returns INT_MAX) when the
		// final rdcost(cost, distortion) reaches best_rd. govpx's
		// predictBestBPredLumaModeRD bails per-block but accumulates
		// totalRate from 0 (without the mode cost), so a B_PRED candidate
		// whose final yrd including the mode cost matches or exceeds
		// best_yrd survives the in-loop bail. Mirror libvpx's reject
		// gate here so those candidates are dropped before they reach
		// the became_best comparison in the inter RD picker.
		yrd := e.rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate, bDist)
		if e.activityMapValid {
			yrd = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, yRate, bDist)
		}
		if bestRD > 0 && yrd >= bestRD {
			return interIntraModeRDResult{}, false
		}
		uvMode, uvRate, uvDist, uvEOBSum, ok := chromaRD()
		if !ok {
			return interIntraModeRDResult{}, false
		}
		uvModeRate := intraUVModeRateWithProbs(false, uvMode, e.modeProbs.UVMode[:])
		uvTokenRate := uvRate - uvModeRate
		yrd = e.rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate+uvModeRate, bDist)
		if e.activityMapValid {
			yrd = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, yRate+uvModeRate, bDist)
		}
		rate := yRate + uvRate + e.interIntraMacroblockModeRate()
		// Port libvpx vp8/encoder/rdopt.c calculate_final_rd_costs (lines
		// 1684-1714) tteob==0 rate2 backout for the B_PRED intra-in-inter
		// path. libvpx computes:
		//   has_y2_block = (mode != SPLITMV && mode != B_PRED)  // false here
		//   tteob = sum(eobs[0..15] > has_y2_block) = sum(eobs[0..15] > 0)
		//   if ref_frame == INTRA_FRAME: tteob += uv_intra_tteob
		// When tteob == 0 the picker drops rate_y + rate_uv from rate2
		// and adds the skip-flag delta. This is the B_PRED twin of the
		// DC/V/H/TM backout below; mirror libvpx verbatim so future
		// content that exercises B_PRED-with-all-zero-AC (synthetic
		// high-frequency-edge low-motion sequences) matches libvpx's
		// rate book-keeping.
		tteob := bPredEOBCount + uvEOBSum
		mbSkipCoeff := tteob == 0
		if mbSkipCoeff {
			rate -= bRate + uvRate - uvModeRate
			rate += e.interMacroblockSkipRate(true) - e.interMacroblockSkipRate(false)
			uvTokenRate = 0
		}
		score := e.rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, bDist+uvDist) + vp8enc.InterIntraRDPenalty(qIndex)
		if e.activityMapValid {
			score = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, rate, bDist+uvDist) + vp8enc.InterIntraRDPenalty(qIndex)
		}
		distortion := bDist + uvDist
		return interIntraModeRDResult{
			mode:         vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: uvMode, BModes: bModes, MBSkipCoeff: mbSkipCoeff},
			score:        score,
			yrd:          yrd,
			rate:         rate,
			rateY:        bRate,
			rateUV:       uvTokenRate,
			distortion:   distortion,
			distortionUV: uvDist,
		}, true
	}
	if mbMode < vp8common.DCPred || mbMode > vp8common.TMPred {
		return interIntraModeRDResult{}, false
	}
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, mbRow, mbCol, &mode, &e.reconstructScratch) {
		return interIntraModeRDResult{}, false
	}
	yRate, yDist, y2EOB, y2QCoeff, yACEOBCount := wholeBlockYTransformRDWithEOBs(src, &e.analysis.Img, mbRow, mbCol, zbinOverQuant, actZbinAdj, aboveTok, leftTok, quant, pickerProbs, fastQuant, pickerTokenCosts)
	uvMode, uvRate, uvDist, uvEOBSum, ok := chromaRD()
	if !ok {
		return interIntraModeRDResult{}, false
	}
	modeRate := e.interIntraYModeRate(mbMode)
	uvModeRate := intraUVModeRateWithProbs(false, uvMode, e.modeProbs.UVMode[:])
	uvTokenRate := uvRate - uvModeRate
	rate := yRate + uvRate + modeRate + e.interIntraMacroblockModeRate()
	// Port libvpx vp8/encoder/rdopt.c calculate_final_rd_costs (lines
	// 1684-1714) tteob==0 rate2 backout for the intra-in-inter-loop path.
	// libvpx computes:
	//   has_y2_block = (mode != SPLITMV && mode != B_PRED)  // true here
	//   tteob = eobs[Y2] + sum(eobs[0..15] > has_y2_block)
	//   if ref_frame == INTRA_FRAME: tteob += uv_intra_tteob
	// When tteob == 0 the picker drops rate_y + rate_uv from rate2 and
	// adds the skip-flag delta in their place. Without this, govpx
	// charges DC_PRED / V_PRED / H_PRED / TM_PRED the full coefficient
	// rate even when every Y AC and every UV coefficient quantizes to
	// zero; the 1280x720 screen-content fixture measured +20826-bit rate
	// inflation on flat-Y MBs (frame 1 MB(5,0) DC_PRED:
	// govpx rate=20838 vs libvpx rate=1012), driving the picker to spend
	// bits on NEWMV+LAST candidates whose `became_best=true` flip leaves
	// libvpx with DC_PRED+all-zero coefficients. Mirror libvpx verbatim.
	tteob := int(y2EOB) + yACEOBCount + uvEOBSum
	mbSkipCoeff := tteob == 0
	if mbSkipCoeff {
		rate -= yRate + uvRate - uvModeRate
		// libvpx also adjusts the skip-flag bit cost: line 1709-1712 adds
		// (cost_bit(prob_skip_false, 1) - cost_bit(prob_skip_false, 0)) when
		// the skip flag flips from 0 to 1. interIntraMacroblockModeRate
		// already accounts for cost_bit(prob, 0); add the delta.
		rate += e.interMacroblockSkipRate(true) - e.interMacroblockSkipRate(false)
		uvTokenRate = 0
	}
	score := e.rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, yDist+uvDist) + vp8enc.InterIntraRDPenalty(qIndex)
	yrd := e.rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate+modeRate+uvModeRate, yDist)
	if e.activityMapValid {
		score = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, rate, yDist+uvDist) + vp8enc.InterIntraRDPenalty(qIndex)
		yrd = e.tunedRDModeScoreWithZbin(qIndex, zbinOverQuant, mbRow, mbCol, yRate+modeRate+uvModeRate, yDist)
	}
	distortion := yDist + uvDist
	var staleY2 staleY2Snapshot
	if oracleTraceBuild {
		staleY2 = makeOracleStaleY2Snapshot(uint8(y2EOB), y2QCoeff)
	}
	return interIntraModeRDResult{
		mode:         vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: uvMode, MBSkipCoeff: mbSkipCoeff},
		score:        score,
		yrd:          yrd,
		rate:         rate,
		rateY:        yRate,
		rateUV:       uvTokenRate,
		distortion:   distortion,
		distortionUV: uvDist,
		staleY2:      staleY2,
	}, true
}

func (e *VP8Encoder) interModeForRDLoopEntry(
	src vp8enc.SourceImage, ref interAnalysisReference, refIndex int, mbMode vp8common.MBPredictionMode,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	newMVCandidates *[3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
		start    interFrameSearchStart
	},
	modeMVs *interModeMVSlots,
) (vp8enc.InterFrameMacroblockMode, bool) {
	switch mbMode {
	case vp8common.ZeroMV:
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: vp8common.ZeroMV}, true
	case vp8common.NearestMV, vp8common.NearMV:
		signBias := e.interFrameSignBias()
		var state interModeMVSlots
		if modeMVs != nil {
			state = *modeMVs
		} else {
			state = e.interModeMVSlots([]interAnalysisReference{ref}, [4]int8{-1, 0, -1, -1}, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
		}
		slot := interModeSignBiasSlotForReference(ref.Frame, signBias)
		// slot is 0 or 1 by construction; AND-mask with 1 elides BC on
		// the [2]MotionVector slot arrays.
		nearest, near := state.nearest[slot&1], state.near[slot&1]
		mv := nearest
		if mbMode == vp8common.NearMV {
			mv = near
		}
		mv = vp8enc.ClampInterMotionVectorToModeEdges(mv, mbRow, mbCol, mbRows, mbCols)
		if mv.IsZero() {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		if !vp8enc.InterFrameUMVFullPixelInRange(mv, mbRow, mbCol, mbRows, mbCols) {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: mbMode, MV: mv}, true
	case vp8common.NewMV:
		if uint(refIndex) >= uint(len(newMVCandidates)) {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		candidate := &newMVCandidates[refIndex]
		if !candidate.searched {
			signBias := e.interFrameSignBias()
			bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
			if modeMVs != nil {
				bestRefMV = modeMVs.best[interModeSignBiasSlotForReference(ref.Frame, signBias)&1]
			}
			search := e.interAnalysisSearchConfig()
			start := e.improvedInterFrameSearchStart(src, ref.Frame, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, search, nil)
			var motionStats interFrameMotionSearchStats
			var stats *interFrameMotionSearchStats
			if vp8PhaseStatsEnabled {
				if phaseStats := e.phaseStats(); phaseStats != nil && !e.threadedRowsActive {
					motionStats.phase = phaseStats
					stats = &motionStats
				}
			}
			searcher := interFrameMotionVectorSearch{
				src:         src,
				ref:         ref.Img,
				mbRow:       mbRow,
				mbCol:       mbCol,
				mbRows:      mbRows,
				mbCols:      mbCols,
				bestRefMV:   bestRefMV,
				qIndex:      qIndex,
				errorPerBit: e.tunedErrorPerBit(qIndex, mbRow, mbCol),
				search:      search,
				start:       start,
				mvProbs:     &e.modeProbs.MV,
				mvCosts:     e.currentMotionVectorCostTables(),
			}
			var result interFrameMotionVectorSearchResult
			if stats != nil {
				result = searcher.selectRDWithStats(stats)
			} else {
				result = searcher.selectRD()
			}
			mv := result.mv
			mv = vp8enc.ClampInterMotionVectorToModeEdges(mv, mbRow, mbCol, mbRows, mbCols)
			candidate.searched = true
			candidate.ok = true
			candidate.mv = mv
			candidate.start = start
		}
		if !candidate.ok {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		mode := vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: vp8common.NewMV, MV: candidate.mv}
		return mode, true
	default:
		return vp8enc.InterFrameMacroblockMode{}, false
	}
}

func interReferenceSearchOrder(refs []interAnalysisReference, refCount int) [4]int8 {
	order := [4]int8{-1, -1, -1, -1}
	searchSlot := 1
	// Hoist the min(refCount, len(refs)) bound out of the loop condition.
	refLimit := min(refCount, len(refs))
	for refIndex := 0; refIndex < refLimit && searchSlot < len(order); refIndex++ {
		if refs[refIndex].Img == nil {
			continue
		}
		switch refs[refIndex].Frame {
		case vp8common.LastFrame, vp8common.GoldenFrame, vp8common.AltRefFrame:
			// searchSlot is in [1,4) by the loop condition above; AND-mask
			// with 3 elides the bounds check on the [4]int8 order array.
			order[searchSlot&3] = int8(refIndex)
			searchSlot++
		}
	}
	return order
}

func interReferenceBySearchSlot(refs []interAnalysisReference, searchOrder [4]int8, refSlot int) (interAnalysisReference, int, bool) {
	// uint(refSlot-1) folds (refSlot <= 0) and (refSlot >= len) into one
	// branch: refSlot=0 → -1 wraps to huge, refSlot=len → len-1 >= len-1
	// (false on the upper bound for the original test); use len-1 as
	// inclusive max instead.
	if uint(refSlot-1) >= uint(len(searchOrder)-1) {
		return interAnalysisReference{}, 0, false
	}
	refIndex := int(searchOrder[refSlot])
	if uint(refIndex) >= uint(len(refs)) || refs[refIndex].Img == nil {
		return interAnalysisReference{}, 0, false
	}
	return refs[refIndex], refIndex, true
}

type interModeMVSlots struct {
	nearest [2]vp8enc.MotionVector
	near    [2]vp8enc.MotionVector
	best    [2]vp8enc.MotionVector
	counts  vp8enc.InterModeCounts
}

func interModeSignBiasSlot(bias bool) int {
	if bias {
		return 1
	}
	return 0
}

func interModeSignBiasSlotForReference(refFrame vp8common.MVReferenceFrame, signBias [vp8common.MaxRefFrames]bool) int {
	// Single uint range check folds the refFrame >= 0 and < len guards.
	if uint(refFrame) >= uint(len(signBias)) {
		return 0
	}
	return interModeSignBiasSlot(signBias[refFrame])
}

func (e *VP8Encoder) interModeMVSlots(
	refs []interAnalysisReference, refSearchOrder [4]int8,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	mbRow int, mbCol int, mbRows int, mbCols int,
) interModeMVSlots {
	var state interModeMVSlots
	baseRef, _, ok := interReferenceBySearchSlot(refs, refSearchOrder, 1)
	if !ok {
		return state
	}
	signBias := e.interFrameSignBias()
	slot := interModeSignBiasSlotForReference(baseRef.Frame, signBias)
	// Single-pass neighbor walk: libvpx vp8_find_near_mvs_bias computes
	// nearest/near/best_ref and mdcounts together in one sweep over the
	// three neighbor modes. The previous three-helper form repeated the
	// identical findNearInterMotionVectors walk per output and was a
	// measurable share of the per-MB picker setup in the 720p realtime
	// profile. Outputs (including clamps) are value-identical.
	nearest, near, best, counts := vp8enc.InterFrameNearMVsCountsAt(above, left, aboveLeft, baseRef.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
	state.counts = counts
	// slot from interModeSignBiasSlotForReference is 0 or 1; AND-mask
	// with 1 elides the bounds check on the [2]MotionVector slot arrays
	// for both the active slot and its opposite.
	state.nearest[slot&1] = nearest
	state.near[slot&1] = near
	state.best[slot&1] = best
	opp := (1 - slot) & 1
	state.nearest[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -nearest.Row, Col: -nearest.Col}, mbRow, mbCol, mbRows, mbCols)
	state.near[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -near.Row, Col: -near.Col}, mbRow, mbCol, mbRows, mbCols)
	state.best[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -best.Row, Col: -best.Col}, mbRow, mbCol, mbRows, mbCols)
	return state
}

type interAnalysisReference struct {
	Img        *vp8common.Image
	RefRate    int
	Frame      vp8common.MVReferenceFrame
	RefRateSet bool
}

type interMacroblockImageSnapshot struct {
	y [16 * 16]byte
	u [8 * 8]byte
	v [8 * 8]byte
}

func snapshotInterMacroblockImage(img *vp8common.Image, row int, col int, snap *interMacroblockImageSnapshot) {
	if img == nil || snap == nil {
		return
	}
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	copyMacroblockY(snap.y[:], 16, img.Y[yOff:], img.YStride)
	copyMacroblock8x8(snap.u[:], 8, img.U[uOff:], img.UStride)
	copyMacroblock8x8(snap.v[:], 8, img.V[vOff:], img.VStride)
}

func restoreInterMacroblockImage(img *vp8common.Image, row int, col int, snap *interMacroblockImageSnapshot) {
	if img == nil || snap == nil {
		return
	}
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	copyMacroblockY(img.Y[yOff:], img.YStride, snap.y[:], 16)
	copyMacroblock8x8(img.U[uOff:], img.UStride, snap.u[:], 8)
	copyMacroblock8x8(img.V[vOff:], img.VStride, snap.v[:], 8)
}

type interAnalysisMotionCandidate struct {
	Ref interAnalysisReference
	MV  vp8enc.MotionVector
}

func (e *VP8Encoder) interAnalysisReferences(flags EncodeFlags, refs *[3]interAnalysisReference) int {
	count := 0
	lastRate, goldenRate, altRate := e.interReferenceFrameRatesForFlags(flags)
	lastEnabled, goldenEnabled, altEnabled := e.interReferenceAvailability(flags)
	if lastEnabled {
		refs[count] = interAnalysisReference{Frame: vp8common.LastFrame, Img: &e.lastRef.Img, RefRate: lastRate, RefRateSet: true}
		count++
	}
	if goldenEnabled {
		refs[count] = interAnalysisReference{Frame: vp8common.GoldenFrame, Img: &e.goldenRef.Img, RefRate: goldenRate, RefRateSet: true}
		count++
	}
	if altEnabled {
		refs[count] = interAnalysisReference{Frame: vp8common.AltRefFrame, Img: &e.altRef.Img, RefRate: altRate, RefRateSet: true}
		count++
	}
	return count
}

func prepareInterAnalysisReferenceStates(refs []interAnalysisReference, refCount int, useBilinear bool) [3]vp8dec.InterFrameRefState {
	var states [3]vp8dec.InterFrameRefState
	cfg := vp8dec.InterPredictionConfig{UseBilinear: useBilinear}
	limit := min(refCount, len(refs))
	for i := range limit {
		ref := &refs[i]
		if ref.Img == nil || ref.Frame < vp8common.LastFrame || ref.Frame > vp8common.AltRefFrame {
			continue
		}
		states[int(ref.Frame-vp8common.LastFrame)] = vp8dec.PrepareInterFrameRefState(ref.Img, cfg)
	}
	return states
}

func interAnalysisReferenceState(states *[3]vp8dec.InterFrameRefState, frame vp8common.MVReferenceFrame) *vp8dec.InterFrameRefState {
	if states == nil || frame < vp8common.LastFrame || frame > vp8common.AltRefFrame {
		return nil
	}
	return &states[int(frame-vp8common.LastFrame)]
}

func (e *VP8Encoder) closestInterAnalysisReference(refs []interAnalysisReference, refCount int) vp8common.MVReferenceFrame {
	closest := vp8common.IntraFrame
	limit := min(refCount, len(refs))
	for i := range limit {
		refFrame := refs[i].Frame
		if refFrame < vp8common.LastFrame || refFrame >= vp8common.MaxRefFrames {
			continue
		}
		if closest == vp8common.IntraFrame || e.referenceFrameNumbers[refFrame] > e.referenceFrameNumbers[closest] {
			closest = refFrame
		}
	}
	if closest == vp8common.IntraFrame {
		return vp8common.LastFrame
	}
	return closest
}

func interAnalysisReferencesInclude(refs []interAnalysisReference, refCount int, frame vp8common.MVReferenceFrame) bool {
	limit := min(refCount, len(refs))
	for i := range limit {
		if refs[i].Frame == frame {
			return true
		}
	}
	return false
}

func interAnalysisValidReferenceCount(refs []interAnalysisReference, refCount int) int {
	limit := min(refCount, len(refs))
	count := 0
	for i := range limit {
		if refs[i].Img != nil && refs[i].Frame >= vp8common.LastFrame && refs[i].Frame < vp8common.MaxRefFrames {
			count++
		}
	}
	return count
}

func (e *VP8Encoder) interAnalysisMacroblockCount() int {
	if min(e.opts.Width, e.opts.Height) > 0 {
		return geometry.MacroblockCount(e.opts.Width, e.opts.Height)
	}
	return len(e.interFrameModes)
}

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c motion search.
// vp8_hex_search finishes with an eight-step full-pixel diamond refinement.
