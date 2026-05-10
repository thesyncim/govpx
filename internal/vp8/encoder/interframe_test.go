package encoder

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
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

func TestWriteInterFrameStateHeaderParsesSegmentation(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.Segmentation = testSegmentationConfig()
	packet := make([]byte, 1024)
	n, err := WriteZeroInterFrame(packet, 16, 16, cfg)
	if err != nil {
		t.Fatalf("WriteZeroInterFrame returned error: %v", err)
	}
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)

	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	assertParsedSegmentation(t, state.Segmentation)
}

func TestWriteInterFrameStateHeaderCanSkipLastRefresh(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.RefreshLast = false
	packet := make([]byte, 256)
	n, err := WriteZeroInterFrame(packet, 16, 16, cfg)
	if err != nil {
		t.Fatalf("WriteZeroInterFrame returned error: %v", err)
	}
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)

	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if state.Refresh.RefreshLast {
		t.Fatalf("RefreshLast = true, want false")
	}
}

func TestWriteInterFrameStateHeaderCanRefreshGoldenAndAltRef(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.RefreshGolden = true
	cfg.RefreshAltRef = true
	packet := make([]byte, 256)
	n, err := WriteZeroInterFrame(packet, 16, 16, cfg)
	if err != nil {
		t.Fatalf("WriteZeroInterFrame returned error: %v", err)
	}
	var coefProbs = tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)

	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if !state.Refresh.RefreshLast || !state.Refresh.RefreshGolden || !state.Refresh.RefreshAltRef {
		t.Fatalf("refresh = %+v, want last/golden/altref refresh", state.Refresh)
	}
	if state.Refresh.CopyBufferToGolden != 0 || state.Refresh.CopyBufferToAltRef != 0 {
		t.Fatalf("copy buffers = golden:%d alt:%d, want zero when refreshing", state.Refresh.CopyBufferToGolden, state.Refresh.CopyBufferToAltRef)
	}
}

func TestWriteInterFrameRejectsInvalidReferenceCopyConfigs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*InterFrameStateConfig)
	}{
		{
			name: "copy to golden while refreshing golden",
			mutate: func(cfg *InterFrameStateConfig) {
				cfg.RefreshGolden = true
				cfg.CopyBufferToGolden = 1
			},
		},
		{
			name: "copy to altref while refreshing altref",
			mutate: func(cfg *InterFrameStateConfig) {
				cfg.RefreshAltRef = true
				cfg.CopyBufferToAltRef = 2
			},
		},
		{
			name: "invalid copy to golden selector",
			mutate: func(cfg *InterFrameStateConfig) {
				cfg.RefreshGolden = false
				cfg.CopyBufferToGolden = 3
			},
		},
		{
			name: "invalid copy to altref selector",
			mutate: func(cfg *InterFrameStateConfig) {
				cfg.RefreshAltRef = false
				cfg.CopyBufferToAltRef = 3
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultInterFrameStateConfig(20)
			tt.mutate(&cfg)
			if _, err := WriteZeroInterFrame(make([]byte, 256), 16, 16, cfg); !errors.Is(err, ErrInvalidPacketConfig) {
				t.Fatalf("WriteZeroInterFrame error = %v, want ErrInvalidPacketConfig", err)
			}
		})
	}
}

func TestWriteZeroInterFrameDecodesLastZeroMVSkipGrid(t *testing.T) {
	packet := zeroInterFramePacket(t, 32, 16)
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
	modes := make([]vp8dec.MacroblockMode, 2)
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 1, 2, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, modes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	for i, mode := range modes {
		if !mode.MBSkipCoeff || mode.RefFrame != common.LastFrame || mode.Mode != common.ZeroMV || !mode.MV.IsZero() {
			t.Fatalf("mode[%d] = %+v, want skipped LAST/ZEROMV", i, mode)
		}
	}
	readers := [8]boolcoder.Decoder{}
	if err := readers[0].Init(layout.Tokens[0]); err != nil {
		t.Fatalf("token reader Init returned error: %v", err)
	}
	tokens := make([]vp8dec.MacroblockTokens, 2)
	above := make([]vp8dec.EntropyContextPlanes, 2)
	total, err := vp8dec.DecodeTokenGrid(readers[:1], 1, 2, &coefProbs, modes, above, tokens)
	if err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}
	if total != 0 {
		t.Fatalf("decoded coefficient count = %d, want 0", total)
	}
}

func TestWriteLastFrameZeroMVModeGridWithSkipWritesSegmentMap(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.Segmentation = testSegmentationConfig()
	modes := []InterFrameMacroblockMode{
		{SegmentID: 0, Mode: common.ZeroMV, MBSkipCoeff: true},
		{SegmentID: 1, Mode: common.ZeroMV, MBSkipCoeff: true},
		{SegmentID: 2, Mode: common.ZeroMV, MBSkipCoeff: true},
		{SegmentID: 3, Mode: common.ZeroMV, MBSkipCoeff: true},
	}
	var w BoolWriter
	buf := make([]byte, 128)
	w.Init(buf)
	if err := WriteLastFrameZeroMVModeGridWithSkip(&w, 2, 2, cfg, modes); err != nil {
		t.Fatalf("WriteLastFrameZeroMVModeGridWithSkip returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}

	var br boolcoder.Decoder
	if err := br.Init(w.Bytes()); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	decoded := make([]vp8dec.MacroblockMode, 4)
	decoderSegmentation := decoderSegmentationHeader(cfg.Segmentation)
	modeHeader := vp8dec.ModeHeader{
		MBNoCoeffSkip: true,
		ProbSkipFalse: cfg.ProbSkipFalse,
		ProbIntra:     cfg.ProbIntra,
		ProbLast:      cfg.ProbLast,
		ProbGolden:    cfg.ProbGolden,
	}
	if err := vp8dec.DecodeInterModeGrid(&br, 2, 2, &decoderSegmentation, modeHeader, &modeProbs, [common.MaxRefFrames]bool{}, decoded); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	for i, mode := range decoded {
		if mode.SegmentID != modes[i].SegmentID || !mode.MBSkipCoeff || mode.RefFrame != common.LastFrame || mode.Mode != common.ZeroMV || !mode.MV.IsZero() {
			t.Fatalf("mode[%d] = %+v, want segment %d skipped LAST/ZEROMV", i, mode, modes[i].SegmentID)
		}
	}
}

func TestWriteZeroReferenceInterFrameDecodesReferenceZeroMVSkipGrid(t *testing.T) {
	tests := []struct {
		name string
		ref  common.MVReferenceFrame
	}{
		{name: "golden", ref: common.GoldenFrame},
		{name: "altref", ref: common.AltRefFrame},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := make([]byte, 512)
			n, err := WriteZeroReferenceInterFrame(packet, 32, 16, DefaultInterFrameStateConfig(20), tt.ref)
			if err != nil {
				t.Fatalf("WriteZeroReferenceInterFrame returned error: %v", err)
			}
			var coefProbs = tables.DefaultCoefProbs
			var modeProbs vp8dec.ModeProbs
			vp8dec.ResetModeProbs(&modeProbs)
			_, state, modeReader, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet[:n], vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
			if err != nil {
				t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
			}
			modes := make([]vp8dec.MacroblockMode, 2)
			if err := vp8dec.DecodeInterModeGrid(&modeReader, 1, 2, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, modes); err != nil {
				t.Fatalf("DecodeInterModeGrid returned error: %v", err)
			}
			for i, mode := range modes {
				if !mode.MBSkipCoeff || mode.RefFrame != tt.ref || mode.Mode != common.ZeroMV || !mode.MV.IsZero() {
					t.Fatalf("mode[%d] = %+v, want skipped %v/ZEROMV", i, mode, tt.ref)
				}
			}
		})
	}
}

func TestWriteCoefficientInterFrameDecodesResidualTokenGrid(t *testing.T) {
	modes := []InterFrameMacroblockMode{
		{Mode: common.ZeroMV, MBSkipCoeff: false},
		{Mode: common.ZeroMV, MBSkipCoeff: true},
	}
	coeffs := make([]MacroblockCoefficients, 2)
	coeffs[0].QCoeff[24][0] = 1
	setAllMacroblockEOBs(&coeffs[0], false)
	packet := make([]byte, 512)
	above := make([]TokenContextPlanes, 2)
	n, err := WriteCoefficientInterFrame(packet, 32, 16, DefaultInterFrameStateConfig(20), modes, coeffs, above)
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
	decodedModes := make([]vp8dec.MacroblockMode, 2)
	if err := vp8dec.DecodeInterModeGrid(&modeReader, 1, 2, &state.Segmentation, state.Mode, &modeProbs, [common.MaxRefFrames]bool{}, decodedModes); err != nil {
		t.Fatalf("DecodeInterModeGrid returned error: %v", err)
	}
	if decodedModes[0].MBSkipCoeff || !decodedModes[1].MBSkipCoeff {
		t.Fatalf("decoded skip flags = %t/%t, want false/true", decodedModes[0].MBSkipCoeff, decodedModes[1].MBSkipCoeff)
	}

	readers := [8]boolcoder.Decoder{}
	if err := readers[0].Init(layout.Tokens[0]); err != nil {
		t.Fatalf("token reader Init returned error: %v", err)
	}
	tokens := make([]vp8dec.MacroblockTokens, 2)
	decoderAbove := make([]vp8dec.EntropyContextPlanes, 2)
	total, err := vp8dec.DecodeTokenGrid(readers[:1], 1, 2, &coefProbs, decodedModes, decoderAbove, tokens)
	if err != nil {
		t.Fatalf("DecodeTokenGrid returned error: %v", err)
	}
	if total == 0 || tokens[0].QCoeff[24][0] != 1 || tokens[1] != (vp8dec.MacroblockTokens{}) {
		t.Fatalf("decoded tokens total=%d firstY2=%d second=%+v, want residual then skipped", total, tokens[0].QCoeff[24][0], tokens[1])
	}
}

func TestWriteCoefficientInterFrameScratchMatchesPublicPacket(t *testing.T) {
	tests := []struct {
		name  string
		rows  int
		cols  int
		cfg   InterFrameStateConfig
		modes []InterFrameMacroblockMode
		init  func([]MacroblockCoefficients)
	}{
		{
			name: "mixed references",
			rows: 2,
			cols: 3,
			cfg: func() InterFrameStateConfig {
				cfg := DefaultInterFrameStateConfig(31)
				cfg.ProbIntra = 120
				cfg.ProbLast = 180
				cfg.ProbGolden = 100
				cfg.MVBase = tables.DefaultMVContext
				cfg.MVProbs = tables.DefaultMVContext
				return cfg
			}(),
			modes: []InterFrameMacroblockMode{
				{Mode: common.ZeroMV, MBSkipCoeff: false},
				{Mode: common.ZeroMV, MBSkipCoeff: false},
				{RefFrame: common.GoldenFrame, Mode: common.ZeroMV, MBSkipCoeff: true},
				{RefFrame: common.IntraFrame, Mode: common.DCPred, UVMode: common.TMPred},
				{Mode: common.ZeroMV, MBSkipCoeff: false},
				{Mode: common.ZeroMV, MBSkipCoeff: false},
			},
			init: func(coeffs []MacroblockCoefficients) {
				coeffs[0].QCoeff[24][0] = 1
				coeffs[0].QCoeff[0][1] = -2
				coeffs[1].QCoeff[3][2] = 4
				coeffs[3].QCoeff[24][0] = -1
				coeffs[3].QCoeff[16][0] = 3
				coeffs[4].QCoeff[0][5] = -7
				coeffs[5].QCoeff[20][0] = 2
				setAllMacroblockEOBs(&coeffs[0], false)
				setAllMacroblockEOBs(&coeffs[1], false)
				setAllMacroblockEOBs(&coeffs[3], false)
				setAllMacroblockEOBs(&coeffs[4], false)
				setAllMacroblockEOBs(&coeffs[5], false)
			},
		},
		{
			name: "four by four tokens",
			rows: 2,
			cols: 2,
			cfg: func() InterFrameStateConfig {
				cfg := DefaultInterFrameStateConfig(22)
				cfg.IndependentContexts = true
				return cfg
			}(),
			modes: []InterFrameMacroblockMode{
				{RefFrame: common.IntraFrame, Mode: common.BPred, UVMode: common.VPred},
				{Mode: common.ZeroMV, MBSkipCoeff: false},
				{RefFrame: common.IntraFrame, Mode: common.DCPred, UVMode: common.DCPred, MBSkipCoeff: true},
				{RefFrame: common.AltRefFrame, Mode: common.ZeroMV, MBSkipCoeff: false},
			},
			init: func(coeffs []MacroblockCoefficients) {
				coeffs[0].QCoeff[0][0] = 5
				coeffs[0].QCoeff[0][3] = -9
				coeffs[0].QCoeff[7][15] = 34
				coeffs[0].QCoeff[16][0] = -4
				coeffs[1].QCoeff[24][0] = 2
				coeffs[1].QCoeff[1][1] = 1
				coeffs[3].QCoeff[24][3] = -18
				coeffs[3].QCoeff[23][8] = 67
				setAllMacroblockEOBs(&coeffs[0], true)
				setAllMacroblockEOBs(&coeffs[1], false)
				setAllMacroblockEOBs(&coeffs[3], false)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coeffs := make([]MacroblockCoefficients, tt.rows*tt.cols)
			tt.init(coeffs)
			publicPacket := make([]byte, 8192)
			scratchPacket := make([]byte, 8192)
			publicAbove := make([]TokenContextPlanes, tt.cols)
			scratchAbove := make([]TokenContextPlanes, tt.cols)

			publicN, publicCoef, publicY, publicUV, publicMV, err := WriteCoefficientInterFrameWithProbabilityBase(publicPacket, tt.cols*16, tt.rows*16, tt.cfg, tt.modes, coeffs, publicAbove, &tables.DefaultCoefProbs, tables.DefaultYModeProbs, tables.DefaultUVModeProbs, tables.DefaultMVContext)
			if err != nil {
				t.Fatalf("public WriteCoefficientInterFrameWithProbabilityBase returned error: %v", err)
			}
			var scratch PartitionScratch
			scratchN, scratchCoef, scratchY, scratchUV, scratchMV, _, err := WriteCoefficientInterFrameWithProbabilityBaseScratchAndSavings(scratchPacket, tt.cols*16, tt.rows*16, tt.cfg, tt.modes, coeffs, scratchAbove, &tables.DefaultCoefProbs, tables.DefaultYModeProbs, tables.DefaultUVModeProbs, tables.DefaultMVContext, &scratch)
			if err != nil {
				t.Fatalf("scratch WriteCoefficientInterFrameWithProbabilityBaseScratchAndSavings returned error: %v", err)
			}
			if publicN != scratchN || !bytes.Equal(publicPacket[:publicN], scratchPacket[:scratchN]) {
				t.Fatalf("scratch packet differs from public path: public=%d scratch=%d", publicN, scratchN)
			}
			if publicCoef != scratchCoef || publicY != scratchY || publicUV != scratchUV || publicMV != scratchMV {
				t.Fatalf("scratch probability outputs differ from public path")
			}
			assertInterPacketDecodes(t, scratchPacket[:scratchN], tt.rows, tt.cols)
		})
	}
}

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
