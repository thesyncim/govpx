package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"runtime"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

const vp9SteadyStateAllocRuns = 25

func TestVP9DecoderEffectivePostProcessFlagsMatchLibvpxVP9Default(t *testing.T) {
	if got, want := (VP9DecoderOptions{PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock}).effectivePostProcessFlags(),
		PostProcessDeblock|PostProcessDemacroblock; got != want {
		t.Fatalf("VP9 default postprocess flags = 0x%x, want 0x%x", got, want)
	}
	if got, want := (VP9DecoderOptions{
		PostProcessFlags:      PostProcessDeblock | PostProcessDemacroblock | PostProcessAddNoise,
		PostProcessNoiseLevel: 4,
	}).effectivePostProcessFlags(),
		PostProcessDeblock|PostProcessDemacroblock|PostProcessAddNoise; got != want {
		t.Fatalf("VP9 noise postprocess flags = 0x%x, want 0x%x", got, want)
	}
	if got, want := (VP9DecoderOptions{
		PostProcessFlags: PostProcessMFQE,
	}).effectivePostProcessFlags(), PostProcessMFQE; got != want {
		t.Fatalf("VP9 explicit postprocess flags = 0x%x, want 0x%x", got, want)
	}
}

func TestVP9DecoderPrepareIntraOnlyFrameContextResetSemantics(t *testing.T) {
	d, _ := NewVP9Decoder(VP9DecoderOptions{})
	d.frameContexts[0].SkipProbs[0] = 77
	hdr := vp9dec.UncompressedHeader{
		FrameType:         common.InterFrame,
		IntraOnly:         true,
		ResetFrameContext: 0,
		FrameContextIdx:   2,
	}
	if idx := d.prepareVP9FrameContext(&hdr); idx != 0 {
		t.Fatalf("prepareVP9FrameContext reset=0 idx = %d, want 0", idx)
	}
	if got := d.fc.SkipProbs[0]; got != 77 {
		t.Fatalf("prepareVP9FrameContext reset=0 SkipProbs[0] = %d, want preserved context 0", got)
	}

	d.frameContexts[0].SkipProbs[0] = 77
	hdr.ResetFrameContext = 2
	hdr.FrameContextIdx = 0
	if idx := d.prepareVP9FrameContext(&hdr); idx != 0 {
		t.Fatalf("prepareVP9FrameContext reset=2 idx = %d, want 0", idx)
	}
	var want vp9dec.FrameContext
	vp9dec.ResetFrameContext(&want)
	if d.fc != want || d.frameContexts[0] != want {
		t.Fatal("prepareVP9FrameContext reset=2 did not reset selected intra-only context")
	}
}

func TestVP9DecoderRuntimeThreadingControlsUpdateState(t *testing.T) {
	threaded, err := NewVP9Decoder(VP9DecoderOptions{Threads: 2})
	if err != nil {
		t.Fatalf("threaded NewVP9Decoder: %v", err)
	}
	defer threaded.Close()
	if err := threaded.SetRowMT(true); err != nil {
		t.Errorf("threaded SetRowMT(true) err = %v, want nil", err)
	}
	if !threaded.opts.DecoderRowMT {
		t.Errorf("threaded SetRowMT(true) did not record option")
	}
	if !threaded.vp9TilePool.rowMTArmed {
		t.Errorf("threaded SetRowMT(true) did not arm tile pool")
	}
	if err := threaded.SetLoopFilterOpt(true); err != nil {
		t.Errorf("threaded SetLoopFilterOpt(true) err = %v, want nil", err)
	}
	if !threaded.opts.DecoderLoopFilterOpt {
		t.Errorf("threaded SetLoopFilterOpt(true) did not record option")
	}
	if err := threaded.SetRowMT(false); err != nil {
		t.Errorf("threaded SetRowMT(false) err = %v, want nil", err)
	}
	if threaded.opts.DecoderRowMT {
		t.Errorf("threaded SetRowMT(false) did not clear option")
	}
	if threaded.vp9TilePool.rowMTArmed {
		t.Errorf("threaded SetRowMT(false) did not disarm tile pool")
	}
}

// TestVP9DecoderRowMTMatchesSerial proves enabling VP9D_SET_ROW_MT keeps
// the multi-tile-column decode output byte-identical to the serial path.
// The wavefront primitive is exercised inside each tile-column body but
// the body still runs single-goroutine, mirroring the encoder foundation.
func TestVP9DecoderRowMTMatchesSerial(t *testing.T) {
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, 1)

	serial := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
		VP9DecoderOptions{Threads: 4}, packet)
	rowMT := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
		VP9DecoderOptions{Threads: 4, DecoderRowMT: true}, packet)
	assertVP9ImagesEqual(t, serial, rowMT)
}

// TestVP9DecoderRowMTDisabledDoesNotRetainSyncState verifies that threaded
// VP9 decode uses normal tile workers without retaining Row-MT wavefront
// state unless VP9D_SET_ROW_MT is enabled.
func TestVP9DecoderRowMTDisabledDoesNotRetainSyncState(t *testing.T) {
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, 1)

	d, err := NewVP9Decoder(VP9DecoderOptions{Threads: 4})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()

	for frame := range 2 {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode[%d]: %v", frame, err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("NextFrame[%d] returned !ok", frame)
		}
	}
	if d.rowMTSync != nil {
		t.Fatal("decoder retained active row-MT sync with DecoderRowMT disabled")
	}
	if d.vp9TilePool == nil {
		t.Fatal("threaded decode did not initialize tile worker pool")
	}
	if got := len(d.vp9TilePool.rowMTSyncs); got != 0 {
		t.Fatalf("rowMTSyncs len = %d, want 0 with DecoderRowMT disabled", got)
	}

	if err := d.SetRowMT(true); err != nil {
		t.Fatalf("SetRowMT(true): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode after SetRowMT(true): %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame after SetRowMT(true) returned !ok")
	}
	if got := len(d.vp9TilePool.rowMTSyncs); got == 0 {
		t.Fatal("DecoderRowMT enabled decode did not allocate rowMTSyncs")
	}

	if err := d.SetRowMT(false); err != nil {
		t.Fatalf("SetRowMT(false): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode after SetRowMT(false): %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame after SetRowMT(false) returned !ok")
	}
	if d.rowMTSync != nil {
		t.Fatal("decoder retained active row-MT sync after SetRowMT(false)")
	}
	if got := len(d.vp9TilePool.rowMTSyncs); got != 0 {
		t.Fatalf("rowMTSyncs len after SetRowMT(false) = %d, want 0", got)
	}
}

// TestVP9DecoderRowMTRuntimeToggleMatchesSerial cycles SetRowMT mid-stream
// and confirms each decode still produces byte-identical output.
func TestVP9DecoderRowMTRuntimeToggleMatchesSerial(t *testing.T) {
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, 1)

	want := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
		VP9DecoderOptions{Threads: 4}, packet)

	d, err := NewVP9Decoder(VP9DecoderOptions{Threads: 4})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()

	for i, enabled := range []bool{true, false, true} {
		if err := d.SetRowMT(enabled); err != nil {
			t.Fatalf("iter %d: SetRowMT(%v): %v", i, enabled, err)
		}
		if err := d.Decode(packet); err != nil {
			t.Fatalf("iter %d: Decode: %v", i, err)
		}
		frame, ok := d.NextFrame()
		if !ok {
			t.Fatalf("iter %d: NextFrame returned !ok", i)
		}
		assertVP9ImagesEqual(t, want, frame)
	}
}

// TestVP9DecoderLoopFilterOptGatesLoopFilterPool covers the gate: with the
// option off the deblock pass uses the serial path even on a threaded
// decoder, and with the option on the threaded helper pool drives the
// U / V plane deblock.
func TestVP9DecoderLoopFilterOptGatesLoopFilterPool(t *testing.T) {
	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)

	serial := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
		VP9DecoderOptions{}, packet)

	d, err := NewVP9Decoder(VP9DecoderOptions{
		Threads: 3, DecoderLoopFilterOpt: true,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if d.vp9LoopFilterPool == nil {
		t.Fatal("threaded decoder did not initialise loop-filter pool")
	}

	if err := d.Decode(packet); err != nil {
		t.Fatalf("DecoderLoopFilterOpt=true Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("DecoderLoopFilterOpt=true NextFrame returned !ok")
	}
	assertVP9ImagesEqual(t, serial, frame)

	// Toggling off mid-stream keeps the deblock pass on the serial path.
	if err := d.SetLoopFilterOpt(false); err != nil {
		t.Fatalf("SetLoopFilterOpt(false): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("DecoderLoopFilterOpt=false Decode: %v", err)
	}
	frame, ok = d.NextFrame()
	if !ok {
		t.Fatal("DecoderLoopFilterOpt=false NextFrame returned !ok")
	}
	assertVP9ImagesEqual(t, serial, frame)
}

// TestVP9DecoderRowMTSteadyStateAlloc confirms the row-MT decode loop does
// not introduce per-frame allocations after warm-up. The wavefront primitive
// is allocated once at construction / first frame and reused thereafter.
func TestVP9DecoderRowMTSteadyStateAlloc(t *testing.T) {
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, 1)

	d, err := NewVP9Decoder(VP9DecoderOptions{Threads: 4, DecoderRowMT: true})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode: %v", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if allocs != 0 {
		t.Fatalf("row-MT decode steady state: got %v allocs/op, want 0", allocs)
	}
}

// TestVP9DecoderRowMTNoGoroutineLeak proves Close shuts down the row-MT
// arming + tile pool without leaving worker goroutines around.
func TestVP9DecoderRowMTNoGoroutineLeak(t *testing.T) {
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, 1)
	baseline := vp9TestGoroutineCount()

	d, err := NewVP9Decoder(VP9DecoderOptions{Threads: 4, DecoderRowMT: true})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for range 3 {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatal("NextFrame returned !ok")
		}
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := vp9TestGoroutineCount(); got > baseline {
		t.Fatalf("goroutines leaked: baseline=%d after-close=%d", baseline, got)
	}
}

func vp9TestGoroutineCount() int {
	// Allow the runtime a short window to drain finished goroutines after
	// channel close before sampling.
	const samples = 8
	last := runtime.NumGoroutine()
	for range samples {
		runtime.Gosched()
		last = runtime.NumGoroutine()
	}
	return last
}
