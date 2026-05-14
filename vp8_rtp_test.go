package govpx

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestVP8RTPPayloadDescriptorMinimalRoundTrip(t *testing.T) {
	desc := VP8RTPPayloadDescriptor{StartOfPartition: true}
	payload := []byte{0x9d, 0x01, 0x2a}
	packet, err := PackVP8RTPPayload(desc, payload)
	if err != nil {
		t.Fatalf("PackVP8RTPPayload: %v", err)
	}
	if want := []byte{0x10, 0x9d, 0x01, 0x2a}; !bytes.Equal(packet, want) {
		t.Fatalf("packet = % x, want % x", packet, want)
	}
	got, rest, err := ParseVP8RTPPayloadDescriptor(packet)
	if err != nil {
		t.Fatalf("ParseVP8RTPPayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
	if !bytes.Equal(rest, payload) {
		t.Fatalf("payload = % x, want % x", rest, payload)
	}
}

func TestVP8RTPPayloadDescriptorPictureID(t *testing.T) {
	desc := VP8RTPPayloadDescriptor{
		StartOfPartition: true,
		PictureIDPresent: true,
		PictureID:        17,
	}
	buf, err := desc.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := []byte{0x90, 0x80, 0x11}; !bytes.Equal(buf, want) {
		t.Fatalf("descriptor bytes = % x, want % x", buf, want)
	}
	got, _, err := ParseVP8RTPPayloadDescriptor(buf)
	if err != nil {
		t.Fatalf("ParseVP8RTPPayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
}

func TestVP8RTPPayloadDescriptorExtendedRoundTrip(t *testing.T) {
	desc := VP8RTPPayloadDescriptor{
		NonReferenceFrame: true,
		StartOfPartition:  true,
		PartitionID:       3,
		PictureIDPresent:  true,
		PictureID:         4711,
		PictureID15Bit:    true,
		TL0PICIDXPresent:  true,
		TL0PICIDX:         44,
		TemporalIDPresent: true,
		TemporalID:        2,
		LayerSync:         true,
		KeyIndexPresent:   true,
		KeyIndex:          17,
	}
	buf, err := desc.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := []byte{0xb3, 0xf0, 0x92, 0x67, 0x2c, 0xb1}; !bytes.Equal(buf, want) {
		t.Fatalf("descriptor bytes = % x, want % x", buf, want)
	}
	got, rest, err := ParseVP8RTPPayloadDescriptor(append(buf, 0xaa))
	if err != nil {
		t.Fatalf("ParseVP8RTPPayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
	if want := []byte{0xaa}; !bytes.Equal(rest, want) {
		t.Fatalf("payload = % x, want % x", rest, want)
	}
}

func TestVP8RTPPayloadDescriptorKeyIndexOnly(t *testing.T) {
	desc := VP8RTPPayloadDescriptor{
		PartitionID:     7,
		LayerSync:       true,
		KeyIndexPresent: true,
		KeyIndex:        31,
	}
	buf, err := desc.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := []byte{0x87, 0x10, 0x3f}; !bytes.Equal(buf, want) {
		t.Fatalf("descriptor bytes = % x, want % x", buf, want)
	}
	got, _, err := ParseVP8RTPPayloadDescriptor(buf)
	if err != nil {
		t.Fatalf("ParseVP8RTPPayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
}

func TestPackVP8RTPPayloadInto(t *testing.T) {
	desc := VP8RTPPayloadDescriptor{StartOfPartition: true}
	payload := []byte{0x01, 0x02}
	need, err := PackVP8RTPPayloadInto(make([]byte, 1), desc, payload)
	if !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("short dst error = %v, want ErrBufferTooSmall", err)
	}
	if need != 3 {
		t.Fatalf("short dst need = %d, want 3", need)
	}
	dst := make([]byte, need)
	n, err := PackVP8RTPPayloadInto(dst, desc, payload)
	if err != nil {
		t.Fatalf("PackVP8RTPPayloadInto: %v", err)
	}
	if n != need {
		t.Fatalf("n = %d, want %d", n, need)
	}
	if want := []byte{0x10, 0x01, 0x02}; !bytes.Equal(dst, want) {
		t.Fatalf("packet = % x, want % x", dst, want)
	}
	if _, err := VP8RTPPayloadSize(desc, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("empty payload size error = %v, want ErrInvalidConfig", err)
	}
}

func TestPacketizeVP8RTPFrameSinglePayload(t *testing.T) {
	desc := VP8RTPPayloadDescriptor{
		PictureIDPresent: true,
		PictureID:        17,
	}
	frame := []byte{0x9d, 0x01, 0x2a, 0x88}
	payloads, err := PacketizeVP8RTPFrame(desc, frame, 1200)
	if err != nil {
		t.Fatalf("PacketizeVP8RTPFrame: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	if !payloads[0].Marker {
		t.Fatal("single payload marker = false, want true")
	}
	gotDesc, gotFrame, err := ParseVP8RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP8RTPPayloadDescriptor: %v", err)
	}
	if !gotDesc.StartOfPartition || gotDesc.PartitionID != 0 {
		t.Fatalf("descriptor start/partition = %v/%d, want true/0",
			gotDesc.StartOfPartition, gotDesc.PartitionID)
	}
	if gotDesc.PictureID != desc.PictureID || !gotDesc.PictureIDPresent {
		t.Fatalf("picture id = %d present=%v, want %d true",
			gotDesc.PictureID, gotDesc.PictureIDPresent, desc.PictureID)
	}
	if !bytes.Equal(gotFrame, frame) {
		t.Fatalf("reassembled frame = % x, want % x", gotFrame, frame)
	}
}

func TestPacketizeVP8RTPFrameIntoFragmentsByMTU(t *testing.T) {
	desc := VP8RTPPayloadDescriptor{
		PictureIDPresent: true,
		PictureID:        0x1234,
		PictureID15Bit:   true,
	}
	frame := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	const mtu = 7
	packets, totalBytes, err := VP8RTPFramePacketizationSize(desc, frame, mtu)
	if err != nil {
		t.Fatalf("VP8RTPFramePacketizationSize: %v", err)
	}
	if packets != 4 || totalBytes != 26 {
		t.Fatalf("size = packets:%d bytes:%d, want 4/26", packets, totalBytes)
	}

	payloads := make([]RTPPayloadFragment, packets)
	buf := make([]byte, totalBytes)
	n, used, err := PacketizeVP8RTPFrameInto(payloads, buf, desc, frame, mtu)
	if err != nil {
		t.Fatalf("PacketizeVP8RTPFrameInto: %v", err)
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
		gotDesc, fragment, err := ParseVP8RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP8RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if gotDesc.StartOfPartition != (i == 0) {
			t.Fatalf("payload %d start = %v, want %v",
				i, gotDesc.StartOfPartition, i == 0)
		}
		if gotDesc.PartitionID != 0 {
			t.Fatalf("payload %d partition = %d, want 0", i, gotDesc.PartitionID)
		}
		got = append(got, fragment...)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("reassembled frame = % x, want % x", got, frame)
	}
}

func TestPacketizeVP8RTPFrameRejectsInvalidInputs(t *testing.T) {
	desc := VP8RTPPayloadDescriptor{PictureIDPresent: true, PictureID: 1}
	frame := []byte{0x01}
	packets, totalBytes, err := VP8RTPFramePacketizationSize(desc, frame, 3)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("small mtu error = %v, want ErrInvalidConfig", err)
	}
	if packets != 0 || totalBytes != 0 {
		t.Fatalf("small mtu size = %d/%d, want 0/0", packets, totalBytes)
	}
	if _, _, err := VP8RTPFramePacketizationSize(desc, nil, 1200); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("empty frame error = %v, want ErrInvalidConfig", err)
	}
	if _, _, err := VP8RTPFramePacketizationSize(VP8RTPPayloadDescriptor{PartitionID: 1}, frame, 1200); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("partition id error = %v, want ErrInvalidConfig", err)
	}

	packets, totalBytes, err = VP8RTPFramePacketizationSize(desc, frame, 1200)
	if err != nil {
		t.Fatalf("VP8RTPFramePacketizationSize: %v", err)
	}
	if gotPackets, gotBytes, err := PacketizeVP8RTPFrameInto(
		make([]RTPPayloadFragment, packets-1), make([]byte, totalBytes),
		desc, frame, 1200,
	); !errors.Is(err, ErrBufferTooSmall) || gotPackets != packets || gotBytes != totalBytes {
		t.Fatalf("short dst = packets:%d bytes:%d err:%v, want %d/%d ErrBufferTooSmall",
			gotPackets, gotBytes, err, packets, totalBytes)
	}
	if gotPackets, gotBytes, err := PacketizeVP8RTPFrameInto(
		make([]RTPPayloadFragment, packets), make([]byte, totalBytes-1),
		desc, frame, 1200,
	); !errors.Is(err, ErrBufferTooSmall) || gotPackets != packets || gotBytes != totalBytes {
		t.Fatalf("short payload buffer = packets:%d bytes:%d err:%v, want %d/%d ErrBufferTooSmall",
			gotPackets, gotBytes, err, packets, totalBytes)
	}
}

func TestVP8RTPPayloadDescriptorRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		desc VP8RTPPayloadDescriptor
	}{
		{name: "partition id overflow", desc: VP8RTPPayloadDescriptor{PartitionID: 8}},
		{name: "fifteen bit without picture id", desc: VP8RTPPayloadDescriptor{PictureID15Bit: true}},
		{name: "seven bit picture id overflow", desc: VP8RTPPayloadDescriptor{PictureIDPresent: true, PictureID: 0x80}},
		{name: "fifteen bit picture id overflow", desc: VP8RTPPayloadDescriptor{PictureIDPresent: true, PictureID15Bit: true, PictureID: 0x8000}},
		{name: "tl0 without temporal id", desc: VP8RTPPayloadDescriptor{TL0PICIDXPresent: true}},
		{name: "stale tl0", desc: VP8RTPPayloadDescriptor{TL0PICIDX: 1}},
		{name: "temporal id overflow", desc: VP8RTPPayloadDescriptor{TemporalIDPresent: true, TemporalID: 4}},
		{name: "stale temporal id", desc: VP8RTPPayloadDescriptor{TemporalID: 1}},
		{name: "layer sync without tk byte", desc: VP8RTPPayloadDescriptor{LayerSync: true}},
		{name: "key index overflow", desc: VP8RTPPayloadDescriptor{KeyIndexPresent: true, KeyIndex: 32}},
		{name: "stale key index", desc: VP8RTPPayloadDescriptor{KeyIndex: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.desc.Size(); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Size error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestParseVP8RTPPayloadDescriptorRejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
	}{
		{name: "empty"},
		{name: "truncated extension", in: []byte{0x80}},
		{name: "truncated picture id", in: []byte{0x80, 0x80}},
		{name: "truncated extended picture id", in: []byte{0x80, 0x80, 0x80}},
		{name: "tl0 without temporal id", in: []byte{0x80, 0x40, 0x00}},
		{name: "truncated tl0", in: []byte{0x80, 0x60}},
		{name: "truncated temporal key index", in: []byte{0x80, 0x20}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ParseVP8RTPPayloadDescriptor(tc.in); !errors.Is(err, ErrInvalidData) {
				t.Fatalf("ParseVP8RTPPayloadDescriptor error = %v, want ErrInvalidData", err)
			}
		})
	}
}
