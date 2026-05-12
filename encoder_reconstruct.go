package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
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
	if e.useThreadedKeyFrameRows(rows, cols) {
		return e.buildReconstructingKeyFrameCoefficientsWithSegmentationThreaded(src, qIndex, segmentation, preserveSegmentID, modes, coeffs, rows, cols)
	}
	return e.buildReconstructingKeyFrameCoefficientsWithSegmentationSerial(src, qIndex, segmentation, preserveSegmentID, modes, coeffs, rows, cols)
}

func (e *VP8Encoder) buildReconstructingKeyFrameCoefficientsWithSegmentationThreaded(src vp8enc.SourceImage, qIndex int, segmentation vp8enc.SegmentationConfig, preserveSegmentID bool, modes []vp8enc.KeyFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int) (int, error) {
	if qIndex < vp8common.MinQ || qIndex > vp8common.MaxQ {
		return 0, ErrInvalidConfig
	}
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	if traceEnabled {
		e.resetOracleMBTraceBuffer()
	}
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
	args := threadedKeyRowsArgs{
		src:               src,
		qIndex:            qIndex,
		segmentation:      segmentation,
		preserveSegmentID: preserveSegmentID,
		modes:             modes,
		coeffs:            coeffs,
		rows:              rows,
		cols:              cols,
		quants:            quants,
		aboveTok:          aboveTok,
	}
	threadedRate, err := e.buildReconstructingKeyFrameCoefficientsThreaded(args)
	if err != nil {
		return 0, err
	}
	e.analysis.ExtendBorders()
	return threadedRate, nil
}

func (e *VP8Encoder) buildReconstructingKeyFrameCoefficientsWithSegmentationSerial(src vp8enc.SourceImage, qIndex int, segmentation vp8enc.SegmentationConfig, preserveSegmentID bool, modes []vp8enc.KeyFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int) (int, error) {
	if qIndex < vp8common.MinQ || qIndex > vp8common.MaxQ {
		return 0, ErrInvalidConfig
	}
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	if traceEnabled {
		// Reset oracle trace MB buffer at the start of each build pass so
		// retried key-frame attempts overwrite earlier rows.
		e.resetOracleMBTraceBuffer()
	}
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
			totalRate = addProjectedMacroblockRate(totalRate, projectedRate)
			mode.SegmentID = segmentID
			modes[index] = mode
			convertKeyFrameMode(&modes[index], &e.reconstructModes[index])
			if modes[index].YMode == vp8common.BPred {
				if !buildReconstructingBPredMacroblockCoefficients(&vp8tables.DefaultCoefProbs, src, row, col, &e.analysis.Img, &e.reconstructModes[index], &aboveTok[col], &leftTok, &quants[segmentID], segmentQIndex, 0, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), traceEnabled, &coeffs[index], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
				convertMacroblockCoefficients(&coeffs[index], true, &e.reconstructTokens[index])
				vp8enc.UpdateTokenContextPlanesFromCoefficients(&aboveTok[col], &leftTok, true, &coeffs[index])
				if traceEnabled {
					e.emitOracleKeyFrameMBTrace(row, col, &modes[index], &coeffs[index], projectedRate, totalRate)
				}
				continue
			}
			if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
				return 0, ErrInvalidConfig
			}
			is4x4 := modes[index].YMode == vp8common.BPred
			buildPredictedMacroblockCoefficients(predictedMacroblockCoefficientArgs{
				coefProbs:     &vp8tables.DefaultCoefProbs,
				src:           src,
				mbRow:         row,
				mbCol:         col,
				pred:          &e.analysis.Img,
				aboveTok:      &aboveTok[col],
				leftTok:       &leftTok,
				quant:         &quants[segmentID],
				qIndex:        segmentQIndex,
				is4x4:         is4x4,
				intra:         true,
				fastQuant:     e.libvpxUseFastQuant(),
				optimize:      e.libvpxOptimizeCoefficients(),
				collectOracle: traceEnabled,
				coeffs:        &coeffs[index],
			})
			convertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
			if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID&3], &e.reconstructScratch) {
				return 0, ErrInvalidConfig
			}
			vp8enc.UpdateTokenContextPlanesFromCoefficients(&aboveTok[col], &leftTok, is4x4, &coeffs[index])
			if traceEnabled {
				e.emitOracleKeyFrameMBTrace(row, col, &modes[index], &coeffs[index], projectedRate, totalRate)
			}
		}
		vp8dec.ExtendIntraRightEdgeForRow(&e.analysis.Img, row)
	}
	e.analysis.ExtendBorders()
	return totalRate, nil
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficients(src vp8enc.SourceImage, qIndex int, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int, flags EncodeFlags) (int, error) {
	return e.buildReconstructingInterFrameCoefficientsWithSegmentation(src, qIndex, vp8enc.SegmentationConfig{}, false, modes, coeffs, rows, cols, flags)
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficientsMaybeThreaded(src vp8enc.SourceImage, qIndex int, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int, flags EncodeFlags) (int, error) {
	return e.buildReconstructingInterFrameCoefficientsWithSegmentationMaybeThreaded(src, qIndex, vp8enc.SegmentationConfig{}, false, modes, coeffs, rows, cols, flags)
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficientsWithSegmentationMaybeThreaded(src vp8enc.SourceImage, qIndex int, segmentation vp8enc.SegmentationConfig, preserveSegmentID bool, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int, flags EncodeFlags) (int, error) {
	if !e.useThreadedInterFrameRows(rows, cols) {
		return e.buildReconstructingInterFrameCoefficientsWithSegmentation(src, qIndex, segmentation, preserveSegmentID, modes, coeffs, rows, cols, flags)
	}
	return e.buildReconstructingInterFrameCoefficientsWithSegmentationThreaded(src, qIndex, segmentation, preserveSegmentID, modes, coeffs, rows, cols, flags)
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficientsWithSegmentationThreaded(src vp8enc.SourceImage, qIndex int, segmentation vp8enc.SegmentationConfig, preserveSegmentID bool, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int, flags EncodeFlags) (int, error) {
	if qIndex < vp8common.MinQ || qIndex > vp8common.MaxQ {
		return 0, ErrInvalidConfig
	}
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	if traceEnabled {
		e.resetOracleMBTraceBuffer()
	}
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(e.reconstructModes) < required || len(e.reconstructTokens) < required {
		return 0, ErrInvalidConfig
	}
	// Lane D: the threaded path does not populate the per-frame coefficient
	// token caches (workers visit MBs in non-row-major order across
	// concurrent rows). Invalidate them so InterFramePacket.Write falls back
	// to its own count/token walks for the threaded case.
	e.interCoefTokenCountsValid = false
	e.interCoefTokenRecordsValid = false

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
	if traceEnabled {
		e.emitOracleLastRefWindow(&e.lastRef.Img)
	}
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	aboveTok := e.acquireReconstructAboveTok(cols)

	args := threadedInterRowsArgs{
		src:                    src,
		qIndex:                 qIndex,
		segmentation:           segmentation,
		preserveSegmentID:      preserveSegmentID,
		modes:                  modes,
		coeffs:                 coeffs,
		rows:                   rows,
		cols:                   cols,
		refs:                   refs,
		refCount:               refCount,
		quants:                 quants,
		aboveTok:               aboveTok,
		sourceAltRefZeroMVOnly: sourceAltRefZeroMVOnly,
	}
	threadedRate, err := e.buildReconstructingInterFrameCoefficientsThreaded(args)
	if err != nil {
		return 0, err
	}
	e.analysis.ExtendBorders()
	return threadedRate, nil
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
	traceEnabled := oracleTraceBuild && e.oracleTraceEnabled()
	if traceEnabled {
		// Reset oracle trace MB buffer at the start of each build pass so
		// retried attempts overwrite earlier rows.
		e.resetOracleMBTraceBuffer()
	}
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(e.reconstructModes) < required || len(e.reconstructTokens) < required {
		return 0, ErrInvalidConfig
	}
	// Lane D: accumulate the per-frame coefficient token counts during this
	// accepted-MB walk so InterFramePacket.Write can skip its own count pass.
	// Reset both the accumulator and the validity flag so a partial failure
	// downstream invalidates the cache.
	vp8enc.ResetInterCoefficientTokenCounts(&e.interCoefTokenCounts)
	e.interCoefTokenCountsValid = false
	vp8enc.ResetInterCoefficientTokenRecords(&e.interCoefTokenRecords, rows, required)
	e.interCoefTokenRecordsValid = false

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
	if traceEnabled {
		e.emitOracleLastRefWindow(&e.lastRef.Img)
	}
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	aboveTok := e.acquireReconstructAboveTok(cols)
	totalRate := 0
	totalPredictionError := int64(0)
	for row := range rows {
		vp8enc.MarkInterCoefficientTokenRecordRowStart(&e.interCoefTokenRecords, row)
		var leftTok vp8enc.TokenContextPlanes
		for col := range cols {
			index := row*cols + col
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
			var fallbackSnapshot interMacroblockImageSnapshot
			haveFallbackSnapshot := false
			// Snapshot only the reconstructed pixels for cyclic-refresh
			// quantizer fallback. Libvpx still keeps the picker-side
			// rd_thresh_mult / mode-test mutations from the original
			// segment-1 picker call, then clears segment_id after the picker
			// if the chosen mode is not LAST/ZEROMV.
			if segmentID != 0 {
				snapshotInterMacroblockImage(&e.analysis.Img, row, col, &fallbackSnapshot)
				haveFallbackSnapshot = true
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
			if !e.roi.enabled && segmentID != 0 && !decision.cyclicRefreshEligible() {
				if haveFallbackSnapshot {
					restoreInterMacroblockImage(&e.analysis.Img, row, col, &fallbackSnapshot)
				}
				segmentID = 0
				decision.interMode.SegmentID = 0
				decision.intraMode.SegmentID = 0
			}
			totalRate = addProjectedMacroblockRate(totalRate, decision.projectedRate)
			totalPredictionError += int64(decision.predictionError)
			segmentQIndex := encoderSegmentQIndex(qIndex, segmentation, segmentID)
			quant := &quants[segmentID]

			if decision.useIntra {
				modes[index] = decision.intraMode
				modes[index].SegmentID = segmentID
				convertInterFrameMode(&modes[index], &e.reconstructModes[index])
				if modes[index].Mode == vp8common.BPred {
					zbinOverQuant := e.rc.currentZbinOverQuant
					if e.activityMapValid {
						zbinOverQuant = e.tunedZbinOverQuant(zbinOverQuant, row, col)
					}
					if !buildReconstructingBPredMacroblockCoefficients(&e.coefProbs, src, row, col, &e.analysis.Img, &e.reconstructModes[index], &aboveTok[col], &leftTok, quant, segmentQIndex, zbinOverQuant, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), traceEnabled, &coeffs[index], &e.reconstructScratch) {
						return 0, ErrInvalidConfig
					}
					if oracleTraceBuild {
						applyOracleStaleY2Snapshot(&coeffs[index], decision.staleY2)
					}
				} else if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
			} else {
				modes[index] = decision.interMode
				convertInterFrameMode(&modes[index], &e.reconstructModes[index])
				predMode := e.reconstructModes[index]
				predMode.MBSkipCoeff = true
				if !reconstructInterAnalysisMacroblock(&e.analysis.Img, decision.ref.Img, row, col, &predMode, &e.reconstructTokens[index], &e.dequants[segmentID&3], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
				if traceEnabled {
					// Capture the inter predictor before residual is added.
					e.emitOracleInterPredictorTrace(row, col, &e.analysis.Img)
				}
			}
			breakoutSkip := modes[index].RefFrame != vp8common.IntraFrame &&
				(modes[index].MBSkipCoeff || staticInterRDEncodeBreakout(src, &e.analysis.Img, row, col, quant, e.interStaticThresholdForSegment(segmentID)))
			if breakoutSkip {
				clearMacroblockCoefficients(&coeffs[index])
			} else if modes[index].RefFrame != vp8common.IntraFrame || modes[index].Mode != vp8common.BPred {
				is4x4 := interFrameModeUses4x4Tokens(modes[index].Mode)
				// When the RD picker staged the winning candidate's
				// post-FDCT DCT inputs in the winner cache slot,
				// hand them in as cacheIn so the function skips
				// predictor + residual gather + FDCT. Parity is
				// validated by interRDCacheReusable inside
				// buildPredictedMacroblockCoefficients (mbRow/mbCol/
				// is4x4/intra/fastQuant/qIndex/zbin must all match).
				// The cache is consumed at most once per accepted MB
				// and invalidated by reset() before returning so the
				// next MB's picker run starts fresh.
				cacheIn := e.consumeInterRDCoeffCache()
				zbinOverQuant := e.rc.currentZbinOverQuant
				if e.activityMapValid {
					zbinOverQuant = e.tunedZbinOverQuant(zbinOverQuant, row, col)
				}
				buildPredictedMacroblockCoefficients(predictedMacroblockCoefficientArgs{
					coefProbs:     &e.coefProbs,
					src:           src,
					mbRow:         row,
					mbCol:         col,
					pred:          &e.analysis.Img,
					aboveTok:      &aboveTok[col],
					leftTok:       &leftTok,
					quant:         quant,
					qIndex:        segmentQIndex,
					zbinOverQuant: zbinOverQuant,
					zbinModeBoost: interZbinModeBoost(&modes[index]),
					is4x4:         is4x4,
					intra:         modes[index].RefFrame == vp8common.IntraFrame,
					fastQuant:     e.libvpxUseFastQuant(),
					optimize:      e.libvpxOptimizeCoefficients(),
					collectOracle: traceEnabled,
					coeffs:        &coeffs[index],
					cacheIn:       cacheIn,
					phaseStats:    e.opts.PhaseStats,
				})
				if is4x4 {
					if oracleTraceBuild {
						applyOracleStaleY2Snapshot(&coeffs[index], decision.staleY2)
					}
				}
			}
			is4x4 := interFrameModeUses4x4Tokens(modes[index].Mode)
			modes[index].MBSkipCoeff = breakoutSkip || macroblockCoefficientsEmpty(&coeffs[index], is4x4)
			// Lane C accepted-candidate reuse: the picker→accepted boundary
			// for an inter MB ends up calling convertInterFrameMode twice —
			// first at the winner branch above (lines 469/480) right after
			// `modes[index] = decision.{intra,inter}Mode`, then a second
			// time here after the MBSkipCoeff fix-up below. Between those
			// two calls the only field of modes[index] that mutates is
			// MBSkipCoeff (set on the line just above): every other input
			// to convertInterFrameMode (SegmentID, RefFrame, Mode, UVMode,
			// Is4x4, BModes, MV, BlockMV, Partition) is assigned exactly
			// once when `modes[index] = decision.{...}` runs and is not
			// touched by the intervening builders
			// (buildReconstructingBPredMacroblockCoefficients,
			// predictAnalysisMacroblock, reconstructInterAnalysisMacroblock
			// via a predMode stack copy, buildPredictedMacroblockCoefficients,
			// clearMacroblockCoefficients) — they read mode fields but never
			// write back into modes[index] or e.reconstructModes[index].
			// So patching MBSkipCoeff alone is byte-identical to the
			// previous full re-serialize, but skips an entire MacroblockMode
			// memset plus the per-MB [16]MotionVector BlockMV fill that the
			// compiler cannot prove dead.
			e.reconstructModes[index].MBSkipCoeff = modes[index].MBSkipCoeff
			convertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
			if modes[index].RefFrame == vp8common.IntraFrame && modes[index].Mode == vp8common.BPred {
				if err := updateInterAnalysisTokenContextAndCount(&e.interCoefTokenCounts, &e.interCoefTokenRecords, &aboveTok[col], &leftTok, is4x4, modes[index].MBSkipCoeff, &coeffs[index]); err != nil {
					return 0, ErrInvalidConfig
				}
				// B_PRED reconstruction was already written to
				// e.analysis.Img by buildReconstructingBPredMacroblockCoefficients
				// above, so emit the reconstructed trace here too. Without
				// this the predictor-diff harness silently skips B_PRED MBs
				// (e.g. the 4 right-edge col-7 B_PRED MBs on 128x128 frame
				// 1) and reports the visible Y as MATCH even when they
				// diverge.
				if traceEnabled {
					e.emitOracleInterReconstructedTrace(row, col, &e.analysis.Img)
					e.emitOracleMBTrace(row, col, &modes[index], &coeffs[index], decision.projectedRate, totalRate)
				}
				continue
			}
			if modes[index].RefFrame == vp8common.IntraFrame {
				if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID&3], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
			} else {
				if !addInterResidualToAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID&3], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
			}
			if err := updateInterAnalysisTokenContextAndCount(&e.interCoefTokenCounts, &e.interCoefTokenRecords, &aboveTok[col], &leftTok, is4x4, modes[index].MBSkipCoeff, &coeffs[index]); err != nil {
				return 0, ErrInvalidConfig
			}
			if traceEnabled {
				e.emitOracleInterReconstructedTrace(row, col, &e.analysis.Img)
				e.emitOracleMBTrace(row, col, &modes[index], &coeffs[index], decision.projectedRate, totalRate)
			}
		}
		vp8enc.MarkInterCoefficientTokenRecordRowEnd(&e.interCoefTokenRecords, row)
		vp8dec.ExtendIntraRightEdgeForRow(&e.analysis.Img, row)
	}
	e.analysis.ExtendBorders()
	e.framePredictionError = totalPredictionError
	// Lane D: cache is now fully populated for the consumer (packet writer).
	e.interCoefTokenCountsValid = true
	e.interCoefTokenRecordsValid = true
	if stats := e.opts.PhaseStats; stats != nil {
		stats.InterCoefTokenRecords += int64(len(e.interCoefTokenRecords.Records))
	}
	return totalRate, nil
}

func updateInterAnalysisTokenContext(above *vp8enc.TokenContextPlanes, left *vp8enc.TokenContextPlanes, is4x4 bool, skipped bool, coeffs *vp8enc.MacroblockCoefficients) {
	if skipped {
		vp8enc.ResetTokenContextPlanes(above, left, is4x4)
		return
	}
	vp8enc.UpdateTokenContextPlanesFromCoefficients(above, left, is4x4, coeffs)
}

// updateInterAnalysisTokenContextAndCount mirrors updateInterAnalysisTokenContext
// but also accumulates per-MB coefficient token counts into the encoder's
// Lane D cache so InterFramePacket.Write can skip its own count walk. The
// context-plane updates produced by AccumulateInterMacroblockTokenCounts are
// identical to UpdateTokenContextPlanesFromCoefficients for accepted MBs;
// skipped MBs reset the planes the same way the count walk does. Returns an
// error mirroring the count walk's validation so the caller can fail closed
// (the same way buildInterCoefficientTokenCounts would).
func updateInterAnalysisTokenContextAndCount(counts *vp8enc.InterCoefficientTokenCounts, records *vp8enc.InterCoefficientTokenRecords, above *vp8enc.TokenContextPlanes, left *vp8enc.TokenContextPlanes, is4x4 bool, skipped bool, coeffs *vp8enc.MacroblockCoefficients) error {
	if skipped {
		vp8enc.ResetTokenContextPlanes(above, left, is4x4)
		return nil
	}
	return vp8enc.AccumulateInterMacroblockTokenCountsAndRecords(counts, records, is4x4, above, left, coeffs)
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
