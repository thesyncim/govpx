package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestEncodeIntoStaticThresholdWritesCyclicRefreshSegmentation(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := segmentedQuantizationTestImage()
	packet := make([]byte, 16384)

	key, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled || !keyState.Segmentation.UpdateMap || !keyState.Segmentation.UpdateData {
		t.Fatalf("key segmentation = %+v, want map and data update", keyState.Segmentation)
	}
	wantAltQ := int8(vp8common.PublicQuantizerToQIndex(20)/2 - vp8common.PublicQuantizerToQIndex(20))
	if got := keyState.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != wantAltQ {
		t.Fatalf("key static segment alt-q = %d, want %d", got, wantAltQ)
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if d.modes[0].SegmentID != 0 || d.modes[1].SegmentID != 0 {
		t.Fatalf("key segment IDs = %d/%d, want all zero for cyclic refresh keyframe", d.modes[0].SegmentID, d.modes[1].SegmentID)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	assertImagesEqual(t, "static key current", keyFrame, publicImageFromVP8(&e.current.Img))

	second := segmentedQuantizationTestImage()
	for row := 0; row < second.Height; row++ {
		for col := range 16 {
			second.Y[row*second.YStride+col] = 96
		}
	}
	inter, err := e.EncodeInto(packet, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	interState := packetState(t, inter.Data)
	if !interState.Segmentation.Enabled || !interState.Segmentation.UpdateMap || !interState.Segmentation.UpdateData {
		t.Fatalf("inter segmentation = %+v, want map and data update", interState.Segmentation)
	}
	if err := d.Decode(inter.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	if d.modes[0].SegmentID != 0 || d.modes[1].SegmentID != 0 {
		t.Fatalf("inter segment IDs = %d/%d, want no cyclic refresh blocks in tiny frame", d.modes[0].SegmentID, d.modes[1].SegmentID)
	}
	interFrame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "static inter current", interFrame, publicImageFromVP8(&e.current.Img))
}

func TestCyclicRefreshSegmentationConfigMirrorsLibvpxEnablementAndBoost(t *testing.T) {
	e := VP8Encoder{}
	e.cyclicRefreshConfigured = true
	e.rc.mode = RateControlCBR
	e.rc.currentQuantizer = 20

	cfg := e.cyclicRefreshSegmentationConfig(false)

	if !cfg.Enabled || !cfg.UpdateMap || !cfg.UpdateData {
		t.Fatalf("cyclic segmentation = %+v, want enabled map/data update", cfg)
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != -10 {
		t.Fatalf("cyclic segment alt-q = %d, want background delta -10", got)
	}

	e.rc.currentQuantizer = 21
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled {
		t.Fatalf("q=21 cyclic segmentation disabled, want background boost")
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != -11 {
		t.Fatalf("q=21 cyclic segment alt-q = %d, want libvpx Q/2-Q delta -11", got)
	}

	e.rc.currentQuantizer = 1
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled {
		t.Fatalf("q=1 cyclic segmentation disabled, want libvpx Q/2-Q delta enabled")
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != -1 {
		t.Fatalf("q=1 cyclic segment alt-q = %d, want libvpx Q/2-Q delta -1", got)
	}

	e.rc.currentQuantizer = 0
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled || cfg.FeatureEnabled[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID] {
		t.Fatalf("q=0 cyclic segmentation = %+v, want enabled with no alt-q feature", cfg)
	}

	e.rc.mode = RateControlVBR
	e.opts.StaticThreshold = 1
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("CBR-born runtime VBR cyclic segmentation disabled, want sticky libvpx enablement")
	}

	vbrBorn := VP8Encoder{}
	vbrBorn.rc.mode = RateControlCBR
	vbrBorn.rc.currentQuantizer = 20
	if cfg := vbrBorn.cyclicRefreshSegmentationConfig(false); cfg.Enabled {
		t.Fatalf("VBR-born runtime CBR cyclic segmentation = %+v, want disabled", cfg)
	}

	e.rc.mode = RateControlCBR
	e.opts.ScreenContentMode = 2
	if cfg := e.cyclicRefreshSegmentationConfig(true); cfg.Enabled {
		t.Fatalf("screen-content mode 2 golden-refresh segmentation = %+v, want disabled", cfg)
	}
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("screen-content mode 2 non-golden cyclic segmentation disabled, want enabled")
	}

	e.rtcExternalDisableCyclicRefresh = true
	if cfg := e.cyclicRefreshSegmentationConfig(false); cfg.Enabled {
		t.Fatalf("RTC external rate-control cyclic segmentation = %+v, want disabled", cfg)
	}
	e.rtcExternalDisableCyclicRefresh = false
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("after disabling RTC external rate-control cyclic segmentation disabled, want enabled")
	}
}

func TestEncodeIntoDefaultCBREnablesLibvpxCyclicRefreshSegmentation(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled || !keyState.Segmentation.UpdateMap || !keyState.Segmentation.UpdateData {
		t.Fatalf("key segmentation = %+v, want libvpx default cyclic refresh", keyState.Segmentation)
	}
	if got := keyState.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != -2 {
		t.Fatalf("key cyclic alt-q = %d, want libvpx q/2-q delta -2", got)
	}

	inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	interState := packetState(t, inter.Data)
	if !interState.Segmentation.Enabled || !interState.Segmentation.UpdateMap || !interState.Segmentation.UpdateData {
		t.Fatalf("inter segmentation = %+v, want libvpx default cyclic refresh", interState.Segmentation)
	}
}

func TestCyclicRefreshSegmentationUsesPreRecodeQuantizer(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             128,
		Height:            128,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           -3,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 1<<20)
	var result EncodeResult
	for frame := range 3 {
		result, err = e.EncodeInto(dst, encoderValidationPanningFrame(128, 128, frame), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", frame, err)
		}
	}
	state := packetState(t, result.Data)
	if got := state.Quant.BaseQIndex; got != 4 {
		t.Fatalf("frame 2 base Q = %d, want recoded final Q 4", got)
	}
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != -7 {
		t.Fatalf("frame 2 cyclic alt-q = %d, want pre-recode Q/2-Q delta -7", got)
	}
}

func TestEncodeIntoScreenContentMode2KeepsKeyFrameCyclicSegmentation(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		ScreenContentMode:   2,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled || !keyState.Segmentation.UpdateMap || !keyState.Segmentation.UpdateData {
		t.Fatalf("screen-content mode 2 key segmentation = %+v, want map/data update", keyState.Segmentation)
	}
}

func TestOvershootDropCommitsCyclicRefreshState(t *testing.T) {
	const (
		width      = 256
		height     = 144
		fps        = 30
		targetKbps = 700
	)
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		ScreenContentMode: 2,
		Tuning:            TunePSNR,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, 1<<20)
	key, err := e.EncodeInto(packet, encoderValidationPanningFrame(width, height, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if key.Dropped || !key.KeyFrame {
		t.Fatalf("key result = dropped:%t key:%t, want emitted keyframe", key.Dropped, key.KeyFrame)
	}

	beforeIndex := e.cyclicRefreshIndex
	dropped, err := e.EncodeInto(packet, encoderValidationPanningFrame(width, height, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("drop EncodeInto returned error: %v", err)
	}
	if !dropped.Dropped {
		t.Fatalf("frame 1 dropped = false, want screen-content overshoot drop")
	}
	if e.cyclicRefreshIndex == beforeIndex {
		t.Fatalf("cyclicRefreshIndex after overshoot drop = %d, want committed advance", e.cyclicRefreshIndex)
	}

	rows := (height + 15) / 16
	cols := (width + 15) / 16
	nonZero := false
	for _, v := range e.cyclicRefreshMap[:rows*cols] {
		if v != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		t.Fatalf("cyclicRefreshMap stayed all zero after overshoot drop")
	}
}

func TestCyclicRefreshRecodeReusesMutatedSegmentMap(t *testing.T) {
	const rows, cols = 4, 10
	count := rows * cols
	e := &VP8Encoder{
		opts:                    EncoderOptions{RateControlMode: RateControlCBR, ScreenContentMode: 1},
		cyclicRefreshConfigured: true,
		cyclicRefreshMap:        make([]int8, count),
		cyclicRefreshAttemptMap: make([]int8, count),
		cyclicRefreshIndex:      0,
		lastInterSkipCount:      0,
	}
	e.rc.mode = RateControlCBR
	e.rc.framesSinceKeyframe = 2
	state := newInterFrameCyclicRefreshRecodeState(127)
	modes := make([]vp8enc.InterFrameMacroblockMode, count)
	src := vp8enc.SourceImage{Width: cols * 16, Height: rows * 16}

	segmentation, enabled := e.interFrameCyclicRefreshSegmentationForRecode(&state, false)
	if !enabled || !segmentation.Enabled {
		t.Fatalf("cyclic segmentation disabled, want enabled")
	}
	next := e.prepareInterFrameCyclicRefreshSegmentsForRecode(&state, src, rows, cols, modes)
	if next != 4 || modes[0].SegmentID != vp8enc.StaticSegmentID {
		t.Fatalf("initial cyclic refresh next/seg0 = %d/%d, want 4/%d", next, modes[0].SegmentID, vp8enc.StaticSegmentID)
	}

	modes[0] = vp8enc.InterFrameMacroblockMode{SegmentID: 0, RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV}
	modes[1] = vp8enc.InterFrameMacroblockMode{SegmentID: vp8enc.StaticSegmentID, RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	e.updateInterFrameCyclicRefreshAttemptMapForRecode(&state, rows, cols, modes)
	if got := e.cyclicRefreshAttemptMap[0]; got != 1 {
		t.Fatalf("attempt refresh map[0] after cleared non-LAST segment = %d, want dirty 1", got)
	}
	if got := e.cyclicRefreshAttemptMap[1]; got != -1 {
		t.Fatalf("attempt refresh map[1] after refreshed segment = %d, want cooldown -1", got)
	}

	next = e.prepareInterFrameCyclicRefreshSegmentsForRecode(&state, src, rows, cols, modes)
	if next != 4 || modes[0].SegmentID != 0 {
		t.Fatalf("second recode next/seg0 = %d/%d, want reused 4/0", next, modes[0].SegmentID)
	}
	modes[0] = vp8enc.InterFrameMacroblockMode{SegmentID: 0, RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	e.updateInterFrameCyclicRefreshAttemptMapForRecode(&state, rows, cols, modes)
	if got := e.cyclicRefreshAttemptMap[0]; got != 0 {
		t.Fatalf("attempt refresh map[0] after later LAST/ZEROMV = %d, want candidate 0", got)
	}

	e.commitLiveCyclicRefreshMap(rows, cols, state.nextIndex)
	if e.cyclicRefreshIndex != 4 || e.cyclicRefreshMap[0] != 0 || e.cyclicRefreshMap[1] != -1 {
		t.Fatalf("committed cyclic refresh index/map[0:2] = %d/%v, want 4/[0 -1]", e.cyclicRefreshIndex, e.cyclicRefreshMap[:2])
	}
}

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

func TestEncodeIntoStaticThresholdRotatesCyclicRefreshSegments(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               80,
		Height:              64,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, 65536)
	keySource := testImage(80, 64)
	fillImage(keySource, 128, 128, 128)
	key, err := e.EncodeInto(packet, keySource, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}

	var prevRefreshed []int
	nonEmptyRefreshes := 0
	for frame := range 4 {
		src := publicImageFromVP8(&e.lastRef.Img)
		inter, err := e.EncodeInto(packet, src, uint64(frame+1), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		if err := d.Decode(inter.Data); err != nil {
			t.Fatalf("inter %d Decode returned error: %v", frame, err)
		}
		refreshed := make([]int, 0, len(d.modes))
		for i := range d.modes {
			if d.modes[i].SegmentID == vp8enc.StaticSegmentID {
				refreshed = append(refreshed, i)
			}
		}
		if len(refreshed) == 0 {
			t.Fatalf("inter %d refreshed set empty, want cyclic refresh activity", frame)
		}
		if nonEmptyRefreshes > 0 && len(refreshed) == len(prevRefreshed) {
			same := true
			for i := range refreshed {
				if refreshed[i] != prevRefreshed[i] {
					same = false
					break
				}
			}
			if same {
				t.Fatalf("inter %d refreshed set %v, want rotation from previous frame %v", frame, refreshed, prevRefreshed)
			}
		}
		prevRefreshed = append(prevRefreshed[:0], refreshed...)
		nonEmptyRefreshes++
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("inter %d NextFrame returned no frame", frame)
		}
	}
	if nonEmptyRefreshes < 2 {
		t.Fatalf("non-empty refresh frames = %d, want at least 2 to verify rotation", nonEmptyRefreshes)
	}
}

// TestEncodeIntoCyclicRefreshIndexPreservedAcrossKeyFrames pins libvpx
// vp8/encoder/onyx_if.c cyclic_background_refresh: the cyclic_refresh_mode_index
// is reset to 0 only on init (line 1213) and resize, not on each key frame
// (the iteration loop at line 534 is gated on frame_type != KEY_FRAME so a
// key frame leaves the index untouched). govpx must mirror that — resetting
// the index on each forced keyframe shifts the rolling refresh window
// relative to libvpx for every GOP after the first.
func TestEncodeIntoCyclicRefreshIndexPreservedAcrossKeyFrames(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               80,
		Height:              64,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, 65536)
	src := testImage(80, 64)
	fillImage(src, 128, 128, 128)
	if _, err := e.EncodeInto(packet, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	// Drive a few inter frames so the cyclic refresh index advances away
	// from zero. A 5x4 frame has 20 MBs and refreshCount = 20/20 = 1, so
	// each inter frame advances the index by exactly 1.
	for frame := range 3 {
		s := publicImageFromVP8(&e.lastRef.Img)
		if _, err := e.EncodeInto(packet, s, uint64(frame+1), 1, 0); err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
	}
	beforeKey := e.cyclicRefreshIndex
	if beforeKey == 0 {
		t.Fatalf("cyclicRefreshIndex stayed at 0 after 3 inter frames; expected forward progress")
	}
	// Force a second keyframe and confirm the index survives.
	e.ForceKeyFrame()
	src2 := testImage(80, 64)
	fillImage(src2, 128, 128, 128)
	if _, err := e.EncodeInto(packet, src2, 4, 1, 0); err != nil {
		t.Fatalf("forced key EncodeInto returned error: %v", err)
	}
	if e.cyclicRefreshIndex != beforeKey {
		t.Fatalf("cyclicRefreshIndex after key frame = %d, want libvpx-preserved %d", e.cyclicRefreshIndex, beforeKey)
	}
}

func TestCommitKeyFrameCyclicRefreshMapPreservesRollingMap(t *testing.T) {
	e := &VP8Encoder{
		cyclicRefreshMap:        []int8{0, -1, 1, 0},
		cyclicRefreshAttemptMap: []int8{1, 0, -1, 1},
	}
	modes := []vp8enc.KeyFrameMacroblockMode{
		{SegmentID: 0},
		{SegmentID: 1},
		{SegmentID: 0},
		{SegmentID: 0},
	}

	e.commitKeyFrameCyclicRefreshMap(2, 2, modes, true)

	wantMap := []int8{0, -1, 1, 0}
	wantAttempt := []int8{1, 0, -1, 1}
	for i := range wantMap {
		if e.cyclicRefreshMap[i] != wantMap[i] {
			t.Fatalf("cyclicRefreshMap[%d] = %d, want %d (map=%v)", i, e.cyclicRefreshMap[i], wantMap[i], e.cyclicRefreshMap)
		}
		if e.cyclicRefreshAttemptMap[i] != wantAttempt[i] {
			t.Fatalf("cyclicRefreshAttemptMap[%d] = %d, want %d (map=%v)", i, e.cyclicRefreshAttemptMap[i], wantAttempt[i], e.cyclicRefreshAttemptMap)
		}
	}
}

func TestEncodeIntoStaticThresholdWritesCyclicRefreshSegmentationForMatchingReference(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := segmentedQuantizationTestImage()
	keyPacket := make([]byte, 16384)
	key, err := e.EncodeInto(keyPacket, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := publicImageFromVP8(&e.lastRef.Img)
	interPacket := make([]byte, 16384)

	inter, err := e.EncodeInto(interPacket, reconstructed, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want matching-reference interframe")
	}
	state := packetState(t, inter.Data)
	if !state.Segmentation.Enabled || !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("inter segmentation = %+v, want map and data update for matching reference", state.Segmentation)
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	if err := d.Decode(inter.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	if d.modes[0].SegmentID != 0 || d.modes[1].SegmentID != 0 {
		t.Fatalf("inter segment IDs = %d/%d, want no cyclic refresh blocks in tiny frame", d.modes[0].SegmentID, d.modes[1].SegmentID)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "matching-reference segmented inter", frame, publicImageFromVP8(&e.current.Img))
}

func TestEncodeIntoStaticThresholdSkipsTemporalEnhancementLayerSegmentation(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	if err := e.SetStaticThreshold(1); err != nil {
		t.Fatalf("SetStaticThreshold returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	packet := make([]byte, 4096)

	key, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled {
		t.Fatalf("key segmentation disabled, want base-layer cyclic refresh enabled")
	}

	enhancement, err := e.EncodeInto(packet, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("enhancement EncodeInto returned error: %v", err)
	}
	if enhancement.TemporalLayerID != 1 {
		t.Fatalf("enhancement temporal layer = %d, want 1", enhancement.TemporalLayerID)
	}
	enhancementState := packetState(t, enhancement.Data)
	if enhancementState.Segmentation.Enabled || enhancementState.Segmentation.UpdateMap || enhancementState.Segmentation.UpdateData {
		t.Fatalf("enhancement segmentation = %+v, want disabled like libvpx non-base temporal layer", enhancementState.Segmentation)
	}
}
