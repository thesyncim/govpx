package govpx

import (
	"testing"
)

func TestVP9EncoderRuntimeBoostControlsApplyValue(t *testing.T) {
	cases := []struct {
		name string
		mode RateControlMode
		set  func(*VP9Encoder, bool) error
		got  func(*VP9Encoder) (bool, bool)
	}{
		{
			name: "frame periodic boost",
			mode: RateControlCBR,
			set:  (*VP9Encoder).SetFramePeriodicBoost,
			got:  func(e *VP9Encoder) (bool, bool) { return e.opts.FramePeriodicBoost, e.rc.framePeriodicBoost },
		},
		{
			name: "alt ref aq",
			mode: RateControlVBR,
			set:  (*VP9Encoder).SetAltRefAQ,
			got:  func(e *VP9Encoder) (bool, bool) { return e.opts.AltRefAQ, e.rc.altRefAQ },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:              64,
				Height:             64,
				FPS:                30,
				RateControlModeSet: true,
				RateControlMode:    tc.mode,
				TargetBitrateKbps:  600,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			if opt, rc := tc.got(e); opt || rc {
				t.Fatalf("default opts/rc = %v/%v, want false/false", opt, rc)
			}
			if err := tc.set(e, true); err != nil {
				t.Fatalf("set(true): %v", err)
			}
			if opt, rc := tc.got(e); !opt || !rc {
				t.Fatalf("opts/rc = %v/%v, want true/true", opt, rc)
			}
		})
	}
}

func TestVP9FramePeriodicBoostLowersActiveBestOnGFRefresh(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:      true,
		mode:         RateControlCBR,
		bestQuality:  16,
		worstQuality: 200,
	}
	refresh := uint8(1) << vp9GoldenRefSlot
	before := rc.applyVP9RefreshActiveBestBias(120, false, refresh, 16, 200)
	if before != 120 {
		t.Fatalf("no-op bias = %d, want 120", before)
	}
	rc.framePeriodicBoost = true
	boosted := rc.applyVP9RefreshActiveBestBias(120, false, refresh, 16, 200)
	if boosted >= 120 {
		t.Fatalf("FramePeriodicBoost active-best = %d, want < 120", boosted)
	}
}

func TestVP9AltRefAQLeavesActiveBestOnAltRefRefresh(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:      true,
		mode:         RateControlVBR,
		bestQuality:  16,
		worstQuality: 200,
		altRefAQ:     true,
	}
	// Golden-only refresh: AltRefAQ should not bite.
	goldenOnly := uint8(1) << vp9GoldenRefSlot
	goldenBest := rc.applyVP9RefreshActiveBestBias(150, false, goldenOnly, 16,
		200)
	if goldenBest != 150 {
		t.Fatalf("AltRefAQ on golden-only refresh active-best = %d, want 150",
			goldenBest)
	}
	// libvpx v1.16.0 wires VP9E_SET_ALT_REF_AQ but leaves
	// vp9_alt_ref_aq.c stubbed, so the control must not perturb the
	// active-best quantizer.
	altRefRefresh := uint8(1) << vp9AltRefSlot
	altBest := rc.applyVP9RefreshActiveBestBias(150, false, altRefRefresh, 16,
		200)
	if altBest != 150 {
		t.Fatalf("AltRefAQ on alt-ref refresh active-best = %d, want 150",
			altBest)
	}
}

func TestVP9PeriodicBoostClampAtBest(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:            true,
		mode:               RateControlVBR,
		bestQuality:        16,
		worstQuality:       200,
		framePeriodicBoost: true,
	}
	refresh := uint8(1) << vp9AltRefSlot
	got := rc.applyVP9RefreshActiveBestBias(16, false, refresh, 16, 200)
	if got != 16 {
		t.Fatalf("clamped active-best = %d, want 16", got)
	}
}

func TestVP9AltRefAQNoopAtWorst(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:      true,
		mode:         RateControlVBR,
		bestQuality:  16,
		worstQuality: 200,
		altRefAQ:     true,
	}
	refresh := uint8(1) << vp9AltRefSlot
	got := rc.applyVP9RefreshActiveBestBias(200, false, refresh, 16, 200)
	if got != 200 {
		t.Fatalf("clamped active-best = %d, want 200", got)
	}
}

func TestVP9PeriodicBoostNoBiasOnIntraOnly(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:            true,
		mode:               RateControlVBR,
		bestQuality:        16,
		worstQuality:       200,
		framePeriodicBoost: true,
	}
	refresh := uint8(1) << vp9GoldenRefSlot
	got := rc.applyVP9RefreshActiveBestBias(100, true, refresh, 16, 200)
	if got != 100 {
		t.Fatalf("intra-only bias = %d, want 100", got)
	}
}
