package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestCyclicRefreshStaticClassificationPopulatesSkinMapOnly(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               160,
		Height:              64,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(160, 64)
	fillImage(src, 128, 128, 128)
	fillMacroblock(src, 0, 0, 120, 117, 150)
	modes := make([]vp8enc.InterFrameMacroblockMode, 40)
	source := sourceImageFromPublic(src)

	e.prepareInterFrameSkinMap(source, 4, 10)
	next := e.assignInterFrameStaticSegments(source, 4, 10, modes)

	if e.skinMap[0] != 1 {
		t.Fatalf("skinMap[0] = %d, want libvpx skin classification", e.skinMap[0])
	}
	refreshed := 0
	for i := range 3 {
		if modes[i].SegmentID == vp8enc.StaticSegmentID {
			refreshed++
		}
	}
	if refreshed == 0 {
		t.Fatalf("segment IDs = %d/%d/%d, want cyclic refresh activity", modes[0].SegmentID, modes[1].SegmentID, modes[2].SegmentID)
	}
	if next != 2 {
		t.Fatalf("next cyclic refresh index = %d, want 2 from cyclic refresh cadence", next)
	}
}

func TestUpdateConsecutiveZeroLastMirrorsLibvpxCounter(t *testing.T) {
	counters := []uint8{0, 254, 7}
	modes := []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
	}

	updateConsecutiveZeroLast(modes, counters)
	updateConsecutiveZeroLast(modes, counters)

	want := []uint8{2, 255, 0}
	for i := range want {
		if counters[i] != want[i] {
			t.Fatalf("counter[%d] = %d, want %d", i, counters[i], want[i])
		}
	}
}

func TestUpdateCyclicRefreshMapFromInterFrameMirrorsLibvpxStates(t *testing.T) {
	refreshMap := []int8{0, 1, 0, 0}
	modes := []vp8enc.InterFrameMacroblockMode{
		{SegmentID: vp8enc.StaticSegmentID, RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV},
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
	}

	updateCyclicRefreshMapFromInterFrame(modes, refreshMap)

	want := []int8{-1, 0, 1, 1}
	for i := range want {
		if refreshMap[i] != want[i] {
			t.Fatalf("refreshMap[%d] = %d, want %d", i, refreshMap[i], want[i])
		}
	}
}

// TestAssignInterFrameStaticSegmentsAggressiveDenoiseOverridesByConsecZeroLast
// pins libvpx vp8/encoder/onyx_if.c cyclic_background_refresh lines 560-583:
// when aggressive denoising is engaged AND the initial cyclic refresh budget
// (block_count) is > 0, the per-MB seg map is re-derived from
// consec_zero_last[i] > denoise_pars.consec_zerolast, overriding the
// cyclic-refresh walker's seg-1 selection. The walker still runs first so
// cyclic_refresh_mode_index advances; only the seg-IDs get overwritten.

func TestAssignInterFrameStaticSegmentsAggressiveDenoiseOverridesByConsecZeroLast(t *testing.T) {
	t.Parallel()
	const rows, cols = 4, 5
	count := rows * cols
	e := &VP8Encoder{
		opts: EncoderOptions{NoiseSensitivity: 3, ErrorResilient: true},
	}
	e.cyclicRefreshConfigured = true
	e.rc.mode = RateControlCBR
	// Q below qp_thresh=80; frames_since_key > 2*15=30.
	e.rc.currentQuantizer = 50
	e.rc.framesSinceKeyframe = 60
	e.denoiser.allocated = true
	e.denoiser.mode, e.denoiser.params = vp8enc.DenoiserSetParameters(vp8enc.DenoiserModeForSensitivity(e.opts.NoiseSensitivity))
	if !e.aggressiveDenoiseSegmentationActive() {
		t.Fatalf("aggressiveDenoiseSegmentationActive=false, want true under test setup")
	}
	e.cyclicRefreshMap = make([]int8, count)
	e.cyclicRefreshAttemptMap = make([]int8, count)
	e.consecZeroLast = make([]uint8, count)
	// Set three MBs above the consec_zerolast threshold (15) and others below.
	threshold := e.denoiser.params.ConsecZeroLast
	overIdx := map[int]bool{3: true, 7: true, 11: true}
	for i := range count {
		if overIdx[i] {
			e.consecZeroLast[i] = uint8(threshold + 5)
		} else {
			e.consecZeroLast[i] = uint8(min(threshold, 255))
		}
	}
	// Seed the cyclic refresh map so the walker would normally pick MB 0 first.
	e.cyclicRefreshIndex = 0
	modes := make([]vp8enc.InterFrameMacroblockMode, count)
	src := vp8enc.SourceImage{Width: cols * 16, Height: rows * 16}
	_ = e.assignInterFrameStaticSegmentsForQuantizer(src, rows, cols, modes, e.rc.currentQuantizer)
	for i := range count {
		want := uint8(0)
		if overIdx[i] {
			want = vp8enc.StaticSegmentID
		}
		if uint8(modes[i].SegmentID) != want {
			t.Fatalf("modes[%d].SegmentID = %d, want %d (aggressive-denoise override should follow consec_zero_last > threshold)", i, modes[i].SegmentID, want)
		}
	}
}

// TestAssignInterFrameStaticSegmentsAggressiveDenoiseSkippedWhenBudgetZero
// pins the gate at libvpx onyx_if.c line 534: the aggressive-denoise override
// only runs when block_count > 0 (i.e., the initial refresh budget for this
// frame is positive). When screen-content-mode kills the budget, govpx must
// leave seg IDs all zero just like libvpx (which skips into the no-walker
// branch and inherits the memset).

func TestAssignInterFrameStaticSegmentsAggressiveDenoiseSkippedWhenBudgetZero(t *testing.T) {
	t.Parallel()
	const rows, cols = 4, 5
	count := rows * cols
	e := &VP8Encoder{
		opts: EncoderOptions{NoiseSensitivity: 3, ErrorResilient: true, ScreenContentMode: 1},
	}
	e.cyclicRefreshConfigured = true
	e.rc.mode = RateControlCBR
	// Drive screen-content stable-low-q branch: block_count = 0.
	e.rc.currentQuantizer = 19
	e.rc.framesSinceKeyframe = 300
	e.lastInterSkipCount = count // 100% > 95% threshold
	e.denoiser.allocated = true
	e.denoiser.mode, e.denoiser.params = vp8enc.DenoiserSetParameters(vp8enc.DenoiserModeForSensitivity(e.opts.NoiseSensitivity))
	if !e.aggressiveDenoiseSegmentationActive() {
		t.Fatalf("aggressive-denoise gate flipped off; this test relies on it being active")
	}
	refreshCount := e.cyclicRefreshMaxMBsPerFrameForQuantizer(rows, cols, e.rc.currentQuantizer)
	if refreshCount != 0 {
		t.Fatalf("refreshCount = %d, want 0 (screen-content stable low-q kills budget)", refreshCount)
	}
	e.cyclicRefreshMap = make([]int8, count)
	e.cyclicRefreshAttemptMap = make([]int8, count)
	e.consecZeroLast = make([]uint8, count)
	threshold := e.denoiser.params.ConsecZeroLast
	for i := range count {
		// Even with very-zero-last MBs, the override must not run because
		// block_count == 0 at the libvpx gate.
		e.consecZeroLast[i] = uint8(threshold + 5)
	}
	modes := make([]vp8enc.InterFrameMacroblockMode, count)
	src := vp8enc.SourceImage{Width: cols * 16, Height: rows * 16}
	_ = e.assignInterFrameStaticSegmentsForQuantizer(src, rows, cols, modes, e.rc.currentQuantizer)
	for i := range count {
		if modes[i].SegmentID != 0 {
			t.Fatalf("modes[%d].SegmentID = %d, want 0 (aggressive-denoise override gated off when block_count==0)", i, modes[i].SegmentID)
		}
	}
}

func TestCyclicRefreshMaxMBsPerFrameMirrorsLibvpxLayerCadence(t *testing.T) {
	if got := cyclicRefreshMaxMBsPerFrameForLayers(8, 8, 1); got != 3 {
		t.Fatalf("one-layer cyclic refresh MBs = %d, want libvpx MBs/20", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForLayers(8, 8, 2); got != 6 {
		t.Fatalf("two-layer cyclic refresh MBs = %d, want libvpx MBs/10", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForLayers(8, 8, 3); got != 9 {
		t.Fatalf("three-layer cyclic refresh MBs = %d, want libvpx MBs/7", got)
	}
}

func TestCyclicRefreshMaxMBsPerFrameMirrorsLibvpxScreenContentCadence(t *testing.T) {
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 1, 100, 0, 0); got != 6 {
		t.Fatalf("screen-content high-q cyclic refresh MBs = %d, want libvpx MBs/10", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 2, 80, 0, 0); got != 6 {
		t.Fatalf("aggressive screen-content high-q cyclic refresh MBs = %d, want libvpx MBs/10", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 1, 19, 251, 61); got != 0 {
		t.Fatalf("screen-content stable low-q cyclic refresh MBs = %d, want disabled", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 1, 19, 251, 60); got != 3 {
		t.Fatalf("screen-content low-q cyclic refresh MBs = %d, want libvpx MBs/20", got)
	}
}
