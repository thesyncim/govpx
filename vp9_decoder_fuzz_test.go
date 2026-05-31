package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// FuzzVP9DecoderDecode feeds arbitrary bytes to VP9Decoder.Decode and asserts
// the decoder never panics and only returns sentinel errors documented for
// untrusted input. Go writes failing fuzz inputs to
// testdata/fuzz/FuzzVP9DecoderDecode/ and replays them in regular test runs.
func FuzzVP9DecoderDecode(f *testing.F) {
	seeds := vp9DecoderFuzzSeeds(f)
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, packet []byte) {
		d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
			MaxWidth: 256, MaxHeight: 256,
		})
		if err != nil {
			t.Fatalf("NewVP9Decoder: %v", err)
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Decode panicked on %d-byte input: %v", len(packet), r)
			}
			_ = d.Close()
		}()
		if err := d.Decode(packet); err != nil {
			assertVP9FuzzDecodeError(t, err)
		}
		// Re-entrancy: ensure Decode → Reset → Decode is safe on the
		// same bytes. Reset must not be allowed to panic regardless of
		// internal state, and a second pass must still return one of
		// the documented sentinels.
		d.Reset()
		if err := d.Decode(packet); err != nil {
			assertVP9FuzzDecodeError(t, err)
		}
		_, _ = d.NextFrame()
	})
}

// FuzzVP9DecoderDecodeInto exercises the DecodeInto path with a caller-owned
// destination image so the I420 plane writeback hits arbitrary user buffers.
// The destination is intentionally sized smaller than the fuzz inputs so the
// reject path is exercised alongside the happy path.
func FuzzVP9DecoderDecodeInto(f *testing.F) {
	seeds := vp9DecoderFuzzSeeds(f)
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, packet []byte) {
		d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
			MaxWidth: 256, MaxHeight: 256,
		})
		if err != nil {
			t.Fatalf("NewVP9Decoder: %v", err)
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("DecodeInto panicked on %d-byte input: %v", len(packet), r)
			}
			_ = d.Close()
		}()
		dst := newVP9TestImageForTest(64, 64)
		if _, err := d.DecodeInto(packet, &dst); err != nil {
			assertVP9FuzzDecodeError(t, err)
		}
		// Run a second DecodeInto pass with a larger destination so
		// the path that may have rejected the dst before is reached
		// after a state-carry frame.
		dst2 := newVP9TestImageForTest(256, 256)
		if _, err := d.DecodeInto(packet, &dst2); err != nil {
			assertVP9FuzzDecodeError(t, err)
		}
	})
}

// assertVP9FuzzDecodeError pins the set of errors the VP9 decoder may return
// for arbitrary inputs. Anything else means the decoder leaked an internal
// sentinel or panicked in disguise — both of which are bugs.
func assertVP9FuzzDecodeError(t *testing.T, err error) {
	t.Helper()
	switch {
	case errors.Is(err, govpx.ErrInvalidVP9Data):
	case errors.Is(err, govpx.ErrVP9NotImplemented):
	case errors.Is(err, govpx.ErrFrameRejected):
	case errors.Is(err, govpx.ErrInvalidConfig):
	case errors.Is(err, govpx.ErrClosed):
	default:
		t.Fatalf("Decode returned unexpected error: %v", err)
	}
}

// vp9DecoderFuzzSeeds returns a hand-curated seed corpus that steers go fuzz
// towards interesting decoder branches: empty/short packets, malformed
// uncompressed headers, valid keyframes, and valid superframes.
func vp9DecoderFuzzSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	seeds := [][]byte{
		nil,
		{},
		{0},
		{0x82},
		{0x82, 0x49},
		{0x82, 0x49, 0x83},
		{0x82, 0x49, 0x83, 0x42},
		// frame_marker=10, profile=3 (out of profile-0 scope) → triggers
		// ErrVP9NotImplemented after enough header bytes.
		{0xb0, 0x49, 0x83, 0x42, 0x00, 0x00, 0x00, 0x00},
		// VP9 superframe index marker only (no body, not a real
		// superframe).
		{0xc0},
		// Trailing marker that claims one 1-byte frame, but content
		// is too small for the parser to extract.
		{0xc0, 0x00, 0xc0},
		// Two-frame superframe index with mismatched bytes.
		{0xff, 0xff, 0xc1, 0x01, 0x01, 0xc1},
	}
	// Append a real visible keyframe so the corpus contains valid
	// bitstreams that go fuzz can mutate around.
	if pkt := vp9FuzzEncodedKeyframe(tb, 16, 16); len(pkt) > 0 {
		seeds = append(seeds, pkt)
	}
	if pkt := vp9FuzzEncodedKeyframe(tb, 64, 64); len(pkt) > 0 {
		seeds = append(seeds, pkt)
	}
	return seeds
}

// vp9FuzzEncodedKeyframe encodes a single visible VP9 keyframe with the public
// encoder at a fixed quantizer for use as a fuzz seed. Returns nil if the
// encoder is unavailable so the fuzz harness still runs on smoke seeds.
func vp9FuzzEncodedKeyframe(tb testing.TB, width, height int) []byte {
	tb.Helper()
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: 64,
	})
	if err != nil {
		return nil
	}
	defer e.Close()
	packet, err := e.Encode(vp9test.NewYCbCr(width, height, 96, 128, 128))
	if err != nil || len(packet) == 0 {
		return nil
	}
	return append([]byte(nil), packet...)
}
