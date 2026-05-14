package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

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
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	thresholds := e.interModeRDThresholdsForReferences(qIndex, refs, refCount)
	bestSet := false
	bestScore := maxInt()
	bestDistortion := maxInt()
	bestSSE := maxInt()
	bestModeIndex := -1
	best := interFrameModeDecision{}
	denoiseActive := e.opts.NoiseSensitivity > 0
	denoiseDecision := newDenoiserMacroblockDecision()
	if denoiseActive {
		denoiseDecision.useSkinGate = true
	}
	var loopCtx fastInterModeLoopContext
	if !e.interRDFrameRefSearchOrderValid {
		e.interRDFrameRefSearchOrder = interReferenceSearchOrder(refs, refCount)
		e.interRDFrameRefSearchOrderValid = true
	}
	refSearchOrder := e.interRDFrameRefSearchOrder
	loopCtx.modeMVs = e.interModeMVSlots(refs, refSearchOrder, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
	loopCtx.signBias = e.interFrameSignBias()
	loopCtx.mvCosts = e.currentMotionVectorCostTables()
	activeSignBiasSlot := 0
	bestRefMV := vp8enc.MotionVector{}
	if baseRefIndex := refSearchOrder[1]; uint(baseRefIndex) < uint(len(refs)) {
		activeSignBiasSlot = interModeSignBiasSlotForReference(refs[baseRefIndex].Frame, loopCtx.signBias)
		bestRefMV = loopCtx.modeMVs.best[activeSignBiasSlot&1]
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
			if !sourceAltRefCandidateAllowed(sourceAltRefZeroMVOnly, vp8common.IntraFrame, mbMode) {
				continue
			}
			bestScoreBefore := bestScore
			bestSSEBefore := bestSSE
			mode, score, distortion, sse, rate, ok := e.estimateFastIntraModeScore(src, mbRow, mbCol, qIndex, mbMode, bestSSE, quant)
			if !ok {
				e.raiseInterRDThreshold(modeIndex)
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
		// refSearchOrder is [4]int and refSlot ∈ [0,3]; AND-mask with 3
		// elides the bounds check.
		refIndex := refSearchOrder[refSlot&3]
		// Single uint range check folds the (refIndex < 0) and
		// (refIndex >= len) guards.
		if uint(refIndex) >= uint(len(refs)) {
			continue
		}
		ref := refs[refIndex]
		if ref.Img == nil {
			continue
		}
		refBiasSlot := interModeSignBiasSlotForReference(ref.Frame, loopCtx.signBias)
		if activeSignBiasSlot != refBiasSlot {
			activeSignBiasSlot = refBiasSlot
			bestRefMV = loopCtx.modeMVs.best[activeSignBiasSlot&1]
			loopCtx.bestRefMV = bestRefMV
		}
		if rdActive && !e.interRDModeTestAllowedFast(modeIndex) {
			continue
		}
		if rdActive {
			e.interModeTestHitCounts[modeIndex]++
		}
		if !sourceAltRefCandidateAllowed(sourceAltRefZeroMVOnly, ref.Frame, mbMode) {
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
		improvedStart := interFrameSearchStart{}
		switch mbMode {
		case vp8common.ZeroMV:
		case vp8common.NearestMV, vp8common.NearMV:
			mv := loopCtx.modeMVs.nearest[activeSignBiasSlot&1]
			if mbMode == vp8common.NearMV {
				mv = loopCtx.modeMVs.near[activeSignBiasSlot&1]
			}
			if mv.IsZero() {
				continue
			}
			mode.MV = mv
		case vp8common.NewMV:
			search := loopCtx.searchConfig(e)
			start := e.improvedInterFrameSearchStart(src, ref.Frame, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, search)
			improvedStart = start
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
				mvCosts:     mvCosts,
				stats:       stats,
			}.selectFast()
			mv := clampInterMotionVectorToModeEdges(result.mv, mbRow, mbCol, mbRows, mbCols)
			if result.haveError && mv == result.mv {
				loopCtx.storeVariance(ref.Img, mv, result.variance, result.sse)
			}
			if mv.IsZero() {
				continue
			}
			mode.MV = mv
		default:
			continue
		}
		if !interFrameUMVFullPixelInRange(mode.MV, mbRow, mbCol, mbRows, mbCols) {
			continue
		}
		mode.SegmentID = segmentID
		if inactiveMB {
			if !e.roi.enabled {
				mode.SegmentID = 0
			}
			mode.MBSkipCoeff = true
			rate := e.interMotionModeRateWithReferenceRateAndModeContext(&mode, left, above, e.interReferenceFrameRateForReference(ref), loopCtx.modeMVs.counts, bestRefMV, libvpxFastNewMVBitCostWeight)
			if traceEnabled {
				e.emitFastPickerInterCandidateTrace(mbRow, mbCol, modeIndex, refSlot, ref.Frame, threshold, bestScore, bestSSE, true, true, maxInt(), rate, 0, 0, &mode, improvedStart)
			}
			e.lowerInterRDThresholdForImprovement(modeIndex)
			bestSet = true
			bestDistortion = 0
			bestModeIndex = modeIndex
			best = interFrameModeDecision{
				ref:             ref,
				interMode:       mode,
				intraMode:       vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred},
				projectedRate:   rate,
				improvedMVStart: improvedStart,
				predictionError: 0,
			}
			break
		}
		score, distortion, sse, rate, breakoutSkip, ok := e.estimateFastInterModeScoreWithReferenceRateAndSkipCached(src, ref.Img, mbRow, mbCol, mbRows, mbCols, &mode, above, left, aboveLeft, qIndex, e.interReferenceFrameRateForReference(ref), quant, &loopCtx)
		if !ok {
			continue
		}
		if denoiseActive && !e.denoiserReferenceTooOld(ref.Frame) {
			candidateSSE := uint32(sse)
			if mbMode == vp8common.ZeroMV && candidateSSE < denoiseDecision.zeroMVSSE {
				denoiseDecision.zeroMVSSE = candidateSSE
				denoiseDecision.zeroMVReferenceFrame = ref.Frame
			}
			if mbMode == vp8common.NewMV && candidateSSE < denoiseDecision.bestSSE {
				denoiseDecision.bestSSE = candidateSSE
				denoiseDecision.bestMode = vp8common.NewMV
				denoiseDecision.bestMV = mode.MV
				denoiseDecision.bestReferenceFrame = ref.Frame
			}
		}
		becameBest := breakoutSkip || !bestSet || score < bestScore
		if traceEnabled {
			e.emitFastPickerInterCandidateTrace(mbRow, mbCol, modeIndex, refSlot, ref.Frame, threshold, bestScoreBefore, bestSSEBefore, becameBest, breakoutSkip, score, rate, distortion, sse, &mode, improvedStart)
		}
		if becameBest {
			e.lowerInterRDThresholdForImprovement(modeIndex)
			bestSet = true
			bestScore = score
			bestDistortion = distortion
			bestSSE = sse
			bestModeIndex = modeIndex
			mode.MBSkipCoeff = breakoutSkip
			best = interFrameModeDecision{ref: ref, interMode: mode, projectedRate: rate, improvedMVStart: improvedStart, predictionError: distortion}
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
	if denoiseActive {
		if denoiseDecision.bestReferenceFrame == vp8common.IntraFrame {
			if best.useIntra {
				denoiseDecision.bestReferenceFrame = vp8common.IntraFrame
				denoiseDecision.bestMode = best.intraMode.Mode
				denoiseDecision.bestMV = best.intraMode.MV
			} else {
				denoiseDecision.bestReferenceFrame = best.interMode.RefFrame
				denoiseDecision.bestMode = best.interMode.Mode
				denoiseDecision.bestMV = best.interMode.MV
			}
			if bestSSE >= 0 {
				denoiseDecision.bestSSE = uint32(bestSSE)
			}
		}
		best.denoise = denoiseDecision
	}
	return best, true
}
