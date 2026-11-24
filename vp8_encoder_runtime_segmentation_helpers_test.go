package govpx

import (
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

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
