package encoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestWriteMotionVectorRoundTripsSmall(t *testing.T) {
	probs := tables.DefaultMVContext
	payload := motionVectorPayload(t, &probs, MotionVector{Row: -6, Col: 0})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	got := vp8dec.DecodeMotionVector(&br, &probs)

	if got != (vp8dec.MotionVector{Row: -6, Col: 0}) {
		t.Fatalf("mv = %+v, want {-6,0}", got)
	}
}

func TestWriteMotionVectorRoundTripsLarge(t *testing.T) {
	probs := tables.DefaultMVContext
	payload := motionVectorPayload(t, &probs, MotionVector{Row: 40, Col: -24})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	got := vp8dec.DecodeMotionVector(&br, &probs)

	if got != (vp8dec.MotionVector{Row: 40, Col: -24}) {
		t.Fatalf("mv = %+v, want {40,-24}", got)
	}
}

func TestWriteMotionVectorRejectsOddComponent(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 64))
	probs := tables.DefaultMVContext

	err := WriteMotionVector(&w, &probs, MotionVector{Row: 1})

	if !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("error = %v, want ErrInvalidPacketConfig", err)
	}
}

func TestMotionVectorBitCostMatchesLibvpxComponentCost(t *testing.T) {
	probs := tables.DefaultMVContext
	mv := MotionVector{Row: -5, Col: 17}
	want := testMVComponentCost(0, probs[0][:]) + testMVComponentCost(8, probs[1][:])

	if got := MotionVectorBitCost(mv, MotionVector{}, &probs, 128); got != want {
		t.Fatalf("MotionVectorBitCost = %d, want %d", got, want)
	}
	if got := MotionVectorCost(mv); got != want {
		t.Fatalf("MotionVectorCost = %d, want %d", got, want)
	}
}

func TestMotionVectorBitCostUsesReferenceAndWeight(t *testing.T) {
	probs := tables.DefaultMVContext
	mv := MotionVector{Row: 40, Col: -24}
	ref := MotionVector{Row: 8, Col: -8}
	base := testMVComponentCost(16, probs[0][:]) + testMVComponentCost(0, probs[1][:])
	want := (base * 96) >> 7

	if got := MotionVectorBitCost(mv, ref, &probs, 96); got != want {
		t.Fatalf("MotionVectorBitCost weighted = %d, want %d", got, want)
	}
}

func TestMotionVectorErrorCostMatchesLibvpxMCompScaling(t *testing.T) {
	probs := tables.DefaultMVContext
	mv := MotionVector{Row: 40, Col: -24}
	ref := MotionVector{Row: 8, Col: -8}
	base := testMVComponentCost(16, probs[0][:]) + testMVComponentCost(0, probs[1][:])
	want := (base*34 + 128) >> 8

	if got := MotionVectorErrorCost(mv, ref, &probs, 34); got != want {
		t.Fatalf("MotionVectorErrorCost = %d, want %d", got, want)
	}
	if got := MotionVectorErrorCost(mv, ref, nil, 34); got != 0 {
		t.Fatalf("nil MotionVectorErrorCost = %d, want 0", got)
	}
}

func TestMotionVectorBitCostClampsLikeLibvpxCostTable(t *testing.T) {
	probs := tables.DefaultMVContext
	want := testMVComponentCost(1023, probs[0][:]) + testMVComponentCost(0, probs[1][:])

	if got := MotionVectorBitCost(MotionVector{Row: 4096, Col: -4096}, MotionVector{}, &probs, 128); got != want {
		t.Fatalf("clamped MotionVectorBitCost = %d, want %d", got, want)
	}
}

func TestMotionVectorSADCostMatchesLibvpxTable(t *testing.T) {
	if got := MotionVectorSADCost(MotionVector{}, MotionVector{}, 2); got != 5 {
		t.Fatalf("zero MotionVectorSADCost = %d, want 5", got)
	}
	if got := MotionVectorSADCost(MotionVector{Row: 8, Col: -64}, MotionVector{}, 3); got != 61 {
		t.Fatalf("MotionVectorSADCost = %d, want 61", got)
	}
	if got := MotionVectorSADCost(MotionVector{Row: 4096}, MotionVector{}, 14); got != 341 {
		t.Fatalf("clamped MotionVectorSADCost = %d, want 341", got)
	}
}

func TestMotionVectorSubpelSearchCostMatchesLibvpxMVC(t *testing.T) {
	probs := tables.DefaultMVContext
	tests := []struct {
		mv  MotionVector
		ref MotionVector
	}{
		{mv: MotionVector{}, ref: MotionVector{}},
		{mv: MotionVector{Row: 6, Col: -2}, ref: MotionVector{Row: 2, Col: 4}},
		{mv: MotionVector{Row: -14, Col: 18}, ref: MotionVector{Row: -8, Col: 6}},
		{mv: MotionVector{Row: 4096, Col: -4096}, ref: MotionVector{}},
	}
	for _, tt := range tests {
		row := clampMVSignedComponent((int(tt.mv.Row) >> 1) - (int(tt.ref.Row) >> 1))
		col := clampMVSignedComponent((int(tt.mv.Col) >> 1) - (int(tt.ref.Col) >> 1))
		want := (testMVComponentCost(row, probs[0][:]) + testMVComponentCost(col, probs[1][:])) * 34
		want = (want + 128) >> 8
		got := MotionVectorSubpelSearchCost(tt.mv, tt.ref, &probs, 34)
		if got != want {
			t.Fatalf("mv=%+v ref=%+v subpel cost = %d, want %d", tt.mv, tt.ref, got, want)
		}
	}
}

func TestMotionVectorCostTablesSubpelSearchCostMatchesLibvpxMVC(t *testing.T) {
	probs := tables.DefaultMVContext
	probs[0][mvProbIsShort] = 99
	probs[1][mvProbSign] = 111
	var costs MotionVectorCostTables
	costs.Build(&probs)

	tests := []struct {
		mvRow4  int
		mvCol4  int
		refRow4 int
		refCol4 int
	}{
		{0, 0, 0, 0},
		{3, -1, 1, 2},
		{-7, 9, -4, 3},
		{2048, -2048, 0, 0},
	}
	for _, tt := range tests {
		got := costs.SubpelSearchCostFromQuarterDeltas(tt.mvRow4, tt.mvCol4, tt.refRow4, tt.refCol4, 34)
		row := clampMVSignedComponent(tt.mvRow4 - tt.refRow4)
		col := clampMVSignedComponent(tt.mvCol4 - tt.refCol4)
		want := (testMVComponentCost(row, probs[0][:]) + testMVComponentCost(col, probs[1][:])) * 34
		want = (want + 128) >> 8
		if got != want {
			t.Fatalf("mv=(%d,%d) ref=(%d,%d) table subpel cost = %d, want %d", tt.mvRow4, tt.mvCol4, tt.refRow4, tt.refCol4, got, want)
		}
	}
}

func TestMotionVectorCostTablesErrorCostMatchesLibvpxMCompCenter(t *testing.T) {
	probs := tables.DefaultMVContext
	probs[0][mvProbIsShort] = 99
	probs[1][mvProbSign] = 111
	var costs MotionVectorCostTables
	costs.Build(&probs)

	tests := []struct {
		mv  MotionVector
		ref MotionVector
	}{
		{MotionVector{}, MotionVector{}},
		{MotionVector{Row: 8, Col: 0}, MotionVector{Row: 14, Col: 2}},
		{MotionVector{Row: 14, Col: -6}, MotionVector{Row: 8, Col: -8}},
		{MotionVector{Row: -16, Col: 24}, MotionVector{Row: 4, Col: -2}},
	}
	for _, tt := range tests {
		got := costs.ErrorCostFromEighthDeltas(int(tt.mv.Row), int(tt.mv.Col), int(tt.ref.Row), int(tt.ref.Col), 34)
		want := MotionVectorErrorCost(tt.mv, tt.ref, &probs, 34)
		if got != want {
			t.Fatalf("mv=%+v ref=%+v table error cost = %d, want %d", tt.mv, tt.ref, got, want)
		}
	}
}

func TestWriteMotionVectorAllocatesZero(t *testing.T) {
	buf := make([]byte, 64)
	probs := tables.DefaultMVContext
	var w BoolWriter
	allocs := testing.AllocsPerRun(1000, func() {
		w.Init(buf)
		_ = WriteMotionVector(&w, &probs, MotionVector{Row: -6, Col: 16})
		w.Finish()
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func motionVectorPayload(t *testing.T, probs *[2][tables.MVPCount]uint8, mv MotionVector) []byte {
	t.Helper()
	var w BoolWriter
	buf := make([]byte, 64)
	w.Init(buf)
	if err := WriteMotionVector(&w, probs, mv); err != nil {
		t.Fatalf("WriteMotionVector returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v", err)
	}
	return w.Bytes()
}

func testMVComponentCost(component int, probs []uint8) int {
	negative := component < 0
	x := component
	if negative {
		x = -x
	}
	if x < mvNumShort {
		cost := testMVBoolCost(probs[mvProbIsShort], 0)
		cost += testMVTreeTokenCost(tables.SmallMVTree[:], probs[mvProbShort:], smallMVTokens[x])
		if x != 0 {
			cost += testMVSignCost(probs[mvProbSign], negative)
		}
		return cost
	}

	cost := testMVBoolCost(probs[mvProbIsShort], 1)
	for i := range 3 {
		cost += testMVBoolCost(probs[mvProbBits+i], (x>>i)&1)
	}
	for i := mvLongWidth - 1; i > 3; i-- {
		cost += testMVBoolCost(probs[mvProbBits+i], (x>>i)&1)
	}
	if x&0xfff0 != 0 {
		cost += testMVBoolCost(probs[mvProbBits+3], (x>>3)&1)
	}
	cost += testMVSignCost(probs[mvProbSign], negative)
	return cost
}

func testMVSignCost(prob uint8, negative bool) int {
	if negative {
		return testMVBoolCost(prob, 1)
	}
	return testMVBoolCost(prob, 0)
}

func testMVBoolCost(prob uint8, bit int) int {
	if bit == 0 {
		return tables.ProbCost[prob]
	}
	return tables.ProbCost[255-int(prob)]
}

func BenchmarkMotionVectorCostTablesSubpelSearchCost(b *testing.B) {
	probs := tables.DefaultMVContext
	var costs MotionVectorCostTables
	costs.Build(&probs)
	rows := [...]int{0, 1, -2, 7, -11, 31, -64, 127}
	cols := [...]int{0, -1, 3, -9, 18, -45, 80, -126}
	refRows := [...]int{0, 2, -4, 8}
	refCols := [...]int{0, -3, 5, -7}

	total := 0
	for i := 0; i < b.N; i++ {
		total += costs.SubpelSearchCostFromQuarterDeltas(rows[i&7], cols[i&7], refRows[i&3], refCols[i&3], 34)
	}
	if total == 42 {
		b.Fatal(total)
	}
}

func testMVTreeTokenCost(tree []int16, probs []uint8, token TreeToken) int {
	node := int16(0)
	cost := 0
	for bitIndex := int(token.Len) - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		bit := int((token.Value >> uint(bitIndex)) & 1)
		cost += testMVBoolCost(probs[probIndex], bit)
		next := tree[int(node)+bit]
		if next <= 0 {
			return cost
		}
		node = next
	}
	return cost
}
