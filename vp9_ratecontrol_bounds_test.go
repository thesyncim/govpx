package govpx

import (
	"errors"
	"testing"
)

func TestVP9EncoderRejectsInvalidRateControlBounds(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*VP9EncoderOptions)
		err  error
	}{
		{"min>max", func(o *VP9EncoderOptions) {
			o.MinBitrateKbps = 1500
			o.MaxBitrateKbps = 800
		}, ErrInvalidBitrate},
		{"target<min", func(o *VP9EncoderOptions) {
			o.MinBitrateKbps = 2000
		}, ErrInvalidBitrate},
		{"target>max", func(o *VP9EncoderOptions) {
			o.MaxBitrateKbps = 200
		}, ErrInvalidBitrate},
		{"negative min", func(o *VP9EncoderOptions) {
			o.MinBitrateKbps = -1
		}, ErrInvalidBitrate},
		{"undershoot>100", func(o *VP9EncoderOptions) {
			o.UndershootPct = 200
		}, ErrInvalidConfig},
		{"overshoot>100", func(o *VP9EncoderOptions) {
			o.OvershootPct = 200
		}, ErrInvalidConfig},
		{"negative max-intra", func(o *VP9EncoderOptions) {
			o.MaxIntraBitratePct = -1
		}, ErrInvalidConfig},
		{"non-cbr gfboost", func(o *VP9EncoderOptions) {
			o.RateControlMode = RateControlVBR
			o.GFCBRBoostPct = 20
		}, ErrInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := VP9EncoderOptions{
				Width:              64,
				Height:             64,
				FPS:                30,
				RateControlModeSet: true,
				RateControlMode:    RateControlCBR,
				TargetBitrateKbps:  1000,
			}
			tc.mut(&opts)
			if _, err := NewVP9Encoder(opts); !errors.Is(err, tc.err) {
				t.Fatalf("NewVP9Encoder err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestVP9EncoderRateControlBoundsAreStored(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1000,
		MinBitrateKbps:     500,
		MaxBitrateKbps:     1500,
		UndershootPct:      80,
		OvershootPct:       60,
		MaxIntraBitratePct: 200,
		GFCBRBoostPct:      30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.rc.minBitrateKbps != 500 || e.rc.maxBitrateKbps != 1500 {
		t.Fatalf("bitrate bounds = (%d,%d), want (500,1500)",
			e.rc.minBitrateKbps, e.rc.maxBitrateKbps)
	}
	if e.rc.undershootPct != 80 || e.rc.overshootPct != 60 {
		t.Fatalf("under/overshoot = (%d,%d), want (80,60)",
			e.rc.undershootPct, e.rc.overshootPct)
	}
	if e.rc.maxIntraBitratePct != 200 || e.rc.gfCBRBoostPct != 30 {
		t.Fatalf("max-intra/gfboost = (%d,%d), want (200,30)",
			e.rc.maxIntraBitratePct, e.rc.gfCBRBoostPct)
	}
}

func TestVP9EncoderDefaultUndershootOvershootMatchLibvpx(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1000,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.rc.undershootPct != defaultRateControlUndershootPct {
		t.Fatalf("default undershoot = %d, want %d",
			e.rc.undershootPct, defaultRateControlUndershootPct)
	}
	if e.rc.overshootPct != defaultRateControlOvershootPct {
		t.Fatalf("default overshoot = %d, want %d",
			e.rc.overshootPct, defaultRateControlOvershootPct)
	}
}

func TestVP9RateControlClampBitrateKbps(t *testing.T) {
	rc := &vp9RateControlState{
		minBitrateKbps: 500,
		maxBitrateKbps: 1500,
	}
	cases := []struct{ in, want int }{
		{100, 500},
		{500, 500},
		{1000, 1000},
		{1500, 1500},
		{2000, 1500},
	}
	for _, tc := range cases {
		if got := rc.clampBitrateKbps(tc.in); got != tc.want {
			t.Fatalf("clampBitrateKbps(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
	rc.minBitrateKbps = 0
	if got := rc.clampBitrateKbps(100); got != 100 {
		t.Fatalf("clampBitrateKbps disables min when zero, got %d", got)
	}
	rc.maxBitrateKbps = 0
	if got := rc.clampBitrateKbps(99999); got != 99999 {
		t.Fatalf("clampBitrateKbps disables max when zero, got %d", got)
	}
}

func TestVP9SetBitrateKbpsClampsToBounds(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  1000,
		MinBitrateKbps:     800,
		MaxBitrateKbps:     1200,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetBitrateKbps(200); err != nil {
		t.Fatalf("SetBitrateKbps 200: %v", err)
	}
	if e.rc.targetBitrateKbps != 800 {
		t.Fatalf("clamped target = %d, want 800 (min)", e.rc.targetBitrateKbps)
	}
	if err := e.SetBitrateKbps(5000); err != nil {
		t.Fatalf("SetBitrateKbps 5000: %v", err)
	}
	if e.rc.targetBitrateKbps != 1200 {
		t.Fatalf("clamped target = %d, want 1200 (max)", e.rc.targetBitrateKbps)
	}
	if err := e.SetBitrateKbps(1000); err != nil {
		t.Fatalf("SetBitrateKbps 1000: %v", err)
	}
	if e.rc.targetBitrateKbps != 1000 {
		t.Fatalf("unclamped target = %d, want 1000", e.rc.targetBitrateKbps)
	}
}

func TestVP9OvershootCeilGrowsWithPct(t *testing.T) {
	cases := []struct {
		bpf, pct, want int
	}{
		{1000, 0, 0},
		{1000, 50, 1500},
		{1000, 100, 2000},
		{0, 50, 0},
	}
	for _, tc := range cases {
		if got := vp9OvershootCeil(tc.bpf, tc.pct); got != tc.want {
			t.Fatalf("vp9OvershootCeil(%d,%d) = %d, want %d",
				tc.bpf, tc.pct, got, tc.want)
		}
	}
}

func TestVP9ApplyVP9MaxIntraBoundCapsTarget(t *testing.T) {
	rc := &vp9RateControlState{
		bitsPerFrame:       1000,
		maxIntraBitratePct: 200,
	}
	if got := rc.applyVP9MaxIntraBound(5000); got != 2000 {
		t.Fatalf("max-intra bound = %d, want 2000", got)
	}
	if got := rc.applyVP9MaxIntraBound(1500); got != 1500 {
		t.Fatalf("under-cap target = %d, want 1500", got)
	}
	rc.maxIntraBitratePct = 0
	if got := rc.applyVP9MaxIntraBound(99999); got != 99999 {
		t.Fatalf("zero cap disabled, got %d", got)
	}
}

func TestVP9ApplyVP9GFCBRBoostAddsTarget(t *testing.T) {
	rc := &vp9RateControlState{
		mode:          RateControlCBR,
		bitsPerFrame:  1000,
		gfCBRBoostPct: 40,
	}
	if got := rc.applyVP9GFCBRBoost(800); got != 1200 {
		t.Fatalf("gfboost = %d, want 1200", got)
	}
	rc.mode = RateControlVBR
	if got := rc.applyVP9GFCBRBoost(800); got != 800 {
		t.Fatalf("non-CBR gfboost = %d, want 800", got)
	}
}
