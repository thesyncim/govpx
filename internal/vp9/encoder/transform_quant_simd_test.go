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

func TestForwardDCT16x16NEONMatchesScalarConstant(t *testing.T) {
	for _, v := range []int16{0, 1, -1, 7, -7, 200, -200} {
		var input [256]int16
		for i := range input {
			input[i] = v
		}
		var simd, scalar [256]int16
		forwardDCT16x16NEONOrScalar(input[:], 16, simd[:])
		forwardDCT16x16Scalar(input[:], 16, scalar[:])
		if simd != scalar {
			t.Fatalf("constant %d DCT16x16 mismatch\nsimd  %v\nscalar %v", v, simd, scalar)
		}
	}
}

func TestForwardDCT16x16NEONMatchesScalarRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for trial := range 100 {
		var input [256]int16
		for i := range input {
			input[i] = int16(rng.Intn(511) - 255)
		}
		var simd, scalar [256]int16
		forwardDCT16x16NEONOrScalar(input[:], 16, simd[:])
		forwardDCT16x16Scalar(input[:], 16, scalar[:])
		if simd != scalar {
			t.Fatalf("trial %d DCT16x16 mismatch\nin    %v\nsimd  %v\nscalar %v", trial, input, simd, scalar)
		}
	}
}

func TestForwardDCT16x16NEONMatchesScalarStrided(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	var input [16 * 32]int16
	for i := range input {
		input[i] = int16(rng.Intn(511) - 255)
	}
	var simd, scalar [256]int16
	forwardDCT16x16NEONOrScalar(input[:], 32, simd[:])
	forwardDCT16x16Scalar(input[:], 32, scalar[:])
	if simd != scalar {
		t.Fatalf("strided DCT16x16 mismatch\nsimd  %v\nscalar %v", simd, scalar)
	}
}

func TestForwardDCT16x16NEONStackStress(t *testing.T) {
	var input [256]int16
	for i := range input {
		input[i] = int16((i*17)%511 - 255)
	}
	var want [256]int16
	forwardDCT16x16Scalar(input[:], 16, want[:])

	for trial := 0; trial < 64; trial++ {
		var got [256]int16
		forwardDCT16x16NEONStackStress(t, 48, input[:], got[:])
		if got != want {
			t.Fatalf("trial %d DCT16x16 stack stress mismatch\nsimd  %v\nscalar %v", trial, got, want)
		}
	}
}

func TestForwardDCT4x4NEONMatchesScalarConstant(t *testing.T) {
	for _, v := range []int16{0, 1, -1, 7, -7, 200, -200} {
		var input [16]int16
		for i := range input {
			input[i] = v
		}
		var simd, scalar [16]int16
		forwardDCT4x4NEONOrScalar(input[:], 4, simd[:])
		forwardDCT4x4Scalar(input[:], 4, scalar[:])
		if simd != scalar {
			t.Fatalf("constant %d DCT4x4 mismatch\nsimd  %v\nscalar %v", v, simd, scalar)
		}
	}
}

func TestForwardDCT4x4NEONMatchesScalarRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := range 100 {
		var input [16]int16
		for i := range input {
			input[i] = int16(rng.Intn(511) - 255)
		}
		var simd, scalar [16]int16
		forwardDCT4x4NEONOrScalar(input[:], 4, simd[:])
		forwardDCT4x4Scalar(input[:], 4, scalar[:])
		if simd != scalar {
			t.Fatalf("trial %d DCT4x4 mismatch\nin    %v\nsimd  %v\nscalar %v", trial, input, simd, scalar)
		}
	}
}

func TestForwardDCT4x4NEONMatchesScalarStrided(t *testing.T) {
	rng := rand.New(rand.NewSource(8))
	var input [4 * 8]int16
	for i := range input {
		input[i] = int16(rng.Intn(511) - 255)
	}
	var simd, scalar [16]int16
	forwardDCT4x4NEONOrScalar(input[:], 8, simd[:])
	forwardDCT4x4Scalar(input[:], 8, scalar[:])
	if simd != scalar {
		t.Fatalf("strided DCT4x4 mismatch\nsimd  %v\nscalar %v", simd, scalar)
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

func BenchmarkForwardDCT4x4NEON(b *testing.B) {
	rng := rand.New(rand.NewSource(9))
	var input [16]int16
	for i := range input {
		input[i] = int16(rng.Intn(511) - 255)
	}
	var output [16]int16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		forwardDCT4x4NEONTest(input[:], 4, output[:])
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

func forwardDCT4x4NEONTest(input []int16, stride int, output []int16) {
	forwardDCT4x4NEON(unsafe.SliceData(input), unsafe.SliceData(output), stride)
}

func forwardDCT16x16NEONTest(input []int16, stride int, output []int16) {
	forwardDCT16x16NEON(unsafe.SliceData(input), unsafe.SliceData(output), stride)
}

func forwardDCT16x16NEONStackStress(t *testing.T, depth int, input []int16, output []int16) {
	var pad [256]byte
	for i := range pad {
		pad[i] = byte(i)
	}
	if depth == 0 {
		forwardDCT16x16NEONOrScalar(input, 16, output)
		if pad[0] != 0 {
			t.Fatalf("unexpected pad mutation")
		}
		return
	}
	forwardDCT16x16NEONStackStress(t, depth-1, input, output)
	if pad[255] != 255 {
		t.Fatalf("unexpected pad mutation")
	}
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

// Test-only thin wrapper that always hits the SIMD path.
func forwardDCT16x16NEONOrScalar(input []int16, stride int, output []int16) {
	if stride < 16 || len(input) < 15*stride+16 || len(output) < 256 {
		forwardDCT16x16Scalar(input, stride, output)
		return
	}
	forwardDCT16x16NEONTest(input, stride, output)
}

// Test-only thin wrapper that always hits the SIMD path.
func forwardDCT4x4NEONOrScalar(input []int16, stride int, output []int16) {
	if stride < 4 || len(input) < 3*stride+4 || len(output) < 16 {
		forwardDCT4x4Scalar(input, stride, output)
		return
	}
	forwardDCT4x4NEONTest(input, stride, output)
}
