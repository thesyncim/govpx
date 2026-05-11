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
	totalRate := 0
	totalPredictionError := int64(0)
	for workerIndex := range workerCount {
		totalRate = libvpxAddProjectedMacroblockRate(totalRate, pool.workers[workerIndex].totalRate)
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
		totalRate = libvpxAddProjectedMacroblockRate(totalRate, pool.workers[workerIndex].totalRate)
	}
	pool.encoder = nil
	pool.job = rowWorkerJobInterFrame
	pool.keyArgs = threadedKeyRowsArgs{}
	return totalRate, nil
}

// applyChromaOnlyLoopFilterThreaded partitions the chroma-only loop-filter
// apply across the row worker pool. Validation and lfi initialization happen
// on the main goroutine (so workers observe a fully-built lfi); workers each
// process an interleaved stripe of MB rows. There is no wave-front: per-row
// chroma writes never overlap across rows (see ApplyLoopFilterChromaOnlyPreparedRow
// for the row-disjoint argument). The main goroutine participates as worker 0
// to avoid burning a context switch for the common small-frame case.
func (e *VP8Encoder) applyChromaOnlyLoopFilterThreaded(rows int, cols int, required int, frameType vp8common.FrameType, header vp8dec.LoopFilterHeader, segmentation vp8enc.SegmentationConfig) error {
	pool := e.rowWorkers
	modes := e.reconstructModes[:required]
	if err := vp8dec.ApplyLoopFilterChromaOnlyPreparedInit(&e.analysis.Img, rows, cols, modes, frameType, header, loopFilterSegmentationHeader(segmentation), &e.loopInfo); err != nil {
		return err
	}
	if header.Level == 0 || header.Type != vp8dec.NormalLoopFilter {
		return nil
	}
	if pool == nil || rows <= 1 || cols <= 0 {
		// Fall back to serial (also catches the unlikely re-entry case
		// where the pool was torn down between gate and dispatch).
		return applyChromaOnlyLoopFilterSerial(&e.analysis.Img, rows, cols, modes, frameType, &e.loopInfo)
	}
	workerCount := min(len(pool.workers), rows)
	// Chroma-only LF is fast per row; below a row threshold the dispatch
	// overhead dominates. 8 MB rows of chroma LF is ~ a few hundred microseconds
	// on the M-class targets; require at least 2 rows per worker on average.
	if workerCount < 2 || rows < 4 {
		return applyChromaOnlyLoopFilterSerial(&e.analysis.Img, rows, cols, modes, frameType, &e.loopInfo)
	}

	pool.encoder = e
	pool.job = rowWorkerJobLFChroma
	pool.lfChromaArgs = threadedLFChromaArgs{
		img:       &e.analysis.Img,
		rows:      rows,
		cols:      cols,
		modes:     modes,
		frameType: frameType,
		lfi:       &e.loopInfo,
	}
	pool.workerCount = workerCount
	pool.required = required
	pool.abort.Store(0)
	for workerIndex := range workerCount {
		pool.workerErrors[workerIndex] = nil
	}

	pool.startHelperWorkers()
	pool.runThreadedLFChromaWorker(0)

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

	pool.encoder = nil
	pool.job = rowWorkerJobInterFrame
	pool.lfChromaArgs = threadedLFChromaArgs{}
	if firstErr != nil {
		return firstErr
	}
	return nil
}

func applyChromaOnlyLoopFilterSerial(img *vp8common.Image, rows int, cols int, modes []vp8dec.MacroblockMode, frameType vp8common.FrameType, lfi *vp8common.LoopFilterInfo) error {
	for row := range rows {
		if err := vp8dec.ApplyLoopFilterChromaOnlyPreparedRow(img, row, cols, modes, frameType, lfi); err != nil {
			return err
		}
	}
	return nil
}

func (rs *rowEncoderState) encodeThreadedKeyFrameRow(pool *rowWorkerPool, args *threadedKeyRowsArgs, row int, abort *atomic.Int32) (int, error) {
	rs.rowIndex = row
	rs.leftTok = vp8enc.TokenContextPlanes{}
	rowRate := 0
	lastCol := args.cols - 1
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
			target := min(col+pool.syncRange, lastCol)
			if !pool.waitForAboveColumnAbort(row, target, abort) {
				return rowRate, nil
			}
		}
		rate, err := rs.encodeThreadedKeyFrameMacroblock(args, row, col)
		if err != nil {
			return 0, err
		}
		rowRate = libvpxAddProjectedMacroblockRate(rowRate, rate)
	}
	vp8dec.ExtendIntraRightEdgeForRow(&rs.enc.analysis.Img, row)
	// Post-row store at `cols - 1 + nsync` so the last syncRange MBs of
	// the row below see a value beyond any in-row target. Matches
	// libvpx's `vpx_atomic_store_release(current_mb_col, mb_col + nsync)`
	// at end-of-row.
	pool.publishRowColumn(row, lastCol+pool.syncRange)
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
	if e.libvpxUseFastIntraPick() {
		mode, projectedRate, ok = predictBestKeyFrameIntraModeFast(args.src, segmentQIndex, row, col, above, left, &args.quants[segmentID], &e.analysis.Img, &e.reconstructScratch, e.libvpxUseFastQuantForPick())
	} else {
		mode, projectedRate, ok = predictBestKeyFrameIntraMode(args.src, segmentQIndex, row, col, above, left, &args.aboveTok[col], &rs.leftTok, &args.quants[segmentID], &e.analysis.Img, &e.reconstructScratch, e.libvpxUseFastQuantForPick())
	}
	if !ok {
		return 0, ErrInvalidConfig
	}
	mode.SegmentID = segmentID
	args.modes[index] = mode
	convertKeyFrameMode(&args.modes[index], &e.reconstructModes[index])
	if args.modes[index].YMode == vp8common.BPred {
		if !buildReconstructingBPredMacroblockCoefficients(&vp8tables.DefaultCoefProbs, args.src, row, col, &e.analysis.Img, &e.reconstructModes[index], &args.aboveTok[col], &rs.leftTok, &args.quants[segmentID], segmentQIndex, 0, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), false, &args.coeffs[index], &e.reconstructScratch) {
			return 0, ErrInvalidConfig
		}
		convertMacroblockCoefficients(&args.coeffs[index], true, &e.reconstructTokens[index])
		vp8enc.UpdateTokenContextPlanesFromCoefficients(&args.aboveTok[col], &rs.leftTok, true, &args.coeffs[index])
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
		quant:         &args.quants[segmentID],
		qIndex:        segmentQIndex,
		is4x4:         is4x4,
		intra:         true,
		fastQuant:     e.libvpxUseFastQuant(),
		optimize:      e.libvpxOptimizeCoefficients(),
		collectOracle: false,
		coeffs:        &args.coeffs[index],
	})
	convertMacroblockCoefficients(&args.coeffs[index], is4x4, &e.reconstructTokens[index])
	if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
		return 0, ErrInvalidConfig
	}
	vp8enc.UpdateTokenContextPlanesFromCoefficients(&args.aboveTok[col], &rs.leftTok, is4x4, &args.coeffs[index])
	return projectedRate, nil
}

func (rs *rowEncoderState) encodeThreadedInterFrameRow(pool *rowWorkerPool, args *threadedInterRowsArgs, row int, abort *atomic.Int32) (int, error) {
	rs.rowIndex = row
	rs.leftTok = vp8enc.TokenContextPlanes{}
	rowRate := 0
	rowPredictionError := int64(0)
	lastCol := args.cols - 1
	for col := range args.cols {
		if col > 0 && (col-1)%pool.syncRange == 0 {
			pool.publishRowColumn(row, col-1)
		}
		if col%pool.syncRange == 0 {
			target := min(col+pool.syncRange, lastCol)
			if !pool.waitForAboveColumnAbort(row, target, abort) {
				return rowRate, nil
			}
		}
		rate, predictionError, err := rs.encodeThreadedInterFrameMacroblock(args, row, col)
		if err != nil {
			return 0, err
		}
		rowRate = libvpxAddProjectedMacroblockRate(rowRate, rate)
		rowPredictionError += predictionError
	}
	rs.totalPredictionError += rowPredictionError
	vp8dec.ExtendIntraRightEdgeForRow(&rs.enc.analysis.Img, row)
	pool.publishRowColumn(row, lastCol+pool.syncRange)
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
	decision, ok := e.selectInterFrameModeDecision(
		args.src, args.refs[:], args.refCount,
		row, col, args.rows, args.cols,
		args.qIndex, args.segmentation, segmentID,
		above, left, aboveLeft,
		&args.aboveTok[col], &rs.leftTok,
		&args.quants[segmentID],
		args.sourceAltRefZeroMVOnly,
	)
	if !ok {
		return 0, 0, ErrInvalidConfig
	}
	if segmentID != 0 && !decision.cyclicRefreshEligible() {
		if haveFallbackSnapshot {
			restoreInterMacroblockImage(&e.analysis.Img, row, col, &fallbackSnapshot)
		}
		segmentID = 0
		decision.interMode.SegmentID = 0
		decision.intraMode.SegmentID = 0
	}

	segmentQIndex := encoderSegmentQIndex(args.qIndex, args.segmentation, segmentID)
	quant := &args.quants[segmentID]
	if decision.useIntra {
		args.modes[index] = decision.intraMode
		args.modes[index].SegmentID = segmentID
		convertInterFrameMode(&args.modes[index], &e.reconstructModes[index])
		if args.modes[index].Mode == vp8common.BPred {
			if !buildReconstructingBPredMacroblockCoefficients(&e.coefProbs, args.src, row, col, &e.analysis.Img, &e.reconstructModes[index], &args.aboveTok[col], &rs.leftTok, quant, segmentQIndex, e.rc.currentZbinOverQuant, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), false, &args.coeffs[index], &e.reconstructScratch) {
				return 0, 0, ErrInvalidConfig
			}
		} else if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
			return 0, 0, ErrInvalidConfig
		}
	} else {
		args.modes[index] = decision.interMode
		convertInterFrameMode(&args.modes[index], &e.reconstructModes[index])
		predMode := e.reconstructModes[index]
		predMode.MBSkipCoeff = true
		if !reconstructInterAnalysisMacroblock(&e.analysis.Img, decision.ref.Img, row, col, &predMode, &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
			return 0, 0, ErrInvalidConfig
		}
	}

	breakoutSkip := args.modes[index].RefFrame != vp8common.IntraFrame &&
		(args.modes[index].MBSkipCoeff || staticInterRDEncodeBreakout(args.src, &e.analysis.Img, row, col, quant, e.opts.StaticThreshold))
	if breakoutSkip {
		clearMacroblockCoefficients(&args.coeffs[index])
	} else if args.modes[index].RefFrame != vp8common.IntraFrame || args.modes[index].Mode != vp8common.BPred {
		is4x4 := interFrameModeUses4x4Tokens(args.modes[index].Mode)
		// Same picker → accepted-path cache short-circuit as the serial
		// builder (see buildReconstructingInterFrameCoefficientsWithSegmentation
		// for the contract). Each row worker has its own encoder view, so
		// the per-encoder DCT cache slots are per-row-private — no cross-
		// worker coordination needed.
		cacheIn := e.consumeInterRDCoeffCache()
		buildPredictedMacroblockCoefficients(predictedMacroblockCoefficientArgs{
			coefProbs:     &e.coefProbs,
			src:           args.src,
			mbRow:         row,
			mbCol:         col,
			pred:          &e.analysis.Img,
			aboveTok:      &args.aboveTok[col],
			leftTok:       &rs.leftTok,
			quant:         quant,
			qIndex:        segmentQIndex,
			zbinOverQuant: e.rc.currentZbinOverQuant,
			zbinModeBoost: interZbinModeBoost(&args.modes[index]),
			is4x4:         is4x4,
			intra:         args.modes[index].RefFrame == vp8common.IntraFrame,
			fastQuant:     e.libvpxUseFastQuant(),
			optimize:      e.libvpxOptimizeCoefficients(),
			collectOracle: false,
			coeffs:        &args.coeffs[index],
			cacheIn:       cacheIn,
		})
	}

	is4x4 := interFrameModeUses4x4Tokens(args.modes[index].Mode)
	args.modes[index].MBSkipCoeff = breakoutSkip || macroblockCoefficientsEmpty(&args.coeffs[index], is4x4)
	// Lane C accepted-candidate reuse: matches the serial path. The first
	// convertInterFrameMode above (lines 383/393) is what produced
	// e.reconstructModes[index] from args.modes[index]; nothing between
	// that call and this point writes back to args.modes[index] or
	// e.reconstructModes[index] except the MBSkipCoeff fix-up on the line
	// above. So updating MBSkipCoeff alone is byte-identical to the prior
	// full re-serialize and skips the MacroblockMode memset plus the
	// per-MB [16]MotionVector BlockMV fill the compiler cannot prove dead.
	e.reconstructModes[index].MBSkipCoeff = args.modes[index].MBSkipCoeff
	convertMacroblockCoefficients(&args.coeffs[index], is4x4, &e.reconstructTokens[index])
	if args.modes[index].RefFrame == vp8common.IntraFrame && args.modes[index].Mode == vp8common.BPred {
		updateInterAnalysisTokenContext(&args.aboveTok[col], &rs.leftTok, is4x4, args.modes[index].MBSkipCoeff, &args.coeffs[index])
		return decision.projectedRate, int64(decision.predictionError), nil
	}
	if args.modes[index].RefFrame == vp8common.IntraFrame {
		if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
			return 0, 0, ErrInvalidConfig
		}
	} else if !addInterResidualToAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
		return 0, 0, ErrInvalidConfig
	}
	updateInterAnalysisTokenContext(&args.aboveTok[col], &rs.leftTok, is4x4, args.modes[index].MBSkipCoeff, &args.coeffs[index])
	return decision.projectedRate, int64(decision.predictionError), nil
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
	// libvpx copies rd_thresh_mult into each row worker and does not merge
	// worker-local threshold mutations back into the primary MACROBLOCK.
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
