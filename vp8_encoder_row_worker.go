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

	// pickerActZbinAdj is the libvpx-stale activity-driven zbin
	// adjustment threaded into the per-MB RD picker. With segmentation
	// disabled (the path mirrored here), libvpx's b->zbin_extra is
	// only refreshed by vp8_update_zbin_extra inside
	// vp8cx_encode_intra_macroblock (vp8/encoder/encodeframe.c line
	// 1126), which runs AFTER vp8_rd_pick_intra_mode. So each MB's
	// picker quantize step reads zbin_extra computed from the PREVIOUS
	// MB's post-pick act_zbin_adj
	// (ZBIN_EXTRA_Y at vp8_quantize.c lines 276-279).
	//
	// In the threaded path the carry survives both within a row AND
	// across the rows a single worker handles (workerIndex,
	// workerIndex+workerCount, workerIndex+2*workerCount, ...): libvpx
	// neither the helper thread loop (ethreading.c:76-310) nor the main
	// thread's encode_mb_row (encodeframe.c:316-575) touches
	// b->zbin_extra or x->act_zbin_adj between rows.
	//
	// runThreadedKeyFrameWorker / runThreadedInterFrameWorker seed
	// pickerActZbinAdj ONCE per worker dispatch:
	//   - workerIndex == 0 ⇒ activityProbeStaleActZbinAdj (mirrors main
	//     thread, whose b->zbin_extra was set by
	//     vp8cx_frame_init_quantizer using the prev-attempt's last-MB
	//     act_zbin_adj).
	//   - workerIndex > 0  ⇒ 0 (mirrors helper threads' MB_ROW_COMP[i].mb
	//     block[i].zbin_extra zero-init from
	//     vp8cx_create_encoder_threads:521-523 memset plus setup_mbby_copy
	//     copying the master's freshly zeroed act_zbin_adj).
	// See vp8_encoder_reconstruct.go for the single-thread anchor.
	pickerActZbinAdj int

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
	rs.interCoefTokenRecords.Reset(0, 0)
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
	// Sized to geometry.MacroblockRows(height) at pool construction.
	rowProgress []paddedAtomicInt64

	// syncRange mirrors libvpx's cpi->mt_sync_range.
	syncRange int

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
	lfApply      lfApplyArgs
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
		case rowWorkerJobLFApply:
			p.runLFApplyWorker(workerIndex)
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
	rowWorkerJobLFApply
)

func (p *rowWorkerPool) runThreadedInterFrameWorker(workerIndex int) {
	workerCount := p.workerCount
	if workerCount <= 0 {
		return
	}
	worker := &p.workers[workerIndex]
	worker.reset(p.encoder, p.required, workerIndex > 0)
	worker.interCoefTokenRecords.Reset(p.args.rows, threadedWorkerMacroblockCount(workerIndex, workerCount, p.args.rows, p.args.cols))
	worker.enc.threadedHelperRowsActive = workerIndex > 0
	// Same libvpx anchor as runThreadedKeyFrameWorker: workerIndex==0 maps
	// to libvpx's main thread (b->zbin_extra seeded from prev-attempt's
	// stale act_zbin_adj via vp8cx_frame_init_quantizer); workerIndex>0
	// maps to libvpx helper threads (b->zbin_extra zero-init,
	// act_zbin_adj=0 from setup_mbby_copy post-init_encode_frame_mb_context).
	// The inter-frame picker does not currently thread pickerActZbinAdj
	// through the per-MB candidate evaluation (the inter rdopt path reads
	// b->zbin_extra after vp8_update_zbin_extra runs INSIDE the candidate
	// loop at rdopt.c:1930 — see vp8_encoder_inter_modes_rd.go), but we keep
	// the field consistent so future inter-side ports can read it without
	// stitching.
	if workerIndex == 0 {
		worker.pickerActZbinAdj = worker.enc.activityProbeStaleActZbinAdj
	} else {
		worker.pickerActZbinAdj = 0
	}
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
	// libvpx vp8/encoder/ethreading.c thread_encoding_proc:
	// helper workers (ithread = 0..encoding_thread_count-1) operate on
	// MB_ROW_COMP[ithread].mb whose block[i].zbin_extra is zero-init
	// (vpx_memalign + memset at vp8cx_create_encoder_threads:521-523) and
	// whose act_zbin_adj was copied from the master's just-zeroed value
	// (setup_mbby_copy at ethreading.c:374 runs AFTER
	// init_encode_frame_mb_context sets x->act_zbin_adj=0 at
	// encodeframe.c:588). Each helper thread carries that state across
	// every row it handles (workerIndex+k*workerCount, k=0,1,2,...) — the
	// row loop never resets b->zbin_extra or x->act_zbin_adj.
	//
	// The MAIN thread (mapped to workerIndex==0 here, which handles rows
	// {0, workerCount, 2*workerCount, ...} — mirroring libvpx's
	// `for (mb_row = 0; ...; mb_row += encoding_thread_count + 1)` at
	// encodeframe.c:778-779) uses cpi->mb directly; its block[i].zbin_extra
	// was just rewritten by vp8cx_frame_init_quantizer (encodeframe.c:719)
	// using x->act_zbin_adj left over from the previous attempt's last MB.
	// govpx mirrors that via activityProbeStaleActZbinAdj.
	if workerIndex == 0 {
		worker.pickerActZbinAdj = worker.enc.activityProbeStaleActZbinAdj
	} else {
		worker.pickerActZbinAdj = 0
	}
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
	p.encoder = nil
	p.job = rowWorkerJobInterFrame
	p.keyArgs = threadedKeyRowsArgs{}
	p.args = threadedInterRowsArgs{}
	p.lfTrial = lfTrialArgs{}
	p.lfApply = lfApplyArgs{}
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
