package common

import "testing"

func TestQuantLookupClampsAndCaps(t *testing.T) {
	if got := DCQuant(-40, 0); got != 4 {
		t.Fatalf("DCQuant low clamp = %d, want 4", got)
	}
	if got := DCQuant(120, 20); got != 157 {
		t.Fatalf("DCQuant high clamp = %d, want 157", got)
	}
	if got := DC2Quant(127, 0); got != 314 {
		t.Fatalf("DC2Quant = %d, want 314", got)
	}
	if got := DCUVQuant(127, 0); got != 132 {
		t.Fatalf("DCUVQuant cap = %d, want 132", got)
	}
	if got := ACYQuant(127); got != 284 {
		t.Fatalf("ACYQuant = %d, want 284", got)
	}
	if got := AC2Quant(0, -20); got != 8 {
		t.Fatalf("AC2Quant minimum = %d, want 8", got)
	}
	if got := AC2Quant(127, 0); got != 440 {
		t.Fatalf("AC2Quant high = %d, want 440", got)
	}
	if got := ACUVQuant(120, 20); got != 284 {
		t.Fatalf("ACUVQuant high clamp = %d, want 284", got)
	}
}

func TestPublicQuantizerTranslationTable(t *testing.T) {
	tests := []struct {
		public int
		qIndex int
	}{
		{public: 0, qIndex: 0},
		{public: 4, qIndex: 4},
		{public: 10, qIndex: 12},
		{public: 32, qIndex: 43},
		{public: 36, qIndex: 51},
		{public: 56, qIndex: 106},
		{public: 63, qIndex: 127},
	}
	for _, tt := range tests {
		if got := PublicQuantizerToQIndex(tt.public); got != tt.qIndex {
			t.Fatalf("public q %d maps to qindex %d, want %d", tt.public, got, tt.qIndex)
		}
		if got := QIndexToPublicQuantizer(tt.qIndex); got != tt.public {
			t.Fatalf("qindex %d maps to public q %d, want %d", tt.qIndex, got, tt.public)
		}
	}
}

func TestPublicQuantizerTranslationClampsBounds(t *testing.T) {
	if got := PublicQuantizerToQIndex(-1); got != 0 {
		t.Fatalf("low public q maps to qindex %d, want 0", got)
	}
	if got := PublicQuantizerToQIndex(64); got != MaxQ {
		t.Fatalf("high public q maps to qindex %d, want %d", got, MaxQ)
	}
	if got := QIndexToPublicQuantizer(-1); got != 0 {
		t.Fatalf("low qindex maps to public q %d, want 0", got)
	}
	if got := QIndexToPublicQuantizer(MaxQ + 1); got != maxPublicQuantizer {
		t.Fatalf("high qindex maps to public q %d, want %d", got, maxPublicQuantizer)
	}
}

func TestBuildFrameDequantTables(t *testing.T) {
	var tables FrameDequantTables
	BuildFrameDequantTables(QuantDeltas{
		Y1DC: 2,
		Y2DC: -1,
		Y2AC: 4,
		UVDC: 30,
		UVAC: -3,
	}, &tables)

	if got := tables.Y1[0][0]; got != int16(DCQuant(0, 2)) {
		t.Fatalf("Y1[0][0] = %d", got)
	}
	if got := tables.Y1[0][1]; got != int16(ACYQuant(0)) {
		t.Fatalf("Y1[0][1] = %d", got)
	}
	if got := tables.Y2[10][0]; got != int16(DC2Quant(10, -1)) {
		t.Fatalf("Y2[10][0] = %d", got)
	}
	if got := tables.Y2[10][1]; got != int16(AC2Quant(10, 4)) {
		t.Fatalf("Y2[10][1] = %d", got)
	}
	if got := tables.UV[127][0]; got != 132 {
		t.Fatalf("UV[127][0] = %d, want capped 132", got)
	}
	if got := tables.UV[10][1]; got != int16(ACUVQuant(10, -3)) {
		t.Fatalf("UV[10][1] = %d", got)
	}
}

func TestInitMacroblockDequant(t *testing.T) {
	var tables FrameDequantTables
	var mb MacroblockDequant
	BuildFrameDequantTables(QuantDeltas{Y1DC: 1, Y2DC: 2, Y2AC: 3, UVDC: 4, UVAC: 5}, &tables)

	InitMacroblockDequant(&tables, 20, &mb)

	if mb.Y1DC[0] != 1 {
		t.Fatalf("Y1DC[0] = %d, want 1", mb.Y1DC[0])
	}
	if mb.Y1[0] != tables.Y1[20][0] || mb.Y2[0] != tables.Y2[20][0] || mb.UV[0] != tables.UV[20][0] {
		t.Fatalf("DC dequant mismatch: Y1=%d Y2=%d UV=%d", mb.Y1[0], mb.Y2[0], mb.UV[0])
	}
	for i := 1; i < 16; i++ {
		if mb.Y1DC[i] != tables.Y1[20][1] || mb.Y1[i] != tables.Y1[20][1] {
			t.Fatalf("Y1 AC dequant[%d] mismatch", i)
		}
		if mb.Y2[i] != tables.Y2[20][1] || mb.UV[i] != tables.UV[20][1] {
			t.Fatalf("Y2/UV AC dequant[%d] mismatch", i)
		}
	}
}

func TestQuantTablesAllocateZero(t *testing.T) {
	var tables FrameDequantTables
	var mb MacroblockDequant
	allocs := testing.AllocsPerRun(1000, func() {
		BuildFrameDequantTables(QuantDeltas{Y1DC: 1, Y2DC: 2, Y2AC: 3, UVDC: 4, UVAC: 5}, &tables)
		InitMacroblockDequant(&tables, 20, &mb)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkBuildFrameDequantTables(b *testing.B) {
	var tables FrameDequantTables
	deltas := QuantDeltas{Y1DC: 1, Y2DC: 2, Y2AC: 3, UVDC: 4, UVAC: 5}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		BuildFrameDequantTables(deltas, &tables)
	}
}
