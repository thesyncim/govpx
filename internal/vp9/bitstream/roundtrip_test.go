package bitstream

import (
	"math/rand"
	"testing"
)

// TestRoundTripFixedProb writes a stream of bits against a fixed
// probability and checks the reader recovers the same sequence. This
// exercises the carry-propagation, normalization, and fill paths in
// isolation from any libvpx-specific framing.
func TestRoundTripFixedProb(t *testing.T) {
	cases := []struct {
		name string
		prob uint32
	}{
		{"prob1", 1},
		{"prob32", 32},
		{"prob128", 128},
		{"prob200", 200},
		{"prob255", 255},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const n = 4096
			rng := rand.New(rand.NewSource(0x9e3779b9))
			bits := make([]uint32, n)
			for i := range bits {
				bits[i] = uint32(rng.Intn(2))
			}

			buf := make([]byte, 8192)
			var w Writer
			w.Start(buf)
			for _, b := range bits {
				w.Write(b, tc.prob)
			}
			size, err := w.Stop()
			if err != nil {
				t.Fatalf("Stop: %v", err)
			}

			var r Reader
			if err := r.Init(buf[:size]); err != nil {
				t.Fatalf("Init: %v", err)
			}
			for i, want := range bits {
				if got := r.Read(tc.prob); got != want {
					t.Fatalf("bit %d: got %d, want %d", i, got, want)
				}
			}
		})
	}
}

// TestRoundTripMixedProb varies the probability per bit so we exercise
// the writer's split-recompute / normalization shift / fill paths over a
// wider range of (range, prob) inputs than the fixed-probability case.
func TestRoundTripMixedProb(t *testing.T) {
	const n = 8192
	rng := rand.New(rand.NewSource(0xdeadbeef))
	bits := make([]uint32, n)
	probs := make([]uint32, n)
	for i := range bits {
		bits[i] = uint32(rng.Intn(2))
		probs[i] = uint32(1 + rng.Intn(255))
	}

	buf := make([]byte, 16384)
	var w Writer
	w.Start(buf)
	for i := range bits {
		w.Write(bits[i], probs[i])
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for i := range bits {
		if got := r.Read(probs[i]); got != bits[i] {
			t.Fatalf("bit %d (prob %d): got %d, want %d", i, probs[i], got, bits[i])
		}
	}
}

// TestRoundTripLiterals exercises the multi-bit literal helpers used by
// the VP9 uncompressed header parser.
func TestRoundTripLiterals(t *testing.T) {
	values := []struct {
		data uint32
		bits uint32
	}{
		{0x00, 1},
		{0x1, 1},
		{0x5, 3},
		{0xff, 8},
		{0x1234, 16},
		{0xa5a5a5, 24},
		{0xdeadbeef, 32},
	}

	buf := make([]byte, 1024)
	var w Writer
	w.Start(buf)
	for _, v := range values {
		w.WriteLiteral(v.data, v.bits)
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for i, v := range values {
		mask := uint32(1)<<uint(v.bits) - 1
		want := v.data & mask
		if got := r.ReadLiteral(int(v.bits)); got != want {
			t.Fatalf("literal %d: got %x, want %x", i, got, want)
		}
	}
}

// TestReaderRejectsBadMarker verifies that an Init against a buffer whose
// very first decoded bit is nonzero is rejected, matching the upstream
// vpx_reader_init contract.
func TestReaderRejectsBadMarker(t *testing.T) {
	src := []byte{0xff, 0x00, 0x00, 0x00}
	var r Reader
	if err := r.Init(src); err == nil {
		t.Fatal("expected ErrInvalidInput on bad marker bit, got nil")
	}
}
