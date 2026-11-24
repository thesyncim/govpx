package govpx

import (
	"bytes"
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9EncoderSpatialScalabilityResultAndRTPDescriptor(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  width,
		Height: height,
		SpatialScalability: VP9SpatialScalabilityConfig{
			Enabled:                    true,
			LayerCount:                 2,
			LayerID:                    1,
			InterLayerDependency:       true,
			NotRefForUpperSpatialLayer: true,
			ResolutionPresent:          true,
			Width:                      [VP9RTPMaxSpatialLayers]uint16{32, width},
			Height:                     [VP9RTPMaxSpatialLayers]uint16{32, height},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	result, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height,
		100, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	if result.SpatialLayerID != 1 || result.SpatialLayerCount != 2 ||
		!result.InterLayerDependency || !result.NotRefForUpperSpatialLayer ||
		!result.ScalabilityStructurePresent {
		t.Fatalf("spatial result = %+v, want layer 1/2 dependency with SS", result)
	}
	ss := result.SpatialScalabilityStructure
	if ss.SpatialLayerCount != 2 || !ss.ResolutionPresent ||
		ss.Width[0] != 32 || ss.Height[0] != 32 ||
		ss.Width[1] != width || ss.Height[1] != height {
		t.Fatalf("spatial scalability structure = %+v", ss)
	}

	desc := result.RTPPayloadDescriptor()
	if !desc.LayerIndicesPresent || desc.TemporalID != 0 ||
		desc.SpatialID != 1 || !desc.InterLayerDependency ||
		!desc.NotRefForUpperSpatialLayer ||
		!desc.ScalabilityStructurePresent {
		t.Fatalf("RTP descriptor = %+v, want spatial layer descriptor", desc)
	}
	if desc.ScalabilityStructure.SpatialLayerCount != 2 ||
		desc.ScalabilityStructure.Width[1] != width ||
		desc.ScalabilityStructure.Height[1] != height {
		t.Fatalf("RTP scalability structure = %+v", desc.ScalabilityStructure)
	}
	payload, err := PackVP9RTPPayload(desc, result.Data)
	if err != nil {
		t.Fatalf("PackVP9RTPPayload: %v", err)
	}
	gotDesc, gotPacket, err := ParseVP9RTPPayloadDescriptor(payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
	}
	if !bytes.Equal(gotPacket, result.Data) {
		t.Fatal("RTP payload packet changed")
	}
	if gotDesc.SpatialID != 1 || !gotDesc.InterLayerDependency ||
		!gotDesc.NotRefForUpperSpatialLayer ||
		gotDesc.ScalabilityStructure.SpatialLayerCount != 2 {
		t.Fatalf("parsed RTP descriptor = %+v", gotDesc)
	}
}

func TestVP9EncoderSetSpatialScalabilityUpdatesResultMetadata(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetSpatialScalability(VP9SpatialScalabilityConfig{
		Enabled:    true,
		LayerCount: 3,
		LayerID:    2,
	}); err != nil {
		t.Fatalf("SetSpatialScalability: %v", err)
	}
	dst := make([]byte, 65536)
	result, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height,
		120, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult layer 2: %v", err)
	}
	if result.SpatialLayerID != 2 || result.SpatialLayerCount != 3 ||
		result.ScalabilityStructurePresent {
		t.Fatalf("spatial result layer 2 = %+v, want 2/3 without SS", result)
	}
	if desc := result.RTPPayloadDescriptor(); !desc.LayerIndicesPresent ||
		desc.SpatialID != 2 || desc.ScalabilityStructurePresent {
		t.Fatalf("RTP descriptor layer 2 = %+v", desc)
	}

	if err := e.SetSpatialLayerID(1); err != nil {
		t.Fatalf("SetSpatialLayerID(1): %v", err)
	}
	result, err = e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height,
		140, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult layer 1: %v", err)
	}
	if result.SpatialLayerID != 1 || result.SpatialLayerCount != 3 {
		t.Fatalf("spatial result layer 1 = %+v, want 1/3", result)
	}
	if err := e.SetSpatialScalability(VP9SpatialScalabilityConfig{}); err != nil {
		t.Fatalf("disable SetSpatialScalability: %v", err)
	}
	result, err = e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height,
		160, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult disabled: %v", err)
	}
	if result.SpatialLayerID != 0 || result.SpatialLayerCount != 1 ||
		result.RTPPayloadDescriptor().LayerIndicesPresent {
		t.Fatalf("disabled spatial result = %+v", result)
	}
}

func TestVP9EncoderSetSpatialScalabilityValidation(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetSpatialLayerID(1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetSpatialLayerID disabled err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetSpatialLayerID(0); err != nil {
		t.Fatalf("SetSpatialLayerID disabled base: %v", err)
	}
	if err := e.SetSpatialScalability(VP9SpatialScalabilityConfig{
		Enabled:    true,
		LayerCount: 2,
		LayerID:    2,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetSpatialScalability invalid err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetSpatialScalability(VP9SpatialScalabilityConfig{
		Enabled:           true,
		LayerCount:        2,
		LayerID:           1,
		ResolutionPresent: true,
		Width:             [VP9RTPMaxSpatialLayers]uint16{32, 32},
		Height:            [VP9RTPMaxSpatialLayers]uint16{32, 32},
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetSpatialScalability mismatched dimensions err = %v, want ErrInvalidConfig", err)
	}
}
