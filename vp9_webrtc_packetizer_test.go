package govpx

import (
	"errors"
	"reflect"
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

func TestVP9WebRTCPacketizerPacketizesPlainNonFlexibleTemporal(t *testing.T) {
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
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	key, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		32, 224, 96, 192), dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	packetizer := NewVP9WebRTCPacketizer(VP9RTPPictureID15BitMask - 1)
	packets, payloadBytes, sent, err := packetizer.WebRTCNonFlexiblePacketizationSize(
		key, 96)
	if err != nil || !sent || packets == 0 || payloadBytes == 0 {
		t.Fatalf("WebRTCNonFlexiblePacketizationSize key = packets:%d bytes:%d sent:%t err:%v",
			packets, payloadBytes, sent, err)
	}
	payloads := make([]RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	n, used, sent, err := packetizer.PacketizeWebRTCNonFlexibleInto(key,
		payloads, payloadBuf, 96)
	if err != nil || !sent {
		t.Fatalf("PacketizeWebRTCNonFlexibleInto key = packets:%d bytes:%d sent:%t err:%v",
			n, used, sent, err)
	}
	if n != packets || used != payloadBytes {
		t.Fatalf("non-flexible key returned %d/%d, want %d/%d",
			n, used, packets, payloadBytes)
	}
	assertVP9WebRTCNonFlexibleTemporalForTest(t, payloads[:n],
		VP9RTPPictureID15BitMask-1, key.TemporalLayerID, key.TL0PICIDX,
		true, true)
	if got := packetizer.PictureID(); got != VP9RTPPictureID15BitMask {
		t.Fatalf("PictureID after key = %d, want %d", got,
			VP9RTPPictureID15BitMask)
	}

	inter, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		36, 220, 100, 188), dst)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithResult: %v", err)
	}
	interPayloads, sent, err := packetizer.PacketizeWebRTCNonFlexible(inter,
		96)
	if err != nil || !sent {
		t.Fatalf("PacketizeWebRTCNonFlexible inter = sent:%t err:%v",
			sent, err)
	}
	assertVP9WebRTCNonFlexibleTemporalForTest(t, interPayloads,
		VP9RTPPictureID15BitMask, inter.TemporalLayerID, inter.TL0PICIDX,
		false, false)
	if got := packetizer.PictureID(); got != 0 {
		t.Fatalf("PictureID after inter = %d, want wrap to 0", got)
	}
}

func TestVP9WebRTCPacketizerNonFlexibleUsesConfiguredTemporalGOF(t *testing.T) {
	const width, height = 64, 64
	const mode = TemporalLayeringThreeLayersNoSync
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:                    width,
		Height:                   height,
		FPS:                      30,
		Deadline:                 DeadlineRealtime,
		CpuUsed:                  8,
		TargetBitrateKbps:        300,
		TemporalScalability:      TemporalScalabilityConfig{Enabled: true, Mode: mode},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	key, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		32, 224, 96, 192), dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if key.TemporalLayeringMode != mode {
		t.Fatalf("key temporal mode = %v, want %v", key.TemporalLayeringMode,
			mode)
	}

	desc := key.WebRTCRTPPayloadDescriptor(0x1234)
	if !desc.ScalabilityStructurePresent ||
		!desc.ScalabilityStructure.PictureGroupPresent {
		t.Fatalf("key WebRTC SS = %+v", desc.ScalabilityStructure)
	}
	want := vp9GenericTemporalScalabilityPictureGroups(mode)
	defaultGroups, ok := vp9WebRTCTemporalScalabilityPictureGroups(
		TemporalLayeringThreeLayers)
	if !ok {
		t.Fatal("default three-layer WebRTC GOF missing")
	}
	if reflect.DeepEqual(want, defaultGroups) {
		t.Fatal("test fixture GOF unexpectedly matches default three-layer GOF")
	}
	if got := desc.ScalabilityStructure.PictureGroups; !reflect.DeepEqual(got, want) {
		t.Fatalf("key WebRTC GOF = %+v, want configured mode %+v", got, want)
	}
}

func TestVP9WebRTCPacketizerFlexibleTemporalRefsHonorReferenceMask(t *testing.T) {
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
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	packetizer := NewVP9WebRTCPacketizer(1)
	key, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		32, 224, 96, 192), dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if _, sent, err := packetizer.Packetize(key, 96); err != nil || !sent {
		t.Fatalf("Packetize key = sent:%t err:%v", sent, err)
	}
	t2, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		36, 220, 100, 188), dst)
	if err != nil {
		t.Fatalf("T2 EncodeIntoWithResult: %v", err)
	}
	t2Payloads, sent, err := packetizer.Packetize(t2, 96)
	if err != nil || !sent {
		t.Fatalf("Packetize T2 = sent:%t err:%v", sent, err)
	}
	t2Desc := firstVP9PayloadDescriptorForTest(t, t2Payloads)
	if t2Desc.TemporalID != 2 || !t2Desc.SwitchingUpPoint ||
		t2Desc.ReferenceIndexCount != 1 ||
		t2Desc.ReferenceIndices[0] != 1 {
		t.Fatalf("T2 flexible refs = tid:%d sync:%t refs:%v/%d, want sync T2 -> key only",
			t2Desc.TemporalID, t2Desc.SwitchingUpPoint,
			t2Desc.ReferenceIndices, t2Desc.ReferenceIndexCount)
	}

	t1, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		40, 216, 104, 184), dst)
	if err != nil {
		t.Fatalf("T1 EncodeIntoWithResult: %v", err)
	}
	t1Payloads, sent, err := packetizer.Packetize(t1, 96)
	if err != nil || !sent {
		t.Fatalf("Packetize T1 = sent:%t err:%v", sent, err)
	}
	t1Desc := firstVP9PayloadDescriptorForTest(t, t1Payloads)
	if t1Desc.TemporalID != 1 || !t1Desc.SwitchingUpPoint ||
		t1Desc.ReferenceIndexCount != 1 ||
		t1Desc.ReferenceIndices[0] != 2 {
		t.Fatalf("T1 flexible refs = tid:%d sync:%t refs:%v/%d, want sync T1 -> key only",
			t1Desc.TemporalID, t1Desc.SwitchingUpPoint,
			t1Desc.ReferenceIndices, t1Desc.ReferenceIndexCount)
	}
}

func TestVP9WebRTCPacketizerFlexibleNoSyncLongStreamDoesNotRecover(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:                    width,
		Height:                   height,
		FPS:                      30,
		Deadline:                 DeadlineRealtime,
		CpuUsed:                  8,
		RateControlModeSet:       true,
		RateControlMode:          RateControlCBR,
		TargetBitrateKbps:        800,
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
		MaxKeyframeInterval:      32,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringThreeLayersNoSync,
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	payloadBuf := make([]byte, 1<<20)
	fragments := make([]RTPPayloadFragment, 256)
	packetizer := NewVP9WebRTCPacketizer(0x120)
	for frame := 0; frame < 96; frame++ {
		if frame == 45 {
			e.ForceKeyFrame()
		}
		result, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(
			width, height, byte(24+frame*3), byte(220-frame),
			byte(96+frame), byte(188-frame)), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		packets, payloadBytes, sent, err := packetizer.PacketizationSize(result,
			500)
		if err != nil {
			t.Fatalf("PacketizationSize frame %d tl=%d key=%t refresh=0x%x needsKey=%t err=%v",
				frame, result.TemporalLayerID, result.KeyFrame,
				result.RefreshFrameFlags, packetizer.NeedsKeyFrame(), err)
		}
		if !sent {
			continue
		}
		if packets > len(fragments) || payloadBytes > len(payloadBuf) {
			t.Fatalf("test buffers too small: packets=%d payloadBytes=%d",
				packets, payloadBytes)
		}
		gotPackets, gotBytes, sent, err := packetizer.PacketizeInto(result,
			fragments[:packets], payloadBuf[:payloadBytes], 500)
		if err != nil || !sent {
			t.Fatalf("PacketizeInto frame %d tl=%d key=%t refresh=0x%x packets=%d/%d bytes=%d/%d needsKey=%t sent=%t err=%v",
				frame, result.TemporalLayerID, result.KeyFrame,
				result.RefreshFrameFlags, gotPackets, packets, gotBytes,
				payloadBytes, packetizer.NeedsKeyFrame(), sent, err)
		}
		if packetizer.NeedsKeyFrame() {
			t.Fatalf("packetizer requested recovery after frame %d", frame)
		}
	}
}

func TestVP9WebRTCPacketizerPacketizesPlainOneLayerNonFlexibleTL0(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		TargetBitrateKbps: 300,
		ErrorResilient:    true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	key, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width, height,
		32, 224, 96, 192), dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	packetizer := NewVP9WebRTCPacketizer(0x1234)
	keyPayloads, sent, err := packetizer.PacketizeWebRTCNonFlexible(key, 96)
	if err != nil || !sent {
		t.Fatalf("PacketizeWebRTCNonFlexible key = sent:%t err:%v",
			sent, err)
	}
	assertVP9WebRTCNonFlexibleTemporalForTest(t, keyPayloads, 0x1234, 0, 0,
		true, false)

	inter, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(width,
		height, 36, 220, 100, 188), dst)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithResult: %v", err)
	}
	interPayloads, sent, err := packetizer.PacketizeWebRTCNonFlexible(inter,
		96)
	if err != nil || !sent {
		t.Fatalf("PacketizeWebRTCNonFlexible inter = sent:%t err:%v",
			sent, err)
	}
	assertVP9WebRTCNonFlexibleTemporalForTest(t, interPayloads, 0x1235, 0, 1,
		false, false)
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
	key, inter := newVP9WebRTCReferenceTestFrames(t)
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
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("NeedsKeyFrame after unmapped inter refs = false, want true")
	}
	if _, _, sent, err := packetizer.PacketizationSize(inter,
		500); !errors.Is(err, ErrInvalidConfig) || sent {
		t.Fatalf("inter retry after unmapped refs = sent:%t err:%v, want recovery ErrInvalidConfig",
			sent, err)
	}
	payloads, sent, err := packetizer.Packetize(key, 500)
	if err != nil || !sent || len(payloads) == 0 {
		t.Fatalf("recovery key Packetize = payloads:%d sent:%t err:%v",
			len(payloads), sent, err)
	}
	if packetizer.NeedsKeyFrame() {
		t.Fatal("NeedsKeyFrame remained set after recovery key")
	}
}

func TestVP9WebRTCPacketizerRejectsInterWithTooOldFlexibleReference(t *testing.T) {
	inter := newVP9WebRTCInterResultForReferenceTest(t)
	slots, slotCount, err := vp9WebRTCReferenceSlotsForFrame(inter.Data,
		vp9WebRTCReferenceMaskForResult(inter))
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
	if !packetizer.NeedsKeyFrame() {
		t.Fatal("NeedsKeyFrame after too-old inter ref = false, want true")
	}
}

func TestVP9WebRTCPacketizerFlexibleSingleLayerForcedKeyChurn(t *testing.T) {
	const width, height = 320, 180
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 25,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		TargetBitrateKbps:   800,
		MaxKeyframeInterval: 2048,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	packetizer := NewVP9WebRTCPacketizer(0x120)
	var fragments []RTPPayloadFragment
	var payloadBuf []byte
	for frame := 0; frame < 180; frame++ {
		if frame == 1 || frame == 2 || (frame != 0 && frame%30 == 0) ||
			frame == 31 {
			e.ForceKeyFrame()
		}
		result, err := e.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(
			width, height, byte(24+frame*7), byte(224-frame*3),
			byte(96+frame*5), byte(192-frame*2)), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		pictureID := packetizer.PictureID()
		fragmentCount, payloadBytes, sent, err := packetizer.PacketizationSize(
			result, 1200)
		if err != nil {
			t.Fatalf("PacketizationSize frame %d pictureID %d key:%t inter:%t refMask:%03b refresh:%02x err:%v",
				frame, pictureID, result.KeyFrame,
				result.vp9RTPInterPicturePredicted(), result.referenceMask,
				result.RefreshFrameFlags, err)
		}
		if !sent {
			t.Fatalf("frame %d reported unsent size without drop", frame)
		}
		if cap(fragments) < fragmentCount {
			fragments = make([]RTPPayloadFragment, fragmentCount)
		}
		fragments = fragments[:fragmentCount]
		if cap(payloadBuf) < payloadBytes {
			payloadBuf = make([]byte, payloadBytes)
		}
		payloadBuf = payloadBuf[:payloadBytes]
		n, used, sent, err := packetizer.PacketizeInto(result, fragments,
			payloadBuf, 1200)
		if err != nil || !sent {
			t.Fatalf("PacketizeInto frame %d pictureID %d = packets:%d bytes:%d sent:%t err:%v",
				frame, pictureID, n, used, sent, err)
		}
		if n != fragmentCount || used != payloadBytes {
			t.Fatalf("PacketizeInto frame %d returned %d/%d, want %d/%d",
				frame, n, used, fragmentCount, payloadBytes)
		}
	}
}

func TestVP9WebRTCPacketizerRequiresRecoveryAfterPacketizationError(t *testing.T) {
	key, inter := newVP9WebRTCReferenceTestFrames(t)
	const mtu = 500

	for _, tc := range []struct {
		name string
		run  func(*VP9WebRTCPacketizer, VP9EncodeResult) (int, int, bool, error)
	}{
		{
			name: "size",
			run: func(packetizer *VP9WebRTCPacketizer,
				result VP9EncodeResult,
			) (int, int, bool, error) {
				return packetizer.PacketizationSize(result, mtu)
			},
		},
		{
			name: "packetize",
			run: func(packetizer *VP9WebRTCPacketizer,
				result VP9EncodeResult,
			) (int, int, bool, error) {
				payloads := make([]RTPPayloadFragment, 8)
				payloadBuf := make([]byte, 1<<20)
				return packetizer.PacketizeInto(result, payloads,
					payloadBuf, mtu)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packetizer := NewVP9WebRTCPacketizer(0x66)
			payloads, sent, err := packetizer.Packetize(key, mtu)
			if err != nil || !sent || len(payloads) == 0 {
				t.Fatalf("key Packetize = payloads:%d sent:%t err:%v",
					len(payloads), sent, err)
			}
			nextPictureID := packetizer.PictureID()

			bad := inter
			bad.Data = nil
			bad.SizeBytes = 0
			packets, payloadBytes, sent, err := tc.run(&packetizer, bad)
			if !errors.Is(err, ErrInvalidConfig) || sent ||
				packets != 0 || payloadBytes != 0 {
				t.Fatalf("%s bad inter = %d/%d sent:%t err:%v, want ErrInvalidConfig",
					tc.name, packets, payloadBytes, sent, err)
			}
			if got := packetizer.PictureID(); got != nextPictureID {
				t.Fatalf("%s bad inter advanced PictureID to %d, want %d",
					tc.name, got, nextPictureID)
			}
			if !packetizer.NeedsKeyFrame() {
				t.Fatalf("%s bad inter did not require recovery key",
					tc.name)
			}
			if packets, payloadBytes, sent, err := packetizer.PacketizationSize(
				inter, mtu); !errors.Is(err, ErrInvalidConfig) ||
				sent || packets != 0 || payloadBytes != 0 {
				t.Fatalf("%s post-error inter = %d/%d sent:%t err:%v, want ErrInvalidConfig",
					tc.name, packets, payloadBytes, sent, err)
			}

			payloads, sent, err = packetizer.Packetize(key, mtu)
			if err != nil || !sent || len(payloads) == 0 {
				t.Fatalf("recovery key Packetize = payloads:%d sent:%t err:%v",
					len(payloads), sent, err)
			}
			if packetizer.NeedsKeyFrame() {
				t.Fatalf("%s recovery key did not clear NeedsKeyFrame",
					tc.name)
			}
		})
	}
}

func TestVP9WebRTCSpatialSVCRecoveryKeyRejectsPredictedEnhancement(t *testing.T) {
	result := VP9SpatialSVCEncodeResult{
		LayerCount:           2,
		InterLayerPrediction: true,
		ScalabilityStructure: VP9RTPScalabilityStructure{
			SpatialLayerCount: 2,
			ResolutionPresent: true,
			Width:             [VP9RTPMaxSpatialLayers]uint16{32, 64},
			Height:            [VP9RTPMaxSpatialLayers]uint16{32, 64},
		},
	}
	result.Layers[0] = VP9EncodeResult{
		Data:                        []byte{0x82},
		KeyFrame:                    true,
		ShowFrame:                   true,
		TemporalLayerID:             0,
		TemporalLayerCount:          3,
		SpatialLayerID:              0,
		SpatialLayerCount:           2,
		ScalabilityStructurePresent: true,
	}
	result.Layers[1] = VP9EncodeResult{
		Data:                       []byte{0x83},
		ShowFrame:                  true,
		InterPicturePredicted:      true,
		TemporalLayerID:            0,
		TemporalLayerCount:         3,
		SpatialLayerID:             1,
		SpatialLayerCount:          2,
		InterLayerDependency:       true,
		NotRefForUpperSpatialLayer: true,
		interPicturePredictedKnown: true,
	}
	if vp9WebRTCSpatialSVCResultIsRecoveryKey(result) {
		t.Fatal("predicted enhancement layer was accepted as WebRTC SVC recovery")
	}

	result.Layers[1].InterPicturePredicted = false
	if !vp9WebRTCSpatialSVCResultIsRecoveryKey(result) {
		t.Fatal("non-predicted enhancement layer was rejected as WebRTC SVC recovery")
	}
}

func newVP9WebRTCInterResultForReferenceTest(t *testing.T) VP9EncodeResult {
	t.Helper()
	_, inter := newVP9WebRTCReferenceTestFrames(t)
	return inter
}

func newVP9WebRTCReferenceTestFrames(
	t *testing.T,
) (VP9EncodeResult, VP9EncodeResult) {
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
	key.Data = append([]byte(nil), key.Data...)
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
	return key, inter
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
	if packetizer.NeedsKeyFrame() {
		t.Fatal("NeedsKeyFrame set after retryable buffer error")
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
	desc := firstVP9PayloadDescriptorForTest(t, payloads)
	if !desc.PictureIDPresent || !desc.PictureID15Bit {
		t.Fatalf("payload PictureID = present:%t 15bit:%t",
			desc.PictureIDPresent, desc.PictureID15Bit)
	}
	return desc.PictureID
}

func firstVP9PayloadDescriptorForTest(
	t *testing.T,
	payloads []RTPPayloadFragment,
) VP9RTPPayloadDescriptor {
	t.Helper()
	if len(payloads) == 0 {
		t.Fatal("no RTP payloads")
	}
	desc, _, err := ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
	}
	return desc
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

func assertVP9WebRTCNonFlexibleTemporalForTest(
	t *testing.T,
	payloads []RTPPayloadFragment,
	wantPictureID uint16,
	wantTemporalID int,
	wantTL0PICIDX uint8,
	wantSS bool,
	wantGOF bool,
) {
	t.Helper()
	if len(payloads) == 0 {
		t.Fatal("no non-flexible RTP payloads")
	}
	for i, payload := range payloads {
		desc, _, err := ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if desc.FlexibleMode {
			t.Fatalf("payload %d used flexible descriptor: %+v", i, desc)
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit ||
			desc.PictureID != wantPictureID {
			t.Fatalf("payload %d PictureID = present:%t 15bit:%t id:%d, want %d",
				i, desc.PictureIDPresent, desc.PictureID15Bit,
				desc.PictureID, wantPictureID)
		}
		if !desc.LayerIndicesPresent ||
			int(desc.TemporalID) != wantTemporalID ||
			desc.TL0PICIDX != wantTL0PICIDX {
			t.Fatalf("payload %d temporal = present:%t tid:%d tl0:%d, want %d/%d",
				i, desc.LayerIndicesPresent, desc.TemporalID,
				desc.TL0PICIDX, wantTemporalID, wantTL0PICIDX)
		}
		if desc.ReferenceIndexCount != 0 {
			t.Fatalf("payload %d carried flexible reference diffs in non-flexible mode",
				i)
		}
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d marker = %t, want %t", i, got, want)
		}
		if i == 0 {
			if desc.ScalabilityStructurePresent != wantSS {
				t.Fatalf("first payload SS present = %t, want %t",
					desc.ScalabilityStructurePresent, wantSS)
			}
			if desc.ScalabilityStructure.PictureGroupPresent != wantGOF {
				t.Fatalf("first payload GOF present = %t, want %t: %+v",
					desc.ScalabilityStructure.PictureGroupPresent,
					wantGOF, desc.ScalabilityStructure)
			}
			if wantGOF && len(desc.ScalabilityStructure.PictureGroups) == 0 {
				t.Fatalf("first payload GOF missing picture groups: %+v",
					desc.ScalabilityStructure)
			}
			if !wantGOF && len(desc.ScalabilityStructure.PictureGroups) != 0 {
				t.Fatalf("first payload carried picture groups without GOF: %+v",
					desc.ScalabilityStructure)
			}
		} else if desc.ScalabilityStructurePresent {
			t.Fatalf("payload %d repeated scalability structure", i)
		}
	}
}
