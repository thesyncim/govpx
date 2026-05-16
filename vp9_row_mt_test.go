package govpx

import (
	"bytes"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestVP9RowMTValidation pins the constructor-time gating on the RowMT option.
// Enabling RowMT without a multi-thread hint is meaningless because the
// wavefront primitive only fires inside the persistent tile worker pool, which
// is itself gated on Threads > 1.
func TestVP9RowMTValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opts    VP9EncoderOptions
		wantErr error
	}{
		{
			name:    "row_mt_with_zero_threads_rejected",
			opts:    VP9EncoderOptions{Width: 64, Height: 64, RowMT: true},
			wantErr: ErrInvalidConfig,
		},
		{
			name:    "row_mt_with_one_thread_rejected",
			opts:    VP9EncoderOptions{Width: 64, Height: 64, Threads: 1, RowMT: true},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "row_mt_accepted_with_threads_gt_one",
			opts: VP9EncoderOptions{Width: 1280, Height: 64, Threads: 4, RowMT: true},
		},
		{
			name: "row_mt_off_with_any_threads_accepted",
			opts: VP9EncoderOptions{Width: 64, Height: 64, Threads: 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(tc.opts)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("NewVP9Encoder err = %v, want %v", err, tc.wantErr)
			}
			if e != nil {
				e.Close()
			}
		})
	}
}

// TestVP9EncoderSetRowMTRuntimeGating exercises the runtime setter mirroring
// libvpx's VP9E_SET_ROW_MT. Enabling without Threads > 1 returns
// ErrInvalidConfig; toggling off releases any latched sync primitive state.
func TestVP9EncoderSetRowMTRuntimeGating(t *testing.T) {
	t.Run("rejects_single_thread", func(t *testing.T) {
		e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		if err := e.SetRowMT(true); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("SetRowMT(true) on single-thread encoder err = %v, want ErrInvalidConfig", err)
		}
		if e.opts.RowMT {
			t.Fatal("rejected SetRowMT(true) left the flag on")
		}
	})
	t.Run("accepts_multi_thread_and_releases_on_off", func(t *testing.T) {
		const width, height = 1280, 64
		e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, Threads: 4})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		if err := e.SetRowMT(true); err != nil {
			t.Fatalf("SetRowMT(true): %v", err)
		}
		if !e.opts.RowMT {
			t.Fatal("SetRowMT(true) did not flip the flag")
		}
		src := newVP9YCbCrForTest(width, height, 82, 123, 211)
		if _, err := e.Encode(src); err != nil {
			t.Fatalf("Encode after enabling row-MT: %v", err)
		}
		if e.vp9TilePool == nil {
			t.Fatal("expected tile worker pool after multi-thread encode")
		}
		if len(e.vp9TilePool.rowMTSyncs) == 0 {
			t.Fatal("row-MT enabled encode did not allocate rowMTSyncs")
		}
		// Toggling off must release the sync arrays so memory does not grow.
		if err := e.SetRowMT(false); err != nil {
			t.Fatalf("SetRowMT(false): %v", err)
		}
		for i, s := range e.vp9TilePool.rowMTSyncs {
			if s.rows != 0 {
				t.Fatalf("rowMTSyncs[%d].rows = %d after SetRowMT(false), want 0",
					i, s.rows)
			}
		}
		if err := e.SetRowMT(true); err != nil {
			t.Fatalf("SetRowMT(true) re-enable: %v", err)
		}
	})
	t.Run("closed_encoder_rejected", func(t *testing.T) {
		e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, Threads: 4})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		e.Close()
		if err := e.SetRowMT(true); !errors.Is(err, ErrClosed) {
			t.Fatalf("SetRowMT on closed encoder err = %v, want ErrClosed", err)
		}
	})
}

// TestVP9RowMTBytewiseIdenticalToSerial confirms that arming the wavefront
// primitive does not perturb bitstream output. Each tile column still encodes
// on a single goroutine, so the Read/Write hooks must collapse to no-ops and
// produce byte-identical packets vs. a serial encode on a 64x64 frame.
func TestVP9RowMTBytewiseIdenticalToSerial(t *testing.T) {
	const width, height = 1280, 64
	serial, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(serial): %v", err)
	}
	rowMT, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
		RowMT:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(rowMT): %v", err)
	}
	dstSerial := make([]byte, 1<<20)
	dstRowMT := make([]byte, 1<<20)
	for frame := range 4 {
		src := newVP9PanningYCbCrForRateTest(width, height, frame)
		nSerial, err := serial.EncodeInto(src, dstSerial)
		if err != nil {
			t.Fatalf("serial EncodeInto[%d]: %v", frame, err)
		}
		nRowMT, err := rowMT.EncodeInto(src, dstRowMT)
		if err != nil {
			t.Fatalf("rowMT EncodeInto[%d]: %v", frame, err)
		}
		if !bytes.Equal(dstSerial[:nSerial], dstRowMT[:nRowMT]) {
			t.Fatalf("row-MT packet %d differs from serial: %d/%d bytes",
				frame, nRowMT, nSerial)
		}
	}
	if rowMT.vp9TilePool == nil {
		t.Fatal("row-MT encode did not initialize tile worker pool")
	}
	if len(rowMT.vp9TilePool.rowMTSyncs) != rowMT.vp9TilePool.workerCount {
		t.Fatalf("rowMTSyncs len = %d, want %d",
			len(rowMT.vp9TilePool.rowMTSyncs), rowMT.vp9TilePool.workerCount)
	}
	if serial.vp9TilePool != nil && len(serial.vp9TilePool.rowMTSyncs) != 0 {
		t.Fatalf("serial encoder allocated %d rowMTSyncs", len(serial.vp9TilePool.rowMTSyncs))
	}
	for i, s := range rowMT.vp9TilePool.rowMTSyncs {
		if s.rows == 0 {
			t.Fatalf("rowMTSyncs[%d] not initialized", i)
		}
		if s.syncRange != vp9RowMTSyncDefaultRange {
			t.Fatalf("rowMTSyncs[%d].syncRange = %d, want %d",
				i, s.syncRange, vp9RowMTSyncDefaultRange)
		}
	}
}

// TestVP9RowMTSteadyStateAllocations gates row-MT for steady-state allocations:
// after one warm encode the rowMTSync arrays are sized for the frame and
// subsequent encodes must reuse them instead of growing capacity.
func TestVP9RowMTSteadyStateAllocations(t *testing.T) {
	const width, height = 1280, 64
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
	dst := make([]byte, 1<<20)
	// Warm-up encode to size all sync buffers.
	src0 := newVP9PanningYCbCrForRateTest(width, height, 0)
	if _, err := e.EncodeInto(src0, dst); err != nil {
		t.Fatalf("warm-up EncodeInto: %v", err)
	}
	if e.vp9TilePool == nil || len(e.vp9TilePool.rowMTSyncs) == 0 {
		t.Fatal("expected row-MT sync arrays after warm-up")
	}
	type snapshot struct {
		muCap     int
		condCap   int
		curColCap int
		rows      int
	}
	before := make([]snapshot, len(e.vp9TilePool.rowMTSyncs))
	for i, s := range e.vp9TilePool.rowMTSyncs {
		before[i] = snapshot{cap(s.mu), cap(s.cond), cap(s.curCol), s.rows}
	}
	// Steady-state encodes must not grow any per-tile sync capacity.
	for frame := 1; frame < 6; frame++ {
		src := newVP9PanningYCbCrForRateTest(width, height, frame)
		if _, err := e.EncodeInto(src, dst); err != nil {
			t.Fatalf("steady-state EncodeInto[%d]: %v", frame, err)
		}
		for i, s := range e.vp9TilePool.rowMTSyncs {
			if cap(s.mu) != before[i].muCap ||
				cap(s.cond) != before[i].condCap ||
				cap(s.curCol) != before[i].curColCap ||
				s.rows != before[i].rows {
				t.Fatalf("frame %d rowMTSyncs[%d] capacity drifted: "+
					"mu %d→%d, cond %d→%d, curCol %d→%d, rows %d→%d",
					frame, i,
					before[i].muCap, cap(s.mu),
					before[i].condCap, cap(s.cond),
					before[i].curColCap, cap(s.curCol),
					before[i].rows, s.rows)
			}
		}
	}
}

// TestVP9RowMTSyncWaitWavefrontProgress exercises the wavefront primitive
// directly with two goroutines. It verifies that read(r, c) blocks until the
// previous row has produced the corresponding SB column and that the broadcast
// path matches libvpx's vp9_row_mt_sync_read / vp9_row_mt_sync_write contract.
func TestVP9RowMTSyncWaitWavefrontProgress(t *testing.T) {
	const rows, cols = 4, 8
	var s vp9RowMTSync
	s.reset(rows)
	if s.syncRange != vp9RowMTSyncDefaultRange {
		t.Fatalf("reset syncRange = %d, want %d", s.syncRange, vp9RowMTSyncDefaultRange)
	}
	for r := 0; r < rows; r++ {
		if s.curCol[r] != -1 {
			t.Fatalf("reset curCol[%d] = %d, want -1", r, s.curCol[r])
		}
	}
	var wg sync.WaitGroup
	var completed atomic.Int32
	wg.Add(rows)
	for r := 0; r < rows; r++ {
		r := r
		go func() {
			defer wg.Done()
			for c := 0; c < cols; c++ {
				s.read(r, c)
				runtime.Gosched()
				s.write(r, c, cols)
				completed.Add(1)
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("wavefront workers did not finish within 5s; completed=%d of %d",
			completed.Load(), rows*cols)
	}
	if got := completed.Load(); got != int32(rows*cols) {
		t.Fatalf("completed = %d, want %d", got, rows*cols)
	}
	// release drops the per-row arrays.
	s.release()
	if s.rows != 0 || len(s.mu) != 0 || len(s.cond) != 0 || len(s.curCol) != 0 {
		t.Fatalf("release left state: rows=%d mu=%d cond=%d curCol=%d",
			s.rows, len(s.mu), len(s.cond), len(s.curCol))
	}
}
