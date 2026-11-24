package encoder

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp9/common"
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
