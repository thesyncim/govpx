package encoder

import (
	"math/rand"
	"testing"
)

// TestForwardWalsh4x4SIMDMatchesScalar checks the per-arch SIMD
// ForwardWalsh4x4 kernel against the scalar reference for sentinel cases
// (zero, DC, sign patterns, residual range) and randomized fuzz across
// the encoder's typical pixel-residual range.
func TestForwardWalsh4x4SIMDMatchesScalar(t *testing.T) {
	cases := []struct {
		name string
		in   [16]int16
	}{
		{name: "zero"},
		{name: "dc_pos", in: [16]int16{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5}},
		{name: "dc_neg", in: [16]int16{-5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5}},
		{name: "ramp", in: [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}},
		{name: "alt_signs", in: [16]int16{
			255, -255, 255, -255,
			-255, 255, -255, 255,
			255, -255, 255, -255,
			-255, 255, -255, 255,
		}},
		{name: "max_pos", in: [16]int16{
			255, 255, 255, 255,
			255, 255, 255, 255,
			255, 255, 255, 255,
			255, 255, 255, 255,
		}},
		{name: "max_neg", in: [16]int16{
			-256, -256, -256, -256,
			-256, -256, -256, -256,
			-256, -256, -256, -256,
			-256, -256, -256, -256,
		}},
		{name: "single_top_left", in: [16]int16{255}},
		{name: "single_bottom_right", in: [16]int16{
			0, 0, 0, 0,
			0, 0, 0, 0,
			0, 0, 0, 0,
			0, 0, 0, 255,
		}},
		// Test with a1 == 0 sentinel: rows where col0+col2 == 0 to verify
		// the (a1 != 0) ? 1 : 0 correction path. Row 0 has col0=-col2 so
		// a1 == 0 there.
		{name: "a1_zero_row0", in: [16]int16{
			3, 7, -3, 5,
			1, 2, 3, 4,
			-1, -2, -3, -4,
			0, 0, 0, 0,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var simd, scalar [16]int16
			forwardWalsh4x4SIMD(tc.in[:], 4, &simd)
			forwardWalsh4x4Scalar(tc.in[:], 4, &scalar)
			if simd != scalar {
				t.Fatalf("Walsh mismatch:\n  in   = %v\n  simd = %v\n  scl  = %v", tc.in, simd, scalar)
			}
		})
	}

	// Random fuzz across realistic residual ranges and various strides.
	r := rand.New(rand.NewSource(0xFEEDFACE))
	for _, stride := range []int{4, 8, 16} {
		for iter := range 1000 {
			buf := make([]int16, stride*4)
			for i := range buf {
				buf[i] = int16(r.Intn(512) - 256)
			}
			var simd, scalar [16]int16
			forwardWalsh4x4SIMD(buf, stride, &simd)
			forwardWalsh4x4Scalar(buf, stride, &scalar)
			if simd != scalar {
				t.Fatalf("stride=%d iter=%d:\n  in    = %v\n  simd  = %v\n  scalar= %v", stride, iter, buf, simd, scalar)
			}
		}
	}
}

func BenchmarkForwardWalsh4x4SIMD(b *testing.B) {
	input := [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}
	var output [16]int16
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		forwardWalsh4x4SIMD(input[:], 4, &output)
	}
}

func BenchmarkForwardWalsh4x4Scalar(b *testing.B) {
	input := [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}
	var output [16]int16
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		forwardWalsh4x4Scalar(input[:], 4, &output)
	}
}
