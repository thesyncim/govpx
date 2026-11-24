package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

func TestRTCExternalReemitsEncodeTimeVBRSegmentationDelta(t *testing.T) {
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
	vbr, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto VBR inter: %v", err)
	}
	wantDelta := packetState(t, vbr.Data).Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]
	if wantDelta == 0 {
		t.Fatalf("VBR preserved ALT_Q delta = 0, want nonzero")
	}
	if err := e.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true): %v", err)
	}
	if _, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 2), 2, 1, 0); err != nil {
		t.Fatalf("EncodeInto RTC header-only inter: %v", err)
	}
	if err := e.SetMaxIntraBitratePct(0); err != nil {
		t.Fatalf("SetMaxIntraBitratePct: %v", err)
	}
	if err := e.SetGFCBRBoostPct(400); err != nil {
		t.Fatalf("SetGFCBRBoostPct: %v", err)
	}
	if err := e.SetCQLevel(40); err != nil {
		t.Fatalf("SetCQLevel: %v", err)
	}
	reemit, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 3), 3, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto reemit inter: %v", err)
	}
	state := packetState(t, reemit.Data)
	if !state.Segmentation.Enabled || !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("reemit segmentation = %+v, want preserved map/data update", state.Segmentation)
	}
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != wantDelta {
		t.Fatalf("reemit ALT_Q delta = %d, want encode-time VBR delta %d", got, wantDelta)
	}
}

func TestRuntimeExtraConfigKeepsRTCExternalCyclicRefreshDisabled(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()
	dst := make([]byte, 4096)
	if _, err := e.EncodeInto(dst, encoderValidationPanningFrame(16, 16, 0), 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto frame 0: %v", err)
	}
	if !e.segmentationHeaderEnabled {
		t.Fatalf("segmentationHeaderEnabled = false after initial CBR frame")
	}
	if err := e.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true): %v", err)
	}
	if e.cyclicRefreshModeEnabled(false) {
		t.Fatalf("RTC external cyclic refresh enabled, want disabled before later config")
	}
	if err := e.SetGFCBRBoostPct(200); err != nil {
		t.Fatalf("SetGFCBRBoostPct: %v", err)
	}
	if !e.opts.RTCExternalRateControl {
		t.Fatalf("RTCExternalRateControl sticky flag cleared after extra config")
	}
	if e.cyclicRefreshModeEnabled(false) {
		t.Fatalf("cyclic refresh enabled after extra config, want RTC external disable to stay sticky")
	}
	if !e.runtimePreserveSegmentationUpdate {
		t.Fatalf("runtimePreserveSegmentationUpdate = false after extra config, want setup_features-style one-shot segmentation update")
	}
}

func TestRTCExternalFirstInterCodecControlsPreserveCyclicSegmentationUpdate(t *testing.T) {
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
	key, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto key: %v", err)
	}
	keyState := packetState(t, key.Data)
	wantDelta := keyState.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]
	if wantDelta == 0 {
		t.Fatalf("key cyclic alt-q delta = 0, want nonzero")
	}
	if err := e.SetMaxIntraBitratePct(0); err != nil {
		t.Fatalf("SetMaxIntraBitratePct: %v", err)
	}
	if err := e.SetGFCBRBoostPct(0); err != nil {
		t.Fatalf("SetGFCBRBoostPct: %v", err)
	}
	if err := e.SetCQLevel(40); err != nil {
		t.Fatalf("SetCQLevel: %v", err)
	}
	if err := e.SetARNR(0, 0, 1); err != nil {
		t.Fatalf("SetARNR: %v", err)
	}
	if err := e.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true): %v", err)
	}
	inter, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto first inter: %v", err)
	}
	state := packetState(t, inter.Data)
	if !state.Segmentation.Enabled || !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("first inter RTC segmentation = %+v, want preserved map/data update", state.Segmentation)
	}
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != wantDelta {
		t.Fatalf("first inter RTC alt-q delta = %d, want preserved %d", got, wantDelta)
	}
}

func TestRTCExternalFirstInterWithoutPendingUpdatePreservesHeaderOnly(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
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

	dst := make([]byte, 1<<20)
	if _, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(32, 16, 0), 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto key: %v", err)
	}
	if err := e.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true): %v", err)
	}
	inter, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(32, 16, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto first inter: %v", err)
	}
	state := packetState(t, inter.Data)
	if !state.Segmentation.Enabled || state.Segmentation.UpdateMap || state.Segmentation.UpdateData {
		t.Fatalf("first inter RTC-only segmentation = %+v, want enabled header without map/data update", state.Segmentation)
	}
}

func TestRTCExternalFirstInterAfterActiveMapPreservesHeaderOnly(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
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
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	active := make([]uint8, rows*cols)
	for row := range rows {
		for col := range cols {
			if (row+col)&1 == 0 {
				active[row*cols+col] = 1
			}
		}
	}
	if err := e.SetActiveMap(active, rows, cols); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	if e.runtimeSegmentationUpdatePending {
		t.Fatalf("runtimeSegmentationUpdatePending = true after active map, want false")
	}
	if err := e.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true): %v", err)
	}
	inter, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto first inter: %v", err)
	}
	state := packetState(t, inter.Data)
	if !state.Segmentation.Enabled || state.Segmentation.UpdateMap || state.Segmentation.UpdateData {
		t.Fatalf("first inter active+RTC segmentation = %+v, want enabled header without map/data update", state.Segmentation)
	}
}

func TestRTCExternalPreservesPendingCodecControlSegmentationUpdate(t *testing.T) {
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
	key, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto key: %v", err)
	}
	wantDelta := packetState(t, key.Data).Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]
	if _, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 1), 1, 1, 0); err != nil {
		t.Fatalf("EncodeInto first inter: %v", err)
	}
	if err := e.SetMaxIntraBitratePct(300); err != nil {
		t.Fatalf("SetMaxIntraBitratePct: %v", err)
	}
	if err := e.SetGFCBRBoostPct(400); err != nil {
		t.Fatalf("SetGFCBRBoostPct: %v", err)
	}
	if err := e.SetCQLevel(24); err != nil {
		t.Fatalf("SetCQLevel: %v", err)
	}
	if !e.runtimeSegmentationUpdatePending {
		t.Fatalf("runtimeSegmentationUpdatePending = false after codec controls")
	}
	if err := e.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true): %v", err)
	}
	if !e.runtimePreserveSegmentationUpdate {
		t.Fatalf("runtimePreserveSegmentationUpdate = false after RTC consumes pending segmentation update")
	}
	inter, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 2), 2, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto second inter: %v", err)
	}
	state := packetState(t, inter.Data)
	if !state.Segmentation.Enabled || !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("second inter RTC segmentation = %+v, want preserved pending map/data update", state.Segmentation)
	}
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != wantDelta {
		t.Fatalf("second inter RTC alt-q delta = %d, want preserved %d", got, wantDelta)
	}
}

func TestRTCExternalPreservesPriorCyclicSegmentationOnForcedKeyFrame(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   400,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		KeyFrameInterval:    999,
		Deadline:            DeadlineRealtime,
		CpuUsed:             0,
		Tuning:              TunePSNR,
		BufferSizeMs:        200,
		BufferInitialSizeMs: 100,
		BufferOptimalSizeMs: 150,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  50,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	for frame := 0; frame <= 1; frame++ {
		result, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, frame), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", frame, err)
		}
		if frame == 1 {
			state := packetState(t, result.Data)
			if !state.Segmentation.Enabled {
				t.Fatalf("frame 1 segmentation disabled, want cyclic refresh header")
			}
		}
	}
	want := e.lastSegmentationConfig.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]
	if want == 0 {
		t.Fatalf("preserved cyclic alt-q delta = 0, want nonzero")
	}
	if err := e.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true): %v", err)
	}
	for frame := 2; frame < 6; frame++ {
		if frame == 5 {
			if err := e.SetRTCExternalRateControl(false); err != nil {
				t.Fatalf("SetRTCExternalRateControl(false): %v", err)
			}
		}
		if _, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, frame), uint64(frame), 1, 0); err != nil {
			t.Fatalf("EncodeInto frame %d: %v", frame, err)
		}
	}
	forced, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, 6), 6, 1, EncodeForceKeyFrame)
	if err != nil {
		t.Fatalf("forced EncodeInto: %v", err)
	}
	state := packetState(t, forced.Data)
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != want {
		t.Fatalf("forced-key cyclic alt-q delta = %d, want preserved %d", got, want)
	}
}
