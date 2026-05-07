package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

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

	for i := 0; i < 16; i++ {
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
