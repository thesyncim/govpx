package govpx_test

import (
	"bytes"
	"errors"
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

func TestVP9SpatialSVCEncoderEncodeActiveLayersForWebRTC(t *testing.T) {
	temporal := govpx.TemporalScalabilityConfig{
		Enabled: true,
		Mode:    govpx.TemporalLayeringThreeLayers,
	}
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           3,
		InterLayerPrediction: true,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			{
				Width:                    32,
				Height:                   32,
				FPS:                      30,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				RateControlModeSet:       true,
				RateControlMode:          govpx.RateControlCBR,
				TargetBitrateKbps:        120,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
			{
				Width:                    64,
				Height:                   64,
				FPS:                      30,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				RateControlModeSet:       true,
				RateControlMode:          govpx.RateControlCBR,
				TargetBitrateKbps:        240,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
			{
				Width:                    128,
				Height:                   128,
				FPS:                      30,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				RateControlModeSet:       true,
				RateControlMode:          govpx.RateControlCBR,
				TargetBitrateKbps:        480,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	defer svc.Close()

	srcs := []*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 72, 120, 136),
		vp9test.NewYCbCr(64, 64, 72, 120, 136),
		vp9test.NewYCbCr(128, 128, 72, 120, 136),
	}
	dst := make([]byte, 1<<21)
	if _, err := svc.EncodeIntoWithResult(srcs, dst); err != nil {
		t.Fatalf("warm full EncodeIntoWithResult: %v", err)
	}

	svc.ForceKeyFrame()
	baseOnly, err := svc.EncodeActiveLayersIntoWithResult(srcs, dst, 1)
	if err != nil {
		t.Fatalf("EncodeActiveLayersIntoWithResult base-only: %v", err)
	}
	assertVP9ActiveSVCResultForTest(t, baseOnly, 1)
	if !baseOnly.Layers[0].KeyFrame ||
		baseOnly.Layers[0].InterPicturePredicted ||
		!baseOnly.Layers[0].NotRefForUpperSpatialLayer {
		t.Fatalf("base-only layer metadata = %+v", baseOnly.Layers[0])
	}
	if baseOnly.ScalabilityStructure.Width[1] != 0 ||
		baseOnly.ScalabilityStructure.Height[1] != 0 {
		t.Fatalf("base-only SS leaked hidden dims = %dx%d",
			baseOnly.ScalabilityStructure.Width[1],
			baseOnly.ScalabilityStructure.Height[1])
	}
	if _, err := baseOnly.PacketizeWebRTCRTP(0x10, 96); err != nil {
		t.Fatalf("base-only PacketizeWebRTCRTP: %v", err)
	}
	baseDecoder, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder base-only: %v", err)
	}
	assertVP9SpatialSVCDecoderOutputForTest(t,
		baseDecoder, baseOnly.Data, 0, 0, 32, 32)

	for frame := 0; frame < 2; frame++ {
		vp9test.FillYCbCr(srcs[0], uint8(90+frame*9), 120, 136)
		baseOnly, err = svc.EncodeActiveLayersIntoWithResult(srcs, dst, 1)
		if err != nil {
			t.Fatalf("EncodeActiveLayersIntoWithResult base-only inter %d: %v",
				frame, err)
		}
		assertVP9ActiveSVCResultForTest(t, baseOnly, 1)
	}

	for i, src := range srcs {
		vp9test.FillYCbCr(src, uint8(120+i*10), 120, 136)
	}
	svc.ForceKeyFrame()
	restored, err := svc.EncodeActiveLayersIntoWithResult(srcs, dst, 3)
	if err != nil {
		t.Fatalf("EncodeActiveLayersIntoWithResult restored: %v", err)
	}
	assertVP9ActiveSVCResultForTest(t, restored, 3)
	baseTL0 := restored.Layers[0].TL0PICIDX
	for layer := 1; layer < int(restored.LayerCount); layer++ {
		if restored.Layers[layer].TL0PICIDX != baseTL0 ||
			restored.Layers[layer].TemporalLayerID !=
				restored.Layers[0].TemporalLayerID {
			t.Fatalf("restored layer %d temporal = tid:%d tl0:%d, want tid:%d tl0:%d",
				layer, restored.Layers[layer].TemporalLayerID,
				restored.Layers[layer].TL0PICIDX,
				restored.Layers[0].TemporalLayerID, baseTL0)
		}
	}
	if _, err := restored.PacketizeWebRTCRTP(0x11, 96); err != nil {
		t.Fatalf("restored PacketizeWebRTCRTP: %v", err)
	}
	restoredDecoder, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder restored: %v", err)
	}
	assertVP9SpatialSVCDecoderOutputForTest(t,
		restoredDecoder, restored.Data, 0, 2, 128, 128)

	if _, err := svc.EncodeActiveLayersIntoWithResult(srcs, dst, 0); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("EncodeActiveLayersIntoWithResult(0) err = %v, want ErrInvalidConfig", err)
	}
	if _, err := svc.EncodeActiveLayersIntoWithResult(srcs[:1], dst, 2); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("EncodeActiveLayersIntoWithResult(short srcs) err = %v, want ErrInvalidConfig", err)
	}
}

func assertVP9ActiveSVCResultForTest(
	t *testing.T,
	result govpx.VP9SpatialSVCEncodeResult,
	wantLayers int,
) {
	t.Helper()
	sf, err := bitstream.ParseSuperframe(result.Data)
	if err != nil {
		t.Fatalf("bitstream.ParseSuperframe: %v", err)
	}
	if sf.Count != wantLayers ||
		int(result.LayerCount) != wantLayers ||
		result.ScalabilityStructure.SpatialLayerCount != wantLayers {
		t.Fatalf("active result counts = sf:%d result:%d ss:%d, want %d",
			sf.Count, result.LayerCount,
			result.ScalabilityStructure.SpatialLayerCount, wantLayers)
	}
	for layer := 0; layer < wantLayers; layer++ {
		got := result.Layers[layer]
		if got.SpatialLayerID != uint8(layer) ||
			got.SpatialLayerCount != uint8(wantLayers) ||
			got.NotRefForUpperSpatialLayer != (layer == wantLayers-1) {
			t.Fatalf("active layer %d metadata = %+v, want count %d",
				layer, got, wantLayers)
		}
		if !bytes.Equal(sf.Frames[layer], got.Data) {
			t.Fatalf("active layer %d data differs from superframe", layer)
		}
	}
	for layer := wantLayers; layer < govpx.VP9MaxSpatialLayers; layer++ {
		if result.Layers[layer].Data != nil ||
			result.Layers[layer].SizeBytes != 0 {
			t.Fatalf("inactive layer %d result = %+v, want zero", layer,
				result.Layers[layer])
		}
	}
}

func TestVP9SpatialSVCEncoderForceKeyFrameInterLayerDecodesNextFrame(t *testing.T) {
	const (
		baseW = 64
		baseH = 64
		enhW  = 128
		enhH  = 128
	)
	temporal := govpx.TemporalScalabilityConfig{
		Enabled: true,
		Mode:    govpx.TemporalLayeringThreeLayers,
	}
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			{
				Width:                    baseW,
				Height:                   baseH,
				TargetBitrateKbps:        300,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
			{
				Width:                    enhW,
				Height:                   enhH,
				TargetBitrateKbps:        700,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	decoder, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
		SVCSpatialLayerSet: true,
		SVCSpatialLayer:    1,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}

	enhLayer, err := svc.LayerEncoder(1)
	if err != nil {
		t.Fatalf("LayerEncoder(1): %v", err)
	}
	dst := make([]byte, 1<<20)
	for frame := 0; frame < 4; frame++ {
		if frame == 1 {
			enhLayer.ForceKeyFrame()
			if svc.IsKeyFrameNext() {
				t.Fatal("enhancement-layer ForceKeyFrame armed inter-layer SVC access unit")
			}
		}
		if frame == 2 {
			svc.ForceKeyFrame()
			if !svc.IsKeyFrameNext() {
				t.Fatal("parent ForceKeyFrame did not arm inter-layer SVC access unit")
			}
		}
		result, err := svc.EncodeIntoWithResult([]*image.YCbCr{
			vp9test.NewPanningYCbCr(baseW, baseH, frame),
			vp9test.NewPanningYCbCr(enhW, enhH, frame),
		}, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		if frame == 1 && (result.Layers[0].KeyFrame ||
			result.Layers[1].KeyFrame) {
			t.Fatalf("enhancement-only force frame key flags = %t/%t, want false/false",
				result.Layers[0].KeyFrame, result.Layers[1].KeyFrame)
		}
		if frame == 2 {
			if !result.Layers[0].KeyFrame || result.Layers[1].KeyFrame {
				t.Fatalf("parent force frame key flags = %t/%t, want true/false",
					result.Layers[0].KeyFrame, result.Layers[1].KeyFrame)
			}
			for layer := 0; layer < 2; layer++ {
				if result.Layers[layer].TemporalLayerID != 0 {
					t.Fatalf("forced frame layer %d temporal id = %d, want 0",
						layer, result.Layers[layer].TemporalLayerID)
				}
			}
		}
		if err := decoder.Decode(result.Data); err != nil {
			t.Fatalf("Decode frame %d: %v", frame, err)
		}
		img, ok := decoder.NextFrame()
		if !ok {
			t.Fatalf("Decode frame %d produced no visible frame", frame)
		}
		if img.Width != enhW || img.Height != enhH {
			t.Fatalf("Decode frame %d image = %dx%d, want %dx%d",
				frame, img.Width, img.Height, enhW, enhH)
		}
	}
}

func TestVP9SpatialSVCEncoderWebRTCThreeByThreeDecodesThroughRTP(t *testing.T) {
	const (
		layerCount = 3
		frames     = 8
		mtu        = 256
	)
	widths := [layerCount]int{160, 320, 640}
	heights := [layerCount]int{90, 180, 360}
	bitrates := [layerCount]int{96, 288, 416}
	temporal := govpx.TemporalScalabilityConfig{
		Enabled: true,
		Mode:    govpx.TemporalLayeringThreeLayers,
	}
	var layerOpts [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions
	for layer := range layerCount {
		layerOpts[layer] = govpx.VP9EncoderOptions{
			Width:                    widths[layer],
			Height:                   heights[layer],
			FPS:                      30,
			TargetBitrateKbps:        bitrates[layer],
			RateControlModeSet:       true,
			RateControlMode:          govpx.RateControlCBR,
			MinQuantizer:             4,
			MaxQuantizer:             56,
			MaxKeyframeInterval:      128,
			Deadline:                 govpx.DeadlineRealtime,
			CpuUsed:                  8,
			TemporalScalability:      temporal,
			ErrorResilient:           true,
			FrameParallelDecodingSet: true,
			FrameParallelDecoding:    true,
		}
	}
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           layerCount,
		InterLayerPrediction: true,
		Layers:               layerOpts,
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	var decoders [layerCount]*govpx.VP9Decoder
	for layer := range layerCount {
		decoders[layer], err = govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    uint8(layer),
		})
		if err != nil {
			t.Fatalf("NewVP9Decoder layer %d: %v", layer, err)
		}
	}

	dst := make([]byte, 4<<20)
	wantTemporal := [frames]int{0, 2, 1, 2, 0, 2, 1, 2}
	for frame := range frames {
		srcs := []*image.YCbCr{
			vp9test.NewPanningYCbCr(widths[0], heights[0], frame),
			vp9test.NewPanningYCbCr(widths[1], heights[1], frame),
			vp9test.NewPanningYCbCr(widths[2], heights[2], frame),
		}
		result, err := svc.EncodeIntoWithResult(srcs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
		if result.LayerCount != layerCount {
			t.Fatalf("frame %d layer count = %d, want %d",
				frame, result.LayerCount, layerCount)
		}
		for layer := range layerCount {
			got := result.Layers[layer].TemporalLayerID
			if got != wantTemporal[frame] ||
				result.Layers[layer].TemporalLayerCount != layerCount {
				t.Fatalf("frame %d layer %d temporal = %d/%d, want %d/%d",
					frame, layer, got,
					result.Layers[layer].TemporalLayerCount,
					wantTemporal[frame], layerCount)
			}
		}

		packet := vp9ReassembleSpatialSVCAccessUnitFromRTPForTest(t, result, mtu)
		if !bytes.Equal(packet, result.Data) {
			t.Fatalf("frame %d RTP-reassembled access unit changed payload", frame)
		}
		for layer := range layerCount {
			assertVP9SpatialSVCDecoderOutputForTest(t, decoders[layer],
				packet, frame, layer, widths[layer], heights[layer])
		}
	}
}

func vp9ReassembleSpatialSVCAccessUnitFromRTPForTest(
	t *testing.T,
	result govpx.VP9SpatialSVCEncodeResult,
	mtu int,
) []byte {
	t.Helper()
	payloads, err := result.PacketizeRTP(mtu)
	if err != nil {
		t.Fatalf("PacketizeRTP: %v", err)
	}
	count := int(result.LayerCount)
	var byLayer [govpx.VP9MaxSpatialLayers][]govpx.RTPPayloadFragment
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if !desc.LayerIndicesPresent || int(desc.SpatialID) >= count {
			t.Fatalf("payload %d descriptor = %+v, want spatial layer < %d",
				i, desc, count)
		}
		layer := int(desc.SpatialID)
		wantLayer := result.Layers[layer]
		if int(desc.TemporalID) != wantLayer.TemporalLayerID ||
			desc.TL0PICIDX != wantLayer.TL0PICIDX {
			t.Fatalf("payload %d layer %d temporal RTP = tid:%d tl0:%d, want %d/%d",
				i, layer, desc.TemporalID, desc.TL0PICIDX,
				wantLayer.TemporalLayerID, wantLayer.TL0PICIDX)
		}
		byLayer[layer] = append(byLayer[layer], payload)
	}

	var frames [govpx.VP9MaxSpatialLayers][]byte
	for layer := range count {
		assembled, err := govpx.AssembleVP9RTPFrame(byLayer[layer])
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame layer %d: %v", layer, err)
		}
		if !bytes.Equal(assembled, result.Layers[layer].Data) {
			t.Fatalf("assembled RTP layer %d does not match encoded layer", layer)
		}
		frames[layer] = assembled
	}
	need, err := bitstream.SuperframeSize(frames[:count]...)
	if err != nil {
		t.Fatalf("SuperframeSize: %v", err)
	}
	packet := make([]byte, need)
	n, err := bitstream.PackSuperframeInto(packet, frames[:count]...)
	if err != nil {
		t.Fatalf("PackSuperframeInto: %v", err)
	}
	return packet[:n]
}

func assertVP9SpatialSVCDecoderOutputForTest(
	t *testing.T,
	decoder *govpx.VP9Decoder,
	packet []byte,
	frame int,
	layer int,
	wantWidth int,
	wantHeight int,
) {
	t.Helper()
	if err := decoder.Decode(packet); err != nil {
		t.Fatalf("Decode frame %d layer %d: %v", frame, layer, err)
	}
	img, ok := decoder.NextFrame()
	if !ok {
		t.Fatalf("Decode frame %d layer %d produced no visible frame",
			frame, layer)
	}
	if img.Width != wantWidth || img.Height != wantHeight {
		t.Fatalf("Decode frame %d layer %d image = %dx%d, want %dx%d",
			frame, layer, img.Width, img.Height, wantWidth, wantHeight)
	}
	info, ok := decoder.LastFrameInfo()
	if !ok || !info.ShowFrame || info.Corrupted ||
		info.Width != wantWidth || info.Height != wantHeight {
		t.Fatalf("Decode frame %d layer %d info = %+v ok=%t, want clean %dx%d",
			frame, layer, info, ok, wantWidth, wantHeight)
	}
}
