package govpx

import (
	"sync/atomic"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

type threadedInterRowsArgs struct {
	src                    vp8enc.SourceImage
	coeffSource            vp8enc.SourceImage
	denoiseActive          bool
	qIndex                 int
	segmentation           vp8enc.SegmentationConfig
	preserveSegmentID      bool
	modes                  []vp8enc.InterFrameMacroblockMode
	coeffs                 []vp8enc.MacroblockCoefficients
	rows                   int
	cols                   int
	refs                   [3]interAnalysisReference
	refCount               int
	quants                 [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	aboveTok               []vp8enc.TokenContextPlanes
	sourceAltRefZeroMVOnly bool
}

type threadedKeyRowsArgs struct {
	src               vp8enc.SourceImage
	qIndex            int
	segmentation      vp8enc.SegmentationConfig
	preserveSegmentID bool
	modes             []vp8enc.KeyFrameMacroblockMode
	coeffs            []vp8enc.MacroblockCoefficients
	rows              int
	cols              int
	quants            [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	aboveTok          []vp8enc.TokenContextPlanes
}

func (e *VP8Encoder) useThreadedKeyFrameRows(rows int, cols int) bool {
	pool := e.rowWorkers
	// libvpx threads the keyframe encode whenever multi_threaded > 1
	// regardless of noise_sensitivity (the denoiser only fires on inter
	// frames per vp8/encoder/onyx_if.c key-frame guard); gating govpx on
	// NoiseSensitivity == 0 silently kept the keyframe serial and the
	// resulting first partition diverged at byte 273 from the libvpx
	// threaded keyframe even though the denoiser itself is inactive.
	return pool != nil &&
		len(pool.workers) > 1 &&
		rows > 1 &&
		cols > pool.syncRange &&
		(!oracleTraceBuild || !e.oracleTraceEnabled())
}

func (e *VP8Encoder) useThreadedInterFrameRows(rows int, cols int) bool {
	pool := e.rowWorkers
	return pool != nil &&
		len(pool.workers) > 1 &&
		rows > 1 &&
		cols > pool.syncRange &&
		(!oracleTraceBuild || !e.oracleTraceEnabled())
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficientsThreaded(args threadedInterRowsArgs) (int, error) {
	pool := e.rowWorkers
	if pool == nil || args.rows <= 1 || args.cols <= 0 {
		return 0, ErrInvalidConfig
	}
	required := args.rows * args.cols
	workerCount := min(len(pool.workers), args.rows)
	if maxWavefrontWorkers := args.cols / pool.syncRange; maxWavefrontWorkers > 0 {
		workerCount = min(workerCount, maxWavefrontWorkers)
	}
	if workerCount < 2 {
		return 0, ErrInvalidConfig
	}

	pool.reset(args.rows)
	if len(e.dotArtifactChecked) >= required {
		clear(e.dotArtifactChecked[:required])
	}

	pool.encoder = e
	pool.job = rowWorkerJobInterFrame
	pool.args = args
	pool.workerCount = workerCount
	pool.required = required
	pool.abort.Store(0)
	for workerIndex := range workerCount {
		pool.workerErrors[workerIndex] = nil
	}

	pool.startHelperWorkers()
	pool.runThreadedInterFrameWorker(0)

	var firstErr error
	if err := pool.workerErrors[0]; err != nil {
		firstErr = err
		pool.abort.Store(1)
	}
	pool.waitHelperWorkers()
	for workerIndex := 1; workerIndex < workerCount; workerIndex++ {
		if err := pool.workerErrors[workerIndex]; err != nil && firstErr == nil {
			firstErr = err
			pool.abort.Store(1)
		}
	}
	if firstErr != nil {
		pool.encoder = nil
		pool.job = rowWorkerJobInterFrame
		pool.args = threadedInterRowsArgs{}
		return 0, firstErr
	}
	if pool.abort.Load() != 0 {
		pool.encoder = nil
		pool.job = rowWorkerJobInterFrame
		pool.args = threadedInterRowsArgs{}
		return 0, ErrInvalidConfig
	}

	pool.mergeThreadedInterFrameState(e, workerCount, required)
	pool.mergeThreadedInterFrameCoefCounts(e, workerCount)
	pool.mergeThreadedInterFrameCoefRecords(e, workerCount, args.rows, args.cols, required)
	e.lastInterReconstructWorkerCount = workerCount
	totalRate := 0
	totalPredictionError := int64(0)
	for workerIndex := range workerCount {
		totalRate = addProjectedMacroblockRate(totalRate, pool.workers[workerIndex].totalRate)
		totalPredictionError += pool.workers[workerIndex].totalPredictionError
	}
	e.framePredictionError = totalPredictionError
	pool.encoder = nil
	pool.job = rowWorkerJobInterFrame
	pool.args = threadedInterRowsArgs{}
	return totalRate, nil
}

func (e *VP8Encoder) buildReconstructingKeyFrameCoefficientsThreaded(args threadedKeyRowsArgs) (int, error) {
	pool := e.rowWorkers
	if pool == nil || args.rows <= 1 || args.cols <= 0 {
		return 0, ErrInvalidConfig
	}
	required := args.rows * args.cols
	workerCount := min(len(pool.workers), args.rows)
	if maxWavefrontWorkers := args.cols / pool.syncRange; maxWavefrontWorkers > 0 {
		workerCount = min(workerCount, maxWavefrontWorkers)
	}
	if workerCount < 2 {
		return 0, ErrInvalidConfig
	}

	pool.reset(args.rows)
	pool.encoder = e
	pool.job = rowWorkerJobKeyFrame
	pool.keyArgs = args
	pool.workerCount = workerCount
	pool.required = required
	pool.abort.Store(0)
	for workerIndex := range workerCount {
		pool.workerErrors[workerIndex] = nil
	}

	pool.startHelperWorkers()
	pool.runThreadedKeyFrameWorker(0)

	var firstErr error
	if err := pool.workerErrors[0]; err != nil {
		firstErr = err
		pool.abort.Store(1)
	}
	pool.waitHelperWorkers()
	for workerIndex := 1; workerIndex < workerCount; workerIndex++ {
		if err := pool.workerErrors[workerIndex]; err != nil && firstErr == nil {
			firstErr = err
			pool.abort.Store(1)
		}
	}
	if firstErr != nil {
		pool.encoder = nil
		pool.job = rowWorkerJobInterFrame
		pool.keyArgs = threadedKeyRowsArgs{}
		return 0, firstErr
	}
	if pool.abort.Load() != 0 {
		pool.encoder = nil
		pool.job = rowWorkerJobInterFrame
		pool.keyArgs = threadedKeyRowsArgs{}
		return 0, ErrInvalidConfig
	}

	totalRate := 0
	for workerIndex := range workerCount {
		totalRate = addProjectedMacroblockRate(totalRate, pool.workers[workerIndex].totalRate)
	}
	pool.mergeThreadedKeyFrameCoefCounts(e, workerCount)
	pool.encoder = nil
	pool.job = rowWorkerJobInterFrame
	pool.keyArgs = threadedKeyRowsArgs{}
	e.lastKeyFrameReconstructWorkerCount = workerCount
	return totalRate, nil
}

func (rs *rowEncoderState) encodeThreadedKeyFrameRow(pool *rowWorkerPool, args *threadedKeyRowsArgs, row int, abort *atomic.Int32) (int, error) {
	rs.rowIndex = row
	rs.leftTok = vp8enc.TokenContextPlanes{}
	// rs.pickerActZbinAdj is NOT reset here: libvpx's encode_mb_row
	// (encodeframe.c:316-575) and helper thread loop (ethreading.c:76-310)
	// only re-zero cm->left_context / mb_row_left_context at row entry —
	// neither x->act_zbin_adj nor block[i].zbin_extra is touched between
	// rows. So within a thread, the stale-zbin carry survives the
	// row→row transition: MB(next_row, 0)'s picker sees b->zbin_extra
	// derived from MB(prev_row, mb_cols-1)'s post-pick act_zbin_adj.
	// runThreadedKeyFrameWorker seeds rs.pickerActZbinAdj once per worker
	// dispatch: activityProbeStaleActZbinAdj for workerIndex==0 (mirrors
	// libvpx's main thread, whose b->zbin_extra was set by
	// vp8cx_frame_init_quantizer with the prev-attempt's stale value), and
	// 0 for workerIndex>0 (mirrors helper threads' zero-init MB_ROW_COMP
	// block[i].zbin_extra at vp8cx_create_encoder_threads).
	rowRate := 0
	// Mirrors libvpx vp8/encoder/ethreading.c: publish at trigger
	// `(mb_col-1)%nsync == 0` (col ∈ {1, 1+nsync, 1+2*nsync, ...})
	// with value `mb_col-1`, and wait at trigger `mb_col%nsync == 0`
	// (col ∈ {0, nsync, 2*nsync, ...}) for above to satisfy
	// `current_mb_col[above] >= mb_col + nsync`. The +nsync target
	// accounts for the 4 above-right pixels of MB(row-1, col+1) that
	// intra prediction reads.
	for col := range args.cols {
		if col > 0 && (col-1)%pool.syncRange == 0 {
			pool.publishRowColumn(row, col-1)
		}
		if col%pool.syncRange == 0 {
			if !pool.waitForAboveColumnAbort(row, col+pool.syncRange, abort) {
				return rowRate, nil
			}
		}
		rate, err := rs.encodeThreadedKeyFrameMacroblock(args, row, col)
		if err != nil {
			return 0, err
		}
		rowRate = addProjectedMacroblockRate(rowRate, rate)
	}
	vp8dec.ExtendIntraRightEdgeForRow(&rs.enc.analysis.Img, row)
	// Post-row store at `cols + nsync` so the last syncRange MBs of
	// the row below see a value beyond any in-row target. Matches
	// libvpx's `vpx_atomic_store_release(current_mb_col, mb_col + nsync)`
	// at end-of-row.
	pool.publishRowColumn(row, args.cols+pool.syncRange)
	return rowRate, nil
}

func (rs *rowEncoderState) encodeThreadedKeyFrameMacroblock(args *threadedKeyRowsArgs, row int, col int) (int, error) {
	e := &rs.enc
	index := row*args.cols + col
	segmentID, ok := keyFrameAnalysisSegmentID(&args.modes[index], args.segmentation, args.preserveSegmentID)
	if !ok {
		return 0, ErrInvalidConfig
	}
	segmentQIndex := encoderSegmentQIndex(args.qIndex, args.segmentation, segmentID)
	zbinOverQuant := 0
	modeZbinOverQuant := zbinOverQuant
	actZbinAdj := 0
	// libvpx vp8/encoder/encodeframe.c:405-406 sets x->rdmult = cpi->RDMULT
	// once per MB (cpi->RDMULT computed from cm->base_qindex per
	// rdopt.c:163-174). The per-MB segment delta-Q swap in
	// vp8cx_mb_init_quantizer never touches x->rdmult, so the trellis
	// optimize_b (encodemb.c:187) scores with the frame-level Q. Use
	// args.qIndex (frame base), not segmentQIndex, for the rdMult input.
	rdMult, rdDiv := vp8enc.RDConstantsWithZbin(args.qIndex, zbinOverQuant)
	if e.activityMapValid {
		modeZbinOverQuant = e.tunedZbinOverQuant(zbinOverQuant, row, col)
		if adjustment, ok := e.tunedZbinAdjustment(row, col); ok {
			actZbinAdj = adjustment
		}
		rdMult = e.tunedRDMultiplier(rdMult, row, col)
	}
	var above *vp8enc.KeyFrameMacroblockMode
	var left *vp8enc.KeyFrameMacroblockMode
	if row > 0 {
		above = &args.modes[index-args.cols]
	}
	if col > 0 {
		left = &args.modes[index-1]
	}
	var mode vp8enc.KeyFrameMacroblockMode
	var projectedRate int
	// See vp8_encoder_reconstruct.go: libvpx's use_fastquant_for_pick only
	// swaps x->quantize_b in the inter macroblock path; KF intra picking
	// uses the speed-feature default (regular when improved_quant==1).
	// Match that here with libvpxUseFastQuant.
	if e.libvpxUseFastIntraPick() {
		mode, projectedRate, ok = predictBestKeyFrameIntraModeFastWithRDConstants(args.src, segmentQIndex, modeZbinOverQuant, row, col, above, left, &args.quants[segmentID&3], &e.analysis.Img, &e.reconstructScratch, e.libvpxUseFastQuant(), rdMult, rdDiv)
	} else {
		// libvpx-stale picker actZbinAdj: pickerActZbinAdj holds the
		// previous MB's post-pick value (or 0 at row start). The per-MB
		// actZbinAdj computed above seeds pickerActZbinAdj for the next
		// MB after this picker returns. See vp8_encoder_reconstruct.go for
		// the libvpx anchor.
		//
		// libvpx vp8/encoder/encodeframe.c line 427-438: when
		// xd->segmentation_enabled is set (cyclic refresh + KF, e.g.
		// --error-resilient=1 enabling cyclic_refresh_mode_enabled via
		// onyx_if.c line 1857), encode_mb_row calls
		// vp8cx_mb_init_quantizer(cpi, x, ok_to_skip=1) on every MB
		// BEFORE the picker. With QIndex unchanged (KF all seg 0 since
		// feature_data[MB_LVL_ALT_Q][0]=0 at cyclic_background_refresh
		// onyx_if.c line 592), the function takes the `else if` branch
		// at vp8_quantize.c line 387, detects
		// last_act_zbin_adj != act_zbin_adj (just set by
		// vp8_activity_masking → adjust_act_zbin at encodeframe.c
		// line 423 for THIS MB), and REWRITES block[i].zbin_extra from
		// THIS MB's act_zbin_adj. The picker then quantizes with the
		// just-refreshed value, NOT the stale prev-MB value. Mirror
		// that here. Closes task #262.
		pickActZbinAdj := rs.pickerActZbinAdj
		if args.segmentation.Enabled {
			pickActZbinAdj = actZbinAdj
		}
		mode, projectedRate, ok = predictBestKeyFrameIntraModeWithRDConstants(args.src, segmentQIndex, zbinOverQuant, pickActZbinAdj, row, col, above, left, &args.aboveTok[col], &rs.leftTok, &args.quants[segmentID&3], &e.analysis.Img, &e.reconstructScratch, e.libvpxUseFastQuant(), rdMult, rdDiv)
	}
	// Mirror libvpx encodeframe.c line 1106-1108 adjust_act_zbin:
	// seed pickerActZbinAdj for the NEXT MB's picker now that we have
	// THIS MB's activity-driven adjustment.
	rs.pickerActZbinAdj = actZbinAdj
	if !ok {
		return 0, ErrInvalidConfig
	}
	mode.SegmentID = segmentID
	args.modes[index] = mode
	vp8enc.ConvertKeyFrameMode(&args.modes[index], &e.reconstructModes[index])
	if args.modes[index].YMode == vp8common.BPred {
		if !buildReconstructingBPredMacroblockCoefficients(&vp8tables.DefaultCoefProbs, args.src, row, col, &e.analysis.Img, &e.reconstructModes[index], &args.aboveTok[col], &rs.leftTok, &args.quants[segmentID&3], segmentQIndex, zbinOverQuant, actZbinAdj, rdMult, rdDiv, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), false, &args.coeffs[index], &e.reconstructScratch) {
			return 0, ErrInvalidConfig
		}
		args.modes[index].MBSkipCoeff = vp8enc.KeyFrameMacroblockIsSkippable(&args.modes[index], &args.coeffs[index])
		e.reconstructModes[index].MBSkipCoeff = args.modes[index].MBSkipCoeff
		vp8enc.ConvertMacroblockCoefficients(&args.coeffs[index], true, &e.reconstructTokens[index])
		if err := rs.updateThreadedKeyFrameTokenContextAndCount(&args.aboveTok[col], &rs.leftTok, true, args.modes[index].MBSkipCoeff, &args.coeffs[index]); err != nil {
			return 0, err
		}
		return projectedRate, nil
	}
	if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
		return 0, ErrInvalidConfig
	}
	is4x4 := args.modes[index].YMode == vp8common.BPred
	buildPredictedMacroblockCoefficients(predictedMacroblockCoefficientArgs{
		coefProbs:     &vp8tables.DefaultCoefProbs,
		src:           args.src,
		mbRow:         row,
		mbCol:         col,
		pred:          &e.analysis.Img,
		aboveTok:      &args.aboveTok[col],
		leftTok:       &rs.leftTok,
		quant:         &args.quants[segmentID&3],
		qIndex:        segmentQIndex,
		zbinOverQuant: zbinOverQuant,
		actZbinAdj:    actZbinAdj,
		rdMult:        rdMult,
		rdDiv:         rdDiv,
		is4x4:         is4x4,
		intra:         true,
		fastQuant:     e.libvpxUseFastQuant(),
		optimize:      e.libvpxOptimizeCoefficients(),
		collectOracle: false,
		coeffs:        &args.coeffs[index],
	})
	args.modes[index].MBSkipCoeff = vp8enc.KeyFrameMacroblockIsSkippable(&args.modes[index], &args.coeffs[index])
	e.reconstructModes[index].MBSkipCoeff = args.modes[index].MBSkipCoeff
	vp8enc.ConvertMacroblockCoefficients(&args.coeffs[index], is4x4, &e.reconstructTokens[index])
	if args.modes[index].MBSkipCoeff {
		vp8enc.ResetTokenContextPlanes(&args.aboveTok[col], &rs.leftTok, is4x4)
	} else {
		if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID&3], &e.reconstructScratch) {
			return 0, ErrInvalidConfig
		}
		if err := rs.accumulateThreadedKeyFrameCoefCounts(is4x4, &args.aboveTok[col], &rs.leftTok, &args.coeffs[index]); err != nil {
			return 0, err
		}
		vp8enc.UpdateTokenContextPlanesFromCoefficients(&args.aboveTok[col], &rs.leftTok, is4x4, &args.coeffs[index])
	}
	return projectedRate, nil
}

func (rs *rowEncoderState) accumulateThreadedKeyFrameCoefCounts(is4x4 bool, above *vp8enc.TokenContextPlanes, left *vp8enc.TokenContextPlanes, coeffs *vp8enc.MacroblockCoefficients) error {
	aboveCopy := *above
	leftCopy := *left
	return vp8enc.AccumulateInterMacroblockTokenCountsAndRecords(&rs.keyFrameCoefTokenCounts, nil, is4x4, &aboveCopy, &leftCopy, coeffs)
}

func (rs *rowEncoderState) updateThreadedKeyFrameTokenContextAndCount(above *vp8enc.TokenContextPlanes, left *vp8enc.TokenContextPlanes, is4x4 bool, skipped bool, coeffs *vp8enc.MacroblockCoefficients) error {
	if skipped {
		vp8enc.ResetTokenContextPlanes(above, left, is4x4)
		return nil
	}
	if err := rs.accumulateThreadedKeyFrameCoefCounts(is4x4, above, left, coeffs); err != nil {
		return err
	}
	vp8enc.UpdateTokenContextPlanesFromCoefficients(above, left, is4x4, coeffs)
	return nil
}

func (rs *rowEncoderState) encodeThreadedInterFrameRow(pool *rowWorkerPool, args *threadedInterRowsArgs, row int, abort *atomic.Int32) (int, error) {
	rs.rowIndex = row
	rs.leftTok = vp8enc.TokenContextPlanes{}
	rowRate := 0
	rowPredictionError := int64(0)
	rs.interCoefTokenRecords.MarkRowStart(row)
	for col := range args.cols {
		if col > 0 && (col-1)%pool.syncRange == 0 {
			pool.publishRowColumn(row, col-1)
		}
		if col%pool.syncRange == 0 {
			if !pool.waitForAboveColumnAbort(row, col+pool.syncRange, abort) {
				return rowRate, nil
			}
		}
		rate, predictionError, err := rs.encodeThreadedInterFrameMacroblock(args, row, col)
		if err != nil {
			return 0, err
		}
		rowRate = addProjectedMacroblockRate(rowRate, rate)
		rowPredictionError += predictionError
	}
	rs.totalPredictionError += rowPredictionError
	rs.interCoefTokenRecords.MarkRowEnd(row)
	vp8dec.ExtendIntraRightEdgeForRow(&rs.enc.analysis.Img, row)
	pool.publishRowColumn(row, args.cols+pool.syncRange)
	return rowRate, nil
}

func (rs *rowEncoderState) encodeThreadedInterFrameMacroblock(args *threadedInterRowsArgs, row int, col int) (int, int64, error) {
	e := &rs.enc
	index := row*args.cols + col

	segmentID, ok := interFrameAnalysisSegmentID(&args.modes[index], args.segmentation, args.preserveSegmentID)
	if !ok {
		return 0, 0, ErrInvalidConfig
	}
	var above *vp8enc.InterFrameMacroblockMode
	var left *vp8enc.InterFrameMacroblockMode
	var aboveLeft *vp8enc.InterFrameMacroblockMode
	if row > 0 {
		above = &args.modes[index-args.cols]
	}
	if col > 0 {
		left = &args.modes[index-1]
	}
	if row > 0 && col > 0 {
		aboveLeft = &args.modes[index-args.cols-1]
	}

	e.beginInterRDModeDecisionMacroblock()
	var fallbackSnapshot interMacroblockImageSnapshot
	haveFallbackSnapshot := false
	if segmentID != 0 {
		snapshotInterMacroblockImage(&e.analysis.Img, row, col, &fallbackSnapshot)
		haveFallbackSnapshot = true
	}
	// Pick using coeffSource (the denoiser working-copy) when the
	// denoiser is active so the picker observes the same per-MB
	// denoised left-edge / above-edge pixel context as libvpx's per-row
	// encode_mb_row loop. Each row worker owns disjoint MB regions and
	// the wave-front sync below already guarantees row r-1's denoiser
	// writes complete before row r reads them, so the per-MB denoiser
	// overlay is byte-identical to the serial walk.
	pickSource := args.src
	if args.denoiseActive {
		pickSource = args.coeffSource
	}
	decision, ok := e.selectInterFrameModeDecision(
		pickSource, args.refs[:], args.refCount,
		row, col, args.rows, args.cols,
		args.qIndex, args.segmentation, segmentID,
		above, left, aboveLeft,
		&args.aboveTok[col], &rs.leftTok,
		&args.quants[segmentID&3],
		args.sourceAltRefZeroMVOnly,
	)
	if !ok {
		return 0, 0, ErrInvalidConfig
	}
	if !e.roi.enabled && segmentID != 0 && !decision.cyclicRefreshEligible() {
		if haveFallbackSnapshot {
			restoreInterMacroblockImage(&e.analysis.Img, row, col, &fallbackSnapshot)
		}
		segmentID = 0
		decision.interMode.SegmentID = 0
		decision.intraMode.SegmentID = 0
	}

	mbSource := args.src
	if args.denoiseActive {
		// vp8_denoiser_denoise_mb overlays the denoised pixels into
		// coeffSource and updates the per-MB running_avg buffers. The
		// row worker's encoder view shares the encoder-level denoiser
		// state (slices alias the backing arrays), so writes here land
		// in the shared per-MB region just like libvpx's threaded
		// encode_mb_row.
		e.applyDenoiserToInterMacroblock(args.coeffSource, args.coeffSource, args.rows, args.cols, row, col, &decision)
		mbSource = args.coeffSource
	}
	if args.denoiseActive && decision.useIntra && !e.interAnalysisUsesRDModeDecision() && decision.intraMode.Mode <= vp8common.BPred {
		if uvMode, _, ok := pickFastIntraChromaMode(mbSource, row, col, &e.analysis.Img, &e.reconstructScratch); ok {
			decision.intraMode.UVMode = uvMode
		}
	}

	segmentQIndex := encoderSegmentQIndex(args.qIndex, args.segmentation, segmentID)
	quant := &args.quants[segmentID&3]
	if decision.useIntra {
		args.modes[index] = decision.intraMode
		args.modes[index].SegmentID = segmentID
		vp8enc.ConvertInterFrameMode(&args.modes[index], &e.reconstructModes[index])
		if args.modes[index].Mode == vp8common.BPred {
			zbinOverQuant := e.rc.currentZbinOverQuant
			actZbinAdj := 0
			// libvpx encodeframe.c:405-406 + encodemb.c:187 (optimize_b):
			// the trellis reads mb->rdmult = cpi->RDMULT (frame-level).
			// ROI/cyclic refresh segment delta-Q never mutates x->rdmult,
			// so rdMult derives from args.qIndex (base), not segmentQIndex.
			// The pass-2 iiratio lift (rdopt.c:189-196) lands on cpi->RDMULT
			// before optimize_b consumes it; route through the encoder helper.
			rdMult, rdDiv := e.libvpxRDConstantsWithZbinForFrame(args.qIndex, zbinOverQuant)
			if e.activityMapValid {
				if adjustment, ok := e.tunedZbinAdjustment(row, col); ok {
					actZbinAdj = adjustment
				}
				rdMult = e.tunedRDMultiplier(rdMult, row, col)
			}
			if !buildReconstructingBPredMacroblockCoefficients(e.pickerCoefProbs(), mbSource, row, col, &e.analysis.Img, &e.reconstructModes[index], &args.aboveTok[col], &rs.leftTok, quant, segmentQIndex, zbinOverQuant, actZbinAdj, rdMult, rdDiv, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), false, &args.coeffs[index], &e.reconstructScratch) {
				return 0, 0, ErrInvalidConfig
			}
		} else if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
			return 0, 0, ErrInvalidConfig
		}
	} else {
		args.modes[index] = decision.interMode
		vp8enc.ConvertInterFrameMode(&args.modes[index], &e.reconstructModes[index])
		predMode := e.reconstructModes[index]
		predMode.MBSkipCoeff = true
		if !reconstructInterAnalysisMacroblock(&e.analysis.Img, decision.ref.Img, row, col, &predMode, &e.reconstructTokens[index], &e.dequants[segmentID&3], &e.reconstructScratch) {
			return 0, 0, ErrInvalidConfig
		}
	}

	// libvpx vp8/encoder/rdopt.c:1607-1635 runs encode_breakout regardless
	// of denoiser state — the denoiser fires AFTER best mode is chosen
	// (rdopt.c:2298, vp8_denoiser_denoise_mb) and never resets x->skip.
	staticBreakout := vp8enc.StaticInterRDEncodeBreakout(mbSource, &e.analysis.Img, row, col, quant, e.interStaticThresholdForSegment(segmentID))
	// libvpx anchor: vp8/encoder/encodeframe.c vp8cx_encode_inter_macroblock
	// (line 1275-1281) runs vp8_encode_inter16x16 whenever x->skip == 0;
	// libvpx sets x->skip = 1 in only two places in evaluate_inter_mode_rd:
	//   (1) rdopt.c:1607-1608 — active_map_enabled && active_ptr[0] == 0
	//   (2) rdopt.c:1620-1628 — static encode_breakout fires.
	// The picker's downstream mbmi.mb_skip_coeff (from tteob==0 rate
	// accounting at rdopt.c:1700) does NOT set x->skip and the encode-side
	// vp8_encode_inter16x16 runs regardless, regenerating coefficients via
	// the regular quantizer + optimize_mb trellis. Mirror that: only the
	// real x->skip sources (inactive map + staticBreakout) gate the encode
	// short-circuit, not the picker's MBSkipCoeff signal. Closes the
	// BestARNR / GoodARNR ARNR pin holds (task #332).
	isInactiveMB := e.interMacroblockInactive(row, col, args.cols)
	breakoutSkip := args.modes[index].RefFrame != vp8common.IntraFrame &&
		(isInactiveMB || staticBreakout)
	if breakoutSkip {
		vp8enc.ClearMacroblockCoefficients(&args.coeffs[index])
	} else if args.modes[index].RefFrame != vp8common.IntraFrame || args.modes[index].Mode != vp8common.BPred {
		is4x4 := vp8enc.InterFrameModeUses4x4Tokens(args.modes[index].Mode)
		// Same picker → accepted-path cache short-circuit as the serial
		// builder (see buildReconstructingInterFrameCoefficientsWithSegmentation
		// for the contract). Each row worker has its own encoder view, so
		// the per-encoder DCT cache slots are per-row-private — no cross-
		// worker coordination needed.
		cacheIn := e.consumeInterRDCoeffCache()
		if args.denoiseActive {
			// The denoiser may have overwritten the source pixels the
			// picker fed into its DCT cache, so discard the cached
			// post-FDCT inputs and let buildPredictedMacroblockCoefficients
			// recompute from the post-denoiser source. Mirrors the serial
			// builder's `if denoiseActive { cacheIn = nil }` gate.
			cacheIn = nil
		}
		zbinOverQuant := e.rc.currentZbinOverQuant
		actZbinAdj := 0
		// Same libvpx anchor as the BPred branch above: trellis optimize_b
		// uses mb->rdmult = cpi->RDMULT (frame-level), so the rdMult fed
		// into buildPredictedMacroblockCoefficients uses args.qIndex (base)
		// not segmentQIndex. The pass-2 iiratio lift (rdopt.c:189-196)
		// lands on the same cpi->RDMULT before optimize_b runs.
		rdMult, rdDiv := e.libvpxRDConstantsWithZbinForFrame(args.qIndex, zbinOverQuant)
		if e.activityMapValid {
			if adjustment, ok := e.tunedZbinAdjustment(row, col); ok {
				actZbinAdj = adjustment
			}
			rdMult = e.tunedRDMultiplier(rdMult, row, col)
		}
		buildPredictedMacroblockCoefficients(predictedMacroblockCoefficientArgs{
			coefProbs:     e.pickerCoefProbs(),
			src:           mbSource,
			mbRow:         row,
			mbCol:         col,
			pred:          &e.analysis.Img,
			aboveTok:      &args.aboveTok[col],
			leftTok:       &rs.leftTok,
			quant:         quant,
			qIndex:        segmentQIndex,
			zbinOverQuant: zbinOverQuant,
			zbinModeBoost: interZbinModeBoost(&args.modes[index]),
			actZbinAdj:    actZbinAdj,
			rdMult:        rdMult,
			rdDiv:         rdDiv,
			is4x4:         is4x4,
			intra:         args.modes[index].RefFrame == vp8common.IntraFrame,
			fastQuant:     e.libvpxUseFastQuant(),
			optimize:      e.libvpxOptimizeCoefficients(),
			collectOracle: false,
			coeffs:        &args.coeffs[index],
			cacheIn:       cacheIn,
			trace:         newPretrellisUVTrace(e),
		})
	}

	is4x4 := vp8enc.InterFrameModeUses4x4Tokens(args.modes[index].Mode)
	args.modes[index].MBSkipCoeff = breakoutSkip || vp8enc.MacroblockCoefficientsEmpty(&args.coeffs[index], is4x4)
	// Lane C accepted-candidate reuse: matches the serial path. The first
	// vp8enc.ConvertInterFrameMode above (lines 383/393) is what produced
	// e.reconstructModes[index] from args.modes[index]; nothing between
	// that call and this point writes back to args.modes[index] or
	// e.reconstructModes[index] except the MBSkipCoeff fix-up on the line
	// above. So updating MBSkipCoeff alone is byte-identical to the prior
	// full re-serialize and skips the MacroblockMode memset plus the
	// per-MB [16]MotionVector BlockMV fill the compiler cannot prove dead.
	e.reconstructModes[index].MBSkipCoeff = args.modes[index].MBSkipCoeff
	vp8enc.ConvertMacroblockCoefficients(&args.coeffs[index], is4x4, &e.reconstructTokens[index])
	if args.modes[index].RefFrame == vp8common.IntraFrame && args.modes[index].Mode == vp8common.BPred {
		if err := rs.updateThreadedInterFrameTokenContextAndCount(&args.aboveTok[col], &rs.leftTok, is4x4, args.modes[index].MBSkipCoeff, &args.coeffs[index]); err != nil {
			return 0, 0, err
		}
		return int(decision.projectedRate), int64(decision.predictionError), nil
	}
	if args.modes[index].RefFrame == vp8common.IntraFrame {
		if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID&3], &e.reconstructScratch) {
			return 0, 0, ErrInvalidConfig
		}
	} else if !addInterResidualToAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID&3], &e.reconstructScratch) {
		return 0, 0, ErrInvalidConfig
	}
	if err := rs.updateThreadedInterFrameTokenContextAndCount(&args.aboveTok[col], &rs.leftTok, is4x4, args.modes[index].MBSkipCoeff, &args.coeffs[index]); err != nil {
		return 0, 0, err
	}
	return int(decision.projectedRate), int64(decision.predictionError), nil
}

func (rs *rowEncoderState) updateThreadedInterFrameTokenContextAndCount(above *vp8enc.TokenContextPlanes, left *vp8enc.TokenContextPlanes, is4x4 bool, skipped bool, coeffs *vp8enc.MacroblockCoefficients) error {
	if skipped {
		vp8enc.ResetTokenContextPlanes(above, left, is4x4)
		return nil
	}
	return vp8enc.AccumulateInterMacroblockTokenCountsAndRecords(&rs.interCoefTokenCounts, &rs.interCoefTokenRecords, is4x4, above, left, coeffs)
}

func (p *rowWorkerPool) mergeThreadedKeyFrameCoefCounts(e *VP8Encoder, workerCount int) {
	if p == nil || e == nil || workerCount <= 0 {
		return
	}
	vp8enc.ResetInterCoefficientTokenCounts(&e.keyFrameCoefTokenCounts)
	for workerIndex := range workerCount {
		counts := &p.workers[workerIndex].keyFrameCoefTokenCounts
		tokenLimit := vp8tables.MaxEntropyTokens
		if workerIndex > 0 {
			// libvpx's threaded sum_coef_counts loop is bounded by
			// ENTROPY_NODES, not MAX_ENTROPY_TOKENS. That intentionally
			// preserves the helper-thread DCT_EOB_TOKEN omission for byte
			// parity with VP8 row threading.
			tokenLimit = vp8tables.EntropyNodes
		}
		for block := range e.keyFrameCoefTokenCounts {
			for band := range e.keyFrameCoefTokenCounts[block] {
				for ctx := range e.keyFrameCoefTokenCounts[block][band] {
					for token := range tokenLimit {
						e.keyFrameCoefTokenCounts[block][band][ctx][token] += (*counts)[block][band][ctx][token]
					}
				}
			}
		}
	}
	e.keyFrameCoefTokenCountsValid = true
}

func (p *rowWorkerPool) mergeThreadedInterFrameCoefCounts(e *VP8Encoder, workerCount int) {
	if p == nil || e == nil || workerCount <= 0 {
		return
	}
	vp8enc.ResetInterCoefficientTokenCounts(&e.interCoefTokenCounts)
	for workerIndex := range workerCount {
		counts := &p.workers[workerIndex].interCoefTokenCounts
		tokenLimit := vp8tables.MaxEntropyTokens
		if workerIndex > 0 {
			// Match libvpx threaded sum_coef_counts: helper rows omit
			// DCT_EOB_TOKEN from the merged probability-update counts.
			tokenLimit = vp8tables.EntropyNodes
		}
		for block := range e.interCoefTokenCounts {
			for band := range e.interCoefTokenCounts[block] {
				for ctx := range e.interCoefTokenCounts[block][band] {
					for token := range tokenLimit {
						e.interCoefTokenCounts[block][band][ctx][token] += (*counts)[block][band][ctx][token]
					}
				}
			}
		}
	}
	e.interCoefTokenCountsValid = true
	e.interCoefTokenRecordsValid = false
}

func (p *rowWorkerPool) mergeThreadedInterFrameCoefRecords(e *VP8Encoder, workerCount int, rows int, cols int, required int) {
	if p == nil || e == nil || workerCount <= 0 || rows < 0 || cols < 0 || required < 0 {
		return
	}
	e.interCoefTokenRecords.Reset(rows, required)
	for row := range rows {
		e.interCoefTokenRecords.MarkRowStart(row)
		workerIndex := row % workerCount
		if uint(workerIndex) >= uint(len(p.workers)) {
			e.interCoefTokenRecordsValid = false
			return
		}
		workerRecords := &p.workers[workerIndex].interCoefTokenRecords
		if workerRecords.Rows != rows {
			e.interCoefTokenRecordsValid = false
			return
		}
		rowStarts := workerRecords.RowStartsForMerge()
		if len(rowStarts) < rows+1 {
			e.interCoefTokenRecordsValid = false
			return
		}
		start, end := int(rowStarts[row]), int(rowStarts[row+1])
		if start < 0 || start > end || end > len(workerRecords.Records) {
			e.interCoefTokenRecordsValid = false
			return
		}
		e.interCoefTokenRecords.Records = append(e.interCoefTokenRecords.Records, workerRecords.Records[start:end]...)
		e.interCoefTokenRecords.MarkRowEnd(row)
	}
	e.interCoefTokenRecordsValid = true
}

func (p *rowWorkerPool) mergeThreadedInterFrameState(e *VP8Encoder, workerCount int, required int) {
	if p == nil || e == nil || workerCount <= 0 {
		return
	}
	var mergedBins [1024]uint32
	mergedDotSuppress := 0
	for workerIndex := range workerCount {
		worker := &p.workers[workerIndex]
		workerEnc := &worker.enc
		for i := range mergedBins {
			mergedBins[i] += workerEnc.interModeErrorBins[i]
		}
		mergedDotSuppress += workerEnc.mbsZeroLastDotSuppress
	}
	e.interModeErrorBins = mergedBins
	e.mbsZeroLastDotSuppress = mergedDotSuppress
	// libvpx's main lane encodes through cpi->mb, so its threshold mutations
	// naturally persist across frames. Helper lanes start from copied
	// thresholds and are not merged back.
	e.interRDThreshMult = p.workers[0].enc.interRDThreshMult
	e.interRDThreshTouched = p.workers[0].enc.interRDThreshTouched
	if len(e.dotArtifactChecked) >= required {
		clear(e.dotArtifactChecked[:required])
		for workerIndex := range workerCount {
			checked := p.workers[workerIndex].dotArtifactChecked
			for i := range min(required, len(checked)) {
				if checked[i] {
					e.dotArtifactChecked[i] = true
				}
			}
		}
	}
}
