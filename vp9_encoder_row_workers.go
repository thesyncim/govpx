package govpx

import (
	"runtime"
	"sync"
	"sync/atomic"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// vp9RowWorkerPool is the per-tile-column pool of row goroutines that drive
// the wavefront primitive established by vp9RowMTSync. It mirrors libvpx's
// per-tile-column row worker layer (vp9/encoder/vp9_ethread.c) and reuses
// the rowWorkerIdleSpinBudget/runtimeProcYield idle path used by the VP8
// row worker pool so steady-state encodes do not park on sync.Cond.
//
// Each worker owns a vp9RowEncoderState containing a persistent encoder clone
// and private decision, transform, predictor, coefficient, left-context, and
// count scratch. Committed reconstruction, mode-info, above-context, and
// row-indexed partition/RD state are shared under wavefront row ownership.
//
// Lifecycle: the pool is allocated lazily by ensureRowWorkers when the
// RowMT option is engaged and a tile-column body is about to dispatch
// rows. Workers persist for the encoder's lifetime; shutdownPool closes
// the start channels and the goroutines drain. The global thread budget is
// divided across tile columns, then clamped to the tile's SB-row count.
type vp9RowWorkerPool struct {
	workers []vp9RowEncoderState
	// rowTokens owns one independent token arena per SB row. Workers write
	// disjoint arenas concurrently; the tile dispatcher merges them in row
	// order after the wavefront completes.
	rowTokens []encoder.TokenFrameBuffer

	// start[i] gates worker i. Workers spin on rowWorkerIdleSpinBudget
	// before parking; the dispatcher sends on start when a row job is
	// queued, matching the VP8 rowWorkerPool latency profile.
	start    []chan struct{}
	shutdown chan struct{}
	wg       sync.WaitGroup

	// queue is the shared FIFO of SB-row indices the row workers consume.
	// nextRow is the atomic claim cursor; rows are popped via FetchAdd so
	// any free worker can grab the next row without a global lock.
	queue   []int
	nextRow atomic.Int32

	// doneCount tracks completion so the dispatcher can join the workers
	// without sync.WaitGroup overhead per row job (mirrors the VP8
	// rowWorkerPool waitHelperWorkers shape).
	doneCount atomic.Int32

	// abort is set by any worker that hits an error; remaining workers
	// observe it and exit their row claim loop early.
	abort atomic.Int32

	// errors[i] receives worker i's first error so the dispatcher can
	// surface it without losing the partial-progress count of how many
	// rows completed before the failure.
	errors []error

	// job is the callback the worker loop invokes for each claimed row.
	// It is rewritten by the dispatcher before each batch of rows so the
	// same worker pool can serve multiple frame-encode phases (count
	// prepass today, bitstream pass once the decision/emission split
	// lands).
	job vp9RowWorkerJob

	workerCount int
}

// vp9RowWorkerJob carries the per-row callback dispatched onto the pool.
// The callback receives the row index and the worker's private encoder
// state clone; it is responsible for calling rowMTSync.read before
// consuming above-context and rowMTSync.write after publishing it.
type vp9RowWorkerJob struct {
	encode     func(workerIndex, row int, state *vp9RowEncoderState) error
	countJob   *vp9CountTileJob
	countInter *vp9InterEncodeState
}

func (j vp9RowWorkerJob) run(workerIndex, row int,
	state *vp9RowEncoderState,
) error {
	if j.countJob != nil {
		return runVP9CountTileRow(j.countJob, j.countInter, row, state)
	}
	if j.encode != nil {
		return j.encode(workerIndex, row, state)
	}
	return nil
}

// vp9RowEncoderState is one row worker's persistent mutable working set. It is
// prepared from the tile-column worker once per count pass while retaining its
// privately owned buffers across frames.
type vp9RowEncoderState struct {
	parent *VP9Encoder
	worker VP9Encoder

	privateAboveSegCtx   []int8
	privatePlaneAboveCtx [vp9dec.MaxMbPlane][]uint8
	privateVarPart       vp9RowPrivateVarPartState

	// leftSegCtx is the per-row left-side partition history; each row
	// owns its own copy because the SB column loop within a row mutates
	// it sequentially. Above-context state is shared across rows and
	// gated via vp9RowMTSync.
	leftSegCtx []int8

	// planeLeftCtx mirrors the per-plane left-entropy context that
	// libvpx clones into each tile-column row worker. Above-context is
	// shared (wavefront-synchronized); left-context is row-private.
	planeLeftCtx [vp9dec.MaxMbPlane][]uint8

	// partitionReconScratch is the per-row reconstruction scratch
	// passed through the decoder-shared inter predictor path. Each row
	// owns its own buffer to avoid races during the wavefront pass.
	partitionReconScratch []byte

	// interPredictScratch is the per-row predictor scratch. Mirrors the
	// per-tile-column clone in prepareVP9TileEncodeWorker.
	interPredictScratch []byte

	// counts is the per-row counts accumulator. The dispatcher folds
	// these into the tile-column counts via addVP9FrameCounts after all
	// rows finish; counts addition is commutative so the wavefront order
	// does not perturb the final accumulation.
	counts encoder.FrameCounts
}

type vp9RowPrivateVarPartState struct {
	grid                []vp9dec.NeighborMi
	sbComputed          []bool
	sbUseMvPart         []bool
	sbMvPart            []vp9dec.MV
	sbPredLast          []vp9dec.MV
	sbPredValid         []bool
	sbVarLow            [][25]uint8
	sbCopiedPartition   []bool
	sbSegmentID         []uint8
	sbContentState      []encoder.ContentStateSB
	sbContentStateValid []bool
	sbZeroTempSADSource []bool
	sbColorSensitivity  [][2]bool
	sbLastHighContent   []uint8
	sbLastHighContentOK []bool
}

func (s *vp9RowPrivateVarPartState) restore(w *VP9Encoder) {
	if s == nil || w == nil || s.grid == nil {
		return
	}
	w.varPartGrid = s.grid
	w.varPartSBComputed = s.sbComputed
	w.varPartSBUseMvPart = s.sbUseMvPart
	w.varPartSBMvPart = s.sbMvPart
	w.varPartSBPredLast = s.sbPredLast
	w.varPartSBPredValid = s.sbPredValid
	w.varPartSBVarLow = s.sbVarLow
	w.varPartSBCopiedPartition = s.sbCopiedPartition
	w.varPartSBSegmentID = s.sbSegmentID
	w.varPartSBContentState = s.sbContentState
	w.varPartSBContentStateValid = s.sbContentStateValid
	w.varPartSBZeroTempSADSource = s.sbZeroTempSADSource
	w.varPartSBColorSensitivity = s.sbColorSensitivity
	w.varPartSBLastHighContent = s.sbLastHighContent
	w.varPartSBLastHighContentValid = s.sbLastHighContentOK
}

func (s *vp9RowPrivateVarPartState) capture(w *VP9Encoder) {
	if s == nil || w == nil {
		return
	}
	s.grid = w.varPartGrid
	s.sbComputed = w.varPartSBComputed
	s.sbUseMvPart = w.varPartSBUseMvPart
	s.sbMvPart = w.varPartSBMvPart
	s.sbPredLast = w.varPartSBPredLast
	s.sbPredValid = w.varPartSBPredValid
	s.sbVarLow = w.varPartSBVarLow
	s.sbCopiedPartition = w.varPartSBCopiedPartition
	s.sbSegmentID = w.varPartSBSegmentID
	s.sbContentState = w.varPartSBContentState
	s.sbContentStateValid = w.varPartSBContentStateValid
	s.sbZeroTempSADSource = w.varPartSBZeroTempSADSource
	s.sbColorSensitivity = w.varPartSBColorSensitivity
	s.sbLastHighContent = w.varPartSBLastHighContent
	s.sbLastHighContentOK = w.varPartSBLastHighContentValid
}

func (s *vp9RowEncoderState) resetCountWorker(parent *VP9Encoder,
	width, height, miRows, miCols int,
) {
	if s == nil || parent == nil {
		return
	}
	s.parent = parent
	if s.privateAboveSegCtx != nil {
		s.worker.aboveSegCtx = s.privateAboveSegCtx
		for plane := range vp9dec.MaxMbPlane {
			s.worker.planes[plane].AboveContext = s.privatePlaneAboveCtx[plane]
		}
	}
	s.privateVarPart.restore(&s.worker)
	s.worker.prepareVP9CountWorker(parent, width, height, miRows, miCols)
	w := &s.worker
	s.privateAboveSegCtx = w.aboveSegCtx
	for plane := range vp9dec.MaxMbPlane {
		s.privatePlaneAboveCtx[plane] = w.planes[plane].AboveContext
	}
	s.privateVarPart.capture(w)

	// Rows share committed frame outputs, above contexts, and row-indexed
	// partition/RD state. Decision caches, transform, predictor, coefficient,
	// and left-context scratch remain worker-private.
	w.aboveSegCtx = parent.aboveSegCtx
	w.varPartGrid = parent.varPartGrid
	w.varPartSBComputed = parent.varPartSBComputed
	w.varPartSBUseMvPart = parent.varPartSBUseMvPart
	w.varPartSBMvPart = parent.varPartSBMvPart
	w.varPartSBPredLast = parent.varPartSBPredLast
	w.varPartSBPredValid = parent.varPartSBPredValid
	w.varPartSBVarLow = parent.varPartSBVarLow
	w.varPartSBCopiedPartition = parent.varPartSBCopiedPartition
	w.varPartSBSegmentID = parent.varPartSBSegmentID
	w.varPartSBContentState = parent.varPartSBContentState
	w.varPartSBContentStateValid = parent.varPartSBContentStateValid
	w.varPartSBZeroTempSADSource = parent.varPartSBZeroTempSADSource
	w.varPartSBColorSensitivity = parent.varPartSBColorSensitivity
	w.varPartSBLastHighContent = parent.varPartSBLastHighContent
	w.varPartSBLastHighContentValid = parent.varPartSBLastHighContentValid
	w.rdThresh = parent.rdThresh
	for plane := range vp9dec.MaxMbPlane {
		w.planes[plane].AboveContext = parent.planes[plane].AboveContext
	}
	w.vp9FilterDiff = [vp9dec.SwitchableFilterContexts]int64{}
	s.counts = encoder.FrameCounts{}
}

// reset arms the row encoder state for a fresh frame on the given
// dimensions. It allocates per-row scratch lazily; subsequent calls
// reuse the cached capacity so steady-state encodes do not allocate.
func (s *vp9RowEncoderState) reset(parent *VP9Encoder) {
	if s == nil {
		return
	}
	s.parent = parent
	s.leftSegCtx = buffers.EnsureLenZeroed(s.leftSegCtx, vp9MiBlockSize())
	for plane := range vp9dec.MaxMbPlane {
		leftLen := vp9dec.PlaneEntropyLen(vp9MiBlockSize(), parent.planes[plane].SubsamplingY)
		s.planeLeftCtx[plane] = buffers.EnsureLenZeroed(s.planeLeftCtx[plane], leftLen)
	}
	s.partitionReconScratch = buffers.EnsureLen(s.partitionReconScratch, vp9MaxPartitionReconScratchStack)
	s.interPredictScratch = buffers.EnsureLen(s.interPredictScratch, vp9MaxPartitionReconScratch)
	s.counts = encoder.FrameCounts{}
}

// release drops the row state arrays. Called when SetRowMT(false) flips
// the option off so steady-state allocation gates can verify the row
// scratch is freed.
func (s *vp9RowEncoderState) release() {
	if s == nil {
		return
	}
	s.parent = nil
	s.leftSegCtx = s.leftSegCtx[:0]
	for plane := range vp9dec.MaxMbPlane {
		s.planeLeftCtx[plane] = s.planeLeftCtx[plane][:0]
	}
	s.partitionReconScratch = s.partitionReconScratch[:0]
	s.interPredictScratch = s.interPredictScratch[:0]
	s.counts = encoder.FrameCounts{}
}

// vp9MiBlockSize returns the SB-MI block stride (8 MI per 64x64 SB).
func vp9MiBlockSize() int {
	// Defined as common.MiBlockSize but inlined here to avoid importing
	// the common package just for the constant. The value (8) is fixed
	// by VP9's SB granularity and validated by tests against
	// common.MiBlockSize.
	return 8
}

// newVP9RowWorkerPool allocates a row worker pool sized for the worker
// count. Each worker runs a persistent goroutine that consumes rows from
// the shared queue via atomic FetchAdd; the dispatcher refills the queue
// per row-batch and signals via the start channels.
//
// workers <= 1 returns nil because a single goroutine collapses to the
// serial path; callers (vp9TileWorkerPool.ensureRowWorkers) skip pool
// allocation in that case so the zero-cost-when-not-used invariant
// holds for Threads=1 encoders.
func newVP9RowWorkerPool(workers int) *vp9RowWorkerPool {
	if workers <= 1 {
		return nil
	}
	pool := &vp9RowWorkerPool{
		workers:     make([]vp9RowEncoderState, workers),
		start:       make([]chan struct{}, workers),
		shutdown:    make(chan struct{}),
		errors:      make([]error, workers),
		workerCount: workers,
	}
	for i := 1; i < workers; i++ {
		ch := make(chan struct{})
		pool.start[i] = ch
		pool.wg.Add(1)
		go pool.workerLoop(i, ch)
	}
	return pool
}

func (p *vp9RowWorkerPool) workerLoop(workerIndex int, start <-chan struct{}) {
	defer p.wg.Done()
	for {
		if !p.waitForWorkerStart(start) {
			return
		}
		if workerIndex < p.workerCount {
			p.consumeRows(workerIndex)
		}
		p.doneCount.Add(1)
	}
}

func (p *vp9RowWorkerPool) waitForWorkerStart(start <-chan struct{}) bool {
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

// consumeRows is the per-worker hot loop. It claims the next row index
// from the shared queue via atomic FetchAdd, dispatches it to the
// configured callback, and stops when the queue is exhausted or another
// worker has flagged abort. Returning leaves p.errors[workerIndex] set
// to the first error the worker hit, so the dispatcher can surface it.
func (p *vp9RowWorkerPool) consumeRows(workerIndex int) {
	job := p.job
	if job.encode == nil && job.countJob == nil {
		return
	}
	state := &p.workers[workerIndex]
	for {
		if p.abort.Load() != 0 {
			return
		}
		idx := p.nextRow.Add(1) - 1
		if int(idx) >= len(p.queue) {
			return
		}
		row := p.queue[idx]
		if err := job.run(workerIndex, row, state); err != nil {
			if p.errors[workerIndex] == nil {
				p.errors[workerIndex] = err
			}
			p.abort.Store(1)
			return
		}
	}
}

// dispatch arms the pool for one row-batch. The caller populates the
// queue with the SB-row indices to process (typically 0..sbRows-1 for a
// tile column), sets the per-row callback, and invokes dispatch which
// signals all helper workers, drives row 0..N-1 on the calling goroutine
// while helpers consume in parallel, and finally joins helpers via the
// idle-spin/Gosched join loop.
//
// Returns the first error any worker hit, or nil. Workers continue
// consuming after their own error path returns; abort propagates so
// stragglers exit promptly.
func (p *vp9RowWorkerPool) dispatch(rows []int, job vp9RowWorkerJob) error {
	if p == nil || p.workerCount <= 0 {
		return nil
	}
	p.queue = rows
	p.job = job
	p.nextRow.Store(0)
	p.doneCount.Store(0)
	p.abort.Store(0)
	for i := range p.errors {
		p.errors[i] = nil
	}
	// Wake helpers (workers 1..workerCount-1).
	for workerIndex := 1; workerIndex < p.workerCount; workerIndex++ {
		p.start[workerIndex] <- struct{}{}
	}
	// Drive worker 0 on the dispatcher goroutine.
	if p.workerCount > 0 {
		p.consumeRows(0)
	}
	// Join helpers.
	want := int32(p.workerCount - 1)
	for spins := 0; p.doneCount.Load() < want; spins++ {
		runtimeProcYield(30)
		if spins >= rowWorkerIdleSchedulerBackoff && spins%rowWorkerIdleSchedulerBackoff == 0 {
			runtime.Gosched()
		}
	}
	for _, err := range p.errors {
		if err != nil {
			return err
		}
	}
	return nil
}

// reset arms each per-row state for a fresh frame on the given parent
// encoder. It is called by the tile-column dispatcher before
// dispatch() so the workers see freshly-zeroed scratch.
func (p *vp9RowWorkerPool) reset(parent *VP9Encoder) {
	if p == nil {
		return
	}
	for i := range p.workers {
		p.workers[i].reset(parent)
	}
}

func (p *vp9RowWorkerPool) resetCountWorkers(parent *VP9Encoder,
	width, height, miRows, miCols, tileMiCols, tileCol, sbRows int,
) {
	if p == nil {
		return
	}
	sbCount := ((miRows + 7) >> 3) * ((miCols + 7) >> 3)
	parent.varPartSBContentState = buffers.EnsureLen(parent.varPartSBContentState, sbCount)
	parent.varPartSBContentStateValid = buffers.EnsureLenZeroed(
		parent.varPartSBContentStateValid, sbCount)
	parent.varPartSBZeroTempSADSource = buffers.EnsureLenZeroed(
		parent.varPartSBZeroTempSADSource, sbCount)
	parent.varPartSBLastHighContent = buffers.EnsureLen(parent.varPartSBLastHighContent, sbCount)
	parent.varPartSBLastHighContentValid = buffers.EnsureLenZeroed(
		parent.varPartSBLastHighContentValid, sbCount)
	for i := range p.workers {
		p.workers[i].resetCountWorker(parent, width, height, miRows, miCols)
	}
	p.rowTokens = buffers.EnsureLen(p.rowTokens, sbRows)
	for row := range sbRows {
		p.rowTokens[row].EnsureForTile(vp9MiBlockSize(), tileMiCols, 0, tileCol)
	}
}

// release drops the per-row scratch arrays. Invoked when the RowMT
// option toggles off so the steady-state allocation gate stays clean.
func (p *vp9RowWorkerPool) release() {
	if p == nil {
		return
	}
	for i := range p.workers {
		p.workers[i].release()
	}
	for i := range p.rowTokens {
		p.rowTokens[i].Release()
	}
	p.rowTokens = p.rowTokens[:0]
}

// shutdownPool stops the persistent worker goroutines and waits for them
// to drain. Called from the encoder's Close path and from the tile
// worker pool when the encoder reconfigures.
func (p *vp9RowWorkerPool) shutdownPool() {
	if p == nil {
		return
	}
	select {
	case <-p.shutdown:
	default:
		close(p.shutdown)
		for workerIndex := 1; workerIndex < len(p.start); workerIndex++ {
			if p.start[workerIndex] != nil {
				close(p.start[workerIndex])
			}
		}
	}
	p.wg.Wait()
}

// vp9RowMTThreadCount mirrors the libvpx clamp used in
// vp9_encode_tiles_row_mt: a tile column dispatches at most one row
// worker per SB row and at most rowMTThreads workers. Returns the
// effective worker count (>= 1).
func vp9RowMTThreadCount(rowMTThreads, sbRows int) int {
	if sbRows <= 0 {
		return 1
	}
	if rowMTThreads <= 1 {
		return 1
	}
	if sbRows < rowMTThreads {
		return sbRows
	}
	return rowMTThreads
}

func vp9RowMTThreadsPerTile(totalThreads, tileCols, sbRows int) int {
	if tileCols <= 0 {
		tileCols = 1
	}
	threads := (totalThreads + tileCols - 1) / tileCols
	return vp9RowMTThreadCount(threads, sbRows)
}
