package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestCondProbDiffUpdateNoUpdate: when newp == oldp we emit only the
// "no update" bit and the decoder leaves the slot unchanged.
func TestCondProbDiffUpdateNoUpdate(t *testing.T) {
	dst := make([]byte, 64)
	var bw bitstream.Writer
	bw.Start(dst)
	old := uint8(128)
	CondProbDiffUpdate(&bw, old, old)
	n, err := bw.Stop()
	if err != nil {
		t.Fatalf("bw.Stop: %v", err)
	}

	var r bitstream.Reader
	if err := r.Init(dst[:n]); err != nil {
		t.Fatalf("Reader.Init: %v", err)
	}
	got := old
	vp9dec.VpxDiffUpdateProb(&r, &got)
	if got != old {
		t.Errorf("no-update changed prob: got %d, want %d", got, old)
	}
}

// TestCondProbDiffUpdateRoundTrip walks a spread of (oldp, newp)
// pairs through the writer and confirms the decoder recovers each
// newp byte-for-byte. The hand-picked pairs hit each of the
// encode_term_subexp buckets (4-bit / second 4-bit / 5-bit / uniform).
func TestCondProbDiffUpdateRoundTrip(t *testing.T) {
	cases := []struct {
		old, new uint8
	}{
		{128, 1},   // far end of 4-bit bucket
		{128, 64},  // mid range
		{128, 192}, // upper half
		{128, 255},
		{32, 200},
		{200, 32},
		{1, 254},
		{50, 60}, // small delta
	}
	for i, c := range cases {
		dst := make([]byte, 64)
		var bw bitstream.Writer
		bw.Start(dst)
		CondProbDiffUpdate(&bw, c.old, c.new)
		n, err := bw.Stop()
		if err != nil {
			t.Fatalf("case %d: bw.Stop: %v", i, err)
		}

		var r bitstream.Reader
		if err := r.Init(dst[:n]); err != nil {
			t.Fatalf("case %d: Reader.Init: %v", i, err)
		}
		got := c.old
		vp9dec.VpxDiffUpdateProb(&r, &got)
		if got != c.new {
			t.Errorf("case %d: (old=%d, new=%d) round-tripped to %d",
				i, c.old, c.new, got)
		}
	}
}

// TestEncodeTermSubexpRoundTripFuzz walks every word in [0, 255]
// through the writer + decoder to confirm the prefix-code + uniform
// tail handle every magnitude bucket.
func TestEncodeTermSubexpRoundTripFuzz(t *testing.T) {
	for word := 0; word < 255; word++ {
		dst := make([]byte, 64)
		var bw bitstream.Writer
		bw.Start(dst)
		encodeTermSubexp(&bw, word)
		n, err := bw.Stop()
		if err != nil {
			t.Fatalf("word=%d: bw.Stop: %v", word, err)
		}
		var r bitstream.Reader
		if err := r.Init(dst[:n]); err != nil {
			t.Fatalf("word=%d: Reader.Init: %v", word, err)
		}
		// The decoder helper isn't exported, so re-derive its body
		// here as the inverse of encodeTermSubexp.
		got := decodeTermSubexpForTest(t, &r)
		if got != word {
			t.Errorf("word=%d: round-trip got %d", word, got)
		}
	}
}

// decodeTermSubexpForTest mirrors decoder.decodeTermSubexp (private).
// Local copy is fine here because the encoder and decoder must agree
// on the wire format anyway.
func decodeTermSubexpForTest(t *testing.T, r *bitstream.Reader) int {
	t.Helper()
	if r.ReadBit() == 0 {
		return int(r.ReadLiteral(4))
	}
	if r.ReadBit() == 0 {
		return int(r.ReadLiteral(4)) + 16
	}
	if r.ReadBit() == 0 {
		return int(r.ReadLiteral(5)) + 32
	}
	// Uniform tail: 7-bit, optional 1-bit extension when value >= 65.
	v := int(r.ReadLiteral(7))
	if v < 65 {
		return v + 64
	}
	return ((v - 65) << 1) + int(r.ReadBit()) + 65 + 64
}
