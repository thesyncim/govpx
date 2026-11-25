package decoder

import (
	"errors"
	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestDecodeInterIntraMacroblockModeDC(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	payload := encodeInterIntraMacroblockMode(t, &probs, common.DCPred, common.HPred, common.BDCPred)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	DecodeInterIntraMacroblockMode(&br, &probs, &out)

	if out.RefFrame != common.IntraFrame || out.Mode != common.DCPred || out.UVMode != common.HPred || out.Is4x4 {
		t.Fatalf("mode = %+v, want intra DC/H non-4x4", out)
	}
}

func TestDecodeInterIntraMacroblockModeBPred(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	payload := encodeInterIntraMacroblockMode(t, &probs, common.BPred, common.VPred, common.BHEPred)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	DecodeInterIntraMacroblockMode(&br, &probs, &out)

	if out.Mode != common.BPred || out.UVMode != common.VPred || !out.Is4x4 {
		t.Fatalf("mode = %+v, want B_PRED/V 4x4", out)
	}
	for i, mode := range out.BModes {
		if mode != common.BHEPred {
			t.Fatalf("BMode[%d] = %d, want B_HE_PRED", i, mode)
		}
	}
}

func TestDecodeInterIntraMacroblockModeAllocatesZero(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	payload := encodeInterIntraMacroblockMode(t, &probs, common.BPred, common.VPred, common.BDCPred)
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		var out MacroblockMode
		DecodeInterIntraMacroblockMode(&br, &probs, &out)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeInterIntraMacroblockMode(t *testing.T, probs *ModeProbs, yMode common.MBPredictionMode, uvMode common.MBPredictionMode, bMode common.BPredictionMode) []byte {
	var w testBoolWriter
	w.init()
	writeTreeToken(t, &w, tables.YModeTree[:], probs.YMode[:], int(yMode))
	if yMode == common.BPred {
		for range 16 {
			writeTreeToken(t, &w, tables.BModeTree[:], probs.BMode[:], int(bMode))
		}
	}
	writeTreeToken(t, &w, tables.UVModeTree[:], probs.UVMode[:], int(uvMode))
	return w.finish()
}

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

func TestDecodeInterMacroblockPreservesSegmentWithoutMapUpdate(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	payload := encodeInterMacroblockInter(t, header, &probs, nil, nil, nil, common.LastFrame, common.ZeroMV, mvComponent{})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	segmentation := SegmentationHeader{Enabled: true}
	out := MacroblockMode{SegmentID: 2}

	if err := DecodeInterMacroblock(&br, &segmentation, header, &probs, nil, nil, nil, [common.MaxRefFrames]bool{}, &out); err != nil {
		t.Fatalf("DecodeInterMacroblock returned error: %v", err)
	}
	if out.SegmentID != 2 || out.RefFrame != common.LastFrame || out.Mode != common.ZeroMV {
		t.Fatalf("mode = %+v, want preserved segment 2 LAST/ZEROMV", out)
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

func TestDecodeInterMacroblockAtClampsNearestMV(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	above := MacroblockMode{RefFrame: common.LastFrame, MV: MotionVector{Col: 136}}
	payload := encodeInterMacroblockInter(t, header, &probs, &above, nil, nil, common.LastFrame, common.NearestMV, mvComponent{})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	if err := decodeInterMacroblockAt(&br, 1, 2, 2, 3, nil, header, &probs, &above, nil, nil, [common.MaxRefFrames]bool{}, &out); err != nil {
		t.Fatalf("DecodeInterMacroblock returned error: %v", err)
	}
	if out.RefFrame != common.LastFrame || out.Mode != common.NearestMV || out.MV != (MotionVector{Col: 128}) {
		t.Fatalf("mode = %+v, want LAST/NEARESTMV {0,128}", out)
	}
}

func TestDecodeInterMacroblockAtClampsNewMVBestPredictor(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	above := MacroblockMode{RefFrame: common.LastFrame, MV: MotionVector{Col: 136}}
	payload := encodeInterMacroblockInterMV(t, header, &probs, &above, nil, nil, common.LastFrame, common.NewMV, mvComponent{}, mvComponent{value: 67, negative: true, large: true})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	if err := decodeInterMacroblockAt(&br, 1, 2, 2, 3, nil, header, &probs, &above, nil, nil, [common.MaxRefFrames]bool{}, &out); err != nil {
		t.Fatalf("DecodeInterMacroblock returned error: %v", err)
	}
	if out.RefFrame != common.LastFrame || out.Mode != common.NewMV || out.MV != (MotionVector{Col: -6}) {
		t.Fatalf("mode = %+v, want LAST/NEWMV {0,-6}", out)
	}
}

func TestDecodeInterMacroblockSplitMV(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	payload := encodeInterMacroblockInter(t, header, &probs, nil, nil, nil, common.LastFrame, common.SplitMV, mvComponent{})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	var out MacroblockMode

	if err := DecodeInterMacroblock(&br, nil, header, &probs, nil, nil, nil, [common.MaxRefFrames]bool{}, &out); err != nil {
		t.Fatalf("DecodeInterMacroblock returned error: %v", err)
	}
	if out.Mode != common.SplitMV || !out.Is4x4 || out.Partition != 3 || !out.MV.IsZero() {
		t.Fatalf("mode = %+v, want SplitMV 4x4 partition 3 zero MV", out)
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
	return encodeInterMacroblockInterMV(t, header, probs, above, left, aboveLeft, refFrame, mode, rowDelta, mvComponent{})
}

func encodeInterMacroblockInterMV(t *testing.T, header ModeHeader, probs *ModeProbs, above *MacroblockMode, left *MacroblockMode, aboveLeft *MacroblockMode, refFrame common.MVReferenceFrame, mode common.MBPredictionMode, rowDelta mvComponent, colDelta mvComponent) []byte {
	var w testBoolWriter
	w.init()
	writeInterReference(&w, header, refFrame)
	_, _, _, counts := FindNearMotionVectors(above, left, aboveLeft, refFrame, [common.MaxRefFrames]bool{})
	writeInterPredictionMode(&w, counts, mode)
	if mode == common.NewMV {
		writeMVComponent(t, &w, probs.MV[0][:], rowDelta)
		writeMVComponent(t, &w, probs.MV[1][:], colDelta)
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

func TestDecodeInterModeGrid(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	payload := encodeInterModeGrid(t, 2, 2, header, common.ZeroMV)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	modes := make([]MacroblockMode, 4)

	if err := DecodeInterModeGrid(&br, 2, 2, nil, header, &probs, [common.MaxRefFrames]bool{}, modes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}

	for i, mode := range modes {
		if mode.RefFrame != common.LastFrame || mode.Mode != common.ZeroMV || !mode.MV.IsZero() {
			t.Fatalf("mode[%d] = %+v, want LAST/ZEROMV", i, mode)
		}
	}
}

func TestDecodeInterModeGridRejectsSmallBuffer(t *testing.T) {
	var br boolcoder.Decoder
	_ = br.Init(make([]byte, 8))
	var probs ModeProbs
	ResetModeProbs(&probs)

	err := DecodeInterModeGrid(&br, 2, 2, nil, ModeHeader{}, &probs, [common.MaxRefFrames]bool{}, make([]MacroblockMode, 3))

	if !errors.Is(err, ErrModeBufferTooSmall) {
		t.Fatalf("error = %v, want ErrModeBufferTooSmall", err)
	}
}

func TestDecodeInterModeGridDecodesSplitMV(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	payload := encodeInterModeGrid(t, 1, 1, header, common.SplitMV)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	modes := make([]MacroblockMode, 1)

	if err := DecodeInterModeGrid(&br, 1, 1, nil, header, &probs, [common.MaxRefFrames]bool{}, modes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	if modes[0].Mode != common.SplitMV || !modes[0].Is4x4 {
		t.Fatalf("mode = %+v, want split MV", modes[0])
	}
}

func TestDecodeInterModeGridAllocatesZero(t *testing.T) {
	var probs ModeProbs
	ResetModeProbs(&probs)
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	payload := encodeInterModeGrid(t, 2, 2, header, common.ZeroMV)
	modes := make([]MacroblockMode, 4)
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		_ = DecodeInterModeGrid(&br, 2, 2, nil, header, &probs, [common.MaxRefFrames]bool{}, modes)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeInterModeGrid(t *testing.T, rows int, cols int, header ModeHeader, mode common.MBPredictionMode) []byte {
	var w testBoolWriter
	w.init()
	modes := make([]MacroblockMode, rows*cols)
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			var above *MacroblockMode
			var left *MacroblockMode
			var aboveLeft *MacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			if row > 0 && col > 0 {
				aboveLeft = &modes[index-cols-1]
			}

			writeInterReference(&w, header, common.LastFrame)
			_, _, _, counts := FindNearMotionVectors(above, left, aboveLeft, common.LastFrame, [common.MaxRefFrames]bool{})
			writeInterPredictionMode(&w, counts, mode)
			modes[index] = MacroblockMode{RefFrame: common.LastFrame, Mode: mode}
		}
	}
	return w.finish()
}

func TestReadInterPredictionMode(t *testing.T) {
	counts := InterModeCounts{Intra: 0, Nearest: 1, Near: 2, Split: 3}
	tests := []struct {
		name string
		bits []uint8
		want common.MBPredictionMode
	}{
		{name: "zero", bits: []uint8{0}, want: common.ZeroMV},
		{name: "nearest", bits: []uint8{1, 0}, want: common.NearestMV},
		{name: "near", bits: []uint8{1, 1, 0}, want: common.NearMV},
		{name: "new", bits: []uint8{1, 1, 1, 0}, want: common.NewMV},
		{name: "split", bits: []uint8{1, 1, 1, 1}, want: common.SplitMV},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var br boolcoder.Decoder
			if err := br.Init(encodeInterModeBits(counts, tt.bits)); err != nil {
				t.Fatalf("Init returned error: %v", err)
			}

			got := ReadInterPredictionMode(&br, counts)

			if got != tt.want {
				t.Fatalf("mode = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReadInterPredictionModeAllocatesZero(t *testing.T) {
	counts := InterModeCounts{Intra: 0, Nearest: 1, Near: 2, Split: 3}
	payload := encodeInterModeBits(counts, []uint8{1, 1, 1, 0})
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		_ = ReadInterPredictionMode(&br, counts)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeInterModeBits(counts InterModeCounts, bits []uint8) []byte {
	var w testBoolWriter
	w.init()
	probs := [4]uint8{
		tables.InterModeContexts[counts.Intra][0],
		tables.InterModeContexts[counts.Nearest][1],
		tables.InterModeContexts[counts.Near][2],
		tables.InterModeContexts[counts.Split][3],
	}
	for i, bit := range bits {
		w.writeBool(bit, probs[i])
	}
	return w.finish()
}

func TestReadInterReferenceFrame(t *testing.T) {
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	tests := []struct {
		name string
		bits []uint8
		want common.MVReferenceFrame
	}{
		{name: "intra", bits: []uint8{0}, want: common.IntraFrame},
		{name: "last", bits: []uint8{1, 0}, want: common.LastFrame},
		{name: "golden", bits: []uint8{1, 1, 0}, want: common.GoldenFrame},
		{name: "altref", bits: []uint8{1, 1, 1}, want: common.AltRefFrame},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var br boolcoder.Decoder
			if err := br.Init(encodeInterReferenceBits(tt.bits)); err != nil {
				t.Fatalf("Init returned error: %v", err)
			}

			got := ReadInterReferenceFrame(&br, header)

			if got != tt.want {
				t.Fatalf("ref = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReadInterReferenceFrameAllocatesZero(t *testing.T) {
	header := ModeHeader{ProbIntra: 128, ProbLast: 128, ProbGolden: 128}
	payload := encodeInterReferenceBits([]uint8{1, 1, 1})
	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		_ = ReadInterReferenceFrame(&br, header)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeInterReferenceBits(bits []uint8) []byte {
	var w testBoolWriter
	w.init()
	for _, bit := range bits {
		w.writeBool(bit, 128)
	}
	return w.finish()
}
