package encoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/common"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
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
	if !state.Mode.MBNoCoeffSkip || state.Mode.ProbSkipFalse != 128 || state.Mode.ProbIntra != 128 || state.Mode.ProbLast != 128 || state.Mode.ProbGolden != 128 {
		t.Fatalf("mode header = %+v, want default inter probabilities and skip support", state.Mode)
	}
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

func TestInterFrameMotionModeForVectorClassifiesNeighbors(t *testing.T) {
	left := InterFrameMacroblockMode{Mode: common.NewMV, MV: MotionVector{Col: -8}}
	mode := InterFrameMotionModeForVector(MotionVector{Col: -8}, nil, &left, nil)
	if mode.Mode != common.NearestMV || mode.MV != left.MV {
		t.Fatalf("mode = %+v, want nearest col -8", mode)
	}

	above := InterFrameMacroblockMode{Mode: common.NewMV, MV: MotionVector{Col: 8}}
	aboveLeft := InterFrameMacroblockMode{Mode: common.NewMV, MV: MotionVector{Col: -8}}
	mode = InterFrameMotionModeForVector(MotionVector{Col: 8}, &above, &left, &aboveLeft)
	if mode.Mode != common.NearMV || mode.MV != above.MV {
		t.Fatalf("mode = %+v, want near col 8", mode)
	}
}

func TestWriteZeroInterFrameRejectsUnsupportedConfig(t *testing.T) {
	cfg := DefaultInterFrameStateConfig(20)
	cfg.MBNoCoeffSkip = false
	_, err := WriteZeroInterFrame(make([]byte, 256), 16, 16, cfg)
	if !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("error = %v, want ErrInvalidPacketConfig", err)
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

func zeroInterFramePacket(t *testing.T, width int, height int) []byte {
	t.Helper()
	dst := make([]byte, 512)
	n, err := WriteZeroInterFrame(dst, width, height, DefaultInterFrameStateConfig(20))
	if err != nil {
		t.Fatalf("WriteZeroInterFrame returned error: %v", err)
	}
	return dst[:n]
}
