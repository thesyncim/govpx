package dsp

import (
	"math/rand"
	"testing"
)

func TestIDCT4x4AddSIMDMatchesScalar(t *testing.T) {
	cases := []struct {
		name string
		in   [16]int16
	}{
		{name: "zero"},
		{name: "dc_pos", in: [16]int16{200}},
		{name: "dc_neg", in: [16]int16{-200}},
		{name: "ramp", in: [16]int16{-8, -7, -6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6, 7}},
		{name: "alt_signs", in: [16]int16{300, -300, 300, -300, -300, 300, -300, 300, 300, -300, 300, -300, -300, 300, -300, 300}},
		{name: "single_dc_top", in: [16]int16{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 100}},
		{name: "high_dc_clip", in: [16]int16{4096}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pred := make([]byte, 8*8)
			for i := range pred {
				pred[i] = byte((i*9 + 17) & 255)
			}
			dstSim := make([]byte, 8*8)
			dstScl := make([]byte, 8*8)
			inSim := tc.in
			inScl := tc.in
			idct4x4AddSIMD(&inSim, pred, 8, dstSim, 8)
			idct4x4AddScalar(&inScl, pred, 8, dstScl, 8)
			for y := range 4 {
				for x := range 4 {
					if dstSim[y*8+x] != dstScl[y*8+x] {
						t.Fatalf("[%d,%d]: simd=%d scalar=%d (in=%v)", x, y, dstSim[y*8+x], dstScl[y*8+x], tc.in)
					}
				}
			}
		})
	}

	// Random fuzz across realistic post-dequant coefficient ranges.
	r := rand.New(rand.NewSource(0xBEEFCAFE))
	for iter := range 2000 {
		var in [16]int16
		// VP8 dequantized coefficients are roughly bounded by 16x quant range,
		// so |val| <= ~6000 covers nearly all real cases. We test smaller for
		// stability.
		for i := range in {
			in[i] = int16(r.Intn(2049) - 1024)
		}
		predStride := 8
		dstStride := 8
		pred := make([]byte, predStride*8)
		for i := range pred {
			pred[i] = byte(r.Intn(256))
		}
		dstSim := make([]byte, dstStride*8)
		dstScl := make([]byte, dstStride*8)
		copy(dstSim, pred)
		copy(dstScl, pred)
		inSim := in
		inScl := in
		idct4x4AddSIMD(&inSim, pred, predStride, dstSim, dstStride)
		idct4x4AddScalar(&inScl, pred, predStride, dstScl, dstStride)
		for y := range 4 {
			for x := range 4 {
				if dstSim[y*dstStride+x] != dstScl[y*dstStride+x] {
					t.Fatalf("iter=%d [%d,%d]: simd=%d scalar=%d in=%v", iter, x, y, dstSim[y*dstStride+x], dstScl[y*dstStride+x], in)
				}
			}
		}
	}
}

func TestDCOnlyIDCT4x4AddSIMDMatchesScalar(t *testing.T) {
	pred := make([]byte, 8*8)
	for i := range pred {
		pred[i] = byte((i*7 + 3) & 255)
	}
	for dc := int16(-2048); dc <= 2047; dc += 17 {
		dstSim := make([]byte, 8*8)
		dstScl := make([]byte, 8*8)
		dcOnlyIDCT4x4AddSIMD(dc, pred, 8, dstSim, 8)
		dcOnlyIDCT4x4AddScalar(dc, pred, 8, dstScl, 8)
		for y := range 4 {
			for x := range 4 {
				if dstSim[y*8+x] != dstScl[y*8+x] {
					t.Fatalf("dc=%d [%d,%d]: simd=%d scalar=%d", dc, x, y, dstSim[y*8+x], dstScl[y*8+x])
				}
			}
		}
	}
}

func BenchmarkIDCT4x4AddSIMD(b *testing.B) {
	var input [16]int16
	for i := range input {
		input[i] = int16(i*9 - 40)
	}
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		idct4x4AddSIMD(&input, pred, 8, dst, 8)
	}
}

func BenchmarkIDCT4x4AddScalar(b *testing.B) {
	var input [16]int16
	for i := range input {
		input[i] = int16(i*9 - 40)
	}
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		idct4x4AddScalar(&input, pred, 8, dst, 8)
	}
}

func BenchmarkDCOnlyIDCT4x4AddSIMD(b *testing.B) {
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dcOnlyIDCT4x4AddSIMD(128, pred, 8, dst, 8)
	}
}

func BenchmarkDCOnlyIDCT4x4AddScalar(b *testing.B) {
	pred := make([]byte, 8*8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dcOnlyIDCT4x4AddScalar(128, pred, 8, dst, 8)
	}
}

func TestDequantIDCTAddFull2xMatchesFallback(t *testing.T) {
	// The fused pair kernel must match the per-block composition
	// (full wrapping dequant + IDCT4x4Add) byte-for-byte across VP8
	// quantized-coefficient and dequant-table ranges.
	r := rand.New(rand.NewSource(0x2B10C7))
	dequants := [][16]int16{
		{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4},
		{157, 132, 132, 132, 132, 132, 132, 132, 132, 132, 132, 132, 132, 132, 132, 132},
		{8, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6},
		{1, 284, 284, 284, 284, 284, 284, 284, 284, 284, 284, 284, 284, 284, 284, 284},
	}
	for iter := range 4000 {
		var q [32]int16
		dq := dequants[iter%len(dequants)]
		nonzero := iter % 33
		for i := range nonzero {
			// Token magnitudes: DCT_VAL_CATEGORY6 extends to 2048+.
			q[(i*7)%32] = int16(r.Intn(4400) - 2200)
		}
		pred := make([]byte, 8*8)
		for i := range pred {
			pred[i] = byte(r.Intn(256))
		}
		dstSim := append([]byte(nil), pred...)
		dstRef := append([]byte(nil), pred...)
		qSim := q
		qRef := q
		DequantIDCTAddFull2x(&qSim, &dq, dstSim, 8)
		dequantIDCTAddFull2xFallback(&qRef, &dq, dstRef, 8)
		for i := range dstSim {
			if dstSim[i] != dstRef[i] {
				t.Fatalf("iter %d: byte %d: fused=%d ref=%d (q=%v dq=%v)", iter, i, dstSim[i], dstRef[i], q, dq)
			}
		}
	}
}
