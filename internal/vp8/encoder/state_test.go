package encoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
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

func TestWriteKeyFrameStateHeaderParsesSegmentation(t *testing.T) {
	cfg := KeyFrameStateConfig{
		TokenPartition: common.OnePartition,
		BaseQIndex:     20,
		Segmentation:   testSegmentationConfig(),
	}
	packet := keyFrameStatePacket(t, cfg)

	_, state, err := vp8dec.ParseStateHeader(packet, vp8dec.QuantHeader{})
	if err != nil {
		t.Fatalf("ParseStateHeader returned error: %v", err)
	}
	assertParsedSegmentation(t, state.Segmentation)
}

func TestWriteKeyFrameStateHeaderParsesQuantDeltas(t *testing.T) {
	cfg := KeyFrameStateConfig{
		TokenPartition: common.OnePartition,
		BaseQIndex:     2,
		QuantDeltas:    common.QuantDeltas{Y2DC: 2},
	}
	packet := keyFrameStatePacket(t, cfg)

	_, state, err := vp8dec.ParseStateHeader(packet, vp8dec.QuantHeader{})
	if err != nil {
		t.Fatalf("ParseStateHeader returned error: %v", err)
	}
	if state.Quant.BaseQIndex != 2 || state.Quant.Y2DCDelta != 2 {
		t.Fatalf("quant = %+v, want base Q 2 with Y2 DC delta 2", state.Quant)
	}
}

func TestWriteKeyFrameStateHeaderParsesLoopFilterDeltas(t *testing.T) {
	cfg := KeyFrameStateConfig{
		LoopFilterLevel: 17,
		LFDeltaEnabled:  true,
		LFDeltaUpdate:   true,
		RefLFDeltas:     [common.MaxRefLFDeltas]int8{2, 0, -2, -2},
		ModeLFDeltas:    [common.MaxModeLFDeltas]int8{4, -12, 2, 4},
		TokenPartition:  common.OnePartition,
		BaseQIndex:      20,
	}
	packet := keyFrameStatePacket(t, cfg)

	_, state, err := vp8dec.ParseStateHeader(packet, vp8dec.QuantHeader{})
	if err != nil {
		t.Fatalf("ParseStateHeader returned error: %v", err)
	}
	if !state.LoopFilter.DeltaEnabled || !state.LoopFilter.DeltaUpdate {
		t.Fatalf("loop filter delta flags = enabled:%t update:%t, want enabled update", state.LoopFilter.DeltaEnabled, state.LoopFilter.DeltaUpdate)
	}
	if state.LoopFilter.RefDeltas != cfg.RefLFDeltas || state.LoopFilter.ModeDeltas != cfg.ModeLFDeltas {
		t.Fatalf("loop filter deltas = %v/%v, want %v/%v", state.LoopFilter.RefDeltas, state.LoopFilter.ModeDeltas, cfg.RefLFDeltas, cfg.ModeLFDeltas)
	}
}

func TestWriteLoopFilterDeltasCanForceZeroDeltaUpdates(t *testing.T) {
	refDeltas := [common.MaxRefLFDeltas]int8{2, 0, -2, -2}
	modeDeltas := [common.MaxModeLFDeltas]int8{4, -12, 2, 4}
	var w BoolWriter
	buf := make([]byte, 64)
	w.Init(buf)
	writeLoopFilterDeltas(&w, true, true, true, refDeltas, modeDeltas, [common.MaxRefLFDeltas]int8{}, [common.MaxModeLFDeltas]int8{})
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}

	var d boolcoder.Decoder
	if err := d.Init(w.Bytes()); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	if d.ReadBit() != 1 || d.ReadBit() != 1 {
		t.Fatalf("loop-filter enable/update bits did not decode to 1/1")
	}
	if d.ReadBit() != 1 || d.ReadLiteral(6) != 2 || d.ReadBit() != 0 {
		t.Fatalf("positive ref delta did not decode as update/value/sign 1/2/0")
	}
	if d.ReadBit() != 1 || d.ReadLiteral(6) != 0 || d.ReadBit() != 1 {
		t.Fatalf("forced zero ref delta did not decode as update/value/sign 1/0/1")
	}
}

func TestWriteLoopFilterDeltasSuppressesUnchangedDeltaUpdates(t *testing.T) {
	refDeltas := [common.MaxRefLFDeltas]int8{2, 0, -2, -2}
	modeDeltas := [common.MaxModeLFDeltas]int8{4, -2, 2, 4}
	var w BoolWriter
	buf := make([]byte, 64)
	w.Init(buf)
	writeLoopFilterDeltas(&w, true, true, false, refDeltas, modeDeltas, refDeltas, modeDeltas)
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}

	var d boolcoder.Decoder
	if err := d.Init(w.Bytes()); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	if d.ReadBit() != 1 || d.ReadBit() != 1 {
		t.Fatalf("loop-filter enable/update bits did not decode to 1/1")
	}
	for i := 0; i < common.MaxRefLFDeltas+common.MaxModeLFDeltas; i++ {
		if d.ReadBit() != 0 {
			t.Fatalf("delta update bit %d = 1, want 0 for unchanged base", i)
		}
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
	badSegmentation := SegmentationConfig{Enabled: true, UpdateData: true}
	badSegmentation.FeatureEnabled[common.MBLvlAltLF][0] = true
	badSegmentation.FeatureData[common.MBLvlAltLF][0] = -64
	err = WriteKeyFrameStateHeader(&w, KeyFrameStateConfig{Segmentation: badSegmentation})
	if !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("invalid segmentation error = %v, want ErrInvalidPacketConfig", err)
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

func testSegmentationConfig() SegmentationConfig {
	var cfg SegmentationConfig
	cfg.Enabled = true
	cfg.UpdateMap = true
	cfg.UpdateData = true
	cfg.AbsDelta = true
	cfg.FeatureEnabled[common.MBLvlAltQ][0] = true
	cfg.FeatureData[common.MBLvlAltQ][0] = 0
	cfg.FeatureEnabled[common.MBLvlAltQ][1] = true
	cfg.FeatureData[common.MBLvlAltQ][1] = -7
	cfg.FeatureEnabled[common.MBLvlAltLF][2] = true
	cfg.FeatureData[common.MBLvlAltLF][2] = 31
	cfg.TreeProbUpdated[0] = true
	cfg.TreeProbs[0] = 200
	cfg.TreeProbUpdated[2] = true
	cfg.TreeProbs[2] = 77
	return cfg
}

func decoderSegmentationHeader(cfg SegmentationConfig) vp8dec.SegmentationHeader {
	return vp8dec.SegmentationHeader{
		Enabled:     cfg.Enabled,
		UpdateMap:   cfg.UpdateMap,
		UpdateData:  cfg.UpdateData,
		AbsDelta:    cfg.AbsDelta,
		FeatureData: cfg.FeatureData,
		TreeProbs:   segmentationTreeProbs(cfg),
	}
}

func assertParsedSegmentation(t *testing.T, segmentation vp8dec.SegmentationHeader) {
	t.Helper()
	if !segmentation.Enabled || !segmentation.UpdateMap || !segmentation.UpdateData || !segmentation.AbsDelta {
		t.Fatalf("segmentation flags = %+v, want enabled update-map update-data abs", segmentation)
	}
	if got := segmentation.FeatureData[common.MBLvlAltQ][1]; got != -7 {
		t.Fatalf("alt-q segment 1 = %d, want -7", got)
	}
	if got := segmentation.FeatureData[common.MBLvlAltLF][2]; got != 31 {
		t.Fatalf("alt-lf segment 2 = %d, want 31", got)
	}
	if segmentation.TreeProbs != ([common.MBFeatureTreeProbs]uint8{200, 255, 77}) {
		t.Fatalf("tree probs = %v, want [200 255 77]", segmentation.TreeProbs)
	}
}
