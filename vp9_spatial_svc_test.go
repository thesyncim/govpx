package govpx

import (
	"bytes"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9SpatialSVCEncoderEncodesInterLayerSuperframe(t *testing.T) {
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: 32, Height: 32, Lossless: true},
			{Width: 64, Height: 64, Lossless: true},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	srcs := []*image.YCbCr{
		newVP9YCbCrForTest(32, 32, 90, 100, 110),
		newVP9YCbCrForTest(64, 64, 90, 100, 110),
	}
	dst := make([]byte, 1<<20)
	result, err := svc.EncodeIntoWithResult(srcs, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	if result.LayerCount != 2 || result.SizeBytes != len(result.Data) ||
		!result.InterLayerPrediction {
		t.Fatalf("SVC result = %+v", result)
	}
	sf, err := vp9ParseSuperframe(result.Data)
	if err != nil {
		t.Fatalf("vp9ParseSuperframe: %v", err)
	}
	if sf.count != 2 {
		t.Fatalf("superframe count = %d, want 2", sf.count)
	}
	if !bytes.Equal(sf.frames[0], result.Layers[0].Data) ||
		!bytes.Equal(sf.frames[1], result.Layers[1].Data) {
		t.Fatal("layer result payloads do not match superframe payloads")
	}
	if !result.Layers[0].KeyFrame ||
		result.Layers[0].SpatialLayerID != 0 ||
		result.Layers[0].SpatialLayerCount != 2 ||
		result.Layers[0].NotRefForUpperSpatialLayer ||
		!result.Layers[0].ScalabilityStructurePresent {
		t.Fatalf("base layer result = %+v", result.Layers[0])
	}
	if result.Layers[1].KeyFrame ||
		!result.Layers[1].ShowFrame ||
		result.Layers[1].SpatialLayerID != 1 ||
		result.Layers[1].SpatialLayerCount != 2 ||
		!result.Layers[1].InterLayerDependency ||
		!result.Layers[1].NotRefForUpperSpatialLayer ||
		result.Layers[1].ScalabilityStructurePresent {
		t.Fatalf("enhancement layer result = %+v", result.Layers[1])
	}

	baseDesc := result.Layers[0].RTPPayloadDescriptor()
	if !baseDesc.ScalabilityStructurePresent ||
		baseDesc.ScalabilityStructure.SpatialLayerCount != 2 ||
		baseDesc.ScalabilityStructure.Width[0] != 32 ||
		baseDesc.ScalabilityStructure.Width[1] != 64 {
		t.Fatalf("base RTP descriptor = %+v", baseDesc)
	}
	enhDesc := result.Layers[1].RTPPayloadDescriptor()
	if !enhDesc.LayerIndicesPresent || enhDesc.SpatialID != 1 ||
		!enhDesc.InterLayerDependency || enhDesc.ScalabilityStructurePresent {
		t.Fatalf("enhancement RTP descriptor = %+v", enhDesc)
	}

	var br vp9dec.BitReader
	br.Init(sf.frames[1])
	upperHeader, err := vp9dec.ReadUncompressedHeader(&br, nil,
		func(uint8) (uint32, uint32) {
			return 32, 32
		})
	if err != nil {
		t.Fatalf("ReadUncompressedHeader enhancement: %v", err)
	}
	if upperHeader.FrameType != common.InterFrame || !upperHeader.ShowFrame ||
		upperHeader.Width != 64 || upperHeader.Height != 64 {
		t.Fatalf("enhancement header = %+v, want visible 64x64 inter frame", upperHeader)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(result.Data); err != nil {
		t.Fatalf("Decode SVC superframe: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no frame")
	}
	assertVP9FilledFrameWithin(t, frame, 64, 64, 90, 100, 110, 0)
}

func TestVP9SpatialSVCEncoderIndependentLayers(t *testing.T) {
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
	dst := make([]byte, 1<<20)
	result, err := svc.EncodeIntoWithResult([]*image.YCbCr{
		newVP9YCbCrForTest(32, 32, 20, 128, 128),
		newVP9YCbCrForTest(64, 64, 40, 128, 128),
	}, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	if !result.Layers[0].KeyFrame || !result.Layers[1].KeyFrame {
		t.Fatalf("independent first access unit key flags = %v/%v, want both key",
			result.Layers[0].KeyFrame, result.Layers[1].KeyFrame)
	}
	if !result.Layers[0].NotRefForUpperSpatialLayer ||
		result.Layers[1].InterLayerDependency {
		t.Fatalf("independent spatial metadata = base:%+v enh:%+v",
			result.Layers[0], result.Layers[1])
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
		newVP9YCbCrForTest(32, 32, 80, 128, 128),
		newVP9YCbCrForTest(64, 64, 80, 128, 128),
	}
	dst := make([]byte, 1<<20)
	for frame := 0; frame < 4; frame++ {
		result, err := svc.EncodeIntoWithResult(srcs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		base := result.Layers[0]
		enh := result.Layers[1]
		if base.TemporalLayerCount != 2 || enh.TemporalLayerCount != 2 ||
			base.TemporalLayerID != enh.TemporalLayerID ||
			base.TL0PICIDX != enh.TL0PICIDX ||
			base.TemporalLayerSync != enh.TemporalLayerSync {
			t.Fatalf("temporal metadata mismatch frame %d: base=%+v enh=%+v",
				frame, base, enh)
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
}

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
	if err := svc.SetLayerBitrateKbps(2, 100); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerBitrateKbps invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerRateControl(2, RateControlConfig{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRateControl invalid layer err = %v, want ErrInvalidConfig", err)
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
		enh.opts.TargetBitrateKbps != 900 {
		t.Fatalf("layer RC opts base=%+v enh=%+v", base.opts, enh.opts)
	}

	dst := make([]byte, 1<<20)
	result, err := svc.EncodeIntoWithResult([]*image.YCbCr{
		newVP9YCbCrForTest(32, 32, 70, 128, 128),
		newVP9YCbCrForTest(64, 64, 90, 128, 128),
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
}

func TestVP9SpatialSVCEncoderSteadyStateNoAlloc(t *testing.T) {
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: 32, Height: 32},
			{Width: 64, Height: 64},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	srcs := []*image.YCbCr{
		newVP9YCbCrForTest(32, 32, 80, 128, 128),
		newVP9YCbCrForTest(64, 64, 80, 128, 128),
	}
	dst := make([]byte, 1<<20)
	for i := 0; i < 3; i++ {
		if _, err := svc.EncodeIntoWithResult(srcs, dst); err != nil {
			t.Fatalf("warmup EncodeIntoWithResult %d: %v", i, err)
		}
	}
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		if _, err := svc.EncodeIntoWithResult(srcs, dst); err != nil {
			t.Fatalf("alloc run EncodeIntoWithResult: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("VP9 spatial SVC steady state allocs = %f, want 0", allocs)
	}
}
