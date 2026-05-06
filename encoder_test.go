package libgopx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/libgopx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/libgopx/internal/vp8/tables"
)

func TestNewVP8EncoderValidation(t *testing.T) {
	_, err := NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30})
	if !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("error = %v, want ErrInvalidBitrate", err)
	}

	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, MinQuantizer: 60, MaxQuantizer: 4})
	if !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("error = %v, want ErrInvalidQuantizer", err)
	}

	_, err = NewVP8Encoder(EncoderOptions{Width: 0, Height: 480, FPS: 30, TargetBitrateKbps: 1200})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}

	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, TokenPartitions: 4})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("token partition error = %v, want ErrInvalidConfig", err)
	}
}

func TestEncoderRateControlBitsPerFrame(t *testing.T) {
	e := newTestEncoder(t)

	if e.rc.bitsPerFrame != 40000 {
		t.Fatalf("bitsPerFrame = %d, want 40000", e.rc.bitsPerFrame)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FPS: 60}); err != nil {
		t.Fatalf("SetRealtimeTarget returned error: %v", err)
	}
	if e.rc.bitsPerFrame != 20000 {
		t.Fatalf("bitsPerFrame = %d, want 20000", e.rc.bitsPerFrame)
	}
	if err := e.SetBitrateKbps(600); err != nil {
		t.Fatalf("SetBitrateKbps returned error: %v", err)
	}
	if e.rc.bitsPerFrame != 10000 {
		t.Fatalf("bitsPerFrame = %d, want 10000", e.rc.bitsPerFrame)
	}
}

func TestEncodeIntoUsesKeyFrameBoostedTargetBits(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !key.KeyFrame || key.FrameTargetBits != e.rc.bitsPerFrame*keyFrameTargetBoost {
		t.Fatalf("key target = key:%t bits:%d, want boosted %d", key.KeyFrame, key.FrameTargetBits, e.rc.bitsPerFrame*keyFrameTargetBoost)
	}

	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame || inter.FrameTargetBits != e.rc.bitsPerFrame {
		t.Fatalf("inter target = key:%t bits:%d, want inter target %d", inter.KeyFrame, inter.FrameTargetBits, e.rc.bitsPerFrame)
	}
}

func TestEncodeIntoWritesConfiguredTokenPartitions(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		TokenPartitions:     int(vp8common.EightPartition),
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if got := packetTokenPartition(t, key.Data); got != vp8common.EightPartition {
		t.Fatalf("key token partition = %d, want eight", got)
	}

	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want inter frame")
	}
	if got := packetTokenPartition(t, inter.Data); got != vp8common.EightPartition {
		t.Fatalf("inter token partition = %d, want eight", got)
	}
}

func TestEncodeIntoUpdatesRateControlAfterFrame(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	initialQuantizer := e.rc.currentQuantizer
	result, err := e.EncodeInto(make([]byte, 4096), testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	if e.rc.rollingActualBits != result.SizeBytes*8 || e.rc.rollingTargetBits != result.FrameTargetBits {
		t.Fatalf("rolling bits = actual:%d target:%d, want %d/%d", e.rc.rollingActualBits, e.rc.rollingTargetBits, result.SizeBytes*8, result.FrameTargetBits)
	}
	if result.BufferLevelBits != e.rc.bufferLevelBits {
		t.Fatalf("result buffer = %d, want rc buffer %d", result.BufferLevelBits, e.rc.bufferLevelBits)
	}
	if e.rc.currentQuantizer <= initialQuantizer {
		t.Fatalf("currentQuantizer = %d, want above initial %d after overshoot", e.rc.currentQuantizer, initialQuantizer)
	}
	if e.rc.framesSinceKeyframe != 0 {
		t.Fatalf("framesSinceKeyframe = %d, want 0 after keyframe", e.rc.framesSinceKeyframe)
	}
}

func TestEncodeIntoRetriesQuantizerBeforeCommitOnOvershoot(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := rateControlTestFrame(32, 32, 0)
	packet := make([]byte, 16384)

	result, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	if result.Quantizer <= 4 {
		t.Fatalf("result quantizer = %d, want retry above initial 4", result.Quantizer)
	}
	if got := packetBaseQIndex(t, result.Data); got != result.Quantizer {
		t.Fatalf("packet base q = %d, want result quantizer %d", got, result.Quantizer)
	}
	if e.rc.lastQuantizer != result.Quantizer {
		t.Fatalf("last quantizer = %d, want committed frame quantizer %d", e.rc.lastQuantizer, result.Quantizer)
	}
	decoded := decodeSingleFrame(t, result.Data)
	assertImagesEqual(t, "retried current", decoded, publicImageFromVP8(&e.current.Img))
}

func TestEncodeIntoStaticThresholdWritesCyclicRefreshSegmentation(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := segmentedQuantizationTestImage()
	packet := make([]byte, 16384)

	key, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled || !keyState.Segmentation.UpdateMap || !keyState.Segmentation.UpdateData {
		t.Fatalf("key segmentation = %+v, want map and data update", keyState.Segmentation)
	}
	if got := keyState.Segmentation.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != -10 {
		t.Fatalf("key static segment alt-q = %d, want -10", got)
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if d.modes[0].SegmentID != 0 || d.modes[1].SegmentID != 0 {
		t.Fatalf("key segment IDs = %d/%d, want all zero for cyclic refresh keyframe", d.modes[0].SegmentID, d.modes[1].SegmentID)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	assertImagesEqual(t, "static key current", keyFrame, publicImageFromVP8(&e.current.Img))

	second := segmentedQuantizationTestImage()
	for row := 0; row < second.Height; row++ {
		for col := 0; col < 16; col++ {
			second.Y[row*second.YStride+col] = 96
		}
	}
	inter, err := e.EncodeInto(packet, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	interState := packetState(t, inter.Data)
	if !interState.Segmentation.Enabled || !interState.Segmentation.UpdateMap || !interState.Segmentation.UpdateData {
		t.Fatalf("inter segmentation = %+v, want map and data update", interState.Segmentation)
	}
	if err := d.Decode(inter.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	if d.modes[0].SegmentID != 0 || d.modes[1].SegmentID != 0 {
		t.Fatalf("inter segment IDs = %d/%d, want no cyclic refresh blocks in tiny frame", d.modes[0].SegmentID, d.modes[1].SegmentID)
	}
	interFrame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "static inter current", interFrame, publicImageFromVP8(&e.current.Img))
}

func TestStaticSegmentationQuantizerDeltaUsesCyclicRefreshBoost(t *testing.T) {
	e := VP8Encoder{}
	e.opts.StaticThreshold = 1
	e.rc.currentQuantizer = 20

	cfg := e.staticSegmentationConfig()

	if !cfg.Enabled || !cfg.UpdateMap || !cfg.UpdateData {
		t.Fatalf("static segmentation = %+v, want enabled map/data update", cfg)
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != -10 {
		t.Fatalf("static segment alt-q = %d, want cyclic background delta -10", got)
	}

	e.rc.currentQuantizer = 1
	if cfg := e.staticSegmentationConfig(); cfg.Enabled {
		t.Fatalf("q=1 static segmentation = %+v, want disabled when cyclic delta is zero", cfg)
	}
}

func TestAssignInterFrameStaticSegmentsUsesCyclicRefreshCadence(t *testing.T) {
	modes := make([]vp8enc.InterFrameMacroblockMode, 40)

	assignInterFrameStaticSegments(4, 10, modes)

	if modes[0].SegmentID != staticSegmentID || modes[1].SegmentID != staticSegmentID {
		t.Fatalf("first cyclic segment IDs = %d/%d, want refreshed", modes[0].SegmentID, modes[1].SegmentID)
	}
	if modes[2].SegmentID != 0 || modes[len(modes)-1].SegmentID != 0 {
		t.Fatalf("later cyclic segment IDs = %d/%d, want zero", modes[2].SegmentID, modes[len(modes)-1].SegmentID)
	}
}

func TestEncodeIntoStaticThresholdWritesCyclicRefreshSegmentationForMatchingReference(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := segmentedQuantizationTestImage()
	keyPacket := make([]byte, 16384)
	key, err := e.EncodeInto(keyPacket, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := publicImageFromVP8(&e.lastRef.Img)
	interPacket := make([]byte, 16384)

	inter, err := e.EncodeInto(interPacket, reconstructed, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want matching-reference interframe")
	}
	state := packetState(t, inter.Data)
	if !state.Segmentation.Enabled || !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("inter segmentation = %+v, want map and data update for matching reference", state.Segmentation)
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	if err := d.Decode(inter.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	if d.modes[0].SegmentID != 0 || d.modes[1].SegmentID != 0 {
		t.Fatalf("inter segment IDs = %d/%d, want no cyclic refresh blocks in tiny frame", d.modes[0].SegmentID, d.modes[1].SegmentID)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "matching-reference segmented inter", frame, publicImageFromVP8(&e.current.Img))
}

func TestEncodeIntoDropsInterFrameWhenBufferEmptyAndAllowed(t *testing.T) {
	e := newLowBitrateDropTestEncoder(t, true)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !key.KeyFrame || key.Dropped {
		t.Fatalf("key result = key:%t dropped:%t, want encoded keyframe", key.KeyFrame, key.Dropped)
	}
	e.rc.bufferLevelBits = 0
	drainedBuffer := e.rc.bufferLevelBits

	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if !inter.Dropped || inter.KeyFrame || len(inter.Data) != 0 || inter.SizeBytes != 0 {
		t.Fatalf("inter result = key:%t dropped:%t size:%d data:%d, want dropped interframe", inter.KeyFrame, inter.Dropped, inter.SizeBytes, len(inter.Data))
	}
	if inter.BufferLevelBits <= drainedBuffer {
		t.Fatalf("buffer after drop = %d, want above drained buffer %d", inter.BufferLevelBits, drainedBuffer)
	}
}

func TestEncodeIntoDoesNotDropWhenFrameDroppingDisabled(t *testing.T) {
	e := newLowBitrateDropTestEncoder(t, false)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.Dropped || inter.KeyFrame || inter.SizeBytes == 0 || len(inter.Data) == 0 {
		t.Fatalf("inter result = key:%t dropped:%t size:%d data:%d, want encoded interframe", inter.KeyFrame, inter.Dropped, inter.SizeBytes, len(inter.Data))
	}
}

func TestEncodeIntoDoesNotDropInvisibleInterFrame(t *testing.T) {
	e := newLowBitrateDropTestEncoder(t, true)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	inter, err := e.EncodeInto(dst, src, 1, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible inter EncodeInto returned error: %v", err)
	}
	if inter.Dropped || inter.KeyFrame || inter.SizeBytes == 0 || len(inter.Data) == 0 {
		t.Fatalf("invisible inter result = key:%t dropped:%t size:%d data:%d, want encoded invisible interframe", inter.KeyFrame, inter.Dropped, inter.SizeBytes, len(inter.Data))
	}
}

func TestSetRateControlValidation(t *testing.T) {
	e := newTestEncoder(t)

	err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        56,
		MaxQuantizer:        4,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("error = %v, want ErrInvalidQuantizer", err)
	}
}

func TestSetBitrateKbpsHonorsConfiguredBounds(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1000,
		MinBitrateKbps:      500,
		MaxBitrateKbps:      1500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	if err := e.SetBitrateKbps(499); !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("below min error = %v, want ErrInvalidBitrate", err)
	}
	if e.rc.targetBitrateKbps != 1000 {
		t.Fatalf("target after below-min update = %d, want unchanged 1000", e.rc.targetBitrateKbps)
	}
	if err := e.SetBitrateKbps(1501); !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("above max error = %v, want ErrInvalidBitrate", err)
	}
	if e.rc.targetBitrateKbps != 1000 {
		t.Fatalf("target after above-max update = %d, want unchanged 1000", e.rc.targetBitrateKbps)
	}
	if err := e.SetBitrateKbps(1200); err != nil {
		t.Fatalf("in-range SetBitrateKbps returned error: %v", err)
	}
	if e.rc.targetBitrateKbps != 1200 {
		t.Fatalf("target after in-range update = %d, want 1200", e.rc.targetBitrateKbps)
	}
}

func TestSetBitrateKbpsAffectsNextEncodeResult(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if key.TargetBitrateKbps != 1200 || key.FrameTargetBits != 160000 {
		t.Fatalf("key target = kbps:%d bits:%d, want 1200/160000", key.TargetBitrateKbps, key.FrameTargetBits)
	}

	if err := e.SetBitrateKbps(600); err != nil {
		t.Fatalf("SetBitrateKbps returned error: %v", err)
	}
	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.TargetBitrateKbps != 600 || inter.FrameTargetBits != 20000 {
		t.Fatalf("inter target = kbps:%d bits:%d, want 600/20000", inter.TargetBitrateKbps, inter.FrameTargetBits)
	}
}

func TestEncodeIntoRateControlTracksReachableTargetsAcrossClip(t *testing.T) {
	low := encodeRateControlTestClip(t, 40)
	high := encodeRateControlTestClip(t, 80)

	if low.BitrateErrorPct < -35 || low.BitrateErrorPct > 35 {
		t.Fatalf("40kbps bitrate error = %.2f%%, want within +/-35%%", low.BitrateErrorPct)
	}
	if high.BitrateErrorPct < -35 || high.BitrateErrorPct > 35 {
		t.Fatalf("80kbps bitrate error = %.2f%%, want within +/-35%%", high.BitrateErrorPct)
	}
	if high.OutputBytes <= low.OutputBytes {
		t.Fatalf("output bytes = low:%d high:%d, want higher target to emit more bits", low.OutputBytes, high.OutputBytes)
	}
	if high.MeanQuantizer >= low.MeanQuantizer {
		t.Fatalf("mean quantizers = low:%.2f high:%.2f, want higher target to use lower quantizer", low.MeanQuantizer, high.MeanQuantizer)
	}
}

func TestSetRealtimeTargetRejectsResolutionChange(t *testing.T) {
	e := newTestEncoder(t)

	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 32, Height: 16}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("larger resolution error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 8, Height: 16}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("smaller resolution error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 16, Height: 16}); err != nil {
		t.Fatalf("same resolution returned error: %v", err)
	}
}

func TestEncoderRuntimeControlValidation(t *testing.T) {
	e := newTestEncoder(t)

	if err := e.SetDeadline(Deadline(-1)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("deadline error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetCPUUsed(17); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("cpu-used error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetKeyFrameInterval(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("keyframe interval error = %v, want ErrInvalidConfig", err)
	}
}

func TestForceKeyFrameIsConsumedByNextEncodeAttempt(t *testing.T) {
	e := newTestEncoder(t)
	e.frameCount = 7
	e.ForceKeyFrame()

	result, err := e.EncodeInto(make([]byte, 4096), testImage(16, 16), 1, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame {
		t.Fatalf("KeyFrame = false, want true")
	}
	if e.forceKeyFrame {
		t.Fatalf("forceKeyFrame = true, want false")
	}
}

func TestEncodeIntoBufferTooSmall(t *testing.T) {
	e := newTestEncoder(t)

	_, err := e.EncodeInto(nil, testImage(16, 16), 0, 1, 0)
	if !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("error = %v, want ErrBufferTooSmall", err)
	}
}

func TestEncodeIntoWritesDecodableKeyFrame(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 4096)

	result, err := e.EncodeInto(dst, testImage(16, 16), 22, 3, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if len(result.Data) == 0 || result.SizeBytes != len(result.Data) || !result.KeyFrame || result.PTS != 22 || result.Duration != 3 {
		t.Fatalf("EncodeResult = %+v, want populated keyframe result", result)
	}
	if e.frameCount != 1 {
		t.Fatalf("frameCount = %d, want 1", e.frameCount)
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(result.Data); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if frame.Width != 16 || frame.Height != 16 || frame.Y[0] >= 128 {
		t.Fatalf("decoded frame = %dx%d Y0=%d, want 16x16 dark source-directed frame", frame.Width, frame.Height, frame.Y[0])
	}
}

func TestEncodeIntoInvisibleFrameUpdatesReferenceWithoutOutput(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	invisiblePacket := make([]byte, 4096)

	invisible, err := e.EncodeInto(invisiblePacket, src, 0, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible EncodeInto returned error: %v", err)
	}
	info, err := PeekVP8StreamInfo(invisible.Data)
	if err != nil {
		t.Fatalf("PeekVP8StreamInfo returned error: %v", err)
	}
	if !invisible.KeyFrame || !info.KeyFrame || info.ShowFrame {
		t.Fatalf("invisible result/header = %+v/%+v, want invisible keyframe", invisible, info)
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(invisible.Data); err != nil {
		t.Fatalf("Decode invisible returned error: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("NextFrame returned invisible frame")
	}

	visiblePacket := make([]byte, 4096)
	visible, err := e.EncodeInto(visiblePacket, publicImageFromVP8(&e.lastRef.Img), 1, 1, 0)
	if err != nil {
		t.Fatalf("visible EncodeInto returned error: %v", err)
	}
	if visible.KeyFrame {
		t.Fatalf("visible KeyFrame = true, want interframe after invisible keyframe reference update")
	}
	if err := d.Decode(visible.Data); err != nil {
		t.Fatalf("Decode visible returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no visible frame")
	}
	assertImagesEqual(t, "visible after invisible", publicImageFromVP8(&e.current.Img), frame)
}

func TestEncodeIntoSharpnessAppliesLoopFilterToReferences(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		Sharpness:           3,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	first := testImage(32, 16)
	fillImage(first, 220, 90, 170)
	for row := 0; row < first.Height; row++ {
		for col := 16; col < first.Width; col++ {
			first.Y[row*first.YStride+col] = 40
		}
	}
	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := parseEncoderStateHeader(t, key.Data)
	if keyState.LoopFilter.Level != 5 || keyState.LoopFilter.SharpnessLevel != 3 {
		t.Fatalf("key loop filter = %+v, want level 5 sharpness 3", keyState.LoopFilter)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	assertImagesEqual(t, "filtered key current", keyFrame, publicImageFromVP8(&e.current.Img))

	second := testImage(32, 16)
	fillImage(second, 40, 90, 170)
	for row := 0; row < second.Height; row++ {
		for col := 16; col < second.Width; col++ {
			second.Y[row*second.YStride+col] = 220
		}
	}
	interPacket := make([]byte, 8192)
	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	interState := parseEncoderStateHeader(t, inter.Data)
	if interState.LoopFilter.Level != 5 || interState.LoopFilter.SharpnessLevel != 3 {
		t.Fatalf("inter loop filter = %+v, want level 5 sharpness 3", interState.LoopFilter)
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	assertImagesEqual(t, "filtered inter current", decoded[1], publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "filtered inter last", decoded[1], publicImageFromVP8(&e.lastRef.Img))
}

func TestEncodeIntoUsesSourcePixels(t *testing.T) {
	darkEncoder := newTestEncoder(t)
	brightEncoder := newTestEncoder(t)
	dark := testImage(16, 16)
	bright := testImage(16, 16)
	fillImage(bright, 220, 128, 128)
	dstDark := make([]byte, 4096)
	dstBright := make([]byte, 4096)

	darkResult, err := darkEncoder.EncodeInto(dstDark, dark, 0, 1, 0)
	if err != nil {
		t.Fatalf("dark EncodeInto returned error: %v", err)
	}
	brightResult, err := brightEncoder.EncodeInto(dstBright, bright, 0, 1, 0)
	if err != nil {
		t.Fatalf("bright EncodeInto returned error: %v", err)
	}

	darkFrame := decodeSingleFrame(t, darkResult.Data)
	brightFrame := decodeSingleFrame(t, brightResult.Data)
	if brightFrame.Y[0] <= darkFrame.Y[0] {
		t.Fatalf("decoded Y0 dark/bright = %d/%d, want bright greater", darkFrame.Y[0], brightFrame.Y[0])
	}
}

func TestEncodeIntoReconstructsReferencesLikeDecoder(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	src := testImage(32, 16)
	fillImage(src, 220, 90, 170)
	for row := 0; row < src.Height; row++ {
		for col := 16; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = 40
		}
	}
	dst := make([]byte, 8192)

	result, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	decoded := decodeSingleFrame(t, result.Data)

	assertImagesEqual(t, "current", decoded, publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "last", decoded, publicImageFromVP8(&e.lastRef.Img))
	assertImagesEqual(t, "golden", decoded, publicImageFromVP8(&e.goldenRef.Img))
	assertImagesEqual(t, "alt", decoded, publicImageFromVP8(&e.altRef.Img))
}

func TestEncodeIntoWritesInterFrameForMatchingReference(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	dstKey := make([]byte, 4096)
	key, err := e.EncodeInto(dstKey, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(t, key.Data)
	dstInter := make([]byte, 4096)

	inter, err := e.EncodeInto(dstInter, reconstructed, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("second frame KeyFrame = true, want interframe")
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	if err := d.Decode(inter.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "inter", reconstructed, frame)
	assertImagesEqual(t, "encoder current", frame, publicImageFromVP8(&e.current.Img))
}

func BenchmarkEncodeIntoMatchingReferenceInterFrame(b *testing.B) {
	e := newTestEncoder(b)
	if err := e.SetKeyFrameInterval(0); err != nil {
		b.Fatalf("SetKeyFrameInterval returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, src, 0, 1, 0)
	if err != nil {
		b.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(b, key.Data)
	interPacket := make([]byte, 4096)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.EncodeInto(interPacket, reconstructed, uint64(i+1), 1, 0); err != nil {
			b.Fatalf("inter EncodeInto returned error: %v", err)
		}
	}
}

func BenchmarkEncodeIntoGoldenReferenceInterFrame(b *testing.B) {
	e := newTestEncoder(b)
	if err := e.SetKeyFrameInterval(0); err != nil {
		b.Fatalf("SetKeyFrameInterval returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		b.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(b, key.Data)
	interPacket := make([]byte, 4096)
	if _, err := e.EncodeInto(interPacket, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef); err != nil {
		b.Fatalf("second EncodeInto returned error: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.EncodeInto(interPacket, keyFrame, uint64(i+2), 1, EncodeNoReferenceLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef); err != nil {
			b.Fatalf("golden EncodeInto returned error: %v", err)
		}
	}
}

func BenchmarkConvertMacroblockCoefficientsSparse(b *testing.B) {
	var src vp8enc.MacroblockCoefficients
	var dst vp8dec.MacroblockTokens
	src.QCoeff[0][0] = 3
	src.SetBlockEOB(0, 1)
	src.QCoeff[24][0] = 4
	src.SetBlockEOB(24, 1)
	src.QCoeff[16][0] = -2
	src.SetBlockEOB(16, 1)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		convertMacroblockCoefficients(&src, false, &dst)
	}
	if dst.EOB[0] != 1 || dst.QCoeff[0][0] != 3 || dst.EOB[24] != 1 || dst.QCoeff[24][0] != 4 || dst.EOB[16] != 1 || dst.QCoeff[16][0] != -2 {
		b.Fatalf("converted tokens = %+v", dst)
	}
}

func TestEncodeIntoWritesResidualInterFrameWhenSourceDiffersFromReference(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}
	dst := make([]byte, 4096)

	result, err := e.EncodeInto(dst, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("second frame KeyFrame = true, want residual interframe")
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	if err := d.Decode(result.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	if frame.Y[0] >= 220 {
		t.Fatalf("inter decoded Y0 = %d, want residual to move toward darker source", frame.Y[0])
	}
	assertImagesEqual(t, "encoder current", frame, publicImageFromVP8(&e.current.Img))
}

func TestEncodeIntoUsesNewMVForShiftedReference(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	first := testImage(32, 16)
	fillImage(first, 0, 90, 170)
	for row := 0; row < first.Height; row++ {
		for col := 0; col < first.Width; col++ {
			first.Y[row*first.YStride+col] = byte(32 + col*5)
		}
	}
	keyPacket := make([]byte, 8192)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(t, key.Data)
	shifted := shiftImageRightOne(reconstructed)
	interPacket := make([]byte, 8192)

	inter, err := e.EncodeInto(interPacket, shifted, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	if e.interFrameModes[0].Mode != vp8common.NewMV || e.interFrameModes[0].MV != (vp8enc.MotionVector{Col: -8}) {
		t.Fatalf("mode[0] = %+v, want NEWMV col -8", e.interFrameModes[0])
	}
	if e.interFrameModes[1].Mode != vp8common.NearestMV || e.interFrameModes[1].MV != (vp8enc.MotionVector{Col: -8}) {
		t.Fatalf("mode[1] = %+v, want NEARESTMV col -8", e.interFrameModes[1])
	}
}

func TestEncodeIntoKeyFrameSelectsBPredLumaAndVerticalChroma(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 32)
	src := rateControlTestFrame(16, 32, 0)

	if _, err := e.EncodeInto(make([]byte, 8192), src, 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	if e.keyFrameModes[1].YMode != vp8common.BPred {
		t.Fatalf("key mode[1] = %+v, want B_PRED luma for repeated rows", e.keyFrameModes[1])
	}
	if e.keyFrameModes[1].UVMode != vp8common.VPred {
		t.Fatalf("key UV mode[1] = %+v, want vertical prediction for repeated chroma rows", e.keyFrameModes[1])
	}
}

func TestEncodeIntoBPredKeyFrameUsesInterleavedReconstruction(t *testing.T) {
	opts := encoderValidationOptions(64, 128, 30, 700, nil)
	e, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := rateControlTestFrame(64, 128, 0)
	packet := make([]byte, 64*128*3)

	result, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	bpredCount := 0
	for _, mode := range e.keyFrameModes {
		if mode.YMode == vp8common.BPred {
			bpredCount++
		}
	}
	if bpredCount == 0 {
		t.Fatalf("B_PRED macroblocks = 0, want regression frame to exercise 4x4 intra reconstruction")
	}
	decoded := decodeSingleFrame(t, result.Data)
	assertImagesEqual(t, "B_PRED keyframe current", decoded, publicImageFromVP8(&e.current.Img))
	if psnr := encoderValidationImagePSNR(src, decoded); psnr < 45 {
		t.Fatalf("B_PRED keyframe PSNR = %.2f dB, want >= 45 dB", psnr)
	}
}

func TestEncodeIntoInterFramePrefersCodedInterResidualOverBPredIntra(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 32)
	first := testImage(16, 32)
	fillImage(first, 0, 90, 170)
	second := rateControlTestFrame(16, 32, 0)
	keyPacket := make([]byte, 8192)
	if _, err := e.EncodeInto(keyPacket, first, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 8192)

	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	if e.interFrameModes[1].RefFrame == vp8common.IntraFrame {
		t.Fatalf("inter mode[1] = %+v, want coded inter residual after RD scoring", e.interFrameModes[1])
	}
}

func TestEncodeIntoInterFrameCodesLargeUniformResidual(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 0, 90, 170)
	fillImage(second, 128, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 4096)

	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	if e.interFrameModes[0].RefFrame != vp8common.LastFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV {
		t.Fatalf("mode[0] = %+v, want LAST/ZEROMV residual macroblock", e.interFrameModes[0])
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertImagesEqual(t, "intra interframe current", decoded[1], publicImageFromVP8(&e.current.Img))
}

func TestEncodeIntoInterFrameCanSkipLastRefresh(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)

	inter, err := e.EncodeInto(interPacket, second, 1, 1, EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	assertImagesEqual(t, "last", keyFrame, publicImageFromVP8(&e.lastRef.Img))
	if publicImageFromVP8(&e.current.Img).Y[0] == keyFrame.Y[0] {
		t.Fatalf("current Y0 = last Y0 = %d, want current reconstructed without last refresh", keyFrame.Y[0])
	}
}

func TestEncodeIntoInterFramePreservesGoldenAndAltRefByDefault(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 91, 171)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)

	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertImagesEqual(t, "current", decoded[1], publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "last", decoded[1], publicImageFromVP8(&e.lastRef.Img))
	assertImagesEqual(t, "golden", keyFrame, publicImageFromVP8(&e.goldenRef.Img))
	assertImagesEqual(t, "alt", keyFrame, publicImageFromVP8(&e.altRef.Img))
}

func TestEncodeIntoInterFrameCanSkipGoldenAndAltRefRefresh(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 91, 171)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)

	inter, err := e.EncodeInto(interPacket, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertImagesEqual(t, "current", decoded[1], publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "last", decoded[1], publicImageFromVP8(&e.lastRef.Img))
	assertImagesEqual(t, "golden", keyFrame, publicImageFromVP8(&e.goldenRef.Img))
	assertImagesEqual(t, "alt", keyFrame, publicImageFromVP8(&e.altRef.Img))
}

func TestEncodeIntoNoReferenceLastCanUseGoldenReference(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)
	secondInter, err := e.EncodeInto(interPacket, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}

	thirdPacket := make([]byte, 4096)
	result, err := e.EncodeInto(thirdPacket, keyFrame, 2, 1, EncodeNoReferenceLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("third EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("KeyFrame = true, want interframe using golden when last reference is disallowed")
	}
	if e.interFrameModes[0].RefFrame != vp8common.GoldenFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped GOLDEN/ZEROMV", e.interFrameModes[0])
	}
	decoded := decodeFrameSequence(t, key.Data, secondInter.Data, result.Data)
	if len(decoded) != 3 {
		t.Fatalf("decoded frame count = %d, want 3", len(decoded))
	}
	assertImagesEqual(t, "golden interframe", keyFrame, decoded[2])
}

func TestEncodeIntoNoReferenceLastOrGoldenCanUseAltRef(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)

	result, err := e.EncodeInto(interPacket, keyFrame, 1, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("KeyFrame = true, want interframe using altref")
	}
	if e.interFrameModes[0].RefFrame != vp8common.AltRefFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped ALTREF/ZEROMV", e.interFrameModes[0])
	}
	decoded := decodeFrameSequence(t, key.Data, result.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertImagesEqual(t, "altref interframe", keyFrame, decoded[1])
}

func TestEncodeIntoNoReferencesForcesKeyFrame(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	dst := make([]byte, 4096)
	if _, err := e.EncodeInto(dst, first, 0, 1, 0); err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}

	result, err := e.EncodeInto(dst, second, 1, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame {
		t.Fatalf("KeyFrame = false, want keyframe when all references are disallowed")
	}
}

func TestConvertMacroblockCoefficientsOverwritesActiveSkippedDCBlock(t *testing.T) {
	var src vp8enc.MacroblockCoefficients
	var dst vp8dec.MacroblockTokens
	src.SetBlockEOB(0, 0)
	dst.QCoeff[0][0] = 99
	dst.QCoeff[0][1] = 77
	dst.EOB[0] = 2

	convertMacroblockCoefficients(&src, false, &dst)

	if got := dst.EOB[0]; got != 1 {
		t.Fatalf("EOB[0] = %d, want skipped-DC EOB 1", got)
	}
	if got := dst.QCoeff[0][0]; got != 0 {
		t.Fatalf("QCoeff[0][0] = %d, want active skipped DC overwritten", got)
	}
}

func TestEncoderHotPathAllocs(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 1)
	src := testImage(16, 16)
	cfg := RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		DropFrameAllowed:    true,
	}

	tests := []struct {
		name string
		fn   func()
	}{
		{name: "EncodeInto", fn: func() { _, _ = e.EncodeInto(dst, src, 0, 1, 0) }},
		{name: "SetBitrateKbps", fn: func() { _ = e.SetBitrateKbps(1200) }},
		{name: "SetRateControl", fn: func() { _ = e.SetRateControl(cfg) }},
		{name: "SetRealtimeTarget", fn: func() { _ = e.SetRealtimeTarget(RealtimeTarget{FPS: 30}) }},
		{name: "SetDeadline", fn: func() { _ = e.SetDeadline(DeadlineRealtime) }},
		{name: "SetCPUUsed", fn: func() { _ = e.SetCPUUsed(8) }},
		{name: "SetKeyFrameInterval", fn: func() { _ = e.SetKeyFrameInterval(120) }},
		{name: "ForceKeyFrame", fn: func() { e.ForceKeyFrame() }},
		{name: "Reset", fn: func() { e.Reset() }},
	}

	for _, tt := range tests {
		allocs := testing.AllocsPerRun(1000, tt.fn)
		if allocs != 0 {
			t.Fatalf("%s allocs = %v, want 0", tt.name, allocs)
		}
	}

	e.closed = false
	allocs := testing.AllocsPerRun(1000, func() {
		e.closed = false
		_ = e.Close()
	})
	if allocs != 0 {
		t.Fatalf("Close allocs = %v, want 0", allocs)
	}
}

func TestEncodeIntoSuccessAllocatesZero(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 4096)
	src := testImage(16, 16)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = e.EncodeInto(dst, src, 0, 1, 0)
	})
	if allocs != 0 {
		t.Fatalf("EncodeInto success allocs = %v, want 0", allocs)
	}
}

func newTestEncoder(tb testing.TB) *VP8Encoder {
	tb.Helper()
	return newSizedTestEncoder(tb, 16, 16)
}

func newSizedTestEncoder(tb testing.TB, width int, height int) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    true,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		ErrorResilient:      true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func newLowBitrateDropTestEncoder(t *testing.T, dropFrameAllowed bool) *VP8Encoder {
	t.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    dropFrameAllowed,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 0,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

type rateControlClipResult struct {
	OutputBytes     int
	BitrateErrorPct float64
	MeanQuantizer   float64
}

func encodeRateControlTestClip(t testing.TB, targetKbps int) rateControlClipResult {
	t.Helper()
	const (
		width  = 32
		height = 32
		fps    = 30
		frames = 20
	)
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	dst := make([]byte, 4096)
	outputBytes := 0
	quantSum := 0
	encodedFrames := 0
	for i := 0; i < frames; i++ {
		result, err := e.EncodeInto(dst, rateControlTestFrame(width, height, i), uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
		if result.Dropped {
			continue
		}
		outputBytes += result.SizeBytes
		quantSum += result.Quantizer
		encodedFrames++
	}
	if encodedFrames != frames {
		t.Fatalf("encoded frames = %d, want %d", encodedFrames, frames)
	}

	outputKbps := float64(outputBytes*8*fps) / float64(frames*1000)
	errorPct := (outputKbps - float64(targetKbps)) * 100 / float64(targetKbps)
	return rateControlClipResult{
		OutputBytes:     outputBytes,
		BitrateErrorPct: errorPct,
		MeanQuantizer:   float64(quantSum) / float64(encodedFrames),
	}
}

func rateControlTestFrame(width int, height int, index int) Image {
	img := testImage(width, height)
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for row := 0; row < uvHeight; row++ {
		for col := 0; col < uvWidth; col++ {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

func testImage(width int, height int) Image {
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

func fillImage(img Image, y byte, u byte, v byte) {
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.U {
		img.U[i] = u
	}
	for i := range img.V {
		img.V[i] = v
	}
}

func packetTokenPartition(t *testing.T, packet []byte) vp8common.TokenPartition {
	t.Helper()
	return packetState(t, packet).TokenPartition
}

func packetBaseQIndex(t *testing.T, packet []byte) int {
	t.Helper()
	return int(packetState(t, packet).Quant.BaseQIndex)
}

func packetState(t *testing.T, packet []byte) vp8dec.StateHeader {
	t.Helper()
	var coefProbs = vp8tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet, vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	return state
}

func shiftImageRightOne(src Image) Image {
	dst := testImage(src.Width, src.Height)
	for row := 0; row < src.Height; row++ {
		dst.Y[row*dst.YStride] = src.Y[row*src.YStride]
		for col := 1; col < src.Width; col++ {
			dst.Y[row*dst.YStride+col] = src.Y[row*src.YStride+col-1]
		}
	}
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	copyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	copyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
	return dst
}

func decodeSingleFrame(tb testing.TB, packet []byte) Image {
	tb.Helper()
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		tb.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		tb.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		tb.Fatalf("NextFrame returned no frame")
	}
	return frame
}

func parseEncoderStateHeader(t *testing.T, packet []byte) vp8dec.StateHeader {
	t.Helper()
	var coefProbs = vp8tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet, vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	return state
}

func assertImagesEqual(t *testing.T, name string, want Image, got Image) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height {
		t.Fatalf("%s dimensions = %dx%d, want %dx%d", name, got.Width, got.Height, want.Width, want.Height)
	}
	assertPlaneEqual(t, name+" Y", want.Y, want.YStride, got.Y, got.YStride, want.Width, want.Height)
	uvWidth := (want.Width + 1) >> 1
	uvHeight := (want.Height + 1) >> 1
	assertPlaneEqual(t, name+" U", want.U, want.UStride, got.U, got.UStride, uvWidth, uvHeight)
	assertPlaneEqual(t, name+" V", want.V, want.VStride, got.V, got.VStride, uvWidth, uvHeight)
}

func assertPlaneEqual(t *testing.T, name string, want []byte, wantStride int, got []byte, gotStride int, width int, height int) {
	t.Helper()
	for row := 0; row < height; row++ {
		wantRow := want[row*wantStride : row*wantStride+width]
		gotRow := got[row*gotStride : row*gotStride+width]
		for col := 0; col < width; col++ {
			if gotRow[col] != wantRow[col] {
				t.Fatalf("%s[%d,%d] = %d, want %d", name, row, col, gotRow[col], wantRow[col])
			}
		}
	}
}
