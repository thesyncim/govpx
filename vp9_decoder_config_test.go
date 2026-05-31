package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
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

// TestVP9DecoderLoopFilterOptGatesLoopFilterPool covers the gate: with the
// option off the deblock pass uses the serial path even on a threaded
// decoder, and with the option on the threaded helper pool drives the
// U / V plane deblock.
func TestVP9DecoderLoopFilterOptGatesLoopFilterPool(t *testing.T) {
	packet := vp9test.ColumnResidueKeyframe(t, 64, 64, 32, 32)

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
