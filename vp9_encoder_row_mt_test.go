package govpx

import (
	"bytes"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// TestVP9RowMTValidation pins the constructor-time gating on the RowMT option.
// Enabling RowMT without an effective multi-thread hint is meaningless because
// the wavefront primitive only fires inside the persistent tile worker pool,
// which is itself gated on the effective VP9 thread hint.
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

func TestVP9RowMTAcceptsRealtimeAutoThreads(t *testing.T) {
	const width, height = 640, 360
	opts := VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  700,
		RowMT:              true,
	}
	wantThreads := vp9RealtimeAutoThreadHint(opts, runtime.NumCPU())
	if wantThreads <= 1 {
		t.Skip("runtime exposes only one usable VP9 realtime tile thread")
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder realtime auto RowMT: %v", err)
	}
	defer e.Close()
	if e.opts.Threads != 0 {
		t.Fatalf("stored Threads = %d, want caller auto value 0", e.opts.Threads)
	}
	if !e.opts.RowMT {
		t.Fatal("constructor dropped RowMT flag")
	}
	if _, err := e.Encode(vp9test.NewPanningYCbCr(width, height, 0)); err != nil {
		t.Fatalf("Encode realtime auto RowMT: %v", err)
	}
	if e.vp9TilePool == nil {
		t.Fatal("realtime auto RowMT encode did not initialize tile worker pool")
	}
	if got := e.vp9TilePool.workerCount; got != wantThreads {
		t.Fatalf("realtime auto RowMT worker count = %d, want %d", got, wantThreads)
	}
	if got := len(e.vp9TilePool.rowMTSyncs); got != e.vp9TilePool.workerCount {
		t.Fatalf("realtime auto RowMT syncs = %d, want %d",
			got, e.vp9TilePool.workerCount)
	}
	sbRows := ((height + 7) >> 3) + common.MiBlockSize - 1
	sbRows >>= common.MiBlockSizeLog2
	wantRowThreads := vp9RowMTThreadsPerTile(wantThreads,
		e.vp9TilePool.workerCount, sbRows)
	wantPools := 0
	if wantRowThreads > 1 {
		wantPools = e.vp9TilePool.workerCount
	}
	if got := len(e.vp9TilePool.rowWorkerPools); got != wantPools {
		t.Fatalf("realtime auto RowMT row worker pools = %d, want %d",
			got, wantPools)
	}
}

func TestVP9RowMTAutoThreadsEnableSpeedFeature(t *testing.T) {
	const width, height = 640, 360
	opts := VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		CpuUsed:            8,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  700,
		RowMT:              true,
	}
	if wantThreads := vp9RealtimeAutoThreadHint(opts, runtime.NumCPU()); wantThreads <= 1 {
		t.Skip("runtime exposes only one usable VP9 realtime tile thread")
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder realtime auto RowMT: %v", err)
	}
	defer e.Close()
	if e.opts.Threads != 0 {
		t.Fatalf("stored Threads = %d, want caller auto value 0", e.opts.Threads)
	}

	for _, tc := range []struct {
		name string
		ctx  func(vp9SpeedFrameContext) vp9SpeedFrameContext
	}{
		{
			name: "single-layer-speed8",
			ctx:  func(ctx vp9SpeedFrameContext) vp9SpeedFrameContext { return ctx },
		},
		{
			name: "svc-speed7",
			ctx: func(ctx vp9SpeedFrameContext) vp9SpeedFrameContext {
				ctx.svc.UseSvc = true
				ctx.svc.NumberSpatialLayers = 3
				ctx.svc.NumberTemporalLayers = 3
				return ctx
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.ctx(e.vp9DefaultSpeedFrameContext())
			ctx.frameType = common.InterFrame
			ctx.intraOnly = false
			var sf SpeedFeatures
			vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 8, ctx)
			vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 8, ctx)
			if sf.AdaptiveRdThreshRowMt != 1 {
				t.Fatalf("AdaptiveRdThreshRowMt = %d, want 1 for realtime auto RowMT",
					sf.AdaptiveRdThreshRowMt)
			}
		})
	}
}

// TestVP9EncoderSetRowMTRuntimeGating exercises the runtime setter mirroring
// libvpx's VP9E_SET_ROW_MT. Enabling without an effective multi-thread hint
// returns ErrInvalidConfig; toggling off releases any latched sync primitive
// state.
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
		src := vp9test.NewYCbCr(width, height, 82, 123, 211)
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
	t.Run("accepts_realtime_auto_threads", func(t *testing.T) {
		const width, height = 640, 360
		opts := VP9EncoderOptions{
			Width:              width,
			Height:             height,
			Deadline:           DeadlineRealtime,
			RateControlModeSet: true,
			RateControlMode:    RateControlCBR,
			TargetBitrateKbps:  700,
		}
		wantThreads := vp9RealtimeAutoThreadHint(opts, runtime.NumCPU())
		if wantThreads <= 1 {
			t.Skip("runtime exposes only one usable VP9 realtime tile thread")
		}
		e, err := NewVP9Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		if err := e.SetRowMT(true); err != nil {
			t.Fatalf("SetRowMT(true) realtime auto: %v", err)
		}
		if _, err := e.Encode(vp9test.NewPanningYCbCr(width, height, 0)); err != nil {
			t.Fatalf("Encode realtime auto RowMT: %v", err)
		}
		if e.vp9TilePool == nil {
			t.Fatal("realtime auto RowMT encode did not initialize tile worker pool")
		}
		if got := e.vp9TilePool.workerCount; got != wantThreads {
			t.Fatalf("realtime auto RowMT worker count = %d, want %d", got, wantThreads)
		}
		if got := len(e.vp9TilePool.rowMTSyncs); got != e.vp9TilePool.workerCount {
			t.Fatalf("realtime auto RowMT syncs = %d, want %d",
				got, e.vp9TilePool.workerCount)
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

func TestVP9RowMTAutoThreadsSurviveRuntimeEligibilityChanges(t *testing.T) {
	const width, height = 640, 360
	opts := VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Deadline:           DeadlineRealtime,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  700,
		RowMT:              true,
	}
	wantThreads := vp9RealtimeAutoThreadHint(opts, runtime.NumCPU())
	if wantThreads <= 1 {
		t.Skip("runtime exposes only one usable VP9 realtime tile thread")
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder realtime auto RowMT: %v", err)
	}
	defer e.Close()
	if _, err := e.Encode(vp9test.NewPanningYCbCr(width, height, 0)); err != nil {
		t.Fatalf("initial Encode: %v", err)
	}
	if e.vp9TilePool == nil || len(e.vp9TilePool.rowMTSyncs) == 0 {
		t.Fatal("initial realtime auto RowMT encode did not arm row-MT")
	}

	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlVBR,
		TargetBitrateKbps: 700,
	}); err != nil {
		t.Fatalf("SetRateControl(VBR) with dormant RowMT: %v", err)
	}
	if e.vp9EffectiveThreadHint() != 1 {
		t.Fatalf("VBR auto effective threads = %d, want 1", e.vp9EffectiveThreadHint())
	}
	if !e.opts.RowMT {
		t.Fatal("SetRateControl(VBR) cleared RowMT instead of leaving it dormant")
	}
	if e.vp9TilePool != nil {
		t.Fatalf("SetRateControl(VBR) left tile pool with %d workers", e.vp9TilePool.workerCount)
	}
	if err := e.SetCQLevel(24); err != nil {
		t.Fatalf("SetCQLevel with dormant RowMT: %v", err)
	}

	if err := e.SetRateControl(RateControlConfig{
		Mode:              RateControlCBR,
		TargetBitrateKbps: 700,
	}); err != nil {
		t.Fatalf("SetRateControl(CBR) rearming auto RowMT: %v", err)
	}
	if _, err := e.Encode(vp9test.NewPanningYCbCr(width, height, 1)); err != nil {
		t.Fatalf("rearmed Encode: %v", err)
	}
	if e.vp9TilePool == nil {
		t.Fatal("rearmed realtime auto RowMT encode did not initialize tile pool")
	}
	if got := e.vp9TilePool.workerCount; got != wantThreads {
		t.Fatalf("rearmed realtime auto RowMT worker count = %d, want %d",
			got, wantThreads)
	}
	if got := len(e.vp9TilePool.rowMTSyncs); got != e.vp9TilePool.workerCount {
		t.Fatalf("rearmed realtime auto RowMT syncs = %d, want %d",
			got, e.vp9TilePool.workerCount)
	}
}

// TestVP9RowMTDisabledDoesNotAllocateSyncState verifies that a threaded
// VP9 encoder using normal tile threading does not retain Row-MT wavefront
// state unless the RowMT control is explicitly enabled.
func TestVP9RowMTDisabledDoesNotAllocateSyncState(t *testing.T) {
	const width, height = 1280, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		Threads: 4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	src := vp9test.NewPanningYCbCr(width, height, 0)
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if e.vp9RowMTSync != nil {
		t.Fatal("encoder retained active row-MT sync with RowMT disabled")
	}
	if e.vp9TilePool == nil {
		t.Fatal("threaded encode did not initialize tile worker pool")
	}
	if got := len(e.vp9TilePool.rowMTSyncs); got != 0 {
		t.Fatalf("rowMTSyncs len = %d, want 0 with RowMT disabled", got)
	}
	if got := len(e.vp9TilePool.rowWorkerPools); got != 0 {
		t.Fatalf("rowWorkerPools len = %d, want 0 with RowMT disabled", got)
	}

	if err := e.SetRowMT(false); err != nil {
		t.Fatalf("SetRowMT(false): %v", err)
	}
	if _, err := e.Encode(vp9test.NewPanningYCbCr(width, height, 1)); err != nil {
		t.Fatalf("Encode after SetRowMT(false): %v", err)
	}
	if got := len(e.vp9TilePool.rowMTSyncs); got != 0 {
		t.Fatalf("rowMTSyncs len after SetRowMT(false) = %d, want 0", got)
	}
	if got := len(e.vp9TilePool.rowWorkerPools); got != 0 {
		t.Fatalf("rowWorkerPools len after SetRowMT(false) = %d, want 0", got)
	}
}

func TestVP9RowMTAdaptiveRDThreshRowsAllocated(t *testing.T) {
	const width, height = 1280, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		Threads:            4,
		Deadline:           DeadlineRealtime,
		CpuUsed:            8,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  700,
		RowMT:              true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	if _, err := e.Encode(vp9test.NewPanningYCbCr(width, height, 0)); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if e.sf.AdaptiveRdThreshRowMt != 1 {
		t.Fatalf("AdaptiveRdThreshRowMt = %d, want 1", e.sf.AdaptiveRdThreshRowMt)
	}
	wantRows := vp9RDThreshSBRows((height + 7) >> 3)
	if got := e.rdThresh.RowMTFreqFactRows(); got != wantRows {
		t.Fatalf("row-MT RD-thresh rows = %d, want %d", got, wantRows)
	}

	if err := e.SetRowMT(false); err != nil {
		t.Fatalf("SetRowMT(false): %v", err)
	}
	if got := e.rdThresh.RowMTFreqFactRows(); got != 0 {
		t.Fatalf("row-MT RD-thresh rows after disable = %d, want 0", got)
	}
}

// TestVP9RowMTBytewiseIdenticalToSerial confirms that enabling RowMT does not
// perturb bitstream output when the frame has one SB row and production row
// dispatch therefore collapses to the serial tile path.
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
		src := vp9test.NewPanningYCbCr(width, height, frame)
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

func TestVP9RowMTProductionRowsDeterministicAcrossThreads(t *testing.T) {
	const width, height = 640, 256
	encode := func(threads int) [][]byte {
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:               width,
			Height:              height,
			Threads:             threads,
			RowMT:               true,
			Deadline:            DeadlineRealtime,
			CpuUsed:             8,
			RateControlModeSet:  true,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   900,
			NoiseSensitivity:    0,
			MaxKeyframeInterval: 3000,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder threads=%d: %v", threads, err)
		}
		defer e.Close()
		dst := make([]byte, 1<<20)
		packets := make([][]byte, 8)
		for frame := range packets {
			src := vp9test.NewPanningYCbCr(width, height, frame)
			n, err := e.EncodeInto(src, dst)
			if err != nil {
				t.Fatalf("threads=%d frame=%d: %v", threads, frame, err)
			}
			packets[frame] = append([]byte(nil), dst[:n]...)
		}
		if threads == 8 && (e.vp9TilePool == nil ||
			e.vp9TilePool.rowMTThreadCount <= 1) {
			t.Fatal("threads=8 did not execute multi-worker row-MT")
		}
		return packets
	}

	baseline := encode(2)
	for _, threads := range []int{4, 8} {
		packets := encode(threads)
		for frame := range baseline {
			if !bytes.Equal(baseline[frame], packets[frame]) {
				t.Fatalf("row-MT packet %d differs at threads=%d: %d/%d bytes",
					frame, threads, len(packets[frame]), len(baseline[frame]))
			}
		}
	}
}

func TestVP9RowMTProductionSteadyStateAllocations(t *testing.T) {
	const width, height = 640, 256
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		Threads:             8,
		RowMT:               true,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   900,
		NoiseSensitivity:    0,
		MaxKeyframeInterval: 3000,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	dst := make([]byte, 1<<20)
	src := vp9test.NewPanningYCbCr(width, height, 1)
	for range 4 {
		if _, err := e.EncodeInto(src, dst); err != nil {
			t.Fatalf("warmup EncodeInto: %v", err)
		}
	}
	allocs := testing.AllocsPerRun(5, func() {
		if _, err := e.EncodeInto(src, dst); err != nil {
			t.Fatalf("EncodeInto: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("row-MT steady-state allocs = %v, want 0", allocs)
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
	src0 := vp9test.NewPanningYCbCr(width, height, 0)
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
		src := vp9test.NewPanningYCbCr(width, height, frame)
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
	for r := range rows {
		if s.curCol[r] != -1 {
			t.Fatalf("reset curCol[%d] = %d, want -1", r, s.curCol[r])
		}
	}
	var wg sync.WaitGroup
	var completed atomic.Int32
	wg.Add(rows)
	for r := range rows {
		go func() {
			defer wg.Done()
			for c := range cols {
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
