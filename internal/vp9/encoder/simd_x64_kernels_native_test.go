//go:build goexperiment.simd && (amd64 || arm64) && !purego

package encoder

import (
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func round2ReferenceYrd(src, dst []byte, stride, off, bw, bh, n int, dq [2]int16, sse uint64) BlockYrdResult {
	var result BlockYrdResult
	result.Valid = true
	result.Skippable = true
	var rate int
	var dist int64
	units := 0
	r := [2]int16{int16(48 * int(dq[0]) >> 7), int16(42 * int(dq[1]) >> 7)}
	q := [2]int16{int16(65536 / int(dq[0])), int16(65536 / int(dq[1]))}
	tx := common.Tx8x8
	if n == 4 {
		tx = common.Tx4x4
	}
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
			if n == 4 {
				ForwardDCT4x4Into(residual[:], n, coeff[:])
			} else if n == 8 {
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

func round2Pixels(pattern string) ([4096]byte, [4096]byte) {
	var a, b [4096]byte
	for i := range a {
		a[i] = byte(i*17 + i/11)
		b[i] = byte(i*29 + i/7)
		if pattern == "zero" {
			b[i] = a[i]
		}
		if pattern == "constant" {
			a[i], b[i] = 129, 127
		}
	}
	return a, b
}
func TestX64Round2Yrd(t *testing.T) {
	for _, n := range []int{4, 8, 16} {
		tx := common.Tx4x4
		if n == 8 {
			tx = common.Tx8x8
		}
		if n == 16 {
			tx = common.Tx16x16
		}
		for _, size := range []int{16, 32, 64} {
			for _, pattern := range []string{"zero", "constant", "dense"} {
				a, b := round2Pixels(pattern)
				for _, d := range [][2]int16{{1, 2}, {3, 5}, {16, 27}, {97, 123}, {2048, 2048}, {2051, 2077}} {
					for _, sse := range []uint64{37, BlockYrdUnknownSSE} {
						var scratch [16384]int16
						got := BlockYrd(a[:], 64, 0, 0, b[:], 64, 0, 0, size, size, tx, d, sse, scratch[:])
						want := round2ReferenceYrd(a[:], b[:], 64, 0, size, size, n, d, sse)
						if got != want {
							t.Fatalf("n%d size%d %s d%v sse%d got%+v want%+v", n, size, pattern, d, sse, got, want)
						}
					}
				}
			}
		}
	}
}

var round2YrdSink BlockYrdResult

func BenchmarkX64Round2Yrd(b *testing.B) {
	for _, n := range []int{4, 8, 16} {
		for _, size := range []int{16, 64} {
			for _, pattern := range []string{"zero", "constant", "dense"} {
				b.Run(fmt.Sprintf("tx%d/size%d/%s", n, size, pattern), func(b *testing.B) {
					a, p := round2Pixels(pattern)
					var scratch [16384]int16
					tx := common.Tx4x4
					if n == 8 {
						tx = common.Tx8x8
					}
					if n == 16 {
						tx = common.Tx16x16
					}
					b.ReportAllocs()
					for b.Loop() {
						round2YrdSink = BlockYrd(a[:], 64, 0, 0, p[:], 64, 0, 0, size, size, tx, [2]int16{97, 123}, BlockYrdUnknownSSE, scratch[:])
					}
				})
			}
		}
	}
}
func TestX64Round2Metrics(t *testing.T) {
	for _, xy := range [][2]int{{maxInt(), 2}, {2, maxInt()}, {maxInt() - 1, 2}, {2, maxInt() - 1}} {
		got, gok, gpanic := round2ClampedOutcome(func() (BlockDiffStats, bool) {
			return BlockDiffStatsClampedSource([]byte{1, 2, 3, 4}, 2, 2, 2, []byte{4, 3, 2, 1}, 2, xy[0], xy[1], 0, 0, 2, 2)
		})
		want, wok, wpanic := round2ClampedOutcome(func() (BlockDiffStats, bool) {
			return round2FrozenClampedStats([]byte{1, 2, 3, 4}, 2, 2, 2, []byte{4, 3, 2, 1}, 2, xy[0], xy[1], 0, 0, 2, 2)
		})
		if gpanic != wpanic || (!gpanic && (gok != wok || got != want)) {
			t.Fatalf("overflow coordinate %v: got%+v/%v/panic%v want%+v/%v/panic%v", xy, got, gok, gpanic, want, wok, wpanic)
		}
	}
	a, b := round2Pixels("dense")
	for _, size := range []int{4, 8, 16, 32, 64} {
		want := blockDiffStatsScalar(a[:], 64, b[:], 64, 0, 0, 0, 0, size, size)
		if got := BlockSSE(a[:], 64, b[:], 64, 0, 0, 0, 0, size, size); got != want.SSE {
			t.Fatalf("SSE%d %d %d", size, got, want.SSE)
		}
		for _, xy := range [][2]int{{0, 0}, {40, 0}, {0, 40}, {40, 40}, {100, 100}} {
			got, ok := BlockDiffStatsClampedSource(a[:], 64, 57, 53, b[:], 64, xy[0], xy[1], 0, 0, size, size)
			want := BlockDiffStats{Count: uint64(size * size)}
			for y := range size {
				sy := xy[1] + y
				if sy >= 53 {
					sy = 52
				}
				for x := range size {
					sx := xy[0] + x
					if sx >= 57 {
						sx = 56
					}
					d := int64(a[sy*64+sx]) - int64(b[y*64+x])
					want.Sum += d
					want.SSE += uint64(d * d)
				}
			}
			if !ok || got != want {
				t.Fatalf("edge size%d xy%v got%+v want%+v", size, xy, got, want)
			}
		}
	}
}

var round2MetricSink uint64
var round2StatsSink BlockDiffStats

func BenchmarkX64Round2SSE(b *testing.B) {
	a, p := round2Pixels("dense")
	for _, size := range []int{8, 16, 32, 64} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				round2MetricSink = BlockSSE(a[:], 64, p[:], 64, 0, 0, 0, 0, size, size)
			}
		})
	}
}
func BenchmarkX64Round2EdgeStats(b *testing.B) {
	a, p := round2Pixels("dense")
	for _, size := range []int{16, 64} {
		for _, xy := range [][2]int{{0, 0}, {40, 0}, {0, 40}, {40, 40}} {
			b.Run(fmt.Sprintf("size%d/x%d/y%d", size, xy[0], xy[1]), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					round2StatsSink, _ = BlockDiffStatsClampedSource(a[:], 64, 57, 53, p[:], 64, xy[0], xy[1], 0, 0, size, size)
				}
			})
		}
	}
}

// Frozen b57 clamped-edge behavior, including integer-wrap panic semantics.
func round2FrozenClampedStats(src []byte, srcStride, srcW, srcH int,
	ref []byte, refStride int, srcX, srcY, refX, refY, w, h int,
) (BlockDiffStats, bool) {
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 ||
		srcW <= 0 || srcH <= 0 || w <= 0 || h <= 0 ||
		srcX < 0 || srcY < 0 || refX < 0 || refY < 0 ||
		srcW > srcStride || refX+w > refStride {
		return BlockDiffStats{}, false
	}
	if (srcH-1)*srcStride+srcW > len(src) ||
		(refY+h-1)*refStride+refX+w > len(ref) {
		return BlockDiffStats{}, false
	}
	if srcX+w <= srcW && srcY+h <= srcH {
		return blockDiffStats(src, srcStride, ref, refStride,
			srcX, srcY, refX, refY, w, h), true
	}

	var stats BlockDiffStats
	stats.Count = uint64(w * h)
	for y := range h {
		sy := srcY + y
		if sy >= srcH {
			sy = srcH - 1
		}
		srcRow := src[sy*srcStride:]
		refRow := ref[(refY+y)*refStride+refX:]
		for x := range w {
			sx := srcX + x
			if sx >= srcW {
				sx = srcW - 1
			}
			diff := int64(int(srcRow[sx]) - int(refRow[x]))
			stats.Sum += diff
			stats.SSE += uint64(diff * diff)
		}
	}
	return stats, true
}
func round2ClampedOutcome(fn func() (BlockDiffStats, bool)) (stats BlockDiffStats, ok, panicked bool) {
	defer func() { panicked = recover() != nil }()
	stats, ok = fn()
	return
}
