package decoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

func TestDecodeInterMacroblockIntra(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	payload := encodeInterMacroblockIntra(t, header, &probs)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	if err := DecodeInterMacroblock(&br, nil, header, &probs, nil, nil, nil, [common.MaxRefFrames]bool{}, &out); err != nil {
		t.Fatalf("DecodeInterMacroblock returned error: %v", err)
	}
	if out.RefFrame != common.IntraFrame || out.Mode != common.DCPred || out.UVMode != common.HPred {
		t.Fatalf("mode = %+v, want intra DC/H", out)
	}
}

func TestDecodeInterMacroblockZeroMV(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	payload := encodeInterMacroblockInter(t, header, &probs, nil, nil, nil, common.LastFrame, common.ZeroMV, mvComponent{})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	if err := DecodeInterMacroblock(&br, nil, header, &probs, nil, nil, nil, [common.MaxRefFrames]bool{}, &out); err != nil {
		t.Fatalf("DecodeInterMacroblock returned error: %v", err)
	}
	if out.RefFrame != common.LastFrame || out.Mode != common.ZeroMV || !out.MV.IsZero() {
		t.Fatalf("mode = %+v, want LAST/ZEROMV zero vector", out)
	}
}

func TestDecodeInterMacroblockNewMV(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	above := MacroblockMode{RefFrame: common.LastFrame, MV: MotionVector{Row: 4, Col: 2}}
	payload := encodeInterMacroblockInter(t, header, &probs, &above, nil, nil, common.LastFrame, common.NewMV, mvComponent{value: 3})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	if err := DecodeInterMacroblock(&br, nil, header, &probs, &above, nil, nil, [common.MaxRefFrames]bool{}, &out); err != nil {
		t.Fatalf("DecodeInterMacroblock returned error: %v", err)
	}
	if out.RefFrame != common.LastFrame || out.Mode != common.NewMV || out.MV != (MotionVector{Row: 10, Col: 2}) {
		t.Fatalf("mode = %+v, want LAST/NEWMV {10,2}", out)
	}
}

func TestDecodeInterMacroblockRejectsSplitMV(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	payload := encodeInterMacroblockInter(t, header, &probs, nil, nil, nil, common.LastFrame, common.SplitMV, mvComponent{})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	err := DecodeInterMacroblock(&br, nil, header, &probs, nil, nil, nil, [common.MaxRefFrames]bool{}, &out)

	if !errors.Is(err, ErrUnsupportedInterMode) {
		t.Fatalf("error = %v, want ErrUnsupportedInterMode", err)
	}
	if out.Mode != common.SplitMV || !out.Is4x4 {
		t.Fatalf("mode = %+v, want SplitMV 4x4 marker", out)
	}
}

func TestDecodeInterMacroblockAllocatesZero(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	payload := encodeInterMacroblockInter(t, header, &probs, nil, nil, nil, common.LastFrame, common.ZeroMV, mvComponent{})
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		var out MacroblockMode
		_ = DecodeInterMacroblock(&br, nil, header, &probs, nil, nil, nil, [common.MaxRefFrames]bool{}, &out)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeInterMacroblockIntra(t *testing.T, header ModeHeader, probs *ModeProbs) []byte {
	var w testBoolWriter
	w.init()
	writeInterReference(&w, header, common.IntraFrame)
	writeTreeToken(t, &w, tables.YModeTree[:], probs.YMode[:], int(common.DCPred))
	writeTreeToken(t, &w, tables.UVModeTree[:], probs.UVMode[:], int(common.HPred))
	return w.finish()
}

func encodeInterMacroblockInter(t *testing.T, header ModeHeader, probs *ModeProbs, above *MacroblockMode, left *MacroblockMode, aboveLeft *MacroblockMode, refFrame common.MVReferenceFrame, mode common.MBPredictionMode, rowDelta mvComponent) []byte {
	var w testBoolWriter
	w.init()
	writeInterReference(&w, header, refFrame)
	_, _, _, counts := FindNearMotionVectors(above, left, aboveLeft, refFrame, [common.MaxRefFrames]bool{})
	writeInterPredictionMode(&w, counts, mode)
	if mode == common.NewMV {
		writeMVComponent(t, &w, probs.MV[0][:], rowDelta)
		writeMVComponent(t, &w, probs.MV[1][:], mvComponent{})
	}
	return w.finish()
}

func writeInterReference(w *testBoolWriter, header ModeHeader, refFrame common.MVReferenceFrame) {
	if refFrame == common.IntraFrame {
		w.writeBool(0, header.ProbIntra)
		return
	}
	w.writeBool(1, header.ProbIntra)
	if refFrame == common.LastFrame {
		w.writeBool(0, header.ProbLast)
		return
	}
	w.writeBool(1, header.ProbLast)
	if refFrame == common.GoldenFrame {
		w.writeBool(0, header.ProbGolden)
		return
	}
	w.writeBool(1, header.ProbGolden)
}

func writeInterPredictionMode(w *testBoolWriter, counts InterModeCounts, mode common.MBPredictionMode) {
	switch mode {
	case common.ZeroMV:
		w.writeBool(0, tables.InterModeContexts[counts.Intra][0])
	case common.NearestMV:
		w.writeBool(1, tables.InterModeContexts[counts.Intra][0])
		w.writeBool(0, tables.InterModeContexts[counts.Nearest][1])
	case common.NearMV:
		w.writeBool(1, tables.InterModeContexts[counts.Intra][0])
		w.writeBool(1, tables.InterModeContexts[counts.Nearest][1])
		w.writeBool(0, tables.InterModeContexts[counts.Near][2])
	case common.NewMV:
		w.writeBool(1, tables.InterModeContexts[counts.Intra][0])
		w.writeBool(1, tables.InterModeContexts[counts.Nearest][1])
		w.writeBool(1, tables.InterModeContexts[counts.Near][2])
		w.writeBool(0, tables.InterModeContexts[counts.Split][3])
	case common.SplitMV:
		w.writeBool(1, tables.InterModeContexts[counts.Intra][0])
		w.writeBool(1, tables.InterModeContexts[counts.Nearest][1])
		w.writeBool(1, tables.InterModeContexts[counts.Near][2])
		w.writeBool(1, tables.InterModeContexts[counts.Split][3])
	}
}
