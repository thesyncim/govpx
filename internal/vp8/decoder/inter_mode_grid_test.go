package decoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/common"
)

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
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
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
