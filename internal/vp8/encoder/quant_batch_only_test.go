package encoder

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

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
