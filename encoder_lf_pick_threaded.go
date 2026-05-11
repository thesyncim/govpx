package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// lfTrialArgs is the pre-allocated parameter slot the persistent
// row-worker goroutine reads when servicing the rowWorkerJobLFTrial
// job. Mirrors libvpx's per-frame argument struct pattern (args/keyArgs
// already on the pool). All inputs are scalars or pointers to
// long-lived encoder/source-image storage, so the dispatcher writes
// fields directly into the pool and the worker reads them without any
// closure capture. The result is written back into errOut for the
// dispatcher to consume after the worker signals done.
//
// Critical zero-alloc invariant: this struct is embedded by value in
// rowWorkerPool. Because the pool itself is only allocated when
// Threads >= 2 (see newRowWorkerPool), Threads=1 never reaches the
// dispatch code path and the Go escape analyzer has no reason to
// heap-allocate any of the inputs (the alternative pattern —
// goroutine closures inside pickFull — caused captured locals to
// escape on Threads=1 too, breaking
// TestEncodeIntoMultiTokenPartitionAllocatesZero, hence this
// pre-allocated-slot design).
type lfTrialArgs struct {
	src        vp8enc.SourceImage
	dst        *vp8common.Image
	srcImg     *vp8common.Image
	modes      []vp8dec.MacroblockMode
	frameType  vp8common.FrameType
	filterType vp8dec.LoopFilterType
	cfg        vp8common.LoopFilterFrameConfig
	lfi        *vp8common.LoopFilterInfo
	rows       int
	cols       int
	level      int
	errOut     int
}

// runLFTrialWorker is the persistent worker entry point for the
// parallel LF-trial job. It mirrors trialLumaSSE's full-frame branch
// (the full picker never takes the partial path) but reads all inputs
// from the pre-allocated pool slot rather than from per-call closure
// captures, keeping the Threads=1 path free of any reachable goroutine
// state.
func (p *rowWorkerPool) runLFTrialWorker(workerIndex int) {
	_ = workerIndex
	args := &p.lfTrial
	copyFrameImageLuma(args.dst, args.srcImg)
	vp8dec.ApplyLoopFilterFullLumaConfiguredUnchecked(
		args.dst,
		args.rows,
		args.cols,
		args.modes,
		args.frameType,
		args.filterType,
		args.level,
		args.cfg,
		args.lfi,
	)
	args.errOut = loopFilterLumaSSE(args.src, args.dst, args.rows, args.cols, false)
}

// canParallelLFTrials reports whether the encoder has a row-worker
// pool with at least two threads. Used by pickFull to gate the
// parallel filt_low/filt_high dispatch. Threads=1 returns false here
// without touching e.rowWorkers (which is nil), so the parallel
// branch is never reachable from the serial path.
func (e *VP8Encoder) canParallelLFTrials() bool {
	return e.rowWorkers != nil && len(e.rowWorkers.workers) >= 2
}

// dispatchLFTrialPair launches the filt_low trial on the row-worker
// pool (using the loopFilterPickAlt scratch + loopInfoAlt), runs the
// filt_high trial inline on the calling goroutine (against
// loopFilterPick + loopInfo, matching the serial scratch the resident
// path expects), and returns (lowErr, highErr).
//
// Pre-conditions (caller-enforced):
//   - e.rowWorkers != nil and at least one idle worker slot.
//   - loopFilterPickAlt has been Resized to match loopFilterPick.
//   - lowLevel and highLevel are valid loop-filter levels.
//
// The dispatch follows the same pattern as
// buildReconstructingInterFrameCoefficientsThreaded: write all inputs
// to the pool's pre-allocated slot, signal the worker via the start
// channel, run the local trial, then drain the done channel. The pool
// is otherwise idle at this point in the frame (the row-parallel
// reconstruction has already finished), so worker slot 1 is
// guaranteed free.
func (ctx *loopFilterPickContext) dispatchLFTrialPair(lowLevel int, highLevel int) (int, int) {
	e := ctx.encoder
	pool := e.rowWorkers
	pool.lfTrial = lfTrialArgs{
		src:        ctx.src,
		dst:        &e.loopFilterPickAlt.Img,
		srcImg:     &e.analysis.Img,
		modes:      ctx.modes,
		frameType:  ctx.frameType,
		filterType: ctx.filterType,
		cfg:        ctx.fullFrameConfig,
		lfi:        &e.loopInfoAlt,
		rows:       ctx.rows,
		cols:       ctx.cols,
		level:      lowLevel,
	}
	// Mirror libvpx's lfi setup on the worker side: InitLoopFilterInfo
	// only depends on sharpness (already set on loopInfo by
	// newLoopFilterPickContext), so the worker can reuse that
	// per-sharpness table by copying it across. Subsequent
	// InitLoopFilterFrame inside ApplyLoopFilterFullLumaConfiguredUnchecked
	// rebuilds the per-level filter level table from the same sharpness
	// table on the alt lfi without touching the main lfi.
	e.loopInfoAlt = e.loopInfo
	pool.job = rowWorkerJobLFTrial
	pool.encoder = e
	pool.workerCount = 1
	pool.required = 0
	pool.abort.Store(0)
	// Worker 1 is guaranteed free at the LF-picker stage: the
	// reconstruction phase that consumes the pool finished before
	// pickLoopFilterLevel was called.
	pool.start[1] <- struct{}{}

	// Run filt_high inline on the main scratch + loopInfo so the
	// resident-level bookkeeping in pickFull matches the serial state
	// after this iteration (residentLevel == highLevel,
	// loopFilterPick.Y == highLevel-filtered luma).
	copyFrameImageLuma(&e.loopFilterPick.Img, &e.analysis.Img)
	vp8dec.ApplyLoopFilterFullLumaConfiguredUnchecked(
		&e.loopFilterPick.Img,
		ctx.rows,
		ctx.cols,
		ctx.modes,
		ctx.frameType,
		ctx.filterType,
		highLevel,
		ctx.fullFrameConfig,
		&e.loopInfo,
	)
	highErr := loopFilterLumaSSE(ctx.src, &e.loopFilterPick.Img, ctx.rows, ctx.cols, false)

	// Drain the worker.
	<-pool.done
	lowErr := pool.lfTrial.errOut
	pool.encoder = nil
	pool.job = rowWorkerJobInterFrame
	pool.lfTrial = lfTrialArgs{}
	return lowErr, highErr
}
