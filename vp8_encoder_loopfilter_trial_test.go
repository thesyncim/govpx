package govpx

import (
	"bytes"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestLoopFilterTrialLumaSSELevelZeroUsesLibvpxTrialFilter(t *testing.T) {
	const width, height = 64, 128
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(33 + (r*13+c*3)%170)
			src.U[(r/2)*src.UStride+(c/2)] = 128
			src.V[(r/2)*src.VStride+(c/2)] = 128
		}
	}

	e := newSizedTestEncoder(t, width, height)
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(57 + (r*5+c*11)%160)
		}
	}
	for i := range e.loopFilterPick.Img.Y {
		e.loopFilterPick.Img.Y[i] = 201
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			RefFrame: vp8common.IntraFrame,
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
		}
	}
	srcImg := sourceImageFromPublic(src)
	ctx := e.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	for _, partial := range []bool{false, true} {
		for i := range e.loopFilterPick.Img.Y {
			e.loopFilterPick.Img.Y[i] = 201
		}
		scratchBefore := append([]byte(nil), e.loopFilterPick.Img.Y...)
		got := ctx.trialLumaSSE(0, partial)
		if got <= 0 {
			t.Fatalf("level zero trial partial=%t SSE = %d, want scored trial", partial, got)
		}
		if bytes.Equal(e.loopFilterPick.Img.Y, scratchBefore) {
			t.Fatalf("level zero trial partial=%t left loop-filter scratch buffer untouched", partial)
		}
	}
}

func TestLoopFilterTrialLumaSSEPartialMatchesFullFrameWindow(t *testing.T) {
	const width, height = 64, 128
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	fillImage(src, 96, 128, 128)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(40 + (r*7+c*11)%160)
		}
	}

	e := newSizedTestEncoder(t, width, height)
	e.threadedRowsActive = true
	// Seed the analysis buffer with reconstructed-like values that differ
	// macroblock-by-macroblock so the loop filter actually has work to do.
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(50 + (r*5+c*9)%180)
		}
	}
	for i := range e.analysis.Img.U {
		e.analysis.Img.U[i] = 128
	}
	for i := range e.analysis.Img.V {
		e.analysis.Img.V[i] = 128
	}
	if len(e.reconstructModes) < required {
		e.reconstructModes = make([]vp8dec.MacroblockMode, required)
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
	}

	srcImg := sourceImageFromPublic(src)
	ctx := e.newLoopFilterPickContext(srcImg, vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	for _, level := range []int{8, 24, 48} {
		partialErr := ctx.trialLumaSSE(level, true)
		fullErr := ctx.trialLumaSSE(level, false)
		// The full path computes SSE over the whole frame; recompute the
		// partial-window SSE on the buffer left behind by the full filter so
		// we can compare against the partial path.
		fullPartialWindow := vp8enc.LoopFilterLumaSSE(srcImg, &e.loopFilterPick.Img, rows, cols, true)
		_ = fullErr
		if partialErr != fullPartialWindow {
			t.Fatalf("level=%d partial SSE = %d, full-frame partial-window SSE = %d", level, partialErr, fullPartialWindow)
		}
	}
}

func TestLoopFilterTrialLumaSSEStatsMatchesNoStatsPath(t *testing.T) {
	const width, height = 64, 64
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(48 + (r*3+c*13)%144)
			src.U[(r/2)*src.UStride+(c/2)] = 128
			src.V[(r/2)*src.VStride+(c/2)] = 128
		}
	}

	e := newSizedTestEncoder(t, width, height)
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(64 + (r*5+c*7)%128)
		}
	}
	for i := range e.analysis.Img.U {
		e.analysis.Img.U[i] = 128
	}
	for i := range e.analysis.Img.V {
		e.analysis.Img.V[i] = 128
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
	}

	ctx := e.newLoopFilterPickContext(sourceImageFromPublic(src), vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	var stats EncoderPhaseStats
	if got, want := ctx.trialLumaSSEPartialStats(24, &stats), ctx.trialLumaSSEPartial(24); got != want {
		t.Fatalf("partial stats SSE = %d, no-stats SSE = %d", got, want)
	}
	if got, want := ctx.trialLumaSSEFullStats(24, &stats), ctx.trialLumaSSEFull(24); got != want {
		t.Fatalf("full stats SSE = %d, no-stats SSE = %d", got, want)
	}
	if stats.LoopFilterTrials != 2 {
		t.Fatalf("LoopFilterTrials = %d, want 2", stats.LoopFilterTrials)
	}
}

func TestLoopFilterPickNoStatsAllocatesZero(t *testing.T) {
	const width, height = 64, 64
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(48 + (r*3+c*13)%144)
			src.U[(r/2)*src.UStride+(c/2)] = 128
			src.V[(r/2)*src.VStride+(c/2)] = 128
		}
	}

	e := newSizedTestEncoder(t, width, height)
	e.rc.currentQuantizer = 48
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(64 + (r*5+c*7)%128)
		}
	}
	for i := range e.analysis.Img.U {
		e.analysis.Img.U[i] = 128
	}
	for i := range e.analysis.Img.V {
		e.analysis.Img.V[i] = 128
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
	}

	ctx := e.newLoopFilterPickContext(sourceImageFromPublic(src), vp8common.InterFrame, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	minLevel := vp8enc.LibvpxMinLoopFilterLevel(e.rc.currentQuantizer)
	allocs := testing.AllocsPerRun(50, func() {
		if _, err := ctx.pickFastNoStats(24, minLevel); err != nil {
			t.Fatalf("pickFastNoStats: %v", err)
		}
		if _, err := ctx.pickFullNoStats(24, minLevel); err != nil {
			t.Fatalf("pickFullNoStats: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("loop-filter no-stats pick allocs = %f, want 0", allocs)
	}
}
