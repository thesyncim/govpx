package dsp

import (
	"math/rand"
	"testing"
)

func TestTransformBlockErrorScalarMatchesReference(t *testing.T) {
	// Reference closed-form across a few hand-built cases to pin the
	// kernel semantics independent of the SIMD path.
	cases := []struct {
		coeff   [16]int16
		dqcoeff [16]int16
		want    int
	}{
		{},
		{
			coeff:   [16]int16{1, -1, 2, -2, 3, -3, 4, -4, 5, -5, 6, -6, 7, -7, 8, -8},
			dqcoeff: [16]int16{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			want:    1 + 1 + 4 + 4 + 9 + 9 + 16 + 16 + 25 + 25 + 36 + 36 + 49 + 49 + 64 + 64,
		},
		{
			coeff:   [16]int16{100, 200, -300, 400, -500, 600, -700, 800, 900, -1000, 1100, -1200, 1300, -1400, 1500, -1600},
			dqcoeff: [16]int16{99, 198, -296, 392, -488, 580, -676, 768, 864, -960, 1056, -1152, 1248, -1344, 1440, -1536},
		},
	}
	// Fill in want for the third case via the scalar reference.
	cases[2].want = transformBlockErrorScalar(&cases[2].coeff, &cases[2].dqcoeff)

	for i, tc := range cases {
		got := TransformBlockError(&tc.coeff, &tc.dqcoeff)
		if got != tc.want {
			t.Fatalf("case %d: got %d want %d", i, got, tc.want)
		}
	}
}

func TestTransformBlockErrorSIMDMatchesScalar(t *testing.T) {
	// Random fuzz with realistic DCT-coeff ranges (well within int16
	// such that PSUBW / SUB don't see overflow).
	rng := rand.New(rand.NewSource(0xC0FFEE))
	for trial := range 4096 {
		var coeff, dq [16]int16
		for i := range coeff {
			coeff[i] = int16(rng.Intn(8001) - 4000) // [-4000, 4000]
			dq[i] = int16(rng.Intn(8001) - 4000)
		}
		want := transformBlockErrorScalar(&coeff, &dq)
		got := TransformBlockError(&coeff, &dq)
		if got != want {
			t.Fatalf("trial %d: SIMD got %d want %d (coeff=%v dq=%v)", trial, got, want, coeff, dq)
		}
	}
}

func TestTransformBlockErrorRealisticRange(t *testing.T) {
	// Quantization-error magnitudes in real VP8 encodes stay well within
	// int16; the SIMD kernel mirrors libvpx vp8_block_error_sse2 which
	// uses int16-domain PSUBW + PMADDWD and so wraps for diffs that
	// don't fit in int16. Verify SIMD == scalar across the full DCT
	// coefficient range that the encoder actually produces (peak around
	// +/- 2048 after the forward transform, well-clear of int16 wrap).
	coeff := [16]int16{2047, -2048, 1500, -1500, 1000, -1000, 500, -500,
		2047, -2048, 1500, -1500, 1000, -1000, 500, -500}
	var dq [16]int16
	for i := range dq {
		dq[i] = coeff[i] / 4
	}
	want := transformBlockErrorScalar(&coeff, &dq)
	got := TransformBlockError(&coeff, &dq)
	if got != want {
		t.Fatalf("realistic range: got %d want %d", got, want)
	}
}

func BenchmarkTransformBlockError(b *testing.B) {
	rng := rand.New(rand.NewSource(0xBEEF))
	var coeff, dq [16]int16
	for i := range coeff {
		coeff[i] = int16(rng.Intn(2001) - 1000)
		dq[i] = int16(rng.Intn(2001) - 1000)
	}
	b.ResetTimer()
	var sink int
	for i := 0; i < b.N; i++ {
		sink ^= TransformBlockError(&coeff, &dq)
	}
	_ = sink
}

func BenchmarkTransformBlockErrorScalar(b *testing.B) {
	rng := rand.New(rand.NewSource(0xBEEF))
	var coeff, dq [16]int16
	for i := range coeff {
		coeff[i] = int16(rng.Intn(2001) - 1000)
		dq[i] = int16(rng.Intn(2001) - 1000)
	}
	b.ResetTimer()
	var sink int
	for i := 0; i < b.N; i++ {
		sink ^= transformBlockErrorScalar(&coeff, &dq)
	}
	_ = sink
}
