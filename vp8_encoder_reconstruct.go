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
	e.keyFrameCoefTokenCountsValid = false
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
	e.keyFrameCoefTokenCountsValid = false
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
	// libvpx-stale BLOCK->zbin_extra carry: even though
	// vp8/encoder/encodeframe.c vp8_activity_masking (line 433) runs at
	// the head of every MB iteration and adjust_act_zbin (line 318, via
	// vp8_activity_masking) updates x->act_zbin_adj to THIS MB's value
	// before the picker, the BLOCK->zbin_extra fields read by
	// vp8_regular_quantize_b_c (vp8_quantize.c line 75) are NOT
	// refreshed at that point unless segmentation is enabled
	// (encodeframe.c line 437 gates the vp8cx_mb_init_quantizer call).
	// With segmentation off (the default and the case for this fix),
	// b->zbin_extra is only re-derived by vp8_update_zbin_extra inside
	// vp8cx_encode_intra_macroblock (encodeframe.c line 1126), which
	// runs AFTER the picker has finished. As a result, each MB's
	// picker quantize step observes b->zbin_extra computed from the
	// PREVIOUS MB's post-pick x->act_zbin_adj
	// (ZBIN_EXTRA_Y = (Y1dequant[Q][1] * (zbin_over_quant +
	//                  zbin_mode_boost + act_zbin_adj)) >> 7 at
	//  vp8_quantize.c lines 276-279). Mirror that staleness here:
	// pickerActZbinAdj carries the previous MB's actZbinAdj forward
	// into the next MB's picker; tunedZbinAdjustment(row, col) still
	// supplies THIS MB's value for the encode-side reconstruct path.
	//
	// Initial value at MB(0,0) of an attempt: libvpx's
	// vp8cx_frame_init_quantizer (vp8_quantize.c line 433) runs at the
	// head of every vp8_encode_frame call and writes b->zbin_extra
	// using x->act_zbin_adj at that moment, which is the value left
	// over from the PREVIOUS attempt's last MB
	// (init_encode_frame_mb_context resets x->act_zbin_adj=0 only
	// AFTER vp8cx_frame_init_quantizer consumes it; across
	// vp8_encode_frame boundaries x->act_zbin_adj is never touched).
	// govpx already mirrors that carry in
	// e.activityProbeStaleActZbinAdj (vp8_encoder_tuning.go
	// captureActivityProbeStaleActZbinAdj sets it from
	// tunedZbinAdjustment(rows-1, cols-1) after each completed
	// attempt; vp8_encoder_lifecycle.go resets it to 0 at encoder reset).
	pickerActZbinAdj := e.activityProbeStaleActZbinAdj
	for row := range rows {
		var leftTok vp8enc.TokenContextPlanes
		for col := range cols {
			index := row*cols + col
			segmentID, ok := keyFrameAnalysisSegmentID(&modes[index], segmentation, preserveSegmentID)
			if !ok {
				return 0, ErrInvalidConfig
			}
			segmentQIndex := encoderSegmentQIndex(qIndex, segmentation, segmentID)
			zbinOverQuant := 0
			modeZbinOverQuant := zbinOverQuant
			actZbinAdj := 0
			// libvpx vp8/encoder/encodeframe.c:405-406 sets x->rdmult =
			// cpi->RDMULT once per MB, where cpi->RDMULT was computed
			// from cm->base_qindex (rdopt.c:163-174 vp8_initialize_rd_consts).
			// The per-MB ROI/cyclic-refresh segment delta-Q swaps quant
			// tables via vp8cx_mb_init_quantizer but does NOT mutate
			// x->rdmult, so the trellis optimizer (encodemb.c:187
			// optimize_b: rdmult = mb->rdmult * err_mult) scores with the
			// frame-level Q. Compute rdMult from qIndex (base), not from
			// segmentQIndex.
			rdMult, rdDiv := vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
			if e.activityMapValid {
				modeZbinOverQuant = e.tunedZbinOverQuant(zbinOverQuant, row, col)
				if adjustment, ok := e.tunedZbinAdjustment(row, col); ok {
					actZbinAdj = adjustment
				}
				rdMult = e.tunedRDMultiplier(rdMult, row, col)
			}
			// libvpx-stale picker actZbinAdj: pickerActZbinAdj is the
			// previous MB's post-pick adjustment, mirroring libvpx's
			// b->zbin_extra carry described in the variable-decl
			// comment above. Snapshot into a local so the post-iter
			// update (pickerActZbinAdj = actZbinAdj) doesn't race with
			// the in-iter reads.
			pickActZbinAdj := pickerActZbinAdj
			// libvpx vp8/encoder/encodeframe.c line 427-438: when
			// xd->segmentation_enabled is set (cyclic_refresh + KF, e.g.
			// --error-resilient=1 enabling cyclic_refresh_mode_enabled
			// via onyx_if.c line 1857), encode_mb_row calls
			// vp8cx_mb_init_quantizer(cpi, x, ok_to_skip=1) on every
			// MB BEFORE vp8cx_encode_intra_macroblock. With ok_to_skip=1
			// and QIndex unchanged (KF all-seg-0 under cyclic_refresh
			// — feature_data[MB_LVL_ALT_Q][0]=0 at onyx_if.c line 592),
			// the function takes the `else if` branch at
			// vp8_quantize.c line 387: it detects
			// `last_act_zbin_adj != x->act_zbin_adj` (just set by
			// vp8_activity_masking → adjust_act_zbin for THIS MB at
			// encodeframe.c line 423) and REWRITES block[i].zbin_extra
			// from THIS MB's act_zbin_adj. The picker
			// (vp8_rd_pick_intra_mode) then quantizes with the just-
			// refreshed zbin_extra — i.e., NOT the stale previous-MB
			// value. Mirror that path here: when segmentation is
			// enabled, the picker sees the current MB's actZbinAdj.
			// Closes task #262 (cohort: 19981bff, 22f3d67c, 788d442c,
			// 94eb71d5).
			if segmentation.Enabled {
				pickActZbinAdj = actZbinAdj
			}
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
			// libvpx vp8/encoder/encodeframe.c lines 1161-1180: the
			// `use_fastquant_for_pick` swap of x->quantize_b fires only in
			// vp8cx_encode_inter_macroblock; vp8cx_encode_intra_macroblock
			// leaves x->quantize_b at the speed-feature default (regular
			// when improved_quant==1, fast when improved_quant==0). So the
			// KF intra picker must use libvpxUseFastQuant (the encode-time
			// flag mirroring libvpx's default x->quantize_b), not
			// libvpxUseFastQuantForPick. Without this, good-quality
			// cpu_used >= 1 KFs pick coefficients with the fast quantizer
			// where libvpx still uses regular, causing single-MB mode
			// flips and a 1-byte first-partition drift on
			// good-quality-cbr-cpu2.
			if e.libvpxUseFastIntraPick() {
				mode, projectedRate, ok = predictBestKeyFrameIntraModeFastWithRDConstants(src, segmentQIndex, modeZbinOverQuant, row, col, above, left, &quants[segmentID&3], &e.analysis.Img, &e.reconstructScratch, e.libvpxUseFastQuant(), rdMult, rdDiv)
			} else {
				mode, projectedRate, ok = predictBestKeyFrameIntraModeWithRDConstants(src, segmentQIndex, zbinOverQuant, pickActZbinAdj, row, col, above, left, &aboveTok[col], &leftTok, &quants[segmentID&3], &e.analysis.Img, &e.reconstructScratch, e.libvpxUseFastQuant(), rdMult, rdDiv)
			}
			// Mirror libvpx encodeframe.c line 1106-1108:
			// adjust_act_zbin updates x->act_zbin_adj using THIS MB's
			// activity before the encode-side quantize runs. Seed
			// pickerActZbinAdj for the NEXT MB's picker.
			pickerActZbinAdj = actZbinAdj
			if !ok {
				return 0, ErrInvalidConfig
			}
			totalRate = addProjectedMacroblockRate(totalRate, projectedRate)
			mode.SegmentID = segmentID
			modes[index] = mode
			vp8enc.ConvertKeyFrameMode(&modes[index], &e.reconstructModes[index])
			if modes[index].YMode == vp8common.BPred {
				if !buildReconstructingBPredMacroblockCoefficients(&vp8tables.DefaultCoefProbs, src, row, col, &e.analysis.Img, &e.reconstructModes[index], &aboveTok[col], &leftTok, &quants[segmentID&3], segmentQIndex, zbinOverQuant, actZbinAdj, rdMult, rdDiv, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), traceEnabled, &coeffs[index], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
				modes[index].MBSkipCoeff = vp8enc.KeyFrameMacroblockIsSkippable(&modes[index], &coeffs[index])
				e.reconstructModes[index].MBSkipCoeff = modes[index].MBSkipCoeff
				vp8enc.ConvertMacroblockCoefficients(&coeffs[index], true, &e.reconstructTokens[index])
				if modes[index].MBSkipCoeff {
					vp8enc.ResetTokenContextPlanes(&aboveTok[col], &leftTok, true)
				} else {
					vp8enc.UpdateTokenContextPlanesFromCoefficients(&aboveTok[col], &leftTok, true, &coeffs[index])
				}
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
				quant:         &quants[segmentID&3],
				qIndex:        segmentQIndex,
				zbinOverQuant: zbinOverQuant,
				actZbinAdj:    actZbinAdj,
				rdMult:        rdMult,
				rdDiv:         rdDiv,
				is4x4:         is4x4,
				intra:         true,
				fastQuant:     e.libvpxUseFastQuant(),
				optimize:      e.libvpxOptimizeCoefficients(),
				collectOracle: traceEnabled,
				coeffs:        &coeffs[index],
				trace:         newPretrellisUVTrace(e),
			})
			modes[index].MBSkipCoeff = vp8enc.KeyFrameMacroblockIsSkippable(&modes[index], &coeffs[index])
			e.reconstructModes[index].MBSkipCoeff = modes[index].MBSkipCoeff
			vp8enc.ConvertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
			if modes[index].MBSkipCoeff {
				vp8enc.ResetTokenContextPlanes(&aboveTok[col], &leftTok, is4x4)
			} else {
				if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID&3], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
				vp8enc.UpdateTokenContextPlanesFromCoefficients(&aboveTok[col], &leftTok, is4x4, &coeffs[index])
			}
			if traceEnabled {
				e.emitOracleKeyFrameMBTrace(row, col, &modes[index], &coeffs[index], projectedRate, totalRate)
			}
		}
		vp8dec.ExtendIntraRightEdgeForRow(&e.analysis.Img, row)
	}
	e.analysis.ExtendBorders()
	e.lastKeyFrameReconstructWorkerCount = 1
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
	sourceAltRefZeroMVOnly := e.sourceAltRefZeroMVOnly(flags)
	if traceEnabled {
		e.emitOracleLastRefWindow(&e.lastRef.Img)
	}
	e.resetInterRDCoeffCache()
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	aboveTok := e.acquireReconstructAboveTok(cols)

	// Mirror the serial denoiser setup: when noise_sensitivity > 0 the
	// picker and per-MB reconstructor must read from coeffSource (the
	// denoiser's working-copy buffer the per-MB denoise overlay writes
	// into), not the raw frame. Without this the threaded path skipped
	// vp8_denoiser_denoise_mb entirely and the per-frame running_avg
	// stream drifted from libvpx starting at the first inter frame.
	denoiseActive := e.opts.NoiseSensitivity > 0 && e.denoiser.allocated
	coeffSource := src
	if denoiseActive {
		vp8enc.CopySourceToFrameBuffer(&e.denoiser.source, src)
		coeffSource = vp8enc.CodedSourceImageFromImage(&e.denoiser.source.Img)
	}

	args := threadedInterRowsArgs{
		src:                    src,
		coeffSource:            coeffSource,
		denoiseActive:          denoiseActive,
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
// via updateInterAnalysisTokenContextAndCount — mirroring libvpx's deferred
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
	e.interCoefTokenRecords.Reset(rows, required)
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
	sourceAltRefZeroMVOnly := e.sourceAltRefZeroMVOnly(flags)
	if traceEnabled {
		e.emitOracleLastRefWindow(&e.lastRef.Img)
	}
	e.resetInterRDCoeffCache()
	e.beginInterRDModeDecisionFrame()
	defer e.endInterRDModeDecisionFrame()
	aboveTok := e.acquireReconstructAboveTok(cols)
	denoiseActive := e.opts.NoiseSensitivity > 0 && e.denoiser.allocated
	coeffSource := src
	decisionSource := src
	if denoiseActive {
		vp8enc.CopySourceToFrameBuffer(&e.denoiser.source, src)
		coeffSource = vp8enc.CodedSourceImageFromImage(&e.denoiser.source.Img)
		decisionSource = coeffSource
	}
	totalRate := 0
	totalPredictionError := int64(0)
	for row := range rows {
		e.interCoefTokenRecords.MarkRowStart(row)
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
				decisionSource, refs[:], refCount,
				row, col, rows, cols,
				qIndex, segmentation, segmentID,
				above, left, aboveLeft,
				&aboveTok[col], &leftTok,
				&quants[segmentID&3],
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
			mbSource := src
			if denoiseActive {
				e.applyDenoiserToInterMacroblock(coeffSource, coeffSource, rows, cols, row, col, &decision)
				mbSource = coeffSource
			}
			if denoiseActive && decision.useIntra && !e.interAnalysisUsesRDModeDecision() && decision.intraMode.Mode <= vp8common.BPred {
				// The fast libvpx picker repeats pick_intra_mbuv_mode after
				// temporal denoising, so the accepted intra MB's chroma mode
				// is chosen against the source that will feed coefficients.
				if uvMode, _, ok := pickFastIntraChromaMode(mbSource, row, col, &e.analysis.Img, &e.reconstructScratch); ok {
					decision.intraMode.UVMode = uvMode
				}
			}
			projectedRate := int(decision.projectedRate)
			totalPredictionError += int64(decision.predictionError)
			segmentQIndex := encoderSegmentQIndex(qIndex, segmentation, segmentID)
			quant := &quants[segmentID&3]

			if decision.useIntra {
				modes[index] = decision.intraMode
				modes[index].SegmentID = segmentID
				vp8enc.ConvertInterFrameMode(&modes[index], &e.reconstructModes[index])
				if modes[index].Mode == vp8common.BPred {
					zbinOverQuant := e.rc.currentZbinOverQuant
					actZbinAdj := 0
					// libvpx encodeframe.c:405-406 + encodemb.c:187: optimize_b
					// (trellis) reads mb->rdmult = cpi->RDMULT computed once per
					// frame from cm->base_qindex (rdopt.c:163-174). ROI/cyclic
					// refresh segment delta-Q never mutates x->rdmult, so the
					// accepted-path rdMult uses qIndex (frame base), not the
					// segment-adjusted segmentQIndex. The pass-2 iiratio lift
					// at rdopt.c:189-196 also lands on cpi->RDMULT before
					// optimize_b consumes it; route through the encoder helper
					// so the lifted value propagates.
					rdMult, rdDiv := e.libvpxRDConstantsWithZbinForFrame(qIndex, zbinOverQuant)
					if e.activityMapValid {
						if adjustment, ok := e.tunedZbinAdjustment(row, col); ok {
							actZbinAdj = adjustment
						}
						rdMult = e.tunedRDMultiplier(rdMult, row, col)
					}
					if !buildReconstructingBPredMacroblockCoefficients(e.pickerCoefProbs(), mbSource, row, col, &e.analysis.Img, &e.reconstructModes[index], &aboveTok[col], &leftTok, quant, segmentQIndex, zbinOverQuant, actZbinAdj, rdMult, rdDiv, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), traceEnabled, &coeffs[index], &e.reconstructScratch) {
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
				vp8enc.ConvertInterFrameMode(&modes[index], &e.reconstructModes[index])
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
			// libvpx vp8/encoder/rdopt.c:1607-1635 runs encode_breakout
			// regardless of denoiser state — the denoiser fires AFTER best
			// mode is chosen (rdopt.c:2298, vp8_denoiser_denoise_mb) and
			// never resets x->skip. The previous denoiser guard hid a
			// genuine breakout-fires miss on static-thresh=1000+noise=6.
			staticBreakout := vp8enc.StaticInterRDEncodeBreakout(mbSource, &e.analysis.Img, row, col, quant, e.interStaticThresholdForSegment(segmentID))
			// libvpx vp8/encoder/encodeframe.c vp8cx_encode_inter_macroblock
			// (line 1275-1281): vp8_encode_inter16x16 runs whenever x->skip
			// is 0. libvpx sets x->skip = 1 in exactly two places inside
			// evaluate_inter_mode_rd:
			//   (1) rdopt.c:1607-1608 — active_map_enabled && active_ptr[0]==0
			//   (2) rdopt.c:1620-1628 — static encode_breakout (sse/var/uvsse
			//                            triple threshold) fires.
			// The picker's downstream mbmi.mb_skip_coeff signal (set from
			// tteob==0 rate accounting in calculate_final_rd_costs at
			// rdopt.c:1700) does NOT set x->skip and does NOT gate the
			// encode-side vp8_encode_inter16x16 rebuild. The encode-side
			// re-runs vp8_subtract_mb + transform_mb + vp8_quantize_mb +
			// optimize_mb with the regular quantizer (post-pick switch at
			// encodeframe.c:1176-1178) and trellis, which can yield non-
			// zero coefficients even when the picker's fastquant /
			// fastquant-for-pick reported tteob==0 (different b->zbin_extra
			// carry, trellis KEEP decisions on small-magnitude tokens,
			// SPLITMV per-subblock predictor rebuild). govpx's picker MB
			// MBSkipCoeff conflated the inactive-map case (real libvpx
			// x->skip=1) with the tteob==0 rate-adjustment case (libvpx
			// leaves x->skip=0). Mirror libvpx faithfully by gating
			// breakoutSkip only on the two real x->skip sources:
			// inactiveMB (active-map) and staticBreakout (encode_breakout).
			//
			// Closes the BestARNR -5 / GoodARNR -6 byte pin holds at
			// threads=1, threads=2, and threads=4 on the 19981bff /
			// 22f3d67c / 788d442c cohort (task #332). Discovery: at
			// threads=1, the entire picker scoreboard (mode/ref/mv) is
			// byte-identical to libvpx for all 3600 frame-1 MBs; the only
			// divergent state is MB(17,79) SPLITMV/LAST where govpx's
			// picker tteob==0 short-circuit zeroed the qcoeff while
			// libvpx's encode produced block-7 eob=13 from its regular
			// trellis-optimized rebuild.
			isInactiveMB := e.interMacroblockInactive(row, col, cols)
			breakoutSkip := modes[index].RefFrame != vp8common.IntraFrame &&
				(isInactiveMB || staticBreakout)
			if breakoutSkip {
				vp8enc.ClearMacroblockCoefficients(&coeffs[index])
			} else if modes[index].RefFrame != vp8common.IntraFrame || modes[index].Mode != vp8common.BPred {
				is4x4 := vp8enc.InterFrameModeUses4x4Tokens(modes[index].Mode)
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
				if denoiseActive {
					cacheIn = nil
				}
				zbinOverQuant := e.rc.currentZbinOverQuant
				actZbinAdj := 0
				// Same libvpx anchor as the BPred branch above: trellis
				// optimize_b uses mb->rdmult = cpi->RDMULT (frame-level),
				// so the rdMult fed into buildPredictedMacroblockCoefficients
				// uses qIndex (frame base) not segmentQIndex. The pass-2
				// iiratio lift at rdopt.c:189-196 lands on the same
				// cpi->RDMULT before optimize_b consumes it.
				rdMult, rdDiv := e.libvpxRDConstantsWithZbinForFrame(qIndex, zbinOverQuant)
				if e.activityMapValid {
					if adjustment, ok := e.tunedZbinAdjustment(row, col); ok {
						actZbinAdj = adjustment
					}
					rdMult = e.tunedRDMultiplier(rdMult, row, col)
				}
				var phaseStats *EncoderPhaseStats
				if vp8PhaseStatsEnabled {
					phaseStats = e.phaseStats()
				}
				buildPredictedMacroblockCoefficients(predictedMacroblockCoefficientArgs{
					coefProbs:     e.pickerCoefProbs(),
					src:           mbSource,
					mbRow:         row,
					mbCol:         col,
					pred:          &e.analysis.Img,
					aboveTok:      &aboveTok[col],
					leftTok:       &leftTok,
					quant:         quant,
					qIndex:        segmentQIndex,
					zbinOverQuant: zbinOverQuant,
					zbinModeBoost: vp8enc.InterZbinModeBoost(&modes[index]),
					actZbinAdj:    actZbinAdj,
					rdMult:        rdMult,
					rdDiv:         rdDiv,
					is4x4:         is4x4,
					intra:         modes[index].RefFrame == vp8common.IntraFrame,
					fastQuant:     e.libvpxUseFastQuant(),
					optimize:      e.libvpxOptimizeCoefficients(),
					collectOracle: traceEnabled,
					coeffs:        &coeffs[index],
					cacheIn:       cacheIn,
					phaseStats:    phaseStats,
					trace:         newPretrellisUVTrace(e),
				})
				if is4x4 {
					if oracleTraceBuild {
						applyOracleStaleY2Snapshot(&coeffs[index], decision.staleY2)
					}
				}
			}
			is4x4 := vp8enc.InterFrameModeUses4x4Tokens(modes[index].Mode)
			finalCoeffSkip := false
			if !breakoutSkip {
				finalCoeffSkip = vp8enc.MacroblockCoefficientsEmpty(&coeffs[index], is4x4)
			}
			modes[index].MBSkipCoeff = breakoutSkip || finalCoeffSkip
			// Lane C accepted-candidate reuse: the picker→accepted boundary
			// for an inter MB ends up calling vp8enc.ConvertInterFrameMode twice —
			// first at the winner branch above (lines 469/480) right after
			// `modes[index] = decision.{intra,inter}Mode`, then a second
			// time here after the MBSkipCoeff fix-up below. Between those
			// two calls the only field of modes[index] that mutates is
			// MBSkipCoeff (set on the line just above): every other input
			// to vp8enc.ConvertInterFrameMode (SegmentID, RefFrame, Mode, UVMode,
			// Is4x4, BModes, MV, BlockMV, Partition) is assigned exactly
			// once when `modes[index] = decision.{...}` runs and is not
			// touched by the intervening builders
			// (buildReconstructingBPredMacroblockCoefficients,
			// predictAnalysisMacroblock, reconstructInterAnalysisMacroblock
			// via a predMode stack copy, buildPredictedMacroblockCoefficients,
			// vp8enc.ClearMacroblockCoefficients) — they read mode fields but never
			// write back into modes[index] or e.reconstructModes[index].
			// So patching MBSkipCoeff alone is byte-identical to the
			// previous full re-serialize, but skips an entire MacroblockMode
			// memset plus the per-MB [16]MotionVector BlockMV fill that the
			// compiler cannot prove dead.
			e.reconstructModes[index].MBSkipCoeff = modes[index].MBSkipCoeff
			if !modes[index].MBSkipCoeff {
				vp8enc.ConvertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
			}
			totalRate = addProjectedMacroblockRate(totalRate, projectedRate)
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
					e.emitOracleMBTrace(row, col, &modes[index], &coeffs[index], decision.improvedMVStart, projectedRate, totalRate)
				}
				continue
			}
			if modes[index].RefFrame == vp8common.IntraFrame {
				if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID&3], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
			} else if !modes[index].MBSkipCoeff {
				if !addInterResidualToAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID&3], &e.reconstructScratch) {
					return 0, ErrInvalidConfig
				}
			}
			if err := updateInterAnalysisTokenContextAndCount(&e.interCoefTokenCounts, &e.interCoefTokenRecords, &aboveTok[col], &leftTok, is4x4, modes[index].MBSkipCoeff, &coeffs[index]); err != nil {
				return 0, ErrInvalidConfig
			}
			if traceEnabled {
				e.emitOracleInterReconstructedTrace(row, col, &e.analysis.Img)
				e.emitOracleMBTrace(row, col, &modes[index], &coeffs[index], decision.improvedMVStart, projectedRate, totalRate)
			}
		}
		e.interCoefTokenRecords.MarkRowEnd(row)
		vp8dec.ExtendIntraRightEdgeForRow(&e.analysis.Img, row)
	}
	e.analysis.ExtendBorders()
	e.framePredictionError = totalPredictionError
	// Lane D: cache is now fully populated for the consumer (packet writer).
	e.interCoefTokenCountsValid = true
	e.interCoefTokenRecordsValid = true
	if vp8PhaseStatsEnabled {
		if stats := e.phaseStats(); stats != nil {
			stats.InterCoefTokenRecords += int64(len(e.interCoefTokenRecords.Records))
		}
	}
	e.lastInterReconstructWorkerCount = 1
	return totalRate, nil
}

// updateInterAnalysisTokenContextAndCount updates inter token contexts and
// accumulates per-MB coefficient token counts into the encoder's Lane D cache
// so InterFramePacket.Write can skip its own count walk. The context-plane
// updates produced by AccumulateInterMacroblockTokenCounts are identical to
// UpdateTokenContextPlanesFromCoefficients for accepted MBs; skipped MBs reset
// the planes the same way the count walk does. Returns an error mirroring the
// count walk's validation so the caller can fail closed.
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
