package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"math/rand"
	"testing"
)

// The canonical scalar butterflies are the pinned vpx_dsp/avg.c layout.
// The scoring kernel may transpose within each 64-coefficient quadrant;
// its DC position and every coefficient value must otherwise be identical.
func TestHadamardBlockYrdPermutation(t *testing.T) {
	rng := rand.New(rand.NewSource(0x55ad))
	for _, n := range []int{8, 16} {
		for _, stride := range []int{n, n + 7, 64, 91} {
			for trial := range 100 {
				src := make([]int16, (n-1)*stride+n)
				for i := range src {
					switch trial % 5 {
					case 0:
						src[i] = 255
					case 1:
						src[i] = -255
					case 2:
						src[i] = int16((i&1)*510 - 255)
					default:
						src[i] = int16(rng.Intn(511) - 255)
					}
				}
				var want, got [256]int16
				if n == 8 {
					hadamard8x8Ref(src, stride, want[:])
				} else {
					hadamard16x16Ref(src, stride, want[:])
				}
				hadamardBlockYrd(src, stride, got[:], n)
				for i := range n * n {
					j := i
					if pixel.Enabled {
						j = (i &^ 63) + ((i & 7) << 3) + ((i & 63) >> 3)
					}
					if got[i] != want[j] {
						t.Fatalf("n=%d stride=%d trial=%d coeff[%d]=%d want[%d]=%d", n, stride, trial, i, got[i], j, want[j])
					}
				}
			}
		}
	}
}

// referenceBlockYrdScore derives the consumer result from scalar Hadamard,
// scalar quantization, and scalar sum/error loops. It does not use the new
// transform, the SIMD quantizer, or the production two-pass scratch layout.
func referenceBlockYrdScore(src, dst []byte, stride, off, bw, bh, n int, dq [2]int16, sse uint64) BlockYrdResult {
	var result BlockYrdResult
	result.Valid = true
	result.Skippable = true
	var rate int
	var dist int64
	units := 0
	r := [2]int16{int16(48 * int(dq[0]) >> 7), int16(42 * int(dq[1]) >> 7)}
	q := [2]int16{int16(65536 / int(dq[0])), int16(65536 / int(dq[1]))}
	tx := common.Tx8x8
	if n == 16 {
		tx = common.Tx16x16
	}
	scan := common.DefaultScanOrders[tx]
	for y := 0; y < bh; y += n {
		for x := 0; x < bw; x += n {
			var residual, coeff, qc, dc [256]int16
			for yy := range n {
				for xx := range n {
					i := off + (y+yy)*stride + x + xx
					residual[yy*n+xx] = int16(src[i]) - int16(dst[i])
				}
			}
			if n == 8 {
				hadamard8x8Ref(residual[:], n, coeff[:])
			} else {
				hadamard16x16Ref(residual[:], n, coeff[:])
			}
			eob := quantizeFPLibvpxScalar(coeff[:], n*n, r, q, dq, scan.Scan, scan.IScan, qc[:], dc[:])
			if eob != 0 {
				result.Skippable = false
			}
			var errorSum int64
			for i := range n * n {
				v := int(qc[i])
				if v < 0 {
					v = -v
				}
				rate += v
				diff := int64(coeff[i]) - int64(dc[i])
				errorSum += diff * diff
			}
			dist += errorSum >> 2
			units++
		}
	}
	if sse < BlockYrdUnknownSSE {
		result.SSE = int64(sse << 4)
		if result.Skippable {
			result.Dist = result.SSE
			return result
		}
	}
	result.Rate = rate<<(2+VP9ProbCostShift) + units<<VP9ProbCostShift
	result.Dist = dist
	return result
}

func TestHadamardBlockYrdConsumer(t *testing.T) {
	rng := rand.New(rand.NewSource(0x55bd))
	for _, n := range []int{8, 16} {
		for _, bw := range []int{16, 32, 64} {
			for pattern := range 5 {
				stride, bh, off := bw+11, bw, 5
				src, dst := make([]byte, (bh+1)*stride), make([]byte, (bh+1)*stride)
				for i := range src {
					src[i] = byte(rng.Intn(256))
					dst[i] = byte(rng.Intn(256))
					switch pattern {
					case 0:
						dst[i] = src[i]
					case 1:
						src[i] = 255
						dst[i] = 0
					case 2:
						src[i] = byte(i&1) * 255
						dst[i] = 255 - src[i]
					case 3:
						src[i] = 129
						dst[i] = 128
					}
				}
				for _, dq := range [][2]int16{{1, 2}, {4, 8}, {16, 17}, {100, 127}, {1000, 32767}} {
					for _, sse := range []uint64{123456, BlockYrdUnknownSSE} {
						var scratch [16384 + 8]int16
						for i := range scratch {
							scratch[i] = -12345
						}
						tx := common.Tx8x8
						if n == 16 {
							tx = common.Tx16x16
						}
						got := BlockYrd(src, stride, off, 0, dst, stride, off, 0, bw, bh, tx, dq, sse, scratch[:len(scratch)-8])
						want := referenceBlockYrdScore(src, dst, stride, off, bw, bh, n, dq, sse)
						if got != want {
							t.Fatalf("n=%d bw=%d pattern=%d dq=%v sse=%d got=%+v want=%+v", n, bw, pattern, dq, sse, got, want)
						}
						for _, v := range scratch[len(scratch)-8:] {
							if v != -12345 {
								t.Fatal("scratch overstore")
							}
						}
					}
				}
			}
		}
	}
}

func BenchmarkHadamardBlockYrdNative(b *testing.B) {
	for _, n := range []int{8, 16} {
		b.Run(fmt.Sprintf("tx%d", n), func(b *testing.B) {
			var src [4096]int16
			var coeff [256]int16
			for i := range src {
				src[i] = int16(i*13%511 - 255)
			}
			b.ReportAllocs()
			for b.Loop() {
				hadamardBlockYrd(src[:], 64, coeff[:], n)
			}
			if coeff[0] == 32767 {
				b.Fatal("unexpected DC")
			}
		})
	}
}

func BenchmarkHadamardScoreTransformNative(b *testing.B) {
	for _, n := range []int{8, 16} {
		b.Run(fmt.Sprintf("tx%d", n), func(b *testing.B) {
			var src [4096]int16
			var coeff [256]int16
			for i := range src {
				src[i] = int16(i*13%511 - 255)
			}
			b.ReportAllocs()
			for b.Loop() {
				hadamardBlockYrd(src[:], 64, coeff[:], n)
			}
			if coeff[0] == 32767 {
				b.Fatal("unexpected DC")
			}
		})
	}
}

func BenchmarkHadamardScoreConsumerNative(b *testing.B) {
	for _, n := range []int{8, 16} {
		for _, bw := range []int{32, 64} {
			for pattern := range 3 {
				b.Run(fmt.Sprintf("tx%d/block%d/pattern%d", n, bw, pattern), func(b *testing.B) {
					src, dst := make([]byte, bw*bw), make([]byte, bw*bw)
					for i := range src {
						src[i] = byte(i * 13)
						dst[i] = byte(i * 7)
						if pattern == 0 {
							dst[i] = src[i]
						}
						if pattern == 1 {
							src[i] = 129
							dst[i] = 128
						}
					}
					var scratch [16384]int16
					tx := common.Tx8x8
					if n == 16 {
						tx = common.Tx16x16
					}
					b.ReportAllocs()
					for b.Loop() {
						benchmarkBlockYrdResult = BlockYrd(src, bw, 0, 0, dst, bw, 0, 0, bw, bw, tx, [2]int16{100, 127}, 123456, scratch[:])
					}
					if !benchmarkBlockYrdResult.Valid {
						b.Fatal("invalid score")
					}
				})
			}
		}
	}
}

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
