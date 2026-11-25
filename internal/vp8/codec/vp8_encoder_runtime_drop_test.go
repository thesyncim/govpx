package codec

import (
	"errors"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	vpxrc "github.com/thesyncim/govpx/internal/vpx/ratecontrol"
	"testing"
)

func TestEncodeIntoDropsInterFrameWhenBufferUnderrunAndAllowed(t *testing.T) {
	e := newLowBitrateDropTestEncoder(t, true)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !key.KeyFrame || key.Dropped {
		t.Fatalf("key result = key:%t dropped:%t, want encoded keyframe", key.KeyFrame, key.Dropped)
	}
	e.rc.bufferLevelBits = -1
	drainedBuffer := e.rc.bufferLevelBits

	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if !inter.Dropped || inter.KeyFrame || len(inter.Data) != 0 || inter.SizeBytes != 0 {
		t.Fatalf("inter result = key:%t dropped:%t size:%d data:%d, want dropped interframe", inter.KeyFrame, inter.Dropped, inter.SizeBytes, len(inter.Data))
	}
	if inter.BufferLevelBits != drainedBuffer+e.rc.bitsPerFrame {
		t.Fatalf("buffer after drop = %d, want libvpx underrun recovery %d", inter.BufferLevelBits, drainedBuffer+e.rc.bitsPerFrame)
	}
}

func TestEncodeIntoDoesNotDropWhenFrameDroppingDisabled(t *testing.T) {
	e := newLowBitrateDropTestEncoder(t, false)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.Dropped || inter.KeyFrame || inter.SizeBytes == 0 || len(inter.Data) == 0 {
		t.Fatalf("inter result = key:%t dropped:%t size:%d data:%d, want encoded interframe", inter.KeyFrame, inter.Dropped, inter.SizeBytes, len(inter.Data))
	}
}

func TestEncodeIntoDoesNotDropInvisibleInterFrame(t *testing.T) {
	e := newLowBitrateDropTestEncoder(t, true)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	inter, err := e.EncodeInto(dst, src, 1, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible inter EncodeInto returned error: %v", err)
	}
	if inter.Dropped || inter.KeyFrame || inter.SizeBytes == 0 || len(inter.Data) == 0 {
		t.Fatalf("invisible inter result = key:%t dropped:%t size:%d data:%d, want encoded invisible interframe", inter.KeyFrame, inter.Dropped, inter.SizeBytes, len(inter.Data))
	}
}

func TestEncodeIntoInvisibleFrameUsesLibvpxBufferOverheadAccounting(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	beforeKeyBuffer := e.rc.bufferLevelBits
	frameBandwidth := e.rc.bitsPerFrame
	maximumBuffer := e.rc.maximumBufferBits
	key, err := e.EncodeInto(dst, src, 0, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible key EncodeInto returned error: %v", err)
	}
	wantKeyBuffer := temporalTestBufferAfterFrame(beforeKeyBuffer, frameBandwidth, maximumBuffer, vpxrc.EncodedSizeBits(key.SizeBytes))
	if key.BufferLevelBits != wantKeyBuffer || e.rc.bufferLevelBits != wantKeyBuffer {
		t.Fatalf("invisible key buffer = result:%d rc:%d, want %d", key.BufferLevelBits, e.rc.bufferLevelBits, wantKeyBuffer)
	}

	beforeInterBuffer := e.rc.bufferLevelBits
	inter, err := e.EncodeInto(dst, src, 1, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible inter EncodeInto returned error: %v", err)
	}
	wantInterBuffer := temporalTestBufferAfterFrame(beforeInterBuffer, frameBandwidth, maximumBuffer, vpxrc.EncodedSizeBits(inter.SizeBytes))
	if inter.BufferLevelBits != wantInterBuffer || e.rc.bufferLevelBits != wantInterBuffer {
		t.Fatalf("invisible inter buffer = result:%d rc:%d, want %d", inter.BufferLevelBits, e.rc.bufferLevelBits, wantInterBuffer)
	}
}

func TestSetRateControlQAcceptsCQLevelWithoutCQFloor(t *testing.T) {
	e := newTestEncoder(t)
	err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             28,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("SetRateControl returned error: %v", err)
	}
	if e.rc.mode != RateControlQ || e.rc.cqLevel != vp8common.PublicQuantizerToQIndex(28) {
		t.Fatalf("Q mode state = mode:%d cq:%d, want RateControlQ / qindex %d", e.rc.mode, e.rc.cqLevel, vp8common.PublicQuantizerToQIndex(28))
	}
	if e.rc.currentQuantizer >= e.rc.cqLevel {
		t.Fatalf("Q current quantizer = %d, want below CQ qindex %d to prove no CQ floor", e.rc.currentQuantizer, e.rc.cqLevel)
	}
}

func TestSetCQLevelRejectedUpdatesPreserveCQState(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             24,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetCQLevel(64); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("out-of-range SetCQLevel error = %v, want ErrInvalidQuantizer", err)
	}
	if err := e.SetCQLevel(3); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("below-min SetCQLevel error = %v, want ErrInvalidQuantizer", err)
	}
	if e.rc.cqLevel != vp8common.PublicQuantizerToQIndex(24) {
		t.Fatalf("CQ level after rejected updates = %d, want qindex %d", e.rc.cqLevel, vp8common.PublicQuantizerToQIndex(24))
	}
}

func TestSetCQLevelValidationAppliesToRateControlQ(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             24,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetCQLevel(3); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("below-min Q SetCQLevel error = %v, want ErrInvalidQuantizer", err)
	}
	if err := e.SetCQLevel(40); err != nil {
		t.Fatalf("Q SetCQLevel returned error: %v", err)
	}
	if e.rc.cqLevel != vp8common.PublicQuantizerToQIndex(40) {
		t.Fatalf("Q cqLevel = %d, want qindex %d", e.rc.cqLevel, vp8common.PublicQuantizerToQIndex(40))
	}
	if e.rc.currentQuantizer >= e.rc.cqLevel {
		t.Fatalf("Q current quantizer = %d, want no reset to CQ qindex %d", e.rc.currentQuantizer, e.rc.cqLevel)
	}
}

func TestSetGFCBRBoostPctAffectsGoldenRefreshTarget(t *testing.T) {
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
	if err := e.SetGFCBRBoostPct(50); err != nil {
		t.Fatalf("SetGFCBRBoostPct returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	refreshFrame := e.rc.framesTillGFUpdateDue + 1
	cbrInterval := e.goldenFrameCBRInterval(rows, cols)
	for frame := 1; frame <= refreshFrame; frame++ {
		wantRC := e.rc
		if frame == refreshFrame {
			wantRC.framesTillGFUpdateDue = cbrInterval
			wantRC.currentGFInterval = cbrInterval
		}
		wantRC.beginFrame(false)
		wantTarget := wantRC.frameTargetBits
		if frame == refreshFrame {
			wantTarget = boostedFrameTargetBits(wantTarget, e.rc.gfCBRBoostPct)
		}
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		if frame == refreshFrame && inter.FrameTargetBits != wantTarget {
			t.Fatalf("boosted target = %d, want libvpx CBR target %d", inter.FrameTargetBits, wantTarget)
		}
	}
}

func TestRuntimeControlsReemitPreservedVBRSegmentationUpdate(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		Tuning:            TunePSNR,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	if _, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 0), 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto key: %v", err)
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlVBR,
		TargetBitrateKbps:   300,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(VBR): %v", err)
	}
	first, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto VBR inter: %v", err)
	}
	if state := packetState(t, first.Data); !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("first VBR segmentation = %+v, want map/data update", state.Segmentation)
	}

	if err := e.SetRTCExternalRateControl(false); err != nil {
		t.Fatalf("SetRTCExternalRateControl(false): %v", err)
	}
	second, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 2), 2, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto RTC false inter: %v", err)
	}
	if state := packetState(t, second.Data); !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("RTC false segmentation = %+v, want preserved map/data update", state.Segmentation)
	}

	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	active := make([]uint8, rows*cols)
	for i := range active {
		active[i] = uint8(i & 1)
	}
	if err := e.SetActiveMap(active, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	third, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 3), 3, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto active-map inter: %v", err)
	}
	if state := packetState(t, third.Data); !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("active-map segmentation = %+v, want preserved map/data update", state.Segmentation)
	}
}

func TestSetRateControlPinsLibvpxCyclicRefreshMode(t *testing.T) {
	cbr, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder(CBR): %v", err)
	}
	defer cbr.Close()
	if !cbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("CBR-born encoder cyclic refresh disabled, want enabled")
	}
	if err := cbr.SetRateControl(RateControlConfig{
		Mode:                RateControlVBR,
		TargetBitrateKbps:   cbr.rc.targetBitrateKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("CBR-born SetRateControl(VBR): %v", err)
	}
	if !cbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("CBR-born runtime VBR cyclic refresh disabled, want libvpx-pinned at construction")
	}

	vbr, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder(VBR): %v", err)
	}
	defer vbr.Close()
	if vbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("VBR-born encoder cyclic refresh enabled, want disabled")
	}
	if err := vbr.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("VBR-born SetRateControl(CBR): %v", err)
	}
	if vbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("VBR-born runtime CBR cyclic refresh enabled, want libvpx-pinned at construction")
	}
}

// TestSetRateControlVBRKeepsLibvpxCyclicRefreshHeader asserts the
// segmentation header carries through a CBR→VBR runtime transition on a
// CBR-born encoder. libvpx pins cyclic_refresh_mode_enabled at compressor
// creation, so VBR inter frames emitted after vpx_codec_enc_config_set
// keep cyclic refresh active and continue to re-emit the segment map and
// alt-Q feature data each frame, matching pack_bitstream output on the
// VBR→CBR / CBR→VBR runtime-transition byte-parity oracles.

func TestSetRateControlVBRKeepsLibvpxCyclicRefreshHeader(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		Tuning:            TunePSNR,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 8192)
	key, err := e.EncodeInto(dst, encoderValidationPanningFrame(16, 16, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto key: %v", err)
	}
	keyState := packetState(t, key.Data)
	wantDelta := keyState.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]
	if !keyState.Segmentation.Enabled || !keyState.Segmentation.UpdateMap || !keyState.Segmentation.UpdateData || wantDelta == 0 {
		t.Fatalf("key segmentation = %+v, want cyclic-refresh map/data with nonzero alt-q", keyState.Segmentation)
	}

	cfg := RateControlConfig{
		Mode:                RateControlVBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}
	if err := e.SetRateControl(cfg); err != nil {
		t.Fatalf("SetRateControl(VBR): %v", err)
	}
	if !e.cyclicRefreshModeEnabled(false) {
		t.Fatalf("cyclic refresh disabled after VBR config, want construction-pinned active for CBR-born")
	}
	inter, err := e.EncodeInto(dst, encoderValidationPanningFrame(16, 16, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto inter: %v", err)
	}
	interState := packetState(t, inter.Data)
	if !interState.Segmentation.Enabled || !interState.Segmentation.UpdateMap || !interState.Segmentation.UpdateData {
		t.Fatalf("VBR inter segmentation = %+v, want cyclic-refresh map/data update", interState.Segmentation)
	}
	if got := interState.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != wantDelta {
		t.Fatalf("VBR inter alt-q delta = %d, want carried %d", got, wantDelta)
	}
}

func TestSetRateControlCQRefreshesPreservedSegmentationDelta(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		Tuning:            TunePSNR,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	if _, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 0), 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto key: %v", err)
	}
	if _, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 1), 1, 1, 0); err != nil {
		t.Fatalf("EncodeInto first inter: %v", err)
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   300,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             30,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(CQ): %v", err)
	}
	inter, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 2), 2, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto CQ inter: %v", err)
	}
	state := packetState(t, inter.Data)
	wantQ := vp8common.PublicQuantizerToQIndex(30)
	wantDelta := cyclicRefreshQuantizerDeltaForQuantizer(wantQ)
	if state.Quant.BaseQIndex != uint8(wantQ) {
		t.Fatalf("CQ base q = %d, want %d", state.Quant.BaseQIndex, wantQ)
	}
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != wantDelta {
		t.Fatalf("CQ preserved alt-q delta = %d, want refreshed %d", got, wantDelta)
	}

	if err := e.SetCQLevel(40); err != nil {
		t.Fatalf("SetCQLevel(40): %v", err)
	}
	next, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 3), 3, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto CQ-level inter: %v", err)
	}
	state = packetState(t, next.Data)
	wantQ = vp8common.PublicQuantizerToQIndex(40)
	wantDelta = cyclicRefreshQuantizerDeltaForQuantizer(wantQ)
	if state.Quant.BaseQIndex != uint8(wantQ) {
		t.Fatalf("CQ level base q = %d, want %d", state.Quant.BaseQIndex, wantQ)
	}
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != wantDelta {
		t.Fatalf("CQ level preserved alt-q delta = %d, want refreshed %d", got, wantDelta)
	}
}

func TestROIMapDisableClearsRuntimeSegmentationPreserve(t *testing.T) {
	e := newTestEncoder(t)
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	roi := ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: make([]uint8, rows*cols),
	}
	roi.DeltaQuantizer[1] = -10
	for row := range rows {
		for col := range cols {
			if row == 0 || col == 0 || row == rows-1 || col == cols-1 {
				roi.SegmentID[row*cols+col] = 1
			}
		}
	}
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap(border1): %v", err)
	}
	modes := make([]vp8enc.KeyFrameMacroblockMode, rows*cols)
	if !e.assignKeyFrameROISegments(rows, cols, modes) {
		t.Fatalf("assignKeyFrameROISegments failed")
	}
	e.rememberSegmentationConfig(e.roiSegmentationConfig())
	if err := e.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true): %v", err)
	}
	if !e.runtimePreserveSegmentation {
		t.Fatalf("runtimePreserveSegmentation = false, want true after ROI header")
	}
	if err := e.SetROIMap(nil); err != nil {
		t.Fatalf("SetROIMap(nil): %v", err)
	}
	if e.runtimePreserveSegmentation || e.runtimePreservedSegmentation.Enabled || e.segmentationHeaderEnabled {
		t.Fatalf("runtime segmentation preserve after ROI disable = preserve:%t preserved:%t header:%t, want all false",
			e.runtimePreserveSegmentation, e.runtimePreservedSegmentation.Enabled, e.segmentationHeaderEnabled)
	}
}
