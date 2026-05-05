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

func zeroInterFramePacket(t *testing.T, width int, height int) []byte {
	t.Helper()
	dst := make([]byte, 512)
	n, err := WriteZeroInterFrame(dst, width, height, DefaultInterFrameStateConfig(20))
	if err != nil {
		t.Fatalf("WriteZeroInterFrame returned error: %v", err)
	}
	return dst[:n]
}
