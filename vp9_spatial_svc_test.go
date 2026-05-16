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
		newVP9YCbCrForTest(32, 32, 80, 128, 128),
		newVP9YCbCrForTest(64, 64, 80, 128, 128),
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

func TestVP9SpatialSVCEncoderThreeLayerInterLayerMultiFrame(t *testing.T) {
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           3,
		InterLayerPrediction: true,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: 32, Height: 32, Lossless: true},
			{Width: 64, Height: 64, Lossless: true},
			{Width: 128, Height: 128, Lossless: true},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	dst := make([]byte, 1<<21)
	widths := [3]int{32, 64, 128}
	heights := [3]int{32, 64, 128}
	for frame := 0; frame < 3; frame++ {
		y := uint8(60 + frame*11)
		srcs := []*image.YCbCr{
			newVP9YCbCrForTest(32, 32, y, 120, 136),
			newVP9YCbCrForTest(64, 64, y, 120, 136),
			newVP9YCbCrForTest(128, 128, y, 120, 136),
		}
		result, err := svc.EncodeIntoWithResult(srcs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		sf, err := vp9ParseSuperframe(result.Data)
		if err != nil {
			t.Fatalf("vp9ParseSuperframe[%d]: %v", frame, err)
		}
		if sf.count != 3 || result.LayerCount != 3 ||
			result.ScalabilityStructure.SpatialLayerCount != 3 {
			t.Fatalf("access unit %d counts sf=%d result=%d ss=%d",
				frame, sf.count, result.LayerCount,
				result.ScalabilityStructure.SpatialLayerCount)
		}
		if result.Layers[0].SpatialLayerID != 0 ||
			result.Layers[1].SpatialLayerID != 1 ||
			result.Layers[2].SpatialLayerID != 2 ||
			result.Layers[0].SpatialLayerCount != 3 ||
			result.Layers[1].SpatialLayerCount != 3 ||
			result.Layers[2].SpatialLayerCount != 3 {
			t.Fatalf("access unit %d spatial metadata = %+v/%+v/%+v",
				frame, result.Layers[0], result.Layers[1], result.Layers[2])
		}
		if result.Layers[0].InterLayerDependency ||
			!result.Layers[1].InterLayerDependency ||
			!result.Layers[2].InterLayerDependency ||
			result.Layers[0].NotRefForUpperSpatialLayer ||
			result.Layers[1].NotRefForUpperSpatialLayer ||
			!result.Layers[2].NotRefForUpperSpatialLayer {
			t.Fatalf("access unit %d dependency metadata = %+v/%+v/%+v",
				frame, result.Layers[0], result.Layers[1], result.Layers[2])
		}
		if frame == 0 {
			if !result.Layers[0].KeyFrame ||
				result.Layers[1].KeyFrame ||
				result.Layers[2].KeyFrame {
				t.Fatalf("first access unit key flags = %v/%v/%v, want true/false/false",
					result.Layers[0].KeyFrame,
					result.Layers[1].KeyFrame,
					result.Layers[2].KeyFrame)
			}
		} else if result.Layers[0].KeyFrame ||
			result.Layers[1].KeyFrame ||
			result.Layers[2].KeyFrame {
			t.Fatalf("inter access unit %d key flags = %v/%v/%v, want all false",
				frame, result.Layers[0].KeyFrame,
				result.Layers[1].KeyFrame,
				result.Layers[2].KeyFrame)
		}
		for layer := 0; layer < 3; layer++ {
			var br vp9dec.BitReader
			br.Init(sf.frames[layer])
			refWidth := uint32(widths[layer])
			refHeight := uint32(heights[layer])
			if layer > 0 {
				refWidth = uint32(widths[layer-1])
				refHeight = uint32(heights[layer-1])
			}
			header, err := vp9dec.ReadUncompressedHeader(&br, nil,
				func(uint8) (uint32, uint32) {
					return refWidth, refHeight
				})
			if err != nil {
				t.Fatalf("ReadUncompressedHeader[%d][%d]: %v", frame, layer, err)
			}
			if header.Width != uint32(widths[layer]) ||
				header.Height != uint32(heights[layer]) ||
				!header.ShowFrame {
				t.Fatalf("header[%d][%d] = %+v", frame, layer, header)
			}
			if frame == 0 && layer == 0 {
				if header.FrameType != common.KeyFrame {
					t.Fatalf("header[%d][%d] frame type = %d, want key",
						frame, layer, header.FrameType)
				}
			} else if header.FrameType != common.InterFrame {
				t.Fatalf("header[%d][%d] frame type = %d, want inter",
					frame, layer, header.FrameType)
			}
		}
		if frame == 0 {
			decoder, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			if err := decoder.Decode(result.Data); err != nil {
				t.Fatalf("Decode first SVC superframe: %v", err)
			}
			top, ok := decoder.NextFrame()
			if !ok {
				t.Fatal("NextFrame returned no frame")
			}
			assertVP9FilledFrameWithin(t, top, 128, 128, y, 120, 136, 0)
		}
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

func TestVP9SpatialSVCEncoderLayerReferenceControls(t *testing.T) {
	const baseW, baseH = 32, 32
	const enhW, enhH = 64, 64
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: baseW, Height: baseH},
			{Width: enhW, Height: enhH},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}

	baseRefYCbCr := newVP9MotionYCbCrForTest(baseW, baseH)
	baseRef := vp9ImageFromYCbCrForTest(baseRefYCbCr)
	baseWant := clonePublicImage(baseRef)
	if err := svc.SetLayerReferenceFrame(0, ReferenceGolden, baseRef); err != nil {
		t.Fatalf("SetLayerReferenceFrame base: %v", err)
	}
	baseRef.Y[0] ^= 0xff
	baseDst := vp9ImageFromYCbCrForTest(newVP9YCbCrForTest(baseW, baseH, 0, 0, 0))
	if err := svc.CopyLayerReferenceFrame(0, ReferenceGolden, &baseDst); err != nil {
		t.Fatalf("CopyLayerReferenceFrame base: %v", err)
	}
	assertImagesEqual(t, "base layer copied GOLDEN reference", baseWant, baseDst)

	enhRefYCbCr := newVP9MotionYCbCrForTest(enhW, enhH)
	enhRef := vp9ImageFromYCbCrForTest(enhRefYCbCr)
	if err := svc.SetLayerReferenceFrame(1, ReferenceLast, enhRef); err != nil {
		t.Fatalf("SetLayerReferenceFrame enhancement: %v", err)
	}
	enhDst := vp9ImageFromYCbCrForTest(newVP9YCbCrForTest(enhW, enhH, 0, 0, 0))
	if err := svc.CopyLayerReferenceFrame(1, ReferenceLast, &enhDst); err != nil {
		t.Fatalf("CopyLayerReferenceFrame enhancement: %v", err)
	}
	assertImagesEqual(t, "enhancement layer copied LAST reference", enhRef, enhDst)

	if err := svc.SetLayerReferenceFrame(2, ReferenceLast, enhRef); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerReferenceFrame invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.CopyLayerReferenceFrame(2, ReferenceLast, &enhDst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("CopyLayerReferenceFrame invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := svc.SetLayerReferenceFrame(0, ReferenceLast, baseWant); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerReferenceFrame after close err = %v, want ErrClosed", err)
	}
	if err := svc.CopyLayerReferenceFrame(0, ReferenceLast, &baseDst); !errors.Is(err, ErrClosed) {
		t.Fatalf("CopyLayerReferenceFrame after close err = %v, want ErrClosed", err)
	}
}

func TestVP9SpatialSVCEncodeResultPacketizeRTP(t *testing.T) {
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
	dst := make([]byte, 1<<20)
	result, err := svc.EncodeIntoWithResult([]*image.YCbCr{
		newVP9CheckerYCbCrForTest(32, 32, 40, 200, 120, 136),
		newVP9CheckerYCbCrForTest(64, 64, 40, 200, 120, 136),
	}, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}

	const mtu = 96
	packets, payloadBytes, err := result.RTPPacketizationSize(mtu)
	if err != nil {
		t.Fatalf("RTPPacketizationSize: %v", err)
	}
	if packets <= int(result.LayerCount) || payloadBytes <= len(result.Layers[0].Data)+len(result.Layers[1].Data) {
		t.Fatalf("packetization size = packets:%d bytes:%d, want fragmented payloads",
			packets, payloadBytes)
	}
	shortPackets := make([]RTPPayloadFragment, packets-1)
	payloadBuf := make([]byte, payloadBytes)
	gotPackets, gotBytes, err := result.PacketizeRTPInto(shortPackets,
		payloadBuf, mtu)
	if !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("short PacketizeRTPInto err = %v, want ErrBufferTooSmall", err)
	}
	if gotPackets != packets || gotBytes != payloadBytes {
		t.Fatalf("short PacketizeRTPInto need = %d/%d, want %d/%d",
			gotPackets, gotBytes, packets, payloadBytes)
	}

	payloads := make([]RTPPayloadFragment, packets)
	n, used, err := result.PacketizeRTPInto(payloads, payloadBuf, mtu)
	if err != nil {
		t.Fatalf("PacketizeRTPInto: %v", err)
	}
	if n != packets || used != payloadBytes {
		t.Fatalf("PacketizeRTPInto returned = %d/%d, want %d/%d",
			n, used, packets, payloadBytes)
	}
	var byLayer [VP9MaxSpatialLayers][]RTPPayloadFragment
	var seen [VP9MaxSpatialLayers]int
	prevSpatial := uint8(0)
	for i, payload := range payloads {
		if len(payload.Payload) > mtu {
			t.Fatalf("payload %d length = %d, exceeds mtu %d",
				i, len(payload.Payload), mtu)
		}
		desc, _, err := ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if desc.SpatialID < prevSpatial {
			t.Fatalf("payload %d spatial id = %d after %d",
				i, desc.SpatialID, prevSpatial)
		}
		prevSpatial = desc.SpatialID
		if desc.SpatialID >= result.LayerCount {
			t.Fatalf("payload %d spatial id = %d, want < %d",
				i, desc.SpatialID, result.LayerCount)
		}
		layerID := int(desc.SpatialID)
		seen[layerID]++
		if layerID == 0 && seen[layerID] == 1 {
			if !desc.ScalabilityStructurePresent ||
				desc.ScalabilityStructure.SpatialLayerCount != 2 ||
				desc.ScalabilityStructure.Width[0] != 32 ||
				desc.ScalabilityStructure.Width[1] != 64 {
				t.Fatalf("base first descriptor = %+v", desc)
			}
		} else if desc.ScalabilityStructurePresent {
			t.Fatalf("payload %d layer %d repeated scalability structure",
				i, layerID)
		}
		if layerID == 1 && !desc.InterLayerDependency {
			t.Fatalf("enhancement payload %d missing inter-layer dependency", i)
		}
		byLayer[layerID] = append(byLayer[layerID], payload)
	}
	for layerID := 0; layerID < int(result.LayerCount); layerID++ {
		assembled, err := AssembleVP9RTPFrame(byLayer[layerID])
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame layer %d: %v", layerID, err)
		}
		if !bytes.Equal(assembled, result.Layers[layerID].Data) {
			t.Fatalf("assembled layer %d does not match encoded layer", layerID)
		}
	}

	allocPayloads, err := result.PacketizeRTP(mtu)
	if err != nil {
		t.Fatalf("PacketizeRTP: %v", err)
	}
	if len(allocPayloads) != packets {
		t.Fatalf("PacketizeRTP payloads = %d, want %d", len(allocPayloads), packets)
	}
}

func TestVP9SpatialSVCRTPPacketizeSteadyStateNoAlloc(t *testing.T) {
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
	result, err := svc.EncodeIntoWithResult(srcs, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	const mtu = 80
	packets, payloadBytes, err := result.RTPPacketizationSize(mtu)
	if err != nil {
		t.Fatalf("RTPPacketizationSize: %v", err)
	}
	payloads := make([]RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	if _, _, err := result.PacketizeRTPInto(payloads, payloadBuf, mtu); err != nil {
		t.Fatalf("warmup PacketizeRTPInto: %v", err)
	}
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRuns, func() {
		n, used, err := result.PacketizeRTPInto(payloads, payloadBuf, mtu)
		if err != nil {
			t.Fatalf("alloc run PacketizeRTPInto: %v", err)
		}
		if n != packets || used != payloadBytes {
			t.Fatalf("alloc run PacketizeRTPInto returned %d/%d, want %d/%d",
				n, used, packets, payloadBytes)
		}
	})
	if allocs != 0 {
		t.Fatalf("VP9 spatial SVC RTP packetize allocs = %f, want 0", allocs)
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
