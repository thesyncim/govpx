package decoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/gopvx/internal/vp8/common"
	"github.com/thesyncim/gopvx/internal/vp8/tables"
)

func TestParseStateHeaderKeyFrameZeroPayload(t *testing.T) {
	packet := append(keyFramePacket(64, 64, 0, 0, 200, 0, true), make([]byte, 200)...)

	frame, state, err := ParseStateHeader(packet, QuantHeader{})
	if err != nil {
		t.Fatalf("ParseStateHeader returned error: %v", err)
	}
	if !frame.KeyFrame() || frame.Width != 64 || frame.Height != 64 {
		t.Fatalf("frame = %+v, want 64x64 keyframe", frame)
	}
	if state.ColorSpace != 0 || state.ClampType != common.ReconClampRequired {
		t.Fatalf("keyframe color/clamp = %d/%d, want 0/0", state.ColorSpace, state.ClampType)
	}
	if state.Segmentation.Enabled {
		t.Fatalf("segmentation enabled for zero payload")
	}
	if state.LoopFilter.Type != NormalLoopFilter || state.LoopFilter.Level != 0 || state.LoopFilter.SharpnessLevel != 0 {
		t.Fatalf("loop filter = %+v, want zero normal filter", state.LoopFilter)
	}
	if state.TokenPartition != common.OnePartition {
		t.Fatalf("token partition = %d, want one partition", state.TokenPartition)
	}
	if state.Quant.BaseQIndex != 0 || state.Quant.Updated {
		t.Fatalf("quant = %+v, want zero unchanged quant", state.Quant)
	}
	if !state.Refresh.RefreshLast || !state.Refresh.RefreshGolden || !state.Refresh.RefreshAltRef {
		t.Fatalf("keyframe refresh = %+v, want all references refreshed", state.Refresh)
	}
	if state.Probability.UpdateCount != 0 || !state.Probability.IndependentPartitions {
		t.Fatalf("probability header = %+v, want no updates and independent partitions", state.Probability)
	}
}

func TestParseStateHeaderInterFrameZeroPayload(t *testing.T) {
	packet := append(interFramePacket(200, 0, true), make([]byte, 200)...)

	frame, state, err := ParseStateHeader(packet, QuantHeader{})
	if err != nil {
		t.Fatalf("ParseStateHeader returned error: %v", err)
	}
	if frame.KeyFrame() {
		t.Fatalf("frame = keyframe, want interframe")
	}
	if state.Refresh.RefreshLast || state.Refresh.RefreshGolden || state.Refresh.RefreshAltRef {
		t.Fatalf("interframe refresh = %+v, want no refresh flags for zero payload", state.Refresh)
	}
}

func TestParseStateHeaderUsesPreviousQuantDeltas(t *testing.T) {
	prev := QuantHeader{Y1DCDelta: 2, Y2DCDelta: -1}
	packet := append(interFramePacket(200, 0, true), make([]byte, 200)...)

	_, state, err := ParseStateHeader(packet, prev)
	if err != nil {
		t.Fatalf("ParseStateHeader returned error: %v", err)
	}
	if state.Quant.Y1DCDelta != 0 || state.Quant.Y2DCDelta != 0 {
		t.Fatalf("quant deltas = %+v, want zero deltas when update bits are absent", state.Quant)
	}
	if !state.Quant.Updated {
		t.Fatalf("Updated = false, want true because previous deltas changed to zero")
	}
}

func TestParseStateHeaderReadsTokenPartitionBeforeQuant(t *testing.T) {
	payload := encodeStateHeaderPrefix(common.EightPartition, 17)
	packet := append(keyFramePacket(64, 64, 0, 0, len(payload), 0, true), payload...)

	_, state, err := ParseStateHeader(packet, QuantHeader{})
	if err != nil {
		t.Fatalf("ParseStateHeader returned error: %v", err)
	}
	if state.TokenPartition != common.EightPartition {
		t.Fatalf("TokenPartition = %d, want eight partitions", state.TokenPartition)
	}
	if state.Quant.BaseQIndex != 17 {
		t.Fatalf("BaseQIndex = %d, want 17", state.Quant.BaseQIndex)
	}
}

func TestParseStateHeaderWithReaderReturnsPostStateReader(t *testing.T) {
	packet := append(keyFramePacket(64, 64, 0, 0, 200, 0, true), make([]byte, 200)...)

	frame, state, br, err := ParseStateHeaderWithReader(packet, QuantHeader{})
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReader returned error: %v", err)
	}
	if !frame.KeyFrame() || state.TokenPartition != common.OnePartition {
		t.Fatalf("frame/state = %+v/%+v, want keyframe one partition", frame, state)
	}
	if br.Err() != nil || br.Corrupted() {
		t.Fatalf("reader error/corrupted = %v/%v, want clean reader", br.Err(), br.Corrupted())
	}
}

func TestParseStateHeaderWithReaderAndProbsAppliesCoefficientUpdates(t *testing.T) {
	payload := encodeStateHeaderWithSingleCoefProbabilityUpdate(common.OnePartition, 0, true, 77)
	packet := append(keyFramePacket(64, 64, 0, 0, len(payload), 0, true), payload...)
	var probs tables.CoefficientProbs
	fillCoefficientProbs(&probs, 99)

	frame, state, _, err := ParseStateHeaderWithReaderAndProbs(packet, QuantHeader{}, &probs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbs returned error: %v", err)
	}
	if !frame.KeyFrame() || !state.Refresh.RefreshEntropyProbs {
		t.Fatalf("frame/refresh = %t/%t, want keyframe refresh entropy", frame.KeyFrame(), state.Refresh.RefreshEntropyProbs)
	}
	if state.Probability.UpdateCount != 1 || !state.Probability.IndependentPartitions {
		t.Fatalf("probability header = %+v, want one independent update", state.Probability)
	}
	if got := probs[0][0][0][0]; got != 77 {
		t.Fatalf("updated probability = %d, want 77", got)
	}
	if got := probs[0][0][0][1]; got != tables.DefaultCoefProbs[0][0][0][1] {
		t.Fatalf("neighbor probability = %d, want keyframe default", got)
	}
}

func TestParseStateHeaderWithReaderAndModeProbsResetsKeyFrame(t *testing.T) {
	packet := append(keyFramePacket(64, 64, 0, 0, 200, 0, true), make([]byte, 200)...)
	modeProbs := ModeProbs{
		YMode:  [tables.YModeProbCount]uint8{1, 2, 3, 4},
		UVMode: [tables.UVModeProbCount]uint8{5, 6, 7},
	}

	frame, _, _, err := ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet, QuantHeader{}, LoopFilterHeader{}, nil, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if !frame.KeyFrame() {
		t.Fatalf("frame is not keyframe")
	}
	if modeProbs.YMode != tables.DefaultYModeProbs || modeProbs.UVMode != tables.DefaultUVModeProbs || modeProbs.MV != tables.DefaultMVContext {
		t.Fatalf("mode probs were not reset to defaults: %+v", modeProbs)
	}
}

func TestParseStateHeaderWithReaderAndModeProbsAppliesInterUpdates(t *testing.T) {
	payload := encodeInterStateHeaderWithModeProbabilityUpdates()
	packet := append(interFramePacket(len(payload), 0, true), payload...)
	var modeProbs ModeProbs
	ResetModeProbs(&modeProbs)

	frame, state, _, err := ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet, QuantHeader{}, LoopFilterHeader{}, nil, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	if frame.KeyFrame() {
		t.Fatalf("frame is keyframe, want inter")
	}
	if !state.Mode.MBNoCoeffSkip || !state.Mode.YModeUpdated || !state.Mode.UVModeUpdated {
		t.Fatalf("mode header = %+v, want skip/y/uv updates", state.Mode)
	}
	if modeProbs.YMode != ([tables.YModeProbCount]uint8{10, 20, 30, 40}) {
		t.Fatalf("YMode = %v, want updated", modeProbs.YMode)
	}
	if modeProbs.UVMode != ([tables.UVModeProbCount]uint8{50, 60, 70}) {
		t.Fatalf("UVMode = %v, want updated", modeProbs.UVMode)
	}
}

func TestParseStateHeaderTruncated(t *testing.T) {
	packet := keyFramePacket(64, 64, 0, 0, 0, 0, true)

	_, _, err := ParseStateHeader(packet, QuantHeader{})
	if !errors.Is(err, ErrTruncatedStateHeader) {
		t.Fatalf("error = %v, want ErrTruncatedStateHeader", err)
	}
}

func TestParseStateHeaderAllocatesZero(t *testing.T) {
	packet := append(keyFramePacket(64, 64, 0, 0, 200, 0, true), make([]byte, 200)...)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _, _ = ParseStateHeader(packet, QuantHeader{})
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParseStateHeaderWithReaderAllocatesZero(t *testing.T) {
	packet := append(keyFramePacket(64, 64, 0, 0, 200, 0, true), make([]byte, 200)...)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _, _, _ = ParseStateHeaderWithReader(packet, QuantHeader{})
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParseStateHeaderWithReaderAndProbsAllocatesZero(t *testing.T) {
	payload := encodeStateHeaderWithSingleCoefProbabilityUpdate(common.OnePartition, 0, true, 77)
	packet := append(keyFramePacket(64, 64, 0, 0, len(payload), 0, true), payload...)
	allocs := testing.AllocsPerRun(1000, func() {
		probs := tables.DefaultCoefProbs
		_, _, _, _ = ParseStateHeaderWithReaderAndProbs(packet, QuantHeader{}, &probs)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeStateHeaderPrefix(tokenPartition common.TokenPartition, baseQ uint8) []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(uint32(tokenPartition), 2)
	w.writeLiteral(uint32(baseQ), 7)
	for i := 0; i < 5; i++ {
		w.writeBool(0, 128)
	}
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func encodeStateHeaderWithSingleCoefProbabilityUpdate(tokenPartition common.TokenPartition, baseQ uint8, refreshEntropy bool, value uint8) []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(uint32(tokenPartition), 2)
	w.writeLiteral(uint32(baseQ), 7)
	for i := 0; i < 5; i++ {
		w.writeBool(0, 128)
	}
	if refreshEntropy {
		w.writeBool(1, 128)
	} else {
		w.writeBool(0, 128)
	}

	first := true
	for block := 0; block < tables.BlockTypes; block++ {
		for band := 0; band < tables.CoefBands; band++ {
			for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
				for node := 0; node < tables.EntropyNodes; node++ {
					if first {
						w.writeBool(1, tables.CoefUpdateProbs[block][band][ctx][node])
						w.writeLiteral(uint32(value), 8)
						first = false
					} else {
						w.writeBool(0, tables.CoefUpdateProbs[block][band][ctx][node])
					}
				}
			}
		}
	}

	w.writeBool(0, 128)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func fillCoefficientProbs(probs *tables.CoefficientProbs, value uint8) {
	for block := range probs {
		for band := range probs[block] {
			for ctx := range probs[block][band] {
				for node := range probs[block][band][ctx] {
					probs[block][band][ctx][node] = value
				}
			}
		}
	}
}

func encodeInterStateHeaderWithModeProbabilityUpdates() []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeLiteral(0, 7)
	for i := 0; i < 5; i++ {
		w.writeBool(0, 128)
	}

	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)

	writeNoCoefficientProbabilityUpdates(&w)
	writeInterModeProbabilityUpdates(&w)

	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func writeNoCoefficientProbabilityUpdates(w *testBoolWriter) {
	for block := 0; block < tables.BlockTypes; block++ {
		for band := 0; band < tables.CoefBands; band++ {
			for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
				for node := 0; node < tables.EntropyNodes; node++ {
					w.writeBool(0, tables.CoefUpdateProbs[block][band][ctx][node])
				}
			}
		}
	}
}

func writeInterModeProbabilityUpdates(w *testBoolWriter) {
	w.writeBool(1, 128)
	w.writeLiteral(77, 8)

	w.writeLiteral(33, 8)
	w.writeLiteral(44, 8)
	w.writeLiteral(55, 8)

	w.writeBool(1, 128)
	w.writeLiteral(10, 8)
	w.writeLiteral(20, 8)
	w.writeLiteral(30, 8)
	w.writeLiteral(40, 8)

	w.writeBool(1, 128)
	w.writeLiteral(50, 8)
	w.writeLiteral(60, 8)
	w.writeLiteral(70, 8)

	for component := 0; component < 2; component++ {
		for i := 0; i < tables.MVPCount; i++ {
			w.writeBool(0, tables.MVUpdateProbs[component][i])
		}
	}
}
