package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestWriteInterFrameStateHeaderParsesInDecoder(t *testing.T) {
	packet := zeroInterFramePacket(t, 16, 16)
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)

	frame, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet, vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if frame.KeyFrame() || frame.HeaderSize != FrameTagSize {
		t.Fatalf("frame = %+v, want interframe with 3-byte header", frame)
	}
	if state.Quant.BaseQIndex != 20 || !state.Refresh.RefreshLast || state.Refresh.RefreshGolden || state.Refresh.RefreshAltRef {
		t.Fatalf("state = %+v, want base q and last refresh only", state)
	}
	if !state.Mode.MBNoCoeffSkip || state.Mode.ProbSkipFalse != 1 || state.Mode.ProbIntra != 1 || state.Mode.ProbLast != 255 || state.Mode.ProbGolden != 128 {
		t.Fatalf("mode header = %+v, want adapted LAST/ZEROMV skip probabilities", state.Mode)
	}
}

func TestWriteInterFrameStateHeaderParsesLoopFilterDeltas(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.LoopFilterLevel = 17
	cfg.LFDeltaEnabled = true
	cfg.LFDeltaUpdate = true
	cfg.RefLFDeltas = [common.MaxRefLFDeltas]int8{2, 0, -2, -2}
	cfg.ModeLFDeltas = [common.MaxModeLFDeltas]int8{4, -12, 2, 4}
	dst := make([]byte, 512)
	n, err := WriteZeroInterFrame(dst, 16, 16, cfg)
	if err != nil {
		t.Fatalf("WriteZeroInterFrame returned error: %v", err)
	}
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)

	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(dst[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if !state.LoopFilter.DeltaEnabled || !state.LoopFilter.DeltaUpdate {
		t.Fatalf("loop filter delta flags = enabled:%t update:%t, want enabled update", state.LoopFilter.DeltaEnabled, state.LoopFilter.DeltaUpdate)
	}
	if state.LoopFilter.RefDeltas != cfg.RefLFDeltas || state.LoopFilter.ModeDeltas != cfg.ModeLFDeltas {
		t.Fatalf("loop filter deltas = %v/%v, want %v/%v", state.LoopFilter.RefDeltas, state.LoopFilter.ModeDeltas, cfg.RefLFDeltas, cfg.ModeLFDeltas)
	}
}

func TestWriteInterFrameStateHeaderParsesQuantDeltas(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(2)
	cfg.QuantDeltas = common.QuantDeltas{Y2DC: 2, UVDC: -3, UVAC: -3}
	dst := make([]byte, 512)
	n, err := WriteZeroInterFrame(dst, 16, 16, cfg)
	if err != nil {
		t.Fatalf("WriteZeroInterFrame returned error: %v", err)
	}
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)

	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(dst[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if state.Quant.BaseQIndex != 2 || state.Quant.Y2DCDelta != 2 || state.Quant.UVDCDelta != -3 || state.Quant.UVACDelta != -3 {
		t.Fatalf("quant = %+v, want base Q 2 with Y2/UV deltas", state.Quant)
	}
}

func TestAdaptInterFrameModeProbabilities(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	modes := []InterFrameMacroblockMode{
		{RefFrame: common.IntraFrame, Mode: common.DCPred, UVMode: common.DCPred},
		{Mode: common.ZeroMV, MBSkipCoeff: true},
		{RefFrame: common.GoldenFrame, Mode: common.ZeroMV, MBSkipCoeff: true},
		{RefFrame: common.AltRefFrame, Mode: common.ZeroMV, MBSkipCoeff: true},
	}

	if err := adaptInterFrameModeProbabilities(1, 4, modes, &cfg); err != nil {
		t.Fatalf("adaptInterFrameModeProbabilities returned error: %v", err)
	}

	if cfg.ProbSkipFalse != 64 || cfg.ProbIntra != 63 || cfg.ProbLast != 85 || cfg.ProbGolden != 127 {
		t.Fatalf("mode probabilities = skip:%d intra:%d last:%d golden:%d, want 64/63/85/127",
			cfg.ProbSkipFalse, cfg.ProbIntra, cfg.ProbLast, cfg.ProbGolden)
	}
}

func TestWriteCoefficientInterFrameEmitsAdaptedModeProbabilities(t *testing.T) {
	modes := []InterFrameMacroblockMode{
		{RefFrame: common.IntraFrame, Mode: common.DCPred, UVMode: common.DCPred},
		{Mode: common.ZeroMV, MBSkipCoeff: true},
		{RefFrame: common.GoldenFrame, Mode: common.ZeroMV, MBSkipCoeff: true},
		{RefFrame: common.AltRefFrame, Mode: common.ZeroMV, MBSkipCoeff: true},
	}
	coeffs := make([]MacroblockCoefficients, len(modes))
	packet := make([]byte, 2048)
	above := make([]TokenContextPlanes, len(modes))

	n, err := WriteCoefficientInterFrame(packet, 64, 16, DefaultInterFrameStateConfig(20), modes, coeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientInterFrame returned error: %v", err)
	}

	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if state.Mode.ProbSkipFalse != 64 || state.Mode.ProbIntra != 63 || state.Mode.ProbLast != 85 || state.Mode.ProbGolden != 127 {
		t.Fatalf("parsed mode probabilities = skip:%d intra:%d last:%d golden:%d, want 64/63/85/127",
			state.Mode.ProbSkipFalse, state.Mode.ProbIntra, state.Mode.ProbLast, state.Mode.ProbGolden)
	}
}

func TestWriteCoefficientInterFrameEmitsInterIntraModeProbabilityUpdates(t *testing.T) {
	const rows = 48
	const cols = 48
	modes := make([]InterFrameMacroblockMode, rows*cols)
	for i := range modes {
		modes[i] = InterFrameMacroblockMode{
			RefFrame:    common.IntraFrame,
			Mode:        common.DCPred,
			UVMode:      common.DCPred,
			MBSkipCoeff: true,
		}
	}
	coeffs := make([]MacroblockCoefficients, len(modes))
	above := make([]TokenContextPlanes, cols)
	packet := make([]byte, 1<<20)

	n, err := WriteCoefficientInterFrame(packet, cols*16, rows*16, DefaultInterFrameStateConfig(20), modes, coeffs, above)
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
	if !state.Mode.YModeUpdated || !state.Mode.UVModeUpdated {
		t.Fatalf("mode update flags = Y:%t UV:%t, want both updated", state.Mode.YModeUpdated, state.Mode.UVModeUpdated)
	}
	if want := ([tables.YModeProbCount]uint8{255, 128, 128, 128}); modeProbs.YMode != want {
		t.Fatalf("Y mode probs = %v, want %v", modeProbs.YMode, want)
	}
	if want := ([tables.UVModeProbCount]uint8{255, 128, 128}); modeProbs.UVMode != want {
		t.Fatalf("UV mode probs = %v, want %v", modeProbs.UVMode, want)
	}

	decoded := make([]vp8dec.MacroblockMode, len(modes))
	if err := vp8dec.DecodeInterModeGrid(&modeReader, rows, cols, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, decoded); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	for i, mode := range decoded {
		if mode.RefFrame != common.IntraFrame || mode.Mode != common.DCPred || mode.UVMode != common.DCPred || !mode.MBSkipCoeff {
			t.Fatalf("decoded mode[%d] = %+v, want skipped INTRA/DCPRED/DCPRED", i, mode)
		}
	}
}

func TestAdaptInterFrameMVProbabilities(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	var counts [2][tables.MVPCount][2]int
	for range 64 {
		if err := countMotionVectorBranches(&counts, MotionVector{Col: 16}); err != nil {
			t.Fatalf("countMotionVectorBranches returned error: %v", err)
		}
	}

	adaptInterFrameMVProbabilities(&counts, &cfg)

	if cfg.MVUpdateCount == 0 {
		t.Fatalf("MVUpdateCount = 0, want repeated NEWMV deltas to update probabilities")
	}
	if !cfg.MVUpdate[1][mvProbIsShort] || cfg.MVProbs[1][mvProbIsShort] == tables.DefaultMVContext[1][mvProbIsShort] {
		t.Fatalf("col is-short update = %t prob=%d default=%d, want updated",
			cfg.MVUpdate[1][mvProbIsShort], cfg.MVProbs[1][mvProbIsShort], tables.DefaultMVContext[1][mvProbIsShort])
	}
}

func TestMotionVectorProbabilityFromBranchCountMatchesLibvpxCalcProb(t *testing.T) {
	tests := []struct {
		name   string
		counts [2]int
		want   uint8
	}{
		{name: "zero", counts: [2]int{}, want: 128},
		{name: "balanced", counts: [2]int{1, 1}, want: 126},
		{name: "all zero branches", counts: [2]int{0, 16}, want: 1},
		{name: "all one branches", counts: [2]int{16, 0}, want: 254},
		{name: "skewed rounds down to even", counts: [2]int{5, 7}, want: 106},
	}
	for _, tt := range tests {
		if got := motionVectorProbabilityFromBranchCount(tt.counts); got != tt.want {
			t.Fatalf("%s: motionVectorProbabilityFromBranchCount(%v) = %d, want %d",
				tt.name, tt.counts, got, tt.want)
		}
	}
}

func TestMotionVectorProbabilityUpdateSavingsMatchesLibvpxCorrection(t *testing.T) {
	counts := [2]int{7, 13}
	oldProb := uint8(164)
	newProb := uint8(88)
	updateProb := uint8(231)
	got := motionVectorProbabilityUpdateSavings(counts, oldProb, newProb, updateProb)
	want := libvpxMotionVectorProbabilityUpdateSavings(counts, oldProb, newProb, updateProb)
	if got != want {
		t.Fatalf("motionVectorProbabilityUpdateSavings = %d, want libvpx correction %d", got, want)
	}

	legacyUpdateBits := 7 + ((coefficientBitCost(updateProb, 1) - coefficientBitCost(updateProb, 0)) >> 8)
	libvpxUpdateBits := 7 - 1 + ((coefficientBitCost(updateProb, 1) - coefficientBitCost(updateProb, 0) + 128) >> 8)
	if legacyUpdateBits == libvpxUpdateBits {
		t.Fatalf("test case does not exercise MV_PROB_UPDATE_CORRECTION: legacy=%d libvpx=%d",
			legacyUpdateBits, libvpxUpdateBits)
	}
}

func libvpxMotionVectorProbabilityUpdateSavings(counts [2]int, oldProb uint8, newProb uint8, updateProb uint8) int {
	oldBits := coefficientBranchCost(counts, oldProb)
	newBits := coefficientBranchCost(counts, newProb)
	updateBits := 7 - 1 + ((coefficientBitCost(updateProb, 1) - coefficientBitCost(updateProb, 0) + 128) >> 8)
	return oldBits - newBits - updateBits
}

func TestMotionVectorEventBranchCountsIncludeImplicitLongBit3(t *testing.T) {
	var events motionVectorEventCounts
	for range 64 {
		if err := countMotionVectorEvents(&events, MotionVector{Col: 16}); err != nil {
			t.Fatalf("countMotionVectorEvents returned error: %v", err)
		}
	}
	counts := motionVectorBranchCountsFromEvents(&events)
	if got, want := counts[1][mvProbBits+3], [2]int{0, 64}; got != want {
		t.Fatalf("event-derived col bit3 counts = %v, want %v", got, want)
	}

	var syntaxCounts [2][tables.MVPCount][2]int
	for range 64 {
		if err := countMotionVectorBranches(&syntaxCounts, MotionVector{Col: 16}); err != nil {
			t.Fatalf("countMotionVectorBranches returned error: %v", err)
		}
	}
	if got := syntaxCounts[1][mvProbBits+3]; got != ([2]int{}) {
		t.Fatalf("syntax col bit3 counts = %v, want omitted bit3 syntax branch", got)
	}
}

func TestAdaptInterFrameModeProbabilitiesUsesMVEventDistribution(t *testing.T) {
	const cols = 512
	modes := make([]InterFrameMacroblockMode, cols)
	for i := range modes {
		modes[i] = InterFrameMacroblockMode{Mode: common.ZeroMV, MBSkipCoeff: true}
		if i%2 == 0 {
			modes[i] = InterFrameMacroblockMode{Mode: common.NewMV, MV: MotionVector{Col: 16}, MBSkipCoeff: true}
		}
	}
	cfg := DefaultInterFrameStateConfig(20)

	got, err := adaptInterFrameModeProbabilitiesWithMVBase(1, cols, modes, tables.DefaultMVContext, &cfg)
	if err != nil {
		t.Fatalf("adaptInterFrameModeProbabilitiesWithMVBase returned error: %v", err)
	}

	var events motionVectorEventCounts
	var syntaxCounts [2][tables.MVPCount][2]int
	for i := 0; i < cols; i += 2 {
		if err := countMotionVectorEvents(&events, MotionVector{Col: 16}); err != nil {
			t.Fatalf("count event MV branches returned error: %v", err)
		}
		if err := countMotionVectorBranches(&syntaxCounts, MotionVector{Col: 16}); err != nil {
			t.Fatalf("count syntax MV branches returned error: %v", err)
		}
	}
	wantCounts := motionVectorBranchCountsFromEvents(&events)
	wantCfg := DefaultInterFrameStateConfig(20)
	want := adaptInterFrameMVProbabilitiesWithBase(&wantCounts, tables.DefaultMVContext, &wantCfg)
	if got != want {
		t.Fatalf("frame MV probs = %v, want event-derived counts %v", got, want)
	}

	syntaxCfg := DefaultInterFrameStateConfig(20)
	syntax := adaptInterFrameMVProbabilitiesWithBase(&syntaxCounts, tables.DefaultMVContext, &syntaxCfg)
	if got == syntax {
		t.Fatalf("frame MV probs matched syntax branch counts, want libvpx MVcount distribution")
	}
}

func TestWriteCoefficientInterFrameEmitsMVProbabilityUpdates(t *testing.T) {
	const rows, cols = 16, 4
	modes := make([]InterFrameMacroblockMode, rows*cols)
	coeffs := make([]MacroblockCoefficients, len(modes))
	for i := range modes {
		modes[i] = InterFrameMacroblockMode{Mode: common.NewMV, MV: MotionVector{Col: 16}, MBSkipCoeff: true}
	}
	packet := make([]byte, 8192)
	above := make([]TokenContextPlanes, cols)

	n, err := WriteCoefficientInterFrame(packet, cols*16, rows*16, DefaultInterFrameStateConfig(20), modes, coeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientInterFrame returned error: %v", err)
	}

	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if state.Mode.MVUpdateCount == 0 {
		t.Fatalf("parsed MVUpdateCount = 0, want emitted MV probability updates")
	}
	if modeProbs.MV == tables.DefaultMVContext {
		t.Fatalf("parsed MV probabilities equal defaults, want updates applied")
	}
}

func TestAdaptInterFrameModeProbabilitiesCountsSignBiasedNewMVPredictor(t *testing.T) {
	const cols = 64
	modes := make([]InterFrameMacroblockMode, cols)
	for i := range modes {
		mode := InterFrameMacroblockMode{RefFrame: common.LastFrame, Mode: common.NewMV, MV: MotionVector{Col: 16}, MBSkipCoeff: true}
		if i%2 == 1 {
			mode.RefFrame = common.GoldenFrame
			mode.MV = MotionVector{Col: -16}
		}
		modes[i] = mode
	}
	cfg := DefaultInterFrameStateConfig(20)
	cfg.GoldenSignBias = true

	got, err := adaptInterFrameModeProbabilitiesWithMVBase(1, cols, modes, tables.DefaultMVContext, &cfg)
	if err != nil {
		t.Fatalf("adaptInterFrameModeProbabilitiesWithMVBase returned error: %v", err)
	}

	var wantEvents motionVectorEventCounts
	if err := countMotionVectorEvents(&wantEvents, MotionVector{Col: 16}); err != nil {
		t.Fatalf("count first MV event returned error: %v", err)
	}
	for i := 1; i < len(modes); i++ {
		if err := countMotionVectorEvents(&wantEvents, MotionVector{}); err != nil {
			t.Fatalf("count biased MV event returned error: %v", err)
		}
	}
	wantCounts := motionVectorBranchCountsFromEvents(&wantEvents)
	wantCfg := DefaultInterFrameStateConfig(20)
	want := adaptInterFrameMVProbabilitiesWithBase(&wantCounts, tables.DefaultMVContext, &wantCfg)
	if got != want {
		t.Fatalf("frame MV probs = %v, want sign-biased predictor counts %v", got, want)
	}

	var noBiasEvents motionVectorEventCounts
	if err := countMotionVectorEvents(&noBiasEvents, MotionVector{Col: 16}); err != nil {
		t.Fatalf("count first no-bias MV event returned error: %v", err)
	}
	for i := 1; i < len(modes); i++ {
		delta := MotionVector{Col: 32}
		if modes[i].RefFrame == common.GoldenFrame {
			delta.Col = -32
		}
		if err := countMotionVectorEvents(&noBiasEvents, delta); err != nil {
			t.Fatalf("count no-bias MV event returned error: %v", err)
		}
	}
	noBiasCounts := motionVectorBranchCountsFromEvents(&noBiasEvents)
	noBiasCfg := DefaultInterFrameStateConfig(20)
	noBias := adaptInterFrameMVProbabilitiesWithBase(&noBiasCounts, tables.DefaultMVContext, &noBiasCfg)
	if got == noBias {
		t.Fatalf("frame MV probs matched un-biased predictor counts, want sign bias to change counted NEWMV deltas")
	}
}
