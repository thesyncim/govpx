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
	for i := 0; i < 3; i++ {
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
