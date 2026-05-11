package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
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
	for refIndex := 1; refIndex < refCount; refIndex++ {
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
	ref           interAnalysisReference
	interMode     vp8enc.InterFrameMacroblockMode
	useIntra      bool
	intraMode     vp8enc.InterFrameMacroblockMode
	projectedRate int
	staleY2       staleY2Snapshot
	// predictionError is the picker `distortion` scalar returned through
	// vp8_encode_inter_macroblock and accumulated into mb.prediction_error.
	predictionError int
}

type staleY2Snapshot struct {
	set    bool
	eob    uint8
	qcoeff [16]int16
}

func (d interFrameModeDecision) cyclicRefreshEligible() bool {
	return !d.useIntra && d.interMode.RefFrame == vp8common.LastFrame && d.interMode.Mode == vp8common.ZeroMV
}

func libvpxAddProjectedMacroblockRate(total int, rate int) int {
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
	segmentQIndex := encoderSegmentQIndex(baseQIndex, segmentation, segmentID)
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
	return e.selectRDInterFrameModeDecision(
		src, refs, refCount,
		mbRow, mbCol, mbRows, mbCols,
		segmentQIndex, segmentID,
		above, left, aboveLeft,
		aboveTok, leftTok,
		quant,
		sourceAltRefZeroMVOnly,
	)
}

func (e *VP8Encoder) sourceAltRefZeroMVOnly(flags EncodeFlags) bool {
	return e != nil &&
		flags&EncodeInvisibleFrame == 0 &&
		e.opts.ARNRMaxFrames == 0 &&
		e.isSrcFrameAltRef(e.currentSourcePTS)
}

func (e *VP8Encoder) interMacroblockInactive(mbRow int, mbCol int, mbCols int) bool {
	if e == nil || !e.activeMapEnabled || mbCols <= 0 {
		return false
	}
	index := mbRow*mbCols + mbCol
	return index >= 0 && index < len(e.activeMap) && e.activeMap[index] == 0
}

func libvpxSourceAltRefCandidate(onlyAltRefZeroMV bool, refFrame vp8common.MVReferenceFrame, mode vp8common.MBPredictionMode) bool {
	return !onlyAltRefZeroMV || (mode == vp8common.ZeroMV && refFrame == vp8common.AltRefFrame)
}

// selectRDInterFrameModeDecision mirrors libvpx vp8/encoder/rdopt.c
// vp8_rd_pick_inter_mode. Token-context commit parity: each candidate-mode
// trial passes aboveTok/leftTok by pointer to the per-mode RD subroutines
// (estimateInterIntraModeRDScore, estimateInterResidualRDAccounting,
// selectInterFrameSplitModeRDScore), but every one of those subroutines
// snapshots the planes into stack-local arrays before mutating them — see
// wholeBlockYTransformRD, wholeBlockChromaTransformRD,
// predictBestBPredLumaModeRD, predictBestIntraChromaModeRD, and
// buildPredictedMacroblockCoefficientsRD. This matches libvpx's "tempa /
// templ" copies inside vp8_rd_pick_inter_mode (rdopt.c) and
// rd_pick_intra4x4block (rdopt.c): only the chosen mode's contexts are
// committed to the per-MB row state. The commit happens later in
// buildReconstructingInterFrameCoefficientsWithSegmentation via
// updateInterAnalysisTokenContext after the winning mode's residual has been
// reconstructed, mirroring libvpx's encode_mb_row "*a/*l" assignment after
// vp8_encode_inter16x16 / vp8_encode_intra4x4mby. The RD picker therefore
// never mutates the caller's aboveTok/leftTok during candidate evaluation.
func (e *VP8Encoder) selectRDInterFrameModeDecision(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int,
	qIndex int, segmentID uint8,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes,
	quant *vp8enc.MacroblockQuant,
	sourceAltRefZeroMVOnly bool,
) (interFrameModeDecision, bool) {
	if !e.interRDFrameActive {
		e.beginInterRDModeDecisionMacroblock()
	}
	// Stage the picker → accepted-path DCT cache. Both slots start
	// invalid; each candidate that calls into
	// buildPredictedMacroblockCoefficients writes into the scratch slot.
	// When a candidate becomes best, we flip the winner index so the
	// winning candidate's DCTs end up in the (new) winner slot without
	// any data copy. The accepted-path consumer (in
	// buildReconstructingInterFrameCoefficientsWithSegmentation) then
	// reads slots[winner] and resets it.
	e.interRDCoeffCacheSlots[0].reset()
	e.interRDCoeffCacheSlots[1].reset()
	e.interRDCoeffCacheScratchTarget = &e.interRDCoeffCacheSlots[1-e.interRDCoeffCacheWinner]
	defer func() {
		e.interRDCoeffCacheScratchTarget = nil
	}()
	traceEnabled := e.oracleTraceEnabled()
	thresholds := e.interModeRDThresholdsForReferences(qIndex, refs, refCount)
	bestSet := false
	bestScore := maxInt()
	bestYRD := maxInt()
	bestDistortion := 0
	bestModeIndex := -1
	best := interFrameModeDecision{
		intraMode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred, SegmentID: segmentID},
	}
	var lastStaleY2 staleY2Snapshot
	var newMVCandidates [3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
		start    interFrameSearchStart
	}
	if !e.interRDFrameRefSearchOrderValid {
		e.interRDFrameRefSearchOrder = libvpxInterReferenceSearchOrder(refs, refCount)
		e.interRDFrameRefSearchOrderValid = true
	}
	refSearchOrder := e.interRDFrameRefSearchOrder
	modeMVs := e.interModeMVSlots(refs, refSearchOrder, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
	signBias := e.interFrameSignBias()
	inactiveMB := e.interMacroblockInactive(mbRow, mbCol, mbCols)

	for modeIndex, mbMode := range libvpxFastInterModeOrder {
		threshold := thresholds[modeIndex]
		if threshold == libvpxInterModeThresholdDisabled {
			continue
		}
		if bestSet && bestScore <= threshold {
			continue
		}
		// Reset the scratch DCT cache before each candidate evaluation so
		// the cache's valid bit accurately reflects whether THIS candidate
		// populated the slot. The slot remains the same target pointer
		// across iterations; we only clear the valid bit.
		e.interRDCoeffCacheScratchTarget.valid = false

		refSlot := libvpxFastRefFrameOrder[modeIndex]
		if refSlot == 0 {
			if !e.interRDModeTestAllowed(modeIndex) {
				continue
			}
			e.recordInterRDModeTest(modeIndex)
			if !libvpxSourceAltRefCandidate(sourceAltRefZeroMVOnly, vp8common.IntraFrame, mbMode) {
				continue
			}
			bestScoreBefore := bestScore
			bestYRDBefore := bestYRD
			mode, score, yrd, rate, distortion, candidateStaleY2, ok := e.estimateInterIntraModeRDScore(src, qIndex, mbRow, mbCol, mbMode, bestYRD, aboveTok, leftTok, quant)
			// libvpx vp8/encoder/rdopt.c B_PRED case (lines 1949-1971):
			// when rd_pick_intra4x4mby_modes returns tmp_rd >= best_yrd
			// the case sets `this_rd = INT_MAX, disable_skip = 1` and
			// falls through to the post-loop best/raise mutation block
			// at lines 2235-2267. The else branch there raises
			// `rd_thresh_mult[mode_index] += 4` and rewrites
			// `rd_threshes[mode_index]`. govpx's intra/B_PRED RD scorer
			// signals that same dropout as `ok == false`; we still need
			// to mirror libvpx's raise so the next MB sees the same
			// pruning threshold (otherwise BPred and the other intra
			// modes carry stale low thresholds across MBs and the
			// per-frame `rd_threshes` evolution drifts -- caught by
			// TestOracleInterCandidateThresholdEvolution
			// good-quality-vbr-cpu3, frame=1 mb=(3,3) BPred 97500 vs
			// 136980).
			if !ok {
				e.raiseInterRDThreshold(modeIndex)
				continue
			}
			if candidateStaleY2.set {
				lastStaleY2 = candidateStaleY2
			}
			mode.SegmentID = segmentID
			becameBest := !bestSet || score < bestScore
			if traceEnabled {
				e.emitOracleInterCandidateTrace(oracleTraceInterCandidateSummary{
					Picker:          "rd",
					MBRow:           mbRow,
					MBCol:           mbCol,
					ModeIndex:       modeIndex,
					Mode:            mode.Mode,
					RefSlot:         0,
					RefFrame:        vp8common.IntraFrame,
					Threshold:       threshold,
					BestScoreBefore: bestScoreBefore,
					BestYRDBefore:   bestYRDBefore,
					BestSSEBefore:   oracleTraceInterCandidateUnknown,
					Outcome:         "tested",
					BecameBest:      becameBest,
					Score:           score,
					YRD:             yrd,
					Rate:            rate,
					RateY:           oracleTraceInterCandidateUnknown,
					RateUV:          oracleTraceInterCandidateUnknown,
					Distortion:      oracleTraceInterCandidateUnknown,
					DistortionUV:    oracleTraceInterCandidateUnknown,
					SSE:             oracleTraceInterCandidateUnknown,
					Skip:            mode.MBSkipCoeff,
					ModeTrace:       mode,
					HasModeTrace:    true,
				})
			}
			if becameBest {
				e.lowerInterRDThresholdForImprovement(modeIndex)
				bestSet = true
				bestScore = score
				bestYRD = yrd
				bestDistortion = distortion
				bestModeIndex = modeIndex
				best = interFrameModeDecision{useIntra: true, intraMode: mode, projectedRate: rate, predictionError: distortion}
				if mode.Mode == vp8common.BPred {
					best.staleY2 = lastStaleY2
				}
				// Flip the cache winner/scratch indices. The intra path
				// did NOT populate the scratch slot
				// (estimateInterIntraModeRDScore uses
				// wholeBlockYTransformRD, not
				// buildPredictedMacroblockCoefficientsRD), so the new
				// winner slot's valid bit stays false and the accepted
				// path falls back to the full coefficient build. The
				// scratch pointer flips so subsequent inter candidates
				// write into what used to be the winner slot, preserving
				// the just-promoted candidate's cache if it had one.
				e.interRDCoeffCacheWinner ^= 1
				e.interRDCoeffCacheScratchTarget = &e.interRDCoeffCacheSlots[1-e.interRDCoeffCacheWinner]
			} else {
				e.raiseInterRDThreshold(modeIndex)
			}
			continue
		}

		ref, refIndex, ok := interReferenceBySearchSlot(refs, refSearchOrder, refSlot)
		if !ok {
			continue
		}
		refBiasSlot := interModeSignBiasSlotForReference(ref.Frame, signBias)
		bestRefMV := modeMVs.best[refBiasSlot]
		if !e.interRDModeTestAllowed(modeIndex) {
			continue
		}
		e.recordInterRDModeTest(modeIndex)
		if !libvpxSourceAltRefCandidate(sourceAltRefZeroMVOnly, ref.Frame, mbMode) {
			continue
		}
		bestScoreBefore := bestScore
		bestYRDBefore := bestYRD
		var mode vp8enc.InterFrameMacroblockMode
		var score int
		var yrd int
		var rate int
		rateY := oracleTraceInterCandidateUnknown
		rateUV := oracleTraceInterCandidateUnknown
		distortion := oracleTraceInterCandidateUnknown
		distortionUV := oracleTraceInterCandidateUnknown
		mbSkipCoeff := false
		rdLoopSkip := false
		var candidateStaleY2 staleY2Snapshot
		if mbMode == vp8common.SplitMV {
			mvthresh := e.splitMVSubsearchThresholdForSlot(qIndex, refs, refCount, refSlot)
			mode, score, yrd, rate, distortion, rdLoopSkip, ok = e.selectInterFrameSplitModeRDScore(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, modeMVs.counts, qIndex, segmentID, mvthresh, bestYRD, above, left, aboveLeft, aboveTok, leftTok, quant)
		} else {
			mode, ok = e.interModeForRDLoopEntry(src, ref, refIndex, mbMode, mbRow, mbCol, mbRows, mbCols, qIndex, above, left, aboveLeft, &newMVCandidates, &modeMVs)
			if ok {
				mode.SegmentID = segmentID
				if inactiveMB {
					mode.SegmentID = 0
					mode.MBSkipCoeff = true
					score = maxInt()
					yrd = maxInt()
					rate = e.interMotionModeRateWithReferenceRateAndModeContext(&mode, left, above, e.interReferenceFrameRateForReference(ref), modeMVs.counts, bestRefMV, libvpxRDNewMVBitCostWeight)
					distortion = 0
					mbSkipCoeff = true
					rdLoopSkip = true
				} else {
					acct, acctOK := e.estimateInterResidualRDAccountingWithModeContext(src, ref.Img, mbRow, mbCol, mbRows, mbCols, &mode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, e.interReferenceFrameRateForReference(ref), modeMVs.counts, bestRefMV)
					ok = acctOK
					score = acct.rd
					yrd = acct.yrd
					rate = acct.rate2
					rateY = acct.rateY
					rateUV = acct.rateUV
					distortion = acct.distortion2
					distortionUV = acct.distortionUV
					mbSkipCoeff = acct.mbSkipCoeff
					rdLoopSkip = acct.rdLoopSkip
					candidateStaleY2 = acct.staleY2
				}
			}
		}
		if !ok {
			continue
		}
		if candidateStaleY2.set {
			lastStaleY2 = candidateStaleY2
		}
		becameBest := rdLoopSkip || !bestSet || score < bestScore
		if traceEnabled {
			e.emitOracleInterCandidateTrace(oracleTraceInterCandidateSummary{
				Picker:          "rd",
				MBRow:           mbRow,
				MBCol:           mbCol,
				ModeIndex:       modeIndex,
				Mode:            mode.Mode,
				RefSlot:         refSlot,
				RefFrame:        ref.Frame,
				Threshold:       threshold,
				BestScoreBefore: bestScoreBefore,
				BestYRDBefore:   bestYRDBefore,
				BestSSEBefore:   oracleTraceInterCandidateUnknown,
				Outcome:         "tested",
				BecameBest:      becameBest,
				LoopBreak:       rdLoopSkip,
				Score:           score,
				YRD:             yrd,
				Rate:            rate,
				RateY:           rateY,
				RateUV:          rateUV,
				Distortion:      distortion,
				DistortionUV:    distortionUV,
				SSE:             oracleTraceInterCandidateUnknown,
				Skip:            mbSkipCoeff || mode.MBSkipCoeff,
				ModeTrace:       mode,
				HasModeTrace:    true,
			})
		}
		if becameBest {
			e.lowerInterRDThresholdForImprovement(modeIndex)
			bestSet = true
			bestScore = score
			bestYRD = yrd
			bestDistortion = distortion
			bestModeIndex = modeIndex
			best = interFrameModeDecision{ref: ref, interMode: mode, intraMode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred, SegmentID: segmentID}, projectedRate: rate, predictionError: distortion}
			if mode.Mode == vp8common.SplitMV {
				best.staleY2 = lastStaleY2
			}
			// Flip the cache winner/scratch indices so the just-evaluated
			// inter candidate's DCTs become the winner slot. For inactiveMB
			// or staticInterRDEncodeBreakoutDistortion winners,
			// estimateInterResidualRDAccountingWithModeContext skipped
			// buildPredictedMacroblockCoefficientsRD entirely so the new
			// winner slot's valid bit stays false — the accepted path then
			// falls back to the original full coefficient build (and most
			// such winners hit breakoutSkip anyway, bypassing
			// buildPredictedMacroblockCoefficients).
			e.interRDCoeffCacheWinner ^= 1
			e.interRDCoeffCacheScratchTarget = &e.interRDCoeffCacheSlots[1-e.interRDCoeffCacheWinner]
		} else {
			e.raiseInterRDThreshold(modeIndex)
		}
		if rdLoopSkip {
			break
		}
	}
	if !bestSet {
		return interFrameModeDecision{}, false
	}
	if bestModeIndex >= 0 {
		e.lowerBestInterRDThreshold(bestModeIndex)
	}
	best.predictionError = bestDistortion
	return best, true
}

func (e *VP8Encoder) selectInterFrameSplitModeRDScore(
	src vp8enc.SourceImage, ref interAnalysisReference,
	mbRow int, mbCol int, mbRows int, mbCols int,
	bestRefMV vp8enc.MotionVector, modeCounts vp8enc.InterModeCounts, qIndex int, segmentID uint8, mvthresh int, bestYRD int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes,
	quant *vp8enc.MacroblockQuant,
) (vp8enc.InterFrameMacroblockMode, int, int, int, int, bool, bool) {
	// libvpx: vp8_rd_pick_inter_mode SPLITMV branch picks
	// x->rd_threshes[THR_NEW{1,2,3}] based on vp8_ref_frame_order[mode_index]
	// (1=LAST, 2=GOLDEN, 3=ALTREF) and feeds it into
	// vp8_rd_pick_best_mbsegmentation as bsi->mvthresh, which the per-label
	// loop divides by label_count to gate NEW4X4 motion searches.
	bestSet := false
	bestSegmentYRD := bestYRD
	if bestSegmentYRD <= 0 {
		bestSegmentYRD = maxInt()
	}
	var bestMode vp8enc.InterFrameMacroblockMode
	var splitSeeds splitMotionSearchSeeds

	tryPartition := func(partition int) bool {
		var labelRD splitMotionLabelRDEvaluator
		initSplitMotionLabelRDEvaluator(&labelRD, e.rc.currentZbinOverQuant, aboveTok, leftTok, e.libvpxUseFastQuantForPick(), false)
		overheadRate := mbSplitPartitionRate(uint8(partition)) + interPredictionModeRate(vp8common.SplitMV, modeCounts)
		overheadRD := rdModeScoreWithZbin(qIndex, e.rc.currentZbinOverQuant, overheadRate, 0)
		shape := selectInterFrameSplitMotionModeWithSegmentCutoff(src, ref.Img, ref.Frame, mbRow, mbCol, bestRefMV, qIndex, partition, left, above, e.interAnalysisSearchConfig(), e.interAnalysisCompressorSpeed(), &splitSeeds, &e.modeProbs.MV, mvthresh, &labelRD, quant, e.pickerCoefProbs(), bestSegmentYRD, overheadRD)
		if !shape.OK {
			return false
		}
		// libvpx: when this_segment_rd >= bsi->segment_rd at any label,
		// rd_check_segment returns without updating bsi (no bsi.r/bsi.d
		// commit). govpx mirrors that — the abandoned shape is not
		// considered for best mode and does not refresh bestSegmentYRD.
		if shape.Cutoff {
			return false
		}
		mode := shape.Mode
		mode.SegmentID = segmentID
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
		return false
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
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false, false
	}
	acct, ok := e.estimateInterResidualRDAccountingWithModeContext(src, ref.Img, mbRow, mbCol, mbRows, mbCols, &bestMode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, e.interReferenceFrameRateForReference(ref), modeCounts, bestRefMV)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false, false
	}
	return bestMode, acct.rd, acct.yrd, acct.rate2, acct.distortion2, acct.rdLoopSkip, true
}

func (e *VP8Encoder) splitMVSubsearchThresholdForSlot(qIndex int, refs []interAnalysisReference, refCount int, refSlot int) int {
	thresholds := e.interModeRDThresholdsForReferences(qIndex, refs, refCount)
	return libvpxSplitMVSubsearchThreshold(thresholds, refSlot)
}

func libvpxSplitMVSubsearchThreshold(thresholds [libvpxInterModeCount]int, refSlot int) int {
	switch refSlot {
	case 1:
		return thresholds[libvpxThrNew1]
	case 2:
		return thresholds[libvpxThrNew2]
	default:
		return thresholds[libvpxThrNew3]
	}
}

func (e *VP8Encoder) estimateInterIntraModeRDScore(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, mbMode vp8common.MBPredictionMode, bestRD int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant) (vp8enc.InterFrameMacroblockMode, int, int, int, int, staleY2Snapshot, bool) {
	zbinOverQuant := 0
	if e != nil {
		zbinOverQuant = e.rc.currentZbinOverQuant
	}
	fastQuant := e.libvpxUseFastQuantForPick()
	pickerProbs := e.pickerCoefProbs()
	if mbMode == vp8common.BPred {
		bModes, bRate, bDist, ok := predictBestBPredLumaModeRD(src, qIndex, zbinOverQuant, false, mbRow, mbCol, nil, nil, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, bestRD, pickerProbs, fastQuant)
		if !ok {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
		}
		uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRDWithProbs(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant)
		if !ok {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
		}
		yRate := bRate + e.interIntraYModeRate(vp8common.BPred)
		rate := yRate + uvRate + e.interIntraMacroblockModeRate()
		score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, bDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
		yrd := rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate, bDist)
		distortion := bDist + uvDist
		return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: uvMode, BModes: bModes}, score, yrd, rate, distortion, staleY2Snapshot{}, true
	}
	if mbMode < vp8common.DCPred || mbMode > vp8common.TMPred {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
	}
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, mbRow, mbCol, &mode, &e.reconstructScratch) {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
	}
	yRate, yDist, y2EOB, y2QCoeff := wholeBlockYTransformRD(src, &e.analysis.Img, mbRow, mbCol, qIndex, zbinOverQuant, aboveTok, leftTok, quant, pickerProbs, fastQuant)
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRDWithProbs(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, staleY2Snapshot{}, false
	}
	modeRate := e.interIntraYModeRate(mbMode)
	rate := yRate + uvRate + modeRate + e.interIntraMacroblockModeRate()
	score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, yDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
	yrd := rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate+modeRate, yDist)
	distortion := yDist + uvDist
	staleY2 := staleY2Snapshot{set: true, eob: y2EOB, qcoeff: y2QCoeff}
	return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: uvMode}, score, yrd, rate, distortion, staleY2, true
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
			state = e.interModeMVSlots([]interAnalysisReference{ref}, [4]int{-1, 0, -1, -1}, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
		}
		slot := interModeSignBiasSlotForReference(ref.Frame, signBias)
		nearest, near := state.nearest[slot], state.near[slot]
		mv := nearest
		if mbMode == vp8common.NearMV {
			mv = near
		}
		mv = clampInterMotionVectorToModeEdges(mv, mbRow, mbCol, mbRows, mbCols)
		if mv.IsZero() {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		if !interFrameUMVFullPixelInRange(mv, mbRow, mbCol, mbRows, mbCols) {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: mbMode, MV: mv}, true
	case vp8common.NewMV:
		if refIndex < 0 || refIndex >= len(newMVCandidates) {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		candidate := &newMVCandidates[refIndex]
		if !candidate.searched {
			signBias := e.interFrameSignBias()
			bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
			if modeMVs != nil {
				bestRefMV = modeMVs.best[interModeSignBiasSlotForReference(ref.Frame, signBias)]
			}
			search := e.interAnalysisSearchConfig()
			start := e.improvedInterFrameSearchStart(src, ref.Frame, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, search)
			var motionStats interFrameMotionSearchStats
			var stats *interFrameMotionSearchStats
			if e.opts.PhaseStats != nil && !e.threadedRowsActive {
				motionStats.phase = e.opts.PhaseStats
				stats = &motionStats
			}
			result := interFrameMotionVectorSearch{
				src:       src,
				ref:       ref.Img,
				mbRow:     mbRow,
				mbCol:     mbCol,
				mbRows:    mbRows,
				mbCols:    mbCols,
				bestRefMV: bestRefMV,
				qIndex:    qIndex,
				search:    search,
				start:     start,
				mvProbs:   &e.modeProbs.MV,
				mvCosts:   e.currentMotionVectorCostTables(),
				stats:     stats,
			}.selectRD()
			mv := result.mv
			mv = clampInterMotionVectorToModeEdges(mv, mbRow, mbCol, mbRows, mbCols)
			candidate.searched = true
			candidate.ok = true
			candidate.mv = mv
			candidate.start = start
		}
		if !candidate.ok {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		mode := vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: vp8common.NewMV, MV: candidate.mv}
		attachImprovedMVTrace(&mode, candidate.start)
		return mode, true
	default:
		return vp8enc.InterFrameMacroblockMode{}, false
	}
}

// selectFastInterFrameModeDecision mirrors libvpx vp8/encoder/pickinter.c
// vp8_pick_inter_mode (the non-RD fast picker used by good-cpu>=4 and
// realtime). The fast picker scores each mode_index candidate by
// `RDCOST(rdmult, rddiv, rate2, distortion2)` where distortion2 is
// `vpx_variance16x16(src, predictor)` — pixel-domain variance of the
// motion-compensated residual.
//
// R9-2 (parity-close-r9-2-bpred-picker): aligned the inter B_PRED fast
// picker's per-block scoring with libvpx via two changes in
// estimateFastBPredIntraModeScore:
//  1. Per-mode rate now reads libvpx's stale `inter_bmode_costs` table
//     via libvpxInterFastBpredModeCost — slots 0..3 (B_DC..B_HE) carry
//     sub_mv_ref token costs after vp8_init_mode_costs's two-step init,
//     and the fast picker's mode loop reads only those four slots.
//  2. After each per-block winner is chosen the function runs
//     vp8_encode_intra4x4block-equivalent DCT/quantize/IDCT-add into
//     the analysis Y plane so the next sub-block's predictor neighbors
//     come from reconstructed pixels, matching libvpx's deferred
//     vp8_encode_intra4x4block call inside pick_intra4x4block.
//
// Result: TestOracleEncoderQHistogramScoreboard's three rt-cpu0/4/8
// 128x128 fixtures dropped from hist_l1=2 to hist_l1=0 (byte-identical
// per-frame Q histograms vs libvpx). The TestOracleInterModeDistribution
// 256x256-panning fixture also tightened to l1_pp=0.
//
// PIN (residual): 1 inter MB in TestOracleEncoderQHistogramScoreboard's
// good-cpu5-128x128 fixture (frame 5 MB(0,7)) still picks NEWMV/GOLDEN
// at MV(-120,-76) here while libvpx picks B_PRED at the same MB. Both
// pickers find the same NEWMV(GOLDEN, -120, -76) candidate (MB(0,7) is
// the top-right corner so the search hits a flat UMV-extension region
// with low variance). R9-2's libvpxInterFastBpredModeCost + per-block
// reconstruction fix is active here, but the residual divergence comes
// from a downstream rate-control / mode-threshold interaction that lifts
// good-cpu5's hist_l1 to 2 (govpx Q=13 vs libvpx Q=12 on one frame).
// Closing the residual would require either rejecting NEWMV candidates
// whose subpel predictor lands in the UMV extension region at the
// top-right corner, or lining up the rate-correction-factor trajectory
// after a single corner-MB ref-frame divergence.
//
// R9-1: TestOracleInterModeDistributionScoreboard's
// rt-cpu8-1280x720-bench-noise fixture pins the high-resolution mode
// dispersal at L1=1.67pp / EOB ratio=1.013. The dominant residual is a
// ~0.83pp ZEROMV<->NEARESTMV swap; the NEAR/NEW gap called out in r7-b
// is closed (NEAR 0.01% govpx vs 0.00% libvpx, NEW 0.30% vs 0.47%).
// cmd/govpx-bench's interframe overshoot is dominated by residual-token
// / entropy-savings path downstream of the picker.
//
// The threshold gates must use the frame base quantizer, not the active
// cyclic-refresh segment quantizer. libvpx derives rd_baseline_thresh from
// cm->base_qindex in vp8_initialize_rd_consts; only residual scoring uses the
// segment quantizer. Using the segment Q here admits modes libvpx would skip.
func (e *VP8Encoder) selectFastInterFrameModeDecision(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int,
	qIndex int, segmentID uint8,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	quant *vp8enc.MacroblockQuant,
	sourceAltRefZeroMVOnly bool,
) (interFrameModeDecision, bool) {
	if !e.interRDFrameActive {
		e.beginInterRDModeDecisionMacroblock()
	}
	traceEnabled := e.oracleTraceEnabled()
	thresholds := e.interModeRDThresholdsForReferences(qIndex, refs, refCount)
	bestSet := false
	bestScore := maxInt()
	bestDistortion := maxInt()
	bestSSE := maxInt()
	bestModeIndex := -1
	best := interFrameModeDecision{}
	var loopCtx fastInterModeLoopContext
	if !e.interRDFrameRefSearchOrderValid {
		e.interRDFrameRefSearchOrder = libvpxInterReferenceSearchOrder(refs, refCount)
		e.interRDFrameRefSearchOrderValid = true
	}
	refSearchOrder := e.interRDFrameRefSearchOrder
	loopCtx.modeMVs = e.interModeMVSlots(refs, refSearchOrder, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
	loopCtx.signBias = e.interFrameSignBias()
	loopCtx.mvCosts = e.currentMotionVectorCostTables()
	activeSignBiasSlot := 0
	bestRefMV := vp8enc.MotionVector{}
	if baseRefIndex := refSearchOrder[1]; baseRefIndex >= 0 && baseRefIndex < len(refs) {
		activeSignBiasSlot = interModeSignBiasSlotForReference(refs[baseRefIndex].Frame, loopCtx.signBias)
		bestRefMV = loopCtx.modeMVs.best[activeSignBiasSlot]
		loopCtx.bestRefMV = bestRefMV
	}
	// Hoist the rd_threshes throttle gate out of the per-mode loop. Once
	// inside the picker e is non-nil, modeIndex is bounded by the loop
	// range, and interRDFrameActive is invariant across iterations — so the
	// fast-path predicate can collapse from the public helper's three guard
	// branches to one indexed read.
	rdActive := e.interRDFrameActive
	// Hoist the package-level mode-order tables to function-local copies.
	// The package globals force a fresh `MOVD $...(SB)` (ADRP+ADD on arm64)
	// on every iteration of the per-mode loop because the compiler cannot
	// prove the SB-relative address is loop-invariant; copying to a local
	// array lets the loop reuse a single base pointer and frees up an
	// extra register for the other indexed reads.
	modeOrder := libvpxFastInterModeOrder
	refOrder := libvpxFastRefFrameOrder
	inactiveMB := e.interMacroblockInactive(mbRow, mbCol, mbCols)

	for modeIndex, mbMode := range modeOrder {
		threshold := thresholds[modeIndex]
		if threshold == libvpxInterModeThresholdDisabled {
			continue
		}
		if bestSet && bestScore <= threshold {
			continue
		}

		refSlot := refOrder[modeIndex]
		if refSlot == 0 {
			if rdActive && !e.interRDModeTestAllowedFast(modeIndex) {
				continue
			}
			if rdActive {
				e.interModeTestHitCounts[modeIndex]++
			}
			if !libvpxSourceAltRefCandidate(sourceAltRefZeroMVOnly, vp8common.IntraFrame, mbMode) {
				continue
			}
			bestScoreBefore := bestScore
			bestSSEBefore := bestSSE
			mode, score, distortion, sse, rate, ok := e.estimateFastIntraModeScore(src, mbRow, mbCol, qIndex, mbMode, bestSSE, quant)
			if !ok {
				continue
			}
			mode.SegmentID = segmentID
			becameBest := !bestSet || score < bestScore
			if traceEnabled {
				e.emitFastPickerIntraCandidateTrace(mbRow, mbCol, modeIndex, threshold, bestScoreBefore, bestSSEBefore, becameBest, score, rate, distortion, sse, &mode)
			}
			if becameBest {
				e.lowerInterRDThresholdForImprovement(modeIndex)
				bestSet = true
				bestScore = score
				bestDistortion = distortion
				bestSSE = sse
				bestModeIndex = modeIndex
				best = interFrameModeDecision{useIntra: true, intraMode: mode, projectedRate: rate, predictionError: distortion}
			} else {
				e.raiseInterRDThreshold(modeIndex)
			}
			continue
		}

		// Inlined interReferenceBySearchSlot fast path (refSlot is in
		// 1..3 by construction here): the helper does the same lookup but
		// the loop touches it on every iteration so inlining avoids the
		// extra bounds checks against searchOrder/refs.
		refIndex := refSearchOrder[refSlot]
		if refIndex < 0 || refIndex >= len(refs) {
			continue
		}
		ref := refs[refIndex]
		if ref.Img == nil {
			continue
		}
		refBiasSlot := interModeSignBiasSlotForReference(ref.Frame, loopCtx.signBias)
		if activeSignBiasSlot != refBiasSlot {
			activeSignBiasSlot = refBiasSlot
			bestRefMV = loopCtx.modeMVs.best[activeSignBiasSlot]
			loopCtx.bestRefMV = bestRefMV
		}
		if rdActive && !e.interRDModeTestAllowedFast(modeIndex) {
			continue
		}
		if rdActive {
			e.interModeTestHitCounts[modeIndex]++
		}
		if !libvpxSourceAltRefCandidate(sourceAltRefZeroMVOnly, ref.Frame, mbMode) {
			continue
		}
		// libvpx pickinter.c does not implement SPLITMV in the non-RD picker
		// (vp8_pick_inter_mode falls back to RAISE-only). Short-circuit
		// here and mirror the RAISE-only outcome on the three SPLITMV slots
		// (modeIndex 16/17/18).
		if mbMode == vp8common.SplitMV {
			e.raiseInterRDThreshold(modeIndex)
			continue
		}
		bestScoreBefore := bestScore
		bestSSEBefore := bestSSE
		mode := vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: mbMode}
		switch mbMode {
		case vp8common.ZeroMV:
		case vp8common.NearestMV, vp8common.NearMV:
			mv := loopCtx.modeMVs.nearest[activeSignBiasSlot]
			if mbMode == vp8common.NearMV {
				mv = loopCtx.modeMVs.near[activeSignBiasSlot]
			}
			if mv.IsZero() {
				continue
			}
			mode.MV = mv
		case vp8common.NewMV:
			search := loopCtx.searchConfig(e)
			start := e.improvedInterFrameSearchStart(src, ref.Frame, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, search)
			mvCosts := loopCtx.mvCosts
			if mvCosts == nil {
				mvCosts = e.currentMotionVectorCostTables()
			}
			var motionStats interFrameMotionSearchStats
			var stats *interFrameMotionSearchStats
			if e.opts.PhaseStats != nil && !e.threadedRowsActive {
				motionStats.phase = e.opts.PhaseStats
				stats = &motionStats
			}
			result := interFrameMotionVectorSearch{
				src:       src,
				ref:       ref.Img,
				mbRow:     mbRow,
				mbCol:     mbCol,
				mbRows:    mbRows,
				mbCols:    mbCols,
				bestRefMV: bestRefMV,
				qIndex:    qIndex,
				search:    search,
				start:     start,
				mvProbs:   &e.modeProbs.MV,
				mvCosts:   mvCosts,
				stats:     stats,
			}.selectFast()
			mv := clampInterMotionVectorToModeEdges(result.mv, mbRow, mbCol, mbRows, mbCols)
			if result.haveError && mv == result.mv {
				loopCtx.storeVariance(ref.Img, mv, result.variance, result.sse)
			}
			if mv.IsZero() {
				continue
			}
			mode.MV = mv
			attachImprovedMVTrace(&mode, start)
		default:
			continue
		}
		if !interFrameUMVFullPixelInRange(mode.MV, mbRow, mbCol, mbRows, mbCols) {
			continue
		}
		mode.SegmentID = segmentID
		if inactiveMB {
			mode.SegmentID = 0
			mode.MBSkipCoeff = true
			rate := e.interMotionModeRateWithReferenceRateAndModeContext(&mode, left, above, e.interReferenceFrameRateForReference(ref), loopCtx.modeMVs.counts, bestRefMV, libvpxFastNewMVBitCostWeight)
			if traceEnabled {
				e.emitFastPickerInterCandidateTrace(mbRow, mbCol, modeIndex, refSlot, ref.Frame, threshold, bestScore, bestSSE, true, true, maxInt(), rate, 0, 0, &mode)
			}
			e.lowerInterRDThresholdForImprovement(modeIndex)
			bestSet = true
			bestScore = maxInt()
			bestDistortion = 0
			bestSSE = 0
			bestModeIndex = modeIndex
			best = interFrameModeDecision{
				ref:             ref,
				interMode:       mode,
				intraMode:       vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred},
				projectedRate:   rate,
				predictionError: 0,
			}
			break
		}
		score, distortion, sse, rate, breakoutSkip, ok := e.estimateFastInterModeScoreWithReferenceRateAndSkipCached(src, ref.Img, mbRow, mbCol, mbRows, mbCols, &mode, above, left, aboveLeft, qIndex, e.interReferenceFrameRateForReference(ref), quant, &loopCtx)
		if !ok {
			continue
		}
		becameBest := breakoutSkip || !bestSet || score < bestScore
		if traceEnabled {
			e.emitFastPickerInterCandidateTrace(mbRow, mbCol, modeIndex, refSlot, ref.Frame, threshold, bestScoreBefore, bestSSEBefore, becameBest, breakoutSkip, score, rate, distortion, sse, &mode)
		}
		if becameBest {
			e.lowerInterRDThresholdForImprovement(modeIndex)
			bestSet = true
			bestScore = score
			bestDistortion = distortion
			bestSSE = sse
			bestModeIndex = modeIndex
			mode.MBSkipCoeff = breakoutSkip
			best = interFrameModeDecision{ref: ref, interMode: mode, projectedRate: rate, predictionError: distortion}
		} else {
			e.raiseInterRDThreshold(modeIndex)
		}
		if breakoutSkip {
			break
		}
	}
	if !bestSet {
		return interFrameModeDecision{}, false
	}
	if bestModeIndex >= 0 {
		e.lowerBestInterFastThreshold(bestModeIndex)
	}
	e.recordFastInterModeErrorBin(bestDistortion)
	if !best.useIntra {
		best.intraMode = vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred, SegmentID: segmentID}
	} else if best.intraMode.Mode <= vp8common.BPred {
		// R14-E: Mirror libvpx pickinter.c vp8_pick_inter_mode (lines
		// 1301-1304): once the winning MB mode is intra (DC/V/H/TM/BPred),
		// dynamically pick the best chroma uv_mode via pick_intra_mbuv_mode
		// (pixel-domain SSE between source U/V and the four predictor
		// candidates). govpx previously hardcoded UVMode=DC_PRED in
		// estimateFastIntraModeScore / estimateFastBPredIntraModeScore,
		// causing chroma reconstruction divergence on B_PRED inter MBs at
		// 128x128 frame 1 (MB(2,7), MB(3,7), MB(5,7) col-7 right-edge MBs
		// where libvpx selected V_PRED/H_PRED/TM_PRED for UV).
		uvMode, _, ok := pickFastIntraChromaMode(src, mbRow, mbCol, &e.analysis.Img, &e.reconstructScratch)
		if ok {
			best.intraMode.UVMode = uvMode
		}
	}
	return best, true
}

// emitFastPickerIntraCandidateTrace and emitFastPickerInterCandidateTrace are
// the trace plumbing for the fast picker hot loop. Splitting them off keeps
// the picker's stack frame small (the oracleTraceInterCandidateSummary
// literal is otherwise materialised twice in selectFastInterFrameModeDecision
// and reserves stack space whether or not OracleTraceWriter is set). The calls
// stay behind oracleTraceEnabled so normal encodes do not build trace rows.
func (e *VP8Encoder) emitFastPickerIntraCandidateTrace(mbRow int, mbCol int, modeIndex int, threshold int, bestScoreBefore int, bestSSEBefore int, becameBest bool, score int, rate int, distortion int, sse int, mode *vp8enc.InterFrameMacroblockMode) {
	e.emitOracleInterCandidateTrace(oracleTraceInterCandidateSummary{
		Picker:          "fast",
		MBRow:           mbRow,
		MBCol:           mbCol,
		ModeIndex:       modeIndex,
		Mode:            mode.Mode,
		RefSlot:         0,
		RefFrame:        vp8common.IntraFrame,
		Threshold:       threshold,
		BestScoreBefore: bestScoreBefore,
		BestYRDBefore:   oracleTraceInterCandidateUnknown,
		BestSSEBefore:   bestSSEBefore,
		Outcome:         "tested",
		BecameBest:      becameBest,
		Score:           score,
		YRD:             oracleTraceInterCandidateUnknown,
		Rate:            rate,
		RateY:           oracleTraceInterCandidateUnknown,
		RateUV:          oracleTraceInterCandidateUnknown,
		Distortion:      distortion,
		DistortionUV:    oracleTraceInterCandidateUnknown,
		SSE:             sse,
		Skip:            mode.MBSkipCoeff,
		ModeTrace:       *mode,
		HasModeTrace:    true,
	})
}

func (e *VP8Encoder) emitFastPickerInterCandidateTrace(mbRow int, mbCol int, modeIndex int, refSlot int, refFrame vp8common.MVReferenceFrame, threshold int, bestScoreBefore int, bestSSEBefore int, becameBest bool, breakoutSkip bool, score int, rate int, distortion int, sse int, mode *vp8enc.InterFrameMacroblockMode) {
	e.emitOracleInterCandidateTrace(oracleTraceInterCandidateSummary{
		Picker:          "fast",
		MBRow:           mbRow,
		MBCol:           mbCol,
		ModeIndex:       modeIndex,
		Mode:            mode.Mode,
		RefSlot:         refSlot,
		RefFrame:        refFrame,
		Threshold:       threshold,
		BestScoreBefore: bestScoreBefore,
		BestYRDBefore:   oracleTraceInterCandidateUnknown,
		BestSSEBefore:   bestSSEBefore,
		Outcome:         "tested",
		BecameBest:      becameBest,
		LoopBreak:       breakoutSkip,
		Score:           score,
		YRD:             oracleTraceInterCandidateUnknown,
		Rate:            rate,
		RateY:           oracleTraceInterCandidateUnknown,
		RateUV:          oracleTraceInterCandidateUnknown,
		Distortion:      distortion,
		DistortionUV:    oracleTraceInterCandidateUnknown,
		SSE:             sse,
		Skip:            breakoutSkip,
		ModeTrace:       *mode,
		HasModeTrace:    true,
	})
}

func libvpxInterReferenceSearchOrder(refs []interAnalysisReference, refCount int) [4]int {
	order := [4]int{-1, -1, -1, -1}
	searchSlot := 1
	for refIndex := 0; refIndex < refCount && refIndex < len(refs) && searchSlot < len(order); refIndex++ {
		if refs[refIndex].Img == nil {
			continue
		}
		switch refs[refIndex].Frame {
		case vp8common.LastFrame, vp8common.GoldenFrame, vp8common.AltRefFrame:
			order[searchSlot] = refIndex
			searchSlot++
		}
	}
	return order
}

func interReferenceBySearchSlot(refs []interAnalysisReference, searchOrder [4]int, refSlot int) (interAnalysisReference, int, bool) {
	if refSlot <= 0 || refSlot >= len(searchOrder) {
		return interAnalysisReference{}, 0, false
	}
	refIndex := searchOrder[refSlot]
	if refIndex < 0 || refIndex >= len(refs) || refs[refIndex].Img == nil {
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
	slot := 0
	if refFrame >= 0 && int(refFrame) < len(signBias) {
		slot = interModeSignBiasSlot(signBias[refFrame])
	}
	return slot
}

func (e *VP8Encoder) interModeMVSlots(
	refs []interAnalysisReference, refSearchOrder [4]int,
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
	nearest, near := interAnalysisReferenceMotionPredictorsWithSignBias(baseRef.Frame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, signBias)
	best := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, baseRef.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
	state.counts = vp8enc.InterFrameModeCounts(above, left, aboveLeft, baseRef.Frame, signBias)
	state.nearest[slot] = nearest
	state.near[slot] = near
	state.best[slot] = best
	opp := 1 - slot
	state.nearest[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -nearest.Row, Col: -nearest.Col}, mbRow, mbCol, mbRows, mbCols)
	state.near[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -near.Row, Col: -near.Col}, mbRow, mbCol, mbRows, mbCols)
	state.best[opp] = clampInterFrameModeMotionVector(vp8enc.MotionVector{Row: -best.Row, Col: -best.Col}, mbRow, mbCol, mbRows, mbCols)
	return state
}

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
	analysisImg := &e.analysis.Img
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(analysisImg, mbRow, mbCol, &mode, &e.reconstructScratch) {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
	}
	variance, sse := macroblockLumaVarianceSSE(src, analysisImg, mbRow, mbCol)
	rate := boolBitCost(e.refProbIntra, 0) + e.interIntraYModeRate(mbMode)
	resultMode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	return resultMode, rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, variance), variance, sse, rate, true
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
		fillBPredResidual4x4(src, mbRow, mbCol, block, bestPred[:], 4, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		eob := quantizeDecisionBlock(fastQuant, &dct, quantY1, qIndex, zbinOverQuant, 0, &qcoeff, &dqcoeff)
		var recon [16]byte
		if eob > 1 {
			dsp.IDCT4x4Add(&dqcoeff, bestPred[:], 4, recon[:], 4)
		} else {
			dsp.DCOnlyIDCT4x4Add(dqcoeff[0], bestPred[:], 4, recon[:], 4)
		}
		copyBPredBlock(recon[:], 4, y, yStride, block)

		rate += bestRate
		distortion += bestDist
		if distortion > bestSSE {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
		}
	}
	variance, sse := macroblockLumaVarianceSSE(src, analysisImg, mbRow, mbCol)
	return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: vp8common.DCPred, BModes: modes}, libvpxRDCost(rdMult, rdDiv, rate, variance), variance, sse, rate, true
}
