//go:build arm64 && !purego

package encoder

import (
	"math/rand"
	"testing"
	"unsafe"
)

func TestForwardWHT4x4NEONMatchesScalarConstant(t *testing.T) {
	for _, v := range []int16{0, 1, -1, 7, -7, 200, -200} {
		var input [16]int16
		for i := range input {
			input[i] = v
		}
		var simd, scalar [16]int16
		forwardWHT4x4NEONOrScalar(input[:], 4, simd[:])
		forwardWHT4x4Scalar(input[:], 4, scalar[:])
		if simd != scalar {
			t.Fatalf("constant %d WHT mismatch\nsimd  %v\nscalar %v", v, simd, scalar)
		}
	}
}

func TestForwardWHT4x4NEONMatchesScalarRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for trial := range 20 {
		var input [16]int16
		for i := range input {
			input[i] = int16(rng.Intn(2049) - 1024)
		}
		var simd, scalar [16]int16
		forwardWHT4x4NEONOrScalar(input[:], 4, simd[:])
		forwardWHT4x4Scalar(input[:], 4, scalar[:])
		if simd != scalar {
			t.Fatalf("trial %d WHT mismatch\nin    %v\nsimd  %v\nscalar %v", trial, input, simd, scalar)
		}
	}
}

func TestForwardDCT8x8NEONMatchesScalarConstant(t *testing.T) {
	for _, v := range []int16{0, 1, -1, 7, -7, 200, -200} {
		var input [64]int16
		for i := range input {
			input[i] = v
		}
		var simd, scalar [64]int16
		forwardDCT8x8NEONOrScalar(input[:], 8, simd[:])
		forwardDCT8x8Scalar(input[:], 8, scalar[:])
		if simd != scalar {
			t.Fatalf("constant %d DCT8x8 mismatch\nsimd  %v\nscalar %v", v, simd, scalar)
		}
	}
}

func TestForwardDCT8x8NEONMatchesScalarRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for trial := range 100 {
		var input [64]int16
		for i := range input {
			input[i] = int16(rng.Intn(511) - 255)
		}
		var simd, scalar [64]int16
		forwardDCT8x8NEONOrScalar(input[:], 8, simd[:])
		forwardDCT8x8Scalar(input[:], 8, scalar[:])
		if simd != scalar {
			t.Fatalf("trial %d DCT8x8 mismatch\nin    %v\nsimd  %v\nscalar %v", trial, input, simd, scalar)
		}
	}
}

func TestForwardDCT8x8NEONMatchesScalarStrided(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	var input [8 * 16]int16
	for i := range input {
		input[i] = int16(rng.Intn(511) - 255)
	}
	var simd, scalar [64]int16
	forwardDCT8x8NEONOrScalar(input[:], 16, simd[:])
	forwardDCT8x8Scalar(input[:], 16, scalar[:])
	if simd != scalar {
		t.Fatalf("strided DCT8x8 mismatch\nsimd  %v\nscalar %v", simd, scalar)
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

func BenchmarkForwardWHT4x4NEON(b *testing.B) {
	rng := rand.New(rand.NewSource(3))
	var input [16]int16
	for i := range input {
		input[i] = int16(rng.Intn(2049) - 1024)
	}
	var output [16]int16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		forwardWHT4x4NEONTest(input[:], 4, output[:])
	}
}

func BenchmarkForwardDCT8x8Scalar(b *testing.B) {
	rng := rand.New(rand.NewSource(6))
	var input [64]int16
	for i := range input {
		input[i] = int16(rng.Intn(511) - 255)
	}
	var output [64]int16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		forwardDCT8x8Scalar(input[:], 8, output[:])
	}
}

func BenchmarkForwardDCT8x8NEON(b *testing.B) {
	rng := rand.New(rand.NewSource(6))
	var input [64]int16
	for i := range input {
		input[i] = int16(rng.Intn(511) - 255)
	}
	var output [64]int16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		forwardDCT8x8NEONTest(input[:], 8, output[:])
	}
}

func forwardWHT4x4NEONTest(input []int16, stride int, output []int16) {
	forwardWHT4x4NEON(unsafe.SliceData(input), stride, unsafe.SliceData(output))
}

func forwardDCT8x8NEONTest(input []int16, stride int, output []int16) {
	forwardDCT8x8NEON(unsafe.SliceData(input), unsafe.SliceData(output), stride)
}

// Test-only thin wrapper that always hits the SIMD path.
func forwardWHT4x4NEONOrScalar(input []int16, stride int, output []int16) {
	if len(input) < 3*stride+4 || len(output) < 16 || stride < 4 {
		forwardWHT4x4Scalar(input, stride, output)
		return
	}
	forwardWHT4x4NEONTest(input, stride, output)
}

// Test-only thin wrapper that always hits the SIMD path.
func forwardDCT8x8NEONOrScalar(input []int16, stride int, output []int16) {
	if stride < 8 || len(input) < 7*stride+8 || len(output) < 64 {
		forwardDCT8x8Scalar(input, stride, output)
		return
	}
	forwardDCT8x8NEONTest(input, stride, output)
}
