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
