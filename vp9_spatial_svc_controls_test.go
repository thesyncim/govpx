package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9SpatialSVCEncoderSetInterLayerPrediction(t *testing.T) {
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: 32, Height: 32, Lossless: true},
			{Width: 64, Height: 64, Lossless: true},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	srcs := []*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 20, 128, 128),
		vp9test.NewYCbCr(64, 64, 40, 128, 128),
	}
	dst := make([]byte, 1<<20)
	result, err := svc.EncodeIntoWithResult(srcs, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult independent: %v", err)
	}
	if result.InterLayerPrediction ||
		!result.Layers[0].NotRefForUpperSpatialLayer ||
		result.Layers[1].InterLayerDependency {
		t.Fatalf("initial independent SVC metadata = base:%+v enh:%+v result:%+v",
			result.Layers[0], result.Layers[1], result)
	}

	if err := svc.SetInterLayerPrediction(true); err != nil {
		t.Fatalf("SetInterLayerPrediction(true): %v", err)
	}
	result, err = svc.EncodeIntoWithResult(srcs, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult inter-layer: %v", err)
	}
	if !result.InterLayerPrediction ||
		result.Layers[0].NotRefForUpperSpatialLayer ||
		!result.Layers[1].InterLayerDependency ||
		!result.Layers[1].NotRefForUpperSpatialLayer {
		t.Fatalf("enabled inter-layer SVC metadata = base:%+v enh:%+v result:%+v",
			result.Layers[0], result.Layers[1], result)
	}

	if err := svc.SetInterLayerPrediction(false); err != nil {
		t.Fatalf("SetInterLayerPrediction(false): %v", err)
	}
	result, err = svc.EncodeIntoWithResult(srcs, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult disabled: %v", err)
	}
	if result.InterLayerPrediction ||
		!result.Layers[0].NotRefForUpperSpatialLayer ||
		result.Layers[1].InterLayerDependency {
		t.Fatalf("disabled inter-layer SVC metadata = base:%+v enh:%+v result:%+v",
			result.Layers[0], result.Layers[1], result)
	}
}

func TestVP9SpatialSVCEncoderValidationAndLayerControls(t *testing.T) {
	base := VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: 32, Height: 32},
			{Width: 64, Height: 64},
		},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*VP9SpatialSVCEncoderOptions)
	}{
		{name: "zero layers", mutate: func(o *VP9SpatialSVCEncoderOptions) { o.LayerCount = 0 }},
		{name: "one layer", mutate: func(o *VP9SpatialSVCEncoderOptions) { o.LayerCount = 1 }},
		{name: "too many layers", mutate: func(o *VP9SpatialSVCEncoderOptions) {
			o.LayerCount = VP9MaxSpatialLayers + 1
		}},
		{name: "preset spatial config", mutate: func(o *VP9SpatialSVCEncoderOptions) {
			o.Layers[0].SpatialScalability.Enabled = true
		}},
		{name: "lookahead", mutate: func(o *VP9SpatialSVCEncoderOptions) {
			o.Layers[0].LookaheadFrames = 2
		}},
		{name: "drop frames", mutate: func(o *VP9SpatialSVCEncoderOptions) {
			o.Layers[0].DropFrameAllowed = true
		}},
		{name: "non increasing", mutate: func(o *VP9SpatialSVCEncoderOptions) {
			o.Layers[1].Width = 32
			o.Layers[1].Height = 32
		}},
		{name: "invalid inter-layer scale", mutate: func(o *VP9SpatialSVCEncoderOptions) {
			o.InterLayerPrediction = true
			o.Layers[1].Width = 544
			o.Layers[1].Height = 544
		}},
		{name: "temporal enabled on one layer", mutate: func(o *VP9SpatialSVCEncoderOptions) {
			o.Layers[1].TemporalScalability = TemporalScalabilityConfig{
				Enabled: true,
				Mode:    TemporalLayeringTwoLayers,
			}
		}},
		{name: "temporal mode mismatch", mutate: func(o *VP9SpatialSVCEncoderOptions) {
			o.Layers[0].TemporalScalability = TemporalScalabilityConfig{
				Enabled: true,
				Mode:    TemporalLayeringTwoLayers,
			}
			o.Layers[1].TemporalScalability = TemporalScalabilityConfig{
				Enabled: true,
				Mode:    TemporalLayeringThreeLayers,
			}
		}},
	} {
		opts := base
		tc.mutate(&opts)
		if _, err := NewVP9SpatialSVCEncoder(opts); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("%s error = %v, want ErrInvalidConfig", tc.name, err)
		}
	}

	svc, err := NewVP9SpatialSVCEncoder(base)
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	if _, err := svc.LayerEncoder(2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("LayerEncoder invalid err = %v, want ErrInvalidConfig", err)
	}
	layer, err := svc.LayerEncoder(1)
	if err != nil {
		t.Fatalf("LayerEncoder(1): %v", err)
	}
	if err := layer.SetSharpness(5); err != nil {
		t.Fatalf("layer SetSharpness: %v", err)
	}
	if err := layer.SetSpatialLayerID(0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("locked layer SetSpatialLayerID err = %v, want ErrInvalidConfig", err)
	}
	if err := layer.SetSpatialScalability(VP9SpatialScalabilityConfig{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("locked layer SetSpatialScalability err = %v, want ErrInvalidConfig", err)
	}
	if err := layer.SetTemporalScalability(TemporalScalabilityConfig{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("locked layer SetTemporalScalability err = %v, want ErrInvalidConfig", err)
	}
	if err := layer.SetTemporalLayerID(0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("locked layer SetTemporalLayerID err = %v, want ErrInvalidConfig", err)
	}
	invalidScale, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: 32, Height: 32},
			{Width: 544, Height: 544},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder invalid-scale-independent: %v", err)
	}
	if err := invalidScale.SetInterLayerPrediction(true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetInterLayerPrediction invalid scale err = %v, want ErrInvalidConfig", err)
	}
	if err := invalidScale.Close(); err != nil {
		t.Fatalf("invalidScale Close: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := svc.LayerEncoder(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("LayerEncoder after close err = %v, want ErrClosed", err)
	}
	if _, err := svc.EncodeIntoWithResult(nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("EncodeIntoWithResult after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetTemporalScalability(TemporalScalabilityConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetTemporalScalability after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetTemporalLayerID(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetTemporalLayerID after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetInterLayerPrediction(true); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetInterLayerPrediction after close err = %v, want ErrClosed", err)
	}
	var nilSVC *VP9SpatialSVCEncoder
	if err := nilSVC.SetInterLayerPrediction(true); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetInterLayerPrediction on nil err = %v, want ErrClosed", err)
	}
}

func TestVP9SpatialSVCEncoderTemporalControls(t *testing.T) {
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: 32, Height: 32, TargetBitrateKbps: 300},
			{Width: 64, Height: 64, TargetBitrateKbps: 700},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	if err := svc.SetTemporalScalability(TemporalScalabilityConfig{
		Enabled: true,
		Mode:    TemporalLayeringTwoLayers,
	}); err != nil {
		t.Fatalf("SetTemporalScalability: %v", err)
	}
	if err := svc.SetTemporalLayerID(2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTemporalLayerID invalid err = %v, want ErrInvalidConfig", err)
	}

	srcs := []*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 80, 128, 128),
		vp9test.NewYCbCr(64, 64, 80, 128, 128),
	}
	dst := make([]byte, 1<<20)
	wantBaseRefresh := []uint8{0xff, 0x04, 0x01, 0x04}
	wantEnhRefresh := []uint8{0x02, 0x00, 0x02, 0x00}
	wantBaseSync := []bool{false, true, false, true}
	wantEnhSync := []bool{false, true, false, true}
	for frame := range 4 {
		result, err := svc.EncodeIntoWithResult(srcs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		base := result.Layers[0]
		enh := result.Layers[1]
		if frame == 0 && (!base.KeyFrame || enh.KeyFrame ||
			enh.RefreshFrameFlags == 0xff) {
			t.Fatalf("first temporal SVC access unit key/refresh = base:%t enh:%t enh_refresh:%#x, want base key and enhancement inter",
				base.KeyFrame, enh.KeyFrame, enh.RefreshFrameFlags)
		}
		if base.RefreshFrameFlags != wantBaseRefresh[frame] ||
			enh.RefreshFrameFlags != wantEnhRefresh[frame] {
			t.Fatalf("temporal SVC refresh frame %d = base:%#x enh:%#x, want %#x/%#x",
				frame, base.RefreshFrameFlags, enh.RefreshFrameFlags,
				wantBaseRefresh[frame], wantEnhRefresh[frame])
		}
		if base.TemporalLayerCount != 2 || enh.TemporalLayerCount != 2 ||
			base.TemporalLayerID != enh.TemporalLayerID ||
			base.TL0PICIDX != enh.TL0PICIDX {
			t.Fatalf("temporal metadata mismatch frame %d: base=%+v enh=%+v",
				frame, base, enh)
		}
		if base.TemporalLayerSync != wantBaseSync[frame] ||
			enh.TemporalLayerSync != wantEnhSync[frame] {
			t.Fatalf("temporal sync frame %d = base:%t enh:%t, want %t/%t",
				frame, base.TemporalLayerSync, enh.TemporalLayerSync,
				wantBaseSync[frame], wantEnhSync[frame])
		}
		baseDesc := base.RTPPayloadDescriptor()
		enhDesc := enh.RTPPayloadDescriptor()
		if !baseDesc.LayerIndicesPresent ||
			int(baseDesc.TemporalID) != base.TemporalLayerID ||
			baseDesc.SpatialID != 0 ||
			!enhDesc.LayerIndicesPresent ||
			int(enhDesc.TemporalID) != enh.TemporalLayerID ||
			enhDesc.SpatialID != 1 {
			t.Fatalf("temporal RTP descriptors frame %d: base=%+v enh=%+v",
				frame, baseDesc, enhDesc)
		}
		if !baseDesc.ScalabilityStructurePresent ||
			!baseDesc.ScalabilityStructure.PictureGroupPresent ||
			len(baseDesc.ScalabilityStructure.PictureGroups) != 2 {
			t.Fatalf("temporal scalability structure frame %d = %+v",
				frame, baseDesc.ScalabilityStructure)
		}
		wantGroups := [2]VP9RTPPictureGroup{
			{
				TemporalID:          0,
				ReferenceIndexCount: 1,
				ReferenceIndices:    [VP9RTPMaxReferenceIndices]uint8{2},
			},
			{
				TemporalID:          1,
				ReferenceIndexCount: 2,
				ReferenceIndices:    [VP9RTPMaxReferenceIndices]uint8{1, 2},
			},
		}
		for i, want := range wantGroups {
			if got := baseDesc.ScalabilityStructure.PictureGroups[i]; got != want {
				t.Fatalf("temporal picture group %d = %+v, want %+v", i, got, want)
			}
		}
	}
	if err := svc.SetTemporalLayerID(1); err != nil {
		t.Fatalf("SetTemporalLayerID(1): %v", err)
	}
	result, err := svc.EncodeIntoWithResult(srcs, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult override: %v", err)
	}
	if result.Layers[0].TemporalLayerID != 1 ||
		result.Layers[1].TemporalLayerID != 1 {
		t.Fatalf("override temporal IDs = %d/%d, want 1/1",
			result.Layers[0].TemporalLayerID, result.Layers[1].TemporalLayerID)
	}
	if desc := result.Layers[0].RTPPayloadDescriptor(); !desc.ScalabilityStructurePresent ||
		desc.ScalabilityStructure.PictureGroupPresent {
		t.Fatalf("override scalability structure = %+v, want resolution-only", desc.ScalabilityStructure)
	}
}

func TestVP9SpatialSVCEncoderInitialTemporalOptions(t *testing.T) {
	temporal := TemporalScalabilityConfig{
		Enabled: true,
		Mode:    TemporalLayeringTwoLayers,
	}
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{
				Width:               32,
				Height:              32,
				TargetBitrateKbps:   300,
				TemporalScalability: temporal,
			},
			{
				Width:               64,
				Height:              64,
				TargetBitrateKbps:   700,
				TemporalScalability: temporal,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	dst := make([]byte, 1<<20)
	result, err := svc.EncodeIntoWithResult([]*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 80, 128, 128),
		vp9test.NewYCbCr(64, 64, 80, 128, 128),
	}, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	if result.Layers[0].TemporalLayerCount != 2 ||
		result.Layers[1].TemporalLayerCount != 2 ||
		!result.ScalabilityStructure.PictureGroupPresent ||
		len(result.ScalabilityStructure.PictureGroups) != 2 {
		t.Fatalf("initial temporal SVC result = base:%+v enh:%+v ss:%+v",
			result.Layers[0], result.Layers[1],
			result.ScalabilityStructure)
	}
}
