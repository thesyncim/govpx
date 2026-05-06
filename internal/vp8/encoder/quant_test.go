package encoder

import (
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/common"
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
