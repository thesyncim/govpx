package govpx

import (
	"testing"
)

func TestVP9GFIntervalBoundsClampBaselineGFInterval(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		MinGFInterval:      6,
		MaxGFInterval:      8,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if int(e.rc.minGFInterval) != 6 || int(e.rc.maxGFInterval) != 8 {
		t.Fatalf("min/max = %d/%d, want 6/8",
			e.rc.minGFInterval, e.rc.maxGFInterval)
	}
	// Average of [6,8] is 7. baselineGFInterval should be clamped into
	// the configured window.
	if int(e.rc.baselineGFInterval) < 6 || int(e.rc.baselineGFInterval) > 8 {
		t.Fatalf("baselineGFInterval = %d, want within [6,8]",
			e.rc.baselineGFInterval)
	}
}

func TestVP9SetMinGFIntervalAppliesValue(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetMinGFInterval(5); err != nil {
		t.Fatalf("SetMinGFInterval: %v", err)
	}
	if e.opts.MinGFInterval != 5 || int(e.rc.minGFInterval) != 5 {
		t.Fatalf("opts=%d rc=%d, want both 5",
			e.opts.MinGFInterval, e.rc.minGFInterval)
	}
	if int(e.rc.baselineGFInterval) < 5 {
		t.Fatalf("baselineGFInterval = %d, want >= 5",
			e.rc.baselineGFInterval)
	}
}

func TestVP9SetMaxGFIntervalAppliesValue(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetMaxGFInterval(10); err != nil {
		t.Fatalf("SetMaxGFInterval: %v", err)
	}
	if e.opts.MaxGFInterval != 10 || int(e.rc.maxGFInterval) != 10 {
		t.Fatalf("opts=%d rc=%d, want both 10",
			e.opts.MaxGFInterval, e.rc.maxGFInterval)
	}
	if int(e.rc.baselineGFInterval) > 10 {
		t.Fatalf("baselineGFInterval = %d, want <= 10",
			e.rc.baselineGFInterval)
	}
}

func TestVP9GFIntervalClampsRuntimeOnePassVBRGoldenInterval(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:            true,
		mode:               RateControlVBR,
		baselineGFInterval: 12,
		minGFInterval:      0,
		maxGFInterval:      8,
	}
	got := rc.runtimeOnePassVBRGoldenInterval()
	if got != 8 {
		t.Fatalf("runtimeOnePassVBRGoldenInterval = %d, want 8", got)
	}
	rc.baselineGFInterval = 3
	rc.minGFInterval = 5
	rc.maxGFInterval = 0
	if got := rc.runtimeOnePassVBRGoldenInterval(); got != 5 {
		t.Fatalf("runtimeOnePassVBRGoldenInterval (clamp low) = %d, want 5",
			got)
	}
}
