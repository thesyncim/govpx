package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
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
