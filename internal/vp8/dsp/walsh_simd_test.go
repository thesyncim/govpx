package dsp

import (
	"math/rand"
	"testing"
)

// TestInverseWalsh4x4SIMDMatchesScalar checks the per-arch SIMD
// InverseWalsh4x4 kernel against the scalar reference for sentinel cases
// (zero, DC, sign patterns, decoder coefficient range) and randomized fuzz.
//
// The SIMD writes only mbDQCoeff[i*16] for i in 0..15; remaining elements
// must be left untouched, matching the scalar contract.
func TestInverseWalsh4x4SIMDMatchesScalar(t *testing.T) {
	cases := []struct {
		name string
		in   [16]int16
	}{
		{name: "zero"},
		{name: "dc_pos", in: [16]int16{1024}},
		{name: "dc_neg", in: [16]int16{-1024}},
		{name: "ramp", in: [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}},
		{name: "alt_signs", in: [16]int16{
			255, -255, 255, -255,
			-255, 255, -255, 255,
			255, -255, 255, -255,
			-255, 255, -255, 255,
		}},
		{name: "high_pos", in: [16]int16{
			2000, 2000, 2000, 2000,
			2000, 2000, 2000, 2000,
			2000, 2000, 2000, 2000,
			2000, 2000, 2000, 2000,
		}},
		{name: "single_top_left", in: [16]int16{1234}},
		{name: "single_bottom_right", in: [16]int16{
			0, 0, 0, 0,
			0, 0, 0, 0,
			0, 0, 0, 0,
			0, 0, 0, 4321,
		}},
		{name: "increasing", in: [16]int16{
			1, 2, 3, 4,
			5, 6, 7, 8,
			9, 10, 11, 12,
			13, 14, 15, 16,
		}},
		{name: "decreasing", in: [16]int16{
			16, 15, 14, 13,
			12, 11, 10, 9,
			8, 7, 6, 5,
			4, 3, 2, 1,
		}},
		{name: "negatives_only", in: [16]int16{
			-1, -2, -3, -4,
			-5, -6, -7, -8,
			-9, -10, -11, -12,
			-13, -14, -15, -16,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			simd := make([]int16, 16*16)
			scalar := make([]int16, 16*16)
			for i := range simd {
				simd[i] = -999
				scalar[i] = -999
			}
			InverseWalsh4x4(&tc.in, simd)
			inverseWalsh4x4Scalar(&tc.in, scalar)
			for i := range 16 * 16 {
				if simd[i] != scalar[i] {
					t.Fatalf("idx=%d simd=%d scalar=%d (in=%v)", i, simd[i], scalar[i], tc.in)
				}
			}
		})
	}

	// Random fuzz across the typical decoder Y2 dequantized range. The
	// libvpx NEON / SSE2 references operate entirely in int16 lanes; the
	// scalar reference uses 64-bit intermediates. We restrict the fuzz to
	// |val| <= 1024, which covers the full realistic VP8 Y2 dequantized
	// coefficient range (post-quant + dq) without int16 overflow after the
	// 2-pass butterfly amplifies by up to 16x.
	r := rand.New(rand.NewSource(0xCAFEBABE))
	for iter := range 4000 {
		var in [16]int16
		for i := range in {
			in[i] = int16(r.Intn(2049) - 1024)
		}
		simd := make([]int16, 16*16)
		scalar := make([]int16, 16*16)
		for i := range simd {
			simd[i] = -1
			scalar[i] = -1
		}
		InverseWalsh4x4(&in, simd)
		inverseWalsh4x4Scalar(&in, scalar)
		for i := range 16 * 16 {
			if simd[i] != scalar[i] {
				t.Fatalf("iter=%d idx=%d simd=%d scalar=%d (in=%v)", iter, i, simd[i], scalar[i], in)
			}
		}
	}
}

func BenchmarkInverseWalsh4x4SIMD(b *testing.B) {
	var input [16]int16
	for i := range input {
		input[i] = int16(i*7 - 48)
	}
	coeff := make([]int16, 16*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		InverseWalsh4x4(&input, coeff)
	}
}

func BenchmarkInverseWalsh4x4Scalar(b *testing.B) {
	var input [16]int16
	for i := range input {
		input[i] = int16(i*7 - 48)
	}
	coeff := make([]int16, 16*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		inverseWalsh4x4Scalar(&input, coeff)
	}
}
