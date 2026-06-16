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

	inter := encodeAfterDroppedVP9FrameForTest(t, e, dst, 2, 1)
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
	packets, payloadBytes, sent, err = packetizer.PacketizationSize(dropped, 500)
	if err != nil || sent || packets != 0 || payloadBytes != 0 {
		t.Fatalf("duplicate dropped PacketizationSize = packets:%d bytes:%d sent:%t err:%v, want same consumed skip",
			packets, payloadBytes, sent, err)
	}
	if got := packetizer.PictureID(); got != 0 {
		t.Fatalf("PictureID after duplicate dropped size query = %d, want 0", got)
	}
	n, used, sent, err := packetizer.PacketizeInto(dropped, nil, nil, 500)
	if err != nil || sent || n != 0 || used != 0 {
		t.Fatalf("dropped PacketizeInto after size = packets:%d bytes:%d sent:%t err:%v, want same consumed skip",
			n, used, sent, err)
	}
	if got := packetizer.PictureID(); got != 0 {
		t.Fatalf("PictureID after duplicate dropped packetize = %d, want 0", got)
	}

	inter := encodeAfterDroppedVP9FrameForTest(t, e, dst, 2, 1)
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

func TestVP9WebRTCPacketizerConsumesCBRPreEncodeDroppedFrames(t *testing.T) {
	e, packetizer, dst, keyPayloads := newVP9WebRTCPreDropTestState(t)

	e.rc.bufferLevelBits = -e.rc.bitsPerFrame - 1
	dropped, err := e.EncodeIntoWithResult(
		vp9test.NewPanningYCbCr(64, 64, 1), dst)
	if err != nil {
		t.Fatalf("dropped EncodeIntoWithResult: %v", err)
	}
	if !dropped.Dropped {
		t.Fatal("test did not force a VP9 CBR pre-encode drop")
	}
	if dropped.TemporalLayerID != 2 {
		t.Fatalf("dropped temporal layer = %d, want 2", dropped.TemporalLayerID)
	}
	packets, payloadBytes, sent, err := packetizer.PacketizationSize(dropped, 500)
	if err != nil || sent || packets != 0 || payloadBytes != 0 {
		t.Fatalf("pre-drop PacketizationSize = packets:%d bytes:%d sent:%t err:%v, want consumed skip",
			packets, payloadBytes, sent, err)
	}
	if got := packetizer.PictureID(); got != 0 {
		t.Fatalf("PictureID after pre-encode drop = %d, want 0", got)
	}
	if err := e.SetFrameDropAllowed(false); err != nil {
		t.Fatalf("SetFrameDropAllowed(false): %v", err)
	}

	inter := encodeAfterDroppedVP9FrameForTest(t, e, dst, 2, 1)
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

func TestVP9WebRTCPacketizerConsumesConsecutiveSizedDrops(t *testing.T) {
	packetizer := NewVP9WebRTCPacketizer(VP9RTPPictureID15BitMask)
	firstDrop := VP9EncodeResult{
		Dropped:            true,
		TemporalLayerID:    2,
		TemporalLayerCount: 3,
		vp9FrameIndex:      1,
	}
	if packets, payloadBytes, sent, err := packetizer.PacketizationSize(firstDrop,
		500); err != nil || sent || packets != 0 || payloadBytes != 0 {
		t.Fatalf("first dropped PacketizationSize = packets:%d bytes:%d sent:%t err:%v, want consumed skip",
			packets, payloadBytes, sent, err)
	}
	if got := packetizer.PictureID(); got != 0 {
		t.Fatalf("PictureID after first drop = %d, want 0", got)
	}
	if _, sent, err := packetizer.Packetize(firstDrop, 500); err != nil || sent {
		t.Fatalf("duplicate first dropped Packetize = sent:%t err:%v, want same consumed skip",
			sent, err)
	}
	if got := packetizer.PictureID(); got != 0 {
		t.Fatalf("PictureID after duplicate first drop = %d, want 0", got)
	}

	secondDrop := VP9EncodeResult{
		Dropped:            true,
		TemporalLayerID:    1,
		TemporalLayerCount: 3,
		vp9FrameIndex:      2,
	}
	if packets, payloadBytes, sent, err := packetizer.PacketizationSize(secondDrop,
		500); err != nil || sent || packets != 0 || payloadBytes != 0 {
		t.Fatalf("second dropped PacketizationSize = packets:%d bytes:%d sent:%t err:%v, want consumed skip",
			packets, payloadBytes, sent, err)
	}
	if got := packetizer.PictureID(); got != 1 {
		t.Fatalf("PictureID after second drop = %d, want 1", got)
	}
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("NeedsKeyFrame after lower temporal-layer drop = false, want true")
	}
}

func TestVP9WebRTCPacketizerRequiresRecoveryKeyAfterLowerTemporalDrop(t *testing.T) {
	e, packetizer, dst, _ := newVP9WebRTCPreDropTestState(t)

	lowerDrop := VP9EncodeResult{
		Dropped:            true,
		TemporalLayerID:    1,
		TemporalLayerCount: 3,
		vp9FrameIndex:      2,
	}
	if _, _, sent, err := packetizer.PacketizationSize(lowerDrop,
		500); err != nil || sent {
		t.Fatalf("lower dropped PacketizationSize = sent:%t err:%v, want consumed skip",
			sent, err)
	}
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("NeedsKeyFrame after lower temporal-layer drop = false, want true")
	}
	if err := e.SetFrameDropAllowed(false); err != nil {
		t.Fatalf("SetFrameDropAllowed(false): %v", err)
	}

	inter, err := e.EncodeIntoWithResult(vp9test.NewPanningYCbCr(64, 64, 1),
		dst)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithResult: %v", err)
	}
	if inter.Dropped || inter.KeyFrame {
		t.Fatalf("inter result = dropped:%t key:%t, want coded inter",
			inter.Dropped, inter.KeyFrame)
	}
	if _, _, sent, err := packetizer.PacketizationSize(inter,
		500); !errors.Is(err, ErrInvalidConfig) || sent {
		t.Fatalf("inter PacketizationSize after lower drop = sent:%t err:%v, want ErrInvalidConfig",
			sent, err)
	}
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("NeedsKeyFrame cleared by rejected inter frame")
	}

	e.ForceKeyFrame()
	key, err := e.EncodeIntoWithResult(vp9test.NewPanningYCbCr(64, 64, 2), dst)
	if err != nil {
		t.Fatalf("forced key EncodeIntoWithResult: %v", err)
	}
	if key.Dropped || !key.KeyFrame || key.TemporalLayerID != 0 {
		t.Fatalf("forced key result = dropped:%t key:%t tid:%d, want TL0 key",
			key.Dropped, key.KeyFrame, key.TemporalLayerID)
	}
	payloads, sent, err := packetizer.Packetize(key, 500)
	if err != nil || !sent {
		t.Fatalf("forced key Packetize = payloads:%d sent:%t err:%v",
			len(payloads), sent, err)
	}
	if packetizer.NeedsKeyFrame() {
		t.Fatal("NeedsKeyFrame remained set after recovery key")
	}
}

func TestVP9WebRTCPacketizerRejectsInterWithUnmappedFlexibleReferences(t *testing.T) {
	inter := newVP9WebRTCInterResultForReferenceTest(t)
	packetizer := NewVP9WebRTCPacketizer(0x42)
	packetizer.references.lastPictureID = 0x41
	packetizer.references.haveLast = true

	packets, payloadBytes, sent, err := packetizer.PacketizationSize(inter, 500)
	if !errors.Is(err, ErrInvalidConfig) || sent ||
		packets != 0 || payloadBytes != 0 {
		t.Fatalf("unmapped inter PacketizationSize = packets:%d bytes:%d sent:%t err:%v, want ErrInvalidConfig",
			packets, payloadBytes, sent, err)
	}
	if got := packetizer.PictureID(); got != 0x42 {
		t.Fatalf("PictureID after rejected inter = %d, want %d", got, 0x42)
	}
}

func TestVP9WebRTCPacketizerRejectsInterWithTooOldFlexibleReference(t *testing.T) {
	inter := newVP9WebRTCInterResultForReferenceTest(t)
	slots, slotCount, err := vp9WebRTCReferenceSlotsForFrame(inter.Data)
	if err != nil {
		t.Fatalf("vp9WebRTCReferenceSlotsForFrame: %v", err)
	}
	if slotCount == 0 {
		t.Fatal("inter frame had no reference slots")
	}

	const pictureID = uint16(0x200)
	packetizer := NewVP9WebRTCPacketizer(pictureID)
	packetizer.references.lastPictureID = pictureID - 1
	packetizer.references.haveLast = true
	tooOld := (pictureID - 128) & VP9RTPPictureID15BitMask
	for i := 0; i < slotCount; i++ {
		slot := slots[i]
		packetizer.references.valid[slot] = true
		packetizer.references.pictureID[slot] = tooOld
	}

	packets, payloadBytes, sent, err := packetizer.PacketizationSize(inter, 500)
	if !errors.Is(err, ErrInvalidConfig) || sent ||
		packets != 0 || payloadBytes != 0 {
		t.Fatalf("too-old inter PacketizationSize = packets:%d bytes:%d sent:%t err:%v, want ErrInvalidConfig",
			packets, payloadBytes, sent, err)
	}
	if got := packetizer.PictureID(); got != pictureID {
		t.Fatalf("PictureID after rejected inter = %d, want %d", got, pictureID)
	}
}

func newVP9WebRTCInterResultForReferenceTest(t *testing.T) VP9EncodeResult {
	t.Helper()
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		TargetBitrateKbps: 300,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { e.Close() })

	dst := make([]byte, 1<<20)
	key, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		32, 224, 96, 192), dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if key.Dropped || !key.KeyFrame {
		t.Fatalf("key result = dropped:%t key:%t, want coded key",
			key.Dropped, key.KeyFrame)
	}
	inter, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width,
		height, 40, 208, 100, 180), dst)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithResult: %v", err)
	}
	if inter.Dropped || inter.KeyFrame || !inter.vp9RTPInterPicturePredicted() {
		t.Fatalf("inter result = dropped:%t key:%t predicted:%t, want predicted inter",
			inter.Dropped, inter.KeyFrame,
			inter.vp9RTPInterPicturePredicted())
	}
	return inter
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

func newVP9WebRTCPreDropTestState(
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
		DropFrameAllowed:    true,
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

func encodeAfterDroppedVP9FrameForTest(
	t *testing.T,
	e *VP9Encoder,
	dst []byte,
	sourceFrame int,
	wantTemporalLayer int,
) VP9EncodeResult {
	t.Helper()
	if err := e.SetPostEncodeDrop(false); err != nil {
		t.Fatalf("SetPostEncodeDrop(false): %v", err)
	}
	inter, err := e.EncodeIntoWithResult(
		vp9test.NewPanningYCbCr(64, 64, sourceFrame), dst)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithResult: %v", err)
	}
	if inter.Dropped || inter.KeyFrame {
		t.Fatalf("inter result = dropped:%t key:%t, want coded inter",
			inter.Dropped, inter.KeyFrame)
	}
	if inter.TemporalLayerID != wantTemporalLayer {
		t.Fatalf("inter temporal layer = %d, want %d",
			inter.TemporalLayerID, wantTemporalLayer)
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
	if !keyDesc.FlexibleMode || !keyDesc.ScalabilityStructurePresent ||
		keyDesc.ScalabilityStructure.PictureGroupPresent {
		t.Fatalf("key payload descriptor = flexible:%t ss:%t gof:%t, want flexible SS without GOF",
			keyDesc.FlexibleMode, keyDesc.ScalabilityStructurePresent,
			keyDesc.ScalabilityStructure.PictureGroupPresent)
	}
	desc, _, err := ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor inter: %v", err)
	}
	if !desc.FlexibleMode {
		t.Fatalf("inter payload used non-flexible descriptor: %+v", desc)
	}
	if int(desc.TemporalID) != wantTemporalID {
		t.Fatalf("inter temporal layer = %d, want %d",
			desc.TemporalID, wantTemporalID)
	}
	if desc.InterPicturePredicted && desc.ReferenceIndexCount == 0 {
		t.Fatalf("inter payload had P=1 without flexible reference diffs: %+v",
			desc)
	}
}
