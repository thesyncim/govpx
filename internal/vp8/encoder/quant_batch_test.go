package encoder

import (
	"math/rand"
	"testing"
)

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
