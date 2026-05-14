package govpx

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestVP9RTPPayloadDescriptorMinimalRoundTrip(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{
		StartOfFrame: true,
		EndOfFrame:   true,
	}
	payload := []byte{0x82, 0x49, 0x83}
	packet, err := PackVP9RTPPayload(desc, payload)
	if err != nil {
		t.Fatalf("PackVP9RTPPayload: %v", err)
	}
	if want := []byte{0x0c, 0x82, 0x49, 0x83}; !bytes.Equal(packet, want) {
		t.Fatalf("packet = % x, want % x", packet, want)
	}
	got, rest, err := ParseVP9RTPPayloadDescriptor(packet)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
	if !bytes.Equal(rest, payload) {
		t.Fatalf("payload = % x, want % x", rest, payload)
	}
}

func TestVP9RTPPayloadDescriptorNonFlexibleLayerRoundTrip(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{
		PictureIDPresent:      true,
		PictureID:             0x1234,
		PictureID15Bit:        true,
		InterPicturePredicted: true,
		LayerIndicesPresent:   true,
		StartOfFrame:          true,
		EndOfFrame:            true,
		TemporalID:            5,
		SwitchingUpPoint:      true,
		SpatialID:             2,
		InterLayerDependency:  true,
		TL0PICIDX:             99,
	}
	buf, err := desc.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := []byte{0xec, 0x92, 0x34, 0xb5, 0x63}; !bytes.Equal(buf, want) {
		t.Fatalf("descriptor bytes = % x, want % x", buf, want)
	}
	got, rest, err := ParseVP9RTPPayloadDescriptor(append(buf, 0xaa, 0xbb))
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
	if want := []byte{0xaa, 0xbb}; !bytes.Equal(rest, want) {
		t.Fatalf("payload = % x, want % x", rest, want)
	}
}

func TestVP9RTPPayloadDescriptorFlexibleReferences(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{
		PictureIDPresent:      true,
		PictureID:             42,
		InterPicturePredicted: true,
		LayerIndicesPresent:   true,
		FlexibleMode:          true,
		StartOfFrame:          true,
		TemporalID:            2,
		SpatialID:             1,
		ReferenceIndexCount:   2,
		ReferenceIndices:      [VP9RTPMaxReferenceIndices]uint8{3, 17},
	}
	buf, err := desc.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := []byte{0xf8, 0x2a, 0x42, 0x07, 0x22}; !bytes.Equal(buf, want) {
		t.Fatalf("descriptor bytes = % x, want % x", buf, want)
	}
	got, _, err := ParseVP9RTPPayloadDescriptor(buf)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
}

func TestVP9RTPPayloadDescriptorScalabilityStructure(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{
		StartOfFrame:                true,
		EndOfFrame:                  true,
		ScalabilityStructurePresent: true,
		ScalabilityStructure: VP9RTPScalabilityStructure{
			SpatialLayerCount:   2,
			ResolutionPresent:   true,
			Width:               [VP9RTPMaxSpatialLayers]uint16{640, 1280},
			Height:              [VP9RTPMaxSpatialLayers]uint16{360, 720},
			PictureGroupPresent: true,
			PictureGroups: []VP9RTPPictureGroup{
				{SwitchingUpPoint: true},
				{
					TemporalID:          2,
					ReferenceIndexCount: 2,
					ReferenceIndices:    [VP9RTPMaxReferenceIndices]uint8{1, 5},
				},
			},
		},
	}
	buf, err := desc.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := []byte{
		0x0e, 0x38,
		0x02, 0x80, 0x01, 0x68,
		0x05, 0x00, 0x02, 0xd0,
		0x02,
		0x10,
		0x48, 0x01, 0x05,
	}
	if !bytes.Equal(buf, want) {
		t.Fatalf("descriptor bytes = % x, want % x", buf, want)
	}
	got, rest, err := ParseVP9RTPPayloadDescriptor(append(buf, 0xaa))
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
	if want := []byte{0xaa}; !bytes.Equal(rest, want) {
		t.Fatalf("payload = % x, want % x", rest, want)
	}
}

func TestPackVP9RTPPayloadInto(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{StartOfFrame: true, EndOfFrame: true}
	payload := []byte{0x01, 0x02}
	need, err := PackVP9RTPPayloadInto(make([]byte, 1), desc, payload)
	if !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("short dst error = %v, want ErrBufferTooSmall", err)
	}
	if need != 3 {
		t.Fatalf("short dst need = %d, want 3", need)
	}
	dst := make([]byte, need)
	n, err := PackVP9RTPPayloadInto(dst, desc, payload)
	if err != nil {
		t.Fatalf("PackVP9RTPPayloadInto: %v", err)
	}
	if n != need {
		t.Fatalf("n = %d, want %d", n, need)
	}
	if want := []byte{0x0c, 0x01, 0x02}; !bytes.Equal(dst, want) {
		t.Fatalf("packet = % x, want % x", dst, want)
	}
	if _, err := VP9RTPPayloadSize(desc, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("empty payload size error = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9RTPPayloadDescriptorRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		desc VP9RTPPayloadDescriptor
	}{
		{name: "flex without picture id", desc: VP9RTPPayloadDescriptor{FlexibleMode: true}},
		{name: "seven bit picture id overflow", desc: VP9RTPPayloadDescriptor{PictureIDPresent: true, PictureID: 0x80}},
		{name: "fifteen bit picture id overflow", desc: VP9RTPPayloadDescriptor{PictureIDPresent: true, PictureID15Bit: true, PictureID: 0x8000}},
		{name: "flex predicted without references", desc: VP9RTPPayloadDescriptor{PictureIDPresent: true, InterPicturePredicted: true, FlexibleMode: true}},
		{name: "zero reference", desc: VP9RTPPayloadDescriptor{PictureIDPresent: true, InterPicturePredicted: true, FlexibleMode: true, ReferenceIndexCount: 1}},
		{name: "layer data without layer flag", desc: VP9RTPPayloadDescriptor{TemporalID: 1}},
		{name: "base layer dependency", desc: VP9RTPPayloadDescriptor{LayerIndicesPresent: true, InterLayerDependency: true}},
		{name: "too many spatial layers", desc: VP9RTPPayloadDescriptor{ScalabilityStructurePresent: true, ScalabilityStructure: VP9RTPScalabilityStructure{SpatialLayerCount: 9}}},
		{name: "missing layer resolution", desc: VP9RTPPayloadDescriptor{ScalabilityStructurePresent: true, ScalabilityStructure: VP9RTPScalabilityStructure{ResolutionPresent: true}}},
		{name: "stale scalability structure", desc: VP9RTPPayloadDescriptor{ScalabilityStructure: VP9RTPScalabilityStructure{SpatialLayerCount: 1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.desc.Size(); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Size error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestParseVP9RTPPayloadDescriptorRejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
	}{
		{name: "empty"},
		{name: "flex without picture id", in: []byte{0x10}},
		{name: "truncated picture id", in: []byte{0x80}},
		{name: "truncated extended picture id", in: []byte{0x80, 0x80}},
		{name: "truncated layer indices", in: []byte{0x20}},
		{name: "nonpredicted temporal layer", in: []byte{0x20, 0x20, 0x00}},
		{name: "predicted flex missing reference", in: []byte{0xd0, 0x01}},
		{name: "too many references", in: []byte{0xd0, 0x01, 0x03, 0x05, 0x07}},
		{name: "zero reference", in: []byte{0xd0, 0x01, 0x00}},
		{name: "truncated scalability structure", in: []byte{0x02}},
		{name: "zero width scalability structure", in: []byte{0x02, 0x10, 0x00, 0x00, 0x00, 0x01}},
		{name: "zero picture-group reference", in: []byte{0x02, 0x08, 0x01, 0x04, 0x00}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ParseVP9RTPPayloadDescriptor(tc.in); !errors.Is(err, ErrInvalidVP9Data) {
				t.Fatalf("ParseVP9RTPPayloadDescriptor error = %v, want ErrInvalidVP9Data", err)
			}
		})
	}
}
