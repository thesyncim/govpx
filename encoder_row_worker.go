package govpx

import (
	"runtime"
	"sync"
	"sync/atomic"

	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// rowEncoderState holds the per-MB-row scratch and adaptive picker
// shadow state that a row worker mutates without contention against
// other row workers. Mirrors libvpx's MB_ROW_COMP +
// setup_mbby_copy(mbdst, x) duplication pattern in
// vp8/encoder/ethreading.c, which gives every encoder thread its own
// copy of the per-MB scratch (entropy contexts, scratch buffers,
// rd_threshes, mode-test counts) so row-level parallelism can run
// without sharing mutable state on the hot path.
//
// The struct is intentionally large: it carries every adaptive field
// the inter-frame picker reads or writes during candidate evaluation.
// At Threads=1 govpx does NOT instantiate this type and the picker
// reads/writes directly off *VP8Encoder.builtinPickerState so the
// canonical single-threaded path stays byte-identical and zero
// overhead.
//
// Lifetime: instances are allocated once at NewVP8Encoder when
// EncoderOptions.Threads >= 2 and reused across frames. Frame-end the
// main goroutine resets the worker-private adaptive shadows from the
// encoder's frame-baseline state.
type rowEncoderState struct {
	// rowIndex is the absolute MB row this worker is responsible for
	// during the current frame. Re-assigned each frame as work is
	// distributed.
	rowIndex int

	// enc is a shallow, worker-private encoder view. Slices and frame
	// buffers still point at the frame-level storage, but adaptive picker
	// scalars, threshold tables, error bins, and scratch are private to
	// the worker. This mirrors libvpx setup_mbby_copy: workers share the
	// immutable frame inputs and write disjoint MB slots, but do not contend
	// on picker state.
	enc VP8Encoder

	// leftTok is the persistent left token-context scratch for the
	// row's MB column traversal. libvpx zeroes this at the start of
	// every row and updates it as each MB commits its tokens (see
	// vp8/encoder/encodeframe.c encode_mb_row).
	leftTok vp8enc.TokenContextPlanes

	// scratch is the row-private intra reconstruction scratch reused
	// across the row's MB columns. Mirrors libvpx MACROBLOCK *x's
	// per-thread workspace from setup_mbby_copy.
	scratch vp8dec.IntraReconstructionScratch

	// dotArtifactChecked is a worker-private shadow of the per-MB
	// dot-artifact reset map. It is OR-merged into the encoder after all
	// rows finish so the hot picker does not share a write-heavy bool slice.
	dotArtifactChecked []bool

	keyFrameCoefTokenCounts vp8enc.InterCoefficientTokenCounts
	interCoefTokenCounts    vp8enc.InterCoefficientTokenCounts
	interCoefTokenRecords   vp8enc.InterCoefficientTokenRecords

	totalRate            int
	totalPredictionError int64
}

// reset re-initializes the per-row worker state for a fresh frame dispatch.
// Called by the row pool before handing the worker its next row index.
func (rs *rowEncoderState) reset(e *VP8Encoder, required int, preserveInterModeTestHits bool) {
	if rs == nil {
		return
	}
	preservedModeTestHits := rs.enc.interModeTestHitCounts
	rs.leftTok = vp8enc.TokenContextPlanes{}
	rs.totalRate = 0
	rs.totalPredictionError = 0
	vp8enc.ResetInterCoefficientTokenCounts(&rs.keyFrameCoefTokenCounts)
	vp8enc.ResetInterCoefficientTokenCounts(&rs.interCoefTokenCounts)
	vp8enc.ResetInterCoefficientTokenRecords(&rs.interCoefTokenRecords, 0, 0)
	if e == nil {
		return
	}
	rs.enc = *e
	if preserveInterModeTestHits {
		// setup_mbby_copy does not copy or clear mode_test_hit_counts for
		// helper workers. They persist across frames while mbs_tested_so_far
		// is reset by vp8cx_init_mbrthread_data.
		rs.enc.interModeTestHitCounts = preservedModeTestHits
	}
	rs.enc.rowWorkers = nil
	rs.enc.threadedRowsActive = true
	rs.enc.threadedDotArtifactBudget = e.threadedDotArtifactBudget
	rs.enc.reconstructScratch = rs.scratch
	if cap(rs.dotArtifactChecked) < required {
		rs.dotArtifactChecked = make([]bool, required)
	} else {
		rs.dotArtifactChecked = rs.dotArtifactChecked[:required]
		clear(rs.dotArtifactChecked)
	}
	rs.enc.dotArtifactChecked = rs.dotArtifactChecked
}

func (rs *rowEncoderState) finish() {
	if rs == nil {
		return
	}
	rs.scratch = rs.enc.reconstructScratch
}

const encoderCacheLineSize = 64

type paddedAtomicInt64 struct {
	value atomic.Int64
	_     [encoderCacheLineSize - 8]byte
}

func (p *paddedAtomicInt64) Load() int64 {
	return p.value.Load()
}

const (
	// Helpers usually receive the next frame immediately after finishing
	// their row slice. Spin briefly before parking so steady realtime
	// encodes avoid runtime sudog churn on the worker start channels.
	rowWorkerIdleSpinBudget       = 65536
	rowWorkerIdleSchedulerBackoff = 4096
)

func encoderThreadSyncRange(mbCols int) int {
	switch {
	case mbCols <= 0:
		return 1
	case mbCols < 40:
		return 1
	case mbCols <= 80:
		return 4
	case mbCols <= 160:
		return 8
	default:
		return 16
	}
}

// rowWorkerPool is the encoder-owned pool of pre-allocated row
// workers and the atomic wave-front coordination state. It is
// constructed once at NewVP8Encoder (only when Threads >= 2) and
// reused for the lifetime of the encoder. Pre-allocation matters
// for the spec's zero-cost-when-not-used invariant: a Threads=1
// encoder does not allocate this struct, does not spawn worker
// goroutines, and the Threads=1 hot path performs no atomic loads
// or channel ops introduced by this scaffolding.
//
// Coordination model (mirrors libvpx ethreading.c):
//   - rowProgress[r] is an atomic int storing the highest MB column
//     index that row r has finished. The row r+1 worker spin-waits
//     against rowProgress[r] before processing MB(r+1, c+nsync).
//   - syncRange mirrors libvpx's width-dependent mt_sync_range: narrower
//     pictures synchronize more often to keep wave-front depth available,
//     wider pictures synchronize less often to reduce atomic traffic.
type rowWorkerPool struct {
	// workers is sized by the EncoderOptions.Threads value (clamped
	// against runtime.NumCPU). Each worker has its own
	// rowEncoderState; the dispatcher hands one row at a time to
	// each free worker.
	workers []rowEncoderState

	// rowProgress[r] is the atomic wave-front counter for row r.
	// Sized to encoderMacroblockRows(height) at pool construction.
	rowProgress []paddedAtomicInt64

	// syncRange mirrors libvpx's cpi->mt_sync_range.
	syncRange int

	// dotArtifactBudget is the frame-global suppression cap shared across
	// worker-private encoder views. It keeps the libvpx ZEROMV-LAST bias
	// budget global instead of letting each worker spend its own copy.
	dotArtifactBudget atomic.Int32

	// start has one lane per helper worker. The main goroutine owns lane 0,
	// so only lanes [1, len(workers)) have persistent goroutines.
	start []chan struct{}

	// Frame-local dispatch state read by persistent worker goroutines.
	// The dispatcher writes these fields before sending on start and
	// rewrites them only after every active helper increments doneCount.
	encoder      *VP8Encoder
	job          rowWorkerJob
	keyArgs      threadedKeyRowsArgs
	args         threadedInterRowsArgs
	lfTrial      lfTrialArgs
	workerCount  int
	required     int
	abort        atomic.Int32
	doneCount    atomic.Int32
	workerErrors []error

	// shutdown closes when the encoder is finalized.
	shutdown chan struct{}

	// wg tracks persistent worker goroutines so Close can wait for them
	// to drain.
	wg sync.WaitGroup
}

// newRowWorkerPool allocates a pool sized for `threads` workers and
// `mbRows` MB-rows of progress storage. Returns nil if threads < 2 —
// the caller (NewVP8Encoder) treats that as the zero-cost serial path
// and never instantiates a pool.
func newRowWorkerPool(threads int, mbRows int, mbCols int) *rowWorkerPool {
	if threads < 2 || min(mbRows, mbCols) <= 0 {
		return nil
	}
	pool := &rowWorkerPool{
		workers:      make([]rowEncoderState, threads),
		rowProgress:  make([]paddedAtomicInt64, mbRows),
		syncRange:    encoderThreadSyncRange(mbCols),
		start:        make([]chan struct{}, threads),
		workerErrors: make([]error, threads),
		shutdown:     make(chan struct{}),
	}
	for i := 1; i < len(pool.workers); i++ {
		start := make(chan struct{})
		pool.start[i] = start
		pool.wg.Add(1)
		go pool.workerLoop(i, start)
	}
	return pool
}

func (p *rowWorkerPool) workerLoop(workerIndex int, start <-chan struct{}) {
	defer p.wg.Done()
	for {
		if !p.waitForWorkerStart(start) {
			return
		}
		workerCount := p.workerCount
		if workerIndex >= workerCount {
			continue
		}
		switch p.job {
		case rowWorkerJobKeyFrame:
			p.runThreadedKeyFrameWorker(workerIndex)
		case rowWorkerJobLFTrial:
			p.runLFTrialWorker(workerIndex)
		default:
			p.runThreadedInterFrameWorker(workerIndex)
		}
		p.doneCount.Add(1)
	}
}

func (p *rowWorkerPool) waitForWorkerStart(start <-chan struct{}) bool {
	for spins := range rowWorkerIdleSpinBudget {
		select {
		case _, ok := <-start:
			return ok
		default:
		}
		runtimeProcYield(30)
		if spins >= rowWorkerIdleSchedulerBackoff && spins%rowWorkerIdleSchedulerBackoff == 0 {
			runtime.Gosched()
		}
	}
	_, ok := <-start
	return ok
}

func (p *rowWorkerPool) startHelperWorkers() {
	if p == nil || p.workerCount <= 1 {
		return
	}
	p.doneCount.Store(0)
	for workerIndex := 1; workerIndex < p.workerCount; workerIndex++ {
		p.start[workerIndex] <- struct{}{}
	}
}

func (p *rowWorkerPool) waitHelperWorkers() {
	if p == nil || p.workerCount <= 1 {
		return
	}
	want := int32(p.workerCount - 1)
	for spins := 0; p.doneCount.Load() < want; spins++ {
		runtimeProcYield(30)
		if spins >= rowWorkerIdleSchedulerBackoff && spins%rowWorkerIdleSchedulerBackoff == 0 {
			runtime.Gosched()
		}
	}
}

type rowWorkerJob uint8

const (
	rowWorkerJobInterFrame rowWorkerJob = iota
	rowWorkerJobKeyFrame
	rowWorkerJobLFTrial
)

func (p *rowWorkerPool) runThreadedInterFrameWorker(workerIndex int) {
	workerCount := p.workerCount
	if workerCount <= 0 {
		return
	}
	worker := &p.workers[workerIndex]
	worker.reset(p.encoder, p.required, workerIndex > 0)
	vp8enc.ResetInterCoefficientTokenRecords(&worker.interCoefTokenRecords, p.args.rows, threadedWorkerMacroblockCount(workerIndex, workerCount, p.args.rows, p.args.cols))
	worker.enc.threadedHelperRowsActive = workerIndex > 0
	defer worker.finish()
	var err error
	for row := workerIndex; row < p.args.rows; row += workerCount {
		if p.abort.Load() != 0 {
			break
		}
		rate, rowErr := worker.encodeThreadedInterFrameRow(p, &p.args, row, &p.abort)
		if rowErr != nil {
			err = rowErr
			p.abort.Store(1)
			break
		}
		worker.totalRate = addProjectedMacroblockRate(worker.totalRate, rate)
	}
	p.workerErrors[workerIndex] = err
}

func threadedWorkerMacroblockCount(workerIndex int, workerCount int, rows int, cols int) int {
	if workerIndex < 0 || workerCount <= 0 || rows <= 0 || cols <= 0 || workerIndex >= rows {
		return 0
	}
	workerRows := 1 + (rows-1-workerIndex)/workerCount
	return workerRows * cols
}

func (p *rowWorkerPool) runThreadedKeyFrameWorker(workerIndex int) {
	workerCount := p.workerCount
	if workerCount <= 0 {
		return
	}
	worker := &p.workers[workerIndex]
	worker.reset(p.encoder, p.required, workerIndex > 0)
	worker.enc.threadedHelperRowsActive = workerIndex > 0
	defer worker.finish()
	var err error
	for row := workerIndex; row < p.keyArgs.rows; row += workerCount {
		if p.abort.Load() != 0 {
			break
		}
		rate, rowErr := worker.encodeThreadedKeyFrameRow(p, &p.keyArgs, row, &p.abort)
		if rowErr != nil {
			err = rowErr
			p.abort.Store(1)
			break
		}
		worker.totalRate = addProjectedMacroblockRate(worker.totalRate, rate)
	}
	p.workerErrors[workerIndex] = err
}

// reset re-initializes the per-row progress counters for a fresh
// frame. Called by the encoder at the start of every parallel
// inter-frame attempt.
func (p *rowWorkerPool) reset(mbRows int) {
	if p == nil {
		return
	}
	p.dotArtifactBudget.Store(0)
	if cap(p.rowProgress) < mbRows {
		p.rowProgress = make([]paddedAtomicInt64, mbRows)
	} else {
		p.rowProgress = p.rowProgress[:mbRows]
	}
	for i := range p.rowProgress {
		p.rowProgress[i].value.Store(-1)
	}
}

func (p *rowWorkerPool) resetForEncoderReset() {
	if p == nil {
		return
	}
	for i := range p.workers {
		p.workers[i] = rowEncoderState{}
	}
	for i := range p.rowProgress {
		p.rowProgress[i].value.Store(0)
	}
	p.dotArtifactBudget.Store(0)
	p.encoder = nil
	p.job = rowWorkerJobInterFrame
	p.keyArgs = threadedKeyRowsArgs{}
	p.args = threadedInterRowsArgs{}
	p.lfTrial = lfTrialArgs{}
	p.workerCount = 0
	p.required = 0
	p.abort.Store(0)
	p.doneCount.Store(0)
	for i := range p.workerErrors {
		p.workerErrors[i] = nil
	}
}

// publishRowColumn updates the wave-front counter for row r so that
// row r+1's worker can advance. Mirrors libvpx's
// vpx_atomic_store_release(current_mb_col, mb_col - 1) at every
// nsync columns.
func (p *rowWorkerPool) publishRowColumn(r int, col int) {
	if p == nil || uint(r) >= uint(len(p.rowProgress)) {
		return
	}
	p.rowProgress[r].value.Store(int64(col))
}

// waitForAboveColumn spin-waits until row r-1 has published a column
// index of at least `col`. Mirrors vp8_atomic_spin_wait in
// vp8/encoder/ethreading.c. Returns immediately when r == 0.
func (p *rowWorkerPool) waitForAboveColumn(r int, col int) {
	_ = p.waitForAboveColumnAbort(r, col, nil)
}

func (p *rowWorkerPool) waitForAboveColumnAbort(r int, col int, abort *atomic.Int32) bool {
	if p == nil || r <= 0 || r >= len(p.rowProgress) {
		return true
	}
	target := int64(col)
	above := &p.rowProgress[r-1].value
	const (
		spinBudget       = 4096
		schedulerBackoff = 256
	)
	for i := 0; ; i++ {
		if above.Load() >= target {
			return true
		}
		if abort != nil && abort.Load() != 0 {
			return false
		}
		runtimeProcYield(30)
		if i >= spinBudget && i%schedulerBackoff == 0 {
			runtime.Gosched()
		}
	}
}

// shutdownPool tears down the worker pool.
func (p *rowWorkerPool) shutdownPool() {
	if p == nil {
		return
	}
	select {
	case <-p.shutdown:
		// already closed
	default:
		close(p.shutdown)
		for workerIndex := 1; workerIndex < len(p.start); workerIndex++ {
			close(p.start[workerIndex])
		}
	}
	p.wg.Wait()
}
