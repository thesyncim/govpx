package encoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/common"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
)

func TestWriteKeyFrameStateHeaderParsesInDecoder(t *testing.T) {
	cfg := KeyFrameStateConfig{
		ClampType:           common.ReconClampNotRequired,
		SimpleLoopFilter:    true,
		LoopFilterLevel:     17,
		SharpnessLevel:      3,
		TokenPartition:      common.EightPartition,
		BaseQIndex:          42,
		RefreshEntropyProbs: true,
		MBNoCoeffSkip:       true,
		ProbSkipFalse:       77,
	}
	packet := keyFrameStatePacket(t, cfg)

	frame, state, err := vp8dec.ParseStateHeader(packet, vp8dec.QuantHeader{})
	if err != nil {
		t.Fatalf("ParseStateHeader returned error: %v", err)
	}
	if !frame.KeyFrame() || frame.Width != 64 || frame.Height != 48 {
		t.Fatalf("frame = %+v, want 64x48 keyframe", frame)
	}
	if state.ColorSpace != 0 || state.ClampType != common.ReconClampNotRequired {
		t.Fatalf("color/clamp = %d/%d, want 0/not-required", state.ColorSpace, state.ClampType)
	}
	if state.Segmentation.Enabled {
		t.Fatalf("segmentation enabled, want disabled")
	}
	if state.LoopFilter.Type != vp8dec.SimpleLoopFilter || state.LoopFilter.Level != 17 || state.LoopFilter.SharpnessLevel != 3 {
		t.Fatalf("loop filter = %+v, want simple level 17 sharpness 3", state.LoopFilter)
	}
	if state.TokenPartition != common.EightPartition || state.Quant.BaseQIndex != 42 {
		t.Fatalf("partition/quant = %d/%d, want eight/42", state.TokenPartition, state.Quant.BaseQIndex)
	}
	if !state.Refresh.RefreshEntropyProbs || state.Probability.UpdateCount != 0 || !state.Probability.IndependentPartitions {
		t.Fatalf("refresh/probability = %+v/%+v, want refresh entropy and no updates", state.Refresh, state.Probability)
	}
	if !state.Mode.MBNoCoeffSkip || state.Mode.ProbSkipFalse != 77 {
		t.Fatalf("mode header = %+v, want skip probability 77", state.Mode)
	}
}

func TestWriteKeyFrameStateHeaderRejectsInvalidConfig(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 512))
	err := WriteKeyFrameStateHeader(&w, KeyFrameStateConfig{ClampType: common.ClampType(2)})
	if !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid clamp error = %v, want ErrInvalidPacketConfig", err)
	}
	err = WriteKeyFrameStateHeader(&w, KeyFrameStateConfig{TokenPartition: common.TokenPartition(4)})
	if !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid partition error = %v, want ErrInvalidPacketConfig", err)
	}
	err = WriteKeyFrameStateHeader(&w, KeyFrameStateConfig{LoopFilterLevel: 64})
	if !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid loop filter error = %v, want ErrInvalidPacketConfig", err)
	}
}

func TestWriteKeyFrameStateHeaderReportsSmallBuffer(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 1))
	err := WriteKeyFrameStateHeader(&w, KeyFrameStateConfig{})
	if !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("error = %v, want ErrBufferTooSmall", err)
	}
}

func TestWriteKeyFrameStateHeaderAllocatesZero(t *testing.T) {
	var w BoolWriter
	buf := make([]byte, 512)
	cfg := KeyFrameStateConfig{TokenPartition: common.OnePartition, BaseQIndex: 20}
	allocs := testing.AllocsPerRun(1000, func() {
		w.Init(buf)
		_ = WriteKeyFrameStateHeader(&w, cfg)
		w.Finish()
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkWriteKeyFrameStateHeader(b *testing.B) {
	var w BoolWriter
	buf := make([]byte, 512)
	cfg := KeyFrameStateConfig{TokenPartition: common.OnePartition, BaseQIndex: 20}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Init(buf)
		_ = WriteKeyFrameStateHeader(&w, cfg)
		w.Finish()
	}
}

func keyFrameStatePacket(t *testing.T, cfg KeyFrameStateConfig) []byte {
	t.Helper()
	first := make([]byte, 512)
	var w BoolWriter
	w.Init(first)
	if err := WriteKeyFrameStateHeader(&w, cfg); err != nil {
		t.Fatalf("WriteKeyFrameStateHeader returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}
	first = w.Bytes()

	packet := make([]byte, KeyFrameUncompressedHdrSize+len(first))
	if err := PutFrameTag(packet, true, 0, true, len(first)); err != nil {
		t.Fatalf("PutFrameTag returned error: %v", err)
	}
	if err := PutKeyFrameExtraHeader(packet[FrameTagSize:], 64, 48, 0, 0); err != nil {
		t.Fatalf("PutKeyFrameExtraHeader returned error: %v", err)
	}
	copy(packet[KeyFrameUncompressedHdrSize:], first)
	return packet
}
