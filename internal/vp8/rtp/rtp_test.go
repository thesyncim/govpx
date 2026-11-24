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
	desc := PayloadDescriptor{StartOfPartition: true}
	payload := []byte{0x9d, 0x01, 0x2a}
	packet, err := vpxrtp.PackPayload(desc, payload)
	if err != nil {
		t.Fatalf("vpxrtp.PackPayload: %v", err)
	}
	if want := []byte{0x10, 0x9d, 0x01, 0x2a}; !bytes.Equal(packet, want) {
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

func TestPayloadDescriptorPictureID(t *testing.T) {
	desc := PayloadDescriptor{
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
	got, _, err := ParsePayloadDescriptor(buf)
	if err != nil {
		t.Fatalf("ParsePayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
}

func TestPayloadDescriptorExtendedRoundTrip(t *testing.T) {
	desc := PayloadDescriptor{
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

func TestPayloadDescriptorKeyIndexOnly(t *testing.T) {
	desc := PayloadDescriptor{
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
	got, _, err := ParsePayloadDescriptor(buf)
	if err != nil {
		t.Fatalf("ParsePayloadDescriptor: %v", err)
	}
	if !reflect.DeepEqual(got, desc) {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
}

func TestPackPayloadInto(t *testing.T) {
	desc := PayloadDescriptor{StartOfPartition: true}
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
	if want := []byte{0x10, 0x01, 0x02}; !bytes.Equal(dst, want) {
		t.Fatalf("packet = % x, want % x", dst, want)
	}
	if _, err := vpxrtp.PayloadSize(desc, nil); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("empty payload size error = %v, want vpxerrors.ErrInvalidConfig", err)
	}
}

func TestPacketizeFrameSinglePayload(t *testing.T) {
	desc := PayloadDescriptor{
		PictureIDPresent: true,
		PictureID:        17,
	}
	frame := []byte{0x9d, 0x01, 0x2a, 0x88}
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
	if packets != 4 || totalBytes != 26 {
		t.Fatalf("size = packets:%d bytes:%d, want 4/26", packets, totalBytes)
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

func TestPacketizeFrameRejectsInvalidInputs(t *testing.T) {
	desc := PayloadDescriptor{PictureIDPresent: true, PictureID: 1}
	frame := []byte{0x01}
	packets, totalBytes, err := FramePacketizationSize(desc, frame, 3)
	if !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("small mtu error = %v, want vpxerrors.ErrInvalidConfig", err)
	}
	if packets != 0 || totalBytes != 0 {
		t.Fatalf("small mtu size = %d/%d, want 0/0", packets, totalBytes)
	}
	if _, _, err := FramePacketizationSize(desc, nil, 1200); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("empty frame error = %v, want vpxerrors.ErrInvalidConfig", err)
	}
	if _, _, err := FramePacketizationSize(PayloadDescriptor{PartitionID: 1}, frame, 1200); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("partition id error = %v, want vpxerrors.ErrInvalidConfig", err)
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
		PictureIDPresent: true,
		PictureID:        0x1234,
		PictureID15Bit:   true,
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

func TestAssembleFrameAcceptsPartitionAwarePayloads(t *testing.T) {
	frame := []byte{0, 1, 2, 3, 4, 5}
	payloads := []vpxrtp.PayloadFragment{
		{
			Payload: rtptest.MustPackPayload(t, PayloadDescriptor{
				StartOfPartition:  true,
				PartitionID:       0,
				PictureIDPresent:  true,
				PictureID:         0x1234,
				PictureID15Bit:    true,
				TL0PICIDXPresent:  true,
				TL0PICIDX:         9,
				TemporalIDPresent: true,
				TemporalID:        1,
				LayerSync:         true,
			}, frame[:2]),
		},
		{
			Payload: rtptest.MustPackPayload(t, PayloadDescriptor{
				StartOfPartition:  true,
				PartitionID:       1,
				PictureIDPresent:  true,
				PictureID:         0x1234,
				PictureID15Bit:    true,
				TL0PICIDXPresent:  true,
				TL0PICIDX:         9,
				TemporalIDPresent: true,
				TemporalID:        1,
				LayerSync:         true,
			}, frame[2:4]),
		},
		{
			Payload: rtptest.MustPackPayload(t, PayloadDescriptor{
				PartitionID:       1,
				PictureIDPresent:  true,
				PictureID:         0x1234,
				PictureID15Bit:    true,
				TL0PICIDXPresent:  true,
				TL0PICIDX:         9,
				TemporalIDPresent: true,
				TemporalID:        1,
				LayerSync:         true,
			}, frame[4:]),
			Marker: true,
		},
	}
	need, err := FrameAssemblySize(payloads)
	if err != nil {
		t.Fatalf("FrameAssemblySize: %v", err)
	}
	if need != len(frame) {
		t.Fatalf("assembly size = %d, want %d", need, len(frame))
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
				PictureIDPresent: true,
				PictureID:        1,
			}, []byte{0x01}),
			Marker: true,
		}}},
		{name: "descriptor mismatch", payloads: func() []vpxrtp.PayloadFragment {
			p := append([]vpxrtp.PayloadFragment(nil), payloads...)
			p[1].Payload = rtptest.MustPackPayload(t, PayloadDescriptor{
				PictureIDPresent: true,
				PictureID:        2,
			}, []byte{0x02})
			return p
		}()},
		{name: "duplicate partition start", payloads: []vpxrtp.PayloadFragment{
			{
				Payload: rtptest.MustPackPayload(t, PayloadDescriptor{
					StartOfPartition: true,
					PartitionID:      0,
				}, []byte{0x01}),
			},
			{
				Payload: rtptest.MustPackPayload(t, PayloadDescriptor{
					StartOfPartition: true,
					PartitionID:      0,
				}, []byte{0x02}),
				Marker: true,
			},
		}},
		{name: "backward partition id", payloads: []vpxrtp.PayloadFragment{
			{
				Payload: rtptest.MustPackPayload(t, PayloadDescriptor{
					StartOfPartition: true,
					PartitionID:      0,
				}, []byte{0x01}),
			},
			{
				Payload: rtptest.MustPackPayload(t, PayloadDescriptor{
					StartOfPartition: true,
					PartitionID:      2,
				}, []byte{0x02}),
			},
			{
				Payload: rtptest.MustPackPayload(t, PayloadDescriptor{
					PartitionID: 1,
				}, []byte{0x03}),
				Marker: true,
			},
		}},
		{name: "missing later partition start", payloads: []vpxrtp.PayloadFragment{
			{
				Payload: rtptest.MustPackPayload(t, PayloadDescriptor{
					StartOfPartition: true,
					PartitionID:      0,
				}, []byte{0x01}),
			},
			{
				Payload: rtptest.MustPackPayload(t, PayloadDescriptor{
					PartitionID: 1,
				}, []byte{0x02}),
				Marker: true,
			},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FrameAssemblySize(tc.payloads); !errors.Is(err, vpxerrors.ErrInvalidData) {
				t.Fatalf("assembly error = %v, want vpxerrors.ErrInvalidData", err)
			}
		})
	}
}

func TestPayloadDescriptorRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		desc PayloadDescriptor
	}{
		{name: "partition id overflow", desc: PayloadDescriptor{PartitionID: 8}},
		{name: "fifteen bit without picture id", desc: PayloadDescriptor{PictureID15Bit: true}},
		{name: "seven bit picture id overflow", desc: PayloadDescriptor{PictureIDPresent: true, PictureID: 0x80}},
		{name: "fifteen bit picture id overflow", desc: PayloadDescriptor{PictureIDPresent: true, PictureID15Bit: true, PictureID: 0x8000}},
		{name: "tl0 without temporal id", desc: PayloadDescriptor{TL0PICIDXPresent: true}},
		{name: "stale tl0", desc: PayloadDescriptor{TL0PICIDX: 1}},
		{name: "temporal id overflow", desc: PayloadDescriptor{TemporalIDPresent: true, TemporalID: 4}},
		{name: "stale temporal id", desc: PayloadDescriptor{TemporalID: 1}},
		{name: "layer sync without tk byte", desc: PayloadDescriptor{LayerSync: true}},
		{name: "key index overflow", desc: PayloadDescriptor{KeyIndexPresent: true, KeyIndex: 32}},
		{name: "stale key index", desc: PayloadDescriptor{KeyIndex: 1}},
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
		{name: "truncated extension", in: []byte{0x80}},
		{name: "truncated picture id", in: []byte{0x80, 0x80}},
		{name: "truncated extended picture id", in: []byte{0x80, 0x80, 0x80}},
		{name: "tl0 without temporal id", in: []byte{0x80, 0x40, 0x00}},
		{name: "truncated tl0", in: []byte{0x80, 0x60}},
		{name: "truncated temporal key index", in: []byte{0x80, 0x20}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ParsePayloadDescriptor(tc.in); !errors.Is(err, vpxerrors.ErrInvalidData) {
				t.Fatalf("ParsePayloadDescriptor error = %v, want vpxerrors.ErrInvalidData", err)
			}
		})
	}
}
