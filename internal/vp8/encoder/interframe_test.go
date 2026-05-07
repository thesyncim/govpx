package encoder

import (
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

	if cfg.ProbSkipFalse != 64 || cfg.ProbIntra != 64 || cfg.ProbLast != 85 || cfg.ProbGolden != 128 {
		t.Fatalf("mode probabilities = skip:%d intra:%d last:%d golden:%d, want 64/64/85/128",
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
	if state.Mode.ProbSkipFalse != 64 || state.Mode.ProbIntra != 64 || state.Mode.ProbLast != 85 || state.Mode.ProbGolden != 128 {
		t.Fatalf("parsed mode probabilities = skip:%d intra:%d last:%d golden:%d, want 64/64/85/128",
			state.Mode.ProbSkipFalse, state.Mode.ProbIntra, state.Mode.ProbLast, state.Mode.ProbGolden)
	}
}

func TestAdaptInterFrameMVProbabilities(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	var counts [2][tables.MVPCount][2]int
	for i := 0; i < 64; i++ {
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

	var wantCounts [2][tables.MVPCount][2]int
	if err := countMotionVectorBranches(&wantCounts, MotionVector{Col: 16}); err != nil {
		t.Fatalf("count first MV branches returned error: %v", err)
	}
	for i := 1; i < len(modes); i++ {
		if err := countMotionVectorBranches(&wantCounts, MotionVector{}); err != nil {
			t.Fatalf("count biased MV branches returned error: %v", err)
		}
	}
	wantCfg := DefaultInterFrameStateConfig(20)
	want := adaptInterFrameMVProbabilitiesWithBase(&wantCounts, tables.DefaultMVContext, &wantCfg)
	if got != want {
		t.Fatalf("frame MV probs = %v, want sign-biased predictor counts %v", got, want)
	}

	var noBiasCounts [2][tables.MVPCount][2]int
	if err := countMotionVectorBranches(&noBiasCounts, MotionVector{Col: 16}); err != nil {
		t.Fatalf("count first no-bias MV branches returned error: %v", err)
	}
	for i := 1; i < len(modes); i++ {
		delta := MotionVector{Col: 32}
		if modes[i].RefFrame == common.GoldenFrame {
			delta.Col = -32
		}
		if err := countMotionVectorBranches(&noBiasCounts, delta); err != nil {
			t.Fatalf("count no-bias MV branches returned error: %v", err)
		}
	}
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
	for i := 0; i < fillCount; i++ {
		mode.BlockMV[tables.MBSplitFillOffset[mode.Partition][fillStart+i]] = mv
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
