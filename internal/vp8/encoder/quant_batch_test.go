package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"math/rand"
	"os"
	"reflect"
	"testing"
)

func TestQuantBatchExtremesScalar(t *testing.T) {
	seed := uint32(0x1931)
	next := func() int16 { seed = seed*1664525 + 1013904223; return int16(seed >> 16) }
	for _, count := range []int{0, 1, 2, 7, 8, 16, 25} {
		for trial := range 80 {
			var q BlockQuant
			for i := range 16 {
				q.Round[i] = next()
				q.QuantFast[i] = next()
				q.Dequant[i] = next()
				if trial%2 == 0 {
					q.Round[i] &= 32767
					q.QuantFast[i] &= 32767
				}
			}
			c := make([]int16, count*16)
			for i := range c {
				c[i] = next()
			}
			if len(c) > 0 {
				c[0] = -32768
			}
			gotQ, gotD, wantQ, wantD := make([]int16, len(c)), make([]int16, len(c)), make([]int16, len(c)), make([]int16, len(c))
			gotE, wantE := make([]uint8, count), make([]uint8, count)
			FastQuantizeBlockBatch(c, &q, gotQ, gotD, gotE, count)
			fastQuantizeBlockBatchScalar(c, &q, wantQ, wantD, wantE, count)
			if !reflect.DeepEqual(gotQ, wantQ) || !reflect.DeepEqual(gotD, wantD) || !reflect.DeepEqual(gotE, wantE) {
				t.Fatalf("count%d trial%d", count, trial)
			}
		}
	}
}

var quantBatchNativeSink uint64

// Identical public-call contract for old assembly and every Go candidate.
func BenchmarkFastQuantizeBatchNative(b *testing.B) {
	for _, count := range []int{1, 2, 8, 16, 25} {
		b.Run(fmt.Sprintf("blocks%d", count), func(b *testing.B) {
			var dequant [16]int16
			for i := range dequant {
				dequant[i] = int16(i + 10)
			}
			var quant BlockQuant
			InitFastBlockQuant(&dequant, &quant)
			c, q, dq, e := make([]int16, count*16), make([]int16, count*16), make([]int16, count*16), make([]uint8, count)
			for i := range c {
				c[i] = int16((i*17)%8191 - 4095)
				if i%7 < 3 {
					c[i] = 0
				}
			}
			wantQ, wantDQ, wantE := make([]int16, len(c)), make([]int16, len(c)), make([]uint8, count)
			for i := range count {
				wantE[i] = uint8(fastQuantizeBlockScalar((*[16]int16)(c[i*16:]), &quant, (*[16]int16)(wantQ[i*16:]), (*[16]int16)(wantDQ[i*16:])))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				FastQuantizeBlockBatch(c, &quant, q, dq, e, count)
			}
			b.StopTimer()
			for i := range q {
				if q[i] != wantQ[i] || dq[i] != wantDQ[i] {
					b.Fatalf("coefficient %d differs from scalar oracle", i)
				}
			}
			for i := range e {
				if e[i] != wantE[i] {
					b.Fatalf("EOB %d differs from scalar oracle", i)
				}
			}
			var sink uint64
			for i := range q {
				sink += uint64(uint16(q[i])) + uint64(uint16(dq[i]))
			}
			for _, v := range e {
				sink += uint64(v)
			}
			quantBatchNativeSink = sink
		})
	}
}

func TestVP8WorkElisionQuantOnlyReference(t *testing.T) {
	rng := rand.New(rand.NewSource(715645))
	for _, count := range []int{1, 2, 8, 16, 25} {
		for trial := 0; trial < 40; trial++ {
			var dq common.MacroblockDequant
			var q MacroblockQuant
			var tables common.FrameDequantTables
			common.BuildFrameDequantTables(common.QuantDeltas{}, &tables)
			common.InitMacroblockDequant(&tables, trial*3%128, &dq)
			InitFastMacroblockQuant(&dq, &q)
			quant := q.Y1
			if trial%4 == 0 { // Exercise signed-table SIMD rejection.
				for i := range 16 {
					quant.Round[i] = int16(rng.Uint32())
					quant.QuantFast[i] = int16(rng.Uint32())
				}
			}
			coeff := make([]int16, count*16)
			for i := range coeff {
				coeff[i] = int16(rng.Uint32())
				if trial%3 == 0 && i%16 != 0 {
					coeff[i] = 0
				}
			}
			coeff[0] = -32768
			gotQ, wantQ, wantDQ := make([]int16, len(coeff)+3), make([]int16, len(coeff)+3), make([]int16, len(coeff)+3)
			gotE, wantE := make([]uint8, count+3), make([]uint8, count+3)
			for i := range gotQ {
				gotQ[i] = 77
				wantQ[i] = 77
			}
			for i := range gotE {
				gotE[i] = 99
				wantE[i] = 99
			}
			FastQuantizeBlockBatchOnly(coeff, &quant, gotQ, gotE, count)
			fastQuantizeBlockBatchScalar(coeff, &quant, wantQ, wantDQ, wantE, count)
			if !reflect.DeepEqual(gotQ, wantQ) || !reflect.DeepEqual(gotE, wantE) {
				t.Fatalf("count=%d trial=%d", count, trial)
			}
		}
	}
}

func TestVP8WorkElisionQuantOnlyCoefficientOverlap(t *testing.T) {
	var dq common.MacroblockDequant
	var quant MacroblockQuant
	var tables common.FrameDequantTables
	common.BuildFrameDequantTables(common.QuantDeltas{}, &tables)
	common.InitMacroblockDequant(&tables, 50, &dq)
	InitFastMacroblockQuant(&dq, &quant)
	for _, shift := range []int{-16, -1, 0, 1, 16} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			const count = 8
			src := make([]int16, count*16+64)
			for i := range src {
				src[i] = int16(i*107 - 8000)
			}
			got, want := append([]int16(nil), src...), append([]int16(nil), src...)
			var ge, we [count]uint8
			var discard [count * 16]int16
			FastQuantizeBlockBatchOnly(got[32:], &quant.Y1, got[32+shift:], ge[:], count)
			FastQuantizeBlockBatch(want[32:], &quant.Y1, want[32+shift:], discard[:], we[:], count)
			if !reflect.DeepEqual(got, want) || ge != we {
				t.Fatal("overlap changed stores")
			}
		})
	}
}

var quantOnlyBenchSink int16

func BenchmarkFastQuantizeBatchOnly(b *testing.B) {
	for _, count := range []int{8, 16} {
		for _, pattern := range []string{"zero", "sparse", "dense"} {
			b.Run(fmt.Sprintf("%d/%s", count, pattern), func(b *testing.B) {
				var dq common.MacroblockDequant
				var quant MacroblockQuant
				var tables common.FrameDequantTables
				common.BuildFrameDequantTables(common.QuantDeltas{}, &tables)
				common.InitMacroblockDequant(&tables, 50, &dq)
				InitFastMacroblockQuant(&dq, &quant)
				c, q, d := make([]int16, count*16), make([]int16, count*16), make([]int16, count*16)
				e := make([]uint8, count)
				for i := range c {
					if pattern == "dense" || pattern == "sparse" && i%16 == 0 {
						c[i] = int16((i*53)%1024 - 512)
					}
				}
				b.Run("with-dequant", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						FastQuantizeBlockBatch(c, &quant.Y1, q, d, e, count)
					}
					quantOnlyBenchSink = q[0]
				})
				b.Run("quant-only", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						FastQuantizeBlockBatchOnly(c, &quant.Y1, q, e, count)
					}
					quantOnlyBenchSink = q[0]
				})
			})
		}
	}
}

func TestQuantBatchNativeSelected(t *testing.T) {
	if !pixel.Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD backend unavailable")
		}
		t.Skip("native unavailable")
	}
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = 10
	}
	var quant BlockQuant
	InitFastBlockQuant(&dequant, &quant)
	var c, q, dq [128]int16
	var e [8]uint8
	if !pixel.QuantizeVP8Batch(c[:], &quant.Round, &quant.QuantFast, &quant.Dequant, q[:], dq[:], &tables.DefaultInvZigZag, e[:], 8) {
		t.Fatal("native batch declined eligible work")
	}
}

// TestFastQuantizeBlockBatchMatchesPerBlock cross-checks the batched
// fast-quantize dispatcher against the per-block FastQuantizeBlock
// for a range of block counts, coefficient ranges, and dequant
// settings. Every batched lane must be byte-identical to the
// per-block scalar reference (the libvpx vp8_fast_quantize_b_c
// invariant).
func TestFastQuantizeBlockBatchMatchesPerBlock(t *testing.T) {
	r := rand.New(rand.NewSource(0xBEEF1234))
	for _, count := range []int{1, 2, 8, 16, 24} {
		t.Run("", func(t *testing.T) {
			var dequant [16]int16
			for i := range dequant {
				dequant[i] = int16(4 + r.Intn(252))
			}
			var quant BlockQuant
			InitFastBlockQuant(&dequant, &quant)
			coeff := make([]int16, count*16)
			for i := range coeff {
				coeff[i] = int16(r.Intn(4096) - 2048)
			}
			batchQ := make([]int16, count*16)
			batchDQ := make([]int16, count*16)
			batchE := make([]uint8, count)
			FastQuantizeBlockBatch(coeff, &quant, batchQ, batchDQ, batchE, count)

			for i := range count {
				var c, qb, dqb [16]int16
				copy(c[:], coeff[i*16:i*16+16])
				wantE := FastQuantizeBlock(&c, &quant, &qb, &dqb)
				for j := range 16 {
					if batchQ[i*16+j] != qb[j] {
						t.Fatalf("count=%d block=%d lane=%d qcoeff: batch=%d per=%d", count, i, j, batchQ[i*16+j], qb[j])
					}
					if batchDQ[i*16+j] != dqb[j] {
						t.Fatalf("count=%d block=%d lane=%d dqcoeff: batch=%d per=%d", count, i, j, batchDQ[i*16+j], dqb[j])
					}
				}
				if int(batchE[i]) != wantE {
					t.Fatalf("count=%d block=%d eob: batch=%d per=%d", count, i, batchE[i], wantE)
				}
			}
		})
	}
}

// TestFastQuantizeBlockBatchSentinels reuses the per-block sentinels
// (zero / DC-only / boundary / sign patterns) packed into multi-block
// batches so the loop's per-iteration state is exercised.
func TestFastQuantizeBlockBatchSentinels(t *testing.T) {
	cases := []struct {
		name  string
		coeff [16]int16
		dq    int16
	}{
		{name: "zero", dq: 10},
		{name: "dc_only_pos", coeff: [16]int16{120}, dq: 10},
		{name: "dc_only_neg", coeff: [16]int16{-120}, dq: 10},
		{name: "last_pos", coeff: [16]int16{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 90}, dq: 8},
		{name: "last_neg", coeff: [16]int16{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -90}, dq: 8},
		{name: "all_pos", coeff: [16]int16{37, 25, 14, 8, 3, 21, 12, 1, 1, 1, 1, 1, 5, 2, 1, 9}, dq: 6},
		{name: "alt_signs", coeff: [16]int16{37, -25, 14, -8, 3, -21, 12, -1, 1, -1, 1, -1, 5, -2, 1, -9}, dq: 12},
		{name: "boundary_high_pos", coeff: [16]int16{2047, 2046, 2045, 2044, 2043, 2042, 2041, 2040, 2039, 2038, 2037, 2036, 2035, 2034, 2033, 2032}, dq: 4},
		{name: "boundary_high_neg", coeff: [16]int16{-2048, -2047, -2046, -2045, -2044, -2043, -2042, -2041, -2040, -2039, -2038, -2037, -2036, -2035, -2034, -2033}, dq: 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const blocks = 16
			var dequant [16]int16
			for i := range dequant {
				dequant[i] = tc.dq
			}
			var quant BlockQuant
			InitFastBlockQuant(&dequant, &quant)
			coeff := make([]int16, blocks*16)
			for i := range blocks {
				copy(coeff[i*16:i*16+16], tc.coeff[:])
			}
			batchQ := make([]int16, blocks*16)
			batchDQ := make([]int16, blocks*16)
			batchE := make([]uint8, blocks)
			FastQuantizeBlockBatch(coeff, &quant, batchQ, batchDQ, batchE, blocks)
			for i := range blocks {
				var c, qb, dqb [16]int16
				copy(c[:], coeff[i*16:i*16+16])
				wantE := FastQuantizeBlock(&c, &quant, &qb, &dqb)
				for j := range 16 {
					if batchQ[i*16+j] != qb[j] {
						t.Fatalf("%s block=%d lane=%d qcoeff: batch=%d per=%d", tc.name, i, j, batchQ[i*16+j], qb[j])
					}
					if batchDQ[i*16+j] != dqb[j] {
						t.Fatalf("%s block=%d lane=%d dqcoeff: batch=%d per=%d", tc.name, i, j, batchDQ[i*16+j], dqb[j])
					}
				}
				if int(batchE[i]) != wantE {
					t.Fatalf("%s block=%d eob: batch=%d per=%d", tc.name, i, batchE[i], wantE)
				}
			}
		})
	}
}

// BenchmarkFastQuantizeBlockBatch25 mirrors the libvpx
// vp8_quantize_mb call pattern: 25 blocks (16 Y + 8 UV + 1 Y2)
// quantized in a single dispatch. Run as a single batch with the
// same quant tables for benchmarking purposes.
func BenchmarkFastQuantizeBlockBatch25(b *testing.B) {
	const blocks = 25
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = int16(i + 10)
	}
	var quant BlockQuant
	InitFastBlockQuant(&dequant, &quant)
	coeff := make([]int16, blocks*16)
	for i := range coeff {
		coeff[i] = int16(((i * 7) % 256) - 128)
	}
	qcoeff := make([]int16, blocks*16)
	dqcoeff := make([]int16, blocks*16)
	eobs := make([]uint8, blocks)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		FastQuantizeBlockBatch(coeff, &quant, qcoeff, dqcoeff, eobs, blocks)
	}
}

// BenchmarkFastQuantizeBlockPerBlock25 dispatches 25 individual
// fast-quantize calls with identical inputs to the batch variant for
// comparison against the existing per-block call pattern.
func BenchmarkFastQuantizeBlockPerBlock25(b *testing.B) {
	const blocks = 25
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = int16(i + 10)
	}
	var quant BlockQuant
	InitFastBlockQuant(&dequant, &quant)
	coeff := make([]int16, blocks*16)
	for i := range coeff {
		coeff[i] = int16(((i * 7) % 256) - 128)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for j := range blocks {
			var c, q, dq [16]int16
			copy(c[:], coeff[j*16:j*16+16])
			_ = FastQuantizeBlock(&c, &quant, &q, &dq)
		}
	}
}
