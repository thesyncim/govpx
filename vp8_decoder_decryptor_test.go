package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8DecoderDecryptorRoundTripsClearKeyFrame asserts that wiring a
// trivial identity-decrypt callback through DecoderOptions does not
// change the decoded output of a clean key frame. Mirrors libvpx's
// vp8_set_decryptor + decode round trip: when the callback is a
// passthrough, the decoder should produce the same Y plane as a
// decryptor-less decoder.
func TestVP8DecoderDecryptorRoundTripsClearKeyFrame(t *testing.T) {
	packet := vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)

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
	packet := vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)
	if err := dec.Decode(packet); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if calls == 0 {
		t.Fatalf("decrypt callback was never invoked during Decode")
	}
}
