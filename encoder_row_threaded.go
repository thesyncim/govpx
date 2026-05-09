package govpx

import (
	"sync/atomic"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
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
	activeMapEnabled       bool
	activeMapLastAvailable bool
	activeMapLastRef       interAnalysisReference
	sourceAltRefZeroMVOnly bool
}

func (e *VP8Encoder) useThreadedInterFrameRows(rows int, cols int) bool {
	pool := e.rowWorkers
	return pool != nil &&
		len(pool.workers) > 1 &&
		rows > 1 &&
		cols > pool.syncRange &&
		!e.oracleTraceEnabled()
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficientsThreaded(args threadedInterRowsArgs) (int, error) {
	pool := e.rowWorkers
	if pool == nil || args.rows <= 1 || args.cols <= 0 {
		return 0, ErrInvalidConfig
	}
	required := args.rows * args.cols
	workerCount := min(len(pool.workers), args.rows)
	if workerCount < 2 {
		return 0, ErrInvalidConfig
	}

	pool.reset(args.rows)
	if len(e.dotArtifactChecked) >= required {
		clear(e.dotArtifactChecked[:required])
	}

	pool.encoder = e
	pool.args = args
	pool.workerCount = workerCount
	pool.required = required
	pool.abort.Store(0)
	for workerIndex := range workerCount {
		pool.workerErrors[workerIndex] = nil
		pool.start[workerIndex] <- struct{}{}
	}

	var firstErr error
	for range workerCount {
		workerIndex := <-pool.done
		if err := pool.workerErrors[workerIndex]; err != nil && firstErr == nil {
			firstErr = err
			pool.abort.Store(1)
		}
	}
	if firstErr != nil {
		pool.encoder = nil
		pool.args = threadedInterRowsArgs{}
		return 0, firstErr
	}
	if pool.abort.Load() != 0 {
		pool.encoder = nil
		pool.args = threadedInterRowsArgs{}
		return 0, ErrInvalidConfig
	}

	pool.mergeThreadedInterFrameState(e, workerCount, required)
	totalRate := 0
	for workerIndex := range workerCount {
		totalRate = libvpxAddProjectedMacroblockRate(totalRate, pool.workers[workerIndex].totalRate)
	}
	pool.encoder = nil
	pool.args = threadedInterRowsArgs{}
	return totalRate, nil
}

func (rs *rowEncoderState) encodeThreadedInterFrameRow(pool *rowWorkerPool, args *threadedInterRowsArgs, row int, abort *atomic.Int32) (int, error) {
	rs.rowIndex = row
	rs.leftTok = vp8enc.TokenContextPlanes{}
	rowRate := 0
	lastCol := args.cols - 1
	for col := range args.cols {
		target := col + pool.syncRange
		if target > lastCol {
			target = lastCol
		}
		if !pool.waitForAboveColumnAbort(row, target, abort) {
			return rowRate, nil
		}
		rate, err := rs.encodeThreadedInterFrameMacroblock(args, row, col)
		if err != nil {
			return 0, err
		}
		rowRate = libvpxAddProjectedMacroblockRate(rowRate, rate)
		if col != lastCol {
			pool.publishRowColumn(row, col)
		}
	}
	vp8dec.ExtendIntraRightEdgeForRow(&rs.enc.analysis.Img, row)
	pool.publishRowColumn(row, lastCol)
	return rowRate, nil
}

func (rs *rowEncoderState) encodeThreadedInterFrameMacroblock(args *threadedInterRowsArgs, row int, col int) (int, error) {
	e := &rs.enc
	index := row*args.cols + col
	if args.activeMapEnabled && args.activeMapLastAvailable && e.activeMap[index] == 0 {
		if !e.encodeInactiveInterMacroblock(row, col, index, args.activeMapLastRef.Img, args.modes, args.coeffs, &args.aboveTok[col], &rs.leftTok) {
			return 0, ErrInvalidConfig
		}
		return 0, nil
	}

	segmentID, ok := interFrameAnalysisSegmentID(&args.modes[index], args.segmentation, args.preserveSegmentID)
	if !ok {
		return 0, ErrInvalidConfig
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
	if segmentID != 0 {
		e.interRDThreshMultSnapshot = e.interRDThreshMult
		e.interRDThreshTouchedSnapshot = e.interRDThreshTouched
		e.interModeTestHitCountsSnapshot = e.interModeTestHitCounts
		e.interMBsTestedSoFarSnapshot = e.interMBsTestedSoFar
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
		return 0, ErrInvalidConfig
	}
	if segmentID != 0 && !decision.cyclicRefreshEligible() {
		e.interRDThreshMult = e.interRDThreshMultSnapshot
		e.interRDThreshTouched = e.interRDThreshTouchedSnapshot
		e.interModeTestHitCounts = e.interModeTestHitCountsSnapshot
		e.interMBsTestedSoFar = e.interMBsTestedSoFarSnapshot
		segmentID = 0
		decision, ok = e.selectInterFrameModeDecision(
			args.src, args.refs[:], args.refCount,
			row, col, args.rows, args.cols,
			args.qIndex, args.segmentation, segmentID,
			above, left, aboveLeft,
			&args.aboveTok[col], &rs.leftTok,
			&args.quants[segmentID],
			args.sourceAltRefZeroMVOnly,
		)
		if !ok {
			return 0, ErrInvalidConfig
		}
	}

	segmentQIndex := encoderSegmentQIndex(args.qIndex, args.segmentation, segmentID)
	quant := &args.quants[segmentID]
	if decision.useIntra {
		args.modes[index] = decision.intraMode
		args.modes[index].SegmentID = segmentID
		convertInterFrameMode(&args.modes[index], &e.reconstructModes[index])
		if args.modes[index].Mode == vp8common.BPred {
			if !buildReconstructingBPredMacroblockCoefficients(&e.coefProbs, args.src, row, col, &e.analysis.Img, &e.reconstructModes[index], &args.aboveTok[col], &rs.leftTok, quant, segmentQIndex, e.rc.currentZbinOverQuant, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), false, &args.coeffs[index], &e.reconstructScratch) {
				return 0, ErrInvalidConfig
			}
		} else if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
			return 0, ErrInvalidConfig
		}
	} else {
		args.modes[index] = decision.interMode
		convertInterFrameMode(&args.modes[index], &e.reconstructModes[index])
		predMode := e.reconstructModes[index]
		predMode.MBSkipCoeff = true
		if !reconstructInterAnalysisMacroblock(&e.analysis.Img, decision.ref.Img, row, col, &predMode, &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
			return 0, ErrInvalidConfig
		}
	}

	breakoutSkip := args.modes[index].RefFrame != vp8common.IntraFrame &&
		(args.modes[index].MBSkipCoeff || staticInterRDEncodeBreakout(args.src, &e.analysis.Img, row, col, quant, e.opts.StaticThreshold))
	if breakoutSkip {
		clearMacroblockCoefficients(&args.coeffs[index])
	} else if args.modes[index].RefFrame != vp8common.IntraFrame || args.modes[index].Mode != vp8common.BPred {
		is4x4 := interFrameModeUses4x4Tokens(args.modes[index].Mode)
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
		})
	}

	is4x4 := interFrameModeUses4x4Tokens(args.modes[index].Mode)
	args.modes[index].MBSkipCoeff = breakoutSkip || macroblockCoefficientsEmpty(&args.coeffs[index], is4x4)
	convertInterFrameMode(&args.modes[index], &e.reconstructModes[index])
	convertMacroblockCoefficients(&args.coeffs[index], is4x4, &e.reconstructTokens[index])
	if args.modes[index].RefFrame == vp8common.IntraFrame && args.modes[index].Mode == vp8common.BPred {
		updateInterAnalysisTokenContext(&args.aboveTok[col], &rs.leftTok, is4x4, args.modes[index].MBSkipCoeff, &args.coeffs[index])
		return decision.projectedRate, nil
	}
	if args.modes[index].RefFrame == vp8common.IntraFrame {
		if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
			return 0, ErrInvalidConfig
		}
	} else if !addInterResidualToAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
		return 0, ErrInvalidConfig
	}
	updateInterAnalysisTokenContext(&args.aboveTok[col], &rs.leftTok, is4x4, args.modes[index].MBSkipCoeff, &args.coeffs[index])
	return decision.projectedRate, nil
}

func (p *rowWorkerPool) mergeThreadedInterFrameState(e *VP8Encoder, workerCount int, required int) {
	if p == nil || e == nil || workerCount <= 0 {
		return
	}
	var mergedBins [1024]uint32
	var mergedHits [libvpxInterModeCount]int
	var mergedTouched [libvpxInterModeCount]bool
	var multSum [libvpxInterModeCount]int
	mbsTested := 0
	dotSuppress := 0
	for workerIndex := range workerCount {
		worker := &p.workers[workerIndex]
		workerEnc := &worker.enc
		mbsTested += workerEnc.interMBsTestedSoFar
		dotSuppress += workerEnc.mbsZeroLastDotSuppress
		for i := range mergedBins {
			mergedBins[i] += workerEnc.interModeErrorBins[i]
		}
		for i := range mergedHits {
			mergedHits[i] += workerEnc.interModeTestHitCounts[i]
			multSum[i] += workerEnc.interRDThreshMult[i]
			if workerEnc.interRDThreshTouched[i] {
				mergedTouched[i] = true
			}
		}
	}
	e.interModeErrorBins = mergedBins
	e.interModeTestHitCounts = mergedHits
	e.interMBsTestedSoFar = mbsTested
	e.mbsZeroLastDotSuppress = dotSuppress
	for i := range e.interRDThreshMult {
		avg := (multSum[i] + workerCount/2) / workerCount
		if avg == 0 {
			avg = libvpxRDThreshMultStart
		}
		if avg < libvpxMinThreshMult {
			avg = libvpxMinThreshMult
		}
		if avg > libvpxMaxThreshMult {
			avg = libvpxMaxThreshMult
		}
		e.interRDThreshMult[i] = avg
		e.interRDThreshTouched[i] = mergedTouched[i]
	}
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
