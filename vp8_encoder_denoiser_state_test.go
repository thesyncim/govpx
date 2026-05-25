package govpx

import (
	"bytes"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

func TestDenoiserInactiveActiveMapMacroblocksUseZeroMVLastDecision(t *testing.T) {
	const width, height = 32, 32
	rows := geometry.MacroblockRows(height)
	cols := geometry.MacroblockCols(width)
	src := testImage(width, height)
	fillImage(src, 96, 128, 128)

	e, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		KeyFrameInterval:  999,
		NoiseSensitivity:  3,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 32*1024)
	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	inactive := make([]uint8, rows*cols)
	if err := e.SetActiveMap(inactive, rows, cols); err != nil {
		t.Fatalf("SetActiveMap returned error: %v", err)
	}
	if _, err := e.EncodeInto(dst, src, 1, 1, 0); err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if len(e.denoiser.state) < rows*cols {
		t.Fatalf("denoiser state len = %d, want at least %d", len(e.denoiser.state), rows*cols)
	}
	for i, state := range e.denoiser.state[:rows*cols] {
		if state != vp8enc.DenoiserStateFilterZeroMV {
			t.Fatalf("inactive MB %d denoiser state = %d, want zero-MV filter state", i, state)
		}
	}
}

func TestDenoiserPickmodeMVBiasReturns75ForAggressiveMode(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.opts.NoiseSensitivity = 0
	if got := e.denoiserPickmodeMVBias(); got != 100 {
		t.Fatalf("denoiser-off bias = %d, want 100", got)
	}
	e.opts.NoiseSensitivity = 2
	if got := e.denoiserPickmodeMVBias(); got != 100 {
		t.Fatalf("YUV mode bias = %d, want 100", got)
	}
	e.opts.NoiseSensitivity = 3
	if got := e.denoiserPickmodeMVBias(); got != 75 {
		t.Fatalf("aggressive bias = %d, want 75", got)
	}
}

func TestRuntimeNoiseSensitivityKeepsAllocatedDenoiserModeSticky(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	src := sourceImageFromImage(testImage(32, 32))

	if err := e.SetNoiseSensitivity(1); err != nil {
		t.Fatalf("SetNoiseSensitivity(1): %v", err)
	}
	e.preprocessSource(src, 0, encodeSourceMetadata{})
	if e.denoiser.mode != vp8enc.DenoiserOnYOnly {
		t.Fatalf("initial mode = %d, want Y-only", e.denoiser.mode)
	}
	if got := e.denoiserPickmodeMVBias(); got != 100 {
		t.Fatalf("Y-only pickmode bias = %d, want 100", got)
	}

	if err := e.SetNoiseSensitivity(3); err != nil {
		t.Fatalf("SetNoiseSensitivity(3): %v", err)
	}
	e.preprocessSource(src, 0, encodeSourceMetadata{})
	if e.denoiser.mode != vp8enc.DenoiserOnYOnly {
		t.Fatalf("mode after 1->3 = %d, want sticky Y-only", e.denoiser.mode)
	}
	if got := e.denoiserPickmodeMVBias(); got != 100 {
		t.Fatalf("pickmode bias after sticky 1->3 = %d, want 100", got)
	}

	// libvpx: vp8/encoder/onyx_if.c:1721-1733 vp8_change_config only
	// allocates the denoiser when noise_sensitivity > 0 AND the buffer is
	// still NULL, and never frees / resets it on the runtime path. Setting
	// the sensitivity to 0 must therefore leave the allocated buffers and
	// the sticky mode in place; only subsequent inter encodes bypass the
	// denoiser via the cpi->oxcf.noise_sensitivity > 0 gates.
	if err := e.SetNoiseSensitivity(0); err != nil {
		t.Fatalf("SetNoiseSensitivity(0): %v", err)
	}
	if !e.denoiser.allocated {
		t.Fatalf("denoiser deallocated after disable; libvpx keeps the buffers")
	}
	if e.denoiser.mode != vp8enc.DenoiserOnYOnly {
		t.Fatalf("mode after sticky disable = %d, want Y-only", e.denoiser.mode)
	}
	if err := e.SetNoiseSensitivity(3); err != nil {
		t.Fatalf("SetNoiseSensitivity(3) after disable: %v", err)
	}
	e.preprocessSource(src, 0, encodeSourceMetadata{})
	// libvpx: vp8_change_config skips vp8_denoiser_allocate when
	// yv12_mc_running_avg.buffer_alloc is non-NULL, so the recorded
	// denoiser_mode stays Y-only across noise_sensitivity 1 → 0 → 3.
	if e.denoiser.mode != vp8enc.DenoiserOnYOnly {
		t.Fatalf("mode after sticky disable->3 = %d, want Y-only", e.denoiser.mode)
	}
	if got := e.denoiserPickmodeMVBias(); got != 100 {
		t.Fatalf("pickmode bias after sticky disable->3 = %d, want 100", got)
	}

	if err := e.SetNoiseSensitivity(6); err != nil {
		t.Fatalf("SetNoiseSensitivity(6): %v", err)
	}
	e.preprocessSource(src, 0, encodeSourceMetadata{})
	if e.denoiser.mode != vp8enc.DenoiserOnYOnly {
		t.Fatalf("mode after 3->6 = %d, want sticky Y-only", e.denoiser.mode)
	}
}

func TestAggressiveDenoiseSegmentationUsesAllocatedDenoiserMode(t *testing.T) {
	e := &VP8Encoder{
		opts: EncoderOptions{NoiseSensitivity: 3},
	}
	e.rc.currentQuantizer = 50
	e.rc.framesSinceKeyframe = 60

	e.denoiser.allocated = true
	e.denoiser.mode, e.denoiser.params = vp8enc.DenoiserSetParameters(vp8enc.DenoiserModeForSensitivity(1))
	if e.aggressiveDenoiseSegmentationActive() {
		t.Fatalf("aggressive denoise segmentation active with sticky Y-only mode")
	}

	e.denoiser.mode, e.denoiser.params = vp8enc.DenoiserSetParameters(vp8enc.DenoiserModeForSensitivity(3))
	if !e.aggressiveDenoiseSegmentationActive() {
		t.Fatalf("aggressive denoise segmentation inactive with allocated aggressive mode")
	}
}

func TestDenoiserReferenceTooOldMirrorsLibvpxRange(t *testing.T) {
	e := &VP8Encoder{}
	e.referenceFrameNumbers[vp8common.GoldenFrame] = 0
	e.referenceFrameNumbers[vp8common.AltRefFrame] = 1
	e.referenceFrameNumbers[vp8common.LastFrame] = 0

	e.frameCount = vp8enc.DenoiserMaxGFARFRange
	if e.denoiserReferenceTooOld(vp8common.GoldenFrame) {
		t.Fatalf("GOLDEN ref at distance %d marked too old", vp8enc.DenoiserMaxGFARFRange)
	}

	e.frameCount = vp8enc.DenoiserMaxGFARFRange + 1
	if !e.denoiserReferenceTooOld(vp8common.GoldenFrame) {
		t.Fatalf("GOLDEN ref at distance %d not marked too old", vp8enc.DenoiserMaxGFARFRange+1)
	}

	if e.denoiserReferenceTooOld(vp8common.LastFrame) {
		t.Fatalf("LAST ref should never be rejected by the GF/ARF denoiser age gate")
	}

	e.frameCount = vp8enc.DenoiserMaxGFARFRange + 1
	if e.denoiserReferenceTooOld(vp8common.AltRefFrame) {
		t.Fatalf("ALTREF ref at distance %d marked too old", vp8enc.DenoiserMaxGFARFRange)
	}
}

func TestDenoiserSkinGateUsesMVBiasCounter(t *testing.T) {
	e := &VP8Encoder{
		skinMap:              []uint8{1},
		consecZeroLast:       []uint8{0},
		consecZeroLastMVBias: []uint8{2},
	}
	if e.denoiserSkinGateBlocksFilter(0, 0, 1, 0, 0) {
		t.Fatalf("skin denoiser gate used regular zero-LAST counter, want mv-bias counter")
	}

	e.consecZeroLastMVBias[0] = 1
	if !e.denoiserSkinGateBlocksFilter(0, 0, 1, 0, 0) {
		t.Fatalf("skin denoiser gate did not block when mv-bias counter < 2")
	}

	e.consecZeroLastMVBias[0] = 2
	if !e.denoiserSkinGateBlocksFilter(0, 0, 1, 0, 1) {
		t.Fatalf("skin denoiser gate did not block non-zero motion")
	}
}

func TestDenoiserAvgForRefreshHonorsCopyBufferControls(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	e.opts.NoiseSensitivity = 2
	if err := e.denoiser.ensureAllocated(32, 32); err != nil {
		t.Fatalf("ensureAllocated: %v", err)
	}

	fillVP8Image(&e.denoiser.runningAvg[denoiserAvgIntra].Img, 33)
	fillVP8Image(&e.denoiser.runningAvg[denoiserAvgLast].Img, 11)
	fillVP8Image(&e.denoiser.runningAvg[denoiserAvgGolden].Img, 22)
	fillVP8Image(&e.denoiser.runningAvg[denoiserAvgAltRef].Img, 44)

	e.copyDenoiserAvgForRefresh(vp8enc.InterFrameStateConfig{
		CopyBufferToGolden: 1,
		CopyBufferToAltRef: 2,
	})

	intra := &e.denoiser.runningAvg[denoiserAvgIntra].Img
	last := &e.denoiser.runningAvg[denoiserAvgLast].Img
	golden := &e.denoiser.runningAvg[denoiserAvgGolden].Img
	alt := &e.denoiser.runningAvg[denoiserAvgAltRef].Img
	if !sameVP8Planes(golden, intra) {
		t.Fatalf("GOLDEN denoiser average did not follow CopyBufferToGolden")
	}
	if !sameVP8Planes(alt, intra) {
		t.Fatalf("ALTREF denoiser average did not follow CopyBufferToAltRef")
	}
	if last.Y[0] != 11 || last.U[0] != 11 || last.V[0] != 11 {
		t.Fatalf("LAST denoiser average changed without RefreshLast")
	}
	assertCodedBordersExtended(t, golden)
	assertCodedBordersExtended(t, alt)
}

func TestDenoiserEnsureAllocatedReusesStateAfterReset(t *testing.T) {
	var d denoiserState
	if err := d.ensureAllocated(64, 64); err != nil {
		t.Fatalf("ensureAllocated returned error: %v", err)
	}
	d.reset()
	allocs := testing.AllocsPerRun(20, func() {
		if err := d.ensureAllocated(64, 64); err != nil {
			panic(err)
		}
		d.reset()
	})
	if allocs != 0 {
		t.Fatalf("ensureAllocated after reset allocs = %f, want 0", allocs)
	}
}

func sameVP8Planes(a *vp8common.Image, b *vp8common.Image) bool {
	return bytes.Equal(a.Y, b.Y) && bytes.Equal(a.U, b.U) && bytes.Equal(a.V, b.V)
}
