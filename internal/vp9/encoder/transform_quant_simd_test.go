//go:build arm64 && !purego

package encoder

import (
	"math/rand"
	"testing"
	"unsafe"
)

func TestQuantizeBACNEONMatchesScalar(t *testing.T) {
	params := []struct {
		zbin       int
		round      int
		quant      int
		quantShift int
		dequant    int
	}{
		{zbin: 24, round: 13, quant: -17873, quantShift: 2048, dequant: 44},
		{zbin: 88, round: 42, quant: -21846, quantShift: 1024, dequant: 96},
		{zbin: 7, round: 3, quant: -32768, quantShift: 16384, dequant: 4},
	}
	for _, count := range []int{8, 56, 248} {
		for pidx, p := range params {
			coeff := make([]int16, count)
			iscan := make([]int16, count)
			rng := rand.New(rand.NewSource(int64(count*131 + pidx*17)))
			for i := range coeff {
				switch i % 17 {
				case 0:
					coeff[i] = 0
				case 1:
					coeff[i] = int16(p.zbin - 1)
				case 2:
					coeff[i] = int16(p.zbin)
				case 3:
					coeff[i] = int16(-p.zbin)
				case 4:
					coeff[i] = 32767
				case 5:
					coeff[i] = -32768
				default:
					coeff[i] = int16(rng.Intn(4097) - 2048)
				}
				iscan[i] = int16(count - i)
			}

			gotQ := make([]int16, count)
			gotDQ := make([]int16, count)
			gotEOB := int(quantizeBACNEON(&coeff[0], &iscan[0],
				&gotQ[0], &gotDQ[0], count, p.zbin, p.round, p.quant,
				p.quantShift, p.dequant))

			wantQ := make([]int16, count)
			wantDQ := make([]int16, count)
			wantEOB := 0
			for i, c16 := range coeff {
				c := int(c16)
				absCoeff := c
				if absCoeff < 0 {
					absCoeff = -absCoeff
				}
				tmp := 0
				if absCoeff >= p.zbin {
					tmp = clampInt16(absCoeff + p.round)
					tmp = ((((tmp * p.quant) >> 16) + tmp) * p.quantShift) >> 16
					q := tmp
					if c < 0 {
						q = -q
					}
					wantQ[i] = int16(q)
					wantDQ[i] = int16(q * p.dequant)
				}
				if tmp != 0 && int(iscan[i]) > wantEOB {
					wantEOB = int(iscan[i])
				}
			}

			if gotEOB != wantEOB {
				t.Fatalf("count=%d params=%d eob=%d want %d",
					count, pidx, gotEOB, wantEOB)
			}
			for i := range coeff {
				if gotQ[i] != wantQ[i] {
					t.Fatalf("count=%d params=%d qcoeff[%d]=%d want %d",
						count, pidx, i, gotQ[i], wantQ[i])
				}
				if gotDQ[i] != wantDQ[i] {
					t.Fatalf("count=%d params=%d dqcoeff[%d]=%d want %d",
						count, pidx, i, gotDQ[i], wantDQ[i])
				}
			}
		}
	}
}

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
