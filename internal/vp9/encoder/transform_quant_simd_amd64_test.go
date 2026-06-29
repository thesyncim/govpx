//go:build amd64 && !purego

package encoder

import (
	"math/rand"
	"strconv"
	"testing"
	"unsafe"
)

func TestForwardWHT4x4SSE2MatchesScalarConstant(t *testing.T) {
	for _, v := range []int16{0, 1, -1, 7, -7, 200, -200} {
		var input [16]int16
		for i := range input {
			input[i] = v
		}
		var simd, scalar [16]int16
		forwardWHT4x4SSE2OrScalar(input[:], 4, simd[:])
		forwardWHT4x4Scalar(input[:], 4, scalar[:])
		if simd != scalar {
			t.Fatalf("constant %d WHT mismatch\nsimd  %v\nscalar %v", v, simd, scalar)
		}
	}
}

func TestForwardWHT4x4SSE2MatchesScalarRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	for trial := range 100 {
		var input [16]int16
		for i := range input {
			input[i] = int16(rng.Intn(2049) - 1024)
		}
		var simd, scalar [16]int16
		forwardWHT4x4SSE2OrScalar(input[:], 4, simd[:])
		forwardWHT4x4Scalar(input[:], 4, scalar[:])
		if simd != scalar {
			t.Fatalf("trial %d WHT mismatch\nin    %v\nsimd  %v\nscalar %v", trial, input, simd, scalar)
		}
	}
}

func TestForwardWHT4x4SSE2MatchesScalarStrided(t *testing.T) {
	rng := rand.New(rand.NewSource(14))
	var input [4 * 8]int16
	for i := range input {
		input[i] = int16(rng.Intn(2049) - 1024)
	}
	var simd, scalar [16]int16
	forwardWHT4x4SSE2OrScalar(input[:], 8, simd[:])
	forwardWHT4x4Scalar(input[:], 8, scalar[:])
	if simd != scalar {
		t.Fatalf("strided WHT mismatch\nsimd  %v\nscalar %v", simd, scalar)
	}
}

func TestForwardDCT4x4SSE2MatchesScalarConstant(t *testing.T) {
	for _, v := range []int16{0, 1, -1, 7, -7, 200, -200, 255, -255} {
		var input [16]int16
		for i := range input {
			input[i] = v
		}
		var simd, scalar [16]int16
		forwardDCT4x4SSE2OrScalar(input[:], 4, simd[:])
		forwardDCT4x4Scalar(input[:], 4, scalar[:])
		if simd != scalar {
			t.Fatalf("constant %d DCT4x4 mismatch\nsimd  %v\nscalar %v", v, simd, scalar)
		}
	}
}

func TestForwardDCT4x4SSE2MatchesScalarRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(15))
	for trial := range 100 {
		var input [16]int16
		for i := range input {
			input[i] = int16(rng.Intn(511) - 255)
		}
		var simd, scalar [16]int16
		forwardDCT4x4SSE2OrScalar(input[:], 4, simd[:])
		forwardDCT4x4Scalar(input[:], 4, scalar[:])
		if simd != scalar {
			t.Fatalf("trial %d DCT4x4 mismatch\nin    %v\nsimd  %v\nscalar %v", trial, input, simd, scalar)
		}
	}
}

func TestForwardDCT4x4SSE2MatchesScalarStrided(t *testing.T) {
	rng := rand.New(rand.NewSource(16))
	var input [4 * 8]int16
	for i := range input {
		input[i] = int16(rng.Intn(511) - 255)
	}
	var simd, scalar [16]int16
	forwardDCT4x4SSE2OrScalar(input[:], 8, simd[:])
	forwardDCT4x4Scalar(input[:], 8, scalar[:])
	if simd != scalar {
		t.Fatalf("strided DCT4x4 mismatch\nsimd  %v\nscalar %v", simd, scalar)
	}
}

func TestQuantizeFPACSSE2MatchesScalar(t *testing.T) {
	roundAC, quantAC, deqAC := 5, 3855, 17
	for _, count := range []int{8, 16, 64} {
		t.Run("n"+strconv.Itoa(count), func(t *testing.T) {
			coeff := make([]int16, count)
			iscan := make([]int16, count)
			rng := rand.New(rand.NewSource(int64(count) * 97))
			for i := range coeff {
				switch i % 11 {
				case 0:
					coeff[i] = -32768
				case 1:
					coeff[i] = 32767
				case 2:
					coeff[i] = 0
				default:
					coeff[i] = int16(rng.Intn(2049) - 1024)
				}
				iscan[i] = int16(count - i)
			}

			gotQ := make([]int16, count)
			gotDQ := make([]int16, count)
			gotEOB := int(quantizeFPACSSE2(&coeff[0], &iscan[0], &gotQ[0],
				&gotDQ[0], count, roundAC, quantAC, deqAC))

			wantQ := make([]int16, count)
			wantDQ := make([]int16, count)
			wantEOB := 0
			for i, c16 := range coeff {
				c := int(c16)
				absCoeff := c
				if absCoeff < 0 {
					absCoeff = -absCoeff
				}
				sum := absCoeff + roundAC
				if sum < deqAC {
					continue
				}
				tmp := clampInt16(sum)
				tmp = (tmp * quantAC) >> 16
				q := tmp
				if c < 0 {
					q = -q
				}
				wantQ[i] = int16(q)
				wantDQ[i] = int16(q * deqAC)
				if tmp != 0 && int(iscan[i]) > wantEOB {
					wantEOB = int(iscan[i])
				}
			}

			if gotEOB != wantEOB {
				t.Fatalf("eob = %d, want %d", gotEOB, wantEOB)
			}
			for i := range coeff {
				if gotQ[i] != wantQ[i] {
					t.Fatalf("qcoeff[%d] = %d, want %d", i, gotQ[i], wantQ[i])
				}
				if gotDQ[i] != wantDQ[i] {
					t.Fatalf("dqcoeff[%d] = %d, want %d", i, gotDQ[i], wantDQ[i])
				}
			}
		})
	}
}

func BenchmarkForwardDCT4x4Scalar(b *testing.B) {
	rng := rand.New(rand.NewSource(9))
	var input [16]int16
	for i := range input {
		input[i] = int16(rng.Intn(511) - 255)
	}
	var output [16]int16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		forwardDCT4x4Scalar(input[:], 4, output[:])
	}
}

func BenchmarkForwardDCT4x4SSE2(b *testing.B) {
	rng := rand.New(rand.NewSource(9))
	var input [16]int16
	for i := range input {
		input[i] = int16(rng.Intn(511) - 255)
	}
	var output [16]int16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		forwardDCT4x4SSE2Test(input[:], 4, output[:])
	}
}

func BenchmarkForwardDCT4x4Dispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(9))
	var input [16]int16
	for i := range input {
		input[i] = int16(rng.Intn(511) - 255)
	}
	var output [16]int16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ForwardDCT4x4Into(input[:], 4, output[:])
	}
}

func BenchmarkForwardWHT4x4Scalar(b *testing.B) {
	rng := rand.New(rand.NewSource(3))
	var input [16]int16
	for i := range input {
		input[i] = int16(rng.Intn(2049) - 1024)
	}
	var output [16]int16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		forwardWHT4x4Scalar(input[:], 4, output[:])
	}
}

func BenchmarkForwardWHT4x4SSE2(b *testing.B) {
	rng := rand.New(rand.NewSource(3))
	var input [16]int16
	for i := range input {
		input[i] = int16(rng.Intn(2049) - 1024)
	}
	var output [16]int16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		forwardWHT4x4SSE2Test(input[:], 4, output[:])
	}
}

func BenchmarkForwardWHT4x4Dispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(3))
	var input [16]int16
	for i := range input {
		input[i] = int16(rng.Intn(2049) - 1024)
	}
	var output [16]int16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ForwardWHT4x4Into(input[:], 4, output[:])
	}
}

func forwardDCT4x4SSE2Test(input []int16, stride int, output []int16) {
	forwardDCT4x4SSE2(unsafe.SliceData(input), stride, unsafe.SliceData(output))
}

func forwardDCT4x4SSE2OrScalar(input []int16, stride int, output []int16) {
	if stride < 4 || len(input) < 3*stride+4 || len(output) < 16 {
		forwardDCT4x4Scalar(input, stride, output)
		return
	}
	forwardDCT4x4SSE2Test(input, stride, output)
}

func forwardWHT4x4SSE2Test(input []int16, stride int, output []int16) {
	forwardWHT4x4SSE2(unsafe.SliceData(input), stride, unsafe.SliceData(output))
}

func forwardWHT4x4SSE2OrScalar(input []int16, stride int, output []int16) {
	if len(input) < 3*stride+4 || len(output) < 16 || stride < 4 {
		forwardWHT4x4Scalar(input, stride, output)
		return
	}
	forwardWHT4x4SSE2Test(input, stride, output)
}
