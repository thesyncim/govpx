package govpx

import (
	"runtime"
	"sync"
	"sync/atomic"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9RowWorkerPool is the per-tile-column pool of row goroutines that drive
// the wavefront primitive established by vp9RowMTSync. It mirrors libvpx's
// per-tile-column row worker layer (vp9/encoder/vp9_ethread.c) and reuses
// the rowWorkerIdleSpinBudget/runtimeProcYield idle path used by the VP8
// row worker pool so steady-state encodes do not park on sync.Cond.
//
// Each worker owns a vp9RowEncoderState — a per-row clone of the encoder's
// mutable scratch (above/left segment contexts, per-plane entropy contexts,
// partition reconstruction scratch, inter predictor scratch, frame counts
// accumulator). The shared frame-level state (reference frames, frame
// context probabilities, loopfilter level table) remains read-only during
// the wavefront pass.
//
// Lifecycle: the pool is allocated lazily by ensureRowWorkers when the
// RowMT option is engaged and a tile-column body is about to dispatch
// rows. Workers persist for the encoder's lifetime; shutdownPool closes
// the start channels and the goroutines drain. Worker count is clamped to
// min(rowMTThreads, sbRows) per the task's dispatch rule.
type vp9RowWorkerPool struct {
	workers []vp9RowEncoderState

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
	encode func(workerIndex, row int, state *vp9RowEncoderState) error
}

// vp9RowEncoderState is the per-row mutable working set. It is cloned
// from the tile-column worker once per frame so each row goroutine reads
// and writes its own buffers. Shared frame-level state is accessed via
// the parent pointer (read-only during the wavefront pass).
type vp9RowEncoderState struct {
	parent *VP9Encoder

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

// reset arms the row encoder state for a fresh frame on the given
// dimensions. It allocates per-row scratch lazily; subsequent calls
// reuse the cached capacity so steady-state encodes do not allocate.
func (s *vp9RowEncoderState) reset(parent *VP9Encoder, miColsAligned int) {
	if s == nil {
		return
	}
	s.parent = parent
	if cap(s.leftSegCtx) < vp9MiBlockSize() {
		s.leftSegCtx = make([]int8, vp9MiBlockSize())
	} else {
		s.leftSegCtx = s.leftSegCtx[:vp9MiBlockSize()]
		for i := range s.leftSegCtx {
			s.leftSegCtx[i] = 0
		}
	}
	for plane := range vp9dec.MaxMbPlane {
		leftLen := vp9PlaneEntropyLen(vp9MiBlockSize(), parent.planes[plane].SubsamplingY)
		if cap(s.planeLeftCtx[plane]) < leftLen {
			s.planeLeftCtx[plane] = make([]uint8, leftLen)
		} else {
			s.planeLeftCtx[plane] = s.planeLeftCtx[plane][:leftLen]
			for i := range s.planeLeftCtx[plane] {
				s.planeLeftCtx[plane][i] = 0
			}
		}
	}
	if cap(s.partitionReconScratch) < vp9MaxPartitionReconScratch {
		s.partitionReconScratch = make([]byte, vp9MaxPartitionReconScratch)
	} else {
		s.partitionReconScratch = s.partitionReconScratch[:vp9MaxPartitionReconScratch]
	}
	if cap(s.interPredictScratch) < vp9MaxPartitionReconScratch {
		s.interPredictScratch = make([]byte, vp9MaxPartitionReconScratch)
	} else {
		s.interPredictScratch = s.interPredictScratch[:vp9MaxPartitionReconScratch]
	}
	s.counts = encoder.FrameCounts{}
	_ = miColsAligned
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
	if job.encode == nil {
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
		if err := job.encode(workerIndex, row, state); err != nil {
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
func (p *vp9RowWorkerPool) reset(parent *VP9Encoder, miColsAligned int) {
	if p == nil {
		return
	}
	for i := range p.workers {
		p.workers[i].reset(parent, miColsAligned)
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
