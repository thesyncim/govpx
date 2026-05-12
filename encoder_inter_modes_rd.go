package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

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
	e.interRDCoeffCacheScratchTarget = &e.interRDCoeffCacheSlots[(1-e.interRDCoeffCacheWinner)&1]
	defer func() {
		e.interRDCoeffCacheScratchTarget = nil
	}()
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
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
		e.interRDFrameRefSearchOrder = interReferenceSearchOrder(refs, refCount)
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
			if !sourceAltRefCandidateAllowed(sourceAltRefZeroMVOnly, vp8common.IntraFrame, mbMode) {
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
			if oracleTraceBuild && oracleStaleY2SnapshotSet(candidateStaleY2) {
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
				e.interRDCoeffCacheScratchTarget = &e.interRDCoeffCacheSlots[(1-e.interRDCoeffCacheWinner)&1]
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
		// refBiasSlot is 0 or 1 by construction; AND-mask with 1 elides
		// the bounds check on the [2]MotionVector slot array.
		bestRefMV := modeMVs.best[refBiasSlot&1]
		if !e.interRDModeTestAllowed(modeIndex) {
			continue
		}
		e.recordInterRDModeTest(modeIndex)
		if !sourceAltRefCandidateAllowed(sourceAltRefZeroMVOnly, ref.Frame, mbMode) {
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
			splitCtx := interSplitModeRDContext{
				src:        src,
				ref:        ref,
				mbRow:      mbRow,
				mbCol:      mbCol,
				mbCols:     mbCols,
				bestRefMV:  bestRefMV,
				modeCounts: modeMVs.counts,
				qIndex:     qIndex,
				segmentID:  segmentID,
				mvthresh:   mvthresh,
				bestYRD:    bestYRD,
				above:      above,
				left:       left,
				aboveLeft:  aboveLeft,
				aboveTok:   aboveTok,
				leftTok:    leftTok,
				quant:      quant,
			}
			split, splitOK := e.selectInterFrameSplitModeRDScore(&splitCtx)
			ok = splitOK
			mode = split.mode
			score = split.rd
			yrd = split.yrd
			rate = split.rate
			distortion = split.distortion
			rdLoopSkip = split.rdLoopSkip
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
					rdCtx := interResidualRDContext{
						src:        src,
						ref:        ref.Img,
						mbRow:      mbRow,
						mbCol:      mbCol,
						mode:       &mode,
						above:      above,
						left:       left,
						aboveLeft:  aboveLeft,
						aboveTok:   aboveTok,
						leftTok:    leftTok,
						quant:      quant,
						qIndex:     qIndex,
						segmentID:  segmentID,
						refRate:    e.interReferenceFrameRateForReference(ref),
						modeCounts: modeMVs.counts,
						bestRefMV:  bestRefMV,
					}
					acct, acctOK := e.estimateInterResidualRDAccountingWithModeContext(&rdCtx)
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
		if oracleTraceBuild && oracleStaleY2SnapshotSet(candidateStaleY2) {
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
			e.interRDCoeffCacheScratchTarget = &e.interRDCoeffCacheSlots[(1-e.interRDCoeffCacheWinner)&1]
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
