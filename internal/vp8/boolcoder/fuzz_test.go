package boolcoder

import (
	"testing"
)

// FuzzVP8BoolCoderRoundtrip drives the VP8 boolean coder with arbitrary
// (bit, probability) pair streams. Each input is split into two halves: the
// first half supplies the bit stream, the second half supplies the
// probability stream. The pair is encoded with the libvpx-equivalent
// testWriter and then decoded with the production Decoder. Any drift
// between encode and decode (carry propagation, range renorm, count
// underflow, etc.) is caught here.
//
// libvpx reference: vp8/encoder/boolhuff.c and vp8/decoder/dboolhuff.c
// (v1.16.0).
func FuzzVP8BoolCoderRoundtrip(f *testing.F) {
	// Seed corpus: cover representative bit-length / probability mixes.
	//
	// The encoder takes (bit_byte, prob_byte) pairs; bit is lsb of the
	// bit_byte, prob is the prob_byte clamped away from 0 inside the fuzz
	// body. Probability=0 is illegal in libvpx (split would be 1 with
	// rng-1 underflow); seed values exercise the {1, 127, 128, 255}
	// boundaries plus a mid-range tail. Bit lengths are {0, 1, 7, 8, 9,
	// 32, 33, 4095}.
	for _, seed := range boolCoderFuzzSeeds() {
		f.Add(seed.bits, seed.probs)
	}

	f.Fuzz(func(t *testing.T, rawBits, rawProbs []byte) {
		n := min(len(rawProbs), len(rawBits))
		if n == 0 {
			return
		}
		// Clamp to a sane upper bound so fuzz runs stay cheap. The
		// renorm logic is exercised once per ~7 bits; 8192 pairs is
		// enough to cover every renorm shift count multiple times.
		if n > 8192 {
			n = 8192
		}

		bits := make([]uint8, n)
		probs := make([]uint8, n)
		for i := 0; i < n; i++ {
			bits[i] = rawBits[i] & 1
			probs[i] = rawProbs[i]
			if probs[i] == 0 {
				// libvpx invariant: prob is in [1, 255]. The
				// encoder will not be called with prob=0, so
				// clamp to 1 here rather than skip.
				probs[i] = 1
			}
		}

		var w testWriter
		w.init()
		for i := 0; i < n; i++ {
			w.writeBool(bits[i], probs[i])
		}
		payload := w.finish()

		var d Decoder
		if err := d.Init(payload); err != nil {
			t.Fatalf("Init returned error: %v", err)
		}
		for i := 0; i < n; i++ {
			got := d.ReadBool(probs[i])
			if got != bits[i] {
				t.Fatalf("roundtrip mismatch at index %d (prob=%d): got %d, want %d", i, probs[i], got, bits[i])
			}
		}
		if err := d.Err(); err != nil {
			t.Fatalf("decoder Err after roundtrip: %v", err)
		}
	})
}

// FuzzVP8BoolCoderRangeNormalization drives the boolcoder with the same
// pair-stream shape but biases the probability distribution towards the
// renormalization extremes (prob in {1, 255}) and forces a small fraction
// of mid-range probs so the renorm shift table is exercised at every
// shift count. It also drives ReadBit / ReadLiteral paths so the fixed
// probability=128 split (1 + (rng-1)*128) >> 8) renorm is fuzzed too.
func FuzzVP8BoolCoderRangeNormalization(f *testing.F) {
	// Seed cases that pin probabilities at 1 and 255 — the values at
	// which the renorm shift in tables.BoolNorm is maximal (7 and 0
	// respectively, depending on which side of the split the value
	// lands on).
	extremes := []struct{ bits, probs []byte }{
		{bits: zeros(1), probs: fillByte(1, 1)},
		{bits: ones(1), probs: fillByte(1, 1)},
		{bits: zeros(1), probs: fillByte(1, 255)},
		{bits: ones(1), probs: fillByte(1, 255)},
		{bits: alternating(8), probs: fillByte(8, 1)},
		{bits: alternating(8), probs: fillByte(8, 255)},
		{bits: alternating(9), probs: fillByte(9, 1)},
		{bits: alternating(9), probs: fillByte(9, 255)},
		{bits: alternating(33), probs: fillByte(33, 1)},
		{bits: alternating(33), probs: fillByte(33, 255)},
		{bits: alternating(4095), probs: fillByte(4095, 1)},
		{bits: alternating(4095), probs: fillByte(4095, 255)},
		// 0xff run: stresses propagateCarry on the encoder side
		// (only reachable through this fuzz target via the
		// indirect testWriter path).
		{bits: ones(64), probs: fillByte(64, 1)},
		{bits: zeros(64), probs: fillByte(64, 255)},
	}
	for _, s := range extremes {
		f.Add(s.bits, s.probs)
	}

	f.Fuzz(func(t *testing.T, rawBits, rawProbs []byte) {
		n := min(len(rawProbs), len(rawBits))
		if n == 0 {
			return
		}
		if n > 8192 {
			n = 8192
		}

		bits := make([]uint8, n)
		probs := make([]uint8, n)
		for i := 0; i < n; i++ {
			bits[i] = rawBits[i] & 1
			// Map probability into one of {1, 127, 128, 255}
			// or pass-through for "wide" coverage. The mapping
			// keeps the fuzz weighted towards the renorm
			// extremes while still exercising mid-range values.
			switch rawProbs[i] & 0x07 {
			case 0:
				probs[i] = 1
			case 1:
				probs[i] = 255
			case 2:
				probs[i] = 127
			case 3:
				probs[i] = 128
			default:
				probs[i] = rawProbs[i]
				if probs[i] == 0 {
					probs[i] = 1
				}
			}
		}

		var w testWriter
		w.init()
		for i := 0; i < n; i++ {
			w.writeBool(bits[i], probs[i])
		}
		payload := w.finish()

		// Roundtrip via ReadBool.
		var d Decoder
		if err := d.Init(payload); err != nil {
			t.Fatalf("Init returned error: %v", err)
		}
		for i := 0; i < n; i++ {
			got := d.ReadBool(probs[i])
			if got != bits[i] {
				t.Fatalf("ReadBool mismatch at index %d (prob=%d): got %d, want %d", i, probs[i], got, bits[i])
			}
		}
		if err := d.Err(); err != nil {
			t.Fatalf("decoder Err after ReadBool roundtrip: %v", err)
		}

		// Now feed a fixed-prob-128 stream so ReadBit and
		// ReadLiteral are exercised. Use the same bit slice.
		var w2 testWriter
		w2.init()
		for i := 0; i < n; i++ {
			w2.writeBool(bits[i], 128)
		}
		payload2 := w2.finish()

		var d2 Decoder
		if err := d2.Init(payload2); err != nil {
			t.Fatalf("Init returned error: %v", err)
		}
		// Read half with ReadBit, half with ReadLiteral, so the
		// fixed-prob renorm hot path is fuzzed both ways.
		half := n / 2
		for i := range half {
			got := d2.ReadBit()
			if got != bits[i] {
				t.Fatalf("ReadBit mismatch at index %d: got %d, want %d", i, got, bits[i])
			}
		}
		// Read the remaining bits as a sequence of literals.
		rem := n - half
		// Use up to 31-bit literals so the literal accumulator
		// (uint32) does not overflow.
		idx := half
		for rem > 0 {
			width := min(rem, 31)
			var want uint32
			for j := range width {
				want |= uint32(bits[idx+j]) << uint(width-1-j)
			}
			got := d2.ReadLiteral(width)
			if got != want {
				t.Fatalf("ReadLiteral(%d) at index %d: got 0x%x, want 0x%x", width, idx, got, want)
			}
			idx += width
			rem -= width
		}
		if err := d2.Err(); err != nil {
			t.Fatalf("decoder Err after ReadBit/ReadLiteral roundtrip: %v", err)
		}
	})
}

// boolCoderFuzzSeeds returns the deterministic seed pairs used by the
// roundtrip fuzz target. Each entry covers a (length, probability) corner
// listed in the harness brief.
func boolCoderFuzzSeeds() []struct{ bits, probs []byte } {
	type seed = struct{ bits, probs []byte }
	lengths := []int{0, 1, 7, 8, 9, 32, 33, 4095}
	probVals := []byte{1, 127, 128, 255}
	bitPatterns := []func(int) []byte{zeros, ones, alternating, lowNibble}

	var out []seed
	for _, n := range lengths {
		for _, p := range probVals {
			for _, bp := range bitPatterns {
				out = append(out, seed{bits: bp(n), probs: fillByte(n, p)})
			}
		}
	}
	// Also add a mixed-prob seed at the longest length so the renorm
	// shift table is exercised across the full range.
	mixedProbs := make([]byte, 4095)
	for i := range mixedProbs {
		mixedProbs[i] = byte(((i * 17) + 1) & 0xff)
		if mixedProbs[i] == 0 {
			mixedProbs[i] = 1
		}
	}
	out = append(out, seed{bits: alternating(4095), probs: mixedProbs})
	return out
}

func zeros(n int) []byte { return make([]byte, n) }

func ones(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 1
	}
	return b
}

func alternating(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i & 1)
	}
	return b
}

func lowNibble(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i >> 2) & 1)
	}
	return b
}

func fillByte(n int, v byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = v
	}
	return b
}
