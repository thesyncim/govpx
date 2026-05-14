package govpx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestEncodeIntoDropsInterFrameWhenBufferUnderrunAndAllowed(t *testing.T) {
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
	e.rc.bufferLevelBits = -1
	drainedBuffer := e.rc.bufferLevelBits

	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if !inter.Dropped || inter.KeyFrame || len(inter.Data) != 0 || inter.SizeBytes != 0 {
		t.Fatalf("inter result = key:%t dropped:%t size:%d data:%d, want dropped interframe", inter.KeyFrame, inter.Dropped, inter.SizeBytes, len(inter.Data))
	}
	if inter.BufferLevelBits != drainedBuffer+e.rc.bitsPerFrame {
		t.Fatalf("buffer after drop = %d, want libvpx underrun recovery %d", inter.BufferLevelBits, drainedBuffer+e.rc.bitsPerFrame)
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

func TestEncodeIntoInvisibleFrameUsesLibvpxBufferOverheadAccounting(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible key EncodeInto returned error: %v", err)
	}
	wantKeyBuffer := max(e.rc.bufferInitialBits-encodedSizeBits(key.SizeBytes), 0)
	if key.BufferLevelBits != wantKeyBuffer || e.rc.bufferLevelBits != wantKeyBuffer {
		t.Fatalf("invisible key buffer = result:%d rc:%d, want %d", key.BufferLevelBits, e.rc.bufferLevelBits, wantKeyBuffer)
	}

	beforeInterBuffer := e.rc.bufferLevelBits
	inter, err := e.EncodeInto(dst, src, 1, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible inter EncodeInto returned error: %v", err)
	}
	wantInterBuffer := max(beforeInterBuffer-encodedSizeBits(inter.SizeBytes), 0)
	if inter.BufferLevelBits != wantInterBuffer || e.rc.bufferLevelBits != wantInterBuffer {
		t.Fatalf("invisible inter buffer = result:%d rc:%d, want %d", inter.BufferLevelBits, e.rc.bufferLevelBits, wantInterBuffer)
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

func TestSetRateControlCQLevelAffectsNextEncode(t *testing.T) {
	e := newTestEncoder(t)
	err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             28,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("SetRateControl returned error: %v", err)
	}
	dst := make([]byte, 4096)
	// First frame is a keyframe; libvpx CQ mode does not floor KF Q to
	// cq_target_quality (vp8/encoder/onyx_if.c lines 3727-3739). Encode
	// a second frame as inter and assert the floor there.
	if _, err := e.EncodeInto(dst, testImage(16, 16), 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	result, err := e.EncodeInto(dst, testImage(16, 16), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if result.Quantizer != 28 || packetBaseQIndex(t, result.Data) != libvpxPublicQuantizerToQIndex(28) {
		t.Fatalf("inter quantizer = result:%d packet:%d, want public CQ level 28 / qindex %d", result.Quantizer, packetBaseQIndex(t, result.Data), libvpxPublicQuantizerToQIndex(28))
	}
}

func TestSetRateControlQAcceptsCQLevelWithoutCQFloor(t *testing.T) {
	e := newTestEncoder(t)
	err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             28,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("SetRateControl returned error: %v", err)
	}
	if e.rc.mode != RateControlQ || e.rc.cqLevel != libvpxPublicQuantizerToQIndex(28) {
		t.Fatalf("Q mode state = mode:%d cq:%d, want RateControlQ / qindex %d", e.rc.mode, e.rc.cqLevel, libvpxPublicQuantizerToQIndex(28))
	}
	if e.rc.currentQuantizer >= e.rc.cqLevel {
		t.Fatalf("Q current quantizer = %d, want below CQ qindex %d to prove no CQ floor", e.rc.currentQuantizer, e.rc.cqLevel)
	}
}

func TestSetCQLevelValidationAndNextEncode(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             24,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetCQLevel(64); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("out-of-range SetCQLevel error = %v, want ErrInvalidQuantizer", err)
	}
	if err := e.SetCQLevel(3); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("below-min SetCQLevel error = %v, want ErrInvalidQuantizer", err)
	}
	if e.rc.cqLevel != libvpxPublicQuantizerToQIndex(24) {
		t.Fatalf("CQ level after rejected updates = %d, want qindex %d", e.rc.cqLevel, libvpxPublicQuantizerToQIndex(24))
	}
	if err := e.SetCQLevel(40); err != nil {
		t.Fatalf("SetCQLevel returned error: %v", err)
	}
	dst := make([]byte, 4096)
	// First frame is a keyframe; libvpx CQ mode does not floor KF Q to
	// cq_target_quality (vp8/encoder/onyx_if.c lines 3727-3739). Encode
	// a second frame as inter to observe the floor.
	if _, err := e.EncodeInto(dst, testImage(16, 16), 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	result, err := e.EncodeInto(dst, testImage(16, 16), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if result.Quantizer != 40 || packetBaseQIndex(t, result.Data) != libvpxPublicQuantizerToQIndex(40) {
		t.Fatalf("inter quantizer = result:%d packet:%d, want public CQ level 40 / qindex %d", result.Quantizer, packetBaseQIndex(t, result.Data), libvpxPublicQuantizerToQIndex(40))
	}
}

func TestSetCQLevelValidationAppliesToRateControlQ(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             24,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetCQLevel(3); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("below-min Q SetCQLevel error = %v, want ErrInvalidQuantizer", err)
	}
	if err := e.SetCQLevel(40); err != nil {
		t.Fatalf("Q SetCQLevel returned error: %v", err)
	}
	if e.rc.cqLevel != libvpxPublicQuantizerToQIndex(40) {
		t.Fatalf("Q cqLevel = %d, want qindex %d", e.rc.cqLevel, libvpxPublicQuantizerToQIndex(40))
	}
	if e.rc.currentQuantizer >= e.rc.cqLevel {
		t.Fatalf("Q current quantizer = %d, want no reset to CQ qindex %d", e.rc.currentQuantizer, e.rc.cqLevel)
	}
}

func TestSetMaxIntraBitratePctAffectsNextKeyFrame(t *testing.T) {
	e := newTestEncoder(t)
	if err := e.SetMaxIntraBitratePct(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetMaxIntraBitratePct error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetMaxIntraBitratePct(150); err != nil {
		t.Fatalf("SetMaxIntraBitratePct returned error: %v", err)
	}
	result, err := e.EncodeInto(make([]byte, 4096), testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	want := (e.rc.bitsPerFrame * 150) / 100
	if result.FrameTargetBits != want {
		t.Fatalf("key target bits = %d, want %d", result.FrameTargetBits, want)
	}
}

func TestSetGFCBRBoostPctValidationAndNextEncode(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetGFCBRBoostPct(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetGFCBRBoostPct error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetGFCBRBoostPct(50); err != nil {
		t.Fatalf("SetGFCBRBoostPct returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	refreshFrame := e.rc.framesTillGFUpdateDue + 1
	cbrInterval := e.goldenFrameCBRInterval(rows, cols)
	for frame := 1; frame <= refreshFrame; frame++ {
		wantRC := e.rc
		if frame == refreshFrame {
			wantRC.framesTillGFUpdateDue = cbrInterval
			wantRC.currentGFInterval = cbrInterval
		}
		wantRC.beginFrame(false)
		wantTarget := wantRC.frameTargetBits
		if frame == refreshFrame {
			wantTarget = boostedFrameTargetBits(wantTarget, e.rc.gfCBRBoostPct)
		}
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		if frame == refreshFrame && inter.FrameTargetBits != wantTarget {
			t.Fatalf("boosted target = %d, want libvpx CBR target %d", inter.FrameTargetBits, wantTarget)
		}
	}
}

func TestSetVP8RuntimeControlsValidationAndNextEncode(t *testing.T) {
	e := newTestEncoder(t)
	if err := e.SetTokenPartitions(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTokenPartitions negative error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetTokenPartitions(4); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTokenPartitions out-of-range error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetSharpness(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetSharpness negative error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetSharpness(8); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetSharpness out-of-range error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetStaticThreshold(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetStaticThreshold negative error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetScreenContentMode(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetScreenContentMode negative error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetScreenContentMode(3); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetScreenContentMode out-of-range error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetNoiseSensitivity(7); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetNoiseSensitivity out-of-range error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetARNR(16, 3, 3); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetARNR max-frames error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetARNR(3, 3, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetARNR type-zero error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetTokenPartitions(int(vp8common.EightPartition)); err != nil {
		t.Fatalf("SetTokenPartitions returned error: %v", err)
	}
	if err := e.SetSharpness(3); err != nil {
		t.Fatalf("SetSharpness returned error: %v", err)
	}
	if err := e.SetStaticThreshold(1); err != nil {
		t.Fatalf("SetStaticThreshold returned error: %v", err)
	}
	if err := e.SetScreenContentMode(1); err != nil {
		t.Fatalf("SetScreenContentMode returned error: %v", err)
	}
	rtc := newTestEncoder(t)
	if err := rtc.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true) returned error: %v", err)
	}
	if !rtc.opts.RTCExternalRateControl {
		t.Fatalf("RTCExternalRateControl = false, want true")
	}
	if err := rtc.SetRTCExternalRateControl(false); err != nil {
		t.Fatalf("SetRTCExternalRateControl(false) returned error: %v", err)
	}
	if !rtc.opts.RTCExternalRateControl {
		t.Fatalf("RTCExternalRateControl = false after disable request, want sticky true")
	}
	if err := e.SetAdaptiveKeyFrames(true); err != nil {
		t.Fatalf("SetAdaptiveKeyFrames returned error: %v", err)
	}
	if err := e.SetNoiseSensitivity(6); err != nil {
		t.Fatalf("SetNoiseSensitivity returned error: %v", err)
	}
	if err := e.SetARNR(15, 6, 3); err != nil {
		t.Fatalf("SetARNR returned error: %v", err)
	}

	result, err := e.EncodeInto(make([]byte, 8192), testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	state := packetState(t, result.Data)
	if state.TokenPartition != vp8common.EightPartition {
		t.Fatalf("token partition = %d, want eight", state.TokenPartition)
	}
	if state.LoopFilter.SharpnessLevel != 0 {
		t.Fatalf("key sharpness = %d, want libvpx keyframe sharpness 0", state.LoopFilter.SharpnessLevel)
	}
	if !state.Segmentation.Enabled || !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("segmentation = %+v, want static-threshold map/data update", state.Segmentation)
	}
	inter, err := e.EncodeInto(make([]byte, 8192), publicImageFromVP8(&e.lastRef.Img), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	interState := packetState(t, inter.Data)
	if interState.LoopFilter.SharpnessLevel != 3 {
		t.Fatalf("inter sharpness = %d, want runtime sharpness 3", interState.LoopFilter.SharpnessLevel)
	}
}

func TestSetRealtimeTargetRejectsCQBoundsWithoutMutation(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             24,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 30}); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetRealtimeTarget error = %v, want ErrInvalidQuantizer", err)
	}
	if e.opts.MinQuantizer != 4 || e.opts.MaxQuantizer != 56 || e.opts.CQLevel != 24 ||
		e.rc.minQuantizer != libvpxPublicQuantizerToQIndex(4) ||
		e.rc.maxQuantizer != libvpxPublicQuantizerToQIndex(56) ||
		e.rc.cqLevel != libvpxPublicQuantizerToQIndex(24) {
		t.Fatalf("rate control after rejected target = opts:%d/%d/%d rc:%d/%d/%d, want public 4/56/24 mapped to qindex",
			e.opts.MinQuantizer, e.opts.MaxQuantizer, e.opts.CQLevel, e.rc.minQuantizer, e.rc.maxQuantizer, e.rc.cqLevel)
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

func TestSetBitrateKbpsPreservesLibvpxZeroBufferLevel(t *testing.T) {
	e := newTestEncoder(t)
	e.rc.bufferLevelBits = 0

	if err := e.SetBitrateKbps(600); err != nil {
		t.Fatalf("SetBitrateKbps returned error: %v", err)
	}

	if e.rc.bufferLevelBits != 0 {
		t.Fatalf("buffer after bitrate change = %d, want libvpx preserved zero", e.rc.bufferLevelBits)
	}
}

func TestSetRateControlPreservesLibvpxZeroBufferLevel(t *testing.T) {
	e := newTestEncoder(t)
	e.rc.bufferLevelBits = 0

	err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   900,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("SetRateControl returned error: %v", err)
	}

	if e.rc.bufferLevelBits != 0 {
		t.Fatalf("buffer after rate-control change = %d, want libvpx preserved zero", e.rc.bufferLevelBits)
	}
}

func TestSetRateControlPreservesLibvpxAdaptiveState(t *testing.T) {
	e := newTestEncoder(t)
	e.rc.decimationFactor = 2
	e.rc.decimationCount = 1
	e.rc.frameTargetBits = 12345
	e.rc.avgFrameQuantizer = 43
	e.rc.normalInterQuantizerTotal = 129
	e.rc.normalInterFrames = 3
	e.rc.normalInterAvgQuantizer = 43
	e.rc.rateCorrectionFactor = 1.75
	e.rc.keyFrameCorrectionFactor = 2.25
	e.rc.goldenCorrectionFactor = 1.5
	e.rc.totalActualBits = 123456
	e.rc.rollingActualBits = 2100
	e.rc.rollingTargetBits = 2200
	e.rc.longRollingActualBits = 2300
	e.rc.longRollingTargetBits = 2400

	err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   900,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  60,
	})
	if err != nil {
		t.Fatalf("SetRateControl returned error: %v", err)
	}

	if e.rc.decimationFactor != 2 || e.rc.decimationCount != 1 {
		t.Fatalf("decimation state = factor:%d count:%d, want 2/1", e.rc.decimationFactor, e.rc.decimationCount)
	}
	if e.rc.frameTargetBits != 12345 {
		t.Fatalf("frameTargetBits = %d, want libvpx preserved stale target 12345", e.rc.frameTargetBits)
	}
	if e.rc.avgFrameQuantizer != 43 || e.rc.normalInterQuantizerTotal != 129 || e.rc.normalInterFrames != 3 || e.rc.normalInterAvgQuantizer != 43 {
		t.Fatalf("quantizer history = avg:%d total:%d frames:%d normal:%d, want 43/129/3/43",
			e.rc.avgFrameQuantizer, e.rc.normalInterQuantizerTotal, e.rc.normalInterFrames, e.rc.normalInterAvgQuantizer)
	}
	if e.rc.rateCorrectionFactor != 1.75 || e.rc.keyFrameCorrectionFactor != 2.25 || e.rc.goldenCorrectionFactor != 1.5 {
		t.Fatalf("correction factors = %g/%g/%g, want 1.75/2.25/1.5",
			e.rc.rateCorrectionFactor, e.rc.keyFrameCorrectionFactor, e.rc.goldenCorrectionFactor)
	}
	if e.rc.totalActualBits != 123456 {
		t.Fatalf("totalActualBits = %d, want 123456", e.rc.totalActualBits)
	}
	if e.rc.rollingActualBits != 2100 || e.rc.rollingTargetBits != 2200 || e.rc.longRollingActualBits != 2300 || e.rc.longRollingTargetBits != 2400 {
		t.Fatalf("rolling bits = short:%d/%d long:%d/%d, want 2100/2200 and 2300/2400",
			e.rc.rollingActualBits, e.rc.rollingTargetBits, e.rc.longRollingActualBits, e.rc.longRollingTargetBits)
	}
}

func TestSetRateControlPreservesLibvpxCyclicRefreshMode(t *testing.T) {
	cbr := newTestEncoder(t)
	if !cbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("CBR-born encoder cyclic refresh disabled, want enabled")
	}
	if err := cbr.SetRateControl(RateControlConfig{
		Mode:                RateControlVBR,
		TargetBitrateKbps:   cbr.rc.targetBitrateKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("CBR-born SetRateControl(VBR): %v", err)
	}
	if !cbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("CBR-born runtime VBR cyclic refresh disabled, want libvpx sticky enablement")
	}

	vbr, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder(VBR): %v", err)
	}
	defer vbr.Close()
	if vbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("VBR-born encoder cyclic refresh enabled, want disabled")
	}
	if err := vbr.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("VBR-born SetRateControl(CBR): %v", err)
	}
	if vbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("VBR-born runtime CBR cyclic refresh enabled, want libvpx sticky disablement")
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
	// User-facing kbps stays at 1200 (the requested value); the
	// internal effective rate is clipped to libvpx's raw-target-rate
	// envelope (16*16*8*3*30/1000 = 184 kbps), so the first-frame KF
	// target is starting_buffer_level/2 = 184_000bps * 400ms / 2 =
	// 36_800 bits (was 240_000 before the raw-rate cap landed).
	if key.TargetBitrateKbps != 1200 || key.FrameTargetBits != 36800 {
		t.Fatalf("key target = kbps:%d bits:%d, want 1200/36800", key.TargetBitrateKbps, key.FrameTargetBits)
	}

	if err := e.SetBitrateKbps(600); err != nil {
		t.Fatalf("SetBitrateKbps returned error: %v", err)
	}
	wantRC := e.rc
	wantRC.beginFrame(false)
	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.TargetBitrateKbps != 600 || inter.FrameTargetBits != wantRC.frameTargetBits {
		t.Fatalf("inter target = kbps:%d bits:%d, want 600/%d", inter.TargetBitrateKbps, inter.FrameTargetBits, wantRC.frameTargetBits)
	}
}

func TestEncodeIntoRateControlTracksReachableTargetsAcrossClip(t *testing.T) {
	low := encodeRateControlTestClip(t, 25)
	high := encodeRateControlTestClip(t, 35)

	if low.BitrateErrorPct < -35 || low.BitrateErrorPct > 35 {
		t.Fatalf("25kbps bitrate error = %.2f%%, want within +/-35%%", low.BitrateErrorPct)
	}
	if high.BitrateErrorPct < -35 || high.BitrateErrorPct > 35 {
		t.Fatalf("35kbps bitrate error = %.2f%%, want within +/-35%%", high.BitrateErrorPct)
	}
	if high.OutputBytes <= low.OutputBytes {
		t.Fatalf("output bytes = low:%d high:%d, want higher target to emit more bits", low.OutputBytes, high.OutputBytes)
	}
	if high.MeanQuantizer >= low.MeanQuantizer {
		t.Fatalf("mean quantizers = low:%.2f high:%.2f, want higher target to use lower quantizer", low.MeanQuantizer, high.MeanQuantizer)
	}
}

func TestSetRealtimeTargetValidatesResolutionChange(t *testing.T) {
	e := newTestEncoder(t)

	if err := e.SetRealtimeTarget(RealtimeTarget{Width: -1, Height: 16}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("negative width error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 16, Height: -1}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("negative height error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: maxVP8Dimension + 1, Height: 16}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("overflowing width error = %v, want ErrInvalidConfig", err)
	}
	// Same-size echo must still be accepted so bitrate-only BWE updates that
	// happen to carry the current dimensions validate cleanly.
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 16, Height: 16}); err != nil {
		t.Fatalf("same resolution returned error: %v", err)
	}
	if e.opts.Width != 16 || e.opts.Height != 16 {
		t.Fatalf("dims after no-op = %dx%d, want 16x16", e.opts.Width, e.opts.Height)
	}
}

func TestSetRealtimeTargetResizesDrainedLookaheadBuffers(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		LookaheadFrames:   4,
		AutoAltRef:        true,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	defer e.Close()

	buf := make([]byte, 96*96*6+4096)
	for i := range 8 {
		if _, err := e.EncodeInto(buf, rateControlTestFrame(64, 64, i), uint64(i), 1, 0); err != nil && !errors.Is(err, ErrFrameNotReady) {
			t.Fatalf("pre-resize EncodeInto %d: %v", i, err)
		}
	}
	for {
		_, err := e.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("pre-resize FlushInto: %v", err)
		}
	}
	if e.lookaheadCount != 0 {
		t.Fatalf("lookaheadCount before resize = %d, want drained", e.lookaheadCount)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 96, Height: 96}); err != nil {
		t.Fatalf("SetRealtimeTarget resize returned error: %v", err)
	}
	for i := range e.lookahead {
		if got := e.lookahead[i].frame.Img.Width; got != 96 {
			t.Fatalf("lookahead[%d] width = %d, want 96", i, got)
		}
		if got := e.lookahead[i].frame.Img.Height; got != 96 {
			t.Fatalf("lookahead[%d] height = %d, want 96", i, got)
		}
	}
	if e.autoAltRefStashFrame.Img.YStride != 0 {
		if e.autoAltRefStashFrame.Img.Width != 96 || e.autoAltRefStashFrame.Img.Height != 96 {
			t.Fatalf("auto-alt-ref stash dims = %dx%d, want 96x96", e.autoAltRefStashFrame.Img.Width, e.autoAltRefStashFrame.Img.Height)
		}
	}
	for i := range 8 {
		if _, err := e.EncodeInto(buf, rateControlTestFrame(96, 96, i+8), uint64(i+8), 1, 0); err != nil && !errors.Is(err, ErrFrameNotReady) {
			t.Fatalf("post-resize EncodeInto %d: %v", i, err)
		}
	}
	for {
		_, err := e.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("post-resize FlushInto: %v", err)
		}
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

func TestAdaptiveKeyFrameCadenceUsesInitialFrequency(t *testing.T) {
	tests := []struct {
		name string
		e    VP8Encoder
		want bool
	}{
		{
			name: "adaptive initial frequency due",
			e: VP8Encoder{
				opts:              EncoderOptions{KeyFrameInterval: 4, AdaptiveKeyFrames: true},
				keyFrameFrequency: 4,
				frameCount:        4,
				rc:                rateControlState{framesSinceKeyframe: 3},
			},
			want: true,
		},
		{
			name: "adaptive ignores runtime interval shrink",
			e: VP8Encoder{
				opts:              EncoderOptions{KeyFrameInterval: 4, AdaptiveKeyFrames: true},
				keyFrameFrequency: 999,
				frameCount:        8,
				rc:                rateControlState{framesSinceKeyframe: 7},
			},
			want: false,
		},
		{
			name: "fixed interval still uses live interval",
			e: VP8Encoder{
				opts: EncoderOptions{KeyFrameInterval: 4},
				rc:   rateControlState{framesSinceKeyframe: 7},
			},
			want: true,
		},
		{
			name: "fixed interval shrink past age is due",
			e: VP8Encoder{
				opts: EncoderOptions{KeyFrameInterval: 4},
				rc:   rateControlState{framesSinceKeyframe: 5},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.e.shouldEncodeKeyFrame(0); got != tc.want {
				t.Fatalf("shouldEncodeKeyFrame = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestSetTwoPassStatsMidstreamTransitions(t *testing.T) {
	opts := EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  60,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           0,
	}
	sources := make([]Image, 4)
	for i := range sources {
		sources[i] = rateControlTestFrame(opts.Width, opts.Height, i)
	}
	stats := collectRuntimeControlFirstPassStats(t, opts, sources)

	e, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<16)
	onePass, err := e.EncodeInto(dst, sources[0], 0, 1, 0)
	if err != nil {
		t.Fatalf("one-pass EncodeInto returned error: %v", err)
	}
	if onePass.TwoPassFrameTargetBits != 0 || onePass.FirstPassStats != (FirstPassFrameStats{}) {
		t.Fatalf("one-pass two-pass fields = target:%d stats:%+v, want zero", onePass.TwoPassFrameTargetBits, onePass.FirstPassStats)
	}

	if err := e.SetTwoPassStats(stats); err != nil {
		t.Fatalf("SetTwoPassStats(enable) returned error: %v", err)
	}
	twoPass, err := e.EncodeInto(dst, sources[1], 1, 1, 0)
	if err != nil {
		t.Fatalf("two-pass EncodeInto returned error: %v", err)
	}
	if twoPass.TwoPassFrameTargetBits == 0 {
		t.Fatalf("TwoPassFrameTargetBits = 0, want enabled two-pass target")
	}
	if twoPass.FirstPassStats != stats[1] {
		t.Fatalf("FirstPassStats = %+v, want stats[1] %+v", twoPass.FirstPassStats, stats[1])
	}

	if err := e.SetTwoPassStats(nil); err != nil {
		t.Fatalf("SetTwoPassStats(disable) returned error: %v", err)
	}
	disabled, err := e.EncodeInto(dst, sources[2], 2, 1, 0)
	if err != nil {
		t.Fatalf("disabled EncodeInto returned error: %v", err)
	}
	if disabled.TwoPassFrameTargetBits != 0 || disabled.FirstPassStats != (FirstPassFrameStats{}) {
		t.Fatalf("disabled two-pass fields = target:%d stats:%+v, want zero", disabled.TwoPassFrameTargetBits, disabled.FirstPassStats)
	}
}

func collectRuntimeControlFirstPassStats(t *testing.T, opts EncoderOptions, sources []Image) []FirstPassFrameStats {
	t.Helper()
	firstPass, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("first-pass NewVP8Encoder returned error: %v", err)
	}
	defer firstPass.Close()
	stats := make([]FirstPassFrameStats, len(sources))
	for i, src := range sources {
		stats[i], err = firstPass.CollectFirstPassStats(src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats[%d] returned error: %v", i, err)
		}
	}
	return FinalizeFirstPassStats(stats)
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

func TestForceKeyFrameWithLookaheadAttachesToNextInput(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		LookaheadFrames:   2,
		AdaptiveKeyFrames: false,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 4096)
	src := testImage(16, 16)

	if _, err := e.EncodeInto(dst, src, 0, 1, 0); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("first EncodeInto error = %v, want ErrFrameNotReady", err)
	}

	e.ForceKeyFrame()
	result, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame {
		t.Fatalf("first emitted packet KeyFrame = false, want bootstrap key frame")
	}
	if e.forceKeyFrame {
		t.Fatalf("forceKeyFrame = true after accepting forced input, want false")
	}

	result, err = e.EncodeInto(dst, src, 2, 1, 0)
	if err != nil {
		t.Fatalf("third EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame {
		t.Fatalf("forced lookahead input KeyFrame = false, want true")
	}

	result, err = e.EncodeInto(dst, src, 3, 1, 0)
	if err != nil {
		t.Fatalf("fourth EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("following lookahead input KeyFrame = true, want false")
	}
}
