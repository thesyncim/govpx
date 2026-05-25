package govpx

import (
	"errors"
	"testing"
)

func TestVP9EncoderSetRealtimeTargetClosed(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 1200}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRealtimeTarget after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetDeadline(DeadlineRealtime); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetDeadline after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetCPUUsed(8); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetCPUUsed after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetBitrateKbps(900); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetBitrateKbps after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetCQLevel(20); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetCQLevel after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetAQMode(VP9AQNone); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetAQMode after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetLossless(true); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLossless after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetRateControl(RateControlConfig{Mode: RateControlVBR, TargetBitrateKbps: 900}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRateControl after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetActiveMap([]uint8{1}, 1, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetActiveMap after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetROIMap(&ROIMap{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetROIMap after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetRateControlBuffer(200, 100, 150); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRateControlBuffer after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetKeyFrameInterval(2); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetKeyFrameInterval after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetKeyFrameIntervalRange(1, 2); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetKeyFrameIntervalRange after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetARNR(5, 6, 3); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetARNR after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetScreenContentMode(1); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetScreenContentMode after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetSharpness(3); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSharpness after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetStaticThreshold(1); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetStaticThreshold after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetTemporalScalability after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetTemporalLayerID(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetTemporalLayerID after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetSpatialScalability(VP9SpatialScalabilityConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSpatialScalability after Close err = %v, want ErrClosed", err)
	}
	if err := e.SetSpatialLayerID(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSpatialLayerID after Close err = %v, want ErrClosed", err)
	}
	var nilEnc *VP9Encoder
	if err := nilEnc.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 1200}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRealtimeTarget on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetDeadline(DeadlineRealtime); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetDeadline on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetCPUUsed(8); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetCPUUsed on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetBitrateKbps(900); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetBitrateKbps on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetCQLevel(20); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetCQLevel on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetAQMode(VP9AQNone); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetAQMode on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetLossless(true); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLossless on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetRateControl(RateControlConfig{Mode: RateControlVBR, TargetBitrateKbps: 900}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRateControl on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetActiveMap([]uint8{1}, 1, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetActiveMap on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetROIMap(&ROIMap{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetROIMap on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetRateControlBuffer(200, 100, 150); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetRateControlBuffer on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetKeyFrameInterval(2); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetKeyFrameInterval on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetKeyFrameIntervalRange(1, 2); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetKeyFrameIntervalRange on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetARNR(5, 6, 3); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetARNR on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetScreenContentMode(1); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetScreenContentMode on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetSharpness(3); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSharpness on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetStaticThreshold(1); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetStaticThreshold on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetTemporalScalability(TemporalScalabilityConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetTemporalScalability on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetTemporalLayerID(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetTemporalLayerID on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetSpatialScalability(VP9SpatialScalabilityConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSpatialScalability on nil encoder err = %v, want ErrClosed", err)
	}
	if err := nilEnc.SetSpatialLayerID(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetSpatialLayerID on nil encoder err = %v, want ErrClosed", err)
	}
}
