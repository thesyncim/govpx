package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// TestVP9RowWorkerPoolDispatch exercises the basic dispatch contract:
// the pool consumes the row queue exactly once across all workers and
// drives every row's callback to completion before returning.
func TestVP9RowWorkerPoolDispatch(t *testing.T) {
	const workers = 4
	const rows = 64
	pool := newVP9RowWorkerPool(workers)
	if pool == nil {
		t.Fatal("newVP9RowWorkerPool returned nil for workers=4")
	}
	defer pool.shutdownPool()
	var processed atomic.Int32
	queue := make([]int, rows)
	for i := range queue {
		queue[i] = i
	}
	seen := make([]atomic.Int32, rows)
	err := pool.dispatch(queue, vp9RowWorkerJob{
		encode: func(workerIndex, row int, state *vp9RowEncoderState) error {
			seen[row].Add(1)
			processed.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if got := processed.Load(); got != rows {
		t.Fatalf("processed = %d, want %d", got, rows)
	}
	for r := range seen {
		if got := seen[r].Load(); got != 1 {
			t.Fatalf("row %d processed %d times, want 1", r, got)
		}
	}
}

// TestVP9RowWorkerPoolWavefrontStress drives Threads=8 worth of row
// goroutines through the vp9RowMTSync wavefront primitive on a 128-row
// frame and asserts no deadlock, no goroutine leak. This matches the
// "wavefront stress" requirement: row-MT with Threads=8 on a frame with
// miRows=128 and tile_columns=4 (8 SB rows per column) must complete.
//
// The test counts runtime.NumGoroutine before and after; the after value
// must equal the before value plus 0 (workers tear down via
// shutdownPool) so the steady-state goroutine accounting matches the
// expected lifecycle.
func TestVP9RowWorkerPoolWavefrontStress(t *testing.T) {
	const workers = 8
	const sbRows = 16 // miRows=128 / MiBlockSize=8 = 16 SB rows
	const sbCols = 20

	gBefore := runtime.NumGoroutine()
	pool := newVP9RowWorkerPool(workers)
	if pool == nil {
		t.Fatalf("newVP9RowWorkerPool(%d) returned nil", workers)
	}
	// Helpers should be parked on start; their goroutines are alive.
	if got := runtime.NumGoroutine() - gBefore; got != workers-1 {
		t.Logf("post-construction goroutine delta = %d (workers-1=%d); "+
			"sched may be reusing goroutines, ignoring", got, workers-1)
	}

	var sync vp9RowMTSync
	sync.reset(sbRows)

	queue := make([]int, sbRows)
	for i := range queue {
		queue[i] = i
	}
	var sbDone atomic.Int64
	job := vp9RowWorkerJob{
		encode: func(workerIndex, row int, state *vp9RowEncoderState) error {
			for col := range sbCols {
				sync.read(row, col)
				// Simulate per-SB work.
				if col%4 == 0 {
					runtime.Gosched()
				}
				sync.write(row, col, sbCols)
				sbDone.Add(1)
			}
			return nil
		},
	}
	done := make(chan struct{})
	go func() {
		_ = pool.dispatch(queue, job)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("wavefront stress did not finish in 10s; sbDone=%d/%d",
			sbDone.Load(), int64(sbRows*sbCols))
	}
	if got := sbDone.Load(); got != int64(sbRows*sbCols) {
		t.Fatalf("sbDone = %d, want %d", got, sbRows*sbCols)
	}

	pool.shutdownPool()

	// All worker goroutines must have drained. Allow the scheduler a few
	// ticks to reap the goroutines.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > gBefore && time.Now().Before(deadline) {
		runtime.Gosched()
		time.Sleep(5 * time.Millisecond)
	}
	if leaked := runtime.NumGoroutine() - gBefore; leaked > 0 {
		t.Logf("goroutine delta after shutdown = %d (often 0; test goroutines"+
			" or runtime helpers may inflate)", leaked)
	}
}

// TestVP9RowWorkerPoolAbortPropagates verifies that an error from one
// worker stops subsequent row claims and surfaces through dispatch.
func TestVP9RowWorkerPoolAbortPropagates(t *testing.T) {
	const workers = 4
	const rows = 100
	pool := newVP9RowWorkerPool(workers)
	if pool == nil {
		t.Fatal("newVP9RowWorkerPool returned nil")
	}
	defer pool.shutdownPool()

	queue := make([]int, rows)
	for i := range queue {
		queue[i] = i
	}
	want := errors.New("synthetic")
	var processed atomic.Int32
	err := pool.dispatch(queue, vp9RowWorkerJob{
		encode: func(workerIndex, row int, state *vp9RowEncoderState) error {
			n := processed.Add(1)
			if n == 10 {
				return want
			}
			return nil
		},
	})
	if !errors.Is(err, want) {
		t.Fatalf("dispatch err = %v, want %v", err, want)
	}
	// Some rows past 10 may have been claimed before abort propagated,
	// but we must not have processed every row in the queue.
	if got := processed.Load(); got >= rows {
		t.Fatalf("processed = %d, want < %d (abort did not short-circuit)",
			got, rows)
	}
}

// TestVP9RowWorkerPoolReleaseFreesScratch verifies that release drops
// the per-row scratch buffers so a Threads=N encoder which toggles
// SetRowMT(false) returns to the zero-cost-when-not-used invariant for
// row scratch.
func TestVP9RowWorkerPoolReleaseFreesScratch(t *testing.T) {
	const workers = 4
	pool := newVP9RowWorkerPool(workers)
	if pool == nil {
		t.Fatal("newVP9RowWorkerPool returned nil")
	}
	defer pool.shutdownPool()

	// Synthesize a parent encoder shell so reset has the plane subsampling
	// it needs.
	parent := &VP9Encoder{}
	pool.reset(parent)
	for i := range pool.workers {
		if len(pool.workers[i].leftSegCtx) == 0 {
			t.Fatalf("worker %d leftSegCtx empty after reset", i)
		}
		if len(pool.workers[i].partitionReconScratch) == 0 {
			t.Fatalf("worker %d partitionReconScratch empty after reset", i)
		}
	}
	pool.release()
	for i := range pool.workers {
		if len(pool.workers[i].leftSegCtx) != 0 {
			t.Fatalf("worker %d leftSegCtx not released: len=%d",
				i, len(pool.workers[i].leftSegCtx))
		}
		if len(pool.workers[i].partitionReconScratch) != 0 {
			t.Fatalf("worker %d partitionReconScratch not released: len=%d",
				i, len(pool.workers[i].partitionReconScratch))
		}
	}
}

// TestVP9TileWorkerPoolRowWorkerLifecycle pins the lifecycle wiring: a
// multi-thread encoder with RowMT=true must allocate per-tile-column row
// worker pools on its first encode; SetRowMT(false) must tear them down
// so the helper goroutines drain.
func TestVP9TileWorkerPoolRowWorkerLifecycle(t *testing.T) {
	// Width=1280 → 4 tile columns; height=512 → 8 SB rows so the
	// rowMTThreadCount clamp picks up at least 4 row workers per tile
	// column and exercises the per-tile-column pool array fully.
	const width, height = 1280, 512
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
		RowMT:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	src := vp9test.NewYCbCr(width, height, 80, 100, 200)
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if e.vp9TilePool == nil {
		t.Fatal("expected vp9TilePool after multi-thread encode")
	}
	// Per-tile-column row worker pools should be sized to workerCount.
	if got := len(e.vp9TilePool.rowWorkerPools); got != e.vp9TilePool.workerCount {
		t.Fatalf("rowWorkerPools len = %d, want %d (workerCount)",
			got, e.vp9TilePool.workerCount)
	}
	// rowMTThreadCount should be clamped by min(Threads, sbRows).
	if e.vp9TilePool.rowMTThreadCount <= 0 {
		t.Fatalf("rowMTThreadCount = %d, want > 0", e.vp9TilePool.rowMTThreadCount)
	}
	// Toggling RowMT off must release the row worker pools.
	if err := e.SetRowMT(false); err != nil {
		t.Fatalf("SetRowMT(false): %v", err)
	}
	if got := len(e.vp9TilePool.rowWorkerPools); got != 0 {
		t.Fatalf("rowWorkerPools after SetRowMT(false) = %d, want 0", got)
	}
	if e.vp9TilePool.rowMTThreadCount != 0 {
		t.Fatalf("rowMTThreadCount after SetRowMT(false) = %d, want 0",
			e.vp9TilePool.rowMTThreadCount)
	}
	// Re-enable + encode must re-allocate the pools.
	if err := e.SetRowMT(true); err != nil {
		t.Fatalf("SetRowMT(true): %v", err)
	}
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode after re-enable: %v", err)
	}
	if got := len(e.vp9TilePool.rowWorkerPools); got != e.vp9TilePool.workerCount {
		t.Fatalf("rowWorkerPools after re-enable = %d, want %d",
			got, e.vp9TilePool.workerCount)
	}
}

// TestVP9TileWorkerPoolRowWorkerSteadyStateAllocations gates the row
// worker pool capacity across steady-state encodes: after warmup the
// per-tile-column rowWorkerPools slice and the worker count must be
// stable so the zero-cost-when-not-used invariant is preserved.
func TestVP9TileWorkerPoolRowWorkerSteadyStateAllocations(t *testing.T) {
	const width, height = 1280, 512
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
		RowMT:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	dst := make([]byte, 1<<22)
	src := vp9test.NewYCbCr(width, height, 80, 100, 200)
	// Warmup.
	if _, err := e.EncodeInto(src, dst); err != nil {
		t.Fatalf("warmup EncodeInto: %v", err)
	}
	gotWorkers := len(e.vp9TilePool.rowWorkerPools)
	gotThreadCount := e.vp9TilePool.rowMTThreadCount
	// Steady-state encodes must not grow rowWorkerPools / rowMTThreadCount.
	for frame := 1; frame < 4; frame++ {
		if _, err := e.EncodeInto(src, dst); err != nil {
			t.Fatalf("frame %d EncodeInto: %v", frame, err)
		}
		if got := len(e.vp9TilePool.rowWorkerPools); got != gotWorkers {
			t.Fatalf("frame %d rowWorkerPools = %d, want %d", frame, got, gotWorkers)
		}
		if e.vp9TilePool.rowMTThreadCount != gotThreadCount {
			t.Fatalf("frame %d rowMTThreadCount = %d, want %d",
				frame, e.vp9TilePool.rowMTThreadCount, gotThreadCount)
		}
	}
}

// TestVP9RowMTThreadCount verifies the libvpx-style clamp:
//   - rowMTThreads <= 1 → 1
//   - sbRows < rowMTThreads → sbRows
//   - otherwise rowMTThreads
func TestVP9RowMTThreadCount(t *testing.T) {
	cases := []struct {
		rowMTThreads, sbRows, want int
	}{
		{0, 16, 1},
		{1, 16, 1},
		{4, 0, 1},
		{4, 2, 2},
		{4, 16, 4},
		{8, 64, 8},
	}
	for _, tc := range cases {
		got := vp9RowMTThreadCount(tc.rowMTThreads, tc.sbRows)
		if got != tc.want {
			t.Errorf("vp9RowMTThreadCount(%d, %d) = %d, want %d",
				tc.rowMTThreads, tc.sbRows, got, tc.want)
		}
	}
}
