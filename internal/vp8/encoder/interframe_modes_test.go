package encoder

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestWriteZeroReferenceInterFrameDecodesTokenPartitions(t *testing.T) {
	tests := []struct {
		name      string
		partition common.TokenPartition
		count     int
	}{
		{name: "two", partition: common.TwoPartition, count: 2},
		{name: "four", partition: common.FourPartition, count: 4},
		{name: "eight", partition: common.EightPartition, count: 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultInterFrameStateConfig(20)
			cfg.TokenPartition = tt.partition
			packet := make([]byte, 1024)
			n, err := WriteZeroReferenceInterFrame(packet, 16, 128, cfg, common.LastFrame)
			if err != nil {
				t.Fatalf("WriteZeroReferenceInterFrame returned error: %v", err)
			}

			var coefProbs = tables.DefaultCoefProbs
			var modeProbs vp8dec.ModeProbs
			vp8dec.ResetModeProbs(&modeProbs)
			frame, state, modeReader, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
			if err != nil {
				t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
			}
			var layout vp8dec.PartitionLayout
			if err := vp8dec.ParsePartitionLayout(packet[:n], frame, state.TokenPartition, &layout); err != nil {
				t.Fatalf("ParsePartitionLayout returned error: %v", err)
			}
			if layout.TokenCount != tt.count {
				t.Fatalf("token count = %d, want %d", layout.TokenCount, tt.count)
			}

			modes := make([]vp8dec.MacroblockMode, 8)
			if err := vp8dec.DecodeInterModeGrid(&modeReader, 8, 1, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, modes); err != nil {
				t.Fatalf("DecodeInterModeGrid returned error: %v", err)
			}
			var readers [8]boolcoder.Decoder
			for i := 0; i < layout.TokenCount; i++ {
				if err := readers[i].Init(layout.Tokens[i]); err != nil {
					t.Fatalf("token reader %d Init returned error: %v", i, err)
				}
			}
			tokens := make([]vp8dec.MacroblockTokens, 8)
			above := make([]vp8dec.EntropyContextPlanes, 1)
			total, err := vp8dec.DecodeTokenGrid(readers[:layout.TokenCount], 8, 1, &coefProbs, modes, above, tokens)
			if err != nil {
				t.Fatalf("DecodeTokenGrid returned error: %v", err)
			}
			if total != 0 {
				t.Fatalf("decoded coefficient count = %d, want 0", total)
			}
		})
	}
}

func TestWriteCoefficientInterFrameDecodesTokenPartitions(t *testing.T) {
	const (
		rows = 8
		cols = 1
	)
	modes := make([]InterFrameMacroblockMode, rows*cols)
	coeffs := make([]MacroblockCoefficients, rows*cols)
	for i := range modes {
		modes[i] = InterFrameMacroblockMode{Mode: common.ZeroMV, MBSkipCoeff: false}
		coeffs[i].QCoeff[24][0] = 1
		setAllMacroblockEOBs(&coeffs[i], false)
	}
	tests := []struct {
		name      string
		partition common.TokenPartition
		count     int
	}{
		{name: "two", partition: common.TwoPartition, count: 2},
		{name: "four", partition: common.FourPartition, count: 4},
		{name: "eight", partition: common.EightPartition, count: 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultInterFrameStateConfig(20)
			cfg.TokenPartition = tt.partition
			packet := make([]byte, 8192)
			above := make([]TokenContextPlanes, cols)
			n, err := WriteCoefficientInterFrame(packet, 16, 128, cfg, modes, coeffs, above)
			if err != nil {
				t.Fatalf("WriteCoefficientInterFrame returned error: %v", err)
			}

			var coefProbs = tables.DefaultCoefProbs
			var modeProbs vp8dec.ModeProbs
			vp8dec.ResetModeProbs(&modeProbs)
			frame, state, modeReader, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
			if err != nil {
				t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
			}
			var layout vp8dec.PartitionLayout
			if err := vp8dec.ParsePartitionLayout(packet[:n], frame, state.TokenPartition, &layout); err != nil {
				t.Fatalf("ParsePartitionLayout returned error: %v", err)
			}
			if layout.TokenCount != tt.count {
				t.Fatalf("token count = %d, want %d", layout.TokenCount, tt.count)
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
			decoderAbove := make([]vp8dec.EntropyContextPlanes, cols)
			total, err := vp8dec.DecodeTokenGrid(readers[:layout.TokenCount], rows, cols, &coefProbs, decodedModes, decoderAbove, tokens)
			if err != nil {
				t.Fatalf("DecodeTokenGrid returned error: %v", err)
			}
			if total == 0 || tokens[0].QCoeff[24][0] != 1 || tokens[len(tokens)-1].QCoeff[24][0] != 1 {
				t.Fatalf("decoded tokens total=%d firstY2=%d lastY2=%d, want partitioned residuals", total, tokens[0].QCoeff[24][0], tokens[len(tokens)-1].QCoeff[24][0])
			}
		})
	}
}

func TestWriteCoefficientInterFrameDecodesNewMV(t *testing.T) {
	modes := []InterFrameMacroblockMode{{Mode: common.NewMV, MV: MotionVector{Col: -8}, MBSkipCoeff: true}}
	coeffs := make([]MacroblockCoefficients, 1)
	packet := make([]byte, 512)
	above := make([]TokenContextPlanes, 1)
	n, err := WriteCoefficientInterFrame(packet, 16, 16, DefaultInterFrameStateConfig(20), modes, coeffs, above)
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
	decodedModes := make([]vp8dec.MacroblockMode, 1)
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 1, 1, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, decodedModes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	if decodedModes[0].Mode != common.NewMV || decodedModes[0].MV != (vp8dec.MotionVector{Col: -8}) || !decodedModes[0].MBSkipCoeff {
		t.Fatalf("mode = %+v, want skipped NEWMV col -8", decodedModes[0])
	}
}

func TestWriteCoefficientInterFrameDecodesIntraMacroblock(t *testing.T) {
	modes := []InterFrameMacroblockMode{{RefFrame: common.IntraFrame, Mode: common.DCPred, UVMode: common.HPred}}
	coeffs := make([]MacroblockCoefficients, 1)
	coeffs[0].QCoeff[24][0] = 1
	setAllMacroblockEOBs(&coeffs[0], false)
	packet := make([]byte, 512)
	above := make([]TokenContextPlanes, 1)
	n, err := WriteCoefficientInterFrame(packet, 16, 16, DefaultInterFrameStateConfig(20), modes, coeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientInterFrame returned error: %v", err)
	}
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	frame, state, modeReader, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	var layout vp8dec.PartitionLayout
	if err := vp8dec.ParsePartitionLayout(packet[:n], frame, state.TokenPartition, &layout); err != nil {
		t.Fatalf("ParsePartitionLayout returned error: %v", err)
	}
	decodedModes := make([]vp8dec.MacroblockMode, 1)
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 1, 1, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, decodedModes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	if decodedModes[0].RefFrame != common.IntraFrame || decodedModes[0].Mode != common.DCPred || decodedModes[0].UVMode != common.HPred || decodedModes[0].MBSkipCoeff || decodedModes[0].Is4x4 {
		t.Fatalf("mode = %+v, want non-skipped intra DCPRED/HPRED", decodedModes[0])
	}

	readers := [8]boolcoder.Decoder{}
	if err := readers[0].Init(layout.Tokens[0]); err != nil {
		t.Fatalf("token reader Init returned error: %v", err)
	}
	tokens := make([]vp8dec.MacroblockTokens, 1)
	decoderAbove := make([]vp8dec.EntropyContextPlanes, 1)
	total, err := vp8dec.DecodeTokenGrid(readers[:1], 1, 1, &coefProbs, decodedModes, decoderAbove, tokens)
	if err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}
	if total == 0 || tokens[0].QCoeff[24][0] != 1 {
		t.Fatalf("decoded tokens total=%d Y2=%d, want intra residual token", total, tokens[0].QCoeff[24][0])
	}
}

func TestWriteCoefficientInterFrameDecodesGoldenAndAltRef(t *testing.T) {
	modes := []InterFrameMacroblockMode{
		{RefFrame: common.GoldenFrame, Mode: common.ZeroMV, MBSkipCoeff: true},
		{RefFrame: common.AltRefFrame, Mode: common.ZeroMV, MBSkipCoeff: true},
	}
	coeffs := make([]MacroblockCoefficients, 2)
	packet := make([]byte, 512)
	above := make([]TokenContextPlanes, 2)
	n, err := WriteCoefficientInterFrame(packet, 32, 16, DefaultInterFrameStateConfig(20), modes, coeffs, above)
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
	decodedModes := make([]vp8dec.MacroblockMode, 2)
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 1, 2, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, decodedModes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	if decodedModes[0].RefFrame != common.GoldenFrame || decodedModes[0].Mode != common.ZeroMV || !decodedModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped GOLDEN/ZEROMV", decodedModes[0])
	}
	if decodedModes[1].RefFrame != common.AltRefFrame || decodedModes[1].Mode != common.ZeroMV || !decodedModes[1].MBSkipCoeff {
		t.Fatalf("mode[1] = %+v, want skipped ALTREF/ZEROMV", decodedModes[1])
	}
}

func TestWriteCoefficientInterFrameDecodesNearestAndNearMV(t *testing.T) {
	modes := []InterFrameMacroblockMode{
		{Mode: common.NewMV, MV: MotionVector{Col: -8}, MBSkipCoeff: true},
		{Mode: common.NewMV, MV: MotionVector{Col: 8}, MBSkipCoeff: true},
		{Mode: common.NearestMV, MV: MotionVector{Col: -8}, MBSkipCoeff: true},
		{Mode: common.NearMV, MV: MotionVector{Col: 8}, MBSkipCoeff: true},
	}
	coeffs := make([]MacroblockCoefficients, 4)
	packet := make([]byte, 1024)
	above := make([]TokenContextPlanes, 2)
	n, err := WriteCoefficientInterFrame(packet, 32, 32, DefaultInterFrameStateConfig(20), modes, coeffs, above)
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
	decodedModes := make([]vp8dec.MacroblockMode, 4)
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 2, 2, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, decodedModes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	want := []struct {
		mode common.MBPredictionMode
		mv   vp8dec.MotionVector
	}{
		{common.NewMV, vp8dec.MotionVector{Col: -8}},
		{common.NewMV, vp8dec.MotionVector{Col: 8}},
		{common.NearestMV, vp8dec.MotionVector{Col: -8}},
		{common.NearMV, vp8dec.MotionVector{Col: 8}},
	}
	for i := range want {
		if decodedModes[i].Mode != want[i].mode || decodedModes[i].MV != want[i].mv || !decodedModes[i].MBSkipCoeff {
			t.Fatalf("mode[%d] = %+v, want %v %+v skipped", i, decodedModes[i], want[i].mode, want[i].mv)
		}
	}
}

func TestWriteCoefficientInterFrameDecodesSignBiasedNearestMV(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.GoldenSignBias = true
	modes := []InterFrameMacroblockMode{
		{RefFrame: common.LastFrame, Mode: common.NewMV, MV: MotionVector{Col: 16}, MBSkipCoeff: true},
		{RefFrame: common.GoldenFrame, Mode: common.NearestMV, MV: MotionVector{Col: -16}, MBSkipCoeff: true},
	}
	coeffs := make([]MacroblockCoefficients, len(modes))
	packet := make([]byte, 1024)
	above := make([]TokenContextPlanes, 2)
	n, err := WriteCoefficientInterFrame(packet, 32, 16, cfg, modes, coeffs, above)
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
	signBias := [common.MaxRefFrames]bool{
		common.GoldenFrame: state.Refresh.GoldenSignBias,
		common.AltRefFrame: state.Refresh.AltRefSignBias,
	}
	if !signBias[common.GoldenFrame] || signBias[common.AltRefFrame] {
		t.Fatalf("parsed sign bias = %v, want GOLDEN only", signBias)
	}
	decodedModes := make([]vp8dec.MacroblockMode, len(modes))
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 1, 2, &state.Segmentation, state.Mode, &modeProbs, signBias, decodedModes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	if decodedModes[1].RefFrame != common.GoldenFrame || decodedModes[1].Mode != common.NearestMV || decodedModes[1].MV != (vp8dec.MotionVector{Col: -16}) {
		t.Fatalf("mode[1] = %+v, want sign-biased GOLDEN/NEARESTMV col -16", decodedModes[1])
	}
}

func TestWriteCoefficientInterFrameDecodesSplitMV(t *testing.T) {
	mode := InterFrameMacroblockMode{
		RefFrame:    common.LastFrame,
		Mode:        common.SplitMV,
		Partition:   2,
		MBSkipCoeff: true,
	}
	fillEncoderSplitSubset(&mode, 0, MotionVector{Col: 8})
	fillEncoderSplitSubset(&mode, 1, MotionVector{Row: 8})
	fillEncoderSplitSubset(&mode, 2, MotionVector{Col: -8})
	fillEncoderSplitSubset(&mode, 3, MotionVector{Row: -8, Col: -8})
	mode.MV = mode.BlockMV[15]

	coeffs := make([]MacroblockCoefficients, 1)
	packet := make([]byte, 2048)
	above := make([]TokenContextPlanes, 1)
	n, err := WriteCoefficientInterFrame(packet, 16, 16, DefaultInterFrameStateConfig(20), []InterFrameMacroblockMode{mode}, coeffs, above)
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
	decodedModes := make([]vp8dec.MacroblockMode, 1)
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 1, 1, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, decodedModes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}

	decoded := decodedModes[0]
	if decoded.RefFrame != common.LastFrame || decoded.Mode != common.SplitMV || !decoded.Is4x4 || !decoded.MBSkipCoeff || decoded.Partition != 2 {
		t.Fatalf("decoded mode = %+v, want LAST/SPLITMV partition 2 skipped", decoded)
	}
	for block, want := range mode.BlockMV {
		got := decoded.BlockMV[block]
		if got.Row != want.Row || got.Col != want.Col {
			t.Fatalf("block mv[%d] = %+v, want %+v", block, got, want)
		}
	}
	if decoded.MV != (vp8dec.MotionVector{Row: mode.MV.Row, Col: mode.MV.Col}) {
		t.Fatalf("mode MV = %+v, want last block %+v", decoded.MV, mode.MV)
	}
}

func TestSplitMotionVectorSyntaxUsesExplicitSubMVLabel(t *testing.T) {
	left := InterFrameMacroblockMode{RefFrame: common.LastFrame, Mode: common.NewMV, MV: MotionVector{Col: 16}}
	newLabelMode := explicitSplitLabelTestMode(common.New4x4, left.MV)
	leftLabelMode := explicitSplitLabelTestMode(common.Left4x4, left.MV)

	newPayload := splitMotionVectorPayload(t, &newLabelMode, &left)
	leftPayload := splitMotionVectorPayload(t, &leftLabelMode, &left)
	if bytes.Equal(newPayload, leftPayload) {
		t.Fatalf("NEW4X4 and LEFT4X4 payloads matched; want explicit sub-MV label to affect syntax")
	}

	var newCounts [2][tables.MVPCount][2]int
	if err := countSplitMotionVectorBranches(&newCounts, &newLabelMode, &left, nil, MotionVector{}); err != nil {
		t.Fatalf("countSplitMotionVectorBranches NEW4X4: %v", err)
	}
	var leftCounts [2][tables.MVPCount][2]int
	if err := countSplitMotionVectorBranches(&leftCounts, &leftLabelMode, &left, nil, MotionVector{}); err != nil {
		t.Fatalf("countSplitMotionVectorBranches LEFT4X4: %v", err)
	}
	if motionBranchCountTotal(newCounts) == 0 {
		t.Fatalf("NEW4X4 branch count total = 0, want MV delta counted even when target equals left")
	}
	if motionBranchCountTotal(leftCounts) != 0 {
		t.Fatalf("LEFT4X4 branch count total = %d, want no MV delta counted", motionBranchCountTotal(leftCounts))
	}
}

func explicitSplitLabelTestMode(label common.BPredictionMode, mv MotionVector) InterFrameMacroblockMode {
	mode := InterFrameMacroblockMode{
		RefFrame:  common.LastFrame,
		Mode:      common.SplitMV,
		Partition: 0,
	}
	fillEncoderSplitSubset(&mode, 0, mv)
	fillEncoderSplitSubset(&mode, 1, mv)
	mode.BModes[0] = label
	mode.BModes[8] = common.Left4x4
	mode.MV = mode.BlockMV[15]
	return mode
}

func splitMotionVectorPayload(t *testing.T, mode *InterFrameMacroblockMode, left *InterFrameMacroblockMode) []byte {
	t.Helper()
	var w BoolWriter
	buf := make([]byte, 128)
	mvProbs := tables.DefaultMVContext
	w.Init(buf)
	if err := WriteSplitMotionVectors(&w, &mvProbs, mode, left, nil, MotionVector{}); err != nil {
		t.Fatalf("WriteSplitMotionVectors: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error: %v", err)
	}
	return append([]byte(nil), w.Bytes()...)
}

func motionBranchCountTotal(counts [2][tables.MVPCount][2]int) int {
	total := 0
	for plane := range counts {
		for idx := range counts[plane] {
			total += counts[plane][idx][0] + counts[plane][idx][1]
		}
	}
	return total
}
