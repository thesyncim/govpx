package rtp

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/rtptest"
	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
	vpxrtp "github.com/thesyncim/govpx/internal/vpx/rtp"
)

func TestPayloadDescriptorMinimalRoundTrip(t *testing.T) {
	desc := PayloadDescriptor{
		StartOfFrame: true,
		EndOfFrame:   true,
	}
	payload := []byte{0x82, 0x49, 0x83}
	packet, err := vpxrtp.PackPayload(desc, payload)
	if err != nil {
		t.Fatalf("vpxrtp.PackPayload: %v", err)
	}
	if want := []byte{0x0c, 0x82, 0x49, 0x83}; !bytes.Equal(packet, want) {
		t.Fatalf("packet = % x, want % x", packet, want)
	}
	got, rest, err := ParsePayloadDescriptor(packet)
	if err != nil {
		t.Fatalf("ParsePayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
	if !bytes.Equal(rest, payload) {
		t.Fatalf("payload = % x, want % x", rest, payload)
	}
}

func TestPayloadDescriptorNonFlexibleLayerRoundTrip(t *testing.T) {
	desc := PayloadDescriptor{
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
	got, rest, err := ParsePayloadDescriptor(append(buf, 0xaa, 0xbb))
	if err != nil {
		t.Fatalf("ParsePayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
	if want := []byte{0xaa, 0xbb}; !bytes.Equal(rest, want) {
		t.Fatalf("payload = % x, want % x", rest, want)
	}
}

func TestPayloadDescriptorFlexibleReferences(t *testing.T) {
	desc := PayloadDescriptor{
		PictureIDPresent:      true,
		PictureID:             42,
		InterPicturePredicted: true,
		LayerIndicesPresent:   true,
		FlexibleMode:          true,
		StartOfFrame:          true,
		TemporalID:            2,
		SpatialID:             1,
		ReferenceIndexCount:   2,
		ReferenceIndices:      [MaxReferenceIndices]uint8{3, 17},
	}
	buf, err := desc.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := []byte{0xf8, 0x2a, 0x42, 0x07, 0x22}; !bytes.Equal(buf, want) {
		t.Fatalf("descriptor bytes = % x, want % x", buf, want)
	}
	got, _, err := ParsePayloadDescriptor(buf)
	if err != nil {
		t.Fatalf("ParsePayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
}

func TestPayloadDescriptorScalabilityStructure(t *testing.T) {
	desc := PayloadDescriptor{
		StartOfFrame:                true,
		EndOfFrame:                  true,
		ScalabilityStructurePresent: true,
		ScalabilityStructure: ScalabilityStructure{
			SpatialLayerCount:   2,
			ResolutionPresent:   true,
			Width:               [MaxSpatialLayers]uint16{640, 1280},
			Height:              [MaxSpatialLayers]uint16{360, 720},
			PictureGroupPresent: true,
			PictureGroups: []PictureGroup{
				{SwitchingUpPoint: true},
				{
					TemporalID:          2,
					ReferenceIndexCount: 2,
					ReferenceIndices:    [MaxReferenceIndices]uint8{1, 5},
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
	got, rest, err := ParsePayloadDescriptor(append(buf, 0xaa))
	if err != nil {
		t.Fatalf("ParsePayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
	if want := []byte{0xaa}; !bytes.Equal(rest, want) {
		t.Fatalf("payload = % x, want % x", rest, want)
	}
}

func TestPackPayloadInto(t *testing.T) {
	desc := PayloadDescriptor{StartOfFrame: true, EndOfFrame: true}
	payload := []byte{0x01, 0x02}
	need, err := vpxrtp.PackPayloadInto(make([]byte, 1), desc, payload)
	if !errors.Is(err, vpxerrors.ErrBufferTooSmall) {
		t.Fatalf("short dst error = %v, want vpxerrors.ErrBufferTooSmall", err)
	}
	if need != 3 {
		t.Fatalf("short dst need = %d, want 3", need)
	}
	dst := make([]byte, need)
	n, err := vpxrtp.PackPayloadInto(dst, desc, payload)
	if err != nil {
		t.Fatalf("vpxrtp.PackPayloadInto: %v", err)
	}
	if n != need {
		t.Fatalf("n = %d, want %d", n, need)
	}
	if want := []byte{0x0c, 0x01, 0x02}; !bytes.Equal(dst, want) {
		t.Fatalf("packet = % x, want % x", dst, want)
	}
	if _, err := vpxrtp.PayloadSize(desc, nil); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("empty payload size error = %v, want vpxerrors.ErrInvalidConfig", err)
	}
}

func TestPacketizeFrameSinglePayload(t *testing.T) {
	desc := PayloadDescriptor{
		PictureIDPresent:      true,
		PictureID:             41,
		InterPicturePredicted: true,
	}
	frame := []byte{0x82, 0x49, 0x83, 0x10}
	payloads, err := PacketizeFrame(desc, frame, 1200)
	if err != nil {
		t.Fatalf("PacketizeFrame: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	if !payloads[0].Marker {
		t.Fatal("single payload marker = false, want true")
	}
	gotDesc, gotFrame, err := ParsePayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParsePayloadDescriptor: %v", err)
	}
	if !gotDesc.StartOfFrame || !gotDesc.EndOfFrame {
		t.Fatalf("descriptor start/end = %v/%v, want true/true",
			gotDesc.StartOfFrame, gotDesc.EndOfFrame)
	}
	if gotDesc.PictureID != desc.PictureID || !gotDesc.PictureIDPresent ||
		!gotDesc.InterPicturePredicted {
		t.Fatalf("descriptor = %+v, want picture id %d and predicted bit",
			gotDesc, desc.PictureID)
	}
	if !bytes.Equal(gotFrame, frame) {
		t.Fatalf("reassembled frame = % x, want % x", gotFrame, frame)
	}
}

func TestPacketizeFrameIntoFragmentsByMTU(t *testing.T) {
	desc := PayloadDescriptor{
		PictureIDPresent: true,
		PictureID:        0x1234,
		PictureID15Bit:   true,
	}
	frame := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	const mtu = 7
	packets, totalBytes, err := FramePacketizationSize(desc, frame, mtu)
	if err != nil {
		t.Fatalf("FramePacketizationSize: %v", err)
	}
	if packets != 3 || totalBytes != 19 {
		t.Fatalf("size = packets:%d bytes:%d, want 3/19", packets, totalBytes)
	}

	payloads := make([]vpxrtp.PayloadFragment, packets)
	buf := make([]byte, totalBytes)
	n, used, err := PacketizeFrameInto(payloads, buf, desc, frame, mtu)
	if err != nil {
		t.Fatalf("PacketizeFrameInto: %v", err)
	}
	if n != packets || used != totalBytes {
		t.Fatalf("returned = packets:%d bytes:%d, want %d/%d",
			n, used, packets, totalBytes)
	}

	var got []byte
	for i, payload := range payloads {
		if len(payload.Payload) > mtu {
			t.Fatalf("payload %d length = %d, exceeds mtu %d", i, len(payload.Payload), mtu)
		}
		if payload.Marker != (i == len(payloads)-1) {
			t.Fatalf("payload %d marker = %v, want %v",
				i, payload.Marker, i == len(payloads)-1)
		}
		gotDesc, fragment, err := ParsePayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParsePayloadDescriptor[%d]: %v", i, err)
		}
		if gotDesc.StartOfFrame != (i == 0) {
			t.Fatalf("payload %d start = %v, want %v",
				i, gotDesc.StartOfFrame, i == 0)
		}
		if gotDesc.EndOfFrame != (i == len(payloads)-1) {
			t.Fatalf("payload %d end = %v, want %v",
				i, gotDesc.EndOfFrame, i == len(payloads)-1)
		}
		got = append(got, fragment...)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("reassembled frame = % x, want % x", got, frame)
	}
}

func TestPacketizeFrameFlexibleLayerReferences(t *testing.T) {
	desc := PayloadDescriptor{
		PictureIDPresent:      true,
		PictureID:             0x1234,
		PictureID15Bit:        true,
		InterPicturePredicted: true,
		LayerIndicesPresent:   true,
		FlexibleMode:          true,
		TemporalID:            2,
		SwitchingUpPoint:      true,
		SpatialID:             1,
		ReferenceIndexCount:   2,
		ReferenceIndices:      [MaxReferenceIndices]uint8{3, 17},
	}
	frame := []byte{0, 1, 2, 3, 4, 5, 6}
	payloads, err := PacketizeFrame(desc, frame, 9)
	if err != nil {
		t.Fatalf("PacketizeFrame: %v", err)
	}
	if len(payloads) != 3 {
		t.Fatalf("payload count = %d, want 3", len(payloads))
	}
	for i, payload := range payloads {
		gotDesc, _, err := ParsePayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParsePayloadDescriptor[%d]: %v", i, err)
		}
		wantDesc := desc
		wantDesc.StartOfFrame = i == 0
		wantDesc.EndOfFrame = i == len(payloads)-1
		if !reflect.DeepEqual(gotDesc, wantDesc) {
			t.Fatalf("payload %d descriptor = %+v, want %+v", i, gotDesc, wantDesc)
		}
	}
	got, err := AssembleFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleFrame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("assembled frame = % x, want % x", got, frame)
	}
}

func TestPacketizeFrameScalabilityStructure(t *testing.T) {
	desc := PayloadDescriptor{
		PictureIDPresent:            true,
		PictureID:                   7,
		StartOfFrame:                true,
		EndOfFrame:                  true,
		ScalabilityStructurePresent: true,
		NotRefForUpperSpatialLayer:  true,
	}
	desc.ScalabilityStructure = ScalabilityStructure{
		SpatialLayerCount: 2,
		ResolutionPresent: true,
		Width:             [MaxSpatialLayers]uint16{640, 1280},
		Height:            [MaxSpatialLayers]uint16{360, 720},
	}
	frame := []byte{0x82, 0x49, 0x83}
	payloads, err := PacketizeFrame(desc, frame, 1200)
	if err != nil {
		t.Fatalf("PacketizeFrame: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	gotDesc, _, err := ParsePayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParsePayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(gotDesc, desc) {
		t.Fatalf("descriptor = %+v, want %+v", gotDesc, desc)
	}
	got, err := AssembleFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleFrame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("assembled frame = % x, want % x", got, frame)
	}
}

func TestPacketizeFrameScalabilityStructureOnlyOnFirstFragment(t *testing.T) {
	desc := PayloadDescriptor{
		PictureIDPresent:            true,
		PictureID:                   7,
		ScalabilityStructurePresent: true,
		ScalabilityStructure: ScalabilityStructure{
			SpatialLayerCount: 2,
			ResolutionPresent: true,
			Width:             [MaxSpatialLayers]uint16{640, 1280},
			Height:            [MaxSpatialLayers]uint16{360, 720},
		},
	}
	frame := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	const mtu = 14
	packets, totalBytes, err := FramePacketizationSize(desc, frame, mtu)
	if err != nil {
		t.Fatalf("FramePacketizationSize: %v", err)
	}
	if packets != 3 || totalBytes != 35 {
		t.Fatalf("size = packets:%d bytes:%d, want 3/35", packets, totalBytes)
	}

	payloads, err := PacketizeFrame(desc, frame, mtu)
	if err != nil {
		t.Fatalf("PacketizeFrame: %v", err)
	}
	if len(payloads) != packets {
		t.Fatalf("payload count = %d, want %d", len(payloads), packets)
	}
	for i, payload := range payloads {
		if len(payload.Payload) > mtu {
			t.Fatalf("payload %d length = %d, exceeds mtu %d", i, len(payload.Payload), mtu)
		}
		gotDesc, _, err := ParsePayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParsePayloadDescriptor[%d]: %v", i, err)
		}
		if gotDesc.ScalabilityStructurePresent != (i == 0) {
			t.Fatalf("payload %d scalability structure present = %v, want %v",
				i, gotDesc.ScalabilityStructurePresent, i == 0)
		}
	}
	got, err := AssembleFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleFrame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("assembled frame = % x, want % x", got, frame)
	}

	repeated := append([]vpxrtp.PayloadFragment(nil), payloads...)
	for i := 1; i < len(repeated); i++ {
		gotDesc, fragment, err := ParsePayloadDescriptor(repeated[i].Payload)
		if err != nil {
			t.Fatalf("ParsePayloadDescriptor repeat[%d]: %v", i, err)
		}
		gotDesc.ScalabilityStructurePresent = true
		gotDesc.ScalabilityStructure = desc.ScalabilityStructure
		repeated[i].Payload = rtptest.MustPackPayload(t, gotDesc, fragment)
	}
	got, err = AssembleFrame(repeated)
	if err != nil {
		t.Fatalf("AssembleFrame repeated SS: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("assembled repeated-SS frame = % x, want % x", got, frame)
	}
}

func TestPacketizeFrameRejectsInvalidInputs(t *testing.T) {
	desc := PayloadDescriptor{PictureIDPresent: true, PictureID: 1}
	frame := []byte{0x01}
	packets, totalBytes, err := FramePacketizationSize(desc, frame, 2)
	if !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("small mtu error = %v, want vpxerrors.ErrInvalidConfig", err)
	}
	if packets != 0 || totalBytes != 0 {
		t.Fatalf("small mtu size = %d/%d, want 0/0", packets, totalBytes)
	}
	if _, _, err := FramePacketizationSize(desc, nil, 1200); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("empty frame error = %v, want vpxerrors.ErrInvalidConfig", err)
	}
	if _, _, err := FramePacketizationSize(PayloadDescriptor{FlexibleMode: true}, frame, 1200); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("invalid flex descriptor error = %v, want vpxerrors.ErrInvalidConfig", err)
	}

	packets, totalBytes, err = FramePacketizationSize(desc, frame, 1200)
	if err != nil {
		t.Fatalf("FramePacketizationSize: %v", err)
	}
	if gotPackets, gotBytes, err := PacketizeFrameInto(
		make([]vpxrtp.PayloadFragment, packets-1), make([]byte, totalBytes),
		desc, frame, 1200,
	); !errors.Is(err, vpxerrors.ErrBufferTooSmall) || gotPackets != packets || gotBytes != totalBytes {
		t.Fatalf("short dst = packets:%d bytes:%d err:%v, want %d/%d vpxerrors.ErrBufferTooSmall",
			gotPackets, gotBytes, err, packets, totalBytes)
	}
	if gotPackets, gotBytes, err := PacketizeFrameInto(
		make([]vpxrtp.PayloadFragment, packets), make([]byte, totalBytes-1),
		desc, frame, 1200,
	); !errors.Is(err, vpxerrors.ErrBufferTooSmall) || gotPackets != packets || gotBytes != totalBytes {
		t.Fatalf("short payload buffer = packets:%d bytes:%d err:%v, want %d/%d vpxerrors.ErrBufferTooSmall",
			gotPackets, gotBytes, err, packets, totalBytes)
	}
}

func TestAssembleFrameFromPacketizer(t *testing.T) {
	desc := PayloadDescriptor{
		PictureIDPresent:      true,
		PictureID:             0x1234,
		PictureID15Bit:        true,
		InterPicturePredicted: true,
	}
	frame := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	payloads, err := PacketizeFrame(desc, frame, 7)
	if err != nil {
		t.Fatalf("PacketizeFrame: %v", err)
	}
	need, err := FrameAssemblySize(payloads)
	if err != nil {
		t.Fatalf("FrameAssemblySize: %v", err)
	}
	if need != len(frame) {
		t.Fatalf("assembly size = %d, want %d", need, len(frame))
	}
	if got, err := AssembleFrameInto(make([]byte, need-1), payloads); !errors.Is(err, vpxerrors.ErrBufferTooSmall) || got != need {
		t.Fatalf("short assemble = %d/%v, want %d vpxerrors.ErrBufferTooSmall", got, err, need)
	}
	got, err := AssembleFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleFrame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("assembled frame = % x, want % x", got, frame)
	}
}

func TestAssembleFrameRejectsInvalidPayloadSequence(t *testing.T) {
	frame := []byte{0, 1, 2, 3, 4}
	payloads, err := PacketizeFrame(PayloadDescriptor{
		PictureIDPresent: true,
		PictureID:        1,
	}, frame, 4)
	if err != nil {
		t.Fatalf("PacketizeFrame: %v", err)
	}
	tests := []struct {
		name     string
		payloads []vpxrtp.PayloadFragment
	}{
		{name: "empty", payloads: nil},
		{name: "early marker", payloads: func() []vpxrtp.PayloadFragment {
			p := append([]vpxrtp.PayloadFragment(nil), payloads...)
			p[0].Marker = true
			return p
		}()},
		{name: "missing start", payloads: []vpxrtp.PayloadFragment{{
			Payload: rtptest.MustPackPayload(t, PayloadDescriptor{
				EndOfFrame: true,
			}, []byte{0x01}),
			Marker: true,
		}}},
		{name: "descriptor mismatch", payloads: func() []vpxrtp.PayloadFragment {
			p := append([]vpxrtp.PayloadFragment(nil), payloads...)
			p[1].Payload = rtptest.MustPackPayload(t, PayloadDescriptor{
				PictureIDPresent: true,
				PictureID:        2,
				EndOfFrame:       false,
			}, []byte{0x02})
			return p
		}()},
		{name: "layer descriptor mismatch", payloads: func() []vpxrtp.PayloadFragment {
			p := append([]vpxrtp.PayloadFragment(nil), payloads...)
			p[1].Payload = rtptest.MustPackPayload(t, PayloadDescriptor{
				PictureIDPresent:      true,
				PictureID:             1,
				InterPicturePredicted: true,
				LayerIndicesPresent:   true,
				TemporalID:            1,
				TL0PICIDX:             9,
				EndOfFrame:            false,
			}, []byte{0x02})
			return p
		}()},
		{name: "late scalability structure", payloads: func() []vpxrtp.PayloadFragment {
			p := append([]vpxrtp.PayloadFragment(nil), payloads...)
			desc, fragment, err := ParsePayloadDescriptor(p[1].Payload)
			if err != nil {
				t.Fatalf("ParsePayloadDescriptor: %v", err)
			}
			desc.ScalabilityStructurePresent = true
			desc.ScalabilityStructure = ScalabilityStructure{
				SpatialLayerCount: 1,
				ResolutionPresent: true,
				Width:             [MaxSpatialLayers]uint16{640},
				Height:            [MaxSpatialLayers]uint16{360},
			}
			p[1].Payload = rtptest.MustPackPayload(t, desc, fragment)
			return p
		}()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FrameAssemblySize(tc.payloads); !errors.Is(err, vpxerrors.ErrInvalidVP9Data) {
				t.Fatalf("assembly error = %v, want vpxerrors.ErrInvalidVP9Data", err)
			}
		})
	}
}

func TestPayloadDescriptorRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		desc PayloadDescriptor
	}{
		{name: "flex without picture id", desc: PayloadDescriptor{FlexibleMode: true}},
		{name: "seven bit picture id overflow", desc: PayloadDescriptor{PictureIDPresent: true, PictureID: 0x80}},
		{name: "fifteen bit picture id overflow", desc: PayloadDescriptor{PictureIDPresent: true, PictureID15Bit: true, PictureID: 0x8000}},
		{name: "flex predicted without references", desc: PayloadDescriptor{PictureIDPresent: true, InterPicturePredicted: true, FlexibleMode: true}},
		{name: "zero reference", desc: PayloadDescriptor{PictureIDPresent: true, InterPicturePredicted: true, FlexibleMode: true, ReferenceIndexCount: 1}},
		{name: "layer data without layer flag", desc: PayloadDescriptor{TemporalID: 1}},
		{name: "base layer dependency", desc: PayloadDescriptor{LayerIndicesPresent: true, InterLayerDependency: true}},
		{name: "too many spatial layers", desc: PayloadDescriptor{ScalabilityStructurePresent: true, ScalabilityStructure: ScalabilityStructure{SpatialLayerCount: 9}}},
		{name: "missing layer resolution", desc: PayloadDescriptor{ScalabilityStructurePresent: true, ScalabilityStructure: ScalabilityStructure{ResolutionPresent: true}}},
		{name: "stale scalability structure", desc: PayloadDescriptor{ScalabilityStructure: ScalabilityStructure{SpatialLayerCount: 1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.desc.Size(); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
				t.Fatalf("Size error = %v, want vpxerrors.ErrInvalidConfig", err)
			}
		})
	}
}

func TestParsePayloadDescriptorRejectsMalformed(t *testing.T) {
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
			if _, _, err := ParsePayloadDescriptor(tc.in); !errors.Is(err, vpxerrors.ErrInvalidVP9Data) {
				t.Fatalf("ParsePayloadDescriptor error = %v, want vpxerrors.ErrInvalidVP9Data", err)
			}
		})
	}
}
