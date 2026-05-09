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

	// leftTok is the persistent left token-context scratch for the
	// row's MB column traversal. libvpx zeroes this at the start of
	// every row and updates it as each MB commits its tokens (see
	// vp8/encoder/encodeframe.c encode_mb_row).
	leftTok vp8enc.TokenContextPlanes

	// scratch is the row-private intra reconstruction scratch reused
	// across the row's MB columns. Mirrors libvpx MACROBLOCK *x's
	// per-thread workspace from setup_mbby_copy.
	scratch vp8dec.IntraReconstructionScratch
}

// reset re-initializes the per-row worker state for a fresh frame
// dispatch. Called by the row pool before handing the worker its
// next row index.
func (rs *rowEncoderState) reset(_ *VP8Encoder) {
	if rs == nil {
		return
	}
	rs.leftTok = vp8enc.TokenContextPlanes{}
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
//   - syncRange (libvpx's mt_sync_range; default 8) is the number of
//     MB columns a row may run ahead of the row below it.
type rowWorkerPool struct {
	// workers is sized by the EncoderOptions.Threads value (clamped
	// against runtime.NumCPU). Each worker has its own
	// rowEncoderState; the dispatcher hands one row at a time to
	// each free worker.
	workers []rowEncoderState

	// rowProgress[r] is the atomic wave-front counter for row r.
	// Sized to encoderMacroblockRows(height) at pool construction.
	rowProgress []atomic.Int64

	// syncRange mirrors libvpx's cpi->mt_sync_range.
	syncRange int

	// shutdown closes when the encoder is being reset or finalized.
	shutdown chan struct{}

	// wg tracks any spawned worker goroutines so Reset / GC can
	// wait for them to drain.
	wg sync.WaitGroup
}

// newRowWorkerPool allocates a pool sized for `threads` workers and
// `mbRows` MB-rows of progress storage. Returns nil if threads < 2 —
// the caller (NewVP8Encoder) treats that as the zero-cost serial path
// and never instantiates a pool.
func newRowWorkerPool(threads int, mbRows int, mbCols int) *rowWorkerPool {
	if threads < 2 || mbRows <= 0 || mbCols <= 0 {
		return nil
	}
	pool := &rowWorkerPool{
		workers:     make([]rowEncoderState, threads),
		rowProgress: make([]atomic.Int64, mbRows),
		syncRange:   8,
		shutdown:    make(chan struct{}),
	}
	return pool
}

// reset re-initializes the per-row progress counters for a fresh
// frame. Called by the encoder at the start of every parallel
// inter-frame attempt.
func (p *rowWorkerPool) reset(mbRows int) {
	if p == nil {
		return
	}
	if cap(p.rowProgress) < mbRows {
		p.rowProgress = make([]atomic.Int64, mbRows)
	} else {
		p.rowProgress = p.rowProgress[:mbRows]
	}
	for i := range p.rowProgress {
		p.rowProgress[i].Store(-1)
	}
}

// publishRowColumn updates the wave-front counter for row r so that
// row r+1's worker can advance. Mirrors libvpx's
// vpx_atomic_store_release(current_mb_col, mb_col - 1) at every
// nsync columns.
func (p *rowWorkerPool) publishRowColumn(r int, col int) {
	if p == nil || r < 0 || r >= len(p.rowProgress) {
		return
	}
	p.rowProgress[r].Store(int64(col))
}

// waitForAboveColumn spin-waits until row r-1 has published a column
// index of at least `col`. Mirrors vp8_atomic_spin_wait in
// vp8/encoder/ethreading.c. Returns immediately when r == 0.
func (p *rowWorkerPool) waitForAboveColumn(r int, col int) {
	if p == nil || r <= 0 || r >= len(p.rowProgress) {
		return
	}
	target := int64(col)
	above := &p.rowProgress[r-1]
	for {
		if above.Load() >= target {
			return
		}
		runtime.Gosched()
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
	}
	p.wg.Wait()
}
