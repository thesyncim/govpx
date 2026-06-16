package govpx_test

import (
	"bytes"
	"image"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

const vp9GoldenReferenceRefreshFlagForTest = uint8(1 << 1)

func TestVP9SpatialSVCEncoderEncodesInterLayerSuperframe(t *testing.T) {
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
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
	sf, err := bitstream.ParseSuperframe(result.Data)
	if err != nil {
		t.Fatalf("bitstream.ParseSuperframe: %v", err)
	}
	if sf.Count != 2 {
		t.Fatalf("superframe count = %d, want 2", sf.Count)
	}
	if !bytes.Equal(sf.Frames[0], result.Layers[0].Data) ||
		!bytes.Equal(sf.Frames[1], result.Layers[1].Data) {
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
		result.Layers[1].InterPicturePredicted ||
		result.Layers[1].SpatialLayerID != 1 ||
		result.Layers[1].SpatialLayerCount != 2 ||
		!result.Layers[1].InterLayerDependency ||
		!result.Layers[1].NotRefForUpperSpatialLayer ||
		result.Layers[1].ScalabilityStructurePresent {
		t.Fatalf("enhancement layer result = %+v", result.Layers[1])
	}
	if got, want := result.Layers[1].RefreshFrameFlags,
		vp9GoldenReferenceRefreshFlagForTest; got != want {
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
		enhDesc.InterPicturePredicted || !enhDesc.InterLayerDependency ||
		enhDesc.ScalabilityStructurePresent {
		t.Fatalf("enhancement RTP descriptor = %+v", enhDesc)
	}

	var br vp9dec.BitReader
	br.Init(sf.Frames[1])
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

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
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
	assertVP9FilledFrameWithinForTest(t, frame, 64, 64, 90, 100, 110, 0)
}

func TestVP9SpatialSVCEncoderLastLayerQuantizers(t *testing.T) {
	var nilSVC *govpx.VP9SpatialSVCEncoder
	_, _, ok := nilSVC.LastLayerQuantizers()
	for i, valid := range ok {
		if valid {
			t.Fatalf("nil LastLayerQuantizers ok[%d] = true, want false", i)
		}
	}

	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
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
	for i := int(result.LayerCount); i < govpx.VP9MaxSpatialLayers; i++ {
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
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
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

func TestVP9SpatialSVCEncoderThreeLayerInterLayerMultiFrame(t *testing.T) {
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           3,
		InterLayerPrediction: true,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
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
		sf, err := bitstream.ParseSuperframe(result.Data)
		if err != nil {
			t.Fatalf("bitstream.ParseSuperframe[%d]: %v", frame, err)
		}
		if sf.Count != 3 || result.LayerCount != 3 ||
			result.ScalabilityStructure.SpatialLayerCount != 3 {
			t.Fatalf("access unit %d counts sf=%d result=%d ss=%d",
				frame, sf.Count, result.LayerCount,
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
			br.Init(sf.Frames[layer])
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
			decoder, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
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
			assertVP9FilledFrameWithinForTest(t, top, 128, 128, y, 120, 136, 0)
		}
	}
}
