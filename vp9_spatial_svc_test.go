package govpx

import (
	"bytes"
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"reflect"
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
		vp9test.NewYCbCr(32, 32, 90, 100, 110),
		vp9test.NewYCbCr(64, 64, 90, 100, 110),
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
	if got, want := result.Layers[1].RefreshFrameFlags,
		uint8(1<<vp9GoldenRefSlot); got != want {
		t.Fatalf("enhancement refresh flags = %#x, want GOLDEN %#x", got, want)
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

func TestVP9SpatialSVCEncoderLastLayerQuantizers(t *testing.T) {
	var nilSVC *VP9SpatialSVCEncoder
	_, _, ok := nilSVC.LastLayerQuantizers()
	for i, valid := range ok {
		if valid {
			t.Fatalf("nil LastLayerQuantizers ok[%d] = true, want false", i)
		}
	}

	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: 32, Height: 32, Quantizer: 64},
			{Width: 64, Height: 64, Quantizer: 128},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	public, internal, ok := svc.LastLayerQuantizers()
	for i, valid := range ok {
		if valid || public[i] != 0 || internal[i] != 0 {
			t.Fatalf("pre-encode quantizer[%d] = (%d,%d,%t), want zeros/false",
				i, public[i], internal[i], valid)
		}
	}

	result, err := svc.EncodeIntoWithResult([]*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 90, 100, 110),
		vp9test.NewYCbCr(64, 64, 120, 130, 140),
	}, make([]byte, 1<<20))
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	public, internal, ok = svc.LastLayerQuantizers()
	for i := 0; i < int(result.LayerCount); i++ {
		if !ok[i] ||
			public[i] != result.Layers[i].Quantizer ||
			internal[i] != result.Layers[i].InternalQuantizer {
			t.Fatalf("quantizer[%d] = (%d,%d,%t), want (%d,%d,true)",
				i, public[i], internal[i], ok[i], result.Layers[i].Quantizer,
				result.Layers[i].InternalQuantizer)
		}
	}
	for i := int(result.LayerCount); i < VP9MaxSpatialLayers; i++ {
		if ok[i] || public[i] != 0 || internal[i] != 0 {
			t.Fatalf("unused quantizer[%d] = (%d,%d,%t), want zeros/false",
				i, public[i], internal[i], ok[i])
		}
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, _, ok = svc.LastLayerQuantizers()
	for i, valid := range ok {
		if valid {
			t.Fatalf("closed LastLayerQuantizers ok[%d] = true, want false", i)
		}
	}
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
		vp9test.NewYCbCr(32, 32, 20, 128, 128),
		vp9test.NewYCbCr(64, 64, 40, 128, 128),
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
	for frame := range 3 {
		y := uint8(60 + frame*11)
		srcs := []*image.YCbCr{
			vp9test.NewYCbCr(32, 32, y, 120, 136),
			vp9test.NewYCbCr(64, 64, y, 120, 136),
			vp9test.NewYCbCr(128, 128, y, 120, 136),
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
		for layer := range 3 {
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
			wantRefresh := uint8(1 << uint(layer))
			if frame == 0 && layer == 0 {
				wantRefresh = 0xff
			}
			if header.RefreshFrameFlags != wantRefresh {
				t.Fatalf("header[%d][%d] refresh = %#02x, want %#02x",
					frame, layer, header.RefreshFrameFlags, wantRefresh)
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

func TestVP9SpatialSVCEncoderLayerAdvancedRuntimeControls(t *testing.T) {
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
			Threads:             2,
		}
	}
	vbrLayer := func(width, height, kbps int) VP9EncoderOptions {
		opts := cbrLayer(width, height, kbps)
		opts.RateControlMode = RateControlVBR
		return opts
	}
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			cbrLayer(32, 32, 300),
			vbrLayer(64, 64, 700),
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}

	stats := finalizedVP9TwoPassTestStats(100, 120, 90, 110)
	if err := svc.SetLayerFrameDropAllowed(0, true); err != nil {
		t.Fatalf("SetLayerFrameDropAllowed: %v", err)
	}
	if err := svc.SetLayerRateControlBuffer(0, 320, 160, 240); err != nil {
		t.Fatalf("SetLayerRateControlBuffer: %v", err)
	}
	if err := svc.SetLayerPostEncodeDrop(0, true); err != nil {
		t.Fatalf("SetLayerPostEncodeDrop: %v", err)
	}
	if err := svc.SetLayerDisableOvershootMaxQCBR(0, true); err != nil {
		t.Fatalf("SetLayerDisableOvershootMaxQCBR: %v", err)
	}
	if err := svc.SetLayerNextFrameQIndex(0, 96); err != nil {
		t.Fatalf("SetLayerNextFrameQIndex: %v", err)
	}
	if err := svc.SetLayerRealtimeTarget(0, RealtimeTarget{
		BitrateKbps:  360,
		FPS:          24,
		Width:        32,
		Height:       32,
		MinQuantizer: 6,
		MaxQuantizer: 54,
		FrameDrop:    RealtimeFrameDropDisabled,
	}); err != nil {
		t.Fatalf("SetLayerRealtimeTarget: %v", err)
	}
	if err := svc.SetLayerTuning(0, TuneSSIM); err != nil {
		t.Fatalf("SetLayerTuning: %v", err)
	}
	if err := svc.SetLayerColorSpace(0, VP9ColorSpaceBT709); err != nil {
		t.Fatalf("SetLayerColorSpace: %v", err)
	}
	if err := svc.SetLayerColorRange(0, VP9ColorRangeFull); err != nil {
		t.Fatalf("SetLayerColorRange: %v", err)
	}
	if err := svc.SetLayerRenderSize(0, 30, 28); err != nil {
		t.Fatalf("SetLayerRenderSize: %v", err)
	}
	if err := svc.SetLayerTargetLevel(0, 31); err != nil {
		t.Fatalf("SetLayerTargetLevel: %v", err)
	}
	if err := svc.SetLayerDisableLoopfilter(0, VP9LoopfilterDisableInter); err != nil {
		t.Fatalf("SetLayerDisableLoopfilter: %v", err)
	}
	if err := svc.SetLayerLossless(0, true); err != nil {
		t.Fatalf("SetLayerLossless: %v", err)
	}
	if err := svc.SetLayerScreenContentMode(0, 1); err != nil {
		t.Fatalf("SetLayerScreenContentMode: %v", err)
	}
	if err := svc.SetLayerSharpness(0, 4); err != nil {
		t.Fatalf("SetLayerSharpness: %v", err)
	}
	if err := svc.SetLayerStaticThreshold(0, 512); err != nil {
		t.Fatalf("SetLayerStaticThreshold: %v", err)
	}
	if err := svc.SetLayerKeyFrameInterval(0, 7); err != nil {
		t.Fatalf("SetLayerKeyFrameInterval: %v", err)
	}
	if err := svc.SetLayerKeyFrameIntervalRange(0, 1, 7); err != nil {
		t.Fatalf("SetLayerKeyFrameIntervalRange: %v", err)
	}
	if err := svc.SetLayerAdaptiveKeyFrames(0, true); err != nil {
		t.Fatalf("SetLayerAdaptiveKeyFrames: %v", err)
	}
	if err := svc.SetLayerCQLevel(1, 24); err != nil {
		t.Fatalf("SetLayerCQLevel: %v", err)
	}
	if err := svc.SetLayerAQMode(1, VP9AQComplexity); err != nil {
		t.Fatalf("SetLayerAQMode: %v", err)
	}
	if err := svc.SetLayerAutoAltRef(0, false); err != nil {
		t.Fatalf("SetLayerAutoAltRef(false): %v", err)
	}
	if err := svc.SetLayerFrameParallelDecoding(0, false); err != nil {
		t.Fatalf("SetLayerFrameParallelDecoding: %v", err)
	}
	if err := svc.SetLayerFrameParallelEncoderThreads(0, 1); err != nil {
		t.Fatalf("SetLayerFrameParallelEncoderThreads: %v", err)
	}
	if err := svc.SetLayerRTCExternalRateControl(0, true); err != nil {
		t.Fatalf("SetLayerRTCExternalRateControl: %v", err)
	}
	if err := svc.SetLayerRowMT(0, true); err != nil {
		t.Fatalf("SetLayerRowMT: %v", err)
	}
	if err := svc.SetLayerTwoPassStats(1, stats); err != nil {
		t.Fatalf("SetLayerTwoPassStats: %v", err)
	}
	if err := svc.SetLayerARNR(1, 3, 4, 2); err != nil {
		t.Fatalf("SetLayerARNR: %v", err)
	}
	if err := svc.SetLayerMinGFInterval(1, 4); err != nil {
		t.Fatalf("SetLayerMinGFInterval: %v", err)
	}
	if err := svc.SetLayerMaxGFInterval(1, 12); err != nil {
		t.Fatalf("SetLayerMaxGFInterval: %v", err)
	}
	if err := svc.SetLayerFramePeriodicBoost(1, true); err != nil {
		t.Fatalf("SetLayerFramePeriodicBoost: %v", err)
	}
	if err := svc.SetLayerAltRefAQ(1, true); err != nil {
		t.Fatalf("SetLayerAltRefAQ: %v", err)
	}
	if err := svc.SetLayerEnableKeyFrameFiltering(1, true); err != nil {
		t.Fatalf("SetLayerEnableKeyFrameFiltering: %v", err)
	}
	if err := svc.SetLayerEnableTPL(0, false); err != nil {
		t.Fatalf("SetLayerEnableTPL(false): %v", err)
	}
	if err := svc.SetLayerDeltaQUV(1, 4); err != nil {
		t.Fatalf("SetLayerDeltaQUV: %v", err)
	}

	base, err := svc.LayerEncoder(0)
	if err != nil {
		t.Fatalf("LayerEncoder(0): %v", err)
	}
	enh, err := svc.LayerEncoder(1)
	if err != nil {
		t.Fatalf("LayerEncoder(1): %v", err)
	}
	if base.opts.DropFrameAllowed ||
		base.rc.dropFrameAllowed ||
		base.opts.TargetBitrateKbps != 360 ||
		base.opts.FPS != 24 ||
		base.opts.TimebaseNum != 1 ||
		base.opts.TimebaseDen != 24 ||
		base.opts.MinQuantizer != 6 ||
		base.opts.MaxQuantizer != 54 ||
		base.opts.BufferSizeMs != 320 ||
		base.opts.BufferInitialSizeMs != 160 ||
		base.opts.BufferOptimalSizeMs != 240 ||
		!base.opts.PostEncodeDrop ||
		!base.rc.postEncodeDrop ||
		!base.opts.DisableOvershootMaxQCBR ||
		!base.rc.disableOvershootMaxQCBR ||
		!base.opts.NextFrameQIndexSet ||
		base.opts.NextFrameQIndex != 96 ||
		!base.rc.nextFrameQIndexSet ||
		base.rc.nextFrameQIndex != 96 ||
		base.opts.Tuning != TuneSSIM ||
		base.opts.ColorSpace != VP9ColorSpaceBT709 ||
		base.opts.ColorRange != VP9ColorRangeFull ||
		base.opts.RenderWidth != 30 ||
		base.opts.RenderHeight != 28 ||
		base.opts.TargetLevel != 31 ||
		base.opts.DisableLoopfilter != VP9LoopfilterDisableInter ||
		!base.opts.FrameParallelDecodingSet ||
		base.opts.FrameParallelDecoding ||
		base.opts.FrameParallelEncoderThreads != 1 ||
		base.opts.AutoAltRef ||
		base.opts.EnableTPL ||
		!base.opts.RTCExternalRateControl ||
		!base.opts.RowMT ||
		!base.opts.Lossless ||
		base.opts.ScreenContentMode != 1 ||
		base.opts.Sharpness != 4 ||
		base.opts.StaticThreshold != 512 ||
		base.opts.MinKeyframeInterval != 1 ||
		base.opts.MaxKeyframeInterval != 7 ||
		!base.opts.AdaptiveKeyFrames {
		t.Fatalf("base layer advanced controls missing: opts=%+v rc=%+v",
			base.opts, base.rc)
	}
	if enh.opts.CQLevel != 24 ||
		enh.opts.AQMode != VP9AQComplexity ||
		len(enh.opts.TwoPassStats) != len(stats) ||
		!enh.twoPass.enabled() ||
		enh.opts.ARNRMaxFrames != 3 ||
		enh.opts.ARNRStrength != 4 ||
		enh.opts.ARNRType != 2 ||
		enh.opts.MinGFInterval != 4 ||
		enh.rc.minGFInterval != 4 ||
		enh.opts.MaxGFInterval != 12 ||
		enh.rc.maxGFInterval != 12 ||
		!enh.opts.FramePeriodicBoost ||
		!enh.rc.framePeriodicBoost ||
		!enh.opts.AltRefAQ ||
		!enh.rc.altRefAQ ||
		!enh.opts.EnableKeyFrameFiltering ||
		enh.opts.DeltaQUV != 4 ||
		enh.opts.DropFrameAllowed ||
		enh.opts.Lossless {
		t.Fatalf("enhancement layer advanced controls missing/leaked: opts=%+v twoPass=%t",
			enh.opts, enh.twoPass.enabled())
	}
	if err := svc.SetLayerSharpness(0, 8); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerSharpness invalid err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.Sharpness != 4 {
		t.Fatalf("invalid sharpness mutated base layer to %d", base.opts.Sharpness)
	}
	if err := svc.SetLayerColorSpace(0, VP9ColorSpaceSRGB); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerColorSpace(SRGB) err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.ColorSpace != VP9ColorSpaceBT709 {
		t.Fatalf("invalid color space mutated base layer to %d", base.opts.ColorSpace)
	}
	if err := svc.SetLayerColorRange(0, VP9ColorRange(2)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerColorRange(2) err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerRenderSize(0, 640, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRenderSize invalid err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.RenderWidth != 30 || base.opts.RenderHeight != 28 {
		t.Fatalf("invalid render size mutated base layer to %dx%d",
			base.opts.RenderWidth, base.opts.RenderHeight)
	}
	if err := svc.SetLayerTargetLevel(0, 12); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerTargetLevel(12) err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.TargetLevel != 31 {
		t.Fatalf("invalid target level mutated base layer to %d", base.opts.TargetLevel)
	}
	if err := svc.SetLayerDisableLoopfilter(0, VP9DisableLoopfilter(3)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerDisableLoopfilter invalid err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.DisableLoopfilter != VP9LoopfilterDisableInter {
		t.Fatalf("invalid disable-loopfilter mutated base layer to %d",
			base.opts.DisableLoopfilter)
	}
	if err := svc.SetLayerFrameParallelDecoding(2, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerFrameParallelDecoding invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerFrameParallelEncoderThreads(0, 2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerFrameParallelEncoderThreads(2) err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.FrameParallelEncoderThreads != 1 {
		t.Fatalf("invalid frame-parallel encoder threads mutated base layer to %d",
			base.opts.FrameParallelEncoderThreads)
	}
	if err := svc.SetLayerAutoAltRef(0, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerAutoAltRef(true) err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.AutoAltRef {
		t.Fatal("invalid auto-alt-ref update mutated base layer")
	}
	if err := svc.SetLayerEnableTPL(0, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerEnableTPL(true) err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.EnableTPL {
		t.Fatal("invalid TPL update mutated base layer")
	}
	singleThreadSVC, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: 32, Height: 32, TargetBitrateKbps: 300},
			{Width: 64, Height: 64, TargetBitrateKbps: 700},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder(single thread): %v", err)
	}
	if err := singleThreadSVC.SetLayerRowMT(0, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRowMT with Threads<=1 err = %v, want ErrInvalidConfig", err)
	}
	if err := singleThreadSVC.Close(); err != nil {
		t.Fatalf("singleThreadSVC.Close: %v", err)
	}
	if err := svc.SetLayerRealtimeTarget(1, RealtimeTarget{
		Width:  32,
		Height: 32,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRealtimeTarget resize err = %v, want ErrInvalidConfig", err)
	}
	if enh.opts.Width != 64 || enh.opts.Height != 64 {
		t.Fatalf("rejected enhancement resize mutated dimensions to %dx%d",
			enh.opts.Width, enh.opts.Height)
	}
	if err := svc.SetLayerRealtimeTarget(1, RealtimeTarget{
		Width:  64,
		Height: 64,
	}); err != nil {
		t.Fatalf("same-size SetLayerRealtimeTarget: %v", err)
	}
	if err := svc.SetLayerRateControlBuffer(1, 320, 160, 240); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRateControlBuffer on VBR err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerPostEncodeDrop(1, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerPostEncodeDrop on VBR err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerDisableOvershootMaxQCBR(1, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerDisableOvershootMaxQCBR on VBR err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerTwoPassStats(0, stats); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerTwoPassStats on CBR err = %v, want ErrInvalidConfig", err)
	}
	if base.twoPass.enabled() {
		t.Fatal("invalid base two-pass update enabled CBR layer")
	}
	if err := svc.SetLayerMinGFInterval(1, 13); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerMinGFInterval above max err = %v, want ErrInvalidConfig", err)
	}
	if enh.opts.MinGFInterval != 4 || enh.rc.minGFInterval != 4 {
		t.Fatalf("invalid min-gf update mutated enhancement to opts=%d rc=%d",
			enh.opts.MinGFInterval, enh.rc.minGFInterval)
	}
	if err := svc.SetLayerNextFrameQIndex(1, 300); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetLayerNextFrameQIndex invalid err = %v, want ErrInvalidQuantizer", err)
	}
	if err := svc.SetLayerDeltaQUV(0, 1); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetLayerDeltaQUV on lossless err = %v, want ErrInvalidQuantizer", err)
	}
	if base.opts.DeltaQUV != 0 {
		t.Fatalf("invalid delta-q-uv mutated base layer to %d", base.opts.DeltaQUV)
	}
	if err := svc.SetLayerDeltaQUV(1, 16); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetLayerDeltaQUV invalid err = %v, want ErrInvalidQuantizer", err)
	}
	if enh.opts.DeltaQUV != 4 {
		t.Fatalf("invalid delta-q-uv mutated enhancement layer to %d", enh.opts.DeltaQUV)
	}
	if err := svc.SetLayerCQLevel(2, 24); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerCQLevel invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerAQMode(2, VP9AQNone); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerAQMode invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerKeyFrameIntervalRange(0, 8, 7); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerKeyFrameIntervalRange invalid err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerAdaptiveKeyFrames(2, false); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerAdaptiveKeyFrames invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerRealtimeTarget(2, RealtimeTarget{
		BitrateKbps: 100,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRealtimeTarget invalid layer err = %v, want ErrInvalidConfig", err)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := svc.SetLayerRealtimeTarget(0, RealtimeTarget{
		BitrateKbps: 360,
	}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerRealtimeTarget after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerARNR(0, 3, 4, 2); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerARNR after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerMinGFInterval(0, 4); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerMinGFInterval after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerPostEncodeDrop(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerPostEncodeDrop after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerNextFrameQIndex(0, 96); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerNextFrameQIndex after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerColorSpace(0, VP9ColorSpaceBT709); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerColorSpace after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerFrameParallelDecoding(0, true); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerFrameParallelDecoding after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerFrameParallelEncoderThreads(0, 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerFrameParallelEncoderThreads after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerAutoAltRef(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerAutoAltRef after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerEnableTPL(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerEnableTPL after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerRowMT(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerRowMT after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerRenderSize(0, 30, 28); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerRenderSize after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerDeltaQUV(0, 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerDeltaQUV after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerAQMode(0, VP9AQNone); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerAQMode after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerAdaptiveKeyFrames(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerAdaptiveKeyFrames after close err = %v, want ErrClosed", err)
	}
	var nilSVC *VP9SpatialSVCEncoder
	if err := nilSVC.SetLayerRealtimeTarget(0, RealtimeTarget{
		BitrateKbps: 360,
	}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerRealtimeTarget on nil err = %v, want ErrClosed", err)
	}
	if err := nilSVC.SetLayerTuning(0, TunePSNR); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerTuning on nil err = %v, want ErrClosed", err)
	}
	if err := nilSVC.SetLayerAQMode(0, VP9AQNone); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerAQMode on nil err = %v, want ErrClosed", err)
	}
	if err := nilSVC.SetLayerAdaptiveKeyFrames(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerAdaptiveKeyFrames on nil err = %v, want ErrClosed", err)
	}
}

func TestVP9SpatialSVCEncoderLayerRuntimeControlSettersNoAlloc(t *testing.T) {
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
			Threads:             2,
		}
	}
	vbrLayer := func(width, height, kbps int) VP9EncoderOptions {
		opts := cbrLayer(width, height, kbps)
		opts.RateControlMode = RateControlVBR
		return opts
	}
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			cbrLayer(32, 32, 300),
			vbrLayer(64, 64, 700),
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	stats := finalizedVP9TwoPassTestStats(100, 120, 90, 110)
	activeOut := make([]uint8, 4)

	tests := []struct {
		name string
		fn   func() error
	}{
		{name: "SetInterLayerPrediction", fn: func() error { return svc.SetInterLayerPrediction(true) }},
		{name: "SetLayerCQLevel", fn: func() error { return svc.SetLayerCQLevel(1, 24) }},
		{name: "SetLayerAQMode", fn: func() error { return svc.SetLayerAQMode(1, VP9AQComplexity) }},
		{name: "SetLayerAutoAltRefFalse", fn: func() error { return svc.SetLayerAutoAltRef(0, false) }},
		{name: "SetLayerFrameParallelDecoding", fn: func() error {
			return svc.SetLayerFrameParallelDecoding(0, false)
		}},
		{name: "SetLayerFrameParallelEncoderThreads", fn: func() error {
			return svc.SetLayerFrameParallelEncoderThreads(0, 1)
		}},
		{name: "SetLayerFrameDropAllowed", fn: func() error { return svc.SetLayerFrameDropAllowed(0, true) }},
		{name: "SetLayerRateControlBuffer", fn: func() error { return svc.SetLayerRateControlBuffer(0, 320, 160, 240) }},
		{name: "SetLayerPostEncodeDrop", fn: func() error { return svc.SetLayerPostEncodeDrop(0, true) }},
		{name: "SetLayerDisableOvershootMaxQCBR", fn: func() error {
			return svc.SetLayerDisableOvershootMaxQCBR(0, true)
		}},
		{name: "SetLayerMaxIntraBitratePct", fn: func() error { return svc.SetLayerMaxIntraBitratePct(0, 180) }},
		{name: "SetLayerMaxInterBitratePct", fn: func() error { return svc.SetLayerMaxInterBitratePct(1, 220) }},
		{name: "SetLayerGFCBRBoostPct", fn: func() error { return svc.SetLayerGFCBRBoostPct(0, 45) }},
		{name: "SetLayerRealtimeTarget", fn: func() error {
			return svc.SetLayerRealtimeTarget(0, RealtimeTarget{
				BitrateKbps:  360,
				FPS:          24,
				Width:        32,
				Height:       32,
				MinQuantizer: 6,
				MaxQuantizer: 54,
				FrameDrop:    RealtimeFrameDropEnabled,
			})
		}},
		{name: "SetLayerTwoPassStats", fn: func() error { return svc.SetLayerTwoPassStats(1, stats) }},
		{name: "SetLayerDeadline", fn: func() error { return svc.SetLayerDeadline(0, DeadlineRealtime) }},
		{name: "SetLayerCPUUsed", fn: func() error { return svc.SetLayerCPUUsed(0, 4) }},
		{name: "SetLayerTuning", fn: func() error { return svc.SetLayerTuning(0, TuneSSIM) }},
		{name: "SetLayerColorSpace", fn: func() error { return svc.SetLayerColorSpace(0, VP9ColorSpaceBT709) }},
		{name: "SetLayerColorRange", fn: func() error { return svc.SetLayerColorRange(0, VP9ColorRangeFull) }},
		{name: "SetLayerRenderSize", fn: func() error { return svc.SetLayerRenderSize(0, 30, 28) }},
		{name: "SetLayerTargetLevel", fn: func() error { return svc.SetLayerTargetLevel(0, 31) }},
		{name: "SetLayerDisableLoopfilter", fn: func() error {
			return svc.SetLayerDisableLoopfilter(0, VP9LoopfilterDisableInter)
		}},
		{name: "SetLayerLossless", fn: func() error { return svc.SetLayerLossless(0, true) }},
		{name: "SetLayerScreenContentMode", fn: func() error { return svc.SetLayerScreenContentMode(0, 1) }},
		{name: "SetLayerNoiseSensitivityZero", fn: func() error { return svc.SetLayerNoiseSensitivity(0, 0) }},
		{name: "SetLayerSharpness", fn: func() error { return svc.SetLayerSharpness(0, 4) }},
		{name: "SetLayerStaticThreshold", fn: func() error { return svc.SetLayerStaticThreshold(0, 512) }},
		{name: "SetLayerKeyFrameInterval", fn: func() error { return svc.SetLayerKeyFrameInterval(0, 7) }},
		{name: "SetLayerKeyFrameIntervalRange", fn: func() error {
			return svc.SetLayerKeyFrameIntervalRange(0, 1, 7)
		}},
		{name: "SetLayerAdaptiveKeyFrames", fn: func() error {
			return svc.SetLayerAdaptiveKeyFrames(0, true)
		}},
		{name: "SetLayerRTCExternalRateControl", fn: func() error {
			return svc.SetLayerRTCExternalRateControl(0, true)
		}},
		{name: "SetLayerRowMT", fn: func() error { return svc.SetLayerRowMT(0, true) }},
		{name: "SetLayerARNR", fn: func() error { return svc.SetLayerARNR(1, 3, 4, 2) }},
		{name: "SetLayerMinGFInterval", fn: func() error { return svc.SetLayerMinGFInterval(1, 4) }},
		{name: "SetLayerMaxGFInterval", fn: func() error { return svc.SetLayerMaxGFInterval(1, 12) }},
		{name: "SetLayerFramePeriodicBoost", fn: func() error { return svc.SetLayerFramePeriodicBoost(1, true) }},
		{name: "SetLayerAltRefAQ", fn: func() error { return svc.SetLayerAltRefAQ(1, true) }},
		{name: "SetLayerEnableKeyFrameFiltering", fn: func() error {
			return svc.SetLayerEnableKeyFrameFiltering(1, true)
		}},
		{name: "SetLayerEnableTPLFalse", fn: func() error { return svc.SetLayerEnableTPL(0, false) }},
		{name: "SetLayerNextFrameQIndex", fn: func() error { return svc.SetLayerNextFrameQIndex(0, 96) }},
		{name: "SetLayerDeltaQUV", fn: func() error { return svc.SetLayerDeltaQUV(1, 4) }},
		{name: "GetLayerActiveMap", fn: func() error { return svc.GetLayerActiveMap(0, activeOut, 2, 2) }},
	}
	for _, tc := range tests {
		allocs := testing.AllocsPerRun(100, func() {
			if err := tc.fn(); err != nil {
				t.Fatalf("%s returned error: %v", tc.name, err)
			}
		})
		if allocs != 0 {
			t.Fatalf("%s allocations = %v, want 0", tc.name, allocs)
		}
	}
}

func TestVP9SpatialSVCEncoderLayerRuntimeControls(t *testing.T) {
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

	if err := svc.SetLayerDeadline(1, DeadlineGoodQuality); err != nil {
		t.Fatalf("SetLayerDeadline: %v", err)
	}
	if err := svc.SetLayerCPUUsed(1, -4); err != nil {
		t.Fatalf("SetLayerCPUUsed: %v", err)
	}
	if err := svc.SetLayerNoiseSensitivity(1, 3); err != nil {
		t.Fatalf("SetLayerNoiseSensitivity: %v", err)
	}
	activeMap := []uint8{0, 1, 1, 0}
	if err := svc.SetLayerActiveMap(0, activeMap, 2, 2); err != nil {
		t.Fatalf("SetLayerActiveMap: %v", err)
	}
	activeMap[0] = 1
	gotActive := make([]uint8, 4)
	if err := svc.GetLayerActiveMap(0, gotActive, 2, 2); err != nil {
		t.Fatalf("GetLayerActiveMap: %v", err)
	}
	if want := []uint8{0, 1, 1, 0}; !reflect.DeepEqual(gotActive, want) {
		t.Fatalf("GetLayerActiveMap = %v, want %v", gotActive, want)
	}
	roi := ROIMap{
		Enabled:   true,
		Rows:      8,
		Cols:      8,
		SegmentID: make([]uint8, 64),
	}
	for i := range roi.SegmentID {
		roi.SegmentID[i] = 1
	}
	roi.DeltaQuantizer[1] = -8
	roi.DeltaLoopFilter[1] = -2
	if err := svc.SetLayerROIMap(1, &roi); err != nil {
		t.Fatalf("SetLayerROIMap: %v", err)
	}
	roi.SegmentID[0] = 0

	base, err := svc.LayerEncoder(0)
	if err != nil {
		t.Fatalf("LayerEncoder(0): %v", err)
	}
	enh, err := svc.LayerEncoder(1)
	if err != nil {
		t.Fatalf("LayerEncoder(1): %v", err)
	}
	if base.opts.Deadline == DeadlineGoodQuality ||
		base.opts.CpuUsed == -4 ||
		base.opts.NoiseSensitivity != 0 ||
		!base.activeMapEnabled ||
		len(base.activeMap) != 16 ||
		base.activeMap[0] != vp9ActiveMapSegmentInactive ||
		base.roi.enabled {
		t.Fatalf("base layer controls leaked or missing: opts=%+v active=%t/%d roi=%t",
			base.opts, base.activeMapEnabled, len(base.activeMap),
			base.roi.enabled)
	}
	if enh.opts.Deadline != DeadlineGoodQuality ||
		enh.opts.CpuUsed != -4 ||
		enh.opts.NoiseSensitivity != 3 ||
		enh.denoiser.sensitivity != 3 ||
		enh.activeMapEnabled ||
		!enh.roi.enabled ||
		len(enh.roi.segmentID) != 64 ||
		enh.roi.segmentID[0] != 1 {
		t.Fatalf("enhancement layer controls = opts:%+v denoise:%d active:%t roi:%t/%d/%d",
			enh.opts, enh.denoiser.sensitivity, enh.activeMapEnabled,
			enh.roi.enabled, len(enh.roi.segmentID), enh.roi.segmentID[0])
	}
	if err := svc.SetLayerCPUUsed(2, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerCPUUsed invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerActiveMap(0, []uint8{1}, 1, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerActiveMap wrong geometry err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.GetLayerActiveMap(0, gotActive[:1], 2, 2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("GetLayerActiveMap short buffer err = %v, want ErrInvalidConfig", err)
	}
	if !reflect.DeepEqual(gotActive, []uint8{0, 1, 1, 0}) {
		t.Fatalf("invalid GetLayerActiveMap mutated output to %v", gotActive)
	}
	if !base.activeMapEnabled || base.activeMap[0] != vp9ActiveMapSegmentInactive {
		t.Fatal("invalid active-map update mutated base layer")
	}
	if err := svc.SetLayerROIMap(1, nil); err != nil {
		t.Fatalf("SetLayerROIMap(nil): %v", err)
	}
	if enh.roi.enabled {
		t.Fatal("SetLayerROIMap(nil) did not disable enhancement ROI")
	}

	dst := make([]byte, 1<<20)
	if _, err := svc.EncodeIntoWithResult([]*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 70, 128, 128),
		vp9test.NewYCbCr(64, 64, 90, 128, 128),
	}, dst); err != nil {
		t.Fatalf("EncodeIntoWithResult after layer controls: %v", err)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := svc.SetLayerDeadline(0, DeadlineRealtime); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerDeadline after close err = %v, want ErrClosed", err)
	}
	if err := svc.GetLayerActiveMap(0, gotActive, 2, 2); !errors.Is(err, ErrClosed) {
		t.Fatalf("GetLayerActiveMap after close err = %v, want ErrClosed", err)
	}
	var nilSVC *VP9SpatialSVCEncoder
	if err := nilSVC.SetLayerNoiseSensitivity(0, 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerNoiseSensitivity on nil err = %v, want ErrClosed", err)
	}
	if err := nilSVC.GetLayerActiveMap(0, gotActive, 2, 2); !errors.Is(err, ErrClosed) {
		t.Fatalf("GetLayerActiveMap on nil err = %v, want ErrClosed", err)
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

	baseRefYCbCr := vp9test.NewMotionYCbCr(baseW, baseH)
	baseRef := vp9ImageFromYCbCrForTest(baseRefYCbCr)
	baseWant := clonePublicImage(baseRef)
	if err := svc.SetLayerReferenceFrame(0, ReferenceGolden, baseRef); err != nil {
		t.Fatalf("SetLayerReferenceFrame base: %v", err)
	}
	baseRef.Y[0] ^= 0xff
	baseDst := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(baseW, baseH, 0, 0, 0))
	if err := svc.CopyLayerReferenceFrame(0, ReferenceGolden, &baseDst); err != nil {
		t.Fatalf("CopyLayerReferenceFrame base: %v", err)
	}
	assertImagesEqual(t, "base layer copied GOLDEN reference", baseWant, baseDst)

	enhRefYCbCr := vp9test.NewMotionYCbCr(enhW, enhH)
	enhRef := vp9ImageFromYCbCrForTest(enhRefYCbCr)
	if err := svc.SetLayerReferenceFrame(1, ReferenceLast, enhRef); err != nil {
		t.Fatalf("SetLayerReferenceFrame enhancement: %v", err)
	}
	enhDst := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(enhW, enhH, 0, 0, 0))
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
		vp9test.NewCheckerYCbCr(32, 32, 40, 200, 120, 136),
		vp9test.NewCheckerYCbCr(64, 64, 40, 200, 120, 136),
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
		vp9test.NewYCbCr(32, 32, 80, 128, 128),
		vp9test.NewYCbCr(64, 64, 80, 128, 128),
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
		vp9test.NewYCbCr(32, 32, 80, 128, 128),
		vp9test.NewYCbCr(64, 64, 80, 128, 128),
	}
	dst := make([]byte, 1<<20)
	for i := range 3 {
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
