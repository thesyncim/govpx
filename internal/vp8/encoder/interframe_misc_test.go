package encoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestWriteCoefficientInterFrameClampsNewMVBestPredictor(t *testing.T) {
	modes := []InterFrameMacroblockMode{
		{Mode: common.ZeroMV, MBSkipCoeff: true},
		{Mode: common.ZeroMV, MBSkipCoeff: true},
		{Mode: common.NewMV, MV: MotionVector{Col: 136}, MBSkipCoeff: true},
		{Mode: common.ZeroMV, MBSkipCoeff: true},
		{Mode: common.ZeroMV, MBSkipCoeff: true},
		{Mode: common.NewMV, MV: MotionVector{Col: -6}, MBSkipCoeff: true},
	}
	coeffs := make([]MacroblockCoefficients, len(modes))
	packet := make([]byte, 2048)
	above := make([]TokenContextPlanes, 3)
	n, err := WriteCoefficientInterFrame(packet, 48, 32, DefaultInterFrameStateConfig(20), modes, coeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientInterFrame returned error: %v", err)
	}

	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, modeReader, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	decodedModes := make([]vp8dec.MacroblockMode, len(modes))
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 2, 3, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, decodedModes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	if decodedModes[5].Mode != common.NewMV || decodedModes[5].MV != (vp8dec.MotionVector{Col: -6}) {
		t.Fatalf("mode[5] = %+v, want NEWMV col -6", decodedModes[5])
	}
}

func fillEncoderSplitSubset(mode *InterFrameMacroblockMode, subset int, mv MotionVector) {
	fillCount := int(tables.MBSplitFillCount[mode.Partition])
	fillStart := subset * fillCount
	for i := range fillCount {
		block := int(tables.MBSplitFillOffset[mode.Partition][fillStart+i])
		bMode := common.New4x4
		if block&3 != 0 && tables.MBSplits[mode.Partition][block-1] == uint8(subset) {
			bMode = common.Left4x4
		} else if block>>2 != 0 && tables.MBSplits[mode.Partition][block-4] == uint8(subset) {
			bMode = common.Above4x4
		}
		mode.BlockMV[block] = mv
		mode.BModes[block] = bMode
	}
}

func TestWriteCoefficientInterFrameClampsNearestMV(t *testing.T) {
	modes := []InterFrameMacroblockMode{
		{Mode: common.ZeroMV, MBSkipCoeff: true},
		{Mode: common.ZeroMV, MBSkipCoeff: true},
		{Mode: common.NewMV, MV: MotionVector{Col: 136}, MBSkipCoeff: true},
		{Mode: common.ZeroMV, MBSkipCoeff: true},
		{Mode: common.ZeroMV, MBSkipCoeff: true},
		{Mode: common.NearestMV, MV: MotionVector{Col: 128}, MBSkipCoeff: true},
	}
	coeffs := make([]MacroblockCoefficients, len(modes))
	packet := make([]byte, 2048)
	above := make([]TokenContextPlanes, 3)
	n, err := WriteCoefficientInterFrame(packet, 48, 32, DefaultInterFrameStateConfig(20), modes, coeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientInterFrame returned error: %v", err)
	}

	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, modeReader, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	decodedModes := make([]vp8dec.MacroblockMode, len(modes))
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 2, 3, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, decodedModes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	if decodedModes[5].Mode != common.NearestMV || decodedModes[5].MV != (vp8dec.MotionVector{Col: 128}) {
		t.Fatalf("mode[5] = %+v, want NEARESTMV col 128", decodedModes[5])
	}
}

func TestInterFrameMotionModeForVectorClassifiesNeighbors(t *testing.T) {
	left := InterFrameMacroblockMode{Mode: common.NewMV, MV: MotionVector{Col: -8}}
	signBias := [common.MaxRefFrames]bool{}
	mode := InterFrameMotionModeForVector(common.LastFrame, MotionVector{Col: -8}, nil, &left, nil, signBias)
	if mode.RefFrame != common.LastFrame || mode.Mode != common.NearestMV || mode.MV != left.MV {
		t.Fatalf("mode = %+v, want nearest col -8", mode)
	}

	above := InterFrameMacroblockMode{Mode: common.NewMV, MV: MotionVector{Col: 8}}
	aboveLeft := InterFrameMacroblockMode{Mode: common.NewMV, MV: MotionVector{Col: -8}}
	mode = InterFrameMotionModeForVector(common.GoldenFrame, MotionVector{Col: 8}, &above, &left, &aboveLeft, signBias)
	if mode.RefFrame != common.GoldenFrame || mode.Mode != common.NearMV || mode.MV != above.MV {
		t.Fatalf("mode = %+v, want near col 8", mode)
	}
}

func TestInterFrameMotionModeForVectorAtClassifiesClampedNeighbor(t *testing.T) {
	above := InterFrameMacroblockMode{Mode: common.NewMV, MV: MotionVector{Col: 136}}
	mode := InterFrameMotionModeForVectorAt(common.LastFrame, MotionVector{Col: 128}, &above, nil, nil, 1, 2, 2, 3, [common.MaxRefFrames]bool{})

	if mode.RefFrame != common.LastFrame || mode.Mode != common.NearestMV || mode.MV != (MotionVector{Col: 128}) {
		t.Fatalf("mode = %+v, want clamped nearest col 128", mode)
	}
}

func TestInterFrameNearMotionVectorsAtInvertsDifferentSignBiasNeighbor(t *testing.T) {
	left := InterFrameMacroblockMode{RefFrame: common.LastFrame, Mode: common.NewMV, MV: MotionVector{Col: 16}}
	signBias := [common.MaxRefFrames]bool{common.GoldenFrame: true}

	nearest, near := InterFrameNearMotionVectorsAt(&left, nil, nil, common.GoldenFrame, 0, 0, 1, 2, signBias)
	best := InterFrameBestMotionVectorAt(&left, nil, nil, common.GoldenFrame, 0, 0, 1, 2, signBias)

	if nearest != (MotionVector{Col: -16}) || best != nearest || !near.IsZero() {
		t.Fatalf("nearest/near/best = %+v/%+v/%+v, want inverted nearest/best col -16 and zero near", nearest, near, best)
	}
}

func TestInterFrameMotionModeForVectorAtClassifiesInvertedSignBiasNearest(t *testing.T) {
	left := InterFrameMacroblockMode{RefFrame: common.LastFrame, Mode: common.NewMV, MV: MotionVector{Col: 16}}
	signBias := [common.MaxRefFrames]bool{common.GoldenFrame: true}

	mode := InterFrameMotionModeForVectorAt(common.GoldenFrame, MotionVector{Col: -16}, &left, nil, nil, 0, 0, 1, 2, signBias)

	if mode.RefFrame != common.GoldenFrame || mode.Mode != common.NearestMV || mode.MV != (MotionVector{Col: -16}) {
		t.Fatalf("mode = %+v, want GOLDEN nearest col -16", mode)
	}
}

func TestInterFrameMotionModeForVectorAtKeepsSameSignBiasNeighbor(t *testing.T) {
	left := InterFrameMacroblockMode{RefFrame: common.GoldenFrame, Mode: common.NewMV, MV: MotionVector{Col: 16}}
	signBias := [common.MaxRefFrames]bool{common.GoldenFrame: true}

	mode := InterFrameMotionModeForVectorAt(common.GoldenFrame, MotionVector{Col: 16}, &left, nil, nil, 0, 0, 1, 2, signBias)

	if mode.RefFrame != common.GoldenFrame || mode.Mode != common.NearestMV || mode.MV != (MotionVector{Col: 16}) {
		t.Fatalf("mode = %+v, want same-bias GOLDEN nearest col 16", mode)
	}
}

func TestInterFrameMotionModeForVectorAtClampsAfterSignBiasInversion(t *testing.T) {
	left := InterFrameMacroblockMode{RefFrame: common.LastFrame, Mode: common.NewMV, MV: MotionVector{Col: 136}}
	signBias := [common.MaxRefFrames]bool{common.GoldenFrame: true}

	mode := InterFrameMotionModeForVectorAt(common.GoldenFrame, MotionVector{Col: -128}, &left, nil, nil, 0, 0, 1, 2, signBias)

	if mode.RefFrame != common.GoldenFrame || mode.Mode != common.NearestMV || mode.MV != (MotionVector{Col: -128}) {
		t.Fatalf("mode = %+v, want inverted then clamped GOLDEN nearest col -128", mode)
	}
}

func TestResetTokenContext4x4PreservesY2(t *testing.T) {
	above := TokenContextPlanes{Y1: [4]uint8{1, 1, 1, 1}, U: [2]uint8{1, 1}, V: [2]uint8{1, 1}, Y2: 1}
	left := TokenContextPlanes{Y1: [4]uint8{1, 1, 1, 1}, U: [2]uint8{1, 1}, V: [2]uint8{1, 1}, Y2: 2}

	resetTokenContext(&above, &left, true)

	if above != (TokenContextPlanes{Y2: 1}) || left != (TokenContextPlanes{Y2: 2}) {
		t.Fatalf("contexts = %+v/%+v, want only Y2 preserved", above, left)
	}
}

func TestWriteZeroInterFrameRejectsUnsupportedConfig(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.MBNoCoeffSkip = false
	_, err := WriteZeroInterFrame(make([]byte, 256), 16, 16, cfg)
	if !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("error = %v, want ErrInvalidPacketConfig", err)
	}
	cfg.MBNoCoeffSkip = true
	_, err = WriteZeroReferenceInterFrame(make([]byte, 256), 16, 16, cfg, common.IntraFrame)
	if !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid reference error = %v, want ErrInvalidPacketConfig", err)
	}
}

func TestWriteZeroInterFrameAllocatesZero(t *testing.T) {
	dst := make([]byte, 256)
	cfg := DefaultInterFrameStateConfig(20)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = WriteZeroInterFrame(dst, 16, 16, cfg)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestWriteCoefficientInterFrameAllocatesZero(t *testing.T) {
	dst := make([]byte, 512)
	modes := []InterFrameMacroblockMode{{Mode: common.ZeroMV, MBSkipCoeff: false}}
	coeffs := make([]MacroblockCoefficients, 1)
	coeffs[0].QCoeff[24][0] = 1
	setAllMacroblockEOBs(&coeffs[0], false)
	above := make([]TokenContextPlanes, 1)
	cfg := DefaultInterFrameStateConfig(20)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = WriteCoefficientInterFrame(dst, 16, 16, cfg, modes, coeffs, above)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkWriteInterCoefficientTokenGridSkipped(b *testing.B) {
	modes := make([]InterFrameMacroblockMode, 16)
	for i := range modes {
		modes[i] = InterFrameMacroblockMode{Mode: common.ZeroMV, MBSkipCoeff: true}
	}
	coeffs := make([]MacroblockCoefficients, 16)
	above := make([]TokenContextPlanes, 4)
	buf := make([]byte, 128)
	var w BoolWriter

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Init(buf)
		_ = WriteInterCoefficientTokenGrid(&w, 4, 4, modes, coeffs, above, &tables.DefaultCoefProbs)
		w.Finish()
	}
}

func zeroInterFramePacket(t *testing.T, width int, height int) []byte {
	t.Helper()
	dst := make([]byte, 512)
	n, err := WriteZeroInterFrame(dst, width, height, DefaultInterFrameStateConfig(20))
	if err != nil {
		t.Fatalf("WriteZeroInterFrame returned error: %v", err)
	}
	return dst[:n]
}

func assertInterPacketDecodes(t *testing.T, packet []byte, rows int, cols int) {
	t.Helper()
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	frame, state, modeReader, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet, vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	var layout vp8dec.PartitionLayout
	if err := vp8dec.ParsePartitionLayout(packet, frame, state.TokenPartition, &layout); err != nil {
		t.Fatalf("ParsePartitionLayout returned error: %v", err)
	}
	decodedModes := make([]vp8dec.MacroblockMode, rows*cols)
	if err := vp8dec.DecodeInterModeGrid(&modeReader, rows, cols, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, decodedModes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	var readers [8]boolcoder.Decoder
	for i := 0; i < layout.TokenCount; i++ {
		if err := readers[i].Init(layout.Tokens[i]); err != nil {
			t.Fatalf("token reader %d Init returned error: %v", i, err)
		}
	}
	tokens := make([]vp8dec.MacroblockTokens, rows*cols)
	above := make([]vp8dec.EntropyContextPlanes, cols)
	if _, err := vp8dec.DecodeTokenGrid(readers[:layout.TokenCount], rows, cols, &coefProbs, decodedModes, above, tokens); err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}
}
