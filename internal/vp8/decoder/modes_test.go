package decoder

import (
	"testing"

	"github.com/thesyncim/gopvx/internal/vp8/boolcoder"
	"github.com/thesyncim/gopvx/internal/vp8/tables"
)

func TestResetModeProbs(t *testing.T) {
	var probs ModeProbs

	ResetModeProbs(&probs)

	if probs.YMode != tables.DefaultYModeProbs {
		t.Fatalf("YMode = %v, want default", probs.YMode)
	}
	if probs.UVMode != tables.DefaultUVModeProbs {
		t.Fatalf("UVMode = %v, want default", probs.UVMode)
	}
	if probs.BMode != tables.DefaultBModeProbs {
		t.Fatalf("BMode = %v, want default", probs.BMode)
	}
	if probs.MV != tables.DefaultMVContext {
		t.Fatalf("MV context = %v, want default", probs.MV)
	}
}

func TestParseModeHeaderKeyFrameSkipOff(t *testing.T) {
	var br boolcoder.Decoder
	if err := br.Init(make([]byte, 8)); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var probs ModeProbs
	ResetModeProbs(&probs)

	header := parseModeHeaderInto(&br, true, &probs)

	if header != (ModeHeader{}) {
		t.Fatalf("header = %+v, want zero keyframe mode header", header)
	}
	if probs.YMode != tables.DefaultYModeProbs || probs.UVMode != tables.DefaultUVModeProbs || probs.MV != tables.DefaultMVContext {
		t.Fatalf("keyframe mode header changed probs: %+v", probs)
	}
}

func TestParseModeHeaderInterUpdates(t *testing.T) {
	payload := encodeInterModeHeaderUpdates()
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var probs ModeProbs
	ResetModeProbs(&probs)

	header := parseModeHeaderInto(&br, false, &probs)

	if !header.MBNoCoeffSkip || header.ProbSkipFalse != 77 {
		t.Fatalf("skip header = %+v, want skip enabled prob 77", header)
	}
	if header.ProbIntra != 33 || header.ProbLast != 44 || header.ProbGolden != 55 {
		t.Fatalf("inter probs = %d/%d/%d, want 33/44/55", header.ProbIntra, header.ProbLast, header.ProbGolden)
	}
	if !header.YModeUpdated || !header.UVModeUpdated || header.MVUpdateCount != 2 {
		t.Fatalf("update flags/count = %+v, want y/uv and two MV updates", header)
	}
	if probs.YMode != ([tables.YModeProbCount]uint8{10, 20, 30, 40}) {
		t.Fatalf("YMode = %v, want updated", probs.YMode)
	}
	if probs.UVMode != ([tables.UVModeProbCount]uint8{50, 60, 70}) {
		t.Fatalf("UVMode = %v, want updated", probs.UVMode)
	}
	if probs.MV[0][0] != 126 {
		t.Fatalf("row MV prob[0] = %d, want 126", probs.MV[0][0])
	}
	if probs.MV[1][1] != 1 {
		t.Fatalf("col MV prob[1] = %d, want 1", probs.MV[1][1])
	}
	if probs.MV[0][1] != tables.DefaultMVContext[0][1] || probs.MV[1][0] != tables.DefaultMVContext[1][0] {
		t.Fatalf("neighbor MV probabilities changed: %v/%v", probs.MV[0][:2], probs.MV[1][:2])
	}
}

func TestParseModeHeaderAllocatesZero(t *testing.T) {
	payload := encodeInterModeHeaderUpdates()
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		var probs ModeProbs
		ResetModeProbs(&probs)
		_ = parseModeHeaderInto(&br, false, &probs)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeInterModeHeaderUpdates() []byte {
	var w testBoolWriter
	w.init()

	w.writeBool(1, 128)
	w.writeLiteral(77, 8)

	w.writeLiteral(33, 8)
	w.writeLiteral(44, 8)
	w.writeLiteral(55, 8)

	w.writeBool(1, 128)
	w.writeLiteral(10, 8)
	w.writeLiteral(20, 8)
	w.writeLiteral(30, 8)
	w.writeLiteral(40, 8)

	w.writeBool(1, 128)
	w.writeLiteral(50, 8)
	w.writeLiteral(60, 8)
	w.writeLiteral(70, 8)

	for component := 0; component < 2; component++ {
		for i := 0; i < tables.MVPCount; i++ {
			update := uint8(0)
			value := uint32(0)
			if component == 0 && i == 0 {
				update = 1
				value = 63
			}
			if component == 1 && i == 1 {
				update = 1
				value = 0
			}
			w.writeBool(update, tables.MVUpdateProbs[component][i])
			if update != 0 {
				w.writeLiteral(value, 7)
			}
		}
	}

	return w.finish()
}
