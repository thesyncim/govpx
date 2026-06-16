package govpx

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9WebRTCPacketizerConsumesCBRDroppedFrames(t *testing.T) {
	e, packetizer, dst, keyPayloads := newVP9WebRTCDropTestState(t)

	e.rc.bufferLevelBits = -e.rc.bitsPerFrame + 1
	dropped, err := e.EncodeIntoWithResult(
		vp9test.NewPanningYCbCr(64, 64, 1), dst)
	if err != nil {
		t.Fatalf("dropped EncodeIntoWithResult: %v", err)
	}
	if !dropped.Dropped {
		t.Fatal("test did not force a VP9 CBR post-encode drop")
	}
	if dropped.TemporalLayerID != 2 {
		t.Fatalf("dropped temporal layer = %d, want 2", dropped.TemporalLayerID)
	}
	droppedPayloads, sent, err := packetizer.Packetize(dropped, 500)
	if err != nil || sent || len(droppedPayloads) != 0 {
		t.Fatalf("dropped Packetize = payloads:%d sent:%t err:%v, want skip",
			len(droppedPayloads), sent, err)
	}
	if got := packetizer.PictureID(); got != 0 {
		t.Fatalf("PictureID after dropped frame = %d, want 0", got)
	}

	inter := encodePostDropVP9FrameForTest(t, e, dst)
	interPayloads, sent, err := packetizer.Packetize(inter, 500)
	if err != nil || !sent {
		t.Fatalf("inter Packetize = sent:%t err:%v", sent, err)
	}
	if got := firstVP9PayloadPictureIDForTest(t, interPayloads); got != 0 {
		t.Fatalf("inter PictureID = %d, want 0", got)
	}
	if got := packetizer.PictureID(); got != 1 {
		t.Fatalf("PictureID after inter = %d, want 1", got)
	}
	assertVP9WebRTCGOFTemporalForTest(t, keyPayloads, interPayloads,
		inter.TemporalLayerID)
}

func TestVP9WebRTCPacketizerSizeConsumesCBRDroppedFrames(t *testing.T) {
	e, packetizer, dst, keyPayloads := newVP9WebRTCDropTestState(t)

	e.rc.bufferLevelBits = -e.rc.bitsPerFrame + 1
	dropped, err := e.EncodeIntoWithResult(
		vp9test.NewPanningYCbCr(64, 64, 1), dst)
	if err != nil {
		t.Fatalf("dropped EncodeIntoWithResult: %v", err)
	}
	if !dropped.Dropped {
		t.Fatal("test did not force a VP9 CBR post-encode drop")
	}
	packets, payloadBytes, sent, err := packetizer.PacketizationSize(dropped, 500)
	if err != nil || sent || packets != 0 || payloadBytes != 0 {
		t.Fatalf("dropped PacketizationSize = packets:%d bytes:%d sent:%t err:%v, want consumed skip",
			packets, payloadBytes, sent, err)
	}
	if got := packetizer.PictureID(); got != 0 {
		t.Fatalf("PictureID after dropped size query = %d, want 0", got)
	}

	inter := encodePostDropVP9FrameForTest(t, e, dst)
	interPayloads, sent, err := packetizer.Packetize(inter, 500)
	if err != nil || !sent {
		t.Fatalf("inter Packetize = sent:%t err:%v", sent, err)
	}
	if got := firstVP9PayloadPictureIDForTest(t, interPayloads); got != 0 {
		t.Fatalf("inter PictureID = %d, want 0", got)
	}
	assertVP9WebRTCGOFTemporalForTest(t, keyPayloads, interPayloads,
		inter.TemporalLayerID)
}

func newVP9WebRTCDropTestState(
	t *testing.T,
) (*VP9Encoder, VP9WebRTCPacketizer, []byte, []RTPPayloadFragment) {
	t.Helper()
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   120,
		BufferSizeMs:        100,
		BufferInitialSizeMs: 10,
		BufferOptimalSizeMs: 20,
		Quantizer:           10,
		PostEncodeDrop:      true,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringThreeLayers,
		},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	var packetizer = NewVP9WebRTCPacketizer(VP9RTPPictureID15BitMask - 1)
	dst := make([]byte, 1<<20)
	key, err := e.EncodeIntoWithResult(vp9test.NewPanningYCbCr(width, height, 0),
		dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if key.Dropped || !key.KeyFrame {
		t.Fatalf("key result = dropped:%t key:%t, want coded key",
			key.Dropped, key.KeyFrame)
	}
	keyPayloads, sent, err := packetizer.Packetize(key, 500)
	if err != nil || !sent {
		t.Fatalf("key Packetize = sent:%t err:%v", sent, err)
	}
	if got := firstVP9PayloadPictureIDForTest(t, keyPayloads); got != VP9RTPPictureID15BitMask-1 {
		t.Fatalf("key PictureID = %d, want %d", got,
			VP9RTPPictureID15BitMask-1)
	}
	if got := packetizer.PictureID(); got != VP9RTPPictureID15BitMask {
		t.Fatalf("PictureID after key = %d, want %d", got,
			VP9RTPPictureID15BitMask)
	}
	return e, packetizer, dst, keyPayloads
}

func encodePostDropVP9FrameForTest(
	t *testing.T,
	e *VP9Encoder,
	dst []byte,
) VP9EncodeResult {
	t.Helper()
	if err := e.SetPostEncodeDrop(false); err != nil {
		t.Fatalf("SetPostEncodeDrop(false): %v", err)
	}
	inter, err := e.EncodeIntoWithResult(
		vp9test.NewPanningYCbCr(64, 64, 2), dst)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithResult: %v", err)
	}
	if inter.Dropped || inter.KeyFrame {
		t.Fatalf("inter result = dropped:%t key:%t, want coded inter",
			inter.Dropped, inter.KeyFrame)
	}
	if inter.TemporalLayerID != 1 {
		t.Fatalf("inter temporal layer = %d, want 1", inter.TemporalLayerID)
	}
	return inter
}

func TestVP9WebRTCPacketizerKeepsPictureIDOnBufferTooSmall(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		TargetBitrateKbps: 300,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringThreeLayers,
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	key, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		32, 224, 96, 192), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}

	packetizer := NewVP9WebRTCPacketizer(0x1234)
	packets, payloadBytes, sent, err := packetizer.PacketizationSize(key, 80)
	if err != nil || !sent {
		t.Fatalf("PacketizationSize = packets:%d bytes:%d sent:%t err:%v",
			packets, payloadBytes, sent, err)
	}
	short := make([]RTPPayloadFragment, packets-1)
	payloadBuf := make([]byte, payloadBytes)
	if gotPackets, gotBytes, sent, err := packetizer.PacketizeInto(key, short,
		payloadBuf, 80); !errors.Is(err, ErrBufferTooSmall) || sent ||
		gotPackets != packets || gotBytes != payloadBytes {
		t.Fatalf("short PacketizeInto = packets:%d bytes:%d sent:%t err:%v, want %d/%d ErrBufferTooSmall",
			gotPackets, gotBytes, sent, err, packets, payloadBytes)
	}
	if got := packetizer.PictureID(); got != 0x1234 {
		t.Fatalf("PictureID advanced on buffer error: got %d want %d",
			got, 0x1234)
	}

	payloads := make([]RTPPayloadFragment, packets)
	n, used, sent, err := packetizer.PacketizeInto(key, payloads, payloadBuf, 80)
	if err != nil || !sent {
		t.Fatalf("PacketizeInto = packets:%d bytes:%d sent:%t err:%v",
			n, used, sent, err)
	}
	if got := firstVP9PayloadPictureIDForTest(t, payloads[:n]); got != 0x1234 {
		t.Fatalf("PictureID = %d, want %d", got, 0x1234)
	}
	if got := packetizer.PictureID(); got != 0x1235 {
		t.Fatalf("PictureID after success = %d, want %d", got, 0x1235)
	}
}

func firstVP9PayloadPictureIDForTest(
	t *testing.T,
	payloads []RTPPayloadFragment,
) uint16 {
	t.Helper()
	if len(payloads) == 0 {
		t.Fatal("no RTP payloads")
	}
	desc, _, err := ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
	}
	if !desc.PictureIDPresent || !desc.PictureID15Bit {
		t.Fatalf("payload PictureID = present:%t 15bit:%t",
			desc.PictureIDPresent, desc.PictureID15Bit)
	}
	return desc.PictureID
}

func assertVP9WebRTCGOFTemporalForTest(
	t *testing.T,
	keyPayloads []RTPPayloadFragment,
	payloads []RTPPayloadFragment,
	wantTemporalID int,
) {
	t.Helper()
	keyDesc, _, err := ParseVP9RTPPayloadDescriptor(keyPayloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor key: %v", err)
	}
	if !keyDesc.ScalabilityStructurePresent ||
		!keyDesc.ScalabilityStructure.PictureGroupPresent {
		t.Fatalf("key payload did not carry WebRTC GOF: %+v", keyDesc)
	}
	desc, _, err := ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor inter: %v", err)
	}
	diff := (int(desc.PictureID) - int(keyDesc.PictureID) +
		int(VP9RTPPictureID15BitMask) + 1) &
		int(VP9RTPPictureID15BitMask)
	groups := keyDesc.ScalabilityStructure.PictureGroups
	gofTemporalID := int(groups[diff%len(groups)].TemporalID)
	if gofTemporalID != wantTemporalID {
		t.Fatalf("GOF temporal layer at PictureID diff %d = %d, want %d",
			diff, gofTemporalID, wantTemporalID)
	}
}
