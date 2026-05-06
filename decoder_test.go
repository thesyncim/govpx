package libgopx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/libgopx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/libgopx/internal/vp8/tables"
)

func TestNewVP8DecoderValidation(t *testing.T) {
	_, err := NewVP8Decoder(DecoderOptions{Threads: -1})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecodeRequiresInitialKeyFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8InterFramePacket(0, 0, true))
	if !errors.Is(err, ErrNeedKeyFrame) {
		t.Fatalf("error = %v, want ErrNeedKeyFrame", err)
	}
}

func TestDecodeQueuesSupportedKeyFrameAfterValidation(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{MaxWidth: 640, MaxHeight: 480})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.DecodeWithPTS(vp8KeyFramePacketWithPayload(320, 240, 200, 0, true), 44)
	if err != nil {
		t.Fatalf("DecodeWithPTS error = %v, want nil", err)
	}
	if d.lastInfo.Width != 320 || d.lastInfo.Height != 240 || d.lastInfo.PTS != 44 {
		t.Fatalf("lastInfo = %+v, want validated frame metadata", d.lastInfo)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if frame.Width != 320 || frame.Height != 240 || frame.YStride == 0 {
		t.Fatalf("frame = %+v, want decoded 320x240 frame", frame)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("NextFrame returned the same frame twice")
	}
}

func TestDecodeInvisibleKeyFrameUpdatesStateWithoutOutput(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.DecodeWithPTS(vp8KeyFramePacketWithPayload(16, 16, 200, 0, false), 44)
	if err != nil {
		t.Fatalf("DecodeWithPTS error = %v, want nil", err)
	}
	if d.needKey {
		t.Fatalf("needKey = true, want false after invisible keyframe")
	}
	if d.lastInfo.ShowFrame || d.lastInfo.PTS != 44 {
		t.Fatalf("lastInfo = %+v, want invisible frame metadata", d.lastInfo)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("NextFrame returned invisible frame")
	}
}

func TestDecodeOutputsLoopFilteredKeyFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(1))

	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	if d.loopInfo.MBLimit[1] == 0 || d.loopInfo.BLimit[1] == 0 {
		t.Fatalf("loop filter tables were not initialized")
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("NextFrame returned no frame for filtered output")
	}
}

func TestDecodePostProcessOutputsPostFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{PostProcess: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))

	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if len(frame.Y) == 0 || len(d.post.Img.Y) == 0 || len(d.current.Img.Y) == 0 {
		t.Fatalf("decoded frame buffers are empty")
	}
	if &frame.Y[0] != &d.post.Img.Y[0] {
		t.Fatalf("NextFrame did not return decoder postprocess buffer")
	}
	if &frame.Y[0] == &d.current.Img.Y[0] {
		t.Fatalf("postprocessed output aliases reconstruction buffer")
	}
}

func TestDecodeIntoPostProcessCopiesPostFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{PostProcess: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newTestImage(16, 16)
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))

	info, err := d.DecodeInto(packet, &dst)
	if err != nil {
		t.Fatalf("DecodeInto error = %v, want nil", err)
	}
	if !info.ShowFrame || info.Width != 16 || info.Height != 16 {
		t.Fatalf("FrameInfo = %+v, want visible 16x16 frame", info)
	}
	if !publicImageEqualVP8(dst, &d.post.Img) {
		t.Fatalf("DecodeInto output does not match decoder postprocess buffer")
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto queued a frame for NextFrame")
	}
}

func TestDecodeOutputsSupportedVersionKeyFrames(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		d, err := NewVP8Decoder(DecoderOptions{})
		if err != nil {
			t.Fatalf("NewVP8Decoder returned error: %v", err)
		}
		packet := vp8KeyFramePacketWithPayload(16, 16, 200, version, true)

		err = d.Decode(packet)
		if err != nil {
			t.Fatalf("version %d Decode error = %v, want nil", version, err)
		}
		frame, ok := d.NextFrame()
		if !ok {
			t.Fatalf("version %d NextFrame returned no frame", version)
		}
		if frame.Width != 16 || frame.Height != 16 {
			t.Fatalf("version %d frame dimensions = %dx%d, want 16x16", version, frame.Width, frame.Height)
		}
	}
}

func TestDecodeSkipsLoopFilterForNoLPFVersion(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartitionProfile(16, 16, 2, vp8FirstPartitionWithLoopFilterLevel(1))

	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	if d.loopInfo.MBLimit[1] != 0 || d.loopInfo.BLimit[1] != 0 {
		t.Fatalf("loop filter tables = mb:%d b:%d, want skipped", d.loopInfo.MBLimit[1], d.loopInfo.BLimit[1])
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("NextFrame returned no frame for no-lpf version")
	}
}

func TestDecodeRejectsReservedVersion(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 4, true))
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("error = %v, want ErrUnsupportedFeature", err)
	}
}

func TestDecodeRejectsConfiguredSizeLimits(t *testing.T) {
	tests := []struct {
		name string
		opts DecoderOptions
	}{
		{name: "width", opts: DecoderOptions{MaxWidth: 15}},
		{name: "height", opts: DecoderOptions{MaxHeight: 15}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewVP8Decoder(tt.opts)
			if err != nil {
				t.Fatalf("NewVP8Decoder returned error: %v", err)
			}

			err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
			if !errors.Is(err, ErrUnsupportedFeature) {
				t.Fatalf("Decode error = %v, want ErrUnsupportedFeature", err)
			}
		})
	}
}

func TestDecodeRejectsConfiguredResolutionChange(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{RejectResolutionChange: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("initial Decode returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(32, 16, 200, 0, true))
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("resolution-change Decode error = %v, want ErrUnsupportedFeature", err)
	}
}

func TestDecodeOutputsMacroblockSkipKeyFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithMacroblockSkip(128))

	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	if len(d.modes) != 1 || !d.modes[0].MBSkipCoeff {
		t.Fatalf("mode skip = %+v, want skipped macroblock", d.modes)
	}
	if d.tokens[0] != (vp8dec.MacroblockTokens{}) {
		t.Fatalf("tokens[0] = %+v, want zero tokens for skipped macroblock", d.tokens[0])
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("NextFrame returned no frame for skipped keyframe")
	}
}

func TestDecodeInitializesReferenceFrameBuffers(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(5, 3, 200, 0, true))
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

	err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
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
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithSingleCoefProbabilityUpdate(true, 77))

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
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithSingleCoefProbabilityUpdate(false, 77))

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
	good := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithSingleCoefProbabilityUpdate(true, 77))
	if err := d.Decode(good); err != nil {
		t.Fatalf("good Decode returned error: %v", err)
	}
	badFirst := vp8FirstPartitionWithSingleCoefProbabilityUpdate(true, 99)
	bad := vp8KeyFramePacket(16, 16, len(badFirst), 0, true)
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

	err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
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

func vp8KeyFramePacketWithFirstPartition(width int, height int, first []byte) []byte {
	return vp8KeyFramePacketWithFirstPartitionProfile(width, height, 0, first)
}

func vp8KeyFramePacketWithFirstPartitionProfile(width int, height int, profile int, first []byte) []byte {
	packet := vp8KeyFramePacket(width, height, len(first), profile, true)
	packet = append(packet, first...)
	return append(packet, make([]byte, 10000)...)
}

func vp8InterFramePacketWithFirstPartition(first []byte) []byte {
	packet := vp8InterFramePacket(len(first), 0, true)
	packet = append(packet, first...)
	return append(packet, make([]byte, 10000)...)
}

func vp8InterFirstPartitionLastZeroMV() []byte {
	var w vp8TestBoolWriter
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
	w.writeBool(0, 128)
	w.writeLiteral(128, 8)
	w.writeLiteral(128, 8)
	w.writeLiteral(128, 8)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	for component := 0; component < 2; component++ {
		for i := 0; i < vp8tables.MVPCount; i++ {
			w.writeBool(0, vp8tables.MVUpdateProbs[component][i])
		}
	}

	w.writeBool(1, 128)
	w.writeBool(0, 128)
	w.writeBool(0, vp8tables.InterModeContexts[0][0])

	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func vp8FirstPartitionWithSingleCoefProbabilityUpdate(refreshEntropy bool, value uint8) []byte {
	var w vp8TestBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
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
	if refreshEntropy {
		w.writeBool(1, 128)
	} else {
		w.writeBool(0, 128)
	}

	first := true
	for block := 0; block < vp8tables.BlockTypes; block++ {
		for band := 0; band < vp8tables.CoefBands; band++ {
			for ctx := 0; ctx < vp8tables.PrevCoefContexts; ctx++ {
				for node := 0; node < vp8tables.EntropyNodes; node++ {
					if first {
						w.writeBool(1, vp8tables.CoefUpdateProbs[block][band][ctx][node])
						w.writeLiteral(uint32(value), 8)
						first = false
					} else {
						w.writeBool(0, vp8tables.CoefUpdateProbs[block][band][ctx][node])
					}
				}
			}
		}
	}

	w.writeBool(0, 128)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func vp8FirstPartitionWithLoopFilterLevel(level uint8) []byte {
	var w vp8TestBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(uint32(level), 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeLiteral(0, 7)
	for i := 0; i < 5; i++ {
		w.writeBool(0, 128)
	}
	w.writeBool(0, 128)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func vp8FirstPartitionWithMacroblockSkip(probSkipFalse uint8) []byte {
	var w vp8TestBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
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
	writeNoCoefficientProbabilityUpdates(&w)
	w.writeBool(1, 128)
	w.writeLiteral(uint32(probSkipFalse), 8)
	w.writeBool(1, probSkipFalse)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func writeNoCoefficientProbabilityUpdates(w *vp8TestBoolWriter) {
	for block := 0; block < vp8tables.BlockTypes; block++ {
		for band := 0; band < vp8tables.CoefBands; band++ {
			for ctx := 0; ctx < vp8tables.PrevCoefContexts; ctx++ {
				for node := 0; node < vp8tables.EntropyNodes; node++ {
					w.writeBool(0, vp8tables.CoefUpdateProbs[block][band][ctx][node])
				}
			}
		}
	}
}

type vp8TestBoolWriter struct {
	low   uint32
	rng   uint32
	count int
	buf   []byte
}

func (w *vp8TestBoolWriter) init() {
	w.low = 0
	w.rng = 255
	w.count = -24
	w.buf = w.buf[:0]
}

func (w *vp8TestBoolWriter) writeLiteral(value uint32, bits int) {
	for bit := bits - 1; bit >= 0; bit-- {
		w.writeBool(uint8((value>>uint(bit))&1), 128)
	}
}

func (w *vp8TestBoolWriter) finish() []byte {
	for i := 0; i < 32; i++ {
		w.writeBool(0, 128)
	}
	return w.buf
}

func (w *vp8TestBoolWriter) writeBool(bit uint8, probability uint8) {
	split := uint32(1 + (((w.rng - 1) * uint32(probability)) >> 8))

	rng := split
	low := w.low
	if bit != 0 {
		low += split
		rng = w.rng - split
	}

	shift := int(vp8tables.BoolNorm[byte(rng)])
	rng <<= uint(shift)
	count := w.count + shift

	if count >= 0 {
		offset := shift - count
		if ((low << uint(offset-1)) & 0x80000000) != 0 {
			for i := len(w.buf) - 1; i >= 0; i-- {
				if w.buf[i] != 0xff {
					w.buf[i]++
					break
				}
				w.buf[i] = 0
			}
		}

		w.buf = append(w.buf, byte((low>>uint(24-offset))&0xff))
		shift = count
		low = uint32((uint64(low) << uint(offset)) & 0xffffff)
		count -= 8
	}

	low <<= uint(shift)
	w.low = low
	w.rng = rng
	w.count = count
}

func assertCodedBordersExtended(t *testing.T, img *vp8common.Image) {
	t.Helper()

	codedUVWidth := (img.CodedWidth + 1) >> 1
	codedUVHeight := (img.CodedHeight + 1) >> 1

	yRightEdge := img.Y[img.CodedWidth-1]
	if got := img.Y[img.CodedWidth]; got != yRightEdge {
		t.Fatalf("first Y right border = %d, want coded edge %d", got, yRightEdge)
	}
	yBottomEdge := img.Y[(img.CodedHeight-1)*img.YStride+img.CodedWidth-1]
	if got := img.YFull[img.YOrigin+img.CodedHeight*img.YStride+img.CodedWidth-1]; got != yBottomEdge {
		t.Fatalf("first Y bottom border = %d, want coded edge %d", got, yBottomEdge)
	}

	uRightEdge := img.U[codedUVWidth-1]
	if got := img.U[codedUVWidth]; got != uRightEdge {
		t.Fatalf("first U right border = %d, want coded edge %d", got, uRightEdge)
	}
	uBottomEdge := img.U[(codedUVHeight-1)*img.UStride+codedUVWidth-1]
	if got := img.UFull[img.UOrigin+codedUVHeight*img.UStride+codedUVWidth-1]; got != uBottomEdge {
		t.Fatalf("first U bottom border = %d, want coded edge %d", got, uBottomEdge)
	}

	vRightEdge := img.V[codedUVWidth-1]
	if got := img.V[codedUVWidth]; got != vRightEdge {
		t.Fatalf("first V right border = %d, want coded edge %d", got, vRightEdge)
	}
	vBottomEdge := img.V[(codedUVHeight-1)*img.VStride+codedUVWidth-1]
	if got := img.VFull[img.VOrigin+codedUVHeight*img.VStride+codedUVWidth-1]; got != vBottomEdge {
		t.Fatalf("first V bottom border = %d, want coded edge %d", got, vBottomEdge)
	}
}

func fillVP8Image(img *vp8common.Image, value byte) {
	for i := range img.Y {
		img.Y[i] = value
	}
	for i := range img.U {
		img.U[i] = value
	}
	for i := range img.V {
		img.V[i] = value
	}
}

func newTestImage(width int, height int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}

func publicImageEqualVP8(got Image, want *vp8common.Image) bool {
	if want == nil || got.Width != want.Width || got.Height != want.Height {
		return false
	}
	uvWidth := (want.Width + 1) >> 1
	uvHeight := (want.Height + 1) >> 1
	return planeEqual(got.Y, got.YStride, want.Y, want.YStride, want.Width, want.Height) &&
		planeEqual(got.U, got.UStride, want.U, want.UStride, uvWidth, uvHeight) &&
		planeEqual(got.V, got.VStride, want.V, want.VStride, uvWidth, uvHeight)
}

func planeEqual(a []byte, aStride int, b []byte, bStride int, width int, height int) bool {
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			if a[row*aStride+col] != b[row*bStride+col] {
				return false
			}
		}
	}
	return true
}

func TestDecodeParsesKeyFrameModeGrid(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(17, 17, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	if len(d.modes) != 4 {
		t.Fatalf("modes len = %d, want 4", len(d.modes))
	}
	for i, mode := range d.modes {
		if mode.Mode != vp8common.BPred || mode.UVMode != vp8common.DCPred || !mode.Is4x4 {
			t.Fatalf("mode[%d] = %+v, want keyframe BPred/DC 4x4", i, mode)
		}
		for block, blockMode := range mode.BModes {
			if blockMode != vp8common.BDCPred {
				t.Fatalf("mode[%d].BModes[%d] = %d, want BDCPred", i, block, blockMode)
			}
		}
	}
}

func TestDecodeParsesInterModeGrid(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	fillVP8Image(&d.lastRef.Img, 77)
	packet := vp8InterFramePacketWithFirstPartition(vp8InterFirstPartitionLastZeroMV())

	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("inter Decode error = %v, want nil", err)
	}
	if len(d.modes) != 1 {
		t.Fatalf("modes len = %d, want 1", len(d.modes))
	}
	if d.modes[0].RefFrame != vp8common.LastFrame || d.modes[0].Mode != vp8common.ZeroMV || !d.modes[0].MV.IsZero() {
		t.Fatalf("inter mode = %+v, want LAST/ZEROMV", d.modes[0])
	}
	for block, coeffs := range d.tokens[0].QCoeff {
		if coeffs != ([16]int16{}) {
			t.Fatalf("tokens[0].QCoeff[%d] = %v, want zero coefficients", block, coeffs)
		}
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no inter frame")
	}
	if frame.Width != 16 || frame.Height != 16 {
		t.Fatalf("inter frame dimensions = %dx%d, want 16x16", frame.Width, frame.Height)
	}
	if frame.Y[0] != 77 || frame.U[0] != 77 || frame.V[0] != 77 {
		t.Fatalf("inter frame samples = %d/%d/%d, want copied last ref 77", frame.Y[0], frame.U[0], frame.V[0])
	}
}

func TestDecodeParsesTokenGrid(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	if len(d.tokens) != 1 {
		t.Fatalf("tokens len = %d, want 1", len(d.tokens))
	}
	if d.tokens[0] != (vp8dec.MacroblockTokens{}) {
		t.Fatalf("tokens[0] = %+v, want zero token macroblock", d.tokens[0])
	}
	if d.tokenAbove[0] != (vp8dec.EntropyContextPlanes{}) {
		t.Fatalf("tokenAbove[0] = %+v, want zero context", d.tokenAbove[0])
	}
}

func TestDecodeReconstructsKeyFrameIntraGridInCurrent(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	yChecks := []struct {
		row  int
		col  int
		want byte
	}{
		{row: 0, col: 0, want: 128},
		{row: 4, col: 0, want: 129},
		{row: 15, col: 15, want: 129},
	}
	for _, check := range yChecks {
		if got := d.current.Img.Y[check.row*d.current.Img.YStride+check.col]; got != check.want {
			t.Fatalf("Y[%d,%d] = %d, want %d", check.row, check.col, got, check.want)
		}
	}
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			if got := d.current.Img.U[row*d.current.Img.UStride+col]; got != 128 {
				t.Fatalf("U[%d,%d] = %d, want 128", row, col, got)
			}
			if got := d.current.Img.V[row*d.current.Img.VStride+col]; got != 128 {
				t.Fatalf("V[%d,%d] = %d, want 128", row, col, got)
			}
		}
	}
}

func TestDecodeExtendsKeyFrameCodedBorders(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(5, 3, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	assertCodedBordersExtended(t, &d.current.Img)
	assertCodedBordersExtended(t, &d.lastRef.Img)
	assertCodedBordersExtended(t, &d.goldenRef.Img)
	assertCodedBordersExtended(t, &d.altRef.Img)
}

func TestDecodeRefreshesKeyFrameReferences(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	if d.lastRef.Img.Y[0] != d.current.Img.Y[0] || d.goldenRef.Img.Y[0] != d.current.Img.Y[0] || d.altRef.Img.Y[0] != d.current.Img.Y[0] {
		t.Fatalf("reference Y[0] values = %d/%d/%d, want current %d", d.lastRef.Img.Y[0], d.goldenRef.Img.Y[0], d.altRef.Img.Y[0], d.current.Img.Y[0])
	}
	if d.lastRef.Img.U[0] != d.current.Img.U[0] || d.goldenRef.Img.V[0] != d.current.Img.V[0] || d.altRef.Img.V[0] != d.current.Img.V[0] {
		t.Fatalf("reference chroma was not refreshed from current")
	}
}

func TestRefreshReferencesCopiesExistingBuffersInVP8Order(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.ensureFrameBuffers(StreamInfo{Width: 16, Height: 16, KeyFrame: true}); err != nil {
		t.Fatalf("ensureFrameBuffers returned error: %v", err)
	}
	fillVP8Image(&d.current.Img, 40)
	fillVP8Image(&d.lastRef.Img, 10)
	fillVP8Image(&d.goldenRef.Img, 20)
	fillVP8Image(&d.altRef.Img, 30)
	d.state.Refresh = vp8dec.RefreshHeader{
		CopyBufferToAltRef: 2,
		CopyBufferToGolden: 1,
	}

	d.refreshReferences()

	if got := d.altRef.Img.Y[0]; got != 20 {
		t.Fatalf("alt Y[0] = %d, want old golden 20", got)
	}
	if got := d.goldenRef.Img.Y[0]; got != 10 {
		t.Fatalf("golden Y[0] = %d, want last 10", got)
	}
	if got := d.lastRef.Img.Y[0]; got != 10 {
		t.Fatalf("last Y[0] = %d, want unchanged 10", got)
	}
}

func TestRefreshReferencesCurrentFrameOverridesCopies(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.ensureFrameBuffers(StreamInfo{Width: 16, Height: 16, KeyFrame: true}); err != nil {
		t.Fatalf("ensureFrameBuffers returned error: %v", err)
	}
	fillVP8Image(&d.current.Img, 40)
	fillVP8Image(&d.lastRef.Img, 10)
	fillVP8Image(&d.goldenRef.Img, 20)
	fillVP8Image(&d.altRef.Img, 30)
	d.state.Refresh = vp8dec.RefreshHeader{
		CopyBufferToGolden: 1,
		RefreshGolden:      true,
		RefreshAltRef:      true,
		RefreshLast:        true,
	}

	d.refreshReferences()

	if d.goldenRef.Img.Y[0] != 40 || d.altRef.Img.Y[0] != 40 || d.lastRef.Img.Y[0] != 40 {
		t.Fatalf("reference Y[0] = %d/%d/%d, want all current 40", d.goldenRef.Img.Y[0], d.altRef.Img.Y[0], d.lastRef.Img.Y[0])
	}
}

func TestDecodeRejectsMissingTokenPartition(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacket(16, 16, 200, 0, true)
	packet = append(packet, make([]byte, 200)...)

	err = d.Decode(packet)
	if !errors.Is(err, ErrInvalidData) {
		t.Fatalf("Decode error = %v, want ErrInvalidData", err)
	}
}

func TestDecodeErrorResilientConcealsCorruptInterFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorResilient: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	keyPacket := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	previous := d.lastRef.Img.Y[0]
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}

	err = d.DecodeWithPTS(vp8InterFramePacket(0, 0, true), 99)
	if err != nil {
		t.Fatalf("corrupt inter DecodeWithPTS error = %v, want nil concealment", err)
	}
	if !d.lastInfo.Corrupted || d.lastInfo.PTS != 99 || d.lastInfo.Width != 16 || d.lastInfo.Height != 16 {
		t.Fatalf("lastInfo = %+v, want corrupted concealed 16x16 PTS 99", d.lastInfo)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("concealed NextFrame returned no frame")
	}
	if frame.Y[0] != previous {
		t.Fatalf("concealed Y[0] = %d, want previous reference %d", frame.Y[0], previous)
	}
}

func TestDecodeIntoErrorResilientConcealsCorruptInterFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorResilient: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	keyPacket := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	previous := d.lastRef.Img.Y[0]
	dst := newTestImage(16, 16)
	fillImage(dst, 7, 8, 9)

	info, err := d.DecodeIntoWithPTS(vp8InterFramePacket(0, 0, true), &dst, 101)
	if err != nil {
		t.Fatalf("corrupt inter DecodeIntoWithPTS error = %v, want nil concealment", err)
	}
	if !info.Corrupted || info.PTS != 101 || info.Width != 16 || info.Height != 16 {
		t.Fatalf("FrameInfo = %+v, want corrupted concealed 16x16 PTS 101", info)
	}
	if dst.Y[0] != previous {
		t.Fatalf("concealed dst Y[0] = %d, want previous reference %d", dst.Y[0], previous)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto concealment queued a NextFrame output")
	}
}

func TestDecodePostProcessConcealsCorruptInterFrameIntoPostFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorResilient: true, PostProcess: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	keyPacket := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}

	err = d.DecodeWithPTS(vp8InterFramePacket(0, 0, true), 99)
	if err != nil {
		t.Fatalf("corrupt inter DecodeWithPTS error = %v, want nil concealment", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("concealed NextFrame returned no frame")
	}
	if len(frame.Y) == 0 || len(d.post.Img.Y) == 0 || len(d.lastRef.Img.Y) == 0 {
		t.Fatalf("decoded frame buffers are empty")
	}
	if &frame.Y[0] != &d.post.Img.Y[0] {
		t.Fatalf("concealed postprocess output did not use decoder postprocess buffer")
	}
	if &frame.Y[0] == &d.lastRef.Img.Y[0] {
		t.Fatalf("concealed postprocess output aliases reference buffer")
	}
}

func TestDecodeIntoPostProcessConcealsCorruptInterFrameIntoPostFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorResilient: true, PostProcess: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	keyPacket := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithLoopFilterLevel(63))
	if err := d.Decode(keyPacket); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	dst := newTestImage(16, 16)

	info, err := d.DecodeIntoWithPTS(vp8InterFramePacket(0, 0, true), &dst, 101)
	if err != nil {
		t.Fatalf("corrupt inter DecodeIntoWithPTS error = %v, want nil concealment", err)
	}
	if !info.Corrupted || info.PTS != 101 || info.Width != 16 || info.Height != 16 {
		t.Fatalf("FrameInfo = %+v, want corrupted concealed 16x16 PTS 101", info)
	}
	if !publicImageEqualVP8(dst, &d.post.Img) {
		t.Fatalf("concealed DecodeInto output does not match decoder postprocess buffer")
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto concealment queued a NextFrame output")
	}
}

func TestDecodeDoesNotConcealCorruptInterFrameWhenDisabled(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("key Decode error = %v, want nil", err)
	}
	if err := d.Decode(vp8InterFramePacket(0, 0, true)); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("corrupt inter error = %v, want ErrInvalidData", err)
	}
}

func TestDecodeRejectsTruncatedStateHeader(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacket(16, 16, 200, 0, true))
	if !errors.Is(err, ErrInvalidData) {
		t.Fatalf("Decode error = %v, want ErrInvalidData", err)
	}
}

func TestReconstructFrameInvalidInterModeReturnsInvalidData(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.ensureFrameBuffers(StreamInfo{Width: 16, Height: 16, KeyFrame: true}); err != nil {
		t.Fatalf("ensureFrameBuffers returned error: %v", err)
	}
	d.modes[0] = vp8dec.MacroblockMode{
		RefFrame: vp8common.LastFrame,
		Mode:     vp8common.MBPredictionMode(99),
	}

	err = d.reconstructFrame(StreamInfo{Profile: 0})
	if !errors.Is(err, ErrInvalidData) {
		t.Fatalf("reconstructFrame error = %v, want ErrInvalidData", err)
	}
}

func TestDecodeReusesReferenceFrameBuffers(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	_ = d.Decode(packet)
	firstY := &d.current.Img.Y[0]
	firstLastY := &d.lastRef.Img.Y[0]
	firstModes := &d.modes[0]
	firstTokens := &d.tokens[0]

	_ = d.Decode(packet)

	if &d.current.Img.Y[0] != firstY {
		t.Fatalf("current frame buffer was reallocated for same resolution")
	}
	if &d.lastRef.Img.Y[0] != firstLastY {
		t.Fatalf("last reference buffer was reallocated for same resolution")
	}
	if &d.modes[0] != firstModes || &d.tokens[0] != firstTokens {
		t.Fatalf("macroblock workspace was reallocated for same resolution")
	}
}

func TestDecodeWorkspaceTracksMacroblockGrid(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	_ = d.Decode(vp8KeyFramePacketWithPayload(17, 17, 200, 0, true))

	if d.mbRows != 2 || d.mbCols != 2 {
		t.Fatalf("workspace grid = %dx%d, want 2x2", d.mbRows, d.mbCols)
	}
	if len(d.modes) != 4 || len(d.tokens) != 4 || len(d.tokenAbove) != 2 {
		t.Fatalf("workspace lengths = %d/%d/%d, want 4/4/2", len(d.modes), len(d.tokens), len(d.tokenAbove))
	}
}

func TestDecodeIntoRejectsNilImage(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	_, err = d.DecodeInto(vp8KeyFramePacket(16, 16, 0, 0, true), nil)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecodeIntoCopiesSupportedKeyFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newTestImage(16, 16)

	info, err := d.DecodeIntoWithPTS(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true), &dst, 88)
	if err != nil {
		t.Fatalf("DecodeIntoWithPTS error = %v, want nil", err)
	}
	if info.Width != 16 || info.Height != 16 || !info.KeyFrame || info.PTS != 88 {
		t.Fatalf("FrameInfo = %+v, want 16x16 keyframe PTS 88", info)
	}
	if got := dst.Y[0]; got != 128 {
		t.Fatalf("dst Y[0] = %d, want 128", got)
	}
	if got := dst.U[0]; got != 128 {
		t.Fatalf("dst U[0] = %d, want 128", got)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto queued a frame for NextFrame")
	}
}

func TestDecodeIntoInvisibleFrameDoesNotCopyOutput(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newTestImage(16, 16)
	fillImage(dst, 7, 8, 9)

	info, err := d.DecodeIntoWithPTS(vp8KeyFramePacketWithPayload(16, 16, 200, 0, false), &dst, 88)
	if err != nil {
		t.Fatalf("DecodeIntoWithPTS error = %v, want nil", err)
	}
	if info.ShowFrame || info.PTS != 88 {
		t.Fatalf("FrameInfo = %+v, want invisible PTS 88", info)
	}
	if dst.Y[0] != 7 || dst.U[0] != 8 || dst.V[0] != 9 {
		t.Fatalf("dst samples = %d/%d/%d, want unchanged 7/8/9", dst.Y[0], dst.U[0], dst.V[0])
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto queued invisible frame")
	}
}

func TestDecodeIntoRejectsInvalidImage(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := Image{Width: 16, Height: 16}

	_, err = d.DecodeInto(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true), &dst)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("DecodeInto error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecoderHotPathAllocs(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithPayload(64, 64, 200, 0, true)
	dst := newTestImage(64, 64)

	tests := []struct {
		name string
		fn   func()
	}{
		{name: "Decode", fn: func() { _ = d.Decode(packet) }},
		{name: "DecodeWithPTS", fn: func() { _ = d.DecodeWithPTS(packet, 123) }},
		{name: "DecodeInto", fn: func() { _, _ = d.DecodeInto(packet, &dst) }},
		{name: "DecodeIntoWithPTS", fn: func() { _, _ = d.DecodeIntoWithPTS(packet, &dst, 123) }},
		{name: "NextFrame", fn: func() { _, _ = d.NextFrame() }},
		{name: "Reset", fn: func() { d.Reset() }},
	}

	for _, tt := range tests {
		allocs := testing.AllocsPerRun(1000, tt.fn)
		if allocs != 0 {
			t.Fatalf("%s allocs = %v, want 0", tt.name, allocs)
		}
	}

	d.closed = false
	allocs := testing.AllocsPerRun(1000, func() {
		d.closed = false
		_ = d.Close()
	})
	if allocs != 0 {
		t.Fatalf("Close allocs = %v, want 0", allocs)
	}
}
