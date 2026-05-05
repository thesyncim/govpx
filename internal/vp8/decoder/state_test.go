package decoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/common"
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
