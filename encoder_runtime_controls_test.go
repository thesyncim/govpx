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
	result, err := e.EncodeInto(dst, testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if result.Quantizer != 28 || packetBaseQIndex(t, result.Data) != libvpxPublicQuantizerToQIndex(28) {
		t.Fatalf("quantizer = result:%d packet:%d, want public CQ level 28 / qindex %d", result.Quantizer, packetBaseQIndex(t, result.Data), libvpxPublicQuantizerToQIndex(28))
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
	result, err := e.EncodeInto(dst, testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if result.Quantizer != 40 || packetBaseQIndex(t, result.Data) != libvpxPublicQuantizerToQIndex(40) {
		t.Fatalf("quantizer = result:%d packet:%d, want public CQ level 40 / qindex %d", result.Quantizer, packetBaseQIndex(t, result.Data), libvpxPublicQuantizerToQIndex(40))
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
	for frame := 1; frame <= 11; frame++ {
		wantRC := e.rc
		wantRC.beginFrame(false)
		wantTarget := wantRC.frameTargetBits
		if frame == 11 {
			wantTarget = boostedFrameTargetBits(wantTarget, e.rc.gfCBRBoostPct)
		}
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		if frame == 11 && inter.FrameTargetBits != wantTarget {
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
	if err := e.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true) returned error: %v", err)
	}
	if !e.opts.RTCExternalRateControl {
		t.Fatalf("RTCExternalRateControl = false, want true")
	}
	if err := e.SetRTCExternalRateControl(false); err != nil {
		t.Fatalf("SetRTCExternalRateControl(false) returned error: %v", err)
	}
	if e.opts.RTCExternalRateControl {
		t.Fatalf("RTCExternalRateControl = true, want false")
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

func TestSetBitrateKbpsAffectsNextEncodeResult(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if key.TargetBitrateKbps != 1200 || key.FrameTargetBits != 240000 {
		t.Fatalf("key target = kbps:%d bits:%d, want 1200/240000", key.TargetBitrateKbps, key.FrameTargetBits)
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
