package decoder

import (
	"testing"

	"github.com/thesyncim/gopvx/internal/vp8/boolcoder"
)

func TestParseLoopFilterHeaderCarriesPreviousDeltasWithoutUpdate(t *testing.T) {
	previous := LoopFilterHeader{
		RefDeltas:  [4]int8{1, -2, 3, -4},
		ModeDeltas: [4]int8{5, -6, 7, -8},
	}
	payload := encodeLoopFilterHeaderNoDeltaUpdate(11, 2, true)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	header := parseLoopFilterHeaderWithPrevious(&br, previous)

	if header.Type != NormalLoopFilter || header.Level != 11 || header.SharpnessLevel != 2 {
		t.Fatalf("loop header basics = %+v, want normal level 11 sharpness 2", header)
	}
	if !header.DeltaEnabled || header.DeltaUpdate {
		t.Fatalf("delta flags = enabled:%t update:%t, want enabled without update", header.DeltaEnabled, header.DeltaUpdate)
	}
	if header.RefDeltas != previous.RefDeltas || header.ModeDeltas != previous.ModeDeltas {
		t.Fatalf("deltas = %v/%v, want previous %v/%v", header.RefDeltas, header.ModeDeltas, previous.RefDeltas, previous.ModeDeltas)
	}
}

func TestParseLoopFilterHeaderAppliesPartialDeltaUpdates(t *testing.T) {
	previous := LoopFilterHeader{
		RefDeltas:  [4]int8{1, -2, 3, -4},
		ModeDeltas: [4]int8{5, -6, 7, -8},
	}
	payload := encodeLoopFilterHeaderPartialDeltaUpdate()
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	header := parseLoopFilterHeaderWithPrevious(&br, previous)

	if !header.DeltaEnabled || !header.DeltaUpdate {
		t.Fatalf("delta flags = enabled:%t update:%t, want enabled update", header.DeltaEnabled, header.DeltaUpdate)
	}
	if header.RefDeltas != ([4]int8{-5, -2, 3, -4}) {
		t.Fatalf("ref deltas = %v, want partial update", header.RefDeltas)
	}
	if header.ModeDeltas != ([4]int8{5, 7, 7, -8}) {
		t.Fatalf("mode deltas = %v, want partial update", header.ModeDeltas)
	}
}

func TestParseLoopFilterHeaderAllocatesZero(t *testing.T) {
	payload := encodeLoopFilterHeaderPartialDeltaUpdate()
	previous := LoopFilterHeader{RefDeltas: [4]int8{1, -2, 3, -4}, ModeDeltas: [4]int8{5, -6, 7, -8}}

	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		_ = parseLoopFilterHeaderWithPrevious(&br, previous)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeLoopFilterHeaderNoDeltaUpdate(level uint8, sharpness uint8, enabled bool) []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeLiteral(uint32(level), 6)
	w.writeLiteral(uint32(sharpness), 3)
	if enabled {
		w.writeBool(1, 128)
		w.writeBool(0, 128)
	} else {
		w.writeBool(0, 128)
	}
	return w.finish()
}

func encodeLoopFilterHeaderPartialDeltaUpdate() []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(1, 128)
	w.writeLiteral(20, 6)
	w.writeLiteral(3, 3)
	w.writeBool(1, 128)
	w.writeBool(1, 128)

	for i := 0; i < 4; i++ {
		if i == 0 {
			w.writeBool(1, 128)
			w.writeLiteral(5, 6)
			w.writeBool(1, 128)
		} else {
			w.writeBool(0, 128)
		}
	}
	for i := 0; i < 4; i++ {
		if i == 1 {
			w.writeBool(1, 128)
			w.writeLiteral(7, 6)
			w.writeBool(0, 128)
		} else {
			w.writeBool(0, 128)
		}
	}
	return w.finish()
}
