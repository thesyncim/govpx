package encoder

import (
	"fmt"
	"math/rand"
	"testing"
)

// hadamard8x8Ref is the scalar reference (libvpx vpx_hadamard_8x8_c) used to
// pin the SIMD dispatch. It mirrors hadamard8x8Scalar but is kept separate so
// the parity tests stay meaningful if the scalar helper is ever refactored.
func hadamard8x8Ref(src []int16, stride int, coeff []int16) {
	var buffer [64]int16
	var buffer2 [64]int16
	for idx := range 8 {
		hadamardCol8(src[idx:], stride, buffer[idx*8:])
	}
	for idx := range 8 {
		hadamardCol8(buffer[idx:], 8, buffer2[idx*8:])
	}
	copy(coeff[:64], buffer2[:])
}

func hadamard16x16Ref(src []int16, stride int, coeff []int16) {
	hadamard8x8Ref(src, stride, coeff[:64])
	hadamard8x8Ref(src[8:], stride, coeff[64:128])
	hadamard8x8Ref(src[8*stride:], stride, coeff[128:192])
	hadamard8x8Ref(src[8*stride+8:], stride, coeff[192:256])
	for idx := range 64 {
		a0 := int(coeff[idx])
		a1 := int(coeff[64+idx])
		a2 := int(coeff[128+idx])
		a3 := int(coeff[192+idx])

		b0 := (a0 + a1) >> 1
		b1 := (a0 - a1) >> 1
		b2 := (a2 + a3) >> 1
		b3 := (a2 - a3) >> 1

		coeff[idx] = int16(b0 + b2)
		coeff[64+idx] = int16(b1 + b3)
		coeff[128+idx] = int16(b0 - b2)
		coeff[192+idx] = int16(b1 - b3)
	}
}

func satdRef(coeff []int16, n int) int {
	sum := 0
	for i := range n {
		v := int(coeff[i])
		if v < 0 {
			v = -v
		}
		sum += v
	}
	return sum
}

func hadamardTestInputs(rng *rand.Rand, size, stride int, extreme bool) []int16 {
	src := make([]int16, (size-1)*stride+size+8)
	for i := range src {
		if extreme {
			switch rng.Intn(4) {
			case 0:
				src[i] = -32768
			case 1:
				src[i] = 32767
			default:
				src[i] = int16(rng.Intn(65536) - 32768)
			}
		} else {
			// Residual range: src-pred of 8-bit pixels.
			src[i] = int16(rng.Intn(511) - 255)
		}
	}
	return src
}

func TestHadamard8x8DispatchMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(0x8ad8))
	for _, extreme := range []bool{false, true} {
		for _, stride := range []int{8, 16, 32, 64, 73} {
			for trial := range 64 {
				src := hadamardTestInputs(rng, 8, stride, extreme)
				want := make([]int16, 64)
				got := make([]int16, 64)
				hadamard8x8Ref(src, stride, want)
				hadamard8x8Into(src, stride, got)
				for i := range want {
					if want[i] != got[i] {
						t.Fatalf("extreme=%v stride=%d trial=%d: coeff[%d] = %d, want %d",
							extreme, stride, trial, i, got[i], want[i])
					}
				}
			}
		}
	}
}

func TestHadamard16x16DispatchMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(0x16ad))
	for _, extreme := range []bool{false, true} {
		for _, stride := range []int{16, 32, 64, 91} {
			for trial := range 32 {
				src := hadamardTestInputs(rng, 16, stride, extreme)
				want := make([]int16, 256)
				got := make([]int16, 256)
				hadamard16x16Ref(src, stride, want)
				hadamard16x16Into(src, stride, got)
				for i := range want {
					if want[i] != got[i] {
						t.Fatalf("extreme=%v stride=%d trial=%d: coeff[%d] = %d, want %d",
							extreme, stride, trial, i, got[i], want[i])
					}
				}
			}
		}
	}
}

func TestSatdDispatchMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5a7d))
	for _, n := range []int{16, 64, 256, 1024} {
		for trial := range 64 {
			coeff := make([]int16, n)
			for i := range coeff {
				switch rng.Intn(8) {
				case 0:
					coeff[i] = -32768
				case 1:
					coeff[i] = 32767
				default:
					coeff[i] = int16(rng.Intn(65536) - 32768)
				}
			}
			want := satdRef(coeff, n)
			got := satdAbsSum(coeff, n)
			if want != got {
				t.Fatalf("n=%d trial=%d: satd = %d, want %d", n, trial, got, want)
			}
		}
	}
}

func BenchmarkHadamard8x8(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := hadamardTestInputs(rng, 8, 64, false)
	coeff := make([]int16, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hadamard8x8Into(src, 64, coeff)
	}
}

func BenchmarkHadamard16x16(b *testing.B) {
	rng := rand.New(rand.NewSource(2))
	src := hadamardTestInputs(rng, 16, 64, false)
	coeff := make([]int16, 256)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hadamard16x16Into(src, 64, coeff)
	}
}

func BenchmarkSatd(b *testing.B) {
	rng := rand.New(rand.NewSource(3))
	for _, n := range []int{16, 64, 256} {
		coeff := make([]int16, n)
		for i := range coeff {
			coeff[i] = int16(rng.Intn(2048) - 1024)
		}
		b.Run(fmt.Sprintf("n%d", n), func(b *testing.B) {
			b.ReportAllocs()
			acc := 0
			for i := 0; i < b.N; i++ {
				acc += satdAbsSum(coeff, n)
			}
			if acc == 0 {
				b.Fatal("unexpected zero satd accumulator")
			}
		})
	}
}
