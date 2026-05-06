package decoder

import (
	"testing"

	"github.com/thesyncim/gopvx/internal/vp8/common"
)

func TestInitSegmentDequantsWithoutSegmentation(t *testing.T) {
	quant := QuantHeader{BaseQIndex: 20, Y1DCDelta: 1, Y2DCDelta: 2, Y2ACDelta: 3, UVDCDelta: 4, UVACDelta: 5}
	var tables common.FrameDequantTables
	var dequants [common.MaxMBSegments]common.MacroblockDequant

	InitSegmentDequants(quant, nil, &tables, &dequants)

	wantY1DC := int16(common.DCQuant(20, 1))
	wantY1AC := int16(common.ACYQuant(20))
	wantY2DC := int16(common.DC2Quant(20, 2))
	wantY2AC := int16(common.AC2Quant(20, 3))
	wantUVDC := int16(common.DCUVQuant(20, 4))
	wantUVAC := int16(common.ACUVQuant(20, 5))
	for segment := 0; segment < common.MaxMBSegments; segment++ {
		if dequants[segment].Y1[0] != wantY1DC || dequants[segment].Y1[1] != wantY1AC {
			t.Fatalf("segment %d Y1 dequant = %d/%d, want %d/%d", segment, dequants[segment].Y1[0], dequants[segment].Y1[1], wantY1DC, wantY1AC)
		}
		if dequants[segment].Y2[0] != wantY2DC || dequants[segment].Y2[1] != wantY2AC {
			t.Fatalf("segment %d Y2 dequant = %d/%d, want %d/%d", segment, dequants[segment].Y2[0], dequants[segment].Y2[1], wantY2DC, wantY2AC)
		}
		if dequants[segment].UV[0] != wantUVDC || dequants[segment].UV[1] != wantUVAC {
			t.Fatalf("segment %d UV dequant = %d/%d, want %d/%d", segment, dequants[segment].UV[0], dequants[segment].UV[1], wantUVDC, wantUVAC)
		}
	}
}

func TestInitSegmentDequantsWithDeltaSegmentation(t *testing.T) {
	quant := QuantHeader{BaseQIndex: 20}
	segmentation := SegmentationHeader{Enabled: true}
	segmentation.FeatureData[common.MBLvlAltQ] = [common.MaxMBSegments]int8{0, 5, -30, 120}
	var tables common.FrameDequantTables
	var dequants [common.MaxMBSegments]common.MacroblockDequant

	InitSegmentDequants(quant, &segmentation, &tables, &dequants)

	want := [common.MaxMBSegments]int{20, 25, 0, 127}
	for segment := 0; segment < common.MaxMBSegments; segment++ {
		if got := dequants[segment].Y1[0]; got != int16(common.DCQuant(want[segment], 0)) {
			t.Fatalf("segment %d Y1 DC = %d, want q=%d", segment, got, want[segment])
		}
	}
}

func TestInitSegmentDequantsWithAbsSegmentation(t *testing.T) {
	quant := QuantHeader{BaseQIndex: 20}
	segmentation := SegmentationHeader{Enabled: true, AbsDelta: true}
	segmentation.FeatureData[common.MBLvlAltQ] = [common.MaxMBSegments]int8{3, 6, 9, 12}
	var tables common.FrameDequantTables
	var dequants [common.MaxMBSegments]common.MacroblockDequant

	InitSegmentDequants(quant, &segmentation, &tables, &dequants)

	for segment := 0; segment < common.MaxMBSegments; segment++ {
		q := int(segmentation.FeatureData[common.MBLvlAltQ][segment])
		if got := dequants[segment].Y1[0]; got != int16(common.DCQuant(q, 0)) {
			t.Fatalf("segment %d Y1 DC = %d, want q=%d", segment, got, q)
		}
	}
}

func TestInitSegmentDequantsAllocatesZero(t *testing.T) {
	quant := QuantHeader{BaseQIndex: 20, Y1DCDelta: 1, Y2DCDelta: 2, Y2ACDelta: 3, UVDCDelta: 4, UVACDelta: 5}
	segmentation := SegmentationHeader{Enabled: true}
	segmentation.FeatureData[common.MBLvlAltQ] = [common.MaxMBSegments]int8{0, 5, -5, 10}
	var tables common.FrameDequantTables
	var dequants [common.MaxMBSegments]common.MacroblockDequant

	allocs := testing.AllocsPerRun(1000, func() {
		InitSegmentDequants(quant, &segmentation, &tables, &dequants)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}
