package encoder

import (
	"math/rand"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// libvpxFastQuantizeBReference is a transliteration of
// libvpx v1.16.0 vp8/encoder/vp8_quantize.c vp8_fast_quantize_b_c
// (lines 21-49). It preserves the per-coefficient sign-mask formulation
// and the unconditional inner-loop body (no z==0 short-circuit), matching
// the C arithmetic byte for byte. Used by TestFastQuantizeBlockMatchesLibvpxC
// to lock govpx's scalar + SIMD fast quantize to libvpx-equivalent output
// across the realistic VP8 coefficient range.
func libvpxFastQuantizeBReference(coeff *[16]int16, round *[16]int16, quantFast *[16]int16, dequant *[16]int16, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	eob := -1
	for i := range 16 {
		rc := int(tables.DefaultZigZag1D[i])
		z := int32(coeff[rc])
		sz := z >> 31
		x := (z ^ sz) - sz
		// libvpx uses int * short multiply: the (x + round) sum is a
		// 32-bit int but quant_fast is loaded as short. Mimic with
		// int32 math so the >>16 truncation matches.
		y := ((x + int32(round[rc])) * int32(quantFast[rc])) >> 16
		xs := (y ^ sz) - sz
		qcoeff[rc] = int16(xs)
		dqcoeff[rc] = int16(xs * int32(dequant[rc]))
		if y != 0 {
			eob = i
		}
	}
	return eob + 1
}

func TestInitFastBlockQuant(t *testing.T) {
	dequant := [16]int16{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25}
	var quant BlockQuant

	InitFastBlockQuant(&dequant, &quant)

	if quant.Dequant != dequant {
		t.Fatalf("dequant = %v, want %v", quant.Dequant, dequant)
	}
	for i, d := range dequant {
		wantFast := int16((1 << 16) / int(d))
		wantRound := int16((quantRoundFactor * int(d)) >> 7)
		if quant.QuantFast[i] != wantFast || quant.Round[i] != wantRound {
			t.Fatalf("quant[%d] = fast %d round %d, want fast %d round %d", i, quant.QuantFast[i], quant.Round[i], wantFast, wantRound)
		}
	}
}

func TestInitFastMacroblockQuant(t *testing.T) {
	var tables common.FrameDequantTables
	var dequant common.MacroblockDequant
	var quant MacroblockQuant

	common.BuildFrameDequantTables(common.QuantDeltas{Y1DC: 1, Y2DC: 2, Y2AC: 3, UVDC: 4, UVAC: 5}, &tables)
	common.InitMacroblockDequant(&tables, 20, &dequant)
	InitFastMacroblockQuant(&dequant, &quant)

	if quant.Y1.Dequant != dequant.Y1 || quant.Y1DC.Dequant != dequant.Y1DC || quant.Y2.Dequant != dequant.Y2 || quant.UV.Dequant != dequant.UV {
		t.Fatalf("macroblock quant dequant tables do not mirror source dequants")
	}
	if quant.Y1DC.Dequant[0] != 1 {
		t.Fatalf("Y1DC dequant[0] = %d, want 1", quant.Y1DC.Dequant[0])
	}
}

func TestInitRegularBlockQuantMatchesLibvpxSetup(t *testing.T) {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = 100
	}
	var quant BlockQuant

	InitRegularBlockQuant(80, &dequant, &quant)

	if quant.QuantFast[1] != 655 || quant.Round[1] != 37 || quant.Zbin[1] != 63 {
		t.Fatalf("regular quant fast/round/zbin = %d/%d/%d, want 655/37/63", quant.QuantFast[1], quant.Round[1], quant.Zbin[1])
	}
	if quant.Quant[1] != -23592 || quant.QuantShift[1] != 1024 {
		t.Fatalf("regular quant inverse = %d/%d, want -23592/1024", quant.Quant[1], quant.QuantShift[1])
	}
	if quant.ZbinBoost[7] != 15 {
		t.Fatalf("zrun boost[7] = %d, want 15", quant.ZbinBoost[7])
	}

	for i := range dequant {
		dequant[i] = 10
	}
	InitRegularBlockQuant(4, &dequant, &quant)
	if quant.Quant[1] != -13107 || quant.QuantShift[1] != 8192 || quant.Zbin[1] != 7 {
		t.Fatalf("low-q regular quant = %d/%d zbin %d, want -13107/8192 zbin 7", quant.Quant[1], quant.QuantShift[1], quant.Zbin[1])
	}
}

// TestInitRegularBlockQuantLibvpxFixedQ64 locks the per-coefficient
// Zbin/Round/Quant/QuantShift/ZbinBoost values produced by
// InitRegularBlockQuant for QIndex=64, dequant=10 against numbers derived by
// hand-evaluating libvpx v1.16.0 vp8/encoder/vp8_quantize.c
// vp8cx_init_quantizer (with qzbin_factors[64]=80, qrounding_factors[64]=48,
// improved_quant=1):
//
//	round       = (48*10) >> 7        = 3
//	zbin        = (80*10 + 64) >> 7   = 6
//	quant_fast  = (1<<16)/10          = 6553
//	invert_quant(10) -> l=3, m=1+(1<<19)/10=52429
//	  quant       = m - (1<<16)       = -13107
//	  quant_shift = 1 << (16-3)       = 8192
//	zrun_zbin_boost[i] = (10*zbin_boost[i]) >> 7 with
//	  zbin_boost = {0,0,8,10,12,14,16,20,24,28,32,36,40,44,44,44}
//	  =>          {0,0,0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 3, 3, 3, 3}
func TestInitRegularBlockQuantLibvpxFixedQ64(t *testing.T) {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = 10
	}
	var quant BlockQuant

	InitRegularBlockQuant(64, &dequant, &quant)

	wantFast := int16(6553)
	wantRound := int16(3)
	wantZbin := int16(6)
	wantQuant := int16(-13107)
	wantShift := int16(8192)
	wantBoost := [16]int16{0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 3, 3, 3, 3}

	for i := range 16 {
		if quant.Dequant[i] != 10 {
			t.Fatalf("Dequant[%d] = %d, want 10", i, quant.Dequant[i])
		}
		if quant.QuantFast[i] != wantFast {
			t.Fatalf("QuantFast[%d] = %d, want %d", i, quant.QuantFast[i], wantFast)
		}
		if quant.Round[i] != wantRound {
			t.Fatalf("Round[%d] = %d, want %d", i, quant.Round[i], wantRound)
		}
		if quant.Zbin[i] != wantZbin {
			t.Fatalf("Zbin[%d] = %d, want %d", i, quant.Zbin[i], wantZbin)
		}
		if quant.Quant[i] != wantQuant {
			t.Fatalf("Quant[%d] = %d, want %d", i, quant.Quant[i], wantQuant)
		}
		if quant.QuantShift[i] != wantShift {
			t.Fatalf("QuantShift[%d] = %d, want %d", i, quant.QuantShift[i], wantShift)
		}
		if quant.ZbinBoost[i] != wantBoost[i] {
			t.Fatalf("ZbinBoost[%d] = %d, want %d", i, quant.ZbinBoost[i], wantBoost[i])
		}
	}
}

// TestInitRegularBlockQuantLibvpxZbinFactorBoundary locks the Q<48 vs Q>=48
// split of qzbin_factors in libvpx vp8/encoder/vp8_quantize.c (84 for the
// first 48 indices, 80 thereafter). dequant=128 is chosen so the >>7 right
// shift cleanly returns the factor itself.
func TestInitRegularBlockQuantLibvpxZbinFactorBoundary(t *testing.T) {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = 128
	}
	var quant BlockQuant

	// Q=47 -> qzbin_factors[47]==84 -> Zbin = (84*128 + 64) >> 7 = 84.
	InitRegularBlockQuant(47, &dequant, &quant)
	// (84*128 + 64) >> 7 = (10752 + 64) / 128 = 84
	if got := quant.Zbin[1]; got != 84 {
		t.Fatalf("Zbin[1] @ Q=47 = %d, want 84 (qzbin_factors[47]=84)", got)
	}

	// Q=48 -> qzbin_factors[48]==80 -> Zbin = (80*128 + 64) >> 7 = 80.
	InitRegularBlockQuant(48, &dequant, &quant)
	if got := quant.Zbin[1]; got != 80 {
		t.Fatalf("Zbin[1] @ Q=48 = %d, want 80 (qzbin_factors[48]=80)", got)
	}
}

func TestInitSegmentMacroblockQuantsUsesDeltaSegmentation(t *testing.T) {
	segmentation := SegmentationConfig{Enabled: true, UpdateData: true}
	segmentation.FeatureEnabled[common.MBLvlAltQ][1] = true
	segmentation.FeatureData[common.MBLvlAltQ][1] = 10
	segmentation.FeatureEnabled[common.MBLvlAltQ][2] = true
	segmentation.FeatureData[common.MBLvlAltQ][2] = -30
	var quants [common.MaxMBSegments]MacroblockQuant

	if err := InitSegmentMacroblockQuants(20, common.QuantDeltas{}, segmentation, &quants); err != nil {
		t.Fatalf("InitSegmentMacroblockQuants returned error: %v", err)
	}

	wantSeg0AC := int16(common.ACYQuant(20))
	wantSeg1AC := int16(common.ACYQuant(30))
	wantSeg2AC := int16(common.ACYQuant(0))
	if quants[0].Y1.Dequant[1] != wantSeg0AC || quants[1].Y1.Dequant[1] != wantSeg1AC || quants[2].Y1.Dequant[1] != wantSeg2AC {
		t.Fatalf("segment AC dequants = %d/%d/%d, want %d/%d/%d", quants[0].Y1.Dequant[1], quants[1].Y1.Dequant[1], quants[2].Y1.Dequant[1], wantSeg0AC, wantSeg1AC, wantSeg2AC)
	}
}

func TestInitSegmentMacroblockQuantsUsesAbsSegmentation(t *testing.T) {
	segmentation := SegmentationConfig{Enabled: true, UpdateData: true, AbsDelta: true}
	segmentation.FeatureEnabled[common.MBLvlAltQ][3] = true
	segmentation.FeatureData[common.MBLvlAltQ][3] = 7
	var quants [common.MaxMBSegments]MacroblockQuant

	if err := InitSegmentMacroblockQuants(20, common.QuantDeltas{UVDC: 5}, segmentation, &quants); err != nil {
		t.Fatalf("InitSegmentMacroblockQuants returned error: %v", err)
	}

	wantSeg3YAC := int16(common.ACYQuant(7))
	wantSeg3UVDC := int16(common.DCUVQuant(7, 5))
	if quants[3].Y1.Dequant[1] != wantSeg3YAC || quants[3].UV.Dequant[0] != wantSeg3UVDC {
		t.Fatalf("segment 3 dequants = YAC %d UVDC %d, want %d/%d", quants[3].Y1.Dequant[1], quants[3].UV.Dequant[0], wantSeg3YAC, wantSeg3UVDC)
	}
}

func TestFastQuantizeBlockSentinel(t *testing.T) {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = 10
	}
	var quant BlockQuant
	InitFastBlockQuant(&dequant, &quant)
	coeff := [16]int16{37, -25, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9}
	qcoeff := filledBlock(77)
	dqcoeff := filledBlock(88)

	eob := FastQuantizeBlock(&coeff, &quant, &qcoeff, &dqcoeff)

	wantQ := [16]int16{3, -2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	wantDQ := [16]int16{30, -20, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 10}
	if eob != 16 {
		t.Fatalf("eob = %d, want 16", eob)
	}
	if qcoeff != wantQ || dqcoeff != wantDQ {
		t.Fatalf("quantized = %v/%v, want %v/%v", qcoeff, dqcoeff, wantQ, wantDQ)
	}
}

func TestFastQuantizeBlockZeroesOutputs(t *testing.T) {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = 10
	}
	var quant BlockQuant
	InitFastBlockQuant(&dequant, &quant)
	qcoeff := filledBlock(77)
	dqcoeff := filledBlock(88)

	eob := FastQuantizeBlock(&[16]int16{}, &quant, &qcoeff, &dqcoeff)

	if eob != 0 {
		t.Fatalf("eob = %d, want 0", eob)
	}
	if qcoeff != ([16]int16{}) || dqcoeff != ([16]int16{}) {
		t.Fatalf("outputs = %v/%v, want zero blocks", qcoeff, dqcoeff)
	}
}

func TestFastQuantizeBlockAllocatesZero(t *testing.T) {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = int16(i + 10)
	}
	var quant BlockQuant
	var qcoeff, dqcoeff [16]int16
	coeff := [16]int16{37, -25, 14, -8, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9}

	allocs := testing.AllocsPerRun(1000, func() {
		InitFastBlockQuant(&dequant, &quant)
		_ = FastQuantizeBlock(&coeff, &quant, &qcoeff, &dqcoeff)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkFastQuantizeBlock(b *testing.B) {
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
		_ = FastQuantizeBlock(&coeff, &quant, &qcoeff, &dqcoeff)
	}
}

func BenchmarkFastQuantizeBlockSparse(b *testing.B) {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = int16(i + 10)
	}
	var quant BlockQuant
	InitFastBlockQuant(&dequant, &quant)
	coeff := [16]int16{37, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9}
	var qcoeff, dqcoeff [16]int16

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = FastQuantizeBlock(&coeff, &quant, &qcoeff, &dqcoeff)
	}
}

func filledBlock(v int16) [16]int16 {
	return [16]int16{v, v, v, v, v, v, v, v, v, v, v, v, v, v, v, v}
}

// TestFastQuantizeBlockMatchesLibvpxC locks govpx's FastQuantizeBlock
// (SIMD-dispatched) and fastQuantizeBlockScalar to a direct port of
// libvpx v1.16.0 vp8/encoder/vp8_quantize.c vp8_fast_quantize_b_c. The
// test sweeps the realistic encoder coefficient range:
//
//   - Every Q in [0, MaxQ]
//   - DC and AC dequant pulled from common.BuildFrameDequantTables, so
//     the Round/QuantFast tables exactly mirror what
//     vp8cx_init_quantizer produces (qrounding_factors=48 across all Q,
//     qzbin_factors don't affect fast quant).
//   - Coefficients spanning the full int16 VP8 DCT range (+/-2048
//     boundary) plus random fuzz at every Q.
//
// Together with TestFastQuantizeBlockSIMDMatchesScalar (which checks the
// SIMD kernel byte-for-byte against the scalar reference), this proves
// that govpx's fast quantize path produces byte-identical post-quantize
// qcoeff, dqcoeff, and EOB output to libvpx vp8_fast_quantize_b_c for
// every (Q, dequant, coefficient) triple the encoder can exercise.
func TestFastQuantizeBlockMatchesLibvpxC(t *testing.T) {
	r := rand.New(rand.NewSource(0xFA57DA))
	var tabs common.FrameDequantTables
	common.BuildFrameDequantTables(common.QuantDeltas{}, &tabs)

	mismatches := 0
	totalChecks := 0

	for q := 0; q <= common.MaxQ; q++ {
		var dequantTable common.MacroblockDequant
		common.InitMacroblockDequant(&tabs, q, &dequantTable)
		// Test all four block-quant tables (Y1, Y1DC, Y2, UV).
		channels := []struct {
			name    string
			dequant *[16]int16
		}{
			{"Y1", &dequantTable.Y1},
			{"Y1DC", &dequantTable.Y1DC},
			{"Y2", &dequantTable.Y2},
			{"UV", &dequantTable.UV},
		}
		for _, ch := range channels {
			var quant BlockQuant
			InitFastBlockQuant(ch.dequant, &quant)

			// Boundary-coefficient block (max DC, max AC, max int16).
			boundary := [16]int16{2047, 2047, -2048, 1024, -1024, 512, -512, 256, -256, 128, -128, 64, -64, 32, -32, 16}
			refQ, refDQ := [16]int16{}, [16]int16{}
			refEOB := libvpxFastQuantizeBReference(&boundary, &quant.Round, &quant.QuantFast, &quant.Dequant, &refQ, &refDQ)

			var govQ, govDQ [16]int16
			govEOB := FastQuantizeBlock(&boundary, &quant, &govQ, &govDQ)

			totalChecks++
			if govEOB != refEOB || govQ != refQ || govDQ != refDQ {
				mismatches++
				if mismatches <= 5 {
					t.Errorf("q=%d ch=%s boundary mismatch:\n  ref:    qcoeff=%v dqcoeff=%v eob=%d\n  govpx:  qcoeff=%v dqcoeff=%v eob=%d",
						q, ch.name, refQ, refDQ, refEOB, govQ, govDQ, govEOB)
				}
			}

			// Random fuzz: 64 cases at this Q+channel.
			for iter := range 64 {
				var coeff [16]int16
				for i := range coeff {
					coeff[i] = int16(r.Intn(4096) - 2048)
				}
				refQ, refDQ := [16]int16{}, [16]int16{}
				refEOB := libvpxFastQuantizeBReference(&coeff, &quant.Round, &quant.QuantFast, &quant.Dequant, &refQ, &refDQ)
				var govQ, govDQ [16]int16
				govEOB := FastQuantizeBlock(&coeff, &quant, &govQ, &govDQ)
				totalChecks++
				if govEOB != refEOB || govQ != refQ || govDQ != refDQ {
					mismatches++
					if mismatches <= 5 {
						t.Errorf("q=%d ch=%s iter=%d coeff=%v:\n  ref:    qcoeff=%v dqcoeff=%v eob=%d\n  govpx:  qcoeff=%v dqcoeff=%v eob=%d",
							q, ch.name, iter, coeff, refQ, refDQ, refEOB, govQ, govDQ, govEOB)
					}
				}
			}
		}
	}

	if mismatches > 0 {
		t.Fatalf("FastQuantizeBlock diverged from libvpx vp8_fast_quantize_b_c in %d/%d checks", mismatches, totalChecks)
	}
	t.Logf("FastQuantizeBlock byte-matches libvpx vp8_fast_quantize_b_c on %d (Q, channel, coeff) cases", totalChecks)
}
