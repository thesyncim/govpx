package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9SpatialSVCEncoderLayerRateControl(t *testing.T) {
	cbrLayer := func(width, height, kbps int) VP9EncoderOptions {
		return VP9EncoderOptions{
			Width:               width,
			Height:              height,
			RateControlModeSet:  true,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   kbps,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
		}
	}
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			cbrLayer(32, 32, 300),
			cbrLayer(64, 64, 700),
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	if err := svc.SetLayerBitrateKbps(1, 900); err != nil {
		t.Fatalf("SetLayerBitrateKbps: %v", err)
	}
	if err := svc.SetLayerRateControl(0, RateControlConfig{
		Mode:              RateControlVBR,
		TargetBitrateKbps: 250,
		MinQuantizer:      6,
		MaxQuantizer:      50,
	}); err != nil {
		t.Fatalf("SetLayerRateControl: %v", err)
	}
	if err := svc.SetLayerMaxIntraBitratePct(0, 180); err != nil {
		t.Fatalf("SetLayerMaxIntraBitratePct: %v", err)
	}
	if err := svc.SetLayerMaxInterBitratePct(1, 220); err != nil {
		t.Fatalf("SetLayerMaxInterBitratePct: %v", err)
	}
	if err := svc.SetLayerGFCBRBoostPct(1, 45); err != nil {
		t.Fatalf("SetLayerGFCBRBoostPct: %v", err)
	}
	if err := svc.SetLayerBitrateKbps(2, 100); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerBitrateKbps invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerRateControl(2, RateControlConfig{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRateControl invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerMaxIntraBitratePct(2, 100); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerMaxIntraBitratePct invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerMaxInterBitratePct(1, -1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerMaxInterBitratePct negative err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerGFCBRBoostPct(0, 45); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerGFCBRBoostPct on VBR layer err = %v, want ErrInvalidConfig", err)
	}

	base, err := svc.LayerEncoder(0)
	if err != nil {
		t.Fatalf("LayerEncoder(0): %v", err)
	}
	enh, err := svc.LayerEncoder(1)
	if err != nil {
		t.Fatalf("LayerEncoder(1): %v", err)
	}
	if base.opts.RateControlMode != RateControlVBR ||
		base.opts.TargetBitrateKbps != 250 ||
		base.opts.MinQuantizer != 6 ||
		base.opts.MaxQuantizer != 50 ||
		base.opts.MaxIntraBitratePct != 180 ||
		base.rc.maxIntraBitratePct != 180 ||
		base.opts.GFCBRBoostPct != 0 ||
		base.rc.gfCBRBoostPct != 0 ||
		enh.opts.TargetBitrateKbps != 900 ||
		enh.opts.MaxInterBitratePct != 220 ||
		enh.rc.maxInterBitratePct != 220 ||
		enh.opts.GFCBRBoostPct != 45 ||
		enh.rc.gfCBRBoostPct != 45 {
		t.Fatalf("layer RC opts base=%+v enh=%+v", base.opts, enh.opts)
	}

	dst := make([]byte, 1<<20)
	result, err := svc.EncodeIntoWithResult([]*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 70, 128, 128),
		vp9test.NewYCbCr(64, 64, 90, 128, 128),
	}, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	if result.Layers[0].TargetBitrateKbps != 250 ||
		result.Layers[1].TargetBitrateKbps != 900 {
		t.Fatalf("result layer bitrates = %d/%d, want 250/900",
			result.Layers[0].TargetBitrateKbps,
			result.Layers[1].TargetBitrateKbps)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := svc.SetLayerBitrateKbps(0, 300); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerBitrateKbps after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerRateControl(0, RateControlConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerRateControl after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerMaxIntraBitratePct(0, 300); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerMaxIntraBitratePct after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerMaxInterBitratePct(0, 300); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerMaxInterBitratePct after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerGFCBRBoostPct(0, 10); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerGFCBRBoostPct after close err = %v, want ErrClosed", err)
	}
}
