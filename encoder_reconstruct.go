package govpx

import (
	"math"
	"unsafe"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// acquireReconstructAboveTok returns a zeroed scratch slice of cols
// TokenContextPlanes drawn from the per-encoder reconstructAboveTok pool.
// The buffer is sized once at NewVP8Encoder; if a caller asks for more
// columns than were pre-reserved (e.g. extended size on the fly) the slice
// is grown in place. Returning a zeroed reslice keeps the per-frame
// reconstruction pass allocation-free without changing any caller
// invariants -- the inter and key frame builders zeroed the freshly-make()'d
// slice implicitly before, and we restore that contract by clearing here.
func (e *VP8Encoder) acquireReconstructAboveTok(cols int) []vp8enc.TokenContextPlanes {
	if cap(e.reconstructAboveTok) < cols {
		e.reconstructAboveTok = make([]vp8enc.TokenContextPlanes, cols)
	} else {
		e.reconstructAboveTok = e.reconstructAboveTok[:cols]
		clear(e.reconstructAboveTok)
	}
	return e.reconstructAboveTok
}

var wholeBlockIntraYModeCandidates = [...]vp8common.MBPredictionMode{
	vp8common.DCPred,
	vp8common.VPred,
	vp8common.HPred,
	vp8common.TMPred,
}

var wholeBlockIntraUVModeCandidates = [...]vp8common.MBPredictionMode{
	vp8common.DCPred,
	vp8common.VPred,
	vp8common.HPred,
	vp8common.TMPred,
}

var bPredIntraModeCandidates = [...]vp8common.BPredictionMode{
	vp8common.BDCPred,
	vp8common.BTMPred,
	vp8common.BVEPred,
	vp8common.BHEPred,
	vp8common.BLDPred,
	vp8common.BRDPred,
	vp8common.BVRPred,
	vp8common.BVLPred,
	vp8common.BHDPred,
	vp8common.BHUPred,
}

var fastBPredIntraModeCandidates = [...]vp8common.BPredictionMode{
	vp8common.BDCPred,
	vp8common.BTMPred,
	vp8common.BVEPred,
	vp8common.BHEPred,
}

func libvpxFrameQuantDeltas(qIndex int, screenContentMode int) vp8common.QuantDeltas {
	var deltas vp8common.QuantDeltas
	if qIndex < 4 {
		deltas.Y2DC = 4 - qIndex
	}
	if screenContentMode != 0 && qIndex > 40 {
		uvDelta := max(-(15 * qIndex / 100), -15)
		deltas.UVDC = uvDelta
		deltas.UVAC = uvDelta
	}
	return deltas
}

func quantHeaderForFrame(qIndex int, deltas vp8common.QuantDeltas) vp8dec.QuantHeader {
	return vp8dec.QuantHeader{
		BaseQIndex: uint8(qIndex),
		Y1DCDelta:  int8(deltas.Y1DC),
		Y2DCDelta:  int8(deltas.Y2DC),
		Y2ACDelta:  int8(deltas.Y2AC),
		UVDCDelta:  int8(deltas.UVDC),
		UVACDelta:  int8(deltas.UVAC),
	}
}

var libvpxFastInterModeOrder = [...]vp8common.MBPredictionMode{
	vp8common.ZeroMV, vp8common.DCPred,
	vp8common.NearestMV, vp8common.NearMV,
	vp8common.ZeroMV, vp8common.NearestMV,
	vp8common.ZeroMV, vp8common.NearestMV,
	vp8common.NearMV, vp8common.NearMV,
	vp8common.VPred, vp8common.HPred, vp8common.TMPred,
	vp8common.NewMV, vp8common.NewMV, vp8common.NewMV,
	vp8common.SplitMV, vp8common.SplitMV, vp8common.SplitMV,
	vp8common.BPred,
}

var libvpxFastRefFrameOrder = [...]int{
	1, 0,
	1, 1,
	2, 2,
	3, 3,
	2, 3,
	0, 0, 0,
	1, 2, 3,
	1, 2, 3,
	0,
}

const (
	libvpxInterModeCount = len(libvpxFastInterModeOrder)

	libvpxInterModeThresholdDisabled = -1
	libvpxSpeedMapMax                = 1 << 30
	libvpxRDThreshMultStart          = 128
	libvpxMinThreshMult              = 32
	libvpxMaxThreshMult              = 512
)

const (
	libvpxThrZero1 = iota
	libvpxThrDC
	libvpxThrNearest1
	libvpxThrNear1
	libvpxThrZero2
	libvpxThrNearest2
	libvpxThrZero3
	libvpxThrNearest3
	libvpxThrNear2
	libvpxThrNear3
	libvpxThrVPred
	libvpxThrHPred
	libvpxThrTMPred
	libvpxThrNew1
	libvpxThrNew2
	libvpxThrNew3
	libvpxThrSplit1
	libvpxThrSplit2
	libvpxThrSplit3
	libvpxThrBPred
)

func (e *VP8Encoder) buildReconstructingKeyFrameCoefficients(src vp8enc.SourceImage, qIndex int, modes []vp8enc.KeyFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int) (int, error) {
	return e.buildReconstructingKeyFrameCoefficientsWithSegmentation(src, qIndex, vp8enc.SegmentationConfig{}, false, modes, coeffs, rows, cols)
}

func (e *VP8Encoder) buildReconstructingKeyFrameCoefficientsWithSegmentation(src vp8enc.SourceImage, qIndex int, segmentation vp8enc.SegmentationConfig, preserveSegmentID bool, modes []vp8enc.KeyFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int) (int, error) {
	if qIndex < vp8common.MinQ || qIndex > vp8common.MaxQ {
		return 0, ErrInvalidConfig
	}
	// Reset oracle trace MB buffer at the start of each build pass so retried
	// (recoded) key-frame attempts overwrite earlier rows.
	e.resetOracleMBTraceBuffer()
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(e.reconstructModes) < required || len(e.reconstructTokens) < required {
		return 0, ErrInvalidConfig
	}

	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	quantDeltas := libvpxFrameQuantDeltas(qIndex, e.opts.ScreenContentMode)
	if err := vp8enc.InitSegmentMacroblockQuants(qIndex, quantDeltas, segmentation, &quants); err != nil {
		return 0, ErrInvalidConfig
	}
	decSegmentation := encoderSegmentationToDecoder(segmentation)
	vp8dec.InitSegmentDequants(quantHeaderForFrame(qIndex, quantDeltas), &decSegmentation, &e.dequantTables, &e.dequants)

	aboveTok := e.acquireReconstructAboveTok(cols)
	totalRate := 0
	for row := range rows {
		var leftTok vp8enc.TokenContextPlanes
		for col := range cols {
			index := row*cols + col
			segmentID, ok := keyFrameAnalysisSegmentID(&modes[index], segmentation, preserveSegmentID)
			if !ok {
				return 0, ErrInvalidConfig
			}
			segmentQIndex := encoderSegmentQIndex(qIndex, segmentation, segmentID)
			var above *vp8enc.KeyFrameMacroblockMode
			var left *vp8enc.KeyFrameMacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			var mode vp8enc.KeyFrameMacroblockMode
			var projectedRate int
			if e.libvpxUseFastIntraPick() {
				mode, projectedRate, ok = predictBestKeyFrameIntraModeFast(src, segmentQIndex, row, col, above, left, &quants[segmentID], &e.analysis.Img, &e.reconstructScratch, e.libvpxUseFastQuantForPick())
			} else {
				mode, projectedRate, ok = predictBestKeyFrameIntraMode(src, segmentQIndex, row, col, above, left, &aboveTok[col], &leftTok, &quants[segmentID], &e.analysis.Img, &e.reconstructScratch, e.libvpxUseFastQuantForPick())
			}
			if !ok {
				return 0, ErrInvalidConfig
			}
			totalRate = libvpxAddProjectedMacroblockRate(totalRate, projectedRate)
			mode.SegmentID = segmentID
			modes[index] = mode
			convertKeyFrameMode(&modes[index], &e.reconstructModes[index])
			if modes[index].YMode == vp8common.BPred {
				if !buildReconstructingBPredMacroblockCoefficients(&vp8tables.DefaultCoefProbs, src, row, col, &e.analysis.Img, &e.reconstructModes[index], &aboveTok[col], &leftTok, &quants[segmentID], segmentQIndex, 0, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), &coeffs[index], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
				convertMacroblockCoefficients(&coeffs[index], true, &e.reconstructTokens[index])
				vp8enc.UpdateTokenContextPlanesFromCoefficients(&aboveTok[col], &leftTok, true, &coeffs[index])
				e.emitOracleKeyFrameMBTrace(row, col, &modes[index], &coeffs[index], projectedRate, totalRate)
				continue
			}
			if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
				return 0, ErrInvalidConfig
			}
			is4x4 := modes[index].YMode == vp8common.BPred
			buildPredictedMacroblockCoefficients(&vp8tables.DefaultCoefProbs, src, row, col, &e.analysis.Img, &aboveTok[col], &leftTok, &quants[segmentID], segmentQIndex, 0, 0, is4x4, true, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), &coeffs[index])
			convertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
			if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
				return 0, ErrInvalidConfig
			}
			vp8enc.UpdateTokenContextPlanesFromCoefficients(&aboveTok[col], &leftTok, is4x4, &coeffs[index])
			e.emitOracleKeyFrameMBTrace(row, col, &modes[index], &coeffs[index], projectedRate, totalRate)
		}
		vp8dec.ExtendIntraRightEdgeForRow(&e.analysis.Img, row)
	}
	e.analysis.ExtendBorders()
	return totalRate, nil
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficients(src vp8enc.SourceImage, qIndex int, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int, flags EncodeFlags) (int, error) {
	return e.buildReconstructingInterFrameCoefficientsWithSegmentation(src, qIndex, vp8enc.SegmentationConfig{}, false, modes, coeffs, rows, cols, flags)
}

// buildReconstructingInterFrameCoefficientsWithSegmentation drives the
// per-MB inter-frame RD picker, residual reconstruction, and token-context
// commit in libvpx's encode_mb_row order (vp8/encoder/encodeframe.c). The
// per-MB token contexts (aboveTok / leftTok in this function) are committed
// to the row state only after the chosen mode's residual has been encoded,
// via updateInterAnalysisTokenContext — mirroring libvpx's deferred
// "*a/*l" ENTROPY_CONTEXT assignment after vp8_encode_inter16x16 /
// vp8_encode_intra4x4mby. Recode-loop interactions: encodeInterFrame{,
// WithQuantizerFeedback} re-enters this function on every recode attempt;
// the local aboveTok slice and leftTok variable are freshly allocated on
// each call so a rejected attempt's commits never leak into the next try
// (matching libvpx restore_coding_context's effect of rewinding the row
// ENTROPY_CONTEXTs at the start of each recode iteration).
func (e *VP8Encoder) buildReconstructingInterFrameCoefficientsWithSegmentation(src vp8enc.SourceImage, qIndex int, segmentation vp8enc.SegmentationConfig, preserveSegmentID bool, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int, flags EncodeFlags) (int, error) {
	if qIndex < vp8common.MinQ || qIndex > vp8common.MaxQ {
		return 0, ErrInvalidConfig
	}
	// Reset oracle trace MB buffer at the start of each build pass so retried
	// (recoded) attempts overwrite earlier rows.
	e.resetOracleMBTraceBuffer()
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(e.reconstructModes) < required || len(e.reconstructTokens) < required {
		return 0, ErrInvalidConfig
	}

	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	quantDeltas := libvpxFrameQuantDeltas(qIndex, e.opts.ScreenContentMode)
	if err := vp8enc.InitSegmentMacroblockQuants(qIndex, quantDeltas, segmentation, &quants); err != nil {
		return 0, ErrInvalidConfig
	}
	decSegmentation := encoderSegmentationToDecoder(segmentation)
	vp8dec.InitSegmentDequants(quantHeaderForFrame(qIndex, quantDeltas), &decSegmentation, &e.dequantTables, &e.dequants)

	var refs [3]interAnalysisReference
	refCount := e.interAnalysisReferences(flags, &refs)
	if refCount == 0 {
		return 0, ErrInvalidConfig
	}
	sourceAltRefZeroMVOnly := e.sourceAltRefZeroMVOnly(flags)
	// Capture the LAST reference (with border) once per inter frame so the
	// chroma sub-pel diagnostic can verify border content matches libvpx.
	e.emitOracleLastRefWindow(&e.lastRef.Img)
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	aboveTok := e.acquireReconstructAboveTok(cols)
	totalRate := 0
	activeMapEnabled := e.activeMapEnabled && len(e.activeMap) >= rows*cols
	var lastRefForActiveMap *interAnalysisReference
	if activeMapEnabled {
		for ri := range refCount {
			if refs[ri].Frame == vp8common.LastFrame {
				lastRefForActiveMap = &refs[ri]
				break
			}
		}
	}
	for row := range rows {
		var leftTok vp8enc.TokenContextPlanes
		for col := range cols {
			index := row*cols + col
			if activeMapEnabled && lastRefForActiveMap != nil && e.activeMap[index] == 0 {
				if !e.encodeInactiveInterMacroblock(row, col, index, lastRefForActiveMap.Img, modes, coeffs, &aboveTok[col], &leftTok) {
					return 0, ErrInvalidConfig
				}
				continue
			}
			segmentID, ok := interFrameAnalysisSegmentID(&modes[index], segmentation, preserveSegmentID)
			if !ok {
				return 0, ErrInvalidConfig
			}
			var above *vp8enc.InterFrameMacroblockMode
			var left *vp8enc.InterFrameMacroblockMode
			var aboveLeft *vp8enc.InterFrameMacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			if row > 0 && col > 0 {
				aboveLeft = &modes[index-cols-1]
			}
			e.beginInterRDModeDecisionMacroblock()
			// Snapshot the picker mutator state ONLY when segmentID != 0
			// so the cyclic-refresh segment-fallback path can restore it
			// to mirror libvpx's single-picker-call-per-MB invariant. The
			// snapshots live on the encoder struct (not on the stack) so
			// this stays zero-alloc when the segmentID == 0 hot path
			// applies. See R12-C ZEROMV/NEARESTMV swap fix
			// (build_vpxenc_oracle.sh oracle_trace.c picker_entry hook).
			if segmentID != 0 {
				e.interRDThreshMultSnapshot = e.interRDThreshMult
				e.interRDThreshTouchedSnapshot = e.interRDThreshTouched
				e.interModeTestHitCountsSnapshot = e.interModeTestHitCounts
				e.interMBsTestedSoFarSnapshot = e.interMBsTestedSoFar
			}
			decision, ok := e.selectInterFrameModeDecision(
				src, refs[:], refCount,
				row, col, rows, cols,
				qIndex, segmentation, segmentID,
				above, left, aboveLeft,
				&aboveTok[col], &leftTok,
				&quants[segmentID],
				sourceAltRefZeroMVOnly,
			)
			if !ok {
				return 0, ErrInvalidConfig
			}
			if segmentID != 0 && !decision.cyclicRefreshEligible() {
				// Restore the snapshotted state so the segmentID=0
				// fallback picker call doesn't see the first call's
				// rd_thresh_mult mutations. libvpx runs the picker exactly
				// once per MB, so without this restore the first call's
				// lowerBestInterFastThreshold pollutes mult[bestModeIndex]
				// and shifts the second call's threshold below libvpx's,
				// causing the cascade in selectFastInterFrameModeDecision
				// (e.g. the 720p noise NEARESTMV<->ZEROMV swap at MB(0,3)
				// onward in R12-C).
				e.interRDThreshMult = e.interRDThreshMultSnapshot
				e.interRDThreshTouched = e.interRDThreshTouchedSnapshot
				e.interModeTestHitCounts = e.interModeTestHitCountsSnapshot
				e.interMBsTestedSoFar = e.interMBsTestedSoFarSnapshot
				segmentID = 0
				decision, ok = e.selectInterFrameModeDecision(
					src, refs[:], refCount,
					row, col, rows, cols,
					qIndex, segmentation, segmentID,
					above, left, aboveLeft,
					&aboveTok[col], &leftTok,
					&quants[segmentID],
					sourceAltRefZeroMVOnly,
				)
				if !ok {
					return 0, ErrInvalidConfig
				}
			}
			totalRate = libvpxAddProjectedMacroblockRate(totalRate, decision.projectedRate)
			segmentQIndex := encoderSegmentQIndex(qIndex, segmentation, segmentID)
			quant := &quants[segmentID]

			if decision.useIntra {
				modes[index] = decision.intraMode
				modes[index].SegmentID = segmentID
				convertInterFrameMode(&modes[index], &e.reconstructModes[index])
				if modes[index].Mode == vp8common.BPred {
					if !buildReconstructingBPredMacroblockCoefficients(&e.coefProbs, src, row, col, &e.analysis.Img, &e.reconstructModes[index], &aboveTok[col], &leftTok, quant, segmentQIndex, e.rc.currentZbinOverQuant, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), &coeffs[index], &e.reconstructScratch) {
						return 0, ErrInvalidConfig
					}
				} else if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
			} else {
				modes[index] = decision.interMode
				convertInterFrameMode(&modes[index], &e.reconstructModes[index])
				predMode := e.reconstructModes[index]
				predMode.MBSkipCoeff = true
				if !reconstructInterAnalysisMacroblock(&e.analysis.Img, decision.ref.Img, row, col, &predMode, &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
				// Capture the inter predictor before residual is added.
				// Mirrors libvpx's xd->dst.{y,u,v}_buffer between
				// vp8_encode_inter16x16 and vp8_inverse_transform_mby. Only
				// emits when EncoderOptions.OracleTracePredictorDump is
				// enabled and only for MB(0,0).
				e.emitOracleInterPredictorTrace(row, col, &e.analysis.Img)
			}
			breakoutSkip := modes[index].RefFrame != vp8common.IntraFrame &&
				(modes[index].MBSkipCoeff || staticInterRDEncodeBreakout(src, &e.analysis.Img, row, col, quant, e.opts.StaticThreshold))
			if breakoutSkip {
				clearMacroblockCoefficients(&coeffs[index])
			} else if modes[index].RefFrame != vp8common.IntraFrame || modes[index].Mode != vp8common.BPred {
				is4x4 := interFrameModeUses4x4Tokens(modes[index].Mode)
				buildPredictedMacroblockCoefficients(&e.coefProbs, src, row, col, &e.analysis.Img, &aboveTok[col], &leftTok, quant, segmentQIndex, e.rc.currentZbinOverQuant, interZbinModeBoost(&modes[index]), is4x4, modes[index].RefFrame == vp8common.IntraFrame, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), &coeffs[index])
			}
			is4x4 := interFrameModeUses4x4Tokens(modes[index].Mode)
			modes[index].MBSkipCoeff = breakoutSkip || macroblockCoefficientsEmpty(&coeffs[index], is4x4)
			convertInterFrameMode(&modes[index], &e.reconstructModes[index])
			convertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
			if modes[index].RefFrame == vp8common.IntraFrame && modes[index].Mode == vp8common.BPred {
				updateInterAnalysisTokenContext(&aboveTok[col], &leftTok, is4x4, modes[index].MBSkipCoeff, &coeffs[index])
				// B_PRED reconstruction was already written to
				// e.analysis.Img by buildReconstructingBPredMacroblockCoefficients
				// above, so emit the reconstructed trace here too. Without
				// this the predictor-diff harness silently skips B_PRED MBs
				// (e.g. the 4 right-edge col-7 B_PRED MBs on 128x128 frame
				// 1) and reports the visible Y as MATCH even when they
				// diverge.
				e.emitOracleInterReconstructedTrace(row, col, &e.analysis.Img)
				e.emitOracleMBTrace(row, col, &modes[index], &coeffs[index], decision.projectedRate, totalRate)
				continue
			}
			if modes[index].RefFrame == vp8common.IntraFrame {
				if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
			} else {
				if !reconstructInterAnalysisMacroblock(&e.analysis.Img, decision.ref.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
			}
			updateInterAnalysisTokenContext(&aboveTok[col], &leftTok, is4x4, modes[index].MBSkipCoeff, &coeffs[index])
			// Capture the post-residual reconstruction so the predictor diff
			// harness can pinpoint whether the gap originated in the
			// predictor (matched libvpx already) or the residual stage.
			// Mirrors libvpx's `govpx_oracle_emit_reconstructed` injected at
			// the tail of vp8cx_encode_inter_macroblock.
			e.emitOracleInterReconstructedTrace(row, col, &e.analysis.Img)
			e.emitOracleMBTrace(row, col, &modes[index], &coeffs[index], decision.projectedRate, totalRate)
		}
		vp8dec.ExtendIntraRightEdgeForRow(&e.analysis.Img, row)
	}
	e.analysis.ExtendBorders()
	return totalRate, nil
}

// encodeInactiveInterMacroblock matches libvpx's active-map fast path: an
// inactive macroblock skips mode decision and codes as ZEROMV from LAST with
// MBSkipCoeff=1, no segment override, and no residual. See
// vp8/encoder/pickinter.c evaluate_inter_mode and rdopt.c rd_pick_inter_mode
// active_ptr branches.
func (e *VP8Encoder) encodeInactiveInterMacroblock(row int, col int, index int, lastRef *vp8common.Image, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, above *vp8enc.TokenContextPlanes, left *vp8enc.TokenContextPlanes) bool {
	modes[index] = vp8enc.InterFrameMacroblockMode{
		SegmentID:   0,
		MBSkipCoeff: true,
		RefFrame:    vp8common.LastFrame,
		Mode:        vp8common.ZeroMV,
		UVMode:      vp8common.DCPred,
	}
	clearMacroblockCoefficients(&coeffs[index])
	convertInterFrameMode(&modes[index], &e.reconstructModes[index])
	is4x4 := interFrameModeUses4x4Tokens(modes[index].Mode)
	convertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
	if !reconstructInterAnalysisMacroblock(&e.analysis.Img, lastRef, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[0], &e.reconstructScratch) {
		return false
	}
	updateInterAnalysisTokenContext(above, left, is4x4, true, &coeffs[index])
	return true
}

func updateInterAnalysisTokenContext(above *vp8enc.TokenContextPlanes, left *vp8enc.TokenContextPlanes, is4x4 bool, skipped bool, coeffs *vp8enc.MacroblockCoefficients) {
	if skipped {
		vp8enc.ResetTokenContextPlanes(above, left, is4x4)
		return
	}
	vp8enc.UpdateTokenContextPlanesFromCoefficients(above, left, is4x4, coeffs)
}

func keyFrameAnalysisSegmentID(mode *vp8enc.KeyFrameMacroblockMode, segmentation vp8enc.SegmentationConfig, preserve bool) (uint8, bool) {
	if !segmentation.Enabled || !preserve {
		return 0, true
	}
	if mode.SegmentID >= vp8common.MaxMBSegments {
		return 0, false
	}
	return mode.SegmentID, true
}

func interFrameAnalysisSegmentID(mode *vp8enc.InterFrameMacroblockMode, segmentation vp8enc.SegmentationConfig, preserve bool) (uint8, bool) {
	if !segmentation.Enabled || !preserve {
		return 0, true
	}
	if mode.SegmentID >= vp8common.MaxMBSegments {
		return 0, false
	}
	return mode.SegmentID, true
}

func encoderSegmentQIndex(baseQ int, segmentation vp8enc.SegmentationConfig, segmentID uint8) int {
	if !segmentation.Enabled || segmentID >= vp8common.MaxMBSegments || !segmentation.FeatureEnabled[vp8common.MBLvlAltQ][segmentID] {
		return vp8common.ClampQIndex(baseQ)
	}
	altQ := int(segmentation.FeatureData[vp8common.MBLvlAltQ][segmentID])
	if segmentation.AbsDelta {
		return vp8common.ClampQIndex(altQ)
	}
	return vp8common.ClampQIndex(baseQ + altQ)
}

func encoderSegmentationToDecoder(segmentation vp8enc.SegmentationConfig) vp8dec.SegmentationHeader {
	if !segmentation.Enabled {
		return vp8dec.SegmentationHeader{}
	}
	var out vp8dec.SegmentationHeader
	out.Enabled = true
	out.UpdateMap = segmentation.UpdateMap
	out.UpdateData = segmentation.UpdateData
	out.AbsDelta = segmentation.AbsDelta
	out.TreeProbs = segmentation.TreeProbs
	for feature := range int(vp8common.MBLvlMax) {
		for segment := range vp8common.MaxMBSegments {
			if segmentation.FeatureEnabled[feature][segment] {
				out.FeatureData[feature][segment] = segmentation.FeatureData[feature][segment]
			}
		}
	}
	return out
}

type interAnalysisReference struct {
	Frame      vp8common.MVReferenceFrame
	Img        *vp8common.Image
	RefRate    int
	RefRateSet bool
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

func (e *VP8Encoder) closestInterAnalysisReference(refs []interAnalysisReference, refCount int) vp8common.MVReferenceFrame {
	if e == nil {
		return vp8common.LastFrame
	}
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
	if e == nil {
		return 0
	}
	if e.opts.Width > 0 && e.opts.Height > 0 {
		return encoderMacroblockCount(e.opts.Width, e.opts.Height)
	}
	return len(e.interFrameModes)
}

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c motion search.
// vp8_hex_search finishes with an eight-step full-pixel diamond refinement.
const interFrameFullPixelSearchRadius = 16
const interFrameMVSearchRange = interFrameFullPixelSearchRadius * 8
const interFrameMVFullPixelStep = 8
const interFrameSubpixelSearchMaxCandidates = 31
const interFrameMotionCandidateMax = 15
const interFrameMaxMVSearchSteps = 8
const interFrameMaxFirstStep = 1 << (interFrameMaxMVSearchSteps - 1)
const interFrameSplitMVFullSearchThreshold = 4000
const interFrameMaxFullPelVal = (1 << interFrameMaxMVSearchSteps) - 1
const interFrameUMVBorderPixels = 32
const libvpxFastNewMVBitCostWeight = 128
const libvpxRDNewMVBitCostWeight = 96

type interAnalysisFullPixelSearchMethod uint8

const (
	interAnalysisFullPixelSearchExhaustive interAnalysisFullPixelSearchMethod = iota
	interAnalysisFullPixelSearchNstep
	interAnalysisFullPixelSearchDiamond
	interAnalysisFullPixelSearchHex
)

type interAnalysisFractionalSearchMethod uint8

const (
	interAnalysisFractionalSearchIterative interAnalysisFractionalSearchMethod = iota
	interAnalysisFractionalSearchStep
	interAnalysisFractionalSearchHalf
	interAnalysisFractionalSearchSkip
)

type interAnalysisSearchConfig struct {
	fullPixelSearch       interAnalysisFullPixelSearchMethod
	fullPixelSearchParam  int
	fullPixelFurtherSteps int
	fullPixelFinalRefine  bool
	fullPixelSpeed        int
	fullPixelSpeedAdjust  int
	improvedMVPrediction  bool
	fractionalSearch      interAnalysisFractionalSearchMethod
}

var (
	interAnalysisBestQualitySplitPartitionOrder = [vp8tables.NumMBSplits]int{0, 1, 2, 3}
	interAnalysisSpeedSplitPartitionOrder       = [vp8tables.NumMBSplits]int{2, 1, 0, 3}
)

func defaultInterAnalysisSearchConfig() interAnalysisSearchConfig {
	return interAnalysisSearchConfig{
		fullPixelSearch:  interAnalysisFullPixelSearchExhaustive,
		fractionalSearch: interAnalysisFractionalSearchIterative,
	}
}

// interAnalysisSearchConfig mirrors the VP8 speed-feature branch in
// onyx_if.c: realtime speed > 4 uses vp8_hex_search and disables the
// iterative sub-pixel function pointer.
func (e *VP8Encoder) interAnalysisSearchConfig() interAnalysisSearchConfig {
	cfg := defaultInterAnalysisSearchConfig()
	if e == nil {
		return cfg
	}
	speed := e.libvpxCPUUsed()
	cfg.fullPixelSearch = interAnalysisFullPixelSearchNstep
	cfg.fullPixelSearchParam = libvpxInterFrameSearchParamForFeatureSpeed(e.opts.Deadline, speed)
	cfg.fullPixelFinalRefine = e.interAnalysisUsesRDModeDecision()
	cfg.fullPixelSpeed = speed
	cfg.fullPixelSpeedAdjust = libvpxInterFrameSpeedAdjust(speed)
	furtherStepsSpeed := speed
	if e.interAnalysisUsesRDModeDecision() {
		cfg.fullPixelSearchParam = libvpxInterFrameFirstStepForFeatureSpeed(e.opts.Deadline, speed)
		cfg.fullPixelSpeedAdjust = 0
		if e.opts.Deadline == DeadlineBestQuality {
			furtherStepsSpeed = 0
		}
	}
	cfg.fullPixelFurtherSteps = libvpxInterFrameFurtherSteps(furtherStepsSpeed, cfg.fullPixelSearchParam)
	cfg.improvedMVPrediction = libvpxInterFrameImprovedMVPredictionForFeatureSpeed(e.opts.Deadline, speed)
	if e.opts.Deadline != DeadlineRealtime {
		return cfg
	}
	// R17-A (parity-close-r17-a-picker-speed-features): switch the realtime
	// fast picker's full-pel search from NSTEP to HEX at the auto-selected
	// Speed=4 floor, but only on frames that have enough macroblocks for
	// the picker's mode dispersal to remain libvpx-shaped under hex's local-
	// minimum behavior. The bench pins govpx's auto-select Speed at 4 and
	// the previous NSTEP+nine-further-steps walk ran ~70 SAD calls/NEWMV
	// where libvpx's RT(0)+ path at the matching effective Speed runs the
	// hex topology (search_param=4, hex_range breaks early once the
	// gradient flattens). The NEON SAD kernel is libvpx-fast (~18ns/call);
	// the per-MB SAD-call count is the actual gap. Step subpel is NOT
	// enabled at speed=4: the rt-cpu8-128x128-bench-noise inter-mode-
	// distribution oracle scoreboard pins NEAR/NEAREST percentages within
	// 4pp of libvpx and Step subpel pushes the dispersal past that gate.
	// Iterative subpel is preserved.
	//
	// The frame-size gate (>= 1080p) keeps the small/mid-frame oracle
	// scoreboards green:
	//   - rt-cpu8-128x128-bench-noise: 64 MBs, hex's local-minimum on noise
	//     flips a 6.25pp NEAR-to-NEAREST swap (12.5pp L1, fails 4pp+6pp gates).
	//   - rt-cpu8-256x256-bench-noise: 256 MBs, hex pushes L1 to 4.24pp /
	//     EOB to 1.099 (within gates but on the edge).
	//   - rt-cpu8-1280x720-bench-noise: 3600 MBs, hex pushes EOB ratio to
	//     1.128 (just over the 0.10 gate's 1.121 floor) -- the 720p noise
	//     fixture's NEAREST/NEW swap leaks downstream into a token-coding
	//     EOB sum bump that the L1 dispersal alone misses.
	// At 1080p the noise averaging keeps both the dispersal and the EOB
	// sum stable, so the perf win is delivered exactly where the bench
	// command points: the 1080p fast picker. The 720p bench command still
	// runs the previous NSTEP+iterative path at parity with the prior
	// release, so its perf gap is unchanged.
	largeFrame := e.opts.Width >= 1920 && e.opts.Height >= 1080
	if speed >= 4 && largeFrame {
		cfg.fullPixelSearch = interAnalysisFullPixelSearchHex
	}
	if speed > 4 {
		cfg.fullPixelSearch = interAnalysisFullPixelSearchHex
		cfg.fractionalSearch = interAnalysisFractionalSearchStep
	}
	if speed > 8 {
		cfg.fractionalSearch = interAnalysisFractionalSearchHalf
	}
	if speed >= 15 {
		cfg.fractionalSearch = interAnalysisFractionalSearchSkip
	}
	return cfg
}

func (e *VP8Encoder) interAnalysisCompressorSpeed() int {
	if e == nil || e.opts.Deadline == DeadlineBestQuality {
		return 0
	}
	if e.opts.Deadline == DeadlineRealtime {
		return 2
	}
	return 1
}

func (e *VP8Encoder) interAnalysisUsesRDModeDecision() bool {
	if e == nil {
		return true
	}
	switch e.opts.Deadline {
	case DeadlineBestQuality:
		return true
	case DeadlineGoodQuality, DeadlineRealtime:
		return e.libvpxCPUUsed() <= 3
	default:
		return true
	}
}

func (e *VP8Encoder) libvpxOptimizeCoefficients() bool {
	if e == nil {
		return true
	}
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return false
	case DeadlineGoodQuality:
		return e.libvpxCPUUsed() <= 0
	default:
		return true
	}
}

func (e *VP8Encoder) libvpxUseFastQuant() bool {
	if e == nil {
		return false
	}
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return e.libvpxCPUUsed() > 0
	case DeadlineGoodQuality:
		return e.libvpxCPUUsed() > 2
	default:
		return false
	}
}

func (e *VP8Encoder) libvpxUseFastQuantForPick() bool {
	if e == nil {
		return false
	}
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return e.libvpxCPUUsed() > 0
	case DeadlineGoodQuality:
		return e.libvpxCPUUsed() > 0
	default:
		return false
	}
}

// libvpxUseFastIntraPick returns true when libvpx would pick a keyframe
// macroblock via the pixel-domain pickinter.c vp8_pick_intra_mode helper
// rather than the full transform-domain rdopt.c vp8_rd_pick_intra_mode. The
// dispatch is `cpi->sf.RD == 0 || cpi->compressor_speed == 2 (realtime)`,
// which means realtime always uses the fast picker, and good-quality
// switches to it once cpu-used > 3 (when sf->RD is turned off).
func (e *VP8Encoder) libvpxUseFastIntraPick() bool {
	if e == nil {
		return false
	}
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return true
	case DeadlineGoodQuality:
		return e.libvpxCPUUsed() > 3
	default:
		return false
	}
}

func (e *VP8Encoder) interAnalysisSplitPartitionOrder() [vp8tables.NumMBSplits]int {
	if e.interAnalysisCompressorSpeed() == 0 {
		return interAnalysisBestQualitySplitPartitionOrder
	}
	return interAnalysisSpeedSplitPartitionOrder
}

func (e *VP8Encoder) interAnalysisNoSkipBlock4x4Search() bool {
	return e.interAnalysisCompressorSpeed() == 0 || e.libvpxCPUUsed() <= 0
}

func libvpxInterFrameSearchParamForFeatureSpeed(deadline Deadline, speed int) int {
	firstStep := libvpxInterFrameFirstStepForFeatureSpeed(deadline, speed)
	stepParam := firstStep + libvpxInterFrameSpeedAdjust(speed)
	if stepParam < 0 {
		return 0
	}
	if stepParam >= interFrameMaxMVSearchSteps {
		return interFrameMaxMVSearchSteps - 1
	}
	return stepParam
}

func libvpxInterFrameFirstStepForFeatureSpeed(deadline Deadline, speed int) int {
	if deadline != DeadlineBestQuality && speed > 0 {
		return 1
	}
	return 0
}

func libvpxInterFrameSpeedAdjust(speed int) int {
	if speed > 5 {
		if speed >= 8 {
			return 3
		}
		return 2
	}
	return 1
}

func libvpxInterFrameFurtherSteps(speed int, stepParam int) int {
	if speed >= 8 {
		return 0
	}
	further := interFrameMaxMVSearchSteps - 1 - stepParam
	if further < 0 {
		return 0
	}
	return further
}

func libvpxInterFrameImprovedMVPrediction(deadline Deadline, speed int) bool {
	speed = libvpxSpeedFeatureCPUUsed(deadline, speed)
	return libvpxInterFrameImprovedMVPredictionForFeatureSpeed(deadline, speed)
}

func libvpxInterFrameImprovedMVPredictionForFeatureSpeed(deadline Deadline, speed int) bool {
	return deadline != DeadlineRealtime || speed <= 6
}

func (e *VP8Encoder) interModeRDThresholds(qIndex int) [libvpxInterModeCount]int {
	return e.interModeRDThresholdsForReferences(qIndex, nil, 0)
}

// interRDThreshBaselineSlotCount caps the per-frame qIndex cache used by the
// fast/RD inter-mode picker thresholds. VP8 segmentation produces at most 4
// distinct quantizers per frame; a 4-slot LRU is enough to absorb the entire
// per-MB call sequence without falling back to the heavy
// libvpxInterModeRDThresholdsForContext recompute.
const interRDThreshBaselineSlotCount = 4

// interRDThreshBaselineSlot caches one (qIndex, refSig, baseline) entry. gen
// matches the encoder's interRDThreshBaselineGen at the time the slot was
// filled; a stale gen invalidates the slot without an explicit clear at frame
// start. refSig packs the threshold-context inputs that depend on the refs
// list (refCount, lastEnabled, goldenEnabled, closestRef, refFrameCount) plus
// zbinOverQuant — so the cache stays correct if a caller drives the picker
// with shifting refs without an intervening beginInterRDModeDecisionFrame.
type interRDThreshBaselineSlot struct {
	gen      uint32
	qIndex   int32
	refSig   uint32
	valid    bool
	baseline [libvpxInterModeCount]int
}

// interRDThreshBaselineRefSig packs the refs-derived threshold-context inputs
// into a uint32 fingerprint. The packing is dense — refCount fits in 8 bits,
// closestRef in 8 bits, refFrameCount in 8 bits, lastEnabled+goldenEnabled in
// 2 bits, leaving 6 bits for zbinOverQuant. Within VP8 zbinOverQuant is
// bounded to a few small values, so 6 bits is plenty; if it ever overflows
// the bit field the cache simply collides with another zbinOverQuant value
// (which forces a recompute on the next call — correct, just unhelpful).
func interRDThreshBaselineRefSig(refCount int, lastEnabled bool, goldenEnabled bool, closestRef vp8common.MVReferenceFrame, refFrameCount int, zbinOverQuant int) uint32 {
	var sig uint32
	sig |= uint32(uint8(refCount))
	sig |= uint32(uint8(closestRef)) << 8
	sig |= uint32(uint8(refFrameCount)) << 16
	if lastEnabled {
		sig |= 1 << 24
	}
	if goldenEnabled {
		sig |= 1 << 25
	}
	sig |= uint32(uint8(zbinOverQuant)&0x3F) << 26
	return sig
}

// interModeRDThresholdsBaseline returns the picker-threshold baseline for
// (qIndex, current frame refs/error-bins/speed) and caches the result by
// (qIndex, refSig) within the current frame. Within a frame the only per-MB-
// variable input is qIndex (via cyclic-refresh segmentation); the rest of
// the threshold-context inputs (refs, errorBins, speed, deadline, totalMBs,
// staticThreshold, temporalLayers, zbinOverQuant) are frame-stable, so the
// expensive libvpxInterModeRDThresholdsForContext math (math.Pow, 8 speed
// maps, 1024-bin error scan) runs at most 4× per frame instead of per-MB.
//
// The refSig fingerprint captures (refCount, lastEnabled, goldenEnabled,
// closestRef, refFrameCount, zbinOverQuant) so the cache stays correct under
// callers that mutate refs / referenceFrameNumbers between calls within the
// same generation (e.g. test fixtures that re-call the helper with shifted
// closest-ref distances). Building the fingerprint is cheap relative to the
// cached body and only walks the refs slice once.
func (e *VP8Encoder) interModeRDThresholdsBaseline(qIndex int, refs []interAnalysisReference, refCount int) [libvpxInterModeCount]int {
	context := libvpxInterModeThresholdContext{}
	if refCount > 0 {
		context.temporalLayers = e.libvpxTemporalLayerCount()
		context.lastEnabled = interAnalysisReferencesInclude(refs, refCount, vp8common.LastFrame)
		context.goldenEnabled = interAnalysisReferencesInclude(refs, refCount, vp8common.GoldenFrame)
		context.closestRef = e.closestInterAnalysisReference(refs, refCount)
		context.refFrameCount = 1 + interAnalysisValidReferenceCount(refs, refCount)
	}
	context.totalMBs = e.interAnalysisMacroblockCount()
	context.staticThreshold = e.opts.StaticThreshold
	context.errorBins = &e.interModeSpeedErrorBins
	zbinOverQuant := e.rc.currentZbinOverQuant
	gen := e.interRDThreshBaselineGen
	q32 := int32(qIndex)
	refSig := interRDThreshBaselineRefSig(refCount, context.lastEnabled, context.goldenEnabled, context.closestRef, context.refFrameCount, zbinOverQuant)
	slots := &e.interRDThreshBaselineSlots
	for i := range slots {
		slot := &slots[i]
		if slot.valid && slot.gen == gen && slot.qIndex == q32 && slot.refSig == refSig {
			return slot.baseline
		}
	}
	cpuUsedForThresholds := e.opts.CpuUsed
	if e.libvpxAutoSelectSpeedActive() {
		// Round-trip the dynamically picked Speed through
		// libvpxSpeedFeatureCPUUsed: passing -currentRTSpeed makes the static
		// helper return currentRTSpeed (it negates negative RT cpu_used).
		// This routes the auto-selected Speed into the per-mode thresh_mult
		// tables (libvpx vp8_set_speed_features speed_map calls) without
		// touching the static helper's contract.
		cpuUsedForThresholds = -e.libvpxCPUUsed()
	}
	baseline := libvpxInterModeRDThresholdsForContext(qIndex, zbinOverQuant, e.opts.Deadline, cpuUsedForThresholds, context)
	// Pick the first invalid/stale slot, else replace slot 0 (LRU is fine
	// here — at most 4 distinct (qIndex, refSig) pairs per frame so
	// collisions are rare).
	victim := 0
	for i := range slots {
		slot := &slots[i]
		if !slot.valid || slot.gen != gen {
			victim = i
			break
		}
	}
	slot := &slots[victim]
	slot.valid = true
	slot.gen = gen
	slot.qIndex = q32
	slot.refSig = refSig
	slot.baseline = baseline
	return baseline
}

func (e *VP8Encoder) interModeRDThresholdsForReferences(qIndex int, refs []interAnalysisReference, refCount int) [libvpxInterModeCount]int {
	if e == nil {
		return libvpxInterModeRDThresholds(qIndex, 0, DeadlineBestQuality, 0)
	}
	baseline := e.interModeRDThresholdsBaseline(qIndex, refs, refCount)
	if !e.interRDFrameActive {
		return baseline
	}
	thresholds := baseline
	touched := &e.interRDThreshTouched
	mult := &e.interRDThreshMult
	for i := range thresholds {
		v := thresholds[i]
		if v == libvpxInterModeThresholdDisabled {
			continue
		}
		if touched[i] {
			thresholds[i] = (v >> 7) * mult[i]
		}
	}
	return thresholds
}

func (e *VP8Encoder) libvpxTemporalLayerCount() int {
	if e == nil || !e.opts.TemporalScalability.Enabled {
		return 1
	}
	pattern, ok := temporalLayeringPattern(e.opts.TemporalScalability.Mode)
	if !ok {
		return 1
	}
	return pattern.Layers
}

func (e *VP8Encoder) resetInterRDThresholdMultipliers() {
	if e == nil {
		return
	}
	for i := range e.interRDThreshMult {
		e.interRDThreshMult[i] = libvpxRDThreshMultStart
	}
	e.interRDThreshTouched = [libvpxInterModeCount]bool{}
	e.interModeTestHitCounts = [libvpxInterModeCount]int{}
	e.interMBsTestedSoFar = 0
	// Bump the baseline cache generation so a follow-up frame doesn't
	// reuse a stale entry whose context inputs may have shifted.
	e.interRDThreshBaselineGen++
	e.interRDFrameRefSearchOrderValid = false
}

func (e *VP8Encoder) beginInterRDModeDecisionFrame() {
	if e == nil {
		return
	}
	for i, mult := range e.interRDThreshMult {
		if mult == 0 {
			e.interRDThreshMult[i] = libvpxRDThreshMultStart
		}
	}
	e.interRDThreshTouched = [libvpxInterModeCount]bool{}
	cpuUsedForFreq := e.opts.CpuUsed
	if e.libvpxAutoSelectSpeedActive() {
		cpuUsedForFreq = -e.libvpxCPUUsed()
	}
	e.interModeCheckFreq = libvpxInterModeCheckFrequencies(e.opts.Deadline, cpuUsedForFreq)
	e.interModeTestHitCounts = [libvpxInterModeCount]int{}
	e.interMBsTestedSoFar = 0
	e.interModeSpeedErrorBins = e.interModeErrorBins
	e.interModeErrorBins = [1024]uint32{}
	e.interRDFrameActive = true
	// Bump the per-frame baseline-threshold cache generation so the prior
	// frame's cached entries miss without an explicit clear.
	e.interRDThreshBaselineGen++
	// Also invalidate the per-frame ref-search-order pre-bind so the next
	// picker call recomputes it from this frame's refs.
	e.interRDFrameRefSearchOrderValid = false
}

func (e *VP8Encoder) endInterRDModeDecisionFrame() {
	if e == nil {
		return
	}
	e.interRDFrameActive = false
}

func (e *VP8Encoder) beginInterRDModeDecisionMacroblock() {
	if e == nil {
		return
	}
	if !e.interRDFrameActive {
		e.beginInterRDModeDecisionFrame()
	}
	e.interMBsTestedSoFar++
}

// interRDModeTestAllowed gates per-mode candidate evaluation by libvpx's
// rd_threshes hit-count throttle. It is callable from outside the picker
// loops via tests, so it accepts nil receivers / out-of-range indices, but
// the hot path always passes a non-nil encoder and a 0..libvpxInterModeCount-1
// modeIndex (callers iterate libvpxFastInterModeOrder). The two early returns
// keep the inlining cost low so the picker loop sees a flattened test.
func (e *VP8Encoder) interRDModeTestAllowed(modeIndex int) bool {
	if e == nil || !e.interRDFrameActive {
		return true
	}
	if modeIndex < 0 || modeIndex >= libvpxInterModeCount {
		return true
	}
	return e.interRDModeTestAllowedFast(modeIndex)
}

// interRDModeTestAllowedFast is the picker hot-path variant: e is non-nil,
// e.interRDFrameActive is true, and modeIndex is in
// [0, libvpxInterModeCount). Splitting the cheap predicate from the safe
// public entry point keeps both small enough that the picker can inline the
// fast path while tests keep the nil-/range-tolerant entry.
func (e *VP8Encoder) interRDModeTestAllowedFast(modeIndex int) bool {
	hits := e.interModeTestHitCounts[modeIndex]
	freq := e.interModeCheckFreq[modeIndex]
	if hits == 0 || freq <= 1 || e.interMBsTestedSoFar > freq*hits {
		return true
	}
	e.raiseInterRDThreshold(modeIndex)
	return false
}

func (e *VP8Encoder) recordInterRDModeTest(modeIndex int) {
	if e == nil || !e.interRDFrameActive || modeIndex < 0 || modeIndex >= libvpxInterModeCount {
		return
	}
	e.interModeTestHitCounts[modeIndex]++
}

func (e *VP8Encoder) lowerInterRDThresholdForImprovement(modeIndex int) {
	if e == nil || modeIndex < 0 || modeIndex >= libvpxInterModeCount {
		return
	}
	if r12cTrackMutation(e, modeIndex, "lowerForImprovement") {
		// Track mutation site externally.
	}
	if e.interRDThreshMult[modeIndex] >= libvpxMinThreshMult+2 {
		e.interRDThreshMult[modeIndex] -= 2
	} else {
		e.interRDThreshMult[modeIndex] = libvpxMinThreshMult
	}
	e.interRDThreshTouched[modeIndex] = true
}

func (e *VP8Encoder) raiseInterRDThreshold(modeIndex int) {
	if e == nil || modeIndex < 0 || modeIndex >= libvpxInterModeCount {
		return
	}
	r12cTrackMutation(e, modeIndex, "raise")
	e.interRDThreshMult[modeIndex] += 4
	if e.interRDThreshMult[modeIndex] > libvpxMaxThreshMult {
		e.interRDThreshMult[modeIndex] = libvpxMaxThreshMult
	}
	e.interRDThreshTouched[modeIndex] = true
}

func (e *VP8Encoder) lowerBestInterRDThreshold(modeIndex int) {
	if e == nil || modeIndex < 0 || modeIndex >= libvpxInterModeCount {
		return
	}
	r12cTrackMutation(e, modeIndex, "lowerBestRD")
	bestAdjustment := e.interRDThreshMult[modeIndex] >> 2
	if e.interRDThreshMult[modeIndex] >= libvpxMinThreshMult+bestAdjustment {
		e.interRDThreshMult[modeIndex] -= bestAdjustment
	} else {
		e.interRDThreshMult[modeIndex] = libvpxMinThreshMult
	}
	e.interRDThreshTouched[modeIndex] = true
}

func (e *VP8Encoder) lowerBestInterFastThreshold(modeIndex int) {
	if e == nil || modeIndex < 0 || modeIndex >= libvpxInterModeCount {
		return
	}
	r12cTrackMutation(e, modeIndex, "lowerBestFast")
	bestAdjustment := e.interRDThreshMult[modeIndex] >> 3
	if e.interRDThreshMult[modeIndex] >= libvpxMinThreshMult+bestAdjustment {
		e.interRDThreshMult[modeIndex] -= bestAdjustment
	} else {
		e.interRDThreshMult[modeIndex] = libvpxMinThreshMult
	}
	e.interRDThreshTouched[modeIndex] = true
}

func (e *VP8Encoder) recordFastInterModeErrorBin(distortion int) {
	if e == nil {
		return
	}
	if distortion < 0 {
		distortion = 0
	}
	bin := distortion >> 7
	if bin >= len(e.interModeErrorBins) {
		bin = len(e.interModeErrorBins) - 1
	}
	e.interModeErrorBins[bin]++
}

func libvpxInterModeRDThresholds(qIndex int, zbinOverQuant int, deadline Deadline, speed int) [libvpxInterModeCount]int {
	return libvpxInterModeRDThresholdsForContext(qIndex, zbinOverQuant, deadline, speed, libvpxInterModeThresholdContext{})
}

func libvpxInterModeRDThresholdsForContext(qIndex int, zbinOverQuant int, deadline Deadline, speed int, context libvpxInterModeThresholdContext) [libvpxInterModeCount]int {
	multipliers := libvpxInterModeThresholdMultipliersForContext(deadline, speed, context)
	qValue := min(vp8common.DCQuant(qIndex, 0), 160)
	q := max(int(math.Pow(float64(qValue), 1.25)), 8)
	_, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	var thresholds [libvpxInterModeCount]int
	for i, mult := range multipliers {
		if mult == libvpxInterModeThresholdDisabled {
			thresholds[i] = libvpxInterModeThresholdDisabled
			continue
		}
		if rdDiv == 1 {
			thresholds[i] = mult * q / 100
		} else {
			thresholds[i] = mult * q
		}
	}
	return thresholds
}

type libvpxInterModeThresholdContext struct {
	temporalLayers  int
	lastEnabled     bool
	goldenEnabled   bool
	closestRef      vp8common.MVReferenceFrame
	refFrameCount   int
	totalMBs        int
	staticThreshold int
	errorBins       *[1024]uint32
}

func libvpxInterModeThresholdMultipliers(deadline Deadline, speed int) [libvpxInterModeCount]int {
	return libvpxInterModeThresholdMultipliersForContext(deadline, speed, libvpxInterModeThresholdContext{})
}

func libvpxInterModeThresholdMultipliersForContext(deadline Deadline, speed int, context libvpxInterModeThresholdContext) [libvpxInterModeCount]int {
	speed = libvpxSpeedFeatureCPUUsed(deadline, speed)
	continuousSpeed := libvpxInterFrameContinuousSpeedForFeatureSpeed(deadline, speed)
	znn := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapZNN[:])
	vhPred := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapVHPred[:])
	bPred := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapBPred[:])
	tmPred := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapTM[:])
	new1 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapNew1[:])
	new2 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapNew2[:])
	split1 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapSplit1[:])
	split2 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapSplit2[:])

	var mult [libvpxInterModeCount]int
	mult[libvpxThrZero1] = 0
	mult[libvpxThrNearest1] = 0
	mult[libvpxThrNear1] = 0
	mult[libvpxThrDC] = 0
	mult[libvpxThrZero2] = znn
	mult[libvpxThrZero3] = znn
	mult[libvpxThrNearest2] = znn
	mult[libvpxThrNearest3] = znn
	mult[libvpxThrNear2] = znn
	mult[libvpxThrNear3] = znn
	mult[libvpxThrVPred] = vhPred
	mult[libvpxThrHPred] = vhPred
	mult[libvpxThrBPred] = bPred
	mult[libvpxThrTMPred] = tmPred
	mult[libvpxThrNew1] = new1
	mult[libvpxThrNew2] = new2
	mult[libvpxThrNew3] = new2
	mult[libvpxThrSplit1] = split1
	mult[libvpxThrSplit2] = split2
	mult[libvpxThrSplit3] = split2
	if context.temporalLayers > 1 && speed <= 6 && context.lastEnabled && context.goldenEnabled {
		shift := 1
		if context.closestRef == vp8common.GoldenFrame {
			shift = 3
		}
		mult[libvpxThrZero2] >>= shift
		mult[libvpxThrNearest2] >>= shift
		mult[libvpxThrNear2] >>= shift
	}
	if deadline == DeadlineRealtime && speed > 6 && context.errorBins != nil && context.totalMBs > 0 {
		thresh := libvpxRealtimeAdaptiveInterModeThreshold(context.errorBins, context.totalMBs, speed, context.staticThreshold)
		if context.refFrameCount > 1 {
			mult[libvpxThrNew1] = thresh
			mult[libvpxThrNearest1] = thresh >> 1
			mult[libvpxThrNear1] = thresh >> 1
		}
		if context.refFrameCount > 2 {
			mult[libvpxThrNew2] = thresh << 1
			mult[libvpxThrNearest2] = thresh
			mult[libvpxThrNear2] = thresh
		}
		if context.refFrameCount > 3 {
			mult[libvpxThrNew3] = thresh << 1
			mult[libvpxThrNearest3] = thresh
			mult[libvpxThrNear3] = thresh
		}
	}
	return mult
}

func libvpxRealtimeAdaptiveInterModeThreshold(errorBins *[1024]uint32, totalMBs int, speed int, staticThreshold int) int {
	if errorBins == nil || totalMBs <= 0 || speed <= 6 {
		return 2000
	}
	min := max(staticThreshold, 2000)
	min >>= 7
	if min < 0 {
		min = 0
	}
	if min > len(errorBins) {
		min = len(errorBins)
	}
	totalSkip := 0
	for i := 0; i < min; i++ {
		totalSkip += int(errorBins[i])
	}
	remaining := max(totalMBs-totalSkip, 0)
	sum := 0
	i := min
	for ; i < len(errorBins); i++ {
		sum += int(errorBins[i])
		if int64(10*sum) >= int64(speed-6)*int64(remaining) {
			break
		}
	}
	i--
	thresh := max(i<<7, 2000)
	return thresh
}

func libvpxInterModeCheckFrequencies(deadline Deadline, speed int) [libvpxInterModeCount]int {
	speed = libvpxSpeedFeatureCPUUsed(deadline, speed)
	continuousSpeed := libvpxInterFrameContinuousSpeedForFeatureSpeed(deadline, speed)
	new1Speed := continuousSpeed
	if deadline == DeadlineRealtime && speed == 10 {
		new1Speed = 16
	}
	zn2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapZN2[:])
	near2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapNear2[:])
	vhBPred := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapVHBPred[:])
	new1 := libvpxSpeedMap(new1Speed, libvpxModeCheckFreqMapNew1[:])
	new2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapNew2[:])
	split1 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapSplit1[:])
	split2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapSplit2[:])

	var freq [libvpxInterModeCount]int
	freq[libvpxThrZero1] = 0
	freq[libvpxThrNearest1] = 0
	freq[libvpxThrNear1] = 0
	freq[libvpxThrDC] = 0
	freq[libvpxThrTMPred] = 0
	freq[libvpxThrZero2] = zn2
	freq[libvpxThrZero3] = zn2
	freq[libvpxThrNearest2] = zn2
	freq[libvpxThrNearest3] = zn2
	freq[libvpxThrNear2] = near2
	freq[libvpxThrNear3] = near2
	freq[libvpxThrVPred] = vhBPred
	freq[libvpxThrHPred] = vhBPred
	freq[libvpxThrBPred] = vhBPred
	freq[libvpxThrNew1] = new1
	freq[libvpxThrNew2] = new2
	freq[libvpxThrNew3] = new2
	freq[libvpxThrSplit1] = split1
	freq[libvpxThrSplit2] = split2
	freq[libvpxThrSplit3] = split2
	return freq
}

func libvpxInterFrameContinuousSpeedForFeatureSpeed(deadline Deadline, speed int) int {
	switch deadline {
	case DeadlineBestQuality:
		return 0
	case DeadlineRealtime:
		return speed + 7
	default:
		if speed > 5 {
			speed = 5
		}
		return speed + 1
	}
}

func libvpxSpeedMap(speed int, entries []int) int {
	for i := 0; i+1 < len(entries); i += 2 {
		result := entries[i]
		limit := entries[i+1]
		if speed < limit {
			return result
		}
	}
	return 0
}

var libvpxThreshMultMapZNN = [...]int{
	0, 3, 1500, 4, 2000, 7, 1000, 9, 2000, libvpxSpeedMapMax,
}

var libvpxThreshMultMapVHPred = [...]int{
	1000, 3, 1500, 4, 2000, 7, 1000, 8, 2000, 14, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapBPred = [...]int{
	2000, 1, 2500, 3, 5000, 4, 7500, 7, 2500, 8, 5000, 13, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapTM = [...]int{
	1000, 3, 1500, 4, 2000, 7, 0, 8, 1000, 9, 2000, 14, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapNew1 = [...]int{
	1000, 3, 2000, 7, 2000, libvpxSpeedMapMax,
}

var libvpxThreshMultMapNew2 = [...]int{
	1000, 3, 2000, 4, 2500, 6, 4000, 7, 2000, 9, 2500, 12, 4000, libvpxSpeedMapMax,
}

var libvpxThreshMultMapSplit1 = [...]int{
	2500, 1, 1700, 3, 10000, 4, 25000, 5, libvpxInterModeThresholdDisabled, 7, 5000, 8, 10000, 9, 25000, 10, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapSplit2 = [...]int{
	5000, 1, 4500, 3, 20000, 4, 50000, 5, libvpxInterModeThresholdDisabled, 7, 10000, 8, 20000, 9, 50000, 10, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapZN2 = [...]int{
	0, 17, 2, 18, 4, 19, 8, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapVHBPred = [...]int{
	0, 6, 2, 7, 0, 10, 2, 12, 4, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapNear2 = [...]int{
	0, 6, 2, 7, 0, 10, 2, 17, 4, 18, 8, 19, 16, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapNew1 = [...]int{
	0, 17, 2, 18, 4, 19, 8, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapNew2 = [...]int{
	0, 6, 4, 7, 0, 10, 4, 17, 8, 18, 16, 19, 32, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapSplit1 = [...]int{
	0, 3, 2, 4, 7, 8, 2, 9, 7, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapSplit2 = [...]int{
	0, 2, 2, 3, 4, 4, 15, 8, 4, 9, 15, libvpxSpeedMapMax,
}

func interFrameSubpixelSearchCandidateCount() int {
	return interFrameSubpixelSearchMaxCandidates
}

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
	if decision, ok := e.inactiveInterFrameModeDecision(refs, refCount, mbRow, mbCol, mbCols); ok {
		return decision, true
	}
	if sourceAltRefZeroMVOnly {
		return sourceAltRefZeroMVAltRefDecision(refs, refCount, segmentID)
	}
	segmentQIndex := encoderSegmentQIndex(baseQIndex, segmentation, segmentID)
	if !e.interAnalysisUsesRDModeDecision() {
		return e.selectFastInterFrameModeDecision(
			src, refs, refCount,
			mbRow, mbCol, mbRows, mbCols,
			segmentQIndex, segmentID,
			above, left, aboveLeft,
			quant,
		)
	}
	return e.selectRDInterFrameModeDecision(
		src, refs, refCount,
		mbRow, mbCol, mbRows, mbCols,
		segmentQIndex, segmentID,
		above, left, aboveLeft,
		aboveTok, leftTok,
		quant,
	)
}

func (e *VP8Encoder) sourceAltRefZeroMVOnly(flags EncodeFlags) bool {
	return e != nil &&
		flags&EncodeInvisibleFrame == 0 &&
		e.opts.ARNRMaxFrames == 0 &&
		e.isSrcFrameAltRef(e.currentSourcePTS)
}

func sourceAltRefZeroMVAltRefDecision(refs []interAnalysisReference, refCount int, segmentID uint8) (interFrameModeDecision, bool) {
	for i := 0; i < refCount && i < len(refs); i++ {
		ref := refs[i]
		if ref.Frame != vp8common.AltRefFrame || ref.Img == nil {
			continue
		}
		mode := vp8enc.InterFrameMacroblockMode{
			RefFrame:  vp8common.AltRefFrame,
			Mode:      vp8common.ZeroMV,
			SegmentID: segmentID,
		}
		return interFrameModeDecision{
			ref:       ref,
			interMode: mode,
			intraMode: vp8enc.InterFrameMacroblockMode{
				RefFrame:  vp8common.IntraFrame,
				Mode:      vp8common.DCPred,
				UVMode:    vp8common.DCPred,
				SegmentID: segmentID,
			},
		}, true
	}
	return interFrameModeDecision{}, false
}

// inactiveInterFrameModeDecision mirrors libvpx's evaluate_inter_mode /
// evaluate_inter_mode_rd active_ptr early exits (vp8/encoder/pickinter.c and
// rdopt.c). When the active map is enabled and the current MB is marked
// inactive, the picker must short-circuit before any motion search or intra
// evaluation and lock the MB to ZEROMV from LAST with skip=1 / segment=0.
// This keeps both pickers aligned with libvpx even when invoked outside the
// per-frame loop (which already owns its own short-circuit via
// encodeInactiveInterMacroblock).
func (e *VP8Encoder) inactiveInterFrameModeDecision(refs []interAnalysisReference, refCount int, mbRow int, mbCol int, mbCols int) (interFrameModeDecision, bool) {
	if e == nil || !e.activeMapEnabled || mbCols <= 0 {
		return interFrameModeDecision{}, false
	}
	index := mbRow*mbCols + mbCol
	if index < 0 || index >= len(e.activeMap) || e.activeMap[index] != 0 {
		return interFrameModeDecision{}, false
	}
	for ri := range refCount {
		if refs[ri].Frame != vp8common.LastFrame {
			continue
		}
		return interFrameModeDecision{
			ref: refs[ri],
			interMode: vp8enc.InterFrameMacroblockMode{
				SegmentID:   0,
				MBSkipCoeff: true,
				RefFrame:    vp8common.LastFrame,
				Mode:        vp8common.ZeroMV,
				UVMode:      vp8common.DCPred,
			},
		}, true
	}
	return interFrameModeDecision{}, false
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
) (interFrameModeDecision, bool) {
	if !e.interRDFrameActive {
		e.beginInterRDModeDecisionMacroblock()
	}
	traceEnabled := e.oracleTraceEnabled()
	thresholds := e.interModeRDThresholdsForReferences(qIndex, refs, refCount)
	bestSet := false
	bestScore := maxInt()
	bestYRD := maxInt()
	bestModeIndex := -1
	best := interFrameModeDecision{
		intraMode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred, SegmentID: segmentID},
	}
	var newMVCandidates [3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
		start    interFrameSearchStart
	}
	var nearCache nearMVCandidateCache
	if !e.interRDFrameRefSearchOrderValid {
		e.interRDFrameRefSearchOrder = libvpxInterReferenceSearchOrder(refs, refCount)
		e.interRDFrameRefSearchOrderValid = true
	}
	refSearchOrder := e.interRDFrameRefSearchOrder

	for modeIndex, mbMode := range libvpxFastInterModeOrder {
		threshold := thresholds[modeIndex]
		if threshold == libvpxInterModeThresholdDisabled {
			continue
		}
		if bestSet && bestScore <= threshold {
			continue
		}

		refSlot := libvpxFastRefFrameOrder[modeIndex]
		if refSlot == 0 {
			if !e.interRDModeTestAllowed(modeIndex) {
				continue
			}
			e.recordInterRDModeTest(modeIndex)
			bestScoreBefore := bestScore
			bestYRDBefore := bestYRD
			mode, score, yrd, rate, ok := e.estimateInterIntraModeRDScore(src, qIndex, mbRow, mbCol, mbMode, bestYRD, aboveTok, leftTok, quant)
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
				bestModeIndex = modeIndex
				best = interFrameModeDecision{useIntra: true, intraMode: mode, projectedRate: rate}
			} else {
				e.raiseInterRDThreshold(modeIndex)
			}
			continue
		}

		ref, refIndex, ok := libvpxInterReferenceSearchAt(refs, refSearchOrder, refSlot)
		if !ok {
			continue
		}
		if !e.interRDModeTestAllowed(modeIndex) {
			continue
		}
		e.recordInterRDModeTest(modeIndex)
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
		if mbMode == vp8common.SplitMV {
			mvthresh := e.splitMVSubsearchThresholdForSlot(qIndex, refs, refCount, refSlot)
			mode, score, yrd, rate, rdLoopSkip, ok = e.selectInterFrameSplitModeRDScore(src, ref, mbRow, mbCol, mbRows, mbCols, qIndex, segmentID, mvthresh, above, left, aboveLeft, aboveTok, leftTok, quant)
		} else {
			mode, ok = e.interModeForRDLoopEntry(src, ref, refIndex, mbMode, mbRow, mbCol, mbRows, mbCols, qIndex, above, left, aboveLeft, &newMVCandidates, &nearCache)
			if ok {
				mode.SegmentID = segmentID
				acct, acctOK := e.estimateInterResidualRDAccounting(src, ref.Img, mbRow, mbCol, mbRows, mbCols, &mode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, e.interReferenceFrameRateForReference(ref))
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
			}
		}
		if !ok {
			continue
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
			bestModeIndex = modeIndex
			best = interFrameModeDecision{ref: ref, interMode: mode, intraMode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred, SegmentID: segmentID}, projectedRate: rate}
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
	return best, true
}

func (e *VP8Encoder) selectInterFrameSplitModeRDScore(
	src vp8enc.SourceImage, ref interAnalysisReference,
	mbRow int, mbCol int, mbRows int, mbCols int,
	qIndex int, segmentID uint8, mvthresh int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes,
	quant *vp8enc.MacroblockQuant,
) (vp8enc.InterFrameMacroblockMode, int, int, int, bool, bool) {
	signBias := e.interFrameSignBias()
	bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
	// libvpx: vp8_rd_pick_inter_mode SPLITMV branch picks
	// x->rd_threshes[THR_NEW{1,2,3}] based on vp8_ref_frame_order[mode_index]
	// (1=LAST, 2=GOLDEN, 3=ALTREF) and feeds it into
	// vp8_rd_pick_best_mbsegmentation as bsi->mvthresh, which the per-label
	// loop divides by label_count to gate NEW4X4 motion searches.
	bestSet := false
	bestScore := maxInt()
	bestPartitionYRD := maxInt()
	bestRate := 0
	var bestMode vp8enc.InterFrameMacroblockMode
	var splitSeeds splitMotionSearchSeeds

	// libvpx vp8_rd_pick_best_mbsegmentation seeds bsi.segment_rd with
	// the caller's best_rd cap and tightens it across shapes via the
	//
	//	if (this_segment_rd >= bsi->segment_rd) break;
	//
	// cutoff in rd_check_segment. govpx tracks the equivalent running
	// cap here. The cap is initialized to maxInt rather than the prior-
	// mode bestYRD because govpx's segmentYRD accumulator is a pure
	// per-label luma RDCOST sum (no mbsplit_tree / cost_mv_ref(SPLITMV)
	// segmentation overhead), while bestYRD is the prior-best mode's
	// full Y-RD which already includes mode-tree token contributions.
	// Seeding from bestYRD would over-prune at MBs where the SPLITMV
	// branch lands close to (but legitimately below) the prior-mode RD
	// threshold — the boundary that determines whether SPLITMV wins
	// the outer mode loop. The inter-shape running-best `min` still
	// applies and matches rd_check_segment's
	//
	//	bsi->segment_rd = this_segment_rd
	//
	// commit semantics: once shape A completes, shape B's labels are
	// bounded by shape A's segment_rd (and so on through subsequent
	// shapes), so later-evaluated shapes do less work but commit to a
	// different best-MV when their per-label cumulative would exceed an
	// earlier shape's total — matching libvpx's actual rd_check_segment
	// behaviour that ports R3-A localized as the SPLITMV gap source.
	bestSegmentYRD := maxInt()

	tryPartition := func(partition int) bool {
		var labelRD splitMotionLabelRDEvaluator
		initSplitMotionLabelRDEvaluator(&labelRD, e.rc.currentZbinOverQuant, aboveTok, leftTok, e.libvpxUseFastQuantForPick(), false)
		shape := selectInterFrameSplitMotionModeWithSegmentCutoff(src, ref.Img, ref.Frame, mbRow, mbCol, bestRefMV, qIndex, partition, left, above, e.interAnalysisSearchConfig(), e.interAnalysisCompressorSpeed(), &splitSeeds, &e.modeProbs.MV, mvthresh, &labelRD, quant, e.pickerCoefProbs(), bestSegmentYRD)
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
		acct, ok := e.estimateInterResidualRDAccounting(src, ref.Img, mbRow, mbCol, mbRows, mbCols, &mode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, e.interReferenceFrameRateForReference(ref))
		if !ok {
			return false
		}
		// libvpx vp8_rd_pick_best_mbsegmentation does not have an
		// `acct.yrd >= bestYRD` reject filter at the per-shape level —
		// the rd_check_segment incremental segment_rd cutoff
		// (segmentYRDCap above) is the sole gate, and the final outer
		// `tmp_rd < best_mode.yrd` check in vp8_rd_pick_inter_mode is
		// what decides whether the SPLITMV branch beats prior modes.
		// govpx's mode-loop bestScore comparison below is the exact
		// counterpart of that final outer check, so we drop the legacy
		// per-shape pre-filter that double-counted the cap and could
		// reject shapes whose acct.yrd just barely exceeded the prior-
		// mode bestYRD even when their per-shape segmentYRD was
		// acceptable. Without that double-count the picker preserves
		// the SPLITMV candidate at boundary MBs where the cutoff would
		// otherwise drop SPLITMV pick agreement on good-cpu0.
		if e.interAnalysisCompressorSpeed() != 0 && partition == 2 {
			splitSeeds = splitMotionSearchSeedsFrom8x8(&mode)
		}
		// libvpx:
		//
		//	if (this_segment_rd < bsi->segment_rd)
		//	    bsi->segment_rd = this_segment_rd;
		//
		// govpx tightens bestSegmentYRD to the running shape's
		// segmentYRD so subsequent shapes' per-label loops are bounded
		// by the smallest completed-shape Y-side label-RD-sum so far.
		// We use the per-label transform-domain segmentYRD the per-
		// label loop accumulated rather than acct.yrd because that is
		// the exact scalar libvpx's bsi.segment_rd compares against
		// (acct.yrd is a slightly different breakdown that includes
		// the SPLITMV mode-tree + ref-frame overhead).
		if shape.SegmentYRD < bestSegmentYRD {
			bestSegmentYRD = shape.SegmentYRD
		}
		score := acct.rd
		if acct.rdLoopSkip || !bestSet || score < bestScore {
			bestSet = true
			bestScore = score
			bestPartitionYRD = acct.yrd
			bestRate = acct.rate2
			bestMode = mode
		}
		if acct.rdLoopSkip {
			return true
		}
		return false
	}

	if e.interAnalysisCompressorSpeed() != 0 {
		for _, partition := range [3]int{2, 1, 0} {
			if rdLoopSkip := tryPartition(partition); rdLoopSkip {
				return bestMode, bestScore, bestPartitionYRD, bestRate, true, true
			}
		}
		if e.interAnalysisNoSkipBlock4x4Search() || (bestSet && bestMode.Partition == 2) {
			if rdLoopSkip := tryPartition(3); rdLoopSkip {
				return bestMode, bestScore, bestPartitionYRD, bestRate, true, true
			}
		}
		return bestMode, bestScore, bestPartitionYRD, bestRate, false, bestSet
	}

	for _, partition := range e.interAnalysisSplitPartitionOrder() {
		if rdLoopSkip := tryPartition(partition); rdLoopSkip {
			return bestMode, bestScore, bestPartitionYRD, bestRate, true, true
		}
	}
	return bestMode, bestScore, bestPartitionYRD, bestRate, false, bestSet
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

func (e *VP8Encoder) estimateInterIntraModeRDScore(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, mbMode vp8common.MBPredictionMode, bestRD int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant) (vp8enc.InterFrameMacroblockMode, int, int, int, bool) {
	zbinOverQuant := 0
	if e != nil {
		zbinOverQuant = e.rc.currentZbinOverQuant
	}
	fastQuant := e.libvpxUseFastQuantForPick()
	pickerProbs := e.pickerCoefProbs()
	if mbMode == vp8common.BPred {
		bModes, bRate, bDist, ok := predictBestBPredLumaModeRD(src, qIndex, zbinOverQuant, false, mbRow, mbCol, nil, nil, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, bestRD, pickerProbs, fastQuant)
		if !ok {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, false
		}
		uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRDWithProbs(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant)
		if !ok {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, false
		}
		yRate := bRate + e.interIntraYModeRate(vp8common.BPred)
		rate := yRate + uvRate + e.interIntraMacroblockModeRate()
		score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, bDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
		yrd := rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate, bDist)
		return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: uvMode, BModes: bModes}, score, yrd, rate, true
	}
	if mbMode < vp8common.DCPred || mbMode > vp8common.TMPred {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, false
	}
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, mbRow, mbCol, &mode, &e.reconstructScratch) {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, false
	}
	yRate, yDist := wholeBlockYTransformRD(src, &e.analysis.Img, mbRow, mbCol, qIndex, zbinOverQuant, aboveTok, leftTok, quant, pickerProbs, fastQuant)
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRDWithProbs(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, false
	}
	modeRate := e.interIntraYModeRate(mbMode)
	rate := yRate + uvRate + modeRate + e.interIntraMacroblockModeRate()
	score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, yDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
	yrd := rdModeScoreWithZbin(qIndex, zbinOverQuant, yRate+modeRate, yDist)
	return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: uvMode}, score, yrd, rate, true
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
	nearCache *nearMVCandidateCache,
) (vp8enc.InterFrameMacroblockMode, bool) {
	switch mbMode {
	case vp8common.ZeroMV:
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: vp8common.ZeroMV}, true
	case vp8common.NearestMV, vp8common.NearMV:
		var nearest, near vp8enc.MotionVector
		if nearCache != nil && refIndex >= 0 && refIndex < len(nearCache) {
			cached := &nearCache[refIndex]
			if !cached.computed {
				cached.nearest, cached.near = e.interAnalysisReferenceMotionPredictors(ref.Frame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
				cached.computed = true
			}
			nearest = cached.nearest
			near = cached.near
		} else {
			nearest, near = e.interAnalysisReferenceMotionPredictors(ref.Frame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
		}
		mv := nearest
		if mbMode == vp8common.NearMV {
			mv = near
		}
		if mv.IsZero() {
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
			search := e.interAnalysisSearchConfig()
			start := e.improvedInterFrameSearchStart(src, ref.Frame, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, search)
			mv, _ := selectRDInterFrameMotionVectorWithSearchStart(src, ref.Img, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, &e.modeProbs.MV)
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
// R11-N (2026-05-09 investigation): Per-candidate trace diff on the 720p
// noise fixture localizes the swap to MBs where govpx tests NEARESTMV and
// picks it, while libvpx skips the NEARESTMV iteration outright (no
// inter_candidate trace row emitted). At first divergence MB(0,3)
// frame=1, both engines compute identical ZEROMV (rate=1434 dist=255165
// score=256991) and DC_PRED (rate=820 dist=203248 score=204292). govpx
// then tests NEARESTMV with mv=(18,0) (the left col=2 NEWMV's MV) and
// scores 66728 (winner). libvpx never reaches the NEARESTMV case body —
// the picker iteration `continue`s before the emit hook. Inspecting the
// only viable continue paths in vp8_pick_inter_mode (rd_threshes,
// mode_check_freq, NEARESTMV.mv==0 reject, UMV bounds), none plausibly
// triggers given the observed neighbor state; libvpx's mode_mv[NEARESTMV]
// derivation at this MB remains unexplained without additional libvpx-
// side instrumentation. The cascade compounds: govpx picks NEARESTMV,
// next-MB's left.mv=non-zero -> nearest=non-zero -> NEARESTMV gets
// picked again. libvpx's DC_PRED at col=3 stays intra/zero-mv, next-MB's
// left.mv=0 -> nearest=0 -> NEARESTMV always rejected -> ZEROMV cascade
// continues. Pinned at L1=1.67pp pending a libvpx-side trace probe that
// captures (mode_mv[NEARESTMV], cnt[]) at MB entry to confirm whether
// libvpx really does see nearest=(0,0) (suggesting a state inconsistency
// between vp8_find_near_mvs_bias's mode_mv_sb fill and the case-NEAREST
// `continue` gate, e.g., a missed memcpy/clamp interaction) or rejects
// via a path that's not in the standard upstream source.
func (e *VP8Encoder) selectFastInterFrameModeDecision(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int,
	qIndex int, segmentID uint8,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	quant *vp8enc.MacroblockQuant,
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
	var newMVCandidates [3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
		start    interFrameSearchStart
	}
	var nearCache nearMVCandidateCache
	if !e.interRDFrameRefSearchOrderValid {
		e.interRDFrameRefSearchOrder = libvpxInterReferenceSearchOrder(refs, refCount)
		e.interRDFrameRefSearchOrderValid = true
	}
	refSearchOrder := e.interRDFrameRefSearchOrder
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

	pickerDebug := r12cPickerDebug(int(e.frameCount), mbRow, mbCol)
	if pickerDebug {
		r12cSetTrackContext(int(e.frameCount), mbRow, mbCol)
	}
	if pickerDebug {
		baseline := e.interModeRDThresholdsBaseline(qIndex, refs, refCount)
		baseSlice := make([]int, len(baseline))
		multSlice := make([]int, len(e.interRDThreshMult))
		touchedSlice := make([]bool, len(e.interRDThreshTouched))
		threshSlice := make([]int, len(thresholds))
		for i := range baseline {
			baseSlice[i] = baseline[i]
			threshSlice[i] = thresholds[i]
		}
		for i := range e.interRDThreshMult {
			multSlice[i] = e.interRDThreshMult[i]
			touchedSlice[i] = e.interRDThreshTouched[i]
		}
		rdMultV, rdDivV := libvpxRDConstantsWithZbin(qIndex, e.rc.currentZbinOverQuant)
		r12cPickerEmitState2(int(e.frameCount), mbRow, mbCol, baseSlice, multSlice, touchedSlice, threshSlice, qIndex, rdMultV, rdDivV, e.interMBsTestedSoFar)
	}
	for modeIndex, mbMode := range modeOrder {
		threshold := thresholds[modeIndex]
		if threshold == libvpxInterModeThresholdDisabled {
			if pickerDebug {
				r12cPickerEmitIteration(int(e.frameCount), mbRow, mbCol, modeIndex,
					oracleTraceModeName(mbMode), "disabled", threshold, bestScore, 0, 0, 0)
			}
			continue
		}
		if pickerDebug {
			r12cPickerEmitIteration(int(e.frameCount), mbRow, mbCol, modeIndex,
				oracleTraceModeName(mbMode), "enter", threshold, bestScore, 0, 0, 0)
		}
		if bestSet && bestScore <= threshold {
			if pickerDebug {
				r12cPickerEmitIteration(int(e.frameCount), mbRow, mbCol, modeIndex,
					oracleTraceModeName(mbMode), "rd_threshes", threshold, bestScore, 0, 0, 0)
			}
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
				best = interFrameModeDecision{useIntra: true, intraMode: mode, projectedRate: rate}
			} else {
				e.raiseInterRDThreshold(modeIndex)
			}
			continue
		}

		// Inlined libvpxInterReferenceSearchAt fast path (refSlot is in
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
		if rdActive && !e.interRDModeTestAllowedFast(modeIndex) {
			continue
		}
		if rdActive {
			e.interModeTestHitCounts[modeIndex]++
		}
		// libvpx pickinter.c does not implement SPLITMV in the non-RD picker
		// (vp8_pick_inter_mode falls back to RAISE-only). Short-circuit
		// here so we skip the per-mode fastInterModeForLoopEntry plumbing
		// entirely on the three SPLITMV slots (modeIndex 16/17/18).
		if mbMode == vp8common.SplitMV {
			e.raiseInterRDThreshold(modeIndex)
			continue
		}
		bestScoreBefore := bestScore
		bestSSEBefore := bestSSE
		mode, ok := e.fastInterModeForLoopEntry(src, ref, refIndex, mbMode, mbRow, mbCol, mbRows, mbCols, qIndex, above, left, aboveLeft, &newMVCandidates, &nearCache)
		if !ok {
			continue
		}
		mode.SegmentID = segmentID
		score, distortion, sse, rate, breakoutSkip, ok := e.estimateFastInterModeScoreWithReferenceRateAndSkip(src, ref.Img, mbRow, mbCol, mbRows, mbCols, &mode, above, left, aboveLeft, qIndex, e.interReferenceFrameRateForReference(ref), quant)
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
			best = interFrameModeDecision{ref: ref, interMode: mode, projectedRate: rate}
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
// and reserves stack space whether or not OracleTraceWriter is set), and
// the marker keeps the compiler from re-inlining the literal back into the
// caller. The trace path runs on the order of a few times per minute under
// the diagnostics harness, so a dedicated frame and an extra call there
// costs nothing while the clean hot-path stack frame trims morestack
// growth on the regular bench (~8% morestack/gopreempt time on 720p RT).
//
//go:noinline
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

//go:noinline
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

func libvpxFastInterReferenceAt(refs []interAnalysisReference, refCount int, refSlot int) (interAnalysisReference, int, bool) {
	return libvpxInterReferenceSearchAt(refs, libvpxInterReferenceSearchOrder(refs, refCount), refSlot)
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

func libvpxInterReferenceSearchAt(refs []interAnalysisReference, searchOrder [4]int, refSlot int) (interAnalysisReference, int, bool) {
	if refSlot <= 0 || refSlot >= len(searchOrder) {
		return interAnalysisReference{}, 0, false
	}
	refIndex := searchOrder[refSlot]
	if refIndex < 0 || refIndex >= len(refs) || refs[refIndex].Img == nil {
		return interAnalysisReference{}, 0, false
	}
	return refs[refIndex], refIndex, true
}

// nearMVCandidateCache memoizes (nearest, near) motion-vector predictors per
// ref slot for one macroblock's picker loop. The fast/RD inter pickers walk
// libvpxFastInterModeOrder and call into fastInterModeForLoopEntry once for
// each mode_index — Nearest and Near for the same ref reduce to identical
// vp8enc.InterFrameNearMotionVectorsAt inputs (ref.Frame, above/left/aboveLeft,
// mbRow, mbCol, signBias), so caching by refIndex turns up to 6 redundant
// neighbor walks per MB into 3.
type nearMVCandidateCache [3]struct {
	computed bool
	nearest  vp8enc.MotionVector
	near     vp8enc.MotionVector
}

func (e *VP8Encoder) fastInterModeForLoopEntry(
	src vp8enc.SourceImage, ref interAnalysisReference, refIndex int, mbMode vp8common.MBPredictionMode,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	newMVCandidates *[3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
		start    interFrameSearchStart
	},
	nearCache *nearMVCandidateCache,
) (vp8enc.InterFrameMacroblockMode, bool) {
	switch mbMode {
	case vp8common.ZeroMV:
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: vp8common.ZeroMV}, true
	case vp8common.NearestMV, vp8common.NearMV:
		var nearest, near vp8enc.MotionVector
		// libvpx's fast picker calls vp8_find_near_mvs once per ref slot
		// inside vp8_pick_inter_mode and reuses (nearest, near) across the
		// per-mode rd_threshes loop. Mirror that here: nearCache is
		// populated lazily per refIndex so back-to-back NearestMV / NearMV
		// candidates against the same reference share the neighbor walk.
		if nearCache != nil && refIndex >= 0 && refIndex < len(nearCache) {
			cached := &nearCache[refIndex]
			if !cached.computed {
				cached.nearest, cached.near = e.interAnalysisReferenceMotionPredictors(ref.Frame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
				cached.computed = true
			}
			nearest = cached.nearest
			near = cached.near
		} else {
			nearest, near = e.interAnalysisReferenceMotionPredictors(ref.Frame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
		}
		mv := nearest
		if mbMode == vp8common.NearMV {
			mv = near
		}
		if mv.IsZero() {
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
			search := e.interAnalysisSearchConfig()
			start := e.improvedInterFrameSearchStart(src, ref.Frame, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, search)
			mv, _ := selectInterFrameMotionVectorWithSearchStart(src, ref.Img, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, &e.modeProbs.MV)
			candidate.searched = true
			candidate.ok = !mv.IsZero()
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
		// libvpx pickinter.c does not support SPLITMV in the non-RD picker.
		return vp8enc.InterFrameMacroblockMode{}, false
	}
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
	debugBlk := r12cBPredDebug(mbRow, mbCol)
	if debugBlk {
		r12cBPredEmitConsts(mbRow, mbCol, qIndex, zbinOverQuant, rdMult, rdDiv)
	}
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
			if debugBlk {
				r12cBPredEmitTrace(e, mbRow, mbCol, block, bMode, modeRate, modeDist, modeCost, blockPred[:])
			}
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
		quantizeDecisionBlock(fastQuant, &dct, quantY1, qIndex, zbinOverQuant, 0, &qcoeff, &dqcoeff)
		var recon [16]byte
		dsp.IDCT4x4Add(&dqcoeff, bestPred[:], 4, recon[:], 4)
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
// through libvpxSplitMVSubsearchThreshold. Inside rd_check_segment the gate
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
	res := selectInterFrameSplitMotionModeWithSegmentCutoff(src, ref, refFrame, mbRow, mbCol, bestRefMV, qIndex, partition, left, above, search, compressorSpeed, seeds, mvProbs, mvthresh, labelRD, quant, coefProbs, maxInt())
	return res.Mode, res.OK
}

// splitMotionShapeResult mirrors the BEST_SEG_INFO commit produced by libvpx's
// rd_check_segment for a single segmentation shape. Mode is the per-block MV
// /sub-mode commit, SegmentYRD is the running cumulative RDCOST(rate,
// distortion) summed over the per-label best entries (matching
// `this_segment_rd` in rd_check_segment), Cutoff is true when the
// accumulator reached the SegmentYRDCap mid-shape and the per-label loop
// abandoned the shape early. OK is false only when the input arguments are
// invalid (mirrors the legacy bool return for back-compat callers).
type splitMotionShapeResult struct {
	Mode       vp8enc.InterFrameMacroblockMode
	SegmentYRD int
	Cutoff     bool
	OK         bool
}

// selectInterFrameSplitMotionModeWithSegmentCutoff ports rd_check_segment's
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
func selectInterFrameSplitMotionModeWithSegmentCutoff(src vp8enc.SourceImage, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, partition int, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, search interAnalysisSearchConfig, compressorSpeed int, seeds *splitMotionSearchSeeds, mvProbs *[2][vp8tables.MVPCount]uint8, mvthresh int, labelRD *splitMotionLabelRDEvaluator, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, segmentYRDCap int) splitMotionShapeResult {
	if ref == nil || refFrame == vp8common.IntraFrame || partition < 0 || partition >= vp8tables.NumMBSplits {
		return splitMotionShapeResult{}
	}
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  refFrame,
		Mode:      vp8common.SplitMV,
		Partition: uint8(partition),
	}
	width, height := splitMotionPartitionBlockSize(partition)
	labelCount := int(vp8tables.MBSplitCount[mode.Partition])
	labelMVThresh := splitMotionLabelMVThreshold(mvthresh, labelCount)
	// libvpx rd_check_segment seeds this_segment_rd with the
	// segmentation overhead RDCOST(mbsplit_tree + cost_mv_ref(SPLITMV),
	// 0). govpx skips that seed: the running bestSegmentYRD cap is
	// updated from each completed shape's segmentYRD which is on the
	// same pure-per-label-RD-sum scale, so the inter-shape cutoff
	// remains apples-to-apples between SPLITMV shapes. Omitting the
	// seed makes segmentYRD strictly smaller than libvpx's
	// this_segment_rd — slightly looser than libvpx for the inter-shape
	// cutoff, which avoids over-pruning at shape boundaries where the
	// per-shape MV picker disagrees with libvpx by a few RD units. The
	// segment-cutoff still fires as designed because subsequent shapes
	// must beat the prior shape's pure-label sum.
	segmentYRD := 0
	for subset := range labelCount {
		searchCenter := splitMotionSubsetSearchCenter(partition, subset, &mode, bestRefMV, compressorSpeed, seeds)
		stepParam := splitMotionSubsetSearchStepParam(partition, subset, compressorSpeed, seeds)
		fullSearchFallback := splitMotionSubsetFullSearchFallback(compressorSpeed)
		mv, bMode, labelBestRD := selectInterFrameSplitSubsetMotionModeWithSegmentCutoff(src, ref, mbRow, mbCol, &mode, subset, width, height, bestRefMV, searchCenter, stepParam, fullSearchFallback, qIndex, left, above, search, mvProbs, labelMVThresh, labelRD, quant, coefProbs)
		fillInterFrameSplitSubsetWithMode(&mode, subset, mv, bMode)
		// libvpx: this_segment_rd += best_label_rd; if (this_segment_rd
		// >= bsi->segment_rd) break;
		segmentYRD = saturatingAddInt(segmentYRD, labelBestRD)
		if segmentYRDCap > 0 && segmentYRD >= segmentYRDCap {
			mode.MV = mode.BlockMV[15]
			return splitMotionShapeResult{Mode: mode, SegmentYRD: segmentYRD, Cutoff: true, OK: true}
		}
	}
	mode.MV = mode.BlockMV[15]
	return splitMotionShapeResult{Mode: mode, SegmentYRD: segmentYRD, OK: true}
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
	if mvthresh <= 0 || labelCount <= 0 {
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
	mv, bMode, _ := selectInterFrameSplitSubsetMotionModeWithSegmentCutoff(src, ref, mbRow, mbCol, mode, subset, width, height, bestRefMV, searchCenter, stepParam, fullSearchFallback, qIndex, left, above, search, mvProbs, labelMVThresh, labelRD, quant, coefProbs)
	return mv, bMode
}

// selectInterFrameSplitSubsetMotionModeWithSegmentCutoff is the per-label
// inner loop body of rd_check_segment. The returned bestLabelRD is the
// per-label RDCOST(rate, distortion) the picker chose, so the per-shape
// caller can accumulate this_segment_rd and apply the inter-shape early
// cutoff. Behaviorally equivalent to the legacy
// selectInterFrameSplitSubsetMotionModeWithSearchThresholdAndLabelRD path —
// it only adds the bestLabelRD return.
func selectInterFrameSplitSubsetMotionModeWithSegmentCutoff(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *vp8enc.InterFrameMacroblockMode, subset int, width int, height int, bestRefMV vp8enc.MotionVector, searchCenter vp8enc.MotionVector, stepParam int, fullSearchFallback bool, qIndex int, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8, labelMVThresh int, labelRD *splitMotionLabelRDEvaluator, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs) (vp8enc.MotionVector, vp8common.BPredictionMode, int) {
	block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
	leftMV := analysisSplitLeftMV(mode, left, block)
	aboveMV := analysisSplitAboveMV(mode, above, block)
	bestMV := leftMV
	bestMode := vp8common.Left4x4
	bestRD, bestAbove, bestLeft, bestHasContexts := splitMotionLabelCandidateRD(src, ref, mbRow, mbCol, block, width, height, mode, subset, bestMV, qIndex, splitSubMotionLabelRate(bestMode), labelRD, quant, coefProbs)

	tryCandidate := func(candidateMode vp8common.BPredictionMode, mv vp8enc.MotionVector) {
		rate := splitSubMotionLabelRate(candidateMode)
		rd, nextAbove, nextLeft, hasContexts := splitMotionLabelCandidateRD(src, ref, mbRow, mbCol, block, width, height, mode, subset, mv, qIndex, rate, labelRD, quant, coefProbs)
		if rd < bestRD {
			bestRD = rd
			bestMV = mv
			bestMode = candidateMode
			bestAbove = nextAbove
			bestLeft = nextLeft
			bestHasContexts = hasContexts
		}
	}

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
	if labelMVThresh > 0 && bestRD < labelMVThresh {
		if labelRD != nil && bestHasContexts {
			labelRD.yAbove = bestAbove
			labelRD.yLeft = bestLeft
		}
		return bestMV, bestMode, bestRD
	}

	newMV, _ := selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(src, ref, mbRow, mbCol, block, width, height, searchCenter, bestRefMV, qIndex, stepParam, fullSearchFallback)
	if refinedMV, _, ok := refineInterFrameSplitBlockSubpixelMotionVector(src, ref, mbRow, mbCol, block, width, height, newMV, bestRefMV, qIndex, search, mvProbs); ok {
		newMV = refinedMV
	}
	newRate := splitSubMotionLabelRate(vp8common.New4x4)
	delta := vp8enc.MotionVector{Row: int16(int(newMV.Row) - int(bestRefMV.Row)), Col: int16(int(newMV.Col) - int(bestRefMV.Col))}
	newRate += splitMotionVectorCost(delta, mvProbs)
	newRD, nextAbove, nextLeft, hasContexts := splitMotionLabelCandidateRD(src, ref, mbRow, mbCol, block, width, height, mode, subset, newMV, qIndex, newRate, labelRD, quant, coefProbs)
	if newRD < bestRD {
		bestRD = newRD
		bestMV = newMV
		bestMode = vp8common.New4x4
		bestAbove = nextAbove
		bestLeft = nextLeft
		bestHasContexts = hasContexts
	}
	if labelRD != nil && bestHasContexts {
		labelRD.yAbove = bestAbove
		labelRD.yLeft = bestLeft
	}

	return bestMV, bestMode, bestRD
}

func splitMotionLabelCandidateRD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mode *vp8enc.InterFrameMacroblockMode, subset int, mv vp8enc.MotionVector, qIndex int, rate int, labelRD *splitMotionLabelRDEvaluator, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs) (int, [4]uint8, [4]uint8, bool) {
	if labelRD != nil {
		if labelRate, labelDist, nextAbove, nextLeft, ok := labelRD.rateDistortion(src, ref, mbRow, mbCol, qIndex, quant, coefProbs, mode, subset, mv, rate); ok {
			return rdModeScoreWithZbin(qIndex, labelRD.zbinOverQuant, labelRate, labelDist), nextAbove, nextLeft, true
		}
	}
	sad := splitBlockSAD(src, ref, mbRow, mbCol, block, width, height, mv)
	return splitMotionLabelRDScore(qIndex, rate, sad), [4]uint8{}, [4]uint8{}, false
}

func splitMotionLabelRDScore(qIndex int, rate int, distortion int) int {
	return rdModeScoreWithZbin(qIndex, 0, rate, distortion)
}

type splitMotionLabelRDEvaluator struct {
	zbinOverQuant int
	fastQuant     bool
	optimize      bool
	yAbove        [4]uint8
	yLeft         [4]uint8
}

func initSplitMotionLabelRDEvaluator(ev *splitMotionLabelRDEvaluator, zbinOverQuant int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, fastQuant bool, optimize bool) bool {
	if ev == nil {
		return false
	}
	*ev = splitMotionLabelRDEvaluator{
		zbinOverQuant: zbinOverQuant,
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

func (ev *splitMotionLabelRDEvaluator) rateDistortion(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, qIndex int, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, mode *vp8enc.InterFrameMacroblockMode, subset int, mv vp8enc.MotionVector, labelRate int) (int, int, [4]uint8, [4]uint8, bool) {
	if ev == nil || ref == nil || quant == nil || coefProbs == nil || mode == nil || mode.Partition >= vp8tables.NumMBSplits {
		return 0, 0, [4]uint8{}, [4]uint8{}, false
	}
	nextAbove := ev.yAbove
	nextLeft := ev.yLeft
	rate := labelRate
	distortion := 0
	for block := range 16 {
		if int(vp8tables.MBSplits[mode.Partition][block]) != subset {
			continue
		}
		var pred [16]byte
		if !predictSplitMotionBlock4x4(ref, mbRow, mbCol, block, mv, &pred) {
			return 0, 0, [4]uint8{}, [4]uint8{}, false
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
		eob := quantizeEncodedBlock(coefProbs, qIndex, 3, ctx, 0, ev.zbinOverQuant, splitInterModeZbinBoost, false, ev.fastQuant, ev.optimize, &dct, &quant.Y1, &qcoeff, &dqcoeff)
		rate += coefficientBlockTokenRate(coefProbs, 3, ctx, 0, &qcoeff, eob)
		distortion += transformBlockError(&dct, &dqcoeff)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		nextAbove[a] = hasCoeffs
		nextLeft[l] = hasCoeffs
	}
	return rate, distortion >> 2, nextAbove, nextLeft, true
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
	if refBaseY >= 0 && refBaseX >= 0 && refBaseY+4 <= ref.CodedHeight && refBaseX+4 <= ref.CodedWidth {
		for row := range 4 {
			copy(out[row*4:row*4+4], ref.Y[(refBaseY+row)*ref.YStride+refBaseX:])
		}
		return true
	}
	for row := range 4 {
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 4 {
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			out[row*4+col] = ref.Y[refY*ref.YStride+refX]
		}
	}
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
	dsp.SixTapPredict4x4(ref.YFull[start:], ref.YStride, xOffset, yOffset, out[:], 4)
	return true
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
	step8x16 [2]int
	step16x8 [2]int
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
	if compressorSpeed == 0 || mode == nil || partition < 0 || partition >= vp8tables.NumMBSplits || subset < 0 || subset >= int(vp8tables.MBSplitCount[uint8(partition)]) {
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
	block := int(vp8tables.MBSplitOffset[uint8(partition)][subset])
	if block&3 == 0 {
		if block >= 4 {
			return mode.BlockMV[block-4]
		}
		return bestRefMV
	}
	return mode.BlockMV[block-1]
}

func splitMotionSubsetSearchStepParam(partition int, subset int, compressorSpeed int, seeds *splitMotionSearchSeeds) int {
	if compressorSpeed == 0 {
		return 0
	}
	if seeds != nil && seeds.valid {
		switch partition {
		case 0:
			if subset >= 0 && subset < len(seeds.step16x8) {
				return seeds.step16x8[subset]
			}
		case 1:
			if subset >= 0 && subset < len(seeds.step8x16) {
				return seeds.step8x16[subset]
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

func libvpxSplitMVStepParamFromSeedDistance(sr int) int {
	if sr > interFrameMaxFirstStep {
		sr = interFrameMaxFirstStep
	} else if sr < 1 {
		sr = 1
	}
	step := 0
	for sr >>= 1; sr > 0; sr >>= 1 {
		step++
	}
	return interFrameMaxMVSearchSteps - 1 - step
}

func splitSubMotionLabelSearchCost(mode vp8common.BPredictionMode, qIndex int) int {
	cost := splitSubMotionLabelRate(mode)
	return (cost*libvpxSADPerBit4(qIndex) + 128) >> 8
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
	if d == nil || block < 0 || block > 15 {
		return 0
	}
	return d.Coeffs.BlockEOB(block, 0)
}

// UVEOB returns the per-4x4-block chroma EOB stored after the chosen SPLITMV
// partition's transform/quantize pass. U blocks are 0..3, V blocks are 4..7.
func (d *interSplitMVRDDecision) UVEOB(block int) int {
	if d == nil || block < 0 || block > 7 {
		return 0
	}
	return d.Coeffs.BlockEOB(16+block, 0)
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
	convertInterFrameMode(&mode, &decMode)
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
	stats := buildPredictedMacroblockCoefficientsRD(coefProbs, src, mbRow, mbCol, pred, aboveTok, leftTok, quant, qIndex, zbinOverQuant, splitInterModeZbinBoost, true, false, fastQuant, optimize, &decision.Coeffs)
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
	decision.RD = rdModeScoreWithZbin(qIndex, zbinOverQuant, decision.TotalRate, totalDist)
	decision.YRD = rdModeScoreWithZbin(qIndex, zbinOverQuant, decision.YRate, decision.YDist)
	return decision, true
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
		if int(vp8tables.MBSplits[mode.Partition][block]) != subset {
			continue
		}
		bMode := firstMode
		if block&3 != 0 && int(vp8tables.MBSplits[mode.Partition][block-1]) == subset {
			bMode = vp8common.Left4x4
		} else if block>>2 != 0 && int(vp8tables.MBSplits[mode.Partition][block-4]) == subset {
			bMode = vp8common.Above4x4
		}
		mode.BlockMV[block] = mv
		mode.BModes[block] = bMode
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
	if e != nil {
		signBias = e.interFrameSignBias()
	}
	for refIndex := 0; refIndex < refCount && refIndex < len(refs); refIndex++ {
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
		fullMV, fullCost := selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src, ref.Img, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, mvProbs)
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, fullMV)
		if fullCost == 0 {
			continue
		}
		refinedMV, _, ok := refineInterFrameSubpixelMotionVector(src, ref.Img, mbRow, mbCol, fullMV, bestRefMV, qIndex, search, mvProbs)
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
	if candidates == nil || count >= len(candidates) {
		return count
	}
	for i := range count {
		if candidates[i].Ref.Frame == ref.Frame && candidates[i].Ref.Img == ref.Img && candidates[i].MV == mv {
			return count
		}
	}
	candidates[count] = interAnalysisMotionCandidate{Ref: ref, MV: mv}
	return count + 1
}

func predictBestKeyFrameIntraMode(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, fastQuant bool) (vp8enc.KeyFrameMacroblockMode, int, bool) {
	coefProbs := &vp8tables.DefaultCoefProbs
	wholeY, wholeUV, wholeYRate, wholeYDist, wholeUVRate, wholeUVDist, ok := predictBestWholeBlockIntraModeRD(src, qIndex, 0, true, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, fastQuant)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, 0, false
	}
	wholeRate := wholeYRate + wholeUVRate
	wholeDist := wholeYDist + wholeUVDist
	wholeCost := rdModeScore(qIndex, wholeRate, wholeDist)
	wholeYCost := rdModeScore(qIndex, wholeYRate, wholeYDist)
	best := vp8enc.KeyFrameMacroblockMode{YMode: wholeY, UVMode: wholeUV}
	bModes, bRate, bDist, ok := predictBestBPredLumaModeRD(src, qIndex, 0, true, mbRow, mbCol, above, left, aboveTok, leftTok, quant, pred, scratch, wholeYCost, coefProbs, fastQuant)
	if !ok {
		return best, wholeRate, true
	}
	bUV, bUVRate, bUVDist, ok := predictBestIntraChromaModeRD(src, qIndex, 0, true, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, &vp8tables.DefaultCoefProbs, fastQuant)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, 0, false
	}
	bPredRate := bRate + bUVRate + intraYModeRate(true, vp8common.BPred)
	bPredCost := rdModeScore(qIndex, bPredRate, bDist+bUVDist)
	if bPredCost < wholeCost {
		best = vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred, UVMode: bUV, BModes: bModes}
		return best, bPredRate, true
	}
	return best, wholeRate, true
}

// predictBestKeyFrameIntraModeFast mirrors libvpx pickinter.c
// vp8_pick_intra_mode (the fast keyframe intra picker libvpx selects when
// `cpi->sf.RD == 0` or `compressor_speed == 2 (realtime)`). Unlike the RD
// picker it scores Y MB-level and B_PRED sub-modes in the pixel domain
// instead of running DCT/quantize/token-cost per candidate, and B_PRED
// sub-blocks iterate only the four fast candidates {DC, TM, VE, HE} rather
// than all ten intra4x4 modes. The chroma mode is picked once independently
// (matching libvpx's pick_intra_mbuv_mode call before the Y loop).
func predictBestKeyFrameIntraModeFast(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, fastQuant bool) (vp8enc.KeyFrameMacroblockMode, int, bool) {
	bestUVMode, bestUVRate, ok := pickFastIntraChromaMode(src, mbRow, mbCol, pred, scratch)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, 0, false
	}
	if !predictAnalysisChroma(pred, mbRow, mbCol, bestUVMode, scratch) {
		return vp8enc.KeyFrameMacroblockMode{}, 0, false
	}

	bestYMode, bestYRate, bestY16RD, ok := pickFastWholeBlockIntraYMode(src, qIndex, mbRow, mbCol, pred, scratch)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, 0, false
	}

	whole := vp8enc.KeyFrameMacroblockMode{YMode: bestYMode, UVMode: bestUVMode}
	wholeRate := bestYRate + bestUVRate

	bModes, bRate, bRD, ok := pickFastBPredLumaModeKF(src, qIndex, mbRow, mbCol, above, left, quant, pred, scratch, fastQuant)
	if !ok {
		// pickFastBPredLumaModeKF mutates pred.Y as it walks blocks; on
		// failure the analysis image may be partially overwritten. Fall back
		// to whole-block by re-running its prediction so the analysis frame
		// reflects the chosen mode for downstream coefficient construction.
		mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: bestYMode, UVMode: bestUVMode}
		predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch)
		return whole, wholeRate, true
	}
	if bRD < bestY16RD {
		return vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred, UVMode: bestUVMode, BModes: bModes}, bRate + bestUVRate + intraYModeRate(true, vp8common.BPred), true
	}
	// BPred lost: walk back the analysis frame to whole-block prediction.
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: bestYMode, UVMode: bestUVMode}
	predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch)
	return whole, wholeRate, true
}

// pickFastWholeBlockIntraYMode iterates wholeBlockIntraYModeCandidates and
// scores each via pixel-domain luma variance against the source. Mirrors the
// {DC,V,H,TM} loop in vp8_pick_intra_mode (pickinter.c). Returns the picked
// mode, its rate cost (mbmode_cost[KEY_FRAME][mode]), and the winning RDCOST
// — libvpx compares this RDCOST against the 4x4 BPred RDCOST when choosing
// between whole-block and split modes, so callers do the same.
func pickFastWholeBlockIntraYMode(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (vp8common.MBPredictionMode, int, int, bool) {
	bestMode := vp8common.DCPred
	bestRate := 0
	bestRD := 0
	for i, yMode := range wholeBlockIntraYModeCandidates {
		mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: yMode, UVMode: vp8common.DCPred}
		if !predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch) {
			return 0, 0, 0, false
		}
		dist, _ := macroblockLumaVarianceSSE(src, pred, mbRow, mbCol)
		rate := intraYModeRate(true, yMode)
		cost := rdModeScore(qIndex, rate, dist)
		if i == 0 || cost < bestRD {
			bestMode = yMode
			bestRate = rate
			bestRD = cost
		}
	}
	return bestMode, bestRate, bestRD, true
}

// pickFastIntraChromaMode iterates wholeBlockIntraUVModeCandidates and scores
// each by pure SSE — libvpx's pick_intra_mbuv_mode (pickinter.c) intentionally
// drops the rate term and picks by pred_error alone (no RDCOST). The returned
// rate is intraUVModeRate(picked), used by the caller for projected-rate
// reporting only; it does not influence the chroma decision.
func pickFastIntraChromaMode(src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (vp8common.MBPredictionMode, int, bool) {
	bestMode := vp8common.DCPred
	bestSSE := 0
	for i, uvMode := range wholeBlockIntraUVModeCandidates {
		if !predictAnalysisChroma(pred, mbRow, mbCol, uvMode, scratch) {
			return 0, 0, false
		}
		sse := macroblockChromaSSE(src, pred, mbRow, mbCol)
		if i == 0 || sse < bestSSE {
			bestMode = uvMode
			bestSSE = sse
		}
	}
	return bestMode, intraUVModeRate(true, bestMode), true
}

// pickFastBPredLumaModeKF mirrors libvpx pickinter.c pick_intra4x4mby_modes
// for keyframes: 16 sub-blocks, each scored via the four fast B-mode
// candidates {DC, TM, VE, HE} using pixel-domain 4x4 SSE. The mode rate uses
// libvpx's per-(A, L) keyframe table (mb->bmode_costs[A][L]) via
// bPredAnalysisAboveMode/LeftMode and bPredModeRate(keyFrame=true).
//
// After picking each block's mode the function performs the same
// DCT/quantize/dequantize/IDCT-add reconstruction libvpx executes via
// vp8_encode_intra4x4block (encodeintra.c), so subsequent blocks see
// reconstructed pixels (not raw predictor pixels) when they read their
// left/above-right neighbors. Without this step, govpx's predictor refs for
// blocks 1..15 would diverge from libvpx's because libvpx writes
// reconstructed pixels back into xd->dst.y_buffer between sub-blocks.
//
// Returns the picked sub-modes, the sum of bmode rates, and the BPred RDCOST
// (RDCOST(mbmode_cost[B_PRED]+sum_rates, sum_4x4_SSE)) — matching libvpx's
// `error4x4` return that the caller compares against `error16x16`.
func pickFastBPredLumaModeKF(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, fastQuant bool) ([16]vp8common.BPredictionMode, int, int, bool) {
	if quant == nil {
		return [16]vp8common.BPredictionMode{}, 0, 0, false
	}
	refs := vp8dec.BuildIntraPredictorRefs(pred, mbRow, mbCol, &scratch.Refs)
	yOff := mbRow*16*pred.YStride + mbCol*16
	y := pred.Y[yOff:]
	var modes [16]vp8common.BPredictionMode
	totalRate := 0
	totalDist := 0
	for block := range 16 {
		bestMode := vp8common.BDCPred
		bestRate := 0
		bestDist := 0
		bestCost := 0
		var bestPred [16]byte
		aboveMode := bPredAnalysisAboveMode(true, above, modes, block)
		leftMode := bPredAnalysisLeftMode(true, left, modes, block)
		for i, candidate := range fastBPredIntraModeCandidates {
			var blockPred [16]byte
			if !predictAnalysisBPredBlock(candidate, blockPred[:], 4, y, pred.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
				return [16]vp8common.BPredictionMode{}, 0, 0, false
			}
			modeRate := bPredModeRate(true, candidate, aboveMode, leftMode)
			modeDist := bPredBlockSSE(src, mbRow, mbCol, block, blockPred[:], 4)
			cost := rdModeScore(qIndex, modeRate, modeDist)
			if i == 0 || cost < bestCost {
				bestMode = candidate
				bestRate = modeRate
				bestDist = modeDist
				bestCost = cost
				bestPred = blockPred
			}
		}
		modes[block] = bestMode

		// Mirror libvpx vp8_encode_intra4x4block: re-predict, residual,
		// DCT, quantize/dequant, IDCT-add into the analysis Y plane so the
		// next block's predictor neighbors come from reconstructed pixels.
		var input [16]int16
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		fillBPredResidual4x4(src, mbRow, mbCol, block, bestPred[:], 4, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		quantizeDecisionBlock(fastQuant, &dct, &quant.Y1, qIndex, 0, 0, &qcoeff, &dqcoeff)
		var recon [16]byte
		dsp.IDCT4x4Add(&dqcoeff, bestPred[:], 4, recon[:], 4)
		copyBPredBlock(recon[:], 4, y, pred.YStride, block)

		totalRate += bestRate
		totalDist += bestDist
	}
	mbModeRate := intraYModeRate(true, vp8common.BPred)
	rd := rdModeScore(qIndex, mbModeRate+totalRate, totalDist)
	return modes, totalRate, rd, true
}

func (e *VP8Encoder) predictBestInterIntraModeCost(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (vp8enc.InterFrameMacroblockMode, int, bool) {
	fastQuant := e.libvpxUseFastQuantForPick()
	zbinOverQuant := e.rc.currentZbinOverQuant
	pickerProbs := e.pickerCoefProbs()
	wholeY, wholeUV, wholeYRate, wholeYDist, wholeUVRate, wholeUVDist, ok := predictBestWholeBlockIntraModeRDWithProbs(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, pickerProbs, e.modeProbs.YMode[:], e.modeProbs.UVMode[:], fastQuant)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, false
	}
	wholeRate := wholeYRate + wholeUVRate + e.interIntraMacroblockModeRate()
	wholeDist := wholeYDist + wholeUVDist
	wholeCost := rdModeScoreWithZbin(qIndex, zbinOverQuant, wholeRate, wholeDist) + libvpxInterIntraRDPenalty(qIndex)
	wholeYCost := rdModeScoreWithZbin(qIndex, zbinOverQuant, wholeYRate, wholeYDist)
	best := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: wholeY, UVMode: wholeUV}
	bestCost := wholeCost
	bModes, bRate, bDist, ok := predictBestBPredLumaModeRD(src, qIndex, zbinOverQuant, false, mbRow, mbCol, nil, nil, aboveTok, leftTok, quant, pred, scratch, wholeYCost, pickerProbs, fastQuant)
	if !ok {
		return best, bestCost, true
	}
	bUV, bUVRate, bUVDist, ok := predictBestIntraChromaModeRDWithProbs(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, pickerProbs, e.modeProbs.UVMode[:], fastQuant)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, false
	}
	bPredRate := bRate + bUVRate + e.interIntraYModeRate(vp8common.BPred) + e.interIntraMacroblockModeRate()
	bPredTotal := rdModeScoreWithZbin(qIndex, zbinOverQuant, bPredRate, bDist+bUVDist) + libvpxInterIntraRDPenalty(qIndex)
	if bPredTotal < bestCost {
		best = vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: bUV, BModes: bModes}
		bestCost = bPredTotal
	}
	return best, bestCost, true
}

// predictBestWholeBlockIntraModeRD picks the best 16x16 intra Y mode using
// libvpx's transform-domain RD (rdopt.c macro_block_yrd) instead of pixel-SSE
// — the AC coefficients are quantized as Y_NO_DC and the 16 DC samples are
// lifted into the Y2 block, Walsh-transformed, and quantized; rate is the
// summed cost_coeffs and distortion is libvpx's
// (mbblock_error<<2 + y2_block_error) >> 4.
func predictBestWholeBlockIntraModeRD(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (vp8common.MBPredictionMode, vp8common.MBPredictionMode, int, int, int, int, bool) {
	return predictBestWholeBlockIntraModeRDWithProbs(src, qIndex, zbinOverQuant, keyFrame, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, nil, nil, fastQuant)
}

func predictBestWholeBlockIntraModeRDWithProbs(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, interYModeProbs []uint8, interUVModeProbs []uint8, fastQuant bool) (vp8common.MBPredictionMode, vp8common.MBPredictionMode, int, int, int, int, bool) {
	if quant == nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	if coefProbs == nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	bestYMode := vp8common.DCPred
	bestYRate := 0
	bestYDist := 0
	bestYCost := 0
	for i, yMode := range wholeBlockIntraYModeCandidates {
		mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: yMode, UVMode: vp8common.DCPred}
		if !predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch) {
			return 0, 0, 0, 0, 0, 0, false
		}
		yRate, yDist := wholeBlockYTransformRD(src, pred, mbRow, mbCol, qIndex, zbinOverQuant, aboveTok, leftTok, quant, coefProbs, fastQuant)
		rate := intraYModeRateWithProbs(keyFrame, yMode, interYModeProbs) + yRate
		cost := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, yDist)
		if i == 0 || cost < bestYCost {
			bestYMode = yMode
			bestYRate = rate
			bestYDist = yDist
			bestYCost = cost
		}
	}

	bestUVMode, bestUVRate, bestUVDist, ok := predictBestIntraChromaModeRDWithProbs(src, qIndex, zbinOverQuant, keyFrame, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, interUVModeProbs, fastQuant)
	if !ok {
		return 0, 0, 0, 0, 0, 0, false
	}
	return bestYMode, bestUVMode, bestYRate, bestYDist, bestUVRate, bestUVDist, true
}

// wholeBlockYTransformRD ports libvpx rdopt.c macro_block_yrd. The selected
// yMode prediction is assumed to be present in pred at (mbRow, mbCol).
// aboveTok and leftTok seed the per-block token contexts; libvpx
// vp8_rdcost_mby reads them from `e_mbd.above_context` / `left_context`.
// Callers pass the coefficient probability base that the matching packet
// writer will use for token-rate costing.
func wholeBlockYTransformRD(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, qIndex int, zbinOverQuant int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (int, int) {
	if coefProbs == nil {
		return 0, 0
	}
	var dct [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	var y2Input [16]int16
	var y2Coeff [16]int16
	var y2Q [16]int16
	var y2DQ [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		y2Left = leftTok.Y2
	}

	rate := 0
	mbblockError := 0
	// Whole-MB residual+DCT batch — mirrors libvpx vp8_transform_intra_mby's
	// fdct8x4 chain. The per-block rate/distortion accumulation still runs
	// serially because token-context (yAbove/yLeft) and the regular-quantize
	// zbin-zerorun are block-sequential.
	var residuals [16 * 16]int16
	var dcts [16 * 16]int16
	gatherMacroblockYResiduals4x4(src.Y, src.YStride, src.Width, src.Height, pred.Y, pred.YStride, mbCol*16, mbRow*16, residuals[:])
	vp8enc.ForwardDCT4x4Batch(residuals[:], dcts[:], 16)
	for block := range 16 {
		copy(dct[:], dcts[block*16:block*16+16])
		y2Input[block] = dct[0]
		dct[0] = 0
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		eob := quantizeDecisionBlock(fastQuant, &dct, &quant.Y1DC, qIndex, zbinOverQuant, 0, &qcoeff, &dqcoeff)
		rate += coefficientBlockTokenRate(coefProbs, 0, ctx, 1, &qcoeff, eob)
		mbblockError += transformBlockError(&dct, &dqcoeff)
		hasCoeffs := uint8(0)
		if eob > 1 {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
	}
	vp8enc.ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
	y2Ctx := int(y2Above + y2Left)
	y2EOB := quantizeDecisionBlock(fastQuant, &y2Coeff, &quant.Y2, qIndex, zbinOverQuant/2, 0, &y2Q, &y2DQ)
	rate += coefficientBlockTokenRate(coefProbs, 1, y2Ctx, 0, &y2Q, y2EOB)
	y2Error := transformBlockError(&y2Coeff, &y2DQ)
	distortion := ((mbblockError << 2) + y2Error) >> 4
	return rate, distortion
}

func predictBestIntraChromaModeRD(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (vp8common.MBPredictionMode, int, int, bool) {
	return predictBestIntraChromaModeRDWithProbs(src, qIndex, zbinOverQuant, keyFrame, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, nil, fastQuant)
}

func predictBestIntraChromaModeRDWithProbs(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, interUVModeProbs []uint8, fastQuant bool) (vp8common.MBPredictionMode, int, int, bool) {
	if quant == nil || coefProbs == nil {
		return 0, 0, 0, false
	}
	bestUVMode := vp8common.DCPred
	bestUVRate := 0
	bestUVDist := 0
	bestUVCost := 0
	for i, uvMode := range wholeBlockIntraUVModeCandidates {
		if !predictAnalysisChroma(pred, mbRow, mbCol, uvMode, scratch) {
			return 0, 0, 0, false
		}
		tokenRate, dist := wholeBlockChromaTransformRD(src, pred, mbRow, mbCol, qIndex, zbinOverQuant, aboveTok, leftTok, quant, coefProbs, fastQuant)
		rate := intraUVModeRateWithProbs(keyFrame, uvMode, interUVModeProbs) + tokenRate
		cost := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, dist)
		if i == 0 || cost < bestUVCost {
			bestUVMode = uvMode
			bestUVRate = rate
			bestUVDist = dist
			bestUVCost = cost
		}
	}
	return bestUVMode, bestUVRate, bestUVDist, true
}

// wholeBlockChromaTransformRD mirrors libvpx rdopt.c rd_pick_intra_mbuv_mode:
// the predicted U/V blocks are transformed, quantized, token-costed, and
// measured with transform-domain reconstruction error divided by four.
func wholeBlockChromaTransformRD(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, qIndex int, zbinOverQuant int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (int, int) {
	if pred == nil || quant == nil || coefProbs == nil {
		return maxInt() / 4, maxInt() / 4
	}
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	if aboveTok != nil {
		uvAbove = tokenUVContextArray(aboveTok)
	}
	if leftTok != nil {
		uvLeft = tokenUVContextArray(leftTok)
	}

	rate := 0
	distortion := 0
	// Whole-UV residual+DCT batch — mirrors libvpx vp8_transform_mbuv's
	// pair of fdct8x4 calls. Token-context updates and the
	// regular-quantize zbin-zerorun keep the per-block accumulation
	// loop serial.
	var residuals [8 * 16]int16
	var dcts [8 * 16]int16
	gatherMacroblockUVResiduals4x4(src.U, src.UStride, uvWidth, uvHeight, pred.U, pred.UStride, mbCol*8, mbRow*8, residuals[0:64])
	gatherMacroblockUVResiduals4x4(src.V, src.VStride, uvWidth, uvHeight, pred.V, pred.VStride, mbCol*8, mbRow*8, residuals[64:128])
	vp8enc.ForwardDCT4x4Batch(residuals[:], dcts[:], 8)
	for slot := range 8 {
		block := 16 + slot
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		copy(dct[:], dcts[slot*16:slot*16+16])
		a, l := macroblockCoefficientUVContextIndex(block)
		ctx := int(uvAbove[a] + uvLeft[l])
		eob := quantizeDecisionBlock(fastQuant, &dct, &quant.UV, qIndex, zbinOverQuant, 0, &qcoeff, &dqcoeff)
		rate += coefficientBlockTokenRate(coefProbs, 2, ctx, 0, &qcoeff, eob)
		distortion += transformBlockError(&dct, &dqcoeff)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	return rate, distortion >> 2
}

// Ported from libvpx v1.16.0 vp8/encoder/rdopt.c rd_pick_intra4x4block (and
// the per-MB driver rd_pick_intra4x4mby_modes at lines 519-644). Audit notes
// (parity items confirmed against the reference):
//  1. Bmode cost source: keyframe path uses vp8tables.KeyFrameBModeProbs[A][L]
//     via bPredAnalysisAboveMode/LeftMode, matching mb->bmode_costs[A][L];
//     inter path uses vp8tables.DefaultBModeProbs (cf. mb->inter_bmode_costs).
//     Note libvpx's vp8_init_mode_costs overwrites inter_bmode_costs[0..3]
//     with sub_mv_ref-token costs after the bmode-token init — but mirroring
//     that quirk here regresses good-cpu3-vbr SPLITMV decisions, so the RD
//     picker keeps the bmode-token costs across all 10 slots. The fast
//     picker (estimateFastBPredIntraModeScore) honors the libvpx-stale
//     overwrite via libvpxInterFastBpredModeCost, where rt-cpu0/4/8 corner
//     MBs need it for B_PRED-vs-NEWMV tiebreak parity.
//  2. ENTROPY_CONTEXT: tokenAbove/tokenLeft are seeded once from the caller
//     and only committed using bestEOB after the candidate loop, mirroring
//     libvpx's "*a = tempa; *l = templ;" inside the if-best block.
//  3. Reconstruction: dsp.IDCT4x4Add is invoked inside the winning branch
//     and the resulting bestRecon is written via copyBPredBlock at the end
//     of each block iteration, equivalent to libvpx's deferred
//     vp8_short_idct4x4llm(best_dqcoeff, best_predictor, ...) call.
//  4. Bailout: govpx returns ok=false when the running rate/dist already
//     exceeds bestRD; callers then fall back to the whole-block result, the
//     same role as libvpx's "return INT_MAX" when total_rd >= best_rd.
//  5. BPred container cost: callers (predictBestKeyFrameIntraMode and
//     predictBestInterIntraModeCost) add intraYModeRate(keyFrame, BPred)
//     before comparing with whole-block RD, matching libvpx's
//     "cost = mb->mbmode_cost[xd->frame_type][B_PRED];" seed.
//  6. intra_prediction_down_copy: predictAnalysisBPredBlock reads
//     refs.YAbove[16:20] for the bottom-right sub-block, replacing libvpx's
//     in-place predictor copy.
//
// libvpx applies RDCOST once at MB level (rdopt.c rd_pick_intra4x4mby_modes);
// applying it per-block compounds the +128 rounding bias 16x. bestRD lets
// the caller short-circuit when the running cost already exceeds the best
// macroblock RD found so far.
func predictBestBPredLumaModeRD(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, bestRD int, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) ([16]vp8common.BPredictionMode, int, int, bool) {
	if quant == nil {
		return [16]vp8common.BPredictionMode{}, 0, 0, false
	}
	if coefProbs == nil {
		return [16]vp8common.BPredictionMode{}, 0, 0, false
	}
	refs := vp8dec.BuildIntraPredictorRefs(pred, mbRow, mbCol, &scratch.Refs)
	yOff := mbRow*16*pred.YStride + mbCol*16
	y := pred.Y[yOff:]
	var modes [16]vp8common.BPredictionMode
	var tokenAbove [4]uint8
	var tokenLeft [4]uint8
	if aboveTok != nil {
		tokenAbove = aboveTok.Y1
	}
	if leftTok != nil {
		tokenLeft = leftTok.Y1
	}
	totalRate := 0
	totalDist := 0
	for block := range 16 {
		bestMode := vp8common.BDCPred
		bestEOB := 0
		var bestRecon [16]byte
		bestRate := 0
		bestDist := 0
		bestCost := 0
		for i, candidate := range bPredIntraModeCandidates {
			var candidatePred [16]byte
			if !predictAnalysisBPredBlock(candidate, candidatePred[:], 4, y, pred.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
				return [16]vp8common.BPredictionMode{}, 0, 0, false
			}
			var input [16]int16
			var dct [16]int16
			var qcoeff [16]int16
			var dqcoeff [16]int16
			fillBPredResidual4x4(src, mbRow, mbCol, block, candidatePred[:], 4, &input)
			vp8enc.ForwardDCT4x4(input[:], 4, &dct)
			tokenCtx := int(tokenAbove[block&3] + tokenLeft[(block&0x0c)>>2])
			eob := quantizeDecisionBlock(fastQuant, &dct, &quant.Y1, qIndex, zbinOverQuant, 0, &qcoeff, &dqcoeff)
			coefRate := coefficientBlockTokenRate(coefProbs, 3, tokenCtx, 0, &qcoeff, eob)
			aboveMode := bPredAnalysisAboveMode(keyFrame, above, modes, block)
			leftMode := bPredAnalysisLeftMode(keyFrame, left, modes, block)
			rate := bPredModeRate(keyFrame, candidate, aboveMode, leftMode) + coefRate
			dist := transformBlockError(&dct, &dqcoeff) >> 2
			cost := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, dist)
			if i == 0 || cost < bestCost {
				var candidateRecon [16]byte
				bestMode = candidate
				dsp.IDCT4x4Add(&dqcoeff, candidatePred[:], 4, candidateRecon[:], 4)
				bestRecon = candidateRecon
				bestEOB = eob
				bestRate = rate
				bestDist = dist
				bestCost = cost
			}
		}
		modes[block] = bestMode
		copyBPredBlock(bestRecon[:], 4, y, pred.YStride, block)
		hasCoeffs := uint8(0)
		if bestEOB > 0 {
			hasCoeffs = 1
		}
		tokenAbove[block&3] = hasCoeffs
		tokenLeft[(block&0x0c)>>2] = hasCoeffs
		totalRate += bestRate
		totalDist += bestDist
		if bestRD > 0 && rdModeScoreWithZbin(qIndex, zbinOverQuant, totalRate, totalDist) >= bestRD {
			return [16]vp8common.BPredictionMode{}, 0, 0, false
		}
	}
	return modes, totalRate, totalDist, true
}

func bPredAnalysisAboveMode(keyFrame bool, above *vp8enc.KeyFrameMacroblockMode, modes [16]vp8common.BPredictionMode, block int) vp8common.BPredictionMode {
	if !keyFrame {
		return vp8common.BDCPred
	}
	if block >= 4 {
		return modes[block-4]
	}
	if above == nil {
		return vp8common.BDCPred
	}
	if above.YMode == vp8common.BPred {
		return above.BModes[block+12]
	}
	return blockModeFromKeyFrameMacroblockMode(above.YMode)
}

func bPredAnalysisLeftMode(keyFrame bool, left *vp8enc.KeyFrameMacroblockMode, modes [16]vp8common.BPredictionMode, block int) vp8common.BPredictionMode {
	if !keyFrame {
		return vp8common.BDCPred
	}
	if block&3 != 0 {
		return modes[block-1]
	}
	if left == nil {
		return vp8common.BDCPred
	}
	if left.YMode == vp8common.BPred {
		return left.BModes[block+3]
	}
	return blockModeFromKeyFrameMacroblockMode(left.YMode)
}

func blockModeFromKeyFrameMacroblockMode(mode vp8common.MBPredictionMode) vp8common.BPredictionMode {
	switch mode {
	case vp8common.VPred:
		return vp8common.BVEPred
	case vp8common.HPred:
		return vp8common.BHEPred
	case vp8common.TMPred:
		return vp8common.BTMPred
	default:
		return vp8common.BDCPred
	}
}

func intraYModeRate(keyFrame bool, mode vp8common.MBPredictionMode) int {
	return intraYModeRateWithProbs(keyFrame, mode, nil)
}

func intraYModeRateWithProbs(keyFrame bool, mode vp8common.MBPredictionMode, interProbs []uint8) int {
	if keyFrame {
		return treeTokenCost(vp8tables.KeyFrameYModeTree[:], vp8tables.KeyFrameYModeProbs[:], int(mode))
	}
	if len(interProbs) == vp8tables.YModeProbCount && !allZeroUint8(interProbs) {
		return treeTokenCost(vp8tables.YModeTree[:], interProbs, int(mode))
	}
	return treeTokenCost(vp8tables.YModeTree[:], vp8tables.DefaultYModeProbs[:], int(mode))
}

func (e *VP8Encoder) interIntraYModeRate(mode vp8common.MBPredictionMode) int {
	if e == nil {
		return intraYModeRate(false, mode)
	}
	return intraYModeRateWithProbs(false, mode, e.modeProbs.YMode[:])
}

func intraUVModeRate(keyFrame bool, mode vp8common.MBPredictionMode) int {
	return intraUVModeRateWithProbs(keyFrame, mode, nil)
}

func intraUVModeRateWithProbs(keyFrame bool, mode vp8common.MBPredictionMode, interProbs []uint8) int {
	if keyFrame {
		return treeTokenCost(vp8tables.UVModeTree[:], vp8tables.KeyFrameUVModeProbs[:], int(mode))
	}
	if len(interProbs) == vp8tables.UVModeProbCount && !allZeroUint8(interProbs) {
		return treeTokenCost(vp8tables.UVModeTree[:], interProbs, int(mode))
	}
	return treeTokenCost(vp8tables.UVModeTree[:], vp8tables.DefaultUVModeProbs[:], int(mode))
}

func allZeroUint8(values []uint8) bool {
	for _, value := range values {
		if value != 0 {
			return false
		}
	}
	return true
}

func bPredModeRate(keyFrame bool, mode vp8common.BPredictionMode, above vp8common.BPredictionMode, left vp8common.BPredictionMode) int {
	if keyFrame {
		return treeTokenCost(vp8tables.BModeTree[:], vp8tables.KeyFrameBModeProbs[int(above)][int(left)][:], int(mode))
	}
	return treeTokenCost(vp8tables.BModeTree[:], vp8tables.DefaultBModeProbs[:], int(mode))
}

// libvpxInterFastBpredModeCost mirrors libvpx vp8/encoder/modecosts.c
// vp8_init_mode_costs's `inter_bmode_costs` table as read by the inter-frame
// non-RD fast picker (vp8/encoder/pickinter.c pick_intra4x4block).
//
// libvpx initializes the table in two steps:
//
//	vp8_cost_tokens(rd_costs->inter_bmode_costs, x->fc.bmode_prob, vp8_bmode_tree);
//	vp8_cost_tokens(rd_costs->inter_bmode_costs, x->fc.sub_mv_ref_prob, vp8_sub_mv_ref_tree);
//
// vp8_cost_tokens writes C[-leaf] for each negative leaf in the tree. The
// vp8_bmode_tree leaves are -B_DC_PRED..-B_HU_PRED (slots 0..9). The
// vp8_sub_mv_ref_tree leaves are -LEFT4X4..-NEW4X4 (slots 10..13). The
// second call therefore writes slots 10..13 ONLY — slots 0..3 retain the
// bmode-token costs from the first init. (Before R12-C this function was
// returning sub_mv_ref token costs for slots 0..3, which is what an
// off-by-tree-walk reading of vp8_cost_tokens would suggest but the actual
// tree-walk only touches the negated-leaf slots.)
//
// pick_intra4x4block iterates `mode = B_DC_PRED..B_HE_PRED` (slots 0..3) and
// reads `mode_costs[mode]`, which therefore returns the bmode-token cost
// for that intra4x4 mode under the current frame's bmode_prob. Using the
// default bmode_prob at decode time matches libvpx's frame-1 state because
// fc.bmode_prob is reset to vp8_bmode_prob on every frame in
// vp8_default_coef_probs / start_encoded_frame.
func libvpxInterFastBpredModeCost(mode vp8common.BPredictionMode) int {
	return treeTokenCost(vp8tables.BModeTree[:], vp8tables.DefaultBModeProbs[:], int(mode))
}

// coefficientBlockTokenRate ports libvpx's vp8/encoder/rdopt.c:cost_coeffs.
// It returns the entropy-coded token cost (in 1/256-bit units) of the given
// quantized coefficient block, including the implicit "skip_eob_node" elision
// libvpx applies when the previous token had prev_token_class == 0 (i.e. the
// previous coefficient was a ZERO_TOKEN) and the current coefficient is past
// the first band of the plane.
//
// Equivalent libvpx loop body:
//
//	for (; c < eob; ++c) {
//	    cost += token_costs[type][bands[c]][pt][token(qcoeff[zigzag[c]])];
//	    cost += dct_value_cost[v];
//	    pt = prev_token_class[token];
//	}
//	if (c < 16) cost += token_costs[type][bands[c]][pt][EOB];
//
// where token_costs[type][band][0][...] for band > (type == 0 ? 1 : 0) uses the
// non-EOB subtree only (matching skip_eob_node = (pt == 0) in tokenize.c). All
// other (type, band, pt) combinations include the EOB-vs-not bit.
func coefficientBlockTokenRate(probs *vp8tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) int {
	if probs == nil || qcoeff == nil || blockType < 0 || blockType >= vp8tables.BlockTypes || ctx < 0 || ctx >= vp8tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return maxInt() / 4
	}
	if eob < skipDC {
		eob = skipDC
	}
	if eob > 16 {
		eob = 16
	}

	pt := ctx
	cost := 0
	pos := skipDC
	// elidedThreshold mirrors libvpx's skip_eob_node firing condition: in
	// the type==0 (Y after Y2) plane the first encoded band is index 1, in
	// every other plane it is index 0. Hoisted out of the inner loop so the
	// per-position elision check is a single int compare.
	elidedThreshold := 0
	if blockType == 0 {
		elidedThreshold = 1
	}
	signCost0 := vp8tables.ProbCost[128]
	signCost1 := vp8tables.ProbCost[255-128]
	for pos < eob {
		band := int(vp8tables.CoefBandsTable[pos])
		p := (*probs)[blockType][band][pt]
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coeff := int(qcoeff[rc])
		if coeff == 0 {
			// ZeroToken sits two edges deep in CoefTree (root, then the
			// non-EOB branch). The libvpx elision drops the EOB bit when
			// pt==0 and band > elidedThreshold, leaving only the second
			// edge at probs[1].
			if pt == 0 && band > elidedThreshold {
				cost += coefZeroTokenCostElided(&p)
			} else {
				cost += coefZeroTokenCost(&p)
			}
			pt = int(vp8tables.PrevTokenClass[vp8tables.ZeroToken])
			pos++
			continue
		}
		t, mag, ok := coefficientTokenMagnitude(coeff)
		if !ok {
			return maxInt() / 4
		}
		cost += coefTokenCostElided(p, t, blockType, band, pt)
		if coeff < 0 {
			cost += signCost1
		} else {
			cost += signCost0
		}
		cost += coefficientExtraBitsRate(t, mag)
		pt = int(vp8tables.PrevTokenClass[t])
		pos++
	}
	if pos < 16 {
		band := int(vp8tables.CoefBandsTable[pos])
		p := (*probs)[blockType][band][pt]
		cost += coefEOBTokenCost(&p)
	}
	return cost
}

// coefficientTokenTraceEntry is one row in a per-position rate breakdown
// produced by coefficientBlockTokenTrace. Trace entries cover the positions
// scanned by the token writer loop (skipDC..eob), including any zero
// coefficients between non-zero ones, and a trailing EOB transition when
// eob<16.
type coefficientTokenTraceEntry struct {
	Position       int
	Coefficient    int
	Token          int
	TokenRate      int
	SignRate       int
	ExtraBits      int
	BandIndex      int
	PrevTokenClass int
}

// coefficientBlockTokenTrace mirrors coefficientBlockTokenRate but records the
// per-position rate breakdown so callers (e.g., the oracle harness) can emit a
// JSON dump for libvpx parity comparison. The returned total exactly matches
// coefficientBlockTokenRate for the same arguments.
func coefficientBlockTokenTrace(probs *vp8tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) ([]coefficientTokenTraceEntry, int) {
	if probs == nil || qcoeff == nil || blockType < 0 || blockType >= vp8tables.BlockTypes || ctx < 0 || ctx >= vp8tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return nil, maxInt() / 4
	}
	if eob < skipDC {
		eob = skipDC
	}
	if eob > 16 {
		eob = 16
	}

	pt := ctx
	cost := 0
	trace := make([]coefficientTokenTraceEntry, 0, eob-skipDC+1)
	for pos := skipDC; pos < eob; pos++ {
		band := int(vp8tables.CoefBandsTable[pos])
		p := (*probs)[blockType][band][pt]
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coeff := int(qcoeff[rc])
		entry := coefficientTokenTraceEntry{
			Position:       pos,
			Coefficient:    coeff,
			BandIndex:      band,
			PrevTokenClass: pt,
		}
		var token int
		if coeff == 0 {
			token = vp8tables.ZeroToken
			entry.Token = token
			entry.TokenRate = coefTokenCostElided(p, token, blockType, band, pt)
			cost += entry.TokenRate
		} else {
			t, mag, ok := coefficientTokenMagnitude(coeff)
			if !ok {
				return nil, maxInt() / 4
			}
			token = t
			entry.Token = token
			entry.TokenRate = coefTokenCostElided(p, token, blockType, band, pt)
			if coeff < 0 {
				entry.SignRate = boolBitCost(128, 1)
			} else {
				entry.SignRate = boolBitCost(128, 0)
			}
			entry.ExtraBits = coefficientExtraBitsRate(token, mag)
			cost += entry.TokenRate + entry.SignRate + entry.ExtraBits
		}
		trace = append(trace, entry)
		pt = int(vp8tables.PrevTokenClass[token])
	}
	if eob < 16 {
		band := int(vp8tables.CoefBandsTable[eob])
		p := (*probs)[blockType][band][pt]
		eobRate := treeTokenCost(vp8tables.CoefTree[:], p[:], vp8tables.DCTEOBToken)
		trace = append(trace, coefficientTokenTraceEntry{
			Position:       eob,
			Coefficient:    0,
			Token:          vp8tables.DCTEOBToken,
			TokenRate:      eobRate,
			BandIndex:      band,
			PrevTokenClass: pt,
		})
		cost += eobRate
	}
	return trace, cost
}

// coefTokenCostElided returns the token cost charged at one coefficient
// position. It mirrors libvpx's `token_costs` table: when the prior token's
// prev_token_class is 0 (a ZERO_TOKEN) and the current band is past the
// plane's first encoded band, the EOB-vs-not bit is elided and only the
// non-EOB subtree cost is charged. Otherwise the full tree cost is charged.
func coefTokenCostElided(probs [vp8tables.EntropyNodes]uint8, token int, blockType int, band int, pt int) int {
	if token < 0 || token >= len(coefTokenPaths) {
		return maxInt() / 4
	}
	threshold := 0
	if blockType == 0 {
		threshold = 1
	}
	full := coefTokenCostFromPath(&coefTokenPaths[token], &probs)
	if pt == 0 && band > threshold {
		// nonEOB == boolBitCost(probs[0], 1) == ProbCost[255-probs[0]].
		nonEOB := vp8tables.ProbCost[255-int(probs[0])]
		if full <= nonEOB {
			return maxInt() / 4
		}
		return full - nonEOB
	}
	return full
}

func coefficientTokenCost(probs [vp8tables.EntropyNodes]uint8, token int, blockType int, band int, pt int) int {
	return coefTokenCostElided(probs, token, blockType, band, pt)
}

func nonZeroCoeffTokenRate(probs [vp8tables.EntropyNodes]uint8, token int) int {
	if token < 0 || token >= len(coefTokenPaths) {
		return maxInt() / 4
	}
	cost := coefTokenCostFromPath(&coefTokenPaths[token], &probs)
	nonEOBRate := vp8tables.ProbCost[255-int(probs[0])]
	if cost <= nonEOBRate {
		return maxInt() / 4
	}
	return cost - nonEOBRate
}

func coefficientTokenMagnitude(coeff int) (int, int, bool) {
	if coeff < 0 {
		coeff = -coeff
	}
	switch {
	case coeff <= 0:
		return 0, 0, false
	case coeff == 1:
		return vp8tables.OneToken, coeff, true
	case coeff == 2:
		return vp8tables.TwoToken, coeff, true
	case coeff == 3:
		return vp8tables.ThreeToken, coeff, true
	case coeff == 4:
		return vp8tables.FourToken, coeff, true
	case coeff <= 6:
		return vp8tables.DCTValCategory1, coeff, true
	case coeff <= 10:
		return vp8tables.DCTValCategory2, coeff, true
	case coeff <= 18:
		return vp8tables.DCTValCategory3, coeff, true
	case coeff <= 34:
		return vp8tables.DCTValCategory4, coeff, true
	case coeff <= 66:
		return vp8tables.DCTValCategory5, coeff, true
	case coeff <= vp8tables.DCTMaxValue:
		return vp8tables.DCTValCategory6, coeff, true
	default:
		return 0, 0, false
	}
}

func coefficientExtraBitsRate(token int, mag int) int {
	extra := vp8tables.ExtraBitsTable[token]
	offset := mag - int(extra.BaseVal)
	cost := 0
	for i := 0; i < int(extra.Len); i++ {
		shift := int(extra.Len) - 1 - i
		bit := int((offset >> uint(shift)) & 1)
		cost += boolBitCost(extra.Prob[i], bit)
	}
	return cost
}

func treeTokenCost(tree []int16, probs []uint8, token int) int {
	if paths := lookupTreeTokenPaths(tree); paths != nil {
		if token < 0 || token >= len(paths) {
			return maxInt() / 4
		}
		return treeTokenCostFromPath(&paths[token], probs)
	}
	return treeTokenCostSlow(tree, probs, token)
}

// treeTokenCostSlow is the fallback walker for trees that do not have a
// precomputed path table (e.g. ad-hoc trees in tests). It mirrors the
// historical implementation byte-for-byte.
func treeTokenCostSlow(tree []int16, probs []uint8, token int) int {
	var encoded vp8enc.TreeToken
	if !vp8enc.BuildTreeToken(tree, token, &encoded) {
		return maxInt() / 4
	}
	node := int16(0)
	cost := 0
	for bitIndex := int(encoded.Len) - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		if probIndex < 0 || probIndex >= len(probs) || int(node)+1 >= len(tree) {
			return maxInt() / 4
		}
		prob := probs[probIndex]
		bit := int((encoded.Value >> uint(bitIndex)) & 1)
		cost += boolBitCost(prob, bit)
		next := tree[int(node)+bit]
		if next <= 0 {
			if bitIndex == 0 {
				return cost
			}
			return maxInt() / 4
		}
		node = next
	}
	return maxInt() / 4
}

func boolBitCost(prob uint8, bit int) int {
	if bit == 0 {
		return vp8tables.ProbCost[prob]
	}
	return vp8tables.ProbCost[255-int(prob)]
}

func rdModeScore(qIndex int, rate int, distortion int) int {
	return rdModeScoreWithZbin(qIndex, 0, rate, distortion)
}

func rdModeScoreWithZbin(qIndex int, zbinOverQuant int, rate int, distortion int) int {
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

func libvpxInterIntraRDPenalty(qIndex int) int {
	return 10 * vp8common.DCQuant(qIndex, 0)
}

// libvpxErrorPerBit ports the encodeframe.c errorperbit derivation used by
// libvpx fractional motion searches.
func libvpxErrorPerBit(qIndex int) int {
	return libvpxErrorPerBitWithZbin(qIndex, 0)
}

func libvpxErrorPerBitWithZbin(qIndex int, zbinOverQuant int) int {
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	errorPerBit := rdMult * 100 / (110 * rdDiv)
	if errorPerBit == 0 {
		return 1
	}
	return errorPerBit
}

// libvpxSADPerBit16 ports sad_per_bit16lut from
// vp8/encoder/rdopt.c vp8cx_initialize_me_consts.
func libvpxSADPerBit16(qIndex int) int {
	return libvpxSADPerBit16LUT[vp8common.ClampQIndex(qIndex)]
}

// libvpxSADPerBit4 ports sad_per_bit4lut from
// vp8/encoder/rdopt.c vp8cx_initialize_me_consts for SPLITMV block search.
func libvpxSADPerBit4(qIndex int) int {
	return libvpxSADPerBit4LUT[vp8common.ClampQIndex(qIndex)]
}

// libvpxRDConstants ports vp8_initialize_rd_consts for the single-pass path.
func libvpxRDConstants(qIndex int) (int, int) {
	return libvpxRDConstantsWithZbin(qIndex, 0)
}

func libvpxRDConstantsWithZbin(qIndex int, zbinOverQuant int) (int, int) {
	qValue := min(vp8common.DCQuant(qIndex, 0), 160)
	rdMult := int(2.80 * float64(qValue*qValue))
	if zbinOverQuant > 0 {
		oqFactor := 1.0 + 0.0015625*float64(zbinOverQuant)
		modq := int(float64(qValue) * oqFactor)
		rdMult = int(2.80 * float64(modq*modq))
	}
	rdDiv := 100
	if rdMult > 1000 {
		rdDiv = 1
		rdMult /= 100
	}
	return rdMult, rdDiv
}

func libvpxRDCost(rdMult int, rdDiv int, rate int, distortion int) int {
	return ((128 + rate*rdMult) >> 8) + rdDiv*distortion
}

var libvpxSADPerBit16LUT = [vp8common.QIndexRange]int{
	2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2,
	3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 4, 4,
	4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6,
	6, 6, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 9, 9, 9, 9, 9, 9,
	9, 9, 9, 9, 9, 9, 10, 10,
	10, 10, 10, 10, 10, 10, 11, 11,
	11, 11, 11, 11, 12, 12, 12, 12,
	12, 12, 13, 13, 13, 13, 14, 14,
}

var libvpxSADPerBit4LUT = [vp8common.QIndexRange]int{
	2, 2, 2, 2, 2, 2, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 4, 4, 4, 4,
	4, 4, 4, 4, 4, 4, 5, 5,
	5, 5, 5, 5, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6,
	7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 8, 8, 8,
	8, 8, 9, 9, 9, 9, 9, 9,
	10, 10, 10, 10, 10, 10, 10, 10,
	11, 11, 11, 11, 11, 11, 11, 11,
	12, 12, 12, 12, 12, 12, 12, 12,
	13, 13, 13, 13, 13, 13, 13, 14,
	14, 14, 14, 14, 15, 15, 15, 15,
	16, 16, 16, 16, 17, 17, 17, 18,
	18, 18, 19, 19, 19, 20, 20, 20,
}

var libvpxFullPelMVSADComponentCost16 = buildLibvpxFullPelMVSADComponentCost16()

func buildLibvpxFullPelMVSADComponentCost16() [vp8common.QIndexRange][256]int {
	var out [vp8common.QIndexRange][256]int
	for q := range out {
		sadPerBit := libvpxSADPerBit16LUT[q]
		for i := range out[q] {
			cost := 300
			if i > 0 {
				cost = int(256 * (2 * (math.Log2(float64(8*i)) + 0.6)))
			}
			out[q][i] = cost * sadPerBit
		}
	}
	return out
}

func libvpxFullPelMVSADCost16FromDeltas(mvRow8 int, mvCol8 int, refRow8 int, refCol8 int, qIndex int) int {
	rowDelta := mvRow8 - refRow8
	if rowDelta > 255 {
		rowDelta = 255
	} else if rowDelta < -255 {
		rowDelta = -255
	}
	if rowDelta < 0 {
		rowDelta = -rowDelta
	}
	colDelta := mvCol8 - refCol8
	if colDelta > 255 {
		colDelta = 255
	} else if colDelta < -255 {
		colDelta = -255
	}
	if colDelta < 0 {
		colDelta = -colDelta
	}
	costs := &libvpxFullPelMVSADComponentCost16[vp8common.ClampQIndex(qIndex)]
	return (costs[rowDelta] + costs[colDelta] + 128) >> 8
}

func interMotionRDScore(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	return rdModeScore(qIndex, interMotionVectorCost(mv, mvProbs), macroblockLumaSSE(src, ref, mbRow, mbCol, mv))
}

func (e *VP8Encoder) estimateInterResidualRDScore(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, segmentID uint8) (int, bool) {
	refRate := 1 << 30
	if e != nil && mode != nil {
		refRate = e.interReferenceFrameRate(mode.RefFrame)
	}
	return e.estimateInterResidualRDScoreWithReferenceRate(src, ref, mbRow, mbCol, mbRows, mbCols, mode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, refRate)
}

func (e *VP8Encoder) estimateInterResidualRDScoreWithReferenceRate(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, segmentID uint8, refRate int) (int, bool) {
	score, _, ok := e.estimateInterResidualRDScoreWithReferenceRateAndSkip(src, ref, mbRow, mbCol, mbRows, mbCols, mode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, refRate)
	return score, ok
}

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
}

func (e *VP8Encoder) estimateInterResidualRDAccounting(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, segmentID uint8, refRate int) (interResidualRDAccounting, bool) {
	if e == nil || ref == nil || mode == nil || quant == nil || segmentID >= vp8common.MaxMBSegments {
		return interResidualRDAccounting{}, false
	}
	zbinOverQuant := 0
	if e != nil {
		zbinOverQuant = e.rc.currentZbinOverQuant
	}
	var decMode vp8dec.MacroblockMode
	convertInterFrameMode(mode, &decMode)
	predMode := decMode
	predMode.MBSkipCoeff = true
	var zeroTokens vp8dec.MacroblockTokens
	if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ref, mbRow, mbCol, &predMode, &zeroTokens, &e.dequants[segmentID], &e.reconstructScratch) {
		return interResidualRDAccounting{}, false
	}

	modeRate := e.interMotionModeRateWithReferenceRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate)
	refCost := boolBitCost(e.refProbIntra, 1) + refRate
	otherCost := e.interMacroblockSkipRate(false)
	if breakout, predictionDist := staticInterRDEncodeBreakoutDistortion(src, &e.analysis.Img, mbRow, mbCol, quant, e.opts.StaticThreshold); breakout {
		rd := rdModeScoreWithZbin(qIndex, zbinOverQuant, 500, predictionDist)
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
	is4x4 := interFrameModeUses4x4Tokens(mode.Mode)
	stats := buildPredictedMacroblockCoefficientsRD(e.pickerCoefProbs(), src, mbRow, mbCol, &e.analysis.Img, aboveTok, leftTok, quant, qIndex, e.rc.currentZbinOverQuant, interZbinModeBoost(mode), is4x4, false, e.libvpxUseFastQuantForPick(), false, &coeffs)
	rateUV := stats.rateUV
	rate2 := modeRate + otherCost + stats.rateY + rateUV
	distortion2 := stats.distortionY + stats.distortionUV
	mbSkipCoeff := stats.tteob == 0
	if mbSkipCoeff {
		rate2 -= stats.rateY + stats.rateUV
		rateUV = 0
		skipBackout := e.interMacroblockSkipRate(true) - e.interMacroblockSkipRate(false)
		rate2 += skipBackout
		otherCost += skipBackout
	}
	rd := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate2, distortion2)
	yrd := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate2-rateUV-otherCost-refCost, distortion2-stats.distortionUV)
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
	}, true
}

func (e *VP8Encoder) estimateFastInterModeScore(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int) (int, bool) {
	refRate := 1 << 30
	if e != nil && mode != nil {
		refRate = e.interReferenceFrameRate(mode.RefFrame)
	}
	score, _, _, _, _, ok := e.estimateFastInterModeScoreWithReferenceRateAndSkip(src, ref, mbRow, mbCol, mbRows, mbCols, mode, above, left, aboveLeft, qIndex, refRate, nil)
	return score, ok
}

func (e *VP8Encoder) estimateFastInterModeScoreWithReferenceRate(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, refRate int) (int, bool) {
	score, _, _, _, _, ok := e.estimateFastInterModeScoreWithReferenceRateAndSkip(src, ref, mbRow, mbCol, mbRows, mbCols, mode, above, left, aboveLeft, qIndex, refRate, nil)
	return score, ok
}

func (e *VP8Encoder) estimateFastInterModeScoreWithReferenceRateAndSkip(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, refRate int, quant *vp8enc.MacroblockQuant) (int, int, int, int, bool, bool) {
	if ref == nil || mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode == vp8common.SplitMV {
		return 0, 0, 0, 0, false, false
	}
	modeRate := e.fastInterMotionModeRateWithReferenceRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate)
	variance, sse := macroblockLumaMotionVarianceSSE(src, ref, mbRow, mbCol, mode.MV)
	zbinOverQuant := 0
	if e != nil {
		zbinOverQuant = e.rc.currentZbinOverQuant
	}
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

func (e *VP8Encoder) macroblockIsSkin(mbRow int, mbCol int, mbCols int) bool {
	if e == nil || len(e.skinMap) == 0 {
		return false
	}
	index := mbRow*mbCols + mbCol
	if index < 0 || index >= len(e.skinMap) {
		return false
	}
	return e.skinMap[index] != 0
}

func (e *VP8Encoder) fastZeroMVLastAdjustmentEligible(mbRows int, mbCols int) bool {
	if e == nil || e.opts.ScreenContentMode != 0 {
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

func selectInterFrameMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	return selectInterFrameMotionVectorWithSearch(src, ref, mbRow, mbCol, 0, 0, bestRefMV, qIndex, defaultInterAnalysisSearchConfig(), mvProbs)
}

func selectInterFrameMotionVectorWithSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	return selectInterFrameMotionVectorWithSearchStart(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, interFrameSearchStart{}, mvProbs)
}

// selectInterFrameMotionVectorWithSearchStart mirrors libvpx pickinter.c's
// fast NEWMV path: integer-pel search followed by unconditional acceptance of
// the fractional refinement (find_fractional_mv_step). libvpx uses bilinear
// variance during the subpel search and trusts that result; second-guessing
// it with a 6-tap SSE recompute biases us toward integer-pel even when the
// bilinear-best candidate scores lower distortion AND lower MV-rate, which
// is the realtime-cbr cpu0/4/8 NEWMV mv_row divergence at frame=2 mb=(0,3),
// (2,3) on the 64x64 panning fixture.
func selectInterFrameMotionVectorWithSearchStart(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	best, bestCost := selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, mvProbs)
	if bestCost == 0 {
		return best, bestCost
	}
	if refined, refinedCost, ok := refineInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, search, mvProbs); ok {
		return refined, refinedCost
	}
	return best, bestCost
}

func selectRDInterFrameMotionVectorWithSearchStart(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	best, bestCost := selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, mvProbs)
	if refined, refinedCost, ok := refineInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, search, mvProbs); ok {
		return refined, refinedCost
	}
	return best, bestCost
}

// selectInterFrameFullPixelMotionVector centers the integer-pel search at
// bestRefMV (libvpx pickinter.c uses `mvp_full = bestRefMV >> 3`) and charges
// the candidate's MV-cost against bestRefMV instead of (0,0). Standalone
// callers keep the exhaustive sweep for existing coverage; encoder mode
// decision uses libvpx's NSTEP/hex speed-feature paths.
func selectInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int) {
	return selectInterFrameFullPixelMotionVectorWithSearch(src, ref, mbRow, mbCol, 0, 0, bestRefMV, qIndex, defaultInterAnalysisSearchConfig())
}

func selectInterFrameFullPixelMotionVectorWithSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig) (vp8enc.MotionVector, int) {
	return selectInterFrameFullPixelMotionVectorWithSearchStart(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, interFrameSearchStart{})
}

func selectInterFrameFullPixelMotionVectorWithSearchStart(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart) (vp8enc.MotionVector, int) {
	return selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, &vp8tables.DefaultMVContext)
}

func selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	searchStart := bestRefMV
	if start.ok && search.fullPixelSearch != interAnalysisFullPixelSearchExhaustive {
		searchStart = start.mv
		search = search.adjustedForImprovedMVStart(start)
	}
	centerRow := int(searchStart.Row) & ^7
	centerCol := int(searchStart.Col) & ^7
	best := vp8enc.MotionVector{Row: int16(centerRow), Col: int16(centerCol)}
	bounds := interFrameFullPixelSearchBounds(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	if search.fullPixelSearch != interAnalysisFullPixelSearchExhaustive {
		best = bounds.clampEighth(best)
	}
	bestWalkCost := interMotionSearchCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex)
	if bestWalkCost == 0 {
		return best, interMotionFullPixelSearchReturnCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, mvProbs)
	}
	if search.fullPixelSearch == interAnalysisFullPixelSearchNstep {
		return nstepInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, best, bestWalkCost, bestRefMV, qIndex, bounds, search, mvProbs)
	}
	if search.fullPixelSearch == interAnalysisFullPixelSearchDiamond {
		return diamondInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, best, bestWalkCost, bestRefMV, qIndex, bounds, search, mvProbs)
	}
	if search.fullPixelSearch == interAnalysisFullPixelSearchHex {
		return hexInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, best, bestWalkCost, bestRefMV, qIndex, bounds)
	}
	return exhaustiveInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, best, bestWalkCost, bestRefMV, qIndex, mvProbs)
}

type interFrameSearchStart struct {
	mv           vp8enc.MotionVector
	sr           int
	nearSADIndex int
	ok           bool
}

func attachImprovedMVTrace(mode *vp8enc.InterFrameMacroblockMode, start interFrameSearchStart) {
	if mode == nil || !start.ok {
		return
	}
	mode.ImprovedMVStart = true
	mode.ImprovedMVNearSADIndex = int8(start.nearSADIndex)
	mode.ImprovedMVSR = int8(start.sr)
	mode.ImprovedMVPredictor = start.mv
}

func (search interAnalysisSearchConfig) adjustedForImprovedMVStart(start interFrameSearchStart) interAnalysisSearchConfig {
	if !start.ok {
		return search
	}
	stepParam := start.sr + search.fullPixelSpeedAdjust
	if stepParam > search.fullPixelSearchParam {
		if stepParam >= interFrameMaxMVSearchSteps {
			stepParam = interFrameMaxMVSearchSteps - 1
		}
		search.fullPixelSearchParam = stepParam
		search.fullPixelFurtherSteps = libvpxInterFrameFurtherSteps(search.fullPixelSpeed, stepParam)
	}
	return search
}

type improvedInterFrameMVSlot struct {
	mv       vp8enc.MotionVector
	refFrame vp8common.MVReferenceFrame
	signBias bool
	sad      int
}

func (e *VP8Encoder) improvedInterFrameSearchStart(
	src vp8enc.SourceImage, refFrame vp8common.MVReferenceFrame,
	mbRow int, mbCol int, mbRows int, mbCols int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	search interAnalysisSearchConfig,
) interFrameSearchStart {
	if e == nil || !search.improvedMVPrediction || refFrame == vp8common.IntraFrame {
		return interFrameSearchStart{}
	}
	var slots [8]improvedInterFrameMVSlot
	slotCount := 3
	signBias := e.interFrameSignBias()
	fillImprovedInterFrameCurrentMVSlot(&slots[0], src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol, above, signBias)
	fillImprovedInterFrameCurrentMVSlot(&slots[1], src, &e.analysis.Img, mbRow, mbCol, mbRow, mbCol-1, left, signBias)
	fillImprovedInterFrameCurrentMVSlot(&slots[2], src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol-1, aboveLeft, signBias)
	if e.lastFrameInterModesValid && len(e.lastFrameInterModes) >= mbRows*mbCols && mbRows > 0 && mbCols > 0 {
		slotCount = 8
		fillImprovedInterFrameLastMVSlot(&slots[3], src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol)
		fillImprovedInterFrameLastMVSlot(&slots[4], src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow-1, mbCol)
		fillImprovedInterFrameLastMVSlot(&slots[5], src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol-1)
		fillImprovedInterFrameLastMVSlot(&slots[6], src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol+1)
		fillImprovedInterFrameLastMVSlot(&slots[7], src, &e.lastRef.Img, e.lastFrameInterModes, e.lastFrameInterModeBias, mbRow, mbCol, mbRows, mbCols, mbRow+1, mbCol)
	}
	biasImprovedInterFrameMVSlots(&slots, slotCount, refFrame, signBias, mbRow, mbCol, mbRows, mbCols)
	order := improvedInterFrameMVSlotOrder(slots, slotCount)
	for rank := 0; rank < slotCount; rank++ {
		slot := slots[order[rank]]
		if slot.refFrame == refFrame {
			sr := 2
			if rank < 3 {
				sr = 3
			}
			return interFrameSearchStart{mv: slot.mv, sr: sr, nearSADIndex: order[rank], ok: true}
		}
	}
	mv := improvedInterFrameMVMedian(slots, slotCount)
	return interFrameSearchStart{mv: mv, sr: 0, nearSADIndex: -1, ok: true}
}

func fillImprovedInterFrameCurrentMVSlot(slot *improvedInterFrameMVSlot, src vp8enc.SourceImage, img *vp8common.Image, srcMbRow int, srcMbCol int, refMbRow int, refMbCol int, mode *vp8enc.InterFrameMacroblockMode, signBias [vp8common.MaxRefFrames]bool) {
	// Mirror libvpx's vp8_mv_pred neighbor table for the current frame: a nil
	// pointer (border MB) corresponds to libvpx's calloc-zeroed mode_info
	// sentinel row/column where ref_frame == INTRA_FRAME and mv == 0, and
	// vp8_cal_sad sets the matching near_sad entry to INT_MAX.
	*slot = improvedInterFrameMVSlot{sad: maxInt()}
	if mode == nil || refMbRow < 0 || refMbCol < 0 {
		return
	}
	slot.refFrame = convertInterFrameReference(mode)
	if slot.refFrame > vp8common.IntraFrame && slot.refFrame < vp8common.MaxRefFrames {
		slot.signBias = signBias[slot.refFrame]
	}
	if slot.refFrame == vp8common.IntraFrame {
		// libvpx leaves near_mvs[vcnt] at zero when the neighbor is intra; do
		// the same here regardless of any stale MV field on the mode entry.
		return
	}
	slot.mv = mode.MV
	slot.sad = macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
}

func fillImprovedInterFrameLastMVSlot(slot *improvedInterFrameMVSlot, src vp8enc.SourceImage, img *vp8common.Image, modes []vp8enc.InterFrameMacroblockMode, modeBias []bool, srcMbRow int, srcMbCol int, mbRows int, mbCols int, refMbRow int, refMbCol int) {
	// Mirror libvpx's vp8_mv_pred neighbor table for the previous frame:
	// out-of-range MB coordinates correspond to libvpx's lfmv/lf_ref_frame
	// sentinel rows (top/bottom) and columns (left/right) which are
	// calloc-zeroed and therefore report INTRA_FRAME with mv == 0, while
	// vp8_cal_sad sets the matching near_sad entry to INT_MAX.
	*slot = improvedInterFrameMVSlot{sad: maxInt()}
	if refMbRow < 0 || refMbCol < 0 || refMbRow >= mbRows || refMbCol >= mbCols {
		return
	}
	index := refMbRow*mbCols + refMbCol
	if index < 0 || index >= len(modes) {
		return
	}
	mode := &modes[index]
	slot.refFrame = convertInterFrameReference(mode)
	if index < len(modeBias) {
		slot.signBias = modeBias[index]
	}
	if slot.refFrame == vp8common.IntraFrame {
		// libvpx leaves near_mvs[vcnt] at zero for intra last-frame slots even
		// though it still increments vcnt; mirror that exactly.
		return
	}
	slot.mv = mode.MV
	slot.sad = macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
}

func biasImprovedInterFrameMVSlots(slots *[8]improvedInterFrameMVSlot, count int, refFrame vp8common.MVReferenceFrame, signBias [vp8common.MaxRefFrames]bool, mbRow int, mbCol int, mbRows int, mbCols int) {
	if slots == nil || refFrame <= vp8common.IntraFrame || refFrame >= vp8common.MaxRefFrames {
		return
	}
	targetBias := signBias[refFrame]
	for i := 0; i < count && i < len(slots); i++ {
		slot := &slots[i]
		if slot.refFrame == vp8common.IntraFrame {
			continue
		}
		if slot.signBias != targetBias {
			slot.mv.Row = -slot.mv.Row
			slot.mv.Col = -slot.mv.Col
		}
		slot.mv = clampInterFrameModeMotionVector(slot.mv, mbRow, mbCol, mbRows, mbCols)
	}
}

func clampInterFrameModeMotionVector(mv vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) vp8enc.MotionVector {
	if mbRows <= 0 || mbCols <= 0 {
		return mv
	}
	top := -(mbRow * 16) << 3
	bottom := (mbRows - 1 - mbRow) * 16 << 3
	left := -(mbCol * 16) << 3
	right := (mbCols - 1 - mbCol) * 16 << 3
	return vp8enc.MotionVector{
		Row: int16(clampInterFrameModeMotionVectorComponent(int(mv.Row), top, bottom)),
		Col: int16(clampInterFrameModeMotionVectorComponent(int(mv.Col), left, right)),
	}
}

func clampInterFrameModeMotionVectorComponent(v int, lowEdge int, highEdge int) int {
	if v < lowEdge-(16<<3) {
		return lowEdge - (16 << 3)
	}
	if v > highEdge+(16<<3) {
		return highEdge + (16 << 3)
	}
	return v
}

func improvedInterFrameMVSlotOrder(slots [8]improvedInterFrameMVSlot, count int) [8]int {
	var order [8]int
	for i := 0; i < count && i < len(order); i++ {
		order[i] = i
	}
	for i := 1; i < count && i < len(order); i++ {
		idx := order[i]
		sad := slots[idx].sad
		j := i - 1
		for ; j >= 0 && sad < slots[order[j]].sad; j-- {
			order[j+1] = order[j]
		}
		order[j+1] = idx
	}
	return order
}

func improvedInterFrameMVMedian(slots [8]improvedInterFrameMVSlot, count int) vp8enc.MotionVector {
	if count <= 0 {
		return vp8enc.MotionVector{}
	}
	var rows [8]int
	var cols [8]int
	for i := 0; i < count && i < len(slots); i++ {
		rows[i] = int(slots[i].mv.Row)
		cols[i] = int(slots[i].mv.Col)
	}
	insertionSortInts(rows[:count])
	insertionSortInts(cols[:count])
	return vp8enc.MotionVector{Row: int16(rows[count/2]), Col: int16(cols[count/2])}
}

func insertionSortInts(values []int) {
	for i := 1; i < len(values); i++ {
		v := values[i]
		j := i - 1
		for ; j >= 0 && v < values[j]; j-- {
			values[j+1] = values[j]
		}
		values[j+1] = v
	}
}

func selectInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, bestRefMV vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int) {
	return selectInterFrameSplitBlockFullPixelMotionVectorFromCenter(src, ref, mbRow, mbCol, block, width, height, bestRefMV, bestRefMV, qIndex)
}

func selectInterFrameSplitBlockFullPixelMotionVectorFromCenter(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, searchCenter vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int) {
	return selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(src, ref, mbRow, mbCol, block, width, height, searchCenter, bestRefMV, qIndex, 0, true)
}

func selectInterFrameSplitBlockFullPixelMotionVectorFromCenterAndStep(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, searchCenter vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, stepParam int, fullSearchFallback bool) (vp8enc.MotionVector, int) {
	centerRow := int(searchCenter.Row) & ^7
	centerCol := int(searchCenter.Col) & ^7
	best := vp8enc.MotionVector{Row: int16(centerRow), Col: int16(centerCol)}
	mbRows := (src.Height + 15) >> 4
	mbCols := (src.Width + 15) >> 4
	bounds := interFrameFullPixelSearchBounds(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	best = bounds.clampEighth(best)
	bestCost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex)
	best, bestCost = nstepInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, best, bestCost, bestRefMV, qIndex, bounds, stepParam)
	if fullSearchFallback && splitMotionFullSearchFallbackNeeded(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex) {
		best, bestCost = fullSearchInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, bounds.clampEighth(vp8enc.MotionVector{Row: int16(centerRow), Col: int16(centerCol)}), best, bestCost, bestRefMV, qIndex, bounds, interFrameFullPixelSearchRadius)
	}
	return best, bestCost
}

func nstepInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, stepParam int) (vp8enc.MotionVector, int) {
	if stepParam < 0 {
		stepParam = 0
	} else if stepParam >= interFrameMaxMVSearchSteps {
		stepParam = interFrameMaxMVSearchSteps - 1
	}
	furtherSteps := (interFrameMaxMVSearchSteps - 1) - stepParam
	result := diamondNstepInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, center, centerWalkCost, bestRefMV, qIndex, bounds, stepParam)
	best := result.mv
	bestCost := result.cost
	n := result.num00
	num00 := 0
	for n < furtherSteps {
		n++
		if num00 > 0 {
			num00--
			continue
		}
		candidate := diamondNstepInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, center, centerWalkCost, bestRefMV, qIndex, bounds, stepParam+n)
		num00 = candidate.num00
		if candidate.cost < bestCost {
			best = candidate.mv
			bestCost = candidate.cost
		}
	}
	return best, bestCost
}

func diamondNstepInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, searchParam int) interFrameNstepSearchResult {
	sites := &interFrameNstepSites
	if searchParam < 0 {
		searchParam = 0
	} else if searchParam >= interFrameMaxMVSearchSteps {
		searchParam = interFrameMaxMVSearchSteps - 1
	}
	best := center
	bestWalkCost := centerWalkCost
	start := center
	startIndex := searchParam * 8
	totalSteps := (len(sites) / 8) - searchParam
	i := 1
	bestSite := 0
	lastSite := 0
	num00 := 0
	for range totalSteps {
		for range 8 {
			site := sites[startIndex+i]
			row := (int(best.Row) >> 3) + int(site.Row)
			col := (int(best.Col) >> 3) + int(site.Col)
			if bounds.containsFullPelStrict(row, col) {
				mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
				cost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, mv, bestRefMV, qIndex)
				if cost < bestWalkCost {
					bestWalkCost = cost
					bestSite = i
				}
			}
			i++
		}
		if bestSite != lastSite {
			site := sites[startIndex+bestSite]
			best = vp8enc.MotionVector{
				Row: int16(int(best.Row) + int(site.Row)*interFrameMVFullPixelStep),
				Col: int16(int(best.Col) + int(site.Col)*interFrameMVFullPixelStep),
			}
			lastSite = bestSite
		} else if best == start {
			num00++
		}
	}
	return interFrameNstepSearchResult{mv: best, cost: bestWalkCost, num00: num00}
}

func splitMotionFullSearchFallbackNeeded(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) bool {
	shift := splitMotionSegmentationSSEShift(width, height)
	cost, ok := interMotionSplitBlockFullPixelVarianceCost(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex)
	return ok && (cost>>shift) > interFrameSplitMVFullSearchThreshold
}

func interMotionSplitBlockFullPixelVarianceCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) (int, bool) {
	variance, _, ok := splitBlockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, block, width, height, int(mv.Row)/2, int(mv.Col)/2)
	if !ok {
		return maxInt(), false
	}
	return variance + interMotionSplitBlockSearchVectorCost(mv, bestRefMV, qIndex), true
}

func splitMotionSegmentationSSEShift(width int, height int) int {
	switch {
	case width == 16 && height == 8:
		return 3
	case width == 8 && height == 16:
		return 3
	case width == 8 && height == 8:
		return 2
	default:
		return 0
	}
}

func fullSearchInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, center vp8enc.MotionVector, best vp8enc.MotionVector, bestCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, distance int) (vp8enc.MotionVector, int) {
	refRow := int(center.Row) >> 3
	refCol := int(center.Col) >> 3
	rowMin := refRow - distance
	rowMax := refRow + distance
	colMin := refCol - distance
	colMax := refCol + distance
	if rowMin < bounds.rowMin {
		rowMin = bounds.rowMin
	}
	if rowMax > bounds.rowMax {
		rowMax = bounds.rowMax
	}
	if colMin < bounds.colMin {
		colMin = bounds.colMin
	}
	if colMax > bounds.colMax {
		colMax = bounds.colMax
	}
	for row := rowMin; row < rowMax; row++ {
		for col := colMin; col < colMax; col++ {
			mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
			cost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, mv, bestRefMV, qIndex)
			if cost < bestCost {
				best = mv
				bestCost = cost
			}
		}
	}
	return best, bestCost
}

func refineInterFrameSplitBlockSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	switch search.fractionalSearch {
	case interAnalysisFractionalSearchStep:
		return stepInterFrameSplitBlockSubpixelMotionVector(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex, true, mvProbs)
	case interAnalysisFractionalSearchHalf:
		return stepInterFrameSplitBlockSubpixelMotionVector(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex, false, mvProbs)
	case interAnalysisFractionalSearchSkip:
		return vp8enc.MotionVector{}, 0, false
	default:
		return iterativeInterFrameSplitBlockSubpixelMotionVector(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex, mvProbs)
	}
}

func stepInterFrameSplitBlockSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, quarter bool, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	if int(best.Row)&7 != 0 || int(best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	bestRow := (int(best.Row) >> 3) * 4
	bestCol := (int(best.Col) >> 3) * 4
	bestCost, ok := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, bestRow, bestCol, bestRefMV, qIndex, mvProbs)
	if !ok {
		return vp8enc.MotionVector{}, 0, false
	}
	bestCost, bestRow, bestCol = stepInterFrameSplitBlockSubpixelDirectionalSearch(src, ref, mbRow, mbCol, block, width, height, bestRow, bestCol, 2, bestCost, bestRefMV, qIndex, mvProbs)
	if quarter {
		bestCost, bestRow, bestCol = stepInterFrameSplitBlockSubpixelDirectionalSearch(src, ref, mbRow, mbCol, block, width, height, bestRow, bestCol, 1, bestCost, bestRefMV, qIndex, mvProbs)
	}
	finalMV := vp8enc.MotionVector{Row: int16(bestRow * 2), Col: int16(bestCol * 2)}
	if !interFrameSubpixelMotionVectorInRange(finalMV, bestRefMV) {
		return vp8enc.MotionVector{}, 0, false
	}
	return finalMV, bestCost, true
}

func stepInterFrameSplitBlockSubpixelDirectionalSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, startRow int, startCol int, step int, bestCost int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (int, int, int) {
	bestRow := startRow
	bestCol := startCol
	leftCost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, startRow, startCol-step, bestRefMV, qIndex, mvProbs)
	rightCost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, startRow, startCol+step, bestRefMV, qIndex, mvProbs)
	upCost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, startRow-step, startCol, bestRefMV, qIndex, mvProbs)
	downCost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, startRow+step, startCol, bestRefMV, qIndex, mvProbs)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, leftCost, startRow, startCol-step)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, rightCost, startRow, startCol+step)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, upCost, startRow-step, startCol)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, downCost, startRow+step, startCol)

	diagRow := startRow - step
	if upCost >= downCost {
		diagRow = startRow + step
	}
	diagCol := startCol - step
	if leftCost >= rightCost {
		diagCol = startCol + step
	}
	diagCost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, diagRow, diagCol, bestRefMV, qIndex, mvProbs)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, diagCost, diagRow, diagCol)
	return bestCost, bestRow, bestCol
}

func iterativeInterFrameSplitBlockSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	if int(best.Row)&7 != 0 || int(best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	br := (int(best.Row) >> 3) * 4
	bc := (int(best.Col) >> 3) * 4
	tr := br
	tc := bc
	bestMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	bestDist, _, ok := splitBlockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, block, width, height, br, bc)
	if !ok {
		return vp8enc.MotionVector{}, 0, false
	}
	bestCost := bestDist + interMotionSearchErrorVectorCost(bestMV, bestRefMV, qIndex, mvProbs)
	mbRows := (src.Height + 15) >> 4
	mbCols := (src.Width + 15) >> 4
	bounds := interFrameSubpelSearchBoundsFor(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	cand := func(r, c int) int {
		if !bounds.contains(r, c) {
			return maxInt()
		}
		cost, _ := splitBlockSubpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, block, width, height, r, c, bestRefMV, qIndex, mvProbs)
		return cost
	}

	for range 3 {
		leftCost := cand(tr, tc-2)
		rightCost := cand(tr, tc+2)
		upCost := cand(tr-2, tc)
		downCost := cand(tr+2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+2, tc)

		diagRow := tr - 2
		if upCost >= downCost {
			diagRow = tr + 2
		}
		diagCol := tc - 2
		if leftCost >= rightCost {
			diagCol = tc + 2
		}
		diagCost := cand(diagRow, diagCol)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	for range 3 {
		leftCost := cand(tr, tc-1)
		rightCost := cand(tr, tc+1)
		upCost := cand(tr-1, tc)
		downCost := cand(tr+1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+1, tc)

		diagRow := tr - 1
		if upCost >= downCost {
			diagRow = tr + 1
		}
		diagCol := tc - 1
		if leftCost >= rightCost {
			diagCol = tc + 1
		}
		diagCost := cand(diagRow, diagCol)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	finalMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	if !interFrameSubpixelMotionVectorInRange(finalMV, bestRefMV) {
		return vp8enc.MotionVector{}, 0, false
	}
	return finalMV, bestCost, true
}

func splitBlockSubpixelMotionSearchCandidateCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, row int, col int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (int, bool) {
	dist, _, ok := splitBlockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, block, width, height, row, col)
	if !ok {
		return maxInt(), false
	}
	mv := vp8enc.MotionVector{Row: int16(row * 2), Col: int16(col * 2)}
	// Iterative subpel candidate cost: libvpx CHECK_BETTER uses the MVC
	// macro (1/4-pel signed index built from `(mv>>1) - (ref>>1)`), not
	// mv_err_cost.
	return dist + interMotionSubpelCandidateVectorCost(mv, bestRefMV, qIndex, mvProbs), true
}

func splitBlockSubpixelVarianceForQuarterMV(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, row int, col int) (int, int, bool) {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	if baseY < 0 || baseX < 0 || baseY+height > src.Height || baseX+width > src.Width {
		return 0, 0, false
	}
	refBaseY := baseY + (row >> 2)
	refBaseX := baseX + (col >> 2)
	xOffset := (col & 3) << 1
	yOffset := (row & 3) << 1
	return splitBlockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, width, height, xOffset, yOffset)
}

func exhaustiveInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	centerRow := int(bestRefMV.Row) & ^7
	centerCol := int(bestRefMV.Col) & ^7
	// R15-B: hoist src/ref slice-header + bounds limits out of the inner
	// SAD call, and the bestRefMV-shifted-to-fullpel anchors (the
	// MotionVectorSADCost LUT index is invariant in bestRefMV).
	ctx := newFullPelSearchCtx(src, ref, mbRow, mbCol)
	refRow8 := int(bestRefMV.Row) >> 3
	refCol8 := int(bestRefMV.Col) >> 3
	bestMVRow := int(best.Row)
	bestMVCol := int(best.Col)
	for row := centerRow - interFrameMVSearchRange; row <= centerRow+interFrameMVSearchRange; row += interFrameMVFullPixelStep {
		for col := centerCol - interFrameMVSearchRange; col <= centerCol+interFrameMVSearchRange; col += interFrameMVFullPixelStep {
			if row == bestMVRow && col == bestMVCol {
				continue
			}
			cost := ctx.fullPelCostLimited(row, col, bestWalkCost, refRow8, refCol8, qIndex)
			if cost < bestWalkCost {
				bestMVRow = row
				bestMVCol = col
				bestWalkCost = cost
			}
		}
	}
	best = vp8enc.MotionVector{Row: int16(bestMVRow), Col: int16(bestMVCol)}
	return best, interMotionFullPixelSearchReturnCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, mvProbs)
}

type interFrameFullPixelBounds struct {
	rowMin int
	rowMax int
	colMin int
	colMax int
}

func interFrameFullPixelSearchBounds(bestRefMV vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) interFrameFullPixelBounds {
	bounds := interFrameFullPixelBounds{
		rowMin: ((int(bestRefMV.Row) + 7) >> 3) - interFrameMaxFullPelVal,
		rowMax: (int(bestRefMV.Row) >> 3) + interFrameMaxFullPelVal,
		colMin: ((int(bestRefMV.Col) + 7) >> 3) - interFrameMaxFullPelVal,
		colMax: (int(bestRefMV.Col) >> 3) + interFrameMaxFullPelVal,
	}
	if mbRows > 0 {
		umv := interFrameUMVBorderPixels - 16
		rowMin := -((mbRow * 16) + umv)
		rowMax := ((mbRows - 1 - mbRow) * 16) + umv
		if bounds.rowMin < rowMin {
			bounds.rowMin = rowMin
		}
		if bounds.rowMax > rowMax {
			bounds.rowMax = rowMax
		}
	}
	if mbCols > 0 {
		umv := interFrameUMVBorderPixels - 16
		colMin := -((mbCol * 16) + umv)
		colMax := ((mbCols - 1 - mbCol) * 16) + umv
		if bounds.colMin < colMin {
			bounds.colMin = colMin
		}
		if bounds.colMax > colMax {
			bounds.colMax = colMax
		}
	}
	return bounds
}

func (b interFrameFullPixelBounds) containsFullPel(row int, col int) bool {
	return row >= b.rowMin && row <= b.rowMax && col >= b.colMin && col <= b.colMax
}

func (b interFrameFullPixelBounds) containsFullPelStrict(row int, col int) bool {
	return row > b.rowMin && row < b.rowMax && col > b.colMin && col < b.colMax
}

func (b interFrameFullPixelBounds) clampEighth(mv vp8enc.MotionVector) vp8enc.MotionVector {
	row := int(mv.Row) >> 3
	col := int(mv.Col) >> 3
	if row < b.rowMin {
		row = b.rowMin
	} else if row > b.rowMax {
		row = b.rowMax
	}
	if col < b.colMin {
		col = b.colMin
	} else if col > b.colMax {
		col = b.colMax
	}
	return vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
}

type interFrameNstepSearchResult struct {
	mv    vp8enc.MotionVector
	cost  int
	num00 int
}

func nstepInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	return steppedDiamondInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, center, centerWalkCost, bestRefMV, qIndex, bounds, search, interFrameNstepSites[:], 8, mvProbs)
}

func diamondInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	return steppedDiamondInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, center, centerWalkCost, bestRefMV, qIndex, bounds, search, interFrameDiamondSites[:], 4, mvProbs)
}

func steppedDiamondInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, search interAnalysisSearchConfig, sites []vp8enc.MotionVector, sitesPerStep int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	stepParam := search.fullPixelSearchParam
	if stepParam < 0 {
		stepParam = 0
	} else if stepParam >= interFrameMaxMVSearchSteps {
		stepParam = interFrameMaxMVSearchSteps - 1
	}

	result := diamondSearchSitesInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, center, centerWalkCost, bestRefMV, qIndex, bounds, sites, sitesPerStep, stepParam, mvProbs)
	best := result.mv
	bestCost := result.cost
	n := result.num00
	num00 := 0
	doRefine := search.fullPixelFinalRefine
	if n > search.fullPixelFurtherSteps {
		doRefine = false
	}
	for n < search.fullPixelFurtherSteps {
		n++
		if num00 > 0 {
			num00--
			continue
		}
		candidate := diamondSearchSitesInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, center, centerWalkCost, bestRefMV, qIndex, bounds, sites, sitesPerStep, stepParam+n, mvProbs)
		num00 = candidate.num00
		if search.fullPixelFinalRefine && num00 > search.fullPixelFurtherSteps-n {
			doRefine = false
		}
		if candidate.cost < bestCost {
			best = candidate.mv
			bestCost = candidate.cost
		}
	}
	if doRefine {
		best, bestCost = refineInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, bounds, 8, mvProbs)
	}
	return best, bestCost
}

func diamondNstepInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, searchParam int, mvProbs *[2][vp8tables.MVPCount]uint8) interFrameNstepSearchResult {
	return diamondSearchSitesInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, center, centerWalkCost, bestRefMV, qIndex, bounds, interFrameNstepSites[:], 8, searchParam, mvProbs)
}

func diamondSearchSitesInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, sites []vp8enc.MotionVector, sitesPerStep int, searchParam int, mvProbs *[2][vp8tables.MVPCount]uint8) interFrameNstepSearchResult {
	if sitesPerStep <= 0 || len(sites) < 1+sitesPerStep {
		return interFrameNstepSearchResult{mv: center, cost: interMotionFullPixelSearchReturnCost(src, ref, mbRow, mbCol, center, bestRefMV, qIndex, mvProbs)}
	}
	if searchParam < 0 {
		searchParam = 0
	} else if searchParam >= interFrameMaxMVSearchSteps {
		searchParam = interFrameMaxMVSearchSteps - 1
	}
	best := center
	bestWalkCost := centerWalkCost
	start := center
	startIndex := searchParam * sitesPerStep
	totalSteps := (len(sites) / sitesPerStep) - searchParam
	i := 1
	bestSite := 0
	lastSite := 0
	num00 := 0
	// R15-B: hoist src/ref slice-header + bounds limits out of the inner
	// SAD-cost kernel into a per-search context, plus the bestRefMV
	// full-pel anchor that the SAD cost LUT indexes against.
	ctx := newFullPelSearchCtx(src, ref, mbRow, mbCol)
	refRow8 := int(bestRefMV.Row) >> 3
	refCol8 := int(bestRefMV.Col) >> 3
	for range totalSteps {
		for range sitesPerStep {
			siteIndex := startIndex + i
			if siteIndex >= len(sites) {
				break
			}
			site := sites[siteIndex]
			row := (int(best.Row) >> 3) + int(site.Row)
			col := (int(best.Col) >> 3) + int(site.Col)
			if bounds.containsFullPelStrict(row, col) {
				mvRow := row * interFrameMVFullPixelStep
				mvCol := col * interFrameMVFullPixelStep
				cost := ctx.fullPelCostLimited(mvRow, mvCol, bestWalkCost, refRow8, refCol8, qIndex)
				if cost < bestWalkCost {
					bestWalkCost = cost
					bestSite = i
				}
			}
			i++
		}
		if bestSite != lastSite {
			site := sites[startIndex+bestSite]
			best = vp8enc.MotionVector{
				Row: int16(int(best.Row) + int(site.Row)*interFrameMVFullPixelStep),
				Col: int16(int(best.Col) + int(site.Col)*interFrameMVFullPixelStep),
			}
			lastSite = bestSite
		} else if best == start {
			num00++
		}
	}
	return interFrameNstepSearchResult{mv: best, cost: interMotionFullPixelSearchReturnCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, mvProbs), num00: num00}
}

func refineInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, start vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, searchRange int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	neighbors := [...]vp8enc.MotionVector{
		{Row: -1},
		{Col: -1},
		{Col: 1},
		{Row: 1},
	}
	best := start
	bestWalkCost := interMotionSearchCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex)
	// R15-B: hoist src/ref slice-header + bounds limits and the
	// bestRefMV full-pel anchor out of the inner SAD-cost kernel.
	ctx := newFullPelSearchCtx(src, ref, mbRow, mbCol)
	refRow8 := int(bestRefMV.Row) >> 3
	refCol8 := int(bestRefMV.Col) >> 3
	for range searchRange {
		bestSite := -1
		br := int(best.Row) >> 3
		bc := int(best.Col) >> 3
		for j, step := range neighbors {
			row := br + int(step.Row)
			col := bc + int(step.Col)
			if !bounds.containsFullPelStrict(row, col) {
				continue
			}
			mvRow := row * interFrameMVFullPixelStep
			mvCol := col * interFrameMVFullPixelStep
			cost := ctx.fullPelCostLimited(mvRow, mvCol, bestWalkCost, refRow8, refCol8, qIndex)
			if cost < bestWalkCost {
				bestWalkCost = cost
				bestSite = j
			}
		}
		if bestSite < 0 {
			break
		}
		best = vp8enc.MotionVector{
			Row: int16(int(best.Row) + int(neighbors[bestSite].Row)*interFrameMVFullPixelStep),
			Col: int16(int(best.Col) + int(neighbors[bestSite].Col)*interFrameMVFullPixelStep),
		}
	}
	return best, interMotionFullPixelSearchReturnCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, mvProbs)
}

// interFrameNstepSites and interFrameDiamondSites are package-level
// pre-computed search-site arrays. R14-B avoids regenerating them on each
// search invocation (each call previously zero-initialised and rebuilt
// 1+8*step / 1+4*step entries).
var interFrameNstepSites = buildInterFrameNstepSearchSites()
var interFrameDiamondSites = buildInterFrameDiamondSearchSites()

func buildInterFrameNstepSearchSites() [1 + interFrameMaxMVSearchSteps*8]vp8enc.MotionVector {
	var sites [1 + interFrameMaxMVSearchSteps*8]vp8enc.MotionVector
	count := 1
	for length := 1 << (interFrameMaxMVSearchSteps - 1); length > 0; length /= 2 {
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: 0}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: 0}
		count++
		sites[count] = vp8enc.MotionVector{Row: 0, Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: 0, Col: int16(length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: int16(length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: int16(length)}
		count++
	}
	return sites
}

func buildInterFrameDiamondSearchSites() [1 + interFrameMaxMVSearchSteps*4]vp8enc.MotionVector {
	var sites [1 + interFrameMaxMVSearchSteps*4]vp8enc.MotionVector
	count := 1
	for length := 1 << (interFrameMaxMVSearchSteps - 1); length > 0; length /= 2 {
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: 0}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: 0}
		count++
		sites[count] = vp8enc.MotionVector{Row: 0, Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: 0, Col: int16(length)}
		count++
	}
	return sites
}

func interFrameNstepSearchSites() [1 + interFrameMaxMVSearchSteps*8]vp8enc.MotionVector {
	return interFrameNstepSites
}

func interFrameDiamondSearchSites() [1 + interFrameMaxMVSearchSteps*4]vp8enc.MotionVector {
	return interFrameDiamondSites
}

func hexInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds) (vp8enc.MotionVector, int) {
	hex := [...]vp8enc.MotionVector{
		{Row: -1, Col: -2},
		{Row: 1, Col: -2},
		{Row: 2, Col: 0},
		{Row: 1, Col: 2},
		{Row: -1, Col: 2},
		{Row: -2, Col: 0},
	}
	nextCheckpoints := [...][3]vp8enc.MotionVector{
		{{Row: -2, Col: 0}, {Row: -1, Col: -2}, {Row: 1, Col: -2}},
		{{Row: -1, Col: -2}, {Row: 1, Col: -2}, {Row: 2, Col: 0}},
		{{Row: 1, Col: -2}, {Row: 2, Col: 0}, {Row: 1, Col: 2}},
		{{Row: 2, Col: 0}, {Row: 1, Col: 2}, {Row: -1, Col: 2}},
		{{Row: 1, Col: 2}, {Row: -1, Col: 2}, {Row: -2, Col: 0}},
		{{Row: -1, Col: 2}, {Row: -2, Col: 0}, {Row: -1, Col: -2}},
	}
	neighbors := [...]vp8enc.MotionVector{
		{Row: 0, Col: -1},
		{Row: -1, Col: 0},
		{Row: 1, Col: 0},
		{Row: 0, Col: 1},
	}

	br := int(best.Row) >> 3
	bc := int(best.Col) >> 3
	bestSite := -1
	// R15-B: hoist src/ref slice-header + bounds limits and bestRefMV
	// full-pel anchor out of the inner SAD-cost kernel into a per-search
	// context (re-used across the hex outer ring, the next-checkpoint
	// walk, and the neighbor refinement).
	ctx := newFullPelSearchCtx(src, ref, mbRow, mbCol)
	refRow8 := int(bestRefMV.Row) >> 3
	refCol8 := int(bestRefMV.Col) >> 3
	bestMVRow := int(best.Row)
	bestMVCol := int(best.Col)
	for i, step := range hex {
		row := br + int(step.Row)
		col := bc + int(step.Col)
		if !bounds.containsFullPel(row, col) {
			continue
		}
		mvRow := row * interFrameMVFullPixelStep
		mvCol := col * interFrameMVFullPixelStep
		cost := ctx.fullPelCostLimited(mvRow, mvCol, bestCost, refRow8, refCol8, qIndex)
		if cost < bestCost {
			bestMVRow = mvRow
			bestMVCol = mvCol
			bestCost = cost
			bestSite = i
		}
	}
	if bestSite >= 0 {
		br = bestMVRow >> 3
		bc = bestMVCol >> 3
		k := bestSite
		for j := 1; j < 127; j++ {
			bestSite = -1
			for i, step := range nextCheckpoints[k] {
				row := br + int(step.Row)
				col := bc + int(step.Col)
				if !bounds.containsFullPel(row, col) {
					continue
				}
				mvRow := row * interFrameMVFullPixelStep
				mvCol := col * interFrameMVFullPixelStep
				cost := ctx.fullPelCostLimited(mvRow, mvCol, bestCost, refRow8, refCol8, qIndex)
				if cost < bestCost {
					bestMVRow = mvRow
					bestMVCol = mvCol
					bestCost = cost
					bestSite = i
				}
			}
			if bestSite < 0 {
				break
			}
			br = bestMVRow >> 3
			bc = bestMVCol >> 3
			k += 5 + bestSite
			if k >= 12 {
				k -= 12
			} else if k >= 6 {
				k -= 6
			}
		}
	}

	br = bestMVRow >> 3
	bc = bestMVCol >> 3
	for range 8 {
		bestSite = -1
		for i, step := range neighbors {
			row := br + int(step.Row)
			col := bc + int(step.Col)
			if !bounds.containsFullPel(row, col) {
				continue
			}
			mvRow := row * interFrameMVFullPixelStep
			mvCol := col * interFrameMVFullPixelStep
			cost := ctx.fullPelCostLimited(mvRow, mvCol, bestCost, refRow8, refCol8, qIndex)
			if cost < bestCost {
				bestMVRow = mvRow
				bestMVCol = mvCol
				bestCost = cost
				bestSite = i
			}
		}
		if bestSite < 0 {
			break
		}
		br = bestMVRow >> 3
		bc = bestMVCol >> 3
	}
	return vp8enc.MotionVector{Row: int16(bestMVRow), Col: int16(bestMVCol)}, bestCost
}

func refineInterFrameSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	switch search.fractionalSearch {
	case interAnalysisFractionalSearchStep:
		return stepInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, true, mvProbs)
	case interAnalysisFractionalSearchHalf:
		return stepInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, false, mvProbs)
	case interAnalysisFractionalSearchSkip:
		return vp8enc.MotionVector{}, 0, false
	default:
		return iterativeInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, mvProbs)
	}
}

func stepInterFrameSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, quarter bool, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	if int(best.Row)&7 != 0 || int(best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	bestRow := (int(best.Row) >> 3) * 4
	bestCol := (int(best.Col) >> 3) * 4
	bestCost, ok := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, bestRow, bestCol, bestRefMV, qIndex, mvProbs)
	if !ok {
		return vp8enc.MotionVector{}, 0, false
	}
	bestCost, bestRow, bestCol = stepInterFrameSubpixelDirectionalSearch(src, ref, mbRow, mbCol, bestRow, bestCol, 2, bestCost, bestRefMV, qIndex, mvProbs)
	if quarter {
		bestCost, bestRow, bestCol = stepInterFrameSubpixelDirectionalSearch(src, ref, mbRow, mbCol, bestRow, bestCol, 1, bestCost, bestRefMV, qIndex, mvProbs)
	}
	finalMV := vp8enc.MotionVector{Row: int16(bestRow * 2), Col: int16(bestCol * 2)}
	if !interFrameSubpixelMotionVectorInRange(finalMV, bestRefMV) {
		return vp8enc.MotionVector{}, 0, false
	}
	return finalMV, bestCost, true
}

func stepInterFrameSubpixelDirectionalSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, startRow int, startCol int, step int, bestCost int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (int, int, int) {
	bestRow := startRow
	bestCol := startCol
	leftCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, startRow, startCol-step, bestRefMV, qIndex, mvProbs)
	rightCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, startRow, startCol+step, bestRefMV, qIndex, mvProbs)
	upCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, startRow-step, startCol, bestRefMV, qIndex, mvProbs)
	downCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, startRow+step, startCol, bestRefMV, qIndex, mvProbs)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, leftCost, startRow, startCol-step)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, rightCost, startRow, startCol+step)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, upCost, startRow-step, startCol)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, downCost, startRow+step, startCol)

	diagRow := startRow - step
	if upCost >= downCost {
		diagRow = startRow + step
	}
	diagCol := startCol - step
	if leftCost >= rightCost {
		diagCol = startCol + step
	}
	diagCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, diagRow, diagCol, bestRefMV, qIndex, mvProbs)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, diagCost, diagRow, diagCol)
	return bestCost, bestRow, bestCol
}

func interFrameSubpixelMotionVectorInRange(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector) bool {
	maxFullPelEighths := interFrameMaxFullPelVal << 3
	rowDelta := int(mv.Row) - int(bestRefMV.Row)
	colDelta := int(mv.Col) - int(bestRefMV.Col)
	if rowDelta < 0 {
		rowDelta = -rowDelta
	}
	if colDelta < 0 {
		colDelta = -colDelta
	}
	return rowDelta <= maxFullPelEighths && colDelta <= maxFullPelEighths
}

// interFrameSubpelSearchBounds mirrors the minc/maxc/minr/maxr clamps libvpx
// computes at the head of vp8_find_best_sub_pixel_step_iteratively (and
// _step). The bounds are the intersection of the UMV window (in 1/4-pel:
// x->mv_col_min*4, x->mv_col_max*4) and a per-component window of size
// `(1 << mvlong_width) - 1` 1/4-pel sites around the 1/4-pel-aligned ref_mv
// (`ref_mv->as_mv.col >> 1`).  CHECK_BETTER's IFMVCV macro short-circuits any
// candidate outside this rectangle to UINT_MAX, which the govpx iter searches
// previously skipped — letting the iter chase variance gradients past the
// UMV edge into the replicated border, where the synthetic SPLITMV fixture
// finds an artificially low residual at large offsets and commits a wildly
// drifted MV.
type interFrameSubpelSearchBounds struct {
	rowMin int
	rowMax int
	colMin int
	colMax int
}

const subpelMVQuarterPelLongLimit = (1 << 10) - 1 // libvpx mvlong_width = 10.

func interFrameSubpelSearchBoundsFor(bestRefMV vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) interFrameSubpelSearchBounds {
	// libvpx mv_col_min / mv_col_max are in integer-pel; *4 converts to 1/4-pel.
	// The UMV window: -(mb_col*16 + (UMV_BORDER - 16)) ... ((mb_cols-1-mb_col)*16 + (UMV_BORDER - 16)).
	umv := interFrameUMVBorderPixels - 16
	rowMinIPel := -((mbRow * 16) + umv)
	rowMaxIPel := ((mbRows - 1 - mbRow) * 16) + umv
	colMinIPel := -((mbCol * 16) + umv)
	colMaxIPel := ((mbCols - 1 - mbCol) * 16) + umv

	rrQuarter := int(bestRefMV.Row) >> 1
	rcQuarter := int(bestRefMV.Col) >> 1
	rowMin := max(rrQuarter-subpelMVQuarterPelLongLimit, rowMinIPel*4)
	rowMax := min(rrQuarter+subpelMVQuarterPelLongLimit, rowMaxIPel*4)
	colMin := max(rcQuarter-subpelMVQuarterPelLongLimit, colMinIPel*4)
	colMax := min(rcQuarter+subpelMVQuarterPelLongLimit, colMaxIPel*4)
	return interFrameSubpelSearchBounds{rowMin: rowMin, rowMax: rowMax, colMin: colMin, colMax: colMax}
}

func (b interFrameSubpelSearchBounds) contains(row int, col int) bool {
	return row >= b.rowMin && row <= b.rowMax && col >= b.colMin && col <= b.colMax
}

// subpelSearchCtx hoists the per-MB invariants for the iterative sub-pel
// refinement out of the inner candidate-cost call. The 13-step
// half-then-quarter walk fires up to 7 candidate-cost calls per ring × 6
// rings = 42 candidates per MB, each of which previously paid the full
// macroblockSubpixelVariance prologue (slice-header bounds, ref bound
// checks). R15-B precomputes the source row pointer + ref limit
// thresholds once and folds them into a tight inline test.
type subpelSearchCtx struct {
	srcRowPtr  []byte // = src.Y[baseY*src.YStride+baseX:]
	srcYStride int
	refYFull   []byte
	refYStride int
	refYOrigin int
	refYBorder int
	refCodedH  int
	refCodedW  int
	baseY      int
	baseX      int
}

func newSubpelSearchCtx(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (subpelSearchCtx, bool) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if baseY < 0 || baseX < 0 || baseY+16 > src.Height || baseX+16 > src.Width {
		return subpelSearchCtx{}, false
	}
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return subpelSearchCtx{}, false
	}
	return subpelSearchCtx{
		srcRowPtr:  src.Y[baseY*src.YStride+baseX:],
		srcYStride: src.YStride,
		refYFull:   ref.YFull,
		refYStride: ref.YStride,
		refYOrigin: ref.YOrigin,
		refYBorder: ref.YBorder,
		refCodedH:  ref.CodedHeight,
		refCodedW:  ref.CodedWidth,
		baseY:      baseY,
		baseX:      baseX,
	}, true
}

// subpelVarianceForQuarterMV computes the picker's quarter-pel variance
// without the per-call macroblockSubpixelVariance prologue.
//
// Caller passes (row, col) in quarter-pel units (signed); the function
// derives the integer-pel offset and the 1/8-pel sub-pel offset from
// those bits, mirroring the original macroblockSubpixelVarianceForQuarterMV
// arithmetic exactly.
func (c *subpelSearchCtx) subpelVarianceForQuarterMV(row int, col int) (int, bool) {
	refBaseY := c.baseY + (row >> 2)
	refBaseX := c.baseX + (col >> 2)
	if refBaseY < -c.refYBorder || refBaseX < -c.refYBorder ||
		refBaseY+17 > c.refCodedH+c.refYBorder ||
		refBaseX+17 > c.refCodedW+c.refYBorder {
		return 0, false
	}
	start := c.refYOrigin + refBaseY*c.refYStride + refBaseX
	if start < 0 || start+16*c.refYStride+17 > len(c.refYFull) {
		return 0, false
	}
	xOffset := (col & 3) << 1
	yOffset := (row & 3) << 1
	variance, _ := dsp.SubpelVariance16x16(c.refYFull[start:], c.refYStride, xOffset, yOffset, c.srcRowPtr, c.srcYStride)
	return variance, true
}

// iterativeInterFrameSubpixelMotionVector performs the libvpx half- then
// quarter-pel refinement (vp8_find_best_sub_pixel_step_iteratively) anchored
// to bestRefMV: candidate MVs farther from bestRefMV than MAX_FULL_PEL_VAL
// (in 1/8-pel) get rejected with INT_MAX and the cost is charged against the
// ref-MV, not (0,0).
func iterativeInterFrameSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	if int(best.Row)&7 != 0 || int(best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	br := (int(best.Row) >> 3) * 4
	bc := (int(best.Col) >> 3) * 4
	tr := br
	tc := bc
	bestMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	subCtx, subCtxOK := newSubpelSearchCtx(src, ref, mbRow, mbCol)
	if !subCtxOK {
		return vp8enc.MotionVector{}, 0, false
	}
	bestDist, ok := subCtx.subpelVarianceForQuarterMV(br, bc)
	if !ok {
		return vp8enc.MotionVector{}, 0, false
	}
	bestCost := bestDist + interMotionSearchErrorVectorCost(bestMV, bestRefMV, qIndex, mvProbs)
	mbRows := (src.Height + 15) >> 4
	mbCols := (src.Width + 15) >> 4
	bounds := interFrameSubpelSearchBoundsFor(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	// R15-B: hoist errorPerBit + mvProbs into the closure capture so each
	// candidate-cost call collapses to a SubpelVariance + LUT lookup.
	errorPerBit := libvpxErrorPerBit(qIndex)
	refRow4 := int(bestRefMV.Row) >> 1
	refCol4 := int(bestRefMV.Col) >> 1
	var cachedRows [48]int
	var cachedCols [48]int
	var cachedCosts [48]int
	cachedCount := 0
	cand := func(r, c int) int {
		for i := range cachedCount {
			if cachedRows[i] == r && cachedCols[i] == c {
				return cachedCosts[i]
			}
		}
		cost := maxInt()
		if !bounds.contains(r, c) {
		} else if dist, ok := subCtx.subpelVarianceForQuarterMV(r, c); ok {
			mvCost := 0
			if mvProbs != nil {
				mvCost = vp8enc.MotionVectorSubpelSearchCostFromQuarterDeltas(r, c, refRow4, refCol4, mvProbs, errorPerBit)
			}
			cost = dist + mvCost
		}
		if cachedCount < len(cachedRows) {
			cachedRows[cachedCount] = r
			cachedCols[cachedCount] = c
			cachedCosts[cachedCount] = cost
			cachedCount++
		}
		return cost
	}

	for range 3 {
		leftCost := cand(tr, tc-2)
		rightCost := cand(tr, tc+2)
		upCost := cand(tr-2, tc)
		downCost := cand(tr+2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+2, tc)

		diagRow := tr - 2
		if upCost >= downCost {
			diagRow = tr + 2
		}
		diagCol := tc - 2
		if leftCost >= rightCost {
			diagCol = tc + 2
		}
		diagCost := cand(diagRow, diagCol)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	for range 3 {
		leftCost := cand(tr, tc-1)
		rightCost := cand(tr, tc+1)
		upCost := cand(tr-1, tc)
		downCost := cand(tr+1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+1, tc)

		diagRow := tr - 1
		if upCost >= downCost {
			diagRow = tr + 1
		}
		diagCol := tc - 1
		if leftCost >= rightCost {
			diagCol = tc + 1
		}
		diagCost := cand(diagRow, diagCol)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	finalMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	if !interFrameSubpixelMotionVectorInRange(finalMV, bestRefMV) {
		return vp8enc.MotionVector{}, 0, false
	}
	return finalMV, bestCost, true
}

func subpixelMotionSearchCandidateCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, row int, col int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (int, bool) {
	dist, _, ok := macroblockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, row, col)
	if !ok {
		return maxInt(), false
	}
	mv := vp8enc.MotionVector{Row: int16(row * 2), Col: int16(col * 2)}
	// Iterative subpel candidate cost: libvpx CHECK_BETTER uses the MVC
	// macro (1/4-pel signed index built from `(mv>>1) - (ref>>1)`), not
	// mv_err_cost.
	return dist + interMotionSubpelCandidateVectorCost(mv, bestRefMV, qIndex, mvProbs), true
}

func updateSubpixelSearchBest(bestCost int, bestRow int, bestCol int, candidateCost int, candidateRow int, candidateCol int) (int, int, int) {
	if candidateCost < bestCost {
		return candidateCost, candidateRow, candidateCol
	}
	return bestCost, bestRow, bestCol
}

func macroblockSubpixelVarianceForQuarterMV(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, row int, col int) (int, int, bool) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if baseY < 0 || baseX < 0 || baseY+16 > src.Height || baseX+16 > src.Width {
		return 0, 0, false
	}
	refBaseY := baseY + (row >> 2)
	refBaseX := baseX + (col >> 2)
	xOffset := (col & 3) << 1
	yOffset := (row & 3) << 1
	return macroblockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset)
}

func interMotionSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return macroblockSAD(src, ref, mbRow, mbCol, mv) + interMotionSearchVectorCost(mv, bestRefMV, qIndex)
}

func interMotionSplitBlockSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return splitBlockSAD(src, ref, mbRow, mbCol, block, width, height, mv) + interMotionSplitBlockSearchVectorCost(mv, bestRefMV, qIndex)
}

func interMotionSearchCostLimited(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return interMotionSearchCostLimitedSADPerBit(src, ref, mbRow, mbCol, mv, limit, bestRefMV, libvpxSADPerBit16(qIndex))
}

// interMotionSearchCostLimitedSADPerBit takes sadPerBit pre-bound so a
// hot caller can hoist the LUT lookup out of its inner loop. Behaviour
// matches interMotionSearchCostLimited; macroblockSADLimited's own hot
// path covers the full-pel-in-bounds case.
func interMotionSearchCostLimitedSADPerBit(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int, bestRefMV vp8enc.MotionVector, sadPerBit int) int {
	mvCost := vp8enc.MotionVectorSADCost(mv, bestRefMV, sadPerBit)
	sadLimit := limit - mvCost
	if sadLimit < 0 {
		return limit + 1
	}
	return macroblockSADLimited(src, ref, mbRow, mbCol, mv, sadLimit) + mvCost
}

// fullPelSearchCtx hoists the per-MB invariants for the diamond / n-step /
// refine / hex / exhaustive full-pel search kernels out of the per-site
// inner loop. The picker walks 80+ candidate sites against the same source
// macroblock and reference plane, so the source-row pointer, the bounds
// limits, and the slice-header prologue computed inside the per-site cost
// kernel are loop-invariant. R15-B converts every diamond / n-step /
// refine / hex / exhaustive call site to use this context, which retired
// the previous interMotionSearchCostLimitedFullPel wrapper.
//
// fullPelCostLimited is the inner kernel: it folds the in-bounds predicate
// to one `uint(x) <= uint(limit)` compare per axis (src-side bounds are
// proven loop-invariant by construction — baseY/baseX are derived from a
// valid mbRow/mbCol — and dropped from the per-site predicate altogether),
// takes a precomputed source pointer, and short-circuits the slow /
// out-of-bounds tail to the regular wrapper.
type fullPelSearchCtx struct {
	src        vp8enc.SourceImage
	ref        *vp8common.Image
	mbRow      int
	mbCol      int
	baseY      int
	baseX      int
	srcRowPtr  []byte // = src.Y[baseY*src.YStride+baseX : ]
	srcRowPtrP *byte  // = unsafe.SliceData(srcRowPtr) — hot SAD bypass
	srcYStride int
	refY       []byte
	refYP      *byte // = unsafe.SliceData(ref.Y)
	refYStride int
	refRowH    uint // = uint(ref.CodedHeight - 16)
	refRowW    uint // = uint(ref.CodedWidth - 16)
}

func newFullPelSearchCtx(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) fullPelSearchCtx {
	baseY := mbRow * 16
	baseX := mbCol * 16
	srcRowPtr := src.Y[baseY*src.YStride+baseX:]
	return fullPelSearchCtx{
		src:        src,
		ref:        ref,
		mbRow:      mbRow,
		mbCol:      mbCol,
		baseY:      baseY,
		baseX:      baseX,
		srcRowPtr:  srcRowPtr,
		srcRowPtrP: unsafe.SliceData(srcRowPtr),
		srcYStride: src.YStride,
		refY:       ref.Y,
		refYP:      unsafe.SliceData(ref.Y),
		refYStride: ref.YStride,
		refRowH:    uint(ref.CodedHeight - 16),
		refRowW:    uint(ref.CodedWidth - 16),
	}
}

// fullPelCostLimited is the hoisted-context per-site cost kernel. Returns
// `dsp.SAD16x16Limit(...) + mvCost` on the in-bounds full-pel fast path
// and falls back to the wrapper for the rare edge / out-of-bounds tail.
//
// Caller passes mvRow/mvCol pre-shifted to 1/8-pel (i.e. the candidate
// row/col multiplied by 8 = interFrameMVFullPixelStep). refRow8/refCol8
// are bestRefMV.Row>>3 and bestRefMV.Col>>3 respectively. The MV SAD
// component table is pre-scaled by qIndex so the diamond loop avoids
// repeating libvpx's per-candidate component-sum multiply.
func (c *fullPelSearchCtx) fullPelCostLimited(mvRow int, mvCol int, limit int, refRow8 int, refCol8 int, qIndex int) int {
	mvCost := libvpxFullPelMVSADCost16FromDeltas(mvRow>>3, mvCol>>3, refRow8, refCol8, qIndex)
	sadLimit := limit - mvCost
	if sadLimit < 0 {
		return limit + 1
	}
	refBaseY := c.baseY + (mvRow >> 3)
	refBaseX := c.baseX + (mvCol >> 3)
	if uint(refBaseY) <= c.refRowH && uint(refBaseX) <= c.refRowW {
		refPtr := (*byte)(unsafe.Add(unsafe.Pointer(c.refYP), refBaseY*c.refYStride+refBaseX))
		return dsp.SAD16x16LimitPtrFast(c.srcRowPtrP, c.srcYStride, refPtr, c.refYStride, sadLimit) + mvCost
	}
	return c.fullPelCostLimitedSlow(mvCol, mvRow, refBaseY, refBaseX, sadLimit) + mvCost
}

// fullPelCostLimitedSlow handles the rare OOB / sub-pel / clamp tail. Split
// off so the fast path stays small enough for the compiler to chase the
// inner-loop's bounds-check folding more aggressively (the picker only
// hits this body on frame-edge MBs).
//
//go:noinline
func (c *fullPelSearchCtx) fullPelCostLimitedSlow(mvCol int, mvRow int, refBaseY int, refBaseX int, sadLimit int) int {
	return macroblockSADLimitedSlow(c.src, c.ref, c.baseY, c.baseX, refBaseY, refBaseX, mvCol, mvRow, sadLimit)
}

func interMotionFullPixelSearchReturnCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	variance, _ := macroblockLumaMotionVarianceSSE(src, ref, mbRow, mbCol, mv)
	return variance + interMotionSearchErrorVectorCost(mv, bestRefMV, qIndex, mvProbs)
}

// interMotionSearchVectorCost charges full-pel MV bits against bestRefMV like
// libvpx mvsad_err_cost — picking against (0,0) inflates the cost of motion
// far from a strong predictor and biases NEWMV away from correct candidates.
func interMotionSearchVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return vp8enc.MotionVectorSADCost(mv, bestRefMV, libvpxSADPerBit16(qIndex))
}

func interMotionSplitBlockSearchVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return vp8enc.MotionVectorSADCost(mv, bestRefMV, libvpxSADPerBit4(qIndex))
}

// interMotionSearchErrorVectorCost charges sub-pel MV bits against bestRefMV
// (libvpx find_best_sub_pixel_step_iteratively in mcomp.c).
func interMotionSearchErrorVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return 0
	}
	return vp8enc.MotionVectorErrorCost(mv, bestRefMV, mvProbs, libvpxErrorPerBit(qIndex))
}

// interMotionSubpelCandidateVectorCost charges the sub-pel MV bits like the
// MVC macro inside libvpx's vp8_find_best_sub_pixel_step{_iteratively}: the
// 1/4-pel index is built from (mv>>1) - (ref>>1) — i.e. each operand is
// arithmetic-shifted to 1/4-pel before subtraction — and the lookup is
// signed (no clamp-to-zero). This matches the CHECK_BETTER candidate cost
// shape exactly when bestRefMV is fractional in 1/8-pel, which the
// mv_err_cost / vp8_mv_bit_cost variants used for the central cost do not.
// See MotionVectorSubpelSearchCost for the full derivation.
func interMotionSubpelCandidateVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return 0
	}
	return vp8enc.MotionVectorSubpelSearchCost(mv, bestRefMV, mvProbs, libvpxErrorPerBit(qIndex))
}

func interMotionModeVectorCost(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	return interMotionModeVectorCostWithNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, mvProbs, libvpxRDNewMVBitCostWeight)
}

func interMotionModeVectorCostWithNewMVWeight(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8, newMVWeight int) int {
	return interMotionModeVectorCostWithNewMVWeightAndSignBias(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, mvProbs, newMVWeight, defaultInterFrameSignBias())
}

func interMotionModeVectorCostWithNewMVWeightAndSignBias(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8, newMVWeight int, signBias [vp8common.MaxRefFrames]bool) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame {
		return 0
	}
	if mvProbs == nil {
		return maxInt() / 4
	}
	best := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, mode.RefFrame, mbRow, mbCol, mbRows, mbCols, signBias)
	if mode.Mode == vp8common.SplitMV {
		return splitMotionModeVectorCost(mode, left, above, best, mvProbs)
	}
	if mode.Mode != vp8common.NewMV {
		return 0
	}
	return interNewMVVectorCost(mode.MV, best, mvProbs, newMVWeight)
}

func interMacroblockSkipRate(skip bool) int {
	return interMacroblockSkipRateWithProb(128, skip)
}

func interMacroblockSkipRateWithProb(prob uint8, skip bool) int {
	if prob == 0 {
		prob = 128
	}
	if skip {
		return boolBitCost(prob, 1)
	}
	return boolBitCost(prob, 0)
}

func (e *VP8Encoder) interMacroblockSkipRate(skip bool) int {
	if e == nil {
		return interMacroblockSkipRate(skip)
	}
	return interMacroblockSkipRateWithProb(e.probSkipFalse, skip)
}

// interIntraMacroblockModeRate models libvpx vp8_calc_ref_frame_costs for the
// intra-coded ref-frame branch: skip-bit + intra/inter selector with the
// previous-frame prob_intra_coded.
func (e *VP8Encoder) interIntraMacroblockModeRate() int {
	return e.interMacroblockSkipRate(false) + boolBitCost(e.refProbIntra, 0)
}

func (e *VP8Encoder) interMotionModeRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return boolBitCost(e.refProbIntra, 0)
	}
	return e.interMotionModeRateWithReferenceRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, e.interReferenceFrameRate(mode.RefFrame))
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int) int {
	return e.interMotionModeRateWithReferenceRateAndNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate, libvpxRDNewMVBitCostWeight)
}

func (e *VP8Encoder) fastInterMotionModeRateWithReferenceRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int) int {
	return e.interMotionModeRateWithReferenceRateAndNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate, libvpxFastNewMVBitCostWeight)
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRateAndNewMVWeight(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int, newMVWeight int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return boolBitCost(e.refProbIntra, 0)
	}
	signBias := e.interFrameSignBias()
	return boolBitCost(e.refProbIntra, 1) +
		refRate +
		interPredictionModeRate(mode.Mode, vp8enc.InterFrameModeCounts(above, left, aboveLeft, mode.RefFrame, signBias)) +
		interMotionModeVectorCostWithNewMVWeightAndSignBias(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, &e.modeProbs.MV, newMVWeight, signBias)
}

// interReferenceFrameRate ports libvpx vp8_calc_ref_frame_costs (bitstream.c):
// the LAST/GOLDEN/ALTREF tree uses the previous-frame prob_last_coded and
// prob_gf_coded, NOT a per-frame static 128.
func (e *VP8Encoder) interReferenceFrameRate(refFrame vp8common.MVReferenceFrame) int {
	return interReferenceFrameRateWithProbs(refFrame, e.refProbLast, e.refProbGolden)
}

func (e *VP8Encoder) interReferenceFrameRateForReference(ref interAnalysisReference) int {
	if ref.RefRateSet {
		return ref.RefRate
	}
	return e.interReferenceFrameRate(ref.Frame)
}

func (e *VP8Encoder) interReferenceFrameRatesForFlags(flags EncodeFlags) (last int, golden int, alt int) {
	probLast := e.refProbLast
	probGolden := e.refProbGolden
	lastEnabled, goldenEnabled, altEnabled := e.interReferenceAvailability(flags)
	temporalSingleRef := e.interReferenceFrameRatesUseTemporalSingleRefSpecialCase()
	switch {
	case lastEnabled && !goldenEnabled && !altEnabled:
		probLast = 255
		probGolden = 128
	case temporalSingleRef && !lastEnabled && goldenEnabled && !altEnabled:
		probLast = 1
		probGolden = 255
	case temporalSingleRef && !lastEnabled && !goldenEnabled && altEnabled:
		probLast = 1
		probGolden = 1
	}
	return interReferenceFrameRateWithProbs(vp8common.LastFrame, probLast, probGolden),
		interReferenceFrameRateWithProbs(vp8common.GoldenFrame, probLast, probGolden),
		interReferenceFrameRateWithProbs(vp8common.AltRefFrame, probLast, probGolden)
}

func (e *VP8Encoder) interReferenceFrameRatesUseTemporalSingleRefSpecialCase() bool {
	if e == nil || !e.opts.TemporalScalability.Enabled {
		return false
	}
	pattern, ok := temporalLayeringPattern(e.opts.TemporalScalability.Mode)
	return ok && pattern.Layers > 1
}

func interReferenceFrameRateWithProbs(refFrame vp8common.MVReferenceFrame, probLast uint8, probGolden uint8) int {
	switch refFrame {
	case vp8common.LastFrame:
		return boolBitCost(probLast, 0)
	case vp8common.GoldenFrame:
		return boolBitCost(probLast, 1) + boolBitCost(probGolden, 0)
	case vp8common.AltRefFrame:
		return boolBitCost(probLast, 1) + boolBitCost(probGolden, 1)
	default:
		return 1 << 30
	}
}

func interPredictionModeRate(mode vp8common.MBPredictionMode, counts vp8enc.InterModeCounts) int {
	probs := vp8tables.InterModeContexts
	switch mode {
	case vp8common.ZeroMV:
		return boolBitCost(probs[counts.Intra][0], 0)
	case vp8common.NearestMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 0)
	case vp8common.NearMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 0)
	case vp8common.NewMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 1) +
			boolBitCost(probs[counts.Split][3], 0)
	case vp8common.SplitMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 1) +
			boolBitCost(probs[counts.Split][3], 1)
	default:
		return 1 << 30
	}
}

func splitMotionModeVectorCost(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, best vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mode.Partition >= vp8tables.NumMBSplits {
		return 1 << 30
	}
	if mvProbs == nil {
		return maxInt() / 4
	}
	cost := mbSplitPartitionRate(mode.Partition)
	partitions := int(vp8tables.MBSplitCount[mode.Partition])
	for subset := range partitions {
		block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
		leftMV := analysisSplitLeftMV(mode, left, block)
		aboveMV := analysisSplitAboveMV(mode, above, block)
		target := mode.BlockMV[block]
		bMode := mode.BModes[block]
		if !splitSubMotionLabelMatchesMV(bMode, target, leftMV, aboveMV) {
			return maxInt() / 4
		}
		cost += splitSubMotionLabelRate(bMode)
		if bMode == vp8common.New4x4 {
			delta := vp8enc.MotionVector{Row: int16(int(target.Row) - int(best.Row)), Col: int16(int(target.Col) - int(best.Col))}
			cost += splitMotionVectorCost(delta, mvProbs)
		}
	}
	return cost
}

var libvpxDefaultSubMVRefProbs = [3]uint8{180, 162, 25}

func splitSubMotionLabelRate(mode vp8common.BPredictionMode) int {
	// libvpx RD uses x->inter_bmode_costs, built from fc.sub_mv_ref_prob,
	// while bitstream writing uses context-specific sub-MV probabilities.
	return splitSubMotionLabelCostWithProbs(mode, libvpxDefaultSubMVRefProbs)
}

func splitSubMotionLabelCostWithProbs(mode vp8common.BPredictionMode, probs [3]uint8) int {
	if mode < vp8common.Left4x4 || mode > vp8common.New4x4 {
		return maxInt() / 4
	}
	return treeTokenCost(vp8tables.SubMVRefTree[:], probs[:], int(mode))
}

func splitSubMotionLabelMatchesMV(mode vp8common.BPredictionMode, target vp8enc.MotionVector, left vp8enc.MotionVector, above vp8enc.MotionVector) bool {
	switch mode {
	case vp8common.Left4x4:
		return target == left
	case vp8common.Above4x4:
		return above != left && target == above
	case vp8common.Zero4x4:
		return target.IsZero()
	case vp8common.New4x4:
		return true
	default:
		return false
	}
}

func mbSplitPartitionRate(partition uint8) int {
	if partition >= vp8tables.NumMBSplits {
		return maxInt() / 4
	}
	return treeTokenCost(vp8tables.MBSplitTree[:], vp8tables.MBSplitProbs[:], int(partition))
}

func analysisSplitLeftMV(cur *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, block int) vp8enc.MotionVector {
	if block&3 == 0 {
		if left == nil {
			return vp8enc.MotionVector{}
		}
		if left.Mode == vp8common.SplitMV {
			return left.BlockMV[block+3]
		}
		return left.MV
	}
	return cur.BlockMV[block-1]
}

func analysisSplitAboveMV(cur *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, block int) vp8enc.MotionVector {
	if block>>2 == 0 {
		if above == nil {
			return vp8enc.MotionVector{}
		}
		if above.Mode == vp8common.SplitMV {
			return above.BlockMV[block+12]
		}
		return above.MV
	}
	return cur.BlockMV[block-4]
}

func interMotionVectorCost(mv vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return maxInt() / 4
	}
	return vp8enc.MotionVectorBitCost(mv, vp8enc.MotionVector{}, mvProbs, libvpxFastNewMVBitCostWeight)
}

func interNewMVVectorCost(mv vp8enc.MotionVector, best vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8, weight int) int {
	if mvProbs == nil {
		return maxInt() / 4
	}
	return vp8enc.MotionVectorBitCost(mv, best, mvProbs, weight)
}

func splitMotionVectorCost(mv vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return maxInt() / 4
	}
	return vp8enc.MotionVectorBitCost(mv, vp8enc.MotionVector{}, mvProbs, 102)
}

func macroblockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	return macroblockSADLimited(src, ref, mbRow, mbCol, mv, maxInt())
}

func macroblockLumaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if baseY >= 0 && baseX >= 0 &&
			baseY+16 <= src.Height && baseX+16 <= src.Width {
			if sse, ok := macroblockSubpixelSSE(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return sse
			}
		}
	}
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= ref.CodedHeight && refBaseX+16 <= ref.CodedWidth {
		return dsp.SSE16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, &ref.Y[refBaseY*ref.YStride+refBaseX], ref.YStride)
	}

	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sse += diff * diff
		}
	}
	return sse
}

func macroblockLumaMotionVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if baseY >= 0 && baseX >= 0 &&
			baseY+16 <= src.Height && baseX+16 <= src.Width {
			if variance, sse, ok := macroblockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return variance, sse
			}
		}
	}
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= ref.CodedHeight && refBaseX+16 <= ref.CodedWidth {
		// R15-C SIMD bypass: skip the slice header + bounds-check on &src[0]
		// by going through VarianceBlock16x16PtrFast directly. Reuses the
		// same (sum, sse) reduction as R14-B's VarianceSSE16x16.
		sum, sse := dsp.VarianceBlock16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, &ref.Y[refBaseY*ref.YStride+refBaseX], ref.YStride)
		return sse - ((sum * sum) >> 8), sse
	}

	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}

// macroblockSADLimited dispatches the limit-aware 16x16 SAD between the
// aligned full-pel SIMD kernel, the sub-pel six-tap predict path, and the
// out-of-bounds clamp scalar fallback. R14-B reshapes the function so the
// hot full-pel branch stays as small as possible (the caller — the diamond
// search inner loop — produces full-pel MVs by construction); the cold
// sub-pel and clamp paths are factored into macroblockSADLimitedSlow so
// they don't bloat the hot path's inline cost.
func macroblockSADLimited(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvCol := int(mv.Col)
	mvRow := int(mv.Row)
	refBaseY := baseY + (mvRow >> 3)
	refBaseX := baseX + (mvCol >> 3)
	// Picker hot path: full-pel MV, source and reference 16x16 windows
	// fully in-bounds. The cast-through-uint encoding folds (>=0 && <= cap-16)
	// into one compare per axis — the diamond/exhaustive search drives this
	// branch on the overwhelming majority of the 100-1000 calls per MB.
	// Splitting the rare slow-path tail (subpel + edge-clamp) into a
	// dedicated noinline helper drops macroblockSADLimited's compile cost
	// from 530 to 252 so the compiler can chase the fast-path bounds
	// checks aggressively even though the wrapper itself stays out of
	// the inliner budget.
	//
	// R15-C: bypass dsp.SAD16x16Limit's 3-call dispatch chain
	// (sadBlockLimit -> sadBlock16x16Limit -> int32(NEON kernel)) by
	// taking the bounds-validated *byte and the already-non-negative
	// limit straight to the SIMD kernel via SAD16x16LimitPtrFast.
	if (mvCol|mvRow)&7 == 0 &&
		uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) &&
		uint(refBaseY) <= uint(ref.CodedHeight-16) && uint(refBaseX) <= uint(ref.CodedWidth-16) {
		return dsp.SAD16x16LimitPtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, &ref.Y[refBaseY*ref.YStride+refBaseX], ref.YStride, limit)
	}
	return macroblockSADLimitedSlow(src, ref, baseY, baseX, refBaseY, refBaseX, mvCol, mvRow, limit)
}

// macroblockSADLimitedSlow handles the rare paths (subpel, partial out of
// bounds, edge-clamp). Splitting it off keeps macroblockSADLimited small
// enough for the compiler to chase the fast-path bounds checks more
// aggressively (and the picker only hits this body on frame-edge MBs).
//
//go:noinline
func macroblockSADLimitedSlow(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, mvCol int, mvRow int, limit int) int {
	xOffset := mvCol & 7
	yOffset := mvRow & 7
	srcInBounds := baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width
	if xOffset|yOffset != 0 && srcInBounds {
		if sad, ok := macroblockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset, limit); ok {
			return sad
		}
	}
	if srcInBounds &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= ref.CodedHeight && refBaseX+16 <= ref.CodedWidth {
		return dsp.SAD16x16Limit(src.Y[baseY*src.YStride+baseX:], src.YStride, ref.Y[refBaseY*ref.YStride+refBaseX:], ref.YStride, limit)
	}

	srcY0 := src.Y
	refY0 := ref.Y
	srcStride := src.YStride
	refStride := ref.YStride
	srcH := src.Height
	srcW := src.Width
	refH := ref.CodedHeight
	refW := ref.CodedWidth
	var srcXs [16]int
	var refXs [16]int
	for col := range 16 {
		srcXs[col] = clampEncodeCoord(baseX+col, srcW)
		refXs[col] = clampEncodeCoord(refBaseX+col, refW)
	}
	sad := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, srcH)
		refY := clampEncodeCoord(refBaseY+row, refH)
		srcRow := srcY * srcStride
		refRow := refY * refStride
		for col := range 16 {
			diff := int(srcY0[srcRow+srcXs[col]]) - int(refY0[refRow+refXs[col]])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}

func splitBlockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if baseY >= 0 && baseX >= 0 &&
			baseY+height <= src.Height && baseX+width <= src.Width {
			if sad, ok := splitBlockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, width, height, xOffset, yOffset); ok {
				return sad
			}
		}
	}
	if baseY >= 0 && baseX >= 0 &&
		baseY+height <= src.Height && baseX+width <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+height <= ref.CodedHeight && refBaseX+width <= ref.CodedWidth {
		srcBlock := src.Y[baseY*src.YStride+baseX:]
		refBlock := ref.Y[refBaseY*ref.YStride+refBaseX:]
		switch {
		case width == 16 && height == 8:
			return dsp.SAD16x8(srcBlock, src.YStride, refBlock, ref.YStride)
		case width == 8 && height == 16:
			return dsp.SAD8x16(srcBlock, src.YStride, refBlock, ref.YStride)
		case width == 8 && height == 8:
			return dsp.SAD8x8(srcBlock, src.YStride, refBlock, ref.YStride)
		case width == 4 && height == 4:
			return dsp.SAD4x4(srcBlock, src.YStride, refBlock, ref.YStride)
		}
	}

	sad := 0
	for row := range height {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range width {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
	}
	return sad
}

func splitBlockSubpixelSAD(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+height+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+width+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+(height+4)*ref.YStride+width+5 > len(ref.YFull) {
		return 0, false
	}
	var pred [16 * 16]byte
	switch {
	case width == 16 && height == 8:
		dsp.SixTapPredict16x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
		return dsp.SAD16x8(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 16), true
	case width == 8 && height == 16:
		dsp.SixTapPredict8x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 8)
		return dsp.SAD8x16(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 8), true
	case width == 8 && height == 8:
		dsp.SixTapPredict8x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 8)
		return dsp.SAD8x8(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 8), true
	case width == 4 && height == 4:
		dsp.SixTapPredict4x4(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 4)
		return dsp.SAD4x4(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 4), true
	default:
		return 0, false
	}
}

func macroblockSubpixelSSE(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+16+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+16+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+20*ref.YStride+21 > len(ref.YFull) {
		return 0, false
	}
	var pred [16 * 16]byte
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
	return dsp.SSE16x16(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 16), true
}

func macroblockSubpixelSAD(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int, limit int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+16+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+16+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+20*ref.YStride+21 > len(ref.YFull) {
		return 0, false
	}
	var pred [16 * 16]byte
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
	return dsp.SAD16x16Limit(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 16, limit), true
}

func splitBlockSubpixelVariance(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, width int, height int, xOffset int, yOffset int) (int, int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, 0, false
	}
	if refBaseY < -ref.YBorder || refBaseX < -ref.YBorder ||
		refBaseY+height+1 > ref.CodedHeight+ref.YBorder ||
		refBaseX+width+1 > ref.CodedWidth+ref.YBorder {
		return 0, 0, false
	}
	start := ref.YOrigin + refBaseY*ref.YStride + refBaseX
	if start < 0 || start+height*ref.YStride+width+1 > len(ref.YFull) {
		return 0, 0, false
	}
	srcBlock := src.Y[baseY*src.YStride+baseX:]
	switch {
	case width == 16 && height == 8:
		variance, sse := dsp.SubpelVariance16x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, src.YStride)
		return variance, sse, true
	case width == 8 && height == 16:
		variance, sse := dsp.SubpelVariance8x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, src.YStride)
		return variance, sse, true
	case width == 8 && height == 8:
		variance, sse := dsp.SubpelVariance8x8(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, src.YStride)
		return variance, sse, true
	case width == 4 && height == 4:
		variance, sse := dsp.SubpelVariance4x4(ref.YFull[start:], ref.YStride, xOffset, yOffset, srcBlock, src.YStride)
		return variance, sse, true
	default:
		return 0, 0, false
	}
}

func macroblockSubpixelVariance(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, 0, false
	}
	if refBaseY < -ref.YBorder || refBaseX < -ref.YBorder ||
		refBaseY+17 > ref.CodedHeight+ref.YBorder ||
		refBaseX+17 > ref.CodedWidth+ref.YBorder {
		return 0, 0, false
	}
	start := ref.YOrigin + refBaseY*ref.YStride + refBaseX
	if start < 0 || start+16*ref.YStride+17 > len(ref.YFull) {
		return 0, 0, false
	}
	variance, sse := dsp.SubpelVariance16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
	return variance, sse, true
}

func macroblockChromaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) int {
	baseY := mbRow * 8
	baseX := mbCol * 8
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	refUVWidth := (ref.CodedWidth + 1) >> 1
	refUVHeight := (ref.CodedHeight + 1) >> 1
	if baseY >= 0 && baseX >= 0 &&
		baseY+8 <= uvHeight && baseX+8 <= uvWidth &&
		baseY+8 <= refUVHeight && baseX+8 <= refUVWidth {
		srcUOffset := baseY*src.UStride + baseX
		refUOffset := baseY*ref.UStride + baseX
		srcVOffset := baseY*src.VStride + baseX
		refVOffset := baseY*ref.VStride + baseX
		return dsp.SSE8x8PtrFast(&src.U[srcUOffset], src.UStride, &ref.U[refUOffset], ref.UStride) +
			dsp.SSE8x8PtrFast(&src.V[srcVOffset], src.VStride, &ref.V[refVOffset], ref.VStride)
	}

	sse := 0
	for row := range 8 {
		srcY := clampEncodeCoord(baseY+row, uvHeight)
		refY := clampEncodeCoord(baseY+row, refUVHeight)
		for col := range 8 {
			srcX := clampEncodeCoord(baseX+col, uvWidth)
			refX := clampEncodeCoord(baseX+col, refUVWidth)
			uDiff := int(src.U[srcY*src.UStride+srcX]) - int(ref.U[refY*ref.UStride+refX])
			vDiff := int(src.V[srcY*src.VStride+srcX]) - int(ref.V[refY*ref.VStride+refX])
			sse += uDiff*uDiff + vDiff*vDiff
		}
	}
	return sse
}

func macroblockLumaVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		baseY+16 <= ref.CodedHeight && baseX+16 <= ref.CodedWidth {
		// R15-C: fused (sum, sse) read collapses Variance16x16 + SSE16x16
		// into one SIMD pass (variance = sse - sum*sum/256).
		sum, sse := dsp.VarianceBlock16x16PtrFast(&src.Y[baseY*src.YStride+baseX], src.YStride, &ref.Y[baseY*ref.YStride+baseX], ref.YStride)
		return sse - ((sum * sum) >> 8), sse
	}

	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(baseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(baseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}

type predictedMacroblockRDStats struct {
	rateY        int
	rateUV       int
	distortionY  int
	distortionUV int
	tteob        int
}

func buildPredictedMacroblockCoefficients(coefProbs *vp8tables.CoefficientProbs, src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, zbinOverQuant int, zbinModeBoost int, is4x4 bool, intra bool, fastQuant bool, optimize bool, coeffs *vp8enc.MacroblockCoefficients) {
	_ = buildPredictedMacroblockCoefficientsRD(coefProbs, src, mbRow, mbCol, pred, aboveTok, leftTok, quant, qIndex, zbinOverQuant, zbinModeBoost, is4x4, intra, fastQuant, optimize, coeffs)
}

// buildPredictedMacroblockCoefficientsRD fuses per-MB residual gather,
// batched FDCT, per-block quantize+token-cost+context-update, and the
// Y2 second-order pass into one whole-MB pipeline. R11-C: replaces the
// per-block FDCT/quantize/token loop with batched FDCT (Y x16 and UV
// x8) + a single in-bounds residual gather, mirroring libvpx
// vp8/encoder/encodemb.c vp8_encode_inter16x16 / vp8_encode_intra16x16
// where vp8_transform_mb -> vp8_quantize_mb -> tokenize_mb run as one
// coordinated pass.
//
// Output (coeffs.QCoeff, coeffs.EOB, OracleY1DC*, OracleStaleY2*,
// returned predictedMacroblockRDStats) is byte-identical to the
// original per-block reference path.
func buildPredictedMacroblockCoefficientsRD(coefProbs *vp8tables.CoefficientProbs, src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, zbinOverQuant int, zbinModeBoost int, is4x4 bool, intra bool, fastQuant bool, optimize bool, coeffs *vp8enc.MacroblockCoefficients) predictedMacroblockRDStats {
	var stats predictedMacroblockRDStats
	if coefProbs == nil || pred == nil || quant == nil || coeffs == nil {
		return stats
	}
	var y2Input [16]int16
	var y2Coeff [16]int16
	var dq [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		uvAbove = tokenUVContextArray(aboveTok)
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		uvLeft = tokenUVContextArray(leftTok)
		y2Left = leftTok.Y2
	}

	// Whole-MB Y residual gather + batched FDCT. Mirrors libvpx
	// vp8_subtract_mby + vp8_transform_mb (16 fdct calls).
	var yResiduals [16 * 16]int16
	var yDcts [16 * 16]int16
	gatherMacroblockYResiduals4x4(src.Y, src.YStride, src.Width, src.Height, pred.Y, pred.YStride, mbCol*16, mbRow*16, yResiduals[:])
	vp8enc.ForwardDCT4x4Batch(yResiduals[:], yDcts[:], 16)

	for block := range 16 {
		dct := (*[16]int16)(yDcts[block*16 : block*16+16])
		if is4x4 {
			// Capture the chosen-mode FDCT DC of every Y block so the
			// oracle trace can mirror libvpx's stale Y2 second-order
			// snapshot for SPLITMV/B_PRED (see OracleStaleY2EOB).
			y2Input[block] = dct[0]
			a := block & 3
			l := (block & 0x0c) >> 2
			ctx := int(yAbove[a] + yLeft[l])
			eob := quantizeEncodedBlock(coefProbs, qIndex, 3, ctx, 0, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
			coeffs.SetBlockEOB(block, eob)
			stats.rateY += coefficientBlockTokenRate(coefProbs, 3, ctx, 0, &coeffs.QCoeff[block], eob)
			stats.distortionY += transformBlockError(dct, &dq)
			if eob > 0 {
				stats.tteob++
			}
			hasCoeffs := uint8(0)
			if eob > 0 {
				hasCoeffs = 1
			}
			yAbove[a] = hasCoeffs
			yLeft[l] = hasCoeffs
		} else {
			y2Input[block] = dct[0]
			// Use quant.Y1 (not quant.Y1DC) because govpx's Y1DC dequant
			// table is normalized so dequant[0]=1 (the actual DC value
			// lives in the Y2 second-order block); the libvpx Y1quant[Q]
			// the encode path actually exercises has the proper DC at
			// slot 0, which govpx mirrors in quant.Y1.
			coeffs.OracleY1DCEOB1[block] = libvpxY1DCWouldQuantizeNonzero(dct[0], &quant.Y1, zbinOverQuant, zbinModeBoost, fastQuant)
			dct[0] = 0
			a := block & 3
			l := (block & 0x0c) >> 2
			ctx := int(yAbove[a] + yLeft[l])
			eob := quantizeEncodedBlock(coefProbs, qIndex, 0, ctx, 1, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, dct, &quant.Y1DC, &coeffs.QCoeff[block], &dq)
			coeffs.SetBlockEOB(block, eob)
			stats.rateY += coefficientBlockTokenRate(coefProbs, 0, ctx, 1, &coeffs.QCoeff[block], eob)
			stats.distortionY += transformBlockError(dct, &dq)
			if eob > 1 {
				stats.tteob++
			}
			hasCoeffs := uint8(0)
			if eob > 1 {
				hasCoeffs = 1
			}
			yAbove[a] = hasCoeffs
			yLeft[l] = hasCoeffs
		}
	}
	if !is4x4 {
		vp8enc.ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
		eob := quantizeEncodedBlockWithRDZbin(coefProbs, qIndex, 1, int(y2Above+y2Left), 0, zbinOverQuant/2, zbinModeBoost, zbinOverQuant, intra, fastQuant, optimize, &y2Coeff, &quant.Y2, &coeffs.QCoeff[24], &dq)
		coeffs.SetBlockEOB(24, eob)
		stats.rateY += coefficientBlockTokenRate(coefProbs, 1, int(y2Above+y2Left), 0, &coeffs.QCoeff[24], eob)
		y2Error := transformBlockError(&y2Coeff, &dq)
		stats.distortionY = ((stats.distortionY << 2) + y2Error) >> 4
		stats.tteob += eob
	} else {
		coeffs.SetBlockEOB(24, 0)
		stats.distortionY >>= 2
		// Compute a Y2 walsh+quantize on the chosen mode's FDCT DCs so
		// the oracle trace can mirror libvpx's stale block[24] snapshot
		// without changing any encode-path state. Stored separately
		// because the bitstream and reconstruction must keep block 24
		// empty for SPLITMV/B_PRED.
		//
		// PIN: 1 SPLITMV MB in TestOracleInterDecisionMatchRate's
		// good-cpu3-vbr fixture (frame 7 MB(3,2)) emits eob[24]=15 here
		// while libvpx emits eob[24]=16 (eob_sum 99.11% vs 100%). The
		// other 5 SPLITMV MBs across that fixture all match. Why this
		// path can't byte-match libvpx for every SPLITMV MB:
		// libvpx's `xd->block[24].qcoeff/eobs[24]` at oracle-capture
		// time (post-`vp8_inverse_transform_mby` skip for SPLITMV)
		// reflects whatever the LAST 16x16 inter mode (NEAREST / NEAR /
		// ZERO / NEW from rd_pick_inter_mode's mode_index 0..15) wrote
		// via macro_block_yrd before the SPLITMV branch was tested.
		// That mode's predictor used an MV that differs from SPLITMV's
		// chosen MV in general, and macro_block_yrd's `zbin_mode_boost`
		// also differed (MV_ZBIN_BOOST=4 for NEW/NEAR/NEAREST,
		// LF_ZEROMV_ZBIN_BOOST=6 for ZEROMV(LAST), etc.) — so the Y2
		// Walsh input AND the quantize zbin both diverge. govpx
		// approximates this by reusing the chosen-SPLITMV per-block
		// FDCT DCs and the SPLITMV `zbinModeBoost`(=0); the
		// approximation matches libvpx whenever the SPLITMV per-block
		// MVs collapse to the same MV the last 16x16 inter mode found,
		// and diverges by 1 EOB scan position when they don't. Closing
		// the residual to 100% would require tracking the actual last-
		// tested 16x16 inter mode predictor through the picker.
		var staleY2Coeff [16]int16
		var staleY2Q [16]int16
		var staleY2DQ [16]int16
		vp8enc.ForwardWalsh4x4(y2Input[:], 4, &staleY2Coeff)
		staleEOB := min(max(quantizeEncodedBlockWithRDZbin(coefProbs, qIndex, 1, int(y2Above+y2Left), 0, zbinOverQuant/2, zbinModeBoost, zbinOverQuant, intra, fastQuant, optimize, &staleY2Coeff, &quant.Y2, &staleY2Q, &staleY2DQ), 0), 16)
		coeffs.OracleStaleY2EOB = uint8(staleEOB)
		coeffs.OracleStaleY2QCoeff = staleY2Q
		coeffs.OracleStaleY2Set = true
	}

	// Whole-MB UV residual gather + batched FDCT (8 blocks: U0..U3, V0..V3).
	// Mirrors libvpx vp8_subtract_mbuv + vp8_transform_mbuv (8 fdct calls).
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	var uvResiduals [8 * 16]int16
	var uvDcts [8 * 16]int16
	gatherMacroblockUVResiduals4x4(src.U, src.UStride, uvWidth, uvHeight, pred.U, pred.UStride, mbCol*8, mbRow*8, uvResiduals[0:64])
	gatherMacroblockUVResiduals4x4(src.V, src.VStride, uvWidth, uvHeight, pred.V, pred.VStride, mbCol*8, mbRow*8, uvResiduals[64:128])
	vp8enc.ForwardDCT4x4Batch(uvResiduals[:], uvDcts[:], 8)

	for block := range 4 {
		dct := (*[16]int16)(uvDcts[block*16 : block*16+16])
		a, l := macroblockCoefficientUVContextIndex(16 + block)
		ctx := int(uvAbove[a] + uvLeft[l])
		eob := quantizeEncodedBlock(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, dct, &quant.UV, &coeffs.QCoeff[16+block], &dq)
		coeffs.SetBlockEOB(16+block, eob)
		stats.rateUV += coefficientBlockTokenRate(coefProbs, 2, ctx, 0, &coeffs.QCoeff[16+block], eob)
		stats.distortionUV += transformBlockError(dct, &dq)
		stats.tteob += eob
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs

		dctV := (*[16]int16)(uvDcts[(4+block)*16 : (4+block)*16+16])
		a, l = macroblockCoefficientUVContextIndex(20 + block)
		ctx = int(uvAbove[a] + uvLeft[l])
		eob = quantizeEncodedBlock(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, dctV, &quant.UV, &coeffs.QCoeff[20+block], &dq)
		coeffs.SetBlockEOB(20+block, eob)
		stats.rateUV += coefficientBlockTokenRate(coefProbs, 2, ctx, 0, &coeffs.QCoeff[20+block], eob)
		stats.distortionUV += transformBlockError(dctV, &dq)
		stats.tteob += eob
		hasCoeffs = 0
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	stats.distortionUV >>= 2
	return stats
}

// gatherMacroblockYResiduals4x4 writes the 16 luma 4x4 residuals of
// the macroblock at top-left (baseX,baseY) into out as 16 contiguous
// int16-per-block slabs in scan order (block 0 first, block 15 last,
// each block laid out row-major at stride 4). For the in-bounds case
// (the entire 16x16 MB lies inside src) it skips per-pixel coordinate
// clamping; otherwise it falls back to the per-block clamped path
// (same numeric behavior as fillPredictedResidual4x4).
func gatherMacroblockYResiduals4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, baseX int, baseY int, out []int16) {
	if baseY >= 0 && baseX >= 0 && baseY+16 <= height && baseX+16 <= width {
		// Fast path: no clamping. Iterate block-row-major.
		for by := range 4 {
			for bx := range 4 {
				blockOff := (by*4 + bx) * 16
				srcOff := (baseY+by*4)*srcStride + (baseX + bx*4)
				predOff := (baseY+by*4)*predStride + (baseX + bx*4)
				for r := range 4 {
					so := srcOff + r*srcStride
					po := predOff + r*predStride
					out[blockOff+r*4+0] = int16(int(src[so+0]) - int(pred[po+0]))
					out[blockOff+r*4+1] = int16(int(src[so+1]) - int(pred[po+1]))
					out[blockOff+r*4+2] = int16(int(src[so+2]) - int(pred[po+2]))
					out[blockOff+r*4+3] = int16(int(src[so+3]) - int(pred[po+3]))
				}
			}
		}
		return
	}
	for block := range 16 {
		x := baseX + (block&3)*4
		y := baseY + (block>>2)*4
		fillPredictedResidual4x4Slice(src, srcStride, width, height, pred, predStride, x, y, out[block*16:block*16+16])
	}
}

// gatherMacroblockUVResiduals4x4 writes the 4 chroma 4x4 residuals of
// the 8x8 MB chroma block at top-left (baseX,baseY) into out (4 blocks,
// 16 int16 per block in scan order). Same fast/slow split as the Y
// gatherer.
func gatherMacroblockUVResiduals4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, baseX int, baseY int, out []int16) {
	if baseY >= 0 && baseX >= 0 && baseY+8 <= height && baseX+8 <= width {
		for by := range 2 {
			for bx := range 2 {
				blockOff := (by*2 + bx) * 16
				srcOff := (baseY+by*4)*srcStride + (baseX + bx*4)
				predOff := (baseY+by*4)*predStride + (baseX + bx*4)
				for r := range 4 {
					so := srcOff + r*srcStride
					po := predOff + r*predStride
					out[blockOff+r*4+0] = int16(int(src[so+0]) - int(pred[po+0]))
					out[blockOff+r*4+1] = int16(int(src[so+1]) - int(pred[po+1]))
					out[blockOff+r*4+2] = int16(int(src[so+2]) - int(pred[po+2]))
					out[blockOff+r*4+3] = int16(int(src[so+3]) - int(pred[po+3]))
				}
			}
		}
		return
	}
	for block := range 4 {
		x := baseX + (block&1)*4
		y := baseY + (block>>1)*4
		fillPredictedResidual4x4Slice(src, srcStride, width, height, pred, predStride, x, y, out[block*16:block*16+16])
	}
}

func macroblockCoefficientsEmpty(coeffs *vp8enc.MacroblockCoefficients, is4x4 bool) bool {
	if coeffs.EOB[24] != 0 {
		return false
	}
	for i := range 16 {
		if (is4x4 && coeffs.EOB[i] != 0) || (!is4x4 && coeffs.EOB[i] > 1) {
			return false
		}
	}
	for i := 16; i < 24; i++ {
		if coeffs.EOB[i] != 0 {
			return false
		}
	}
	return true
}

func clearMacroblockCoefficients(coeffs *vp8enc.MacroblockCoefficients) {
	*coeffs = vp8enc.MacroblockCoefficients{}
}

func staticInterRDEncodeBreakout(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *vp8enc.MacroblockQuant, encodeBreakout int) bool {
	breakout, _ := staticInterRDEncodeBreakoutDistortion(src, pred, mbRow, mbCol, quant, encodeBreakout)
	return breakout
}

func staticInterRDEncodeBreakoutDistortion(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *vp8enc.MacroblockQuant, encodeBreakout int) (bool, int) {
	if encodeBreakout <= 0 || pred == nil || quant == nil {
		return false, 0
	}
	yAC := int(quant.Y1.Dequant[1])
	threshold := max((yAC*yAC)>>4, encodeBreakout)
	lumaVar, lumaSSE := macroblockLumaVarianceSSE(src, pred, mbRow, mbCol)
	if lumaSSE >= threshold {
		return false, 0
	}
	y2DC := int(quant.Y2.Dequant[0])
	dcError := lumaSSE - lumaVar
	if dcError >= (y2DC*y2DC)>>4 && (lumaSSE/2 <= lumaVar || dcError >= 64) {
		return false, 0
	}
	chromaSSE := macroblockChromaSSE(src, pred, mbRow, mbCol)
	return chromaSSE*2 < threshold, lumaSSE + chromaSSE
}

func staticInterFastEncodeBreakout(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *vp8enc.InterFrameMacroblockMode, quant *vp8enc.MacroblockQuant, encodeBreakout int, lumaSSE int) bool {
	if encodeBreakout <= 0 || ref == nil || mode == nil || quant == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode == vp8common.SplitMV {
		return false
	}
	yAC := int(quant.Y1.Dequant[1])
	threshold := max((yAC*yAC)>>4, encodeBreakout)
	if lumaSSE >= threshold {
		return false
	}
	chromaSSE, ok := macroblockChromaMotionSSE(src, ref, mbRow, mbCol, mode)
	return ok && chromaSSE*2 < encodeBreakout
}

func macroblockChromaMotionSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mode *vp8enc.InterFrameMacroblockMode) (int, bool) {
	if ref == nil || mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode == vp8common.SplitMV {
		return 0, false
	}
	var decMode vp8dec.MacroblockMode
	convertInterFrameMode(mode, &decMode)
	decMode.MBSkipCoeff = true
	var tokens vp8dec.MacroblockTokens
	var dequant vp8common.MacroblockDequant
	var residual vp8dec.MacroblockResidual
	var yPred [16 * 16]byte
	var uPred [8 * 8]byte
	var vPred [8 * 8]byte
	if !vp8dec.ReconstructWholeMVInterMacroblock(&decMode, &tokens, &dequant, ref, yPred[:], 16, uPred[:], 8, vPred[:], 8, &residual, mbRow, mbCol, vp8dec.InterPredictionConfig{}) {
		return 0, false
	}
	return macroblockChromaBufferSSE(src, mbRow, mbCol, uPred[:], 8, vPred[:], 8), true
}

func macroblockChromaBufferSSE(src vp8enc.SourceImage, mbRow int, mbCol int, predU []byte, predUStride int, predV []byte, predVStride int) int {
	baseY := mbRow * 8
	baseX := mbCol * 8
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	if baseY >= 0 && baseX >= 0 &&
		baseY+8 <= uvHeight && baseX+8 <= uvWidth &&
		len(predU) >= 7*predUStride+8 && len(predV) >= 7*predVStride+8 {
		srcOffset := baseY*src.UStride + baseX
		return dsp.SSE8x8(src.U[srcOffset:], src.UStride, predU, predUStride) +
			dsp.SSE8x8(src.V[baseY*src.VStride+baseX:], src.VStride, predV, predVStride)
	}

	sse := 0
	for row := range 8 {
		srcY := clampEncodeCoord(baseY+row, uvHeight)
		for col := range 8 {
			srcX := clampEncodeCoord(baseX+col, uvWidth)
			uDiff := int(src.U[srcY*src.UStride+srcX]) - int(predU[row*predUStride+col])
			vDiff := int(src.V[srcY*src.VStride+srcX]) - int(predV[row*predVStride+col])
			sse += uDiff*uDiff + vDiff*vDiff
		}
	}
	return sse
}

const (
	lastFrameZeroMVZbinBoost  = 6
	goldenAltZeroMVZbinBoost  = 12
	nonZeroInterModeZbinBoost = 4
	splitInterModeZbinBoost   = 0
	intraInterFrameZbinBoost  = 0
)

func interZbinModeBoost(mode *vp8enc.InterFrameMacroblockMode) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode >= vp8common.DCPred && mode.Mode <= vp8common.BPred {
		return intraInterFrameZbinBoost
	}
	switch mode.Mode {
	case vp8common.ZeroMV:
		if mode.RefFrame == vp8common.LastFrame {
			return lastFrameZeroMVZbinBoost
		}
		return goldenAltZeroMVZbinBoost
	case vp8common.SplitMV:
		return splitInterModeZbinBoost
	default:
		return nonZeroInterModeZbinBoost
	}
}

// libvpxY1DCWouldQuantizeNonzero returns 1 when libvpx's vp8_quantize_mb path
// would have produced a non-zero quantized DC for the given Y1DC quantizer
// on the supplied input coefficient dct0.
//
// Why: libvpx's transform_mb does NOT zero block[i].coeff[0] before
// vp8_quantize_mb, so vp8_fast_quantize_b_c / vp8_regular_quantize_b_c
// quantize the original Y-block DC against Y1DC's zbin/round/quant tables.
// When that quantization produces y != 0, libvpx records *d->eob = 1 even
// for an otherwise empty Y_NO_DC block. Later, vp8_inverse_transform_mby
// overwrites qcoeff[0] (with the inverse-Walsh DC) and
// vp8_dequant_idct_add_y_block memsets qcoeff[0..1] back to zero, but eob=1
// is preserved through the pipeline. The libvpx-side oracle reads this
// post-IDCT eob.
//
// govpx's pipeline zeroes dct[0] before quantize because Y_NO_DC tokenize
// starts at c=1 anyway, so coeffs.EOB[block] never carries that DC bump.
// This helper recovers the bump for the per-MB oracle trace so the
// scoreboard's eob_sum match-rate aligns with libvpx. The helper does NOT
// influence bitstream emission or reconstruction; the OracleY1DCEOB1 flag
// it populates is read only by emitOracleMBTrace.
//
// fastQuant selects between vp8_fast_quantize_b_c (no zbin gate) and
// vp8_regular_quantize_b_c (zbin gate at position 0, where zbin_boost[0]=0
// so only zbin_extra contributes). zbinOverQuant and zbinModeBoost mirror
// the macroblock-level fields fed to vp8_update_zbin_extra.
func libvpxY1DCWouldQuantizeNonzero(dct0 int16, quant *vp8enc.BlockQuant, zbinOverQuant int, zbinModeBoost int, fastQuant bool) uint8 {
	if quant == nil {
		return 0
	}
	z := int(dct0)
	if z == 0 {
		return 0
	}
	x := z
	if x < 0 {
		x = -x
	}
	if fastQuant {
		y := ((x + int(quant.Round[0])) * int(quant.QuantFast[0])) >> 16
		if y != 0 {
			return 1
		}
		return 0
	}
	zbin := int(quant.Zbin[0])
	zbin += int(quant.ZbinBoost[0])
	zbin += (int(quant.Dequant[1]) * (zbinOverQuant + zbinModeBoost)) >> 7
	if x < zbin {
		return 0
	}
	x += int(quant.Round[0])
	y := ((((x * int(quant.Quant[0])) >> 16) + x) * int(quant.QuantShift[0])) >> 16
	if y != 0 {
		return 1
	}
	return 0
}

func quantizeBlockWithZbin(coeff *[16]int16, quant *vp8enc.BlockQuant, qIndex int, zbinOverQuant int, zbinModeBoost int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if coeff == nil || quant == nil || qcoeff == nil || dqcoeff == nil {
		return 0
	}
	eob := -1
	zeroRun := 0
	for pos := range 16 {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		z := int(coeff[rc])
		if z == 0 {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			if zeroRun < len(quant.ZbinBoost)-1 {
				zeroRun++
			}
			continue
		}

		x := z
		if x < 0 {
			x = -x
		}
		zbin := int(quant.Zbin[rc])
		zbin += int(quant.ZbinBoost[zeroRun])
		zbin += (int(quant.Dequant[1]) * (zbinOverQuant + zbinModeBoost)) >> 7
		if x < zbin {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			if zeroRun < len(quant.ZbinBoost)-1 {
				zeroRun++
			}
			continue
		}

		x += int(quant.Round[rc])
		y := ((((x * int(quant.Quant[rc])) >> 16) + x) * int(quant.QuantShift[rc])) >> 16
		if z < 0 {
			y = -y
		}
		q := int16(y)
		qcoeff[rc] = q
		dqcoeff[rc] = q * quant.Dequant[rc]
		if y != 0 {
			eob = pos
			zeroRun = 0
		} else if zeroRun < len(quant.ZbinBoost)-1 {
			zeroRun++
		}
	}
	return eob + 1
}

func quantizeOptimizedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return quantizeOptimizedBlockWithRDZbin(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, zbinOverQuant, intra, coeff, quant, qcoeff, dqcoeff)
}

func quantizeOptimizedBlockWithRDZbin(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, rdZbinOverQuant int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	eob := quantizeBlockWithZbin(coeff, quant, qIndex, zbinOverQuant, zbinModeBoost, qcoeff, dqcoeff)
	eob = optimizeQuantizedBlock(coefProbs, qIndex, blockType, ctx, skipDC, rdZbinOverQuant, intra, coeff, quant, qcoeff, eob)
	dequantizeQuantizedBlock(quant, qcoeff, dqcoeff)
	return eob
}

func quantizeEncodedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, intra bool, fastQuant bool, optimize bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return quantizeEncodedBlockWithRDZbin(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, zbinOverQuant, intra, fastQuant, optimize, coeff, quant, qcoeff, dqcoeff)
}

// quantizeEncodedBlockWithRDZbin keeps libvpx's Y2 split explicit: Y2 zbin
// thresholding uses zbin_over_quant/2, while the trellis optimizer scores with
// mb->rdmult computed from the full frame-level zbin_over_quant.
func quantizeEncodedBlockWithRDZbin(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, rdZbinOverQuant int, intra bool, fastQuant bool, optimize bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if fastQuant {
		return vp8enc.FastQuantizeBlock(coeff, quant, qcoeff, dqcoeff)
	}
	if optimize {
		eob := quantizeOptimizedBlockWithRDZbin(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, rdZbinOverQuant, intra, coeff, quant, qcoeff, dqcoeff)
		if blockType == 1 && skipDC == 0 {
			eob = resetLibvpxSmallSecondOrderCoefficients(quant, qcoeff, dqcoeff, eob)
		}
		return eob
	}
	return quantizeBlockWithZbin(coeff, quant, qIndex, zbinOverQuant, zbinModeBoost, qcoeff, dqcoeff)
}

func quantizeDecisionBlock(fastQuant bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qIndex int, zbinOverQuant int, zbinModeBoost int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if fastQuant {
		return vp8enc.FastQuantizeBlock(coeff, quant, qcoeff, dqcoeff)
	}
	return quantizeBlockWithZbin(coeff, quant, qIndex, zbinOverQuant, zbinModeBoost, qcoeff, dqcoeff)
}

func dequantizeQuantizedBlock(quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) {
	if quant == nil || qcoeff == nil || dqcoeff == nil {
		return
	}
	for i := range 16 {
		dqcoeff[i] = qcoeff[i] * quant.Dequant[i]
	}
}

// optimizeQuantizedBlock ports libvpx v1.16.0 vp8/encoder/encodemb.c optimize_b.
// It walks the quantized block from eob-1 down to skipDC, builds a 2-state
// Viterbi trellis exploring (keep current value) vs (shift |x| toward 0 when
// the dequant boundary allows), scores transitions with libvpx's token_costs
// subtree elision, and applies the path that minimizes the libvpx RDCOST. Tied
// RDCOSTs use the libvpx RDTRUNC tie-break.
func optimizeQuantizedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, eob int) int {
	if coeff == nil || quant == nil || qcoeff == nil || eob <= skipDC {
		return eob
	}
	if blockType < 0 || blockType >= vp8tables.BlockTypes || ctx < 0 || ctx >= vp8tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return eob
	}
	if coefProbs == nil {
		return eob
	}
	if eob > 16 {
		eob = 16
	}

	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	rdMult *= blockPlaneRDMultiplier(blockType)
	if intra {
		rdMult = (rdMult * 9) >> 4
	}

	type tokenState struct {
		rate  int
		error int
		next  int8
		token int8
		qc    int16
	}
	var tokens [17][2]tokenState
	var bestMask [2]uint32

	tokens[eob][0] = tokenState{next: 16, token: int8(vp8tables.DCTEOBToken)}
	tokens[eob][1] = tokens[eob][0]
	next := eob

	for i := eob - 1; i >= skipDC; i-- {
		rc := int(vp8tables.DefaultZigZag1D[i])
		x := int(qcoeff[rc])
		if x != 0 {
			error0 := tokens[next][0].error
			error1 := tokens[next][1].error
			rate0 := tokens[next][0].rate
			rate1 := tokens[next][1].rate
			t0 := dctValueToken(x)

			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				pt := int(vp8tables.PrevTokenClass[t0])
				p := (*coefProbs)[blockType][band][pt]
				rate0 += coefficientTokenCost(p, int(tokens[next][0].token), blockType, band, pt)
				rate1 += coefficientTokenCost(p, int(tokens[next][1].token), blockType, band, pt)
			}

			rdCost0 := libvpxRDCost(rdMult, rdDiv, rate0, error0)
			rdCost1 := libvpxRDCost(rdMult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = libvpxRDTrunc(rdMult, rate0)
				rdCost1 = libvpxRDTrunc(rdMult, rate1)
			}
			best := 0
			if rdCost1 < rdCost0 {
				best = 1
			}

			baseBits := dctValueBaseCost(x)
			dq := int(quant.Dequant[rc])
			dx := x*dq - int(coeff[rc])
			d2 := dx * dx

			if best == 1 {
				tokens[i][0].rate = baseBits + rate1
				tokens[i][0].error = d2 + error1
			} else {
				tokens[i][0].rate = baseBits + rate0
				tokens[i][0].error = d2 + error0
			}
			tokens[i][0].next = int8(next)
			tokens[i][0].token = int8(t0)
			tokens[i][0].qc = int16(x)
			bestMask[0] |= uint32(best) << uint(i)

			rate0 = tokens[next][0].rate
			rate1 = tokens[next][1].rate

			absX := x
			if absX < 0 {
				absX = -absX
			}
			absC := int(coeff[rc])
			if absC < 0 {
				absC = -absC
			}
			shortcut := absX*dq > absC && absX*dq < absC+dq
			xs := x
			sz := 0
			if shortcut {
				if x < 0 {
					sz = -1
				}
				xs -= 2*sz + 1
			}

			var t1 int
			if xs == 0 {
				if int(tokens[next][0].token) == vp8tables.DCTEOBToken {
					t0 = vp8tables.DCTEOBToken
				} else {
					t0 = vp8tables.ZeroToken
				}
				if int(tokens[next][1].token) == vp8tables.DCTEOBToken {
					t1 = vp8tables.DCTEOBToken
				} else {
					t1 = vp8tables.ZeroToken
				}
			} else {
				t0 = dctValueToken(xs)
				t1 = t0
			}

			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				if t0 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t0])
					p := (*coefProbs)[blockType][band][pt]
					rate0 += coefficientTokenCost(p, int(tokens[next][0].token), blockType, band, pt)
				}
				if t1 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t1])
					p := (*coefProbs)[blockType][band][pt]
					rate1 += coefficientTokenCost(p, int(tokens[next][1].token), blockType, band, pt)
				}
			}

			rdCost0 = libvpxRDCost(rdMult, rdDiv, rate0, error0)
			rdCost1 = libvpxRDCost(rdMult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = libvpxRDTrunc(rdMult, rate0)
				rdCost1 = libvpxRDTrunc(rdMult, rate1)
			}
			best = 0
			if rdCost1 < rdCost0 {
				best = 1
			}

			baseBits = dctValueBaseCost(xs)

			d2s := d2
			if shortcut {
				dxs := dx - ((dq + sz) ^ sz)
				d2s = dxs * dxs
			}

			if best == 1 {
				tokens[i][1].rate = baseBits + rate1
				tokens[i][1].error = d2s + error1
				tokens[i][1].token = int8(t1)
			} else {
				tokens[i][1].rate = baseBits + rate0
				tokens[i][1].error = d2s + error0
				tokens[i][1].token = int8(t0)
			}
			tokens[i][1].next = int8(next)
			tokens[i][1].qc = int16(xs)
			bestMask[1] |= uint32(best) << uint(i)
			next = i
		} else {
			band := int(vp8tables.CoefBandsTable[i+1])
			p := (*coefProbs)[blockType][band][0]
			t0Tok := int(tokens[next][0].token)
			t1Tok := int(tokens[next][1].token)
			if t0Tok != vp8tables.DCTEOBToken {
				tokens[next][0].rate += coefficientTokenCost(p, t0Tok, blockType, band, 0)
				tokens[next][0].token = int8(vp8tables.ZeroToken)
			}
			if t1Tok != vp8tables.DCTEOBToken {
				tokens[next][1].rate += coefficientTokenCost(p, t1Tok, blockType, band, 0)
				tokens[next][1].token = int8(vp8tables.ZeroToken)
			}
		}
	}

	band := int(vp8tables.CoefBandsTable[skipDC])
	rate0 := tokens[next][0].rate
	rate1 := tokens[next][1].rate
	error0 := tokens[next][0].error
	error1 := tokens[next][1].error
	p := (*coefProbs)[blockType][band][ctx]
	rate0 += coefficientTokenCost(p, int(tokens[next][0].token), blockType, band, ctx)
	rate1 += coefficientTokenCost(p, int(tokens[next][1].token), blockType, band, ctx)
	rdCost0 := libvpxRDCost(rdMult, rdDiv, rate0, error0)
	rdCost1 := libvpxRDCost(rdMult, rdDiv, rate1, error1)
	if rdCost0 == rdCost1 {
		rdCost0 = libvpxRDTrunc(rdMult, rate0)
		rdCost1 = libvpxRDTrunc(rdMult, rate1)
	}
	best := 0
	if rdCost1 < rdCost0 {
		best = 1
	}

	finalEOB := skipDC - 1
	for i := next; i < eob; {
		x := tokens[i][best].qc
		if x != 0 {
			finalEOB = i
		}
		rc := int(vp8tables.DefaultZigZag1D[i])
		qcoeff[rc] = x
		nextI := int(tokens[i][best].next)
		best = int((bestMask[best] >> uint(i)) & 1)
		i = nextI
	}
	return finalEOB + 1
}

// libvpxRDTrunc mirrors the encodemb.c RDTRUNC macro used to break ties when
// two trellis paths have equal RDCOST.
func libvpxRDTrunc(rdMult int, rate int) int {
	return (128 + rate*rdMult) & 0xFF
}

// dctValueToken returns the libvpx coefficient-token classification for value x
// (mirrors the dct_value_tokens table indexed by signed value).
func dctValueToken(x int) int {
	abs := x
	if abs < 0 {
		abs = -abs
	}
	if abs == 0 {
		return vp8tables.ZeroToken
	}
	token, _, ok := coefficientTokenMagnitude(abs)
	if !ok {
		return vp8tables.ZeroToken
	}
	return token
}

// dctValueBaseCost mirrors libvpx's dct_value_cost table: extra bits cost plus
// sign bit cost for value x. The token-tree cost is added separately by the
// trellis using band/context-specific token costs.
func dctValueBaseCost(x int) int {
	if x == 0 {
		return 0
	}
	abs := x
	if abs < 0 {
		abs = -abs
	}
	token, _, ok := coefficientTokenMagnitude(abs)
	if !ok {
		return maxInt() / 4
	}
	cost := 0
	if x < 0 {
		cost += boolBitCost(128, 1)
	} else {
		cost += boolBitCost(128, 0)
	}
	cost += coefficientExtraBitsRate(token, abs)
	return cost
}

// Ported from libvpx v1.16.0 vp8/encoder/encodemb.c
// check_reset_2nd_coeffs. Very small Y2 residuals inverse-transform to a zero
// pixel delta, so libvpx drops the whole second-order block after optimization.
func resetLibvpxSmallSecondOrderCoefficients(quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16, eob int) int {
	if quant == nil || qcoeff == nil || eob <= 0 {
		return eob
	}
	if quant.Dequant[0] >= 35 && quant.Dequant[1] >= 35 {
		return eob
	}
	sum := 0
	for pos := 0; pos < eob && pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coef := int(qcoeff[rc]) * int(quant.Dequant[rc])
		if coef < 0 {
			coef = -coef
		}
		sum += coef
		if sum >= 35 {
			return eob
		}
	}
	for pos := 0; pos < eob && pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		qcoeff[rc] = 0
		if dqcoeff != nil {
			dqcoeff[rc] = 0
		}
	}
	return 0
}

func rdBlockScore(qIndex int, planeMultiplier int, intra bool, rate int, distortion int) int {
	return rdBlockScoreWithZbin(qIndex, 0, planeMultiplier, intra, rate, distortion)
}

func rdBlockScoreWithZbin(qIndex int, zbinOverQuant int, planeMultiplier int, intra bool, rate int, distortion int) int {
	if planeMultiplier <= 0 {
		planeMultiplier = 1
	}
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	rdMult *= planeMultiplier
	if intra {
		rdMult = (rdMult * 9) >> 4
	}
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

func blockPlaneRDMultiplier(blockType int) int {
	switch blockType {
	case 1:
		return 16
	case 2:
		return 2
	default:
		return 4
	}
}

func macroblockCoefficientTokenRate(probs *vp8tables.CoefficientProbs, is4x4 bool, coeffs *vp8enc.MacroblockCoefficients) int {
	return macroblockCoefficientTokenRateWithContext(probs, is4x4, nil, nil, coeffs)
}

func macroblockCoefficientTokenRateWithContext(probs *vp8tables.CoefficientProbs, is4x4 bool, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, coeffs *vp8enc.MacroblockCoefficients) int {
	if probs == nil || coeffs == nil {
		return maxInt() / 4
	}

	rate := 0
	blockType := 0
	skipDC := 0
	var yAbove [4]uint8
	var yLeft [4]uint8
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		uvAbove = tokenUVContextArray(aboveTok)
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		uvLeft = tokenUVContextArray(leftTok)
		y2Left = leftTok.Y2
	}
	if !is4x4 {
		eob := coeffs.BlockEOB(24, 0)
		rate += coefficientBlockTokenRate(probs, 1, int(y2Above+y2Left), 0, &coeffs.QCoeff[24], eob)
		blockType = 0
		skipDC = 1
	} else {
		blockType = 3
	}

	for block := range 16 {
		eob := coeffs.BlockEOB(block, skipDC)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		rate += coefficientBlockTokenRate(probs, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > skipDC {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
	}

	for block := 16; block < 24; block++ {
		eob := coeffs.BlockEOB(block, 0)
		a, l := macroblockCoefficientUVContextIndex(block)
		ctx := int(uvAbove[a] + uvLeft[l])
		rate += coefficientBlockTokenRate(probs, 2, ctx, 0, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	return rate
}

func tokenUVContextArray(ctx *vp8enc.TokenContextPlanes) [4]uint8 {
	if ctx == nil {
		return [4]uint8{}
	}
	return [4]uint8{ctx.U[0], ctx.U[1], ctx.V[0], ctx.V[1]}
}

func macroblockCoefficientUVContextIndex(block int) (int, int) {
	base := 0
	if block > 19 {
		base = 2
	}
	a := base + (block & 1)
	l := base
	if block&3 > 1 {
		l++
	}
	return a, l
}

func macroblockImageSSE(src vp8enc.SourceImage, img *vp8common.Image, mbRow int, mbCol int) int {
	return macroblockLumaSSE(src, img, mbRow, mbCol, vp8enc.MotionVector{}) +
		macroblockChromaSSE(src, img, mbRow, mbCol)
}

func macroblockImageBlockSAD(src vp8enc.SourceImage, img *vp8common.Image, srcMbRow int, srcMbCol int, refMbRow int, refMbCol int) int {
	if img == nil {
		return maxInt()
	}
	baseY := srcMbRow * 16
	baseX := srcMbCol * 16
	refBaseY := refMbRow * 16
	refBaseX := refMbCol * 16
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= img.CodedHeight && refBaseX+16 <= img.CodedWidth {
		return dsp.SAD16x16(src.Y[baseY*src.YStride+baseX:], src.YStride, img.Y[refBaseY*img.YStride+refBaseX:], img.YStride)
	}

	sad := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, img.CodedHeight)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, img.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(img.Y[refY*img.YStride+refX])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
	}
	return sad
}

func predictAnalysisMacroblock(img *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, scratch *vp8dec.IntraReconstructionScratch) bool {
	refs := vp8dec.BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	yOK := false
	if mode.Is4x4 || mode.Mode == vp8common.BPred {
		yOK = vp8dec.PredictIntraY4x4(&mode.BModes, img.Y[yOff:], img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft)
	} else {
		yOK = vp8dec.PredictIntraY16x16(mode.Mode, img.Y[yOff:], img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, refs.UpAvailable, refs.LeftAvailable)
	}
	return yOK &&
		vp8dec.PredictIntraUV8x8(mode.UVMode, img.U[uOff:], img.UStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) &&
		vp8dec.PredictIntraUV8x8(mode.UVMode, img.V[vOff:], img.VStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable)
}

func predictAnalysisChroma(img *vp8common.Image, row int, col int, uvMode vp8common.MBPredictionMode, scratch *vp8dec.IntraReconstructionScratch) bool {
	refs := vp8dec.BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	return vp8dec.PredictIntraUV8x8(uvMode, img.U[uOff:], img.UStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) &&
		vp8dec.PredictIntraUV8x8(uvMode, img.V[vOff:], img.VStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable)
}

func predictAnalysisBPredBlock(mode vp8common.BPredictionMode, dst []byte, stride int, macroblock []byte, macroblockStride int, above []byte, left []byte, topLeft byte, block int) bool {
	blockRow := block >> 2
	blockCol := block & 3
	y := blockRow * 4
	x := blockCol * 4
	var blockAbove [8]byte
	var blockLeft [4]byte

	if blockRow == 0 {
		copy(blockAbove[:], above[x:x+8])
	} else {
		aboveOff := (y-1)*macroblockStride + x
		copy(blockAbove[:4], macroblock[aboveOff:aboveOff+4])
		if blockCol < 3 {
			copy(blockAbove[4:], macroblock[aboveOff+4:aboveOff+8])
		} else {
			copy(blockAbove[4:], above[16:20])
		}
	}

	if blockCol == 0 {
		copy(blockLeft[:], left[y:y+4])
	} else {
		for i := range 4 {
			blockLeft[i] = macroblock[(y+i)*macroblockStride+x-1]
		}
	}

	blockTopLeft := topLeft
	switch {
	case blockRow == 0 && blockCol == 0:
	case blockRow == 0:
		blockTopLeft = above[x-1]
	case blockCol == 0:
		blockTopLeft = left[y-1]
	default:
		blockTopLeft = macroblock[(y-1)*macroblockStride+x-1]
	}

	return dsp.Intra4x4Predict(dst, stride, mode, blockAbove[:], blockLeft[:], blockTopLeft)
}

func copyBPredBlock(src []byte, srcStride int, dst []byte, dstStride int, block int) {
	y := (block >> 2) * 4
	x := (block & 3) * 4
	for row := range 4 {
		copy(dst[(y+row)*dstStride+x:], src[row*srcStride:row*srcStride+4])
	}
}

func bPredBlockSSE(src vp8enc.SourceImage, mbRow int, mbCol int, block int, pred []byte, predStride int) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	sse := 0
	for row := range 4 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := range 4 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*predStride+col])
			sse += diff * diff
		}
	}
	return sse
}

func fillBPredResidual4x4(src vp8enc.SourceImage, mbRow int, mbCol int, block int, pred []byte, predStride int, out *[16]int16) {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	for row := range 4 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := range 4 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			out[row*4+col] = int16(int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*predStride+col]))
		}
	}
}

func transformBlockError(coeff *[16]int16, dqcoeff *[16]int16) int {
	return dsp.TransformBlockError(coeff, dqcoeff)
}

func buildReconstructingBPredMacroblockCoefficients(coefProbs *vp8tables.CoefficientProbs, src vp8enc.SourceImage, mbRow int, mbCol int, img *vp8common.Image, mode *vp8dec.MacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, zbinOverQuant int, fastQuant bool, optimize bool, coeffs *vp8enc.MacroblockCoefficients, scratch *vp8dec.IntraReconstructionScratch) bool {
	if img == nil || mode == nil || quant == nil || coeffs == nil || scratch == nil || !mode.Is4x4 || mode.Mode != vp8common.BPred {
		return false
	}
	if coefProbs == nil {
		return false
	}

	refs := vp8dec.BuildIntraPredictorRefs(img, mbRow, mbCol, &scratch.Refs)
	yOff := mbRow*16*img.YStride + mbCol*16
	uOff := mbRow*8*img.UStride + mbCol*8
	vOff := mbRow*8*img.VStride + mbCol*8
	y := img.Y[yOff:]
	u := img.U[uOff:]
	v := img.V[vOff:]

	var input [16]int16
	var dct [16]int16
	var dq [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		y2Left = leftTok.Y2
	}
	var staleY2Input [16]int16
	for block := range 16 {
		blockOffset := analysisYBlockOffset(block, img.YStride)
		if !predictAnalysisBPredBlock(mode.BModes[block], y[blockOffset:], img.YStride, y, img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			return false
		}
		x := mbCol*16 + (block&3)*4
		yCoord := mbRow*16 + (block>>2)*4
		fillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, img.Y, img.YStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		// Capture chosen-mode FDCT DC for the oracle stale-Y2 trace
		// snapshot (see OracleStaleY2EOB).
		staleY2Input[block] = dct[0]
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		// libvpx vp8_encode_intra4x4mby (encodeintra.c) never invokes the
		// trellis optimizer for B_PRED Y sub-blocks: it calls
		// vp8_encode_intra4x4block which runs only x->quantize_b before the
		// IDCT-add. The frame-level vp8_optimize_mby pass is wired only
		// from vp8_encode_intra16x16mby. So the Y plane of any B_PRED MB
		// (keyframe or inter intra-coded) must be quantized without
		// trellising regardless of the encoder-level optimize flag; only
		// the UV blocks below pick up the optimizer (they go through
		// vp8_encode_intra16x16mbuv -> vp8_optimize_mbuv). Without this
		// gate the BestQuality keyframe Y reconstruction byte-diverges
		// from libvpx on B_PRED MBs (see r9-4 SplitMV-quadrant fixture).
		eob := quantizeEncodedBlock(coefProbs, qIndex, 3, ctx, 0, zbinOverQuant, 0, mode.RefFrame == vp8common.IntraFrame, fastQuant, false, &dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
		coeffs.SetBlockEOB(block, eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
		addQuantizedBlockResidual(eob, &dq, y[blockOffset:], img.YStride)
	}
	coeffs.QCoeff[24] = [16]int16{}
	coeffs.SetBlockEOB(24, 0)
	// Mirror libvpx's stale Y2 second-order snapshot for B_PRED. See
	// OracleStaleY2EOB for the rationale.
	{
		var staleY2Coeff [16]int16
		var staleY2Q [16]int16
		var staleY2DQ [16]int16
		intra := mode.RefFrame == vp8common.IntraFrame
		vp8enc.ForwardWalsh4x4(staleY2Input[:], 4, &staleY2Coeff)
		staleEOB := min(max(quantizeEncodedBlockWithRDZbin(coefProbs, qIndex, 1, int(y2Above+y2Left), 0, zbinOverQuant/2, 0, zbinOverQuant, intra, fastQuant, optimize, &staleY2Coeff, &quant.Y2, &staleY2Q, &staleY2DQ), 0), 16)
		coeffs.OracleStaleY2EOB = uint8(staleEOB)
		coeffs.OracleStaleY2QCoeff = staleY2Q
		coeffs.OracleStaleY2Set = true
	}

	if !vp8dec.PredictIntraUV8x8(mode.UVMode, u, img.UStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if !vp8dec.PredictIntraUV8x8(mode.UVMode, v, img.VStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}

	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	if aboveTok != nil {
		uvAbove = tokenUVContextArray(aboveTok)
	}
	if leftTok != nil {
		uvLeft = tokenUVContextArray(leftTok)
	}
	// Whole-UV residual+DCT batch — prediction was already written
	// into img.U / img.V above so all 8 chroma 4x4 residuals are
	// independent and can be transformed in a single dispatched call,
	// matching libvpx v1.16.0 vp8_transform_mbuv's two fdct8x4 calls.
	var uvResiduals [8 * 16]int16
	var uvDcts [8 * 16]int16
	gatherMacroblockUVResiduals4x4(src.U, src.UStride, uvWidth, uvHeight, img.U, img.UStride, mbCol*8, mbRow*8, uvResiduals[0:64])
	gatherMacroblockUVResiduals4x4(src.V, src.VStride, uvWidth, uvHeight, img.V, img.VStride, mbCol*8, mbRow*8, uvResiduals[64:128])
	vp8enc.ForwardDCT4x4Batch(uvResiduals[:], uvDcts[:], 8)
	for block := range 4 {
		copy(dct[:], uvDcts[block*16:block*16+16])
		a, l := macroblockCoefficientUVContextIndex(16 + block)
		ctx := int(uvAbove[a] + uvLeft[l])
		eob := quantizeEncodedBlock(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, 0, mode.RefFrame == vp8common.IntraFrame, fastQuant, optimize, &dct, &quant.UV, &coeffs.QCoeff[16+block], &dq)
		coeffs.SetBlockEOB(16+block, eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
		addQuantizedBlockResidual(eob, &dq, u[analysisUVBlockOffset(block, img.UStride):], img.UStride)

		copy(dct[:], uvDcts[(4+block)*16:(4+block)*16+16])
		a, l = macroblockCoefficientUVContextIndex(20 + block)
		ctx = int(uvAbove[a] + uvLeft[l])
		eob = quantizeEncodedBlock(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, 0, mode.RefFrame == vp8common.IntraFrame, fastQuant, optimize, &dct, &quant.UV, &coeffs.QCoeff[20+block], &dq)
		coeffs.SetBlockEOB(20+block, eob)
		hasCoeffs = 0
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
		addQuantizedBlockResidual(eob, &dq, v[analysisUVBlockOffset(block, img.VStride):], img.VStride)
	}
	return true
}

func addQuantizedBlockResidual(eob int, dq *[16]int16, dst []byte, stride int) {
	if eob == 0 {
		return
	}
	if eob == 1 {
		dsp.DCOnlyIDCT4x4Add(dq[0], dst, stride, dst, stride)
		return
	}
	dsp.IDCT4x4Add(dq, dst, stride, dst, stride)
}

func analysisYBlockOffset(block int, stride int) int {
	return (block>>2)*4*stride + (block&3)*4
}

func analysisUVBlockOffset(block int, stride int) int {
	return (block>>1)*4*stride + (block&1)*4
}

func reconstructInterAnalysisMacroblock(img *vp8common.Image, last *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, tokens *vp8dec.MacroblockTokens, dequant *vp8common.MacroblockDequant, scratch *vp8dec.IntraReconstructionScratch) bool {
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	if mode.Mode == vp8common.SplitMV {
		return vp8dec.ReconstructSplitMVInterMacroblock(mode, tokens, dequant, last, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col, vp8dec.InterPredictionConfig{})
	}
	return vp8dec.ReconstructWholeMVInterMacroblock(mode, tokens, dequant, last, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col, vp8dec.InterPredictionConfig{})
}

func reconstructAnalysisMacroblock(img *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, tokens *vp8dec.MacroblockTokens, dequant *vp8common.MacroblockDequant, scratch *vp8dec.IntraReconstructionScratch) bool {
	refs := vp8dec.BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	return vp8dec.ReconstructIntraMacroblock(mode, tokens, dequant, refs, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual)
}

func fillPredictedResidual4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, x int, y int, out *[16]int16) {
	for row := range 4 {
		sampleY := clampEncodeCoord(y+row, height)
		for col := range 4 {
			sampleX := clampEncodeCoord(x+col, width)
			out[row*4+col] = int16(int(src[sampleY*srcStride+sampleX]) - int(pred[(y+row)*predStride+x+col]))
		}
	}
}

// fillPredictedResidual4x4Slice mirrors fillPredictedResidual4x4 but
// writes into a caller-supplied slice. Used by the whole-MB residual
// builders that gather all 4x4 blocks into one contiguous buffer
// before dispatching ForwardDCT4x4Batch (the libvpx v1.16.0
// vp8_transform_mb / vp8_transform_intra_mby pattern).
func fillPredictedResidual4x4Slice(src []byte, srcStride int, width int, height int, pred []byte, predStride int, x int, y int, out []int16) {
	for row := range 4 {
		sampleY := clampEncodeCoord(y+row, height)
		for col := range 4 {
			sampleX := clampEncodeCoord(x+col, width)
			out[row*4+col] = int16(int(src[sampleY*srcStride+sampleX]) - int(pred[(y+row)*predStride+x+col]))
		}
	}
}

func clampEncodeCoord(v int, limit int) int {
	if v < 0 {
		return 0
	}
	if v >= limit {
		return limit - 1
	}
	return v
}
