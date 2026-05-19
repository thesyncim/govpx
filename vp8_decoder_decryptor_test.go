package govpx

import (
	"bytes"
	"testing"
)

// TestVP8DecoderDecryptorRoundTripsClearKeyFrame asserts that wiring a
// trivial identity-decrypt callback through DecoderOptions does not
// change the decoded output of a clean key frame. Mirrors libvpx's
// vp8_set_decryptor + decode round trip: when the callback is a
// passthrough, the decoder should produce the same Y plane as a
// decryptor-less decoder.
func TestVP8DecoderDecryptorRoundTripsClearKeyFrame(t *testing.T) {
	packet := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)

	plain, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("plain NewVP8Decoder error = %v", err)
	}
	if err := plain.Decode(packet); err != nil {
		t.Fatalf("plain Decode error = %v", err)
	}
	plainFrame, ok := plain.NextFrame()
	if !ok {
		t.Fatalf("plain NextFrame returned no frame")
	}

	identity := func(state any, src, dst []byte, count int) {
		copy(dst[:count], src[:count])
	}
	dec, err := NewVP8Decoder(DecoderOptions{Decryptor: identity})
	if err != nil {
		t.Fatalf("decryptor NewVP8Decoder error = %v", err)
	}
	if err := dec.Decode(packet); err != nil {
		t.Fatalf("decryptor Decode error = %v", err)
	}
	decFrame, ok := dec.NextFrame()
	if !ok {
		t.Fatalf("decryptor NextFrame returned no frame")
	}
	if !bytes.Equal(plainFrame.Y, decFrame.Y) {
		t.Fatalf("identity decryptor changed Y plane")
	}
	if !bytes.Equal(plainFrame.U, decFrame.U) {
		t.Fatalf("identity decryptor changed U plane")
	}
	if !bytes.Equal(plainFrame.V, decFrame.V) {
		t.Fatalf("identity decryptor changed V plane")
	}
}

// TestVP8DecoderDecryptorCallbackIsInvoked confirms the callback fires
// during Decode, proving the boolcoder-level wiring is reached end to
// end. Identity-byte-equivalence of the decoded frame is covered by
// TestVP8DecoderDecryptorRoundTripsClearKeyFrame; this test just
// verifies the callback path is live.
func TestVP8DecoderDecryptorCallbackIsInvoked(t *testing.T) {
	calls := 0
	identity := func(state any, src, dst []byte, count int) {
		calls++
		copy(dst[:count], src[:count])
	}
	dec, err := NewVP8Decoder(DecoderOptions{Decryptor: identity})
	if err != nil {
		t.Fatalf("NewVP8Decoder error = %v", err)
	}
	packet := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	if err := dec.Decode(packet); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if calls == 0 {
		t.Fatalf("decrypt callback was never invoked during Decode")
	}
}

func TestVP9DecoderDecryptorRoundTripsClearKeyFrame(t *testing.T) {
	packet := vp9EncodedKeyframeForTest(t, 32, 32, 96)
	want := vp9DecodeLastVisibleFrameForTest(t, packet)

	calls := 0
	identity := func(state any, src, dst []byte, count int) {
		calls++
		copy(dst[:count], src[:count])
	}
	dec, err := NewVP9Decoder(VP9DecoderOptions{Decryptor: identity})
	if err != nil {
		t.Fatalf("NewVP9Decoder error = %v", err)
	}
	if err := dec.Decode(packet); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	got, ok := dec.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if calls == 0 {
		t.Fatalf("decrypt callback was never invoked during VP9 Decode")
	}
	assertVP9ImagesEqual(t, want, got)
}

func TestVP9DecoderDecryptorDecryptsEncryptedPacket(t *testing.T) {
	const key = byte(0x5a)
	packet := vp9EncodedKeyframeForTest(t, 32, 32, 144)
	encrypted := xorVP9PacketForTest(packet, key)
	want := vp9DecodeLastVisibleFrameForTest(t, packet)

	plain, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("plain NewVP9Decoder error = %v", err)
	}
	if err := plain.Decode(encrypted); err == nil {
		t.Fatalf("encrypted packet decoded without decryptor")
	}

	dec, err := NewVP9Decoder(VP9DecoderOptions{
		Decryptor:      xorVP9DecryptorForTest,
		DecryptorState: key,
	})
	if err != nil {
		t.Fatalf("decryptor NewVP9Decoder error = %v", err)
	}
	if err := dec.Decode(encrypted); err != nil {
		t.Fatalf("decryptor Decode error = %v", err)
	}
	got, ok := dec.NextFrame()
	if !ok {
		t.Fatalf("decryptor NextFrame returned no frame")
	}
	assertVP9ImagesEqual(t, want, got)
}

func TestVP9DecoderDecryptorDecodeIntoDecryptsOnce(t *testing.T) {
	const key = byte(0x5a)
	packet := vp9EncodedKeyframeForTest(t, 32, 32, 112)
	encrypted := xorVP9PacketForTest(packet, key)
	want := vp9DecodeLastVisibleFrameForTest(t, packet)

	calls := 0
	decryptor := func(state any, src, dst []byte, count int) {
		calls++
		xorVP9DecryptorForTest(state, src, dst, count)
	}
	dec, err := NewVP9Decoder(VP9DecoderOptions{
		Decryptor:      decryptor,
		DecryptorState: key,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder error = %v", err)
	}
	dst := newTestImage(32, 32)
	info, err := dec.DecodeIntoWithPTS(encrypted, &dst, 99)
	if err != nil {
		t.Fatalf("DecodeIntoWithPTS error = %v", err)
	}
	if info.PTS != 99 || !info.ShowFrame || info.Width != 32 || info.Height != 32 {
		t.Fatalf("DecodeIntoWithPTS info = %+v, want visible 32x32 PTS 99", info)
	}
	if calls != 1 {
		t.Fatalf("decrypt callback calls = %d, want 1 packet-entry call", calls)
	}
	assertVP9ImagesEqual(t, want, dst)
}

func TestVP9DecoderDecryptorDecryptsEncryptedSuperframeIndex(t *testing.T) {
	const key = byte(0x5a)
	first := vp9EncodedKeyframeForTest(t, 32, 32, 64)
	second := vp9EncodedKeyframeForTest(t, 32, 32, 176)
	packet := vp9SuperframePacketForTest(first, second)
	encrypted := xorVP9PacketForTest(packet, key)
	want := vp9DecodeLastVisibleFrameForTest(t, packet)

	if sf, err := vp9ParseSuperframe(encrypted); err != nil || sf.count != 0 {
		t.Fatalf("encrypted superframe index parsed without decryptor: count=%d err=%v",
			sf.count, err)
	}

	calls := 0
	decryptor := func(state any, src, dst []byte, count int) {
		calls++
		xorVP9DecryptorForTest(state, src, dst, count)
	}
	dec, err := NewVP9Decoder(VP9DecoderOptions{
		Decryptor:      decryptor,
		DecryptorState: key,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder error = %v", err)
	}
	if err := dec.Decode(encrypted); err != nil {
		t.Fatalf("Decode encrypted superframe error = %v", err)
	}
	got, ok := dec.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if calls != 1 {
		t.Fatalf("decrypt callback calls = %d, want 1 packet-entry call", calls)
	}
	assertVP9ImagesEqual(t, want, got)
}

func xorVP9PacketForTest(src []byte, key byte) []byte {
	dst := make([]byte, len(src))
	for i, b := range src {
		dst[i] = b ^ key
	}
	return dst
}

func xorVP9DecryptorForTest(state any, src, dst []byte, count int) {
	key := state.(byte)
	for i := range count {
		dst[i] = src[i] ^ key
	}
}
