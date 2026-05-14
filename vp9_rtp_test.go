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

func TestPacketizeVP9RTPFrameSinglePayload(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{
		PictureIDPresent:      true,
		PictureID:             41,
		InterPicturePredicted: true,
	}
	frame := []byte{0x82, 0x49, 0x83, 0x10}
	payloads, err := PacketizeVP9RTPFrame(desc, frame, 1200)
	if err != nil {
		t.Fatalf("PacketizeVP9RTPFrame: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	if !payloads[0].Marker {
		t.Fatal("single payload marker = false, want true")
	}
	gotDesc, gotFrame, err := ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
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

func TestPacketizeVP9RTPFrameIntoFragmentsByMTU(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{
		PictureIDPresent: true,
		PictureID:        0x1234,
		PictureID15Bit:   true,
	}
	frame := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	const mtu = 7
	packets, totalBytes, err := VP9RTPFramePacketizationSize(desc, frame, mtu)
	if err != nil {
		t.Fatalf("VP9RTPFramePacketizationSize: %v", err)
	}
	if packets != 3 || totalBytes != 19 {
		t.Fatalf("size = packets:%d bytes:%d, want 3/19", packets, totalBytes)
	}

	payloads := make([]RTPPayloadFragment, packets)
	buf := make([]byte, totalBytes)
	n, used, err := PacketizeVP9RTPFrameInto(payloads, buf, desc, frame, mtu)
	if err != nil {
		t.Fatalf("PacketizeVP9RTPFrameInto: %v", err)
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
		gotDesc, fragment, err := ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
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

func TestPacketizeVP9RTPFrameFlexibleLayerReferences(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{
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
		ReferenceIndices:      [VP9RTPMaxReferenceIndices]uint8{3, 17},
	}
	frame := []byte{0, 1, 2, 3, 4, 5, 6}
	payloads, err := PacketizeVP9RTPFrame(desc, frame, 9)
	if err != nil {
		t.Fatalf("PacketizeVP9RTPFrame: %v", err)
	}
	if len(payloads) != 3 {
		t.Fatalf("payload count = %d, want 3", len(payloads))
	}
	for i, payload := range payloads {
		gotDesc, _, err := ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		wantDesc := desc
		wantDesc.StartOfFrame = i == 0
		wantDesc.EndOfFrame = i == len(payloads)-1
		if !reflect.DeepEqual(gotDesc, wantDesc) {
			t.Fatalf("payload %d descriptor = %+v, want %+v", i, gotDesc, wantDesc)
		}
	}
	got, err := AssembleVP9RTPFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleVP9RTPFrame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("assembled frame = % x, want % x", got, frame)
	}
}

func TestPacketizeVP9RTPFrameScalabilityStructure(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{
		PictureIDPresent:            true,
		PictureID:                   7,
		StartOfFrame:                true,
		EndOfFrame:                  true,
		ScalabilityStructurePresent: true,
		NotRefForUpperSpatialLayer:  true,
	}
	desc.ScalabilityStructure = VP9RTPScalabilityStructure{
		SpatialLayerCount: 2,
		ResolutionPresent: true,
		Width:             [VP9RTPMaxSpatialLayers]uint16{640, 1280},
		Height:            [VP9RTPMaxSpatialLayers]uint16{360, 720},
	}
	frame := []byte{0x82, 0x49, 0x83}
	payloads, err := PacketizeVP9RTPFrame(desc, frame, 1200)
	if err != nil {
		t.Fatalf("PacketizeVP9RTPFrame: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	gotDesc, _, err := ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(gotDesc, desc) {
		t.Fatalf("descriptor = %+v, want %+v", gotDesc, desc)
	}
	got, err := AssembleVP9RTPFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleVP9RTPFrame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("assembled frame = % x, want % x", got, frame)
	}
}

func TestPacketizeVP9RTPFrameScalabilityStructureOnlyOnFirstFragment(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{
		PictureIDPresent:            true,
		PictureID:                   7,
		ScalabilityStructurePresent: true,
		ScalabilityStructure: VP9RTPScalabilityStructure{
			SpatialLayerCount: 2,
			ResolutionPresent: true,
			Width:             [VP9RTPMaxSpatialLayers]uint16{640, 1280},
			Height:            [VP9RTPMaxSpatialLayers]uint16{360, 720},
		},
	}
	frame := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	const mtu = 14
	packets, totalBytes, err := VP9RTPFramePacketizationSize(desc, frame, mtu)
	if err != nil {
		t.Fatalf("VP9RTPFramePacketizationSize: %v", err)
	}
	if packets != 3 || totalBytes != 35 {
		t.Fatalf("size = packets:%d bytes:%d, want 3/35", packets, totalBytes)
	}

	payloads, err := PacketizeVP9RTPFrame(desc, frame, mtu)
	if err != nil {
		t.Fatalf("PacketizeVP9RTPFrame: %v", err)
	}
	if len(payloads) != packets {
		t.Fatalf("payload count = %d, want %d", len(payloads), packets)
	}
	for i, payload := range payloads {
		if len(payload.Payload) > mtu {
			t.Fatalf("payload %d length = %d, exceeds mtu %d", i, len(payload.Payload), mtu)
		}
		gotDesc, _, err := ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if gotDesc.ScalabilityStructurePresent != (i == 0) {
			t.Fatalf("payload %d scalability structure present = %v, want %v",
				i, gotDesc.ScalabilityStructurePresent, i == 0)
		}
	}
	got, err := AssembleVP9RTPFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleVP9RTPFrame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("assembled frame = % x, want % x", got, frame)
	}

	repeated := append([]RTPPayloadFragment(nil), payloads...)
	for i := 1; i < len(repeated); i++ {
		gotDesc, fragment, err := ParseVP9RTPPayloadDescriptor(repeated[i].Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor repeat[%d]: %v", i, err)
		}
		gotDesc.ScalabilityStructurePresent = true
		gotDesc.ScalabilityStructure = desc.ScalabilityStructure
		repeated[i].Payload = mustPackVP9RTPPayloadForTest(t, gotDesc, fragment)
	}
	got, err = AssembleVP9RTPFrame(repeated)
	if err != nil {
		t.Fatalf("AssembleVP9RTPFrame repeated SS: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("assembled repeated-SS frame = % x, want % x", got, frame)
	}
}

func TestVP9RTPPacketizeAssembleEncodedFrame(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	frame, err := e.Encode(newVP9CheckerYCbCrForTest(width, height, 32, 224, 96, 192))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	desc := VP9RTPPayloadDescriptor{
		PictureIDPresent:            true,
		PictureID:                   17,
		ScalabilityStructurePresent: true,
		ScalabilityStructure: VP9RTPScalabilityStructure{
			SpatialLayerCount: 1,
			ResolutionPresent: true,
			Width:             [VP9RTPMaxSpatialLayers]uint16{width},
			Height:            [VP9RTPMaxSpatialLayers]uint16{height},
		},
	}
	payloads, err := PacketizeVP9RTPFrame(desc, frame, 64)
	if err != nil {
		t.Fatalf("PacketizeVP9RTPFrame: %v", err)
	}
	if len(payloads) < 2 {
		t.Fatalf("payload count = %d, want fragmented encoded frame", len(payloads))
	}
	assembled, err := AssembleVP9RTPFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleVP9RTPFrame: %v", err)
	}
	if !bytes.Equal(assembled, frame) {
		t.Fatal("assembled RTP frame does not match encoded VP9 payload")
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(assembled); err != nil {
		t.Fatalf("Decode assembled frame: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("Decode assembled frame produced no visible output")
	}
}

func TestPacketizeVP9RTPFrameRejectsInvalidInputs(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{PictureIDPresent: true, PictureID: 1}
	frame := []byte{0x01}
	packets, totalBytes, err := VP9RTPFramePacketizationSize(desc, frame, 2)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("small mtu error = %v, want ErrInvalidConfig", err)
	}
	if packets != 0 || totalBytes != 0 {
		t.Fatalf("small mtu size = %d/%d, want 0/0", packets, totalBytes)
	}
	if _, _, err := VP9RTPFramePacketizationSize(desc, nil, 1200); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("empty frame error = %v, want ErrInvalidConfig", err)
	}
	if _, _, err := VP9RTPFramePacketizationSize(VP9RTPPayloadDescriptor{FlexibleMode: true}, frame, 1200); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid flex descriptor error = %v, want ErrInvalidConfig", err)
	}

	packets, totalBytes, err = VP9RTPFramePacketizationSize(desc, frame, 1200)
	if err != nil {
		t.Fatalf("VP9RTPFramePacketizationSize: %v", err)
	}
	if gotPackets, gotBytes, err := PacketizeVP9RTPFrameInto(
		make([]RTPPayloadFragment, packets-1), make([]byte, totalBytes),
		desc, frame, 1200,
	); !errors.Is(err, ErrBufferTooSmall) || gotPackets != packets || gotBytes != totalBytes {
		t.Fatalf("short dst = packets:%d bytes:%d err:%v, want %d/%d ErrBufferTooSmall",
			gotPackets, gotBytes, err, packets, totalBytes)
	}
	if gotPackets, gotBytes, err := PacketizeVP9RTPFrameInto(
		make([]RTPPayloadFragment, packets), make([]byte, totalBytes-1),
		desc, frame, 1200,
	); !errors.Is(err, ErrBufferTooSmall) || gotPackets != packets || gotBytes != totalBytes {
		t.Fatalf("short payload buffer = packets:%d bytes:%d err:%v, want %d/%d ErrBufferTooSmall",
			gotPackets, gotBytes, err, packets, totalBytes)
	}
}

func TestAssembleVP9RTPFrameFromPacketizer(t *testing.T) {
	desc := VP9RTPPayloadDescriptor{
		PictureIDPresent:      true,
		PictureID:             0x1234,
		PictureID15Bit:        true,
		InterPicturePredicted: true,
	}
	frame := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	payloads, err := PacketizeVP9RTPFrame(desc, frame, 7)
	if err != nil {
		t.Fatalf("PacketizeVP9RTPFrame: %v", err)
	}
	need, err := VP9RTPFrameAssemblySize(payloads)
	if err != nil {
		t.Fatalf("VP9RTPFrameAssemblySize: %v", err)
	}
	if need != len(frame) {
		t.Fatalf("assembly size = %d, want %d", need, len(frame))
	}
	if got, err := AssembleVP9RTPFrameInto(make([]byte, need-1), payloads); !errors.Is(err, ErrBufferTooSmall) || got != need {
		t.Fatalf("short assemble = %d/%v, want %d ErrBufferTooSmall", got, err, need)
	}
	got, err := AssembleVP9RTPFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleVP9RTPFrame: %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("assembled frame = % x, want % x", got, frame)
	}
}

func TestAssembleVP9RTPFrameRejectsInvalidPayloadSequence(t *testing.T) {
	frame := []byte{0, 1, 2, 3, 4}
	payloads, err := PacketizeVP9RTPFrame(VP9RTPPayloadDescriptor{
		PictureIDPresent: true,
		PictureID:        1,
	}, frame, 4)
	if err != nil {
		t.Fatalf("PacketizeVP9RTPFrame: %v", err)
	}
	tests := []struct {
		name     string
		payloads []RTPPayloadFragment
	}{
		{name: "empty", payloads: nil},
		{name: "early marker", payloads: func() []RTPPayloadFragment {
			p := append([]RTPPayloadFragment(nil), payloads...)
			p[0].Marker = true
			return p
		}()},
		{name: "missing start", payloads: []RTPPayloadFragment{{
			Payload: mustPackVP9RTPPayloadForTest(t, VP9RTPPayloadDescriptor{
				EndOfFrame: true,
			}, []byte{0x01}),
			Marker: true,
		}}},
		{name: "descriptor mismatch", payloads: func() []RTPPayloadFragment {
			p := append([]RTPPayloadFragment(nil), payloads...)
			p[1].Payload = mustPackVP9RTPPayloadForTest(t, VP9RTPPayloadDescriptor{
				PictureIDPresent: true,
				PictureID:        2,
				EndOfFrame:       false,
			}, []byte{0x02})
			return p
		}()},
		{name: "layer descriptor mismatch", payloads: func() []RTPPayloadFragment {
			p := append([]RTPPayloadFragment(nil), payloads...)
			p[1].Payload = mustPackVP9RTPPayloadForTest(t, VP9RTPPayloadDescriptor{
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
		{name: "late scalability structure", payloads: func() []RTPPayloadFragment {
			p := append([]RTPPayloadFragment(nil), payloads...)
			desc, fragment, err := ParseVP9RTPPayloadDescriptor(p[1].Payload)
			if err != nil {
				t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
			}
			desc.ScalabilityStructurePresent = true
			desc.ScalabilityStructure = VP9RTPScalabilityStructure{
				SpatialLayerCount: 1,
				ResolutionPresent: true,
				Width:             [VP9RTPMaxSpatialLayers]uint16{640},
				Height:            [VP9RTPMaxSpatialLayers]uint16{360},
			}
			p[1].Payload = mustPackVP9RTPPayloadForTest(t, desc, fragment)
			return p
		}()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := VP9RTPFrameAssemblySize(tc.payloads); !errors.Is(err, ErrInvalidVP9Data) {
				t.Fatalf("assembly error = %v, want ErrInvalidVP9Data", err)
			}
		})
	}
}

func mustPackVP9RTPPayloadForTest(t *testing.T, desc VP9RTPPayloadDescriptor, payload []byte) []byte {
	t.Helper()
	packet, err := PackVP9RTPPayload(desc, payload)
	if err != nil {
		t.Fatalf("PackVP9RTPPayload: %v", err)
	}
	return packet
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
