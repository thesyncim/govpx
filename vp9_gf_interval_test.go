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

func TestVP9SetMaxGFIntervalAcceptsLibvpxUpperBound(t *testing.T) {
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
	if err := e.SetMaxGFInterval(vp9MaxGFIntervalControl); err != nil {
		t.Fatalf("SetMaxGFInterval(%d): %v", vp9MaxGFIntervalControl, err)
	}
	if e.opts.MaxGFInterval != vp9MaxGFIntervalControl ||
		int(e.rc.maxGFInterval) != vp9MaxGFIntervalControl {
		t.Fatalf("opts=%d rc=%d, want both %d", e.opts.MaxGFInterval,
			e.rc.maxGFInterval, vp9MaxGFIntervalControl)
	}
}

func TestVP9GFIntervalClampsRuntimeOnePassVBRGoldenInterval(t *testing.T) {
	// libvpx vp9_set_gf_update_one_pass_vbr (vp9/encoder/vp9_ratectrl.c:2086):
	//   baseline_gf_interval =
	//     VPXMIN(20, VPXMAX(10, (min_gf_interval + max_gf_interval) / 2));
	// The interval is recomputed from min/max each cycle and clamped into the
	// fixed [10, 20] window — not into [min_gf_interval, max_gf_interval], and
	// the stored baseline is not read back.
	rc := &vp9RateControlState{
		enabled:       true,
		mode:          RateControlVBR,
		minGFInterval: 4,
		maxGFInterval: 16,
	}
	// (4 + 16) / 2 = 10 -> VPXMAX(10, 10) = 10 -> VPXMIN(20, 10) = 10.
	if got := rc.runtimeOnePassVBRGoldenInterval(); got != 10 {
		t.Fatalf("runtimeOnePassVBRGoldenInterval = %d, want 10", got)
	}
	// Midpoint below 10 clamps up to the lower bound 10.
	rc.minGFInterval = 4
	rc.maxGFInterval = 8
	if got := rc.runtimeOnePassVBRGoldenInterval(); got != 10 {
		t.Fatalf("runtimeOnePassVBRGoldenInterval (clamp low) = %d, want 10",
			got)
	}
	// Midpoint above 20 clamps down to the upper bound 20.
	rc.minGFInterval = 30
	rc.maxGFInterval = 30
	if got := rc.runtimeOnePassVBRGoldenInterval(); got != 20 {
		t.Fatalf("runtimeOnePassVBRGoldenInterval (clamp high) = %d, want 20",
			got)
	}
}

// TestVP9OnePassQGoldenIntervalIsTen pins the one-pass VPX_Q golden cadence to
// libvpx's 10-frame interval. libvpx vp9_rc_init (vp9/encoder/vp9_ratectrl.c:445)
// seeds baseline_gf_interval = (min_gf_interval + max_gf_interval)/2 for every
// rc_mode; the VPX_Q-only FIXED_GF_INTERVAL=8 assignment at :446-447 targets
// static_scene_max_gf_interval, not baseline_gf_interval. For 64x64@30fps
// min_gf=4, max_gf=16, so the realtime golden refresh fires at current_video_frame
// 10 — matching the VBR/CBR cadence — not at 8. Regression guard for the
// runtime-control cpu=8 NoReference accumulation lane, whose late-frame
// refresh_frame_flags (govpx 0x3 vs libvpx 0x1 at frame 8) traced back to a
// stale FixedGFInterval=8 short-circuit in the VPX_Q baseline_gf_interval seed.
func TestVP9OnePassQGoldenIntervalIsTen(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlQ,
		TargetBitrateKbps:  700,
		CQLevel:            32,
		CpuUsed:            -8,
		Deadline:           DeadlineRealtime,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if got := e.rc.baselineGFInterval; got != 10 {
		t.Fatalf("Q-mode baselineGFInterval = %d, want 10", got)
	}
	if got := e.rc.runtimeOnePassVBRGoldenInterval(); got != 10 {
		t.Fatalf("Q-mode runtimeOnePassVBRGoldenInterval = %d, want 10", got)
	}
}
