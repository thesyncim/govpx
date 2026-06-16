package govpx_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func TestVP8RTPFacadePayloadRoundTrip(t *testing.T) {
	desc := govpx.VP8RTPPayloadDescriptor{StartOfPartition: true}
	payload := []byte{0x9d, 0x01, 0x2a}
	packet, err := govpx.PackVP8RTPPayload(desc, payload)
	if err != nil {
		t.Fatalf("PackVP8RTPPayload: %v", err)
	}
	got, rest, err := govpx.ParseVP8RTPPayloadDescriptor(packet)
	if err != nil {
		t.Fatalf("ParseVP8RTPPayloadDescriptor: %v", err)
	}
	if got != desc {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
	if !bytes.Equal(rest, payload) {
		t.Fatalf("payload = % x, want % x", rest, payload)
	}
	if _, err := govpx.VP8RTPPayloadSize(desc, nil); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("empty payload size error = %v, want ErrInvalidConfig", err)
	}
}

func TestVP8EncodeResultPacketizeWebRTCRTP(t *testing.T) {
	frame := []byte{0x10, 0x20, 0x30, 0x40, 0x50}
	result := govpx.EncodeResult{
		Data:               frame,
		Droppable:          true,
		TemporalLayerID:    1,
		TemporalLayerCount: 2,
		TemporalLayerSync:  true,
		TL0PICIDX:          9,
	}
	pictureID := uint16(0x8123)
	if got := govpx.NextVP8RTPPictureID(govpx.VP8RTPPictureID15BitMask); got != 0 {
		t.Fatalf("NextVP8RTPPictureID wrap = %d, want 0", got)
	}
	desc := result.WebRTCRTPPayloadDescriptor(pictureID)
	if !desc.PictureIDPresent || !desc.PictureID15Bit ||
		desc.PictureID != pictureID&govpx.VP8RTPPictureID15BitMask ||
		!desc.TL0PICIDXPresent || desc.TL0PICIDX != 9 ||
		!desc.TemporalIDPresent || desc.TemporalID != 1 ||
		!desc.LayerSync || !desc.NonReferenceFrame {
		t.Fatalf("WebRTC descriptor = %+v", desc)
	}

	const mtu = 7
	packets, payloadBytes, err := result.WebRTCRTPPacketizationSize(pictureID, mtu)
	if err != nil {
		t.Fatalf("WebRTCRTPPacketizationSize returned error: %v", err)
	}
	if packets < 2 {
		t.Fatalf("packets = %d, want fragmentation at mtu %d", packets, mtu)
	}
	shortPackets := make([]govpx.RTPPayloadFragment, packets-1)
	payloadBuf := make([]byte, payloadBytes)
	if needPackets, needBytes, err := result.PacketizeWebRTCRTPInto(shortPackets,
		payloadBuf, pictureID, mtu); !errors.Is(err, govpx.ErrBufferTooSmall) ||
		needPackets != packets || needBytes != payloadBytes {
		t.Fatalf("short PacketizeWebRTCRTPInto = packets:%d bytes:%d err:%v, want %d/%d ErrBufferTooSmall",
			needPackets, needBytes, err, packets, payloadBytes)
	}

	payloads := make([]govpx.RTPPayloadFragment, packets)
	n, used, err := result.PacketizeWebRTCRTPInto(payloads, payloadBuf, pictureID, mtu)
	if err != nil {
		t.Fatalf("PacketizeWebRTCRTPInto returned error: %v", err)
	}
	if n != packets || used != payloadBytes {
		t.Fatalf("PacketizeWebRTCRTPInto returned %d/%d, want %d/%d",
			n, used, packets, payloadBytes)
	}
	for i, payload := range payloads {
		gotDesc, _, err := govpx.ParseVP8RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP8RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if gotDesc.PictureID != desc.PictureID ||
			gotDesc.TL0PICIDX != desc.TL0PICIDX ||
			gotDesc.TemporalID != desc.TemporalID ||
			gotDesc.LayerSync != desc.LayerSync ||
			gotDesc.NonReferenceFrame != desc.NonReferenceFrame {
			t.Fatalf("payload %d descriptor = %+v, want picture/temporal/nonref from %+v",
				i, gotDesc, desc)
		}
		if gotDesc.StartOfPartition != (i == 0) {
			t.Fatalf("payload %d start = %t, want %t", i, gotDesc.StartOfPartition, i == 0)
		}
		if payload.Marker != (i == packets-1) {
			t.Fatalf("payload %d marker = %t, want %t", i, payload.Marker, i == packets-1)
		}
	}
	assembled, err := govpx.AssembleVP8RTPFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleVP8RTPFrame returned error: %v", err)
	}
	if !bytes.Equal(assembled, frame) {
		t.Fatalf("assembled frame = % x, want % x", assembled, frame)
	}

	allocated, err := result.PacketizeWebRTCRTP(pictureID, mtu)
	if err != nil {
		t.Fatalf("PacketizeWebRTCRTP returned error: %v", err)
	}
	if len(allocated) != packets {
		t.Fatalf("allocated payloads = %d, want %d", len(allocated), packets)
	}
}

func TestVP8RTPFacadeAssemblePartitionAwarePayloads(t *testing.T) {
	frame := []byte{0, 1, 2, 3, 4, 5}
	desc := govpx.VP8RTPPayloadDescriptor{
		PictureIDPresent:  true,
		PictureID:         0x1234,
		PictureID15Bit:    true,
		TL0PICIDXPresent:  true,
		TL0PICIDX:         9,
		TemporalIDPresent: true,
		TemporalID:        1,
		LayerSync:         true,
	}
	first := desc
	first.StartOfPartition = true
	first.PartitionID = 0
	firstPacket, err := govpx.PackVP8RTPPayload(first, frame[:2])
	if err != nil {
		t.Fatalf("PackVP8RTPPayload first: %v", err)
	}
	second := desc
	second.StartOfPartition = true
	second.PartitionID = 1
	secondPacket, err := govpx.PackVP8RTPPayload(second, frame[2:4])
	if err != nil {
		t.Fatalf("PackVP8RTPPayload second: %v", err)
	}
	third := desc
	third.PartitionID = 1
	thirdPacket, err := govpx.PackVP8RTPPayload(third, frame[4:])
	if err != nil {
		t.Fatalf("PackVP8RTPPayload third: %v", err)
	}
	payloads := []govpx.RTPPayloadFragment{
		{Payload: firstPacket},
		{Payload: secondPacket},
		{Payload: thirdPacket, Marker: true},
	}
	need, err := govpx.VP8RTPFrameAssemblySize(payloads)
	if err != nil {
		t.Fatalf("VP8RTPFrameAssemblySize returned error: %v", err)
	}
	if need != len(frame) {
		t.Fatalf("assembly size = %d, want %d", need, len(frame))
	}
	assembled, err := govpx.AssembleVP8RTPFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleVP8RTPFrame returned error: %v", err)
	}
	if !bytes.Equal(assembled, frame) {
		t.Fatalf("assembled frame = % x, want % x", assembled, frame)
	}
}

func TestVP8EncodeResultPacketizeWebRTCRTPValidation(t *testing.T) {
	if _, _, err := (govpx.EncodeResult{
		Dropped:            true,
		TemporalLayerCount: 1,
	}).WebRTCRTPPacketizationSize(1, 1200); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("dropped result error = %v, want ErrInvalidConfig", err)
	}
	if _, _, err := (govpx.EncodeResult{
		Data:               []byte{1},
		TemporalLayerID:    4,
		TemporalLayerCount: 5,
	}).WebRTCRTPPacketizationSize(1, 1200); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("five-layer RTP error = %v, want ErrInvalidConfig", err)
	}
}

func TestVP8DecoderDecodeRTP(t *testing.T) {
	frame := vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)
	payloads, err := (govpx.EncodeResult{
		Data:               frame,
		TemporalLayerID:    0,
		TemporalLayerCount: 1,
	}).PacketizeWebRTCRTP(123, 17)
	if err != nil {
		t.Fatalf("PacketizeWebRTCRTP returned error: %v", err)
	}
	need, err := govpx.VP8RTPFrameAssemblySize(payloads)
	if err != nil {
		t.Fatalf("VP8RTPFrameAssemblySize returned error: %v", err)
	}
	if need != len(frame) {
		t.Fatalf("assembly size = %d, want raw frame size %d", need, len(frame))
	}

	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	shortFrameBuf := make([]byte, need-1)
	if gotNeed, err := d.DecodeRTPIntoWithPTS(shortFrameBuf, payloads, 55); !errors.Is(err, govpx.ErrBufferTooSmall) || gotNeed != need {
		t.Fatalf("short DecodeRTPIntoWithPTS = need:%d err:%v, want %d ErrBufferTooSmall", gotNeed, err, need)
	}

	frameBuf := make([]byte, need)
	n, err := d.DecodeRTPIntoWithPTS(frameBuf, payloads, 55)
	if err != nil {
		t.Fatalf("DecodeRTPIntoWithPTS returned error: %v", err)
	}
	if n != need || !bytes.Equal(frameBuf[:n], frame) {
		t.Fatalf("assembled frame size/data = %d/% x, want %d/% x", n, frameBuf[:n], need, frame)
	}
	info, ok := d.LastFrameInfo()
	if !ok || info.PTS != 55 || !info.KeyFrame || info.Width != 16 || info.Height != 16 {
		t.Fatalf("LastFrameInfo = %+v ok=%t, want key 16x16 PTS 55", info, ok)
	}
	img, ok := d.NextFrame()
	if !ok {
		t.Fatal("DecodeRTPIntoWithPTS queued no visible frame")
	}
	if len(img.Y) == 0 {
		t.Fatal("decoded Y plane is empty")
	}
	if img.Y[0] != 128 {
		t.Fatalf("decoded Y[0] = %d, want 128", img.Y[0])
	}

	allocated, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder allocated returned error: %v", err)
	}
	if err := allocated.DecodeRTPWithPTS(payloads, 66); err != nil {
		t.Fatalf("DecodeRTPWithPTS returned error: %v", err)
	}
	info, ok = allocated.LastFrameInfo()
	if !ok || info.PTS != 66 {
		t.Fatalf("allocated LastFrameInfo = %+v ok=%t, want PTS 66", info, ok)
	}
}

func TestVP8DecoderDecodeRTPRejectsInvalidFragments(t *testing.T) {
	frame := vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)
	payloads, err := govpx.PacketizeVP8RTPFrame(govpx.VP8RTPPayloadDescriptor{}, frame, 17)
	if err != nil {
		t.Fatalf("PacketizeVP8RTPFrame returned error: %v", err)
	}
	payloads[0].Marker = true

	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	frameBuf := make([]byte, len(frame))
	if _, err := d.DecodeRTPInto(frameBuf, payloads); !errors.Is(err, govpx.ErrInvalidData) {
		t.Fatalf("DecodeRTPInto invalid marker error = %v, want ErrInvalidData", err)
	}
}
