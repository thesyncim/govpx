package decoder

import (
	"testing"

	"github.com/thesyncim/gopvx/internal/vp8/boolcoder"
	"github.com/thesyncim/gopvx/internal/vp8/tables"
)

func TestDecodeMotionVectorSmall(t *testing.T) {
	probs := tables.DefaultMVContext
	payload := encodeMotionVector(t, &probs, mvComponent{value: 3, negative: true, large: false}, mvComponent{value: 0, large: false})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	mv := DecodeMotionVector(&br, &probs)

	if mv != (MotionVector{Row: -6, Col: 0}) {
		t.Fatalf("mv = %+v, want {-6,0}", mv)
	}
}

func TestDecodeMotionVectorLarge(t *testing.T) {
	probs := tables.DefaultMVContext
	payload := encodeMotionVector(t, &probs, mvComponent{value: 20, large: true}, mvComponent{value: 12, negative: true, large: true})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	mv := DecodeMotionVector(&br, &probs)

	if mv != (MotionVector{Row: 40, Col: -24}) {
		t.Fatalf("mv = %+v, want {40,-24}", mv)
	}
}

func TestDecodeMotionVectorAllocatesZero(t *testing.T) {
	probs := tables.DefaultMVContext
	payload := encodeMotionVector(t, &probs, mvComponent{value: 7, large: false}, mvComponent{value: 8, large: true})
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		_ = DecodeMotionVector(&br, &probs)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

type mvComponent struct {
	value    int
	negative bool
	large    bool
}

func encodeMotionVector(t *testing.T, probs *[2][tables.MVPCount]uint8, row mvComponent, col mvComponent) []byte {
	var w testBoolWriter
	w.init()
	writeMVComponent(t, &w, probs[0][:], row)
	writeMVComponent(t, &w, probs[1][:], col)
	return w.finish()
}

func writeMVComponent(t *testing.T, w *testBoolWriter, probs []uint8, component mvComponent) {
	if component.large {
		writeLargeMVComponent(t, w, probs, component)
		return
	}
	w.writeBool(0, probs[mvProbIsShort])
	writeTreeToken(t, w, tables.SmallMVTree[:], probs[mvProbShort:], component.value)
	if component.value != 0 {
		w.writeBool(boolToBit(component.negative), probs[mvProbSign])
	}
}

func writeLargeMVComponent(t *testing.T, w *testBoolWriter, probs []uint8, component mvComponent) {
	t.Helper()
	if component.value < 8 {
		t.Fatalf("large MV component value = %d, want >= 8", component.value)
	}
	w.writeBool(1, probs[mvProbIsShort])

	coded := component.value
	if component.value < 16 {
		coded = component.value - 8
	}
	for i := 0; i < 3; i++ {
		w.writeBool(uint8((coded>>i)&1), probs[mvProbBits+i])
	}
	for i := mvLongWidth - 1; i > 3; i-- {
		w.writeBool(uint8((coded>>i)&1), probs[mvProbBits+i])
	}
	if coded&0xfff0 != 0 {
		w.writeBool(uint8((component.value>>3)&1), probs[mvProbBits+3])
	}
	if component.value != 0 {
		w.writeBool(boolToBit(component.negative), probs[mvProbSign])
	}
}

func boolToBit(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}
