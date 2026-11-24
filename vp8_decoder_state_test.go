package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestDecodeInitializesReferenceFrameBuffers(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8test.KeyFramePacketWithPayload(5, 3, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	if d.current.Img.Width != 5 || d.current.Img.Height != 3 {
		t.Fatalf("current visible dimensions = %dx%d, want 5x3", d.current.Img.Width, d.current.Img.Height)
	}
	if d.current.Img.CodedWidth != 16 || d.current.Img.CodedHeight != 16 {
		t.Fatalf("current coded dimensions = %dx%d, want 16x16", d.current.Img.CodedWidth, d.current.Img.CodedHeight)
	}
	if d.lastRef.BufferLen() == 0 || d.goldenRef.BufferLen() == 0 || d.altRef.BufferLen() == 0 {
		t.Fatalf("reference buffers were not initialized")
	}
	if d.mbRows != 1 || d.mbCols != 1 || len(d.modes) != 1 || len(d.tokens) != 1 || len(d.tokenAbove) != 1 {
		t.Fatalf("workspace rows/cols/lens = %dx%d %d/%d/%d, want 1x1 1/1/1", d.mbRows, d.mbCols, len(d.modes), len(d.tokens), len(d.tokenAbove))
	}
	if d.current.Img.CodedWidth < d.mbCols*16 || d.current.Img.CodedHeight < d.mbRows*16 {
		t.Fatalf("coded frame is smaller than macroblock workspace")
	}
}

func TestDecodeParsesStateAndInitializesDequants(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	if d.previousQuant.BaseQIndex != 0 || d.state.Quant.BaseQIndex != 0 {
		t.Fatalf("quant state = %+v/%+v, want base q 0", d.previousQuant, d.state.Quant)
	}
	if d.dequants[0].Y1[0] != 4 || d.dequants[0].Y2[0] != 8 || d.dequants[0].UV[0] != 4 {
		t.Fatalf("segment 0 dequants = Y1:%d Y2:%d UV:%d, want 4/8/4", d.dequants[0].Y1[0], d.dequants[0].Y2[0], d.dequants[0].UV[0])
	}
}

func TestDecodePersistsSegmentationAcrossNoUpdateFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	segmentation := vp8enc.SegmentationConfig{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
	}
	segmentation.FeatureEnabled[vp8common.MBLvlAltQ][1] = true
	segmentation.FeatureData[vp8common.MBLvlAltQ][1] = -8
	for i := range segmentation.TreeProbs {
		segmentation.TreeProbUpdated[i] = true
		segmentation.TreeProbs[i] = 128
	}
	keyModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: 1, YMode: vp8common.DCPred, UVMode: vp8common.DCPred}}
	keyPacket := make([]byte, 4096)
	keyN, err := vp8enc.WriteZeroKeyFrame(keyPacket, 16, 16, vp8enc.KeyFrameStateConfig{
		TokenPartition: vp8common.OnePartition,
		BaseQIndex:     20,
		Segmentation:   segmentation,
	}, keyModes)
	if err != nil {
		t.Fatalf("WriteZeroKeyFrame returned error: %v", err)
	}

	if err := d.Decode(keyPacket[:keyN]); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	if d.modes[0].SegmentID != 1 {
		t.Fatalf("key segment ID = %d, want 1", d.modes[0].SegmentID)
	}
	if d.state.Segmentation.FeatureData[vp8common.MBLvlAltQ][1] != -8 {
		t.Fatalf("key alt-q segment 1 = %d, want -8", d.state.Segmentation.FeatureData[vp8common.MBLvlAltQ][1])
	}

	interCfg := vp8enc.DefaultInterFrameStateConfig(20)
	interCfg.Segmentation = vp8enc.SegmentationConfig{Enabled: true}
	interModes := []vp8enc.InterFrameMacroblockMode{{Mode: vp8common.ZeroMV, MBSkipCoeff: true}}
	interCoeffs := make([]vp8enc.MacroblockCoefficients, 1)
	above := make([]vp8enc.TokenContextPlanes, 1)
	interPacket := make([]byte, 4096)
	interN, err := vp8enc.WriteCoefficientInterFrame(interPacket, 16, 16, interCfg, interModes, interCoeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientInterFrame returned error: %v", err)
	}

	if err := d.Decode(interPacket[:interN]); err != nil {
		t.Fatalf("inter Decode error = %v, want nil", err)
	}
	if d.modes[0].SegmentID != 1 {
		t.Fatalf("inter segment ID = %d, want persisted 1", d.modes[0].SegmentID)
	}
	if d.state.Segmentation.FeatureData[vp8common.MBLvlAltQ][1] != -8 {
		t.Fatalf("inter alt-q segment 1 = %d, want persisted -8", d.state.Segmentation.FeatureData[vp8common.MBLvlAltQ][1])
	}
	if d.state.Segmentation.UpdateMap || d.state.Segmentation.UpdateData {
		t.Fatalf("inter segmentation update flags = map:%v data:%v, want both false", d.state.Segmentation.UpdateMap, d.state.Segmentation.UpdateData)
	}
}

func TestDecodePersistsCoefficientProbabilityUpdates(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithSingleCoefProbabilityUpdate(true, 77))

	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	if d.state.Probability.UpdateCount != 1 || !d.state.Refresh.RefreshEntropyProbs {
		t.Fatalf("probability/refresh = %+v/%t, want one persisted update", d.state.Probability, d.state.Refresh.RefreshEntropyProbs)
	}
	if got := d.frameCoefProbs[0][0][0][0]; got != 77 {
		t.Fatalf("frame coefficient probability = %d, want 77", got)
	}
	if got := d.coefProbs[0][0][0][0]; got != 77 {
		t.Fatalf("persistent coefficient probability = %d, want 77", got)
	}
}

func TestDecodeKeepsTransientCoefficientProbabilityUpdatesFrameLocal(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithSingleCoefProbabilityUpdate(false, 77))

	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	if d.state.Probability.UpdateCount != 1 || d.state.Refresh.RefreshEntropyProbs {
		t.Fatalf("probability/refresh = %+v/%t, want one transient update", d.state.Probability, d.state.Refresh.RefreshEntropyProbs)
	}
	if got := d.frameCoefProbs[0][0][0][0]; got != 77 {
		t.Fatalf("frame coefficient probability = %d, want 77", got)
	}
	if got := d.coefProbs[0][0][0][0]; got != vp8tables.DefaultCoefProbs[0][0][0][0] {
		t.Fatalf("persistent coefficient probability = %d, want default %d", got, vp8tables.DefaultCoefProbs[0][0][0][0])
	}
}

func TestDecodeMalformedFrameDoesNotCommitProbabilityUpdates(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	good := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithSingleCoefProbabilityUpdate(true, 77))
	if err := d.Decode(good); err != nil {
		t.Fatalf("good Decode returned error: %v", err)
	}
	badFirst := vp8test.FirstPartitionWithSingleCoefProbabilityUpdate(true, 99)
	bad := vp8test.KeyFramePacket(16, 16, len(badFirst), 0, true)
	bad = append(bad, badFirst...)
	bad = append(bad, 0)

	if err := d.Decode(bad); err == nil {
		t.Fatalf("malformed Decode returned nil error")
	}

	if got := d.coefProbs[0][0][0][0]; got != 77 {
		t.Fatalf("persistent coefficient probability = %d, want previous successful value 77", got)
	}
	if got := d.previousQuant.BaseQIndex; got != 0 {
		t.Fatalf("previous quant base = %d, want previous successful value 0", got)
	}
}

func TestDecodeParsesPartitionLayout(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	if d.partitions.TokenCount != 1 || len(d.partitions.First) != 200 || len(d.partitions.Tokens[0]) == 0 {
		t.Fatalf("partition layout = first:%d tokenCount:%d token0:%d, want nonempty one-partition layout", len(d.partitions.First), d.partitions.TokenCount, len(d.partitions.Tokens[0]))
	}
	if d.frameHeader.FirstPartitionSize != 200 {
		t.Fatalf("frame first partition = %d, want 200", d.frameHeader.FirstPartitionSize)
	}
	if d.modeReader.Err() != nil || d.modeReader.Corrupted() {
		t.Fatalf("mode reader error/corrupted = %v/%v, want clean reader", d.modeReader.Err(), d.modeReader.Corrupted())
	}
}
