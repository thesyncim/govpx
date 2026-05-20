package govpx

import (
	"errors"
	"testing"

	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestDecodeRejectsMissingTokenPartition(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacket(16, 16, 200, 0, true)
	packet = append(packet, make([]byte, 200)...)

	err = d.Decode(packet)
	if !errors.Is(err, ErrInvalidData) {
		t.Fatalf("Decode error = %v, want ErrInvalidData", err)
	}
}

func TestDecodeErrorConcealmentConcealsCorruptInterFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	keyPacket := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	previous := d.lastRef.Img.Y[0]
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}

	err = d.DecodeWithPTS(vp8InterFramePacket(0, 0, true), 99)
	if err != nil {
		t.Fatalf("corrupt inter DecodeWithPTS error = %v, want nil concealment", err)
	}
	if !d.lastInfo.Corrupted || d.lastInfo.PTS != 99 || d.lastInfo.Width != 16 || d.lastInfo.Height != 16 {
		t.Fatalf("lastInfo = %+v, want corrupted concealed 16x16 PTS 99", d.lastInfo)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("concealed NextFrame returned no frame")
	}
	if frame.Y[0] != previous {
		t.Fatalf("concealed Y[0] = %d, want previous reference %d", frame.Y[0], previous)
	}
}

func TestDecodeErrorConcealmentConcealsMissingFrameTag(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	keyPacket := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	if err := d.DecodeWithPTS([]byte{0x11, 0}, 101); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("initial missing tag DecodeWithPTS error = %v, want ErrInvalidData before EC is active", err)
	}
	fillVP8Image(&d.lastRef.Img, 77)
	if err := d.Decode(vp8InterFramePacketWithFirstPartition(vp8InterFirstPartitionLastZeroMV())); err != nil {
		t.Fatalf("priming inter Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("priming inter NextFrame returned no frame")
	}
	previous := d.lastRef.Img.Y[0]

	err = d.DecodeWithPTS([]byte{0x11, 0}, 102)
	if err != nil {
		t.Fatalf("missing tag DecodeWithPTS error = %v, want nil concealment", err)
	}
	if !d.lastInfo.Corrupted || d.lastInfo.PTS != 102 || d.lastInfo.Width != 16 || d.lastInfo.Height != 16 || !d.lastInfo.ShowFrame {
		t.Fatalf("lastInfo = %+v, want visible corrupted concealed 16x16 PTS 102", d.lastInfo)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("concealed missing-tag NextFrame returned no frame")
	}
	if frame.Y[0] != previous {
		t.Fatalf("concealed Y[0] = %d, want previous reference %d", frame.Y[0], previous)
	}
}

func TestDecodeErrorConcealmentDoesNotCommitProbabilityUpdates(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	good := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithSingleCoefProbabilityUpdate(true, 77))
	if err := d.Decode(good); err != nil {
		t.Fatalf("good Decode error = %v, want nil", err)
	}
	if got := d.coefProbs[0][0][0][0]; got != 77 {
		t.Fatalf("persistent coefficient probability = %d, want 77 after good frame", got)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("good NextFrame returned no frame")
	}

	badFirst := vp8FirstPartitionWithSingleCoefProbabilityUpdate(true, 99)
	bad := vp8InterFramePacket(len(badFirst), 0, true)
	bad = append(bad, badFirst...)
	if err := d.Decode(bad); err != nil {
		t.Fatalf("corrupt inter Decode error = %v, want nil concealment", err)
	}
	if !d.lastInfo.Corrupted {
		t.Fatalf("lastInfo = %+v, want concealed corrupted frame", d.lastInfo)
	}
	if got := d.coefProbs[0][0][0][0]; got != 77 {
		t.Fatalf("persistent coefficient probability = %d, want previous successful value 77", got)
	}
	if got := d.previousQuant.BaseQIndex; got != 0 {
		t.Fatalf("previous quant base = %d, want previous successful value 0", got)
	}
}

func TestCommitParsedStateHonorsModeProbabilityRefreshFlag(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	defaultYModeProb := d.modeProbs.YMode[0]
	updatedYModeProb := defaultYModeProb + 1

	d.frameModeProbs = d.modeProbs
	d.frameModeProbs.YMode[0] = updatedYModeProb
	d.state.Refresh.RefreshEntropyProbs = false
	d.commitParsedState(StreamInfo{})
	if got := d.modeProbs.YMode[0]; got != defaultYModeProb {
		t.Fatalf("inter no-refresh mode prob = %d, want previous %d", got, defaultYModeProb)
	}

	d.state.Refresh.RefreshEntropyProbs = true
	d.commitParsedState(StreamInfo{})
	if got := d.modeProbs.YMode[0]; got != updatedYModeProb {
		t.Fatalf("inter refreshed mode prob = %d, want updated %d", got, updatedYModeProb)
	}

	d.modeProbs.YMode[0] = updatedYModeProb
	d.frameModeProbs.YMode[0] = updatedYModeProb + 1
	d.state.Refresh.RefreshEntropyProbs = false
	d.commitParsedState(StreamInfo{KeyFrame: true})
	if got := d.modeProbs.YMode[0]; got != vp8tables.DefaultYModeProbs[0] {
		t.Fatalf("key no-refresh mode prob = %d, want default %d", got, vp8tables.DefaultYModeProbs[0])
	}
}

func TestDecodeIntoErrorConcealmentConcealsCorruptInterFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	keyPacket := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	previous := d.lastRef.Img.Y[0]
	dst := newTestImage(16, 16)
	fillImage(dst, 7, 8, 9)

	info, err := d.DecodeIntoWithPTS(vp8InterFramePacket(0, 0, true), &dst, 101)
	if err != nil {
		t.Fatalf("corrupt inter DecodeIntoWithPTS error = %v, want nil concealment", err)
	}
	if !info.Corrupted || info.PTS != 101 || info.Width != 16 || info.Height != 16 {
		t.Fatalf("FrameInfo = %+v, want corrupted concealed 16x16 PTS 101", info)
	}
	if dst.Y[0] != previous {
		t.Fatalf("concealed dst Y[0] = %d, want previous reference %d", dst.Y[0], previous)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto concealment queued a NextFrame output")
	}
}

func TestDecodeIntoErrorConcealmentConcealsMissingFrameTag(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	keyPacket := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	fillVP8Image(&d.lastRef.Img, 77)
	if err := d.Decode(vp8InterFramePacketWithFirstPartition(vp8InterFirstPartitionLastZeroMV())); err != nil {
		t.Fatalf("priming inter Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("priming inter NextFrame returned no frame")
	}
	previous := d.lastRef.Img.Y[0]
	dst := newTestImage(16, 16)
	fillImage(dst, 7, 8, 9)

	info, err := d.DecodeIntoWithPTS([]byte{0x11, 0}, &dst, 103)
	if err != nil {
		t.Fatalf("missing tag DecodeIntoWithPTS error = %v, want nil concealment", err)
	}
	if !info.Corrupted || info.PTS != 103 || info.Width != 16 || info.Height != 16 || !info.ShowFrame {
		t.Fatalf("FrameInfo = %+v, want visible corrupted concealed 16x16 PTS 103", info)
	}
	if dst.Y[0] != previous {
		t.Fatalf("concealed dst Y[0] = %d, want previous reference %d", dst.Y[0], previous)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto missing-tag concealment queued a NextFrame output")
	}
}

func TestDecodeErrorConcealmentAllocatesZero(t *testing.T) {
	packet := vp8InterFramePacket(0, 0, true)
	keyPacket := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	decode, err := NewVP8Decoder(DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := decode.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := decode.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	allocs := testing.AllocsPerRun(1000, func() {
		_ = decode.Decode(packet)
		_, _ = decode.NextFrame()
	})
	if allocs != 0 {
		t.Fatalf("Decode concealment allocs = %v, want 0", allocs)
	}

	decodeInto, err := NewVP8Decoder(DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := decodeInto.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := decodeInto.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	dst := newTestImage(16, 16)
	allocs = testing.AllocsPerRun(1000, func() {
		_, _ = decodeInto.DecodeInto(packet, &dst)
	})
	if allocs != 0 {
		t.Fatalf("DecodeInto concealment allocs = %v, want 0", allocs)
	}
}

func TestDecodePostProcessConcealsCorruptInterFrameIntoPostFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorConcealment: true, PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	keyPacket := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}

	err = d.DecodeWithPTS(vp8InterFramePacket(0, 0, true), 99)
	if err != nil {
		t.Fatalf("corrupt inter DecodeWithPTS error = %v, want nil concealment", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("concealed NextFrame returned no frame")
	}
	if len(frame.Y) == 0 || len(d.post.Img.Y) == 0 || len(d.lastRef.Img.Y) == 0 {
		t.Fatalf("decoded frame buffers are empty")
	}
	if &frame.Y[0] != &d.post.Img.Y[0] {
		t.Fatalf("concealed postprocess output did not use decoder postprocess buffer")
	}
	if &frame.Y[0] == &d.lastRef.Img.Y[0] {
		t.Fatalf("concealed postprocess output aliases reference buffer")
	}
}

func TestDecodeIntoPostProcessConcealsCorruptInterFrameIntoPostFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorConcealment: true, PostProcessFlags: PostProcessDeblock | PostProcessDemacroblock | PostProcessMFQE})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	keyPacket := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	dst := newTestImage(16, 16)

	info, err := d.DecodeIntoWithPTS(vp8InterFramePacket(0, 0, true), &dst, 101)
	if err != nil {
		t.Fatalf("corrupt inter DecodeIntoWithPTS error = %v, want nil concealment", err)
	}
	if !info.Corrupted || info.PTS != 101 || info.Width != 16 || info.Height != 16 {
		t.Fatalf("FrameInfo = %+v, want corrupted concealed 16x16 PTS 101", info)
	}
	if !publicImageEqualVP8(dst, &d.post.Img) {
		t.Fatalf("concealed DecodeInto output does not match decoder postprocess buffer")
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto concealment queued a NextFrame output")
	}
}

func TestDecodeDoesNotConcealCorruptInterFrameWhenDisabled(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	if err := d.Decode(vp8InterFramePacket(0, 0, true)); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("corrupt inter error = %v, want ErrInvalidData", err)
	}
}

func TestDecodeDoesNotConcealMissingFrameTagWhenDisabled(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}

	err = d.Decode([]byte{0x10, 0})
	if !errors.Is(err, ErrInvalidData) {
		t.Fatalf("missing tag error = %v, want ErrInvalidData", err)
	}
}

func TestDecodeRejectsTruncatedStateHeader(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacket(16, 16, 200, 0, true))
	if !errors.Is(err, ErrInvalidData) {
		t.Fatalf("Decode error = %v, want ErrInvalidData", err)
	}
}
