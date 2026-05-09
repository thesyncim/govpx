package encoder

import (
	"math/rand"
	"testing"
)

// TestFastQuantizeBlockSIMDMatchesScalar exhaustively cross-checks the
// per-arch SIMD fast-quantize kernel against the scalar reference for the
// boundary cases the encoder exercises (all-zero, sparse, dense, sign
// patterns, large +/- coefficient ranges) plus randomized fuzz.
func TestFastQuantizeBlockSIMDMatchesScalar(t *testing.T) {
	mkQuant := func(d int16) BlockQuant {
		var dequant [16]int16
		for i := range dequant {
			dequant[i] = d
		}
		var quant BlockQuant
		InitFastBlockQuant(&dequant, &quant)
		return quant
	}

	cases := []struct {
		name  string
		coeff [16]int16
		dq    int16
	}{
		{name: "zero", dq: 10},
		{name: "dc_only_pos", coeff: [16]int16{120, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, dq: 10},
		{name: "dc_only_neg", coeff: [16]int16{-120, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, dq: 10},
		{name: "last_pos", coeff: [16]int16{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 90}, dq: 8},
		{name: "last_neg", coeff: [16]int16{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -90}, dq: 8},
		{name: "all_pos", coeff: [16]int16{37, 25, 14, 8, 3, 21, 12, 1, 1, 1, 1, 1, 5, 2, 1, 9}, dq: 6},
		{name: "all_neg", coeff: [16]int16{-37, -25, -14, -8, -3, -21, -12, -1, -1, -1, -1, -1, -5, -2, -1, -9}, dq: 6},
		{name: "alt_signs", coeff: [16]int16{37, -25, 14, -8, 3, -21, 12, -1, 1, -1, 1, -1, 5, -2, 1, -9}, dq: 12},
		{name: "boundary_high_pos", coeff: [16]int16{2047, 2046, 2045, 2044, 2043, 2042, 2041, 2040, 2039, 2038, 2037, 2036, 2035, 2034, 2033, 2032}, dq: 4},
		{name: "boundary_high_neg", coeff: [16]int16{-2048, -2047, -2046, -2045, -2044, -2043, -2042, -2041, -2040, -2039, -2038, -2037, -2036, -2035, -2034, -2033}, dq: 4},
		{name: "below_round", coeff: [16]int16{1, -1, 2, -2, 3, -3, 4, -4, 5, -5, 6, -6, 7, -7, 8, -8}, dq: 64},
		{name: "high_q", coeff: [16]int16{37, -25, 14, -8, 3, -21, 12, -1, 1, -1, 1, -1, 5, -2, 1, -9}, dq: 200},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			quant := mkQuant(tc.dq)

			var qSim, dqSim, qScalar, dqScalar [16]int16
			for i := range qSim {
				qSim[i], dqSim[i], qScalar[i], dqScalar[i] = 99, 99, 99, 99
			}

			eobSim := fastQuantizeBlockSIMD(&tc.coeff, &quant, &qSim, &dqSim)
			eobScalar := fastQuantizeBlockScalar(&tc.coeff, &quant, &qScalar, &dqScalar)

			if eobSim != eobScalar {
				t.Fatalf("eob: simd=%d scalar=%d", eobSim, eobScalar)
			}
			if qSim != qScalar {
				t.Fatalf("qcoeff:\n  simd=%v\n  scl =%v", qSim, qScalar)
			}
			if dqSim != dqScalar {
				t.Fatalf("dqcoeff:\n  simd=%v\n  scl =%v", dqSim, dqScalar)
			}
		})
	}

	// Random fuzz across plausible coefficient and dequant ranges.
	r := rand.New(rand.NewSource(0xC0FFEE))
	for iter := range 4000 {
		var coeff [16]int16
		for i := range coeff {
			coeff[i] = int16(r.Intn(4096) - 2048)
		}
		var dequant [16]int16
		for i := range dequant {
			dequant[i] = int16(4 + r.Intn(252))
		}
		var quant BlockQuant
		InitFastBlockQuant(&dequant, &quant)

		var qSim, dqSim, qScalar, dqScalar [16]int16
		eobSim := fastQuantizeBlockSIMD(&coeff, &quant, &qSim, &dqSim)
		eobScalar := fastQuantizeBlockScalar(&coeff, &quant, &qScalar, &dqScalar)

		if eobSim != eobScalar || qSim != qScalar || dqSim != dqScalar {
			t.Fatalf("iter=%d coeff=%v dq=%v\n simd  q=%v dq=%v eob=%d\n scalar q=%v dq=%v eob=%d",
				iter, coeff, dequant, qSim, dqSim, eobSim, qScalar, dqScalar, eobSim)
		}
	}
}

func BenchmarkFastQuantizeBlockSIMD(b *testing.B) {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = int16(i + 10)
	}
	var quant BlockQuant
	InitFastBlockQuant(&dequant, &quant)
	coeff := [16]int16{37, -25, 14, -8, 3, 21, -12, 0, 0, 0, 0, 0, -5, 2, 0, 9}
	var qcoeff, dqcoeff [16]int16

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = fastQuantizeBlockSIMD(&coeff, &quant, &qcoeff, &dqcoeff)
	}
}

func BenchmarkFastQuantizeBlockScalar(b *testing.B) {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = int16(i + 10)
	}
	var quant BlockQuant
	InitFastBlockQuant(&dequant, &quant)
	coeff := [16]int16{37, -25, 14, -8, 3, 21, -12, 0, 0, 0, 0, 0, -5, 2, 0, 9}
	var qcoeff, dqcoeff [16]int16

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = fastQuantizeBlockScalar(&coeff, &quant, &qcoeff, &dqcoeff)
	}
}
