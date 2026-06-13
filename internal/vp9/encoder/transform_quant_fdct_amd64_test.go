//go:build amd64 && !purego

package encoder

import (
	"math/rand"
	"testing"
	"unsafe"
)

func TestForwardDCT4x4SSE2MatchesScalar(t *testing.T) {
	cases := []struct {
		name string
		in   [16]int16
	}{
		{name: "zero"},
		{name: "dc_pos", in: [16]int16{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5}},
		{name: "dc_neg", in: [16]int16{-5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5}},
		{name: "ramp", in: [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}},
		{name: "alt_signs", in: [16]int16{255, -255, 255, -255, -255, 255, -255, 255, 255, -255, 255, -255, -255, 255, -255, 255}},
		{name: "max_pos", in: [16]int16{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}},
		{name: "max_neg", in: [16]int16{-255, -255, -255, -255, -255, -255, -255, -255, -255, -255, -255, -255, -255, -255, -255, -255}},
		{name: "single_top_left", in: [16]int16{255}},
		{name: "single_bottom_right", in: [16]int16{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var simd, scalar [16]int16
			forwardDCT4x4SSE2(unsafe.SliceData(tc.in[:]), 4, unsafe.SliceData(simd[:]))
			forwardDCT4x4Scalar(tc.in[:], 4, scalar[:])
			if simd != scalar {
				t.Fatalf("DCT mismatch:\n  in   = %v\n  simd = %v\n  scl  = %v", tc.in, simd, scalar)
			}
		})
	}
}

func TestForwardDCT4x4DispatchMatchesScalarRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	for _, stride := range []int{4, 8, 16} {
		for iter := range 1000 {
			input := make([]int16, stride*4)
			for i := range input {
				input[i] = int16(rng.Intn(511) - 255)
			}
			var simd, scalar [16]int16
			ForwardDCT4x4Into(input, stride, simd[:])
			forwardDCT4x4Scalar(input, stride, scalar[:])
			if simd != scalar {
				t.Fatalf("stride=%d iter=%d:\n  in    = %v\n  simd  = %v\n  scalar= %v", stride, iter, input, simd, scalar)
			}
		}
	}
}

func TestForwardDCT4x4DispatchFallsBackOutsideResidualDomain(t *testing.T) {
	input := [16]int16{256, -256, 1024, -1024}
	var got, want [16]int16
	ForwardDCT4x4Into(input[:], 4, got[:])
	forwardDCT4x4Scalar(input[:], 4, want[:])
	if got != want {
		t.Fatalf("fallback mismatch:\n  got  = %v\n  want = %v", got, want)
	}
	if forwardDCT4x4SSE2OK(input[:], 4, got[:]) {
		t.Fatal("forwardDCT4x4SSE2OK accepted input outside residual domain")
	}
}

func BenchmarkForwardDCT4x4Scalar(b *testing.B) {
	input := [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}
	var output [16]int16
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		forwardDCT4x4Scalar(input[:], 4, output[:])
	}
}

func BenchmarkForwardDCT4x4SSE2(b *testing.B) {
	input := [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}
	var output [16]int16
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		forwardDCT4x4SSE2(unsafe.SliceData(input[:]), 4, unsafe.SliceData(output[:]))
	}
}
