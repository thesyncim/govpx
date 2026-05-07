package govpx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
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

	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, RateControlMode: RateControlCQ, TargetBitrateKbps: 1200, MinQuantizer: 4, MaxQuantizer: 56, CQLevel: 64})
	if !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("CQ level error = %v, want ErrInvalidQuantizer", err)
	}

	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, MaxIntraBitratePct: -1})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("max intra bitrate error = %v, want ErrInvalidConfig", err)
	}

	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, GFCBRBoostPct: -1})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("GF CBR boost error = %v, want ErrInvalidConfig", err)
	}

	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, ScreenContentMode: 3})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("screen content mode error = %v, want ErrInvalidConfig", err)
	}

	e, err := NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, NoiseSensitivity: 6, ARNRMaxFrames: 15})
	if err != nil {
		t.Fatalf("libvpx high denoise/ARNR bounds returned error: %v", err)
	}
	if e.opts.ARNRType != 3 {
		t.Fatalf("default ARNR type = %d, want libvpx centered type 3", e.opts.ARNRType)
	}

	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, NoiseSensitivity: 7})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("noise sensitivity error = %v, want ErrInvalidConfig", err)
	}
	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, ARNRMaxFrames: 16})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("ARNR max frames error = %v, want ErrInvalidConfig", err)
	}
	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, ARNRType: 4})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("ARNR type error = %v, want ErrInvalidConfig", err)
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

func TestEncodeIntoUsesLibvpxInitialKeyFrameTargetBits(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !key.KeyFrame || key.FrameTargetBits != e.rc.bufferInitialBits/2 {
		t.Fatalf("key target = key:%t bits:%d, want initial buffer half %d", key.KeyFrame, key.FrameTargetBits, e.rc.bufferInitialBits/2)
	}
	wantRC := e.rc
	wantRC.beginFrame(false)

	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame || inter.FrameTargetBits != wantRC.frameTargetBits {
		t.Fatalf("inter target = key:%t bits:%d, want libvpx CBR buffer target %d", inter.KeyFrame, inter.FrameTargetBits, wantRC.frameTargetBits)
	}
}

func TestEncodeIntoCapsKeyFrameTargetBitsWithMaxIntraBitrate(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxIntraBitratePct:  200,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	result, err := e.EncodeInto(make([]byte, 4096), testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if result.FrameTargetBits != e.rc.bitsPerFrame*2 {
		t.Fatalf("key target bits = %d, want max intra cap %d", result.FrameTargetBits, e.rc.bitsPerFrame*2)
	}
}

func TestEncodeIntoUsesLibvpxLaterForcedKeyFrameTargetBits(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 4096)
	for i := 0; i < 20; i++ {
		if _, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, i), uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
	}
	wantRC := e.rc
	wantRC.beginFrameWithTargetAndContext(true, wantRC.bitsPerFrame, rateControlFrameContext{
		forcedKeyFrame:     true,
		temporalLayerCount: 1,
		timing:             e.timing,
	})

	e.ForceKeyFrame()
	result, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 20), 20, 1, 0)
	if err != nil {
		t.Fatalf("forced key EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame || result.FrameTargetBits != wantRC.frameTargetBits {
		t.Fatalf("forced key target = key:%t bits:%d, want %d", result.KeyFrame, result.FrameTargetBits, wantRC.frameTargetBits)
	}
}

func TestEncodeIntoGFCBRBoostRefreshesGoldenOnInterval(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		GFCBRBoostPct:       100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
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
		state := packetState(t, inter.Data)
		if frame < 11 {
			if state.Refresh.RefreshGolden {
				t.Fatalf("inter %d refresh golden = true, want false before interval", frame)
			}
			if inter.FrameTargetBits != wantTarget {
				t.Fatalf("inter %d target = %d, want libvpx CBR buffer target %d", frame, inter.FrameTargetBits, wantTarget)
			}
			continue
		}
		if !state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = false, want true at GF CBR interval", frame)
		}
		if inter.FrameTargetBits != wantTarget {
			t.Fatalf("inter %d target = %d, want boosted libvpx CBR target %d", frame, inter.FrameTargetBits, wantTarget)
		}
	}
}

func TestEncodeIntoDefaultCBRRefreshesGoldenOnLibvpxInterval(t *testing.T) {
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
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	for frame := 1; frame <= 11; frame++ {
		wantRC := e.rc
		wantRC.beginFrame(false)
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		state := packetState(t, inter.Data)
		if frame < 11 && state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = true, want false before interval", frame)
		}
		if frame < 11 && state.Refresh.CopyBufferToAltRef != 0 {
			t.Fatalf("inter %d copy-to-alt = %d, want none before GF refresh", frame, state.Refresh.CopyBufferToAltRef)
		}
		if frame == 11 && !state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = false, want default libvpx CBR GF refresh", frame)
		}
		if frame == 11 && state.Refresh.CopyBufferToAltRef != 2 {
			t.Fatalf("inter %d copy-to-alt = %d, want libvpx old-GF-to-ARF copy", frame, state.Refresh.CopyBufferToAltRef)
		}
		if inter.FrameTargetBits != wantRC.frameTargetBits {
			t.Fatalf("inter %d target = %d, want unboosted libvpx CBR target %d", frame, inter.FrameTargetBits, wantRC.frameTargetBits)
		}
	}
}

func TestGFCBRBoostRequiresPriorLastZeroMVMajority(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		GFCBRBoostPct:       100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	e.rc.framesSinceKeyframe = e.goldenFrameCBRInterval(rows, cols)

	e.lastInterZeroMVCount = rows * cols / 2
	if e.shouldRefreshGoldenFrameCBR(false, false, 0, rows, cols) {
		t.Fatalf("shouldRefreshGoldenFrameCBR = true, want false without LAST/ZEROMV majority")
	}
	e.lastInterZeroMVCount = rows*cols/2 + 1
	if !e.shouldRefreshGoldenFrameCBR(false, false, 0, rows, cols) {
		t.Fatalf("shouldRefreshGoldenFrameCBR = false, want true with LAST/ZEROMV majority")
	}
}

func TestGoldenFrameCBRIntervalMirrorsLibvpxCyclicRefreshCadence(t *testing.T) {
	e := newSizedTestEncoder(t, 64, 80)

	if got := e.goldenFrameCBRInterval(encoderMacroblockRows(e.opts.Height), encoderMacroblockCols(e.opts.Width)); got != 40 {
		t.Fatalf("GF CBR interval = %d, want libvpx cyclic-refresh cadence clamp 40", got)
	}
}

func TestEncodeIntoGFCBRBoostDisabledForErrorResilient(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		GFCBRBoostPct:       100,
		ErrorResilient:      true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	for frame := 1; frame <= 11; frame++ {
		inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		state := packetState(t, inter.Data)
		if state.Refresh.RefreshGolden {
			t.Fatalf("inter %d refresh golden = true, want disabled for error resilient", frame)
		}
	}
}

func TestEncodeIntoCQLevelSelectsQuantizer(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             32,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 4096)
	key, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if key.Quantizer != 32 || packetBaseQIndex(t, key.Data) != libvpxPublicQuantizerToQIndex(32) {
		t.Fatalf("key quantizer = result:%d packet:%d, want public CQ level 32 / qindex %d", key.Quantizer, packetBaseQIndex(t, key.Data), libvpxPublicQuantizerToQIndex(32))
	}
	inter, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.Quantizer != 32 || packetBaseQIndex(t, inter.Data) != libvpxPublicQuantizerToQIndex(32) {
		t.Fatalf("inter quantizer = result:%d packet:%d, want public CQ level 32 / qindex %d", inter.Quantizer, packetBaseQIndex(t, inter.Data), libvpxPublicQuantizerToQIndex(32))
	}
}

func TestEncodeIntoCQDefaultLevelMirrorsLibvpx(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
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
	if e.opts.CQLevel != defaultCQLevel || e.rc.currentQuantizer != libvpxPublicQuantizerToQIndex(defaultCQLevel) {
		t.Fatalf("default CQ = opts:%d q:%d, want public %d / qindex %d", e.opts.CQLevel, e.rc.currentQuantizer, defaultCQLevel, libvpxPublicQuantizerToQIndex(defaultCQLevel))
	}
}

func TestEncodeIntoCQOutputBitrateAdaptsToContent(t *testing.T) {
	newCQEncoder := func(t *testing.T) *VP8Encoder {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:               32,
			Height:              32,
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
		return e
	}
	flat := testImage(32, 32)
	fillImage(flat, 90, 90, 170)
	detailed := rateControlTestFrame(32, 32, 3)
	dst := make([]byte, 16384)

	flatResult, err := newCQEncoder(t).EncodeInto(dst, flat, 0, 1, 0)
	if err != nil {
		t.Fatalf("flat EncodeInto returned error: %v", err)
	}
	detailedResult, err := newCQEncoder(t).EncodeInto(dst, detailed, 0, 1, 0)
	if err != nil {
		t.Fatalf("detailed EncodeInto returned error: %v", err)
	}
	if detailedResult.SizeBytes <= flatResult.SizeBytes {
		t.Fatalf("CQ sizes = detailed:%d flat:%d, want detailed content to use more bits", detailedResult.SizeBytes, flatResult.SizeBytes)
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
	initialRollingActual := e.rc.rollingActualBits
	initialRollingTarget := e.rc.rollingTargetBits
	initialLongRollingActual := e.rc.longRollingActualBits
	initialLongRollingTarget := e.rc.longRollingTargetBits
	result, err := e.EncodeInto(make([]byte, 4096), testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	wantRollingActual := libvpxRollingBits(initialRollingActual, result.SizeBytes*8, 3, 2)
	wantRollingTarget := libvpxRollingBits(initialRollingTarget, result.FrameTargetBits, 3, 2)
	if e.rc.rollingActualBits != wantRollingActual || e.rc.rollingTargetBits != wantRollingTarget {
		t.Fatalf("rolling bits = actual:%d target:%d, want %d/%d", e.rc.rollingActualBits, e.rc.rollingTargetBits, wantRollingActual, wantRollingTarget)
	}
	wantLongRollingActual := libvpxRollingBits(initialLongRollingActual, result.SizeBytes*8, 31, 5)
	wantLongRollingTarget := libvpxRollingBits(initialLongRollingTarget, result.FrameTargetBits, 31, 5)
	if e.rc.longRollingActualBits != wantLongRollingActual || e.rc.longRollingTargetBits != wantLongRollingTarget {
		t.Fatalf("long rolling bits = actual:%d target:%d, want %d/%d", e.rc.longRollingActualBits, e.rc.longRollingTargetBits, wantLongRollingActual, wantLongRollingTarget)
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
	if got := packetBaseQIndex(t, result.Data); got != libvpxPublicQuantizerToQIndex(result.Quantizer) {
		t.Fatalf("packet base q = %d, want public result quantizer %d mapped to qindex %d", got, result.Quantizer, libvpxPublicQuantizerToQIndex(result.Quantizer))
	}
	if e.rc.lastQuantizer != packetBaseQIndex(t, result.Data) {
		t.Fatalf("last quantizer = %d, want committed packet qindex %d", e.rc.lastQuantizer, packetBaseQIndex(t, result.Data))
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
	wantAltQ := int8(libvpxPublicQuantizerToQIndex(20)/2 - libvpxPublicQuantizerToQIndex(20))
	if got := keyState.Segmentation.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != wantAltQ {
		t.Fatalf("key static segment alt-q = %d, want %d", got, wantAltQ)
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

func TestCyclicRefreshSegmentationConfigMirrorsLibvpxEnablementAndBoost(t *testing.T) {
	e := VP8Encoder{}
	e.rc.mode = RateControlCBR
	e.rc.currentQuantizer = 20

	cfg := e.cyclicRefreshSegmentationConfig(false)

	if !cfg.Enabled || !cfg.UpdateMap || !cfg.UpdateData {
		t.Fatalf("cyclic segmentation = %+v, want enabled map/data update", cfg)
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != -10 {
		t.Fatalf("cyclic segment alt-q = %d, want background delta -10", got)
	}

	e.rc.currentQuantizer = 21
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled {
		t.Fatalf("q=21 cyclic segmentation disabled, want background boost")
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != -11 {
		t.Fatalf("q=21 cyclic segment alt-q = %d, want libvpx Q/2-Q delta -11", got)
	}

	e.rc.currentQuantizer = 1
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled {
		t.Fatalf("q=1 cyclic segmentation disabled, want libvpx Q/2-Q delta enabled")
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != -1 {
		t.Fatalf("q=1 cyclic segment alt-q = %d, want libvpx Q/2-Q delta -1", got)
	}

	e.rc.currentQuantizer = 0
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled || cfg.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] {
		t.Fatalf("q=0 cyclic segmentation = %+v, want enabled with no alt-q feature", cfg)
	}

	e.rc.mode = RateControlVBR
	e.opts.StaticThreshold = 1
	if cfg := e.cyclicRefreshSegmentationConfig(false); cfg.Enabled {
		t.Fatalf("VBR static-threshold cyclic segmentation = %+v, want disabled", cfg)
	}

	e.rc.mode = RateControlCBR
	e.opts.ScreenContentMode = 2
	if cfg := e.cyclicRefreshSegmentationConfig(true); cfg.Enabled {
		t.Fatalf("screen-content mode 2 golden-refresh segmentation = %+v, want disabled", cfg)
	}
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("screen-content mode 2 non-golden cyclic segmentation disabled, want enabled")
	}
}

func TestEncodeIntoDefaultCBREnablesLibvpxCyclicRefreshSegmentation(t *testing.T) {
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
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled || !keyState.Segmentation.UpdateMap || !keyState.Segmentation.UpdateData {
		t.Fatalf("key segmentation = %+v, want libvpx default cyclic refresh", keyState.Segmentation)
	}
	if got := keyState.Segmentation.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != -2 {
		t.Fatalf("key cyclic alt-q = %d, want libvpx q/2-q delta -2", got)
	}

	inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	interState := packetState(t, inter.Data)
	if !interState.Segmentation.Enabled || !interState.Segmentation.UpdateMap || !interState.Segmentation.UpdateData {
		t.Fatalf("inter segmentation = %+v, want libvpx default cyclic refresh", interState.Segmentation)
	}
}

func TestEncodeIntoScreenContentMode2DisablesGoldenRefreshCyclicSegmentation(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		ScreenContentMode:   2,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if keyState.Segmentation.Enabled || keyState.Segmentation.UpdateMap || keyState.Segmentation.UpdateData {
		t.Fatalf("screen-content mode 2 key segmentation = %+v, want disabled on golden refresh", keyState.Segmentation)
	}
}

func TestCyclicRefreshSegmentationTreeProbsMirrorLibvpxCounts(t *testing.T) {
	cfg := vp8enc.SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true}
	keyModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 0}}

	updateKeyFrameSegmentationTreeProbs(&cfg, keyModes)
	if cfg.TreeProbUpdated != ([vp8common.MBFeatureTreeProbs]bool{}) {
		t.Fatalf("key tree prob updates = %v, want none for all-zero segment map", cfg.TreeProbUpdated)
	}

	cfg = vp8enc.SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true}
	interModes := make([]vp8enc.InterFrameMacroblockMode, 40)
	interModes[0].SegmentID = staticSegmentID
	interModes[1].SegmentID = staticSegmentID

	updateInterFrameSegmentationTreeProbs(&cfg, interModes)
	if cfg.TreeProbUpdated[0] || !cfg.TreeProbUpdated[1] || cfg.TreeProbUpdated[2] {
		t.Fatalf("inter tree prob update flags = %v, want only branch 1 updated", cfg.TreeProbUpdated)
	}
	if got := cfg.TreeProbs[1]; got != 242 {
		t.Fatalf("inter tree prob[1] = %d, want libvpx count-derived 242", got)
	}
}

func TestAssignInterFrameStaticSegmentsUsesCyclicRefreshCadence(t *testing.T) {
	modes := make([]vp8enc.InterFrameMacroblockMode, 40)
	refreshCount := cyclicRefreshMaxMBsPerFrameForLayers(4, 10, 1)

	assignInterFrameStaticSegments(4, 10, 0, refreshCount, modes)

	if modes[0].SegmentID != staticSegmentID || modes[1].SegmentID != staticSegmentID {
		t.Fatalf("first cyclic segment IDs = %d/%d, want refreshed", modes[0].SegmentID, modes[1].SegmentID)
	}
	if modes[2].SegmentID != 0 || modes[len(modes)-1].SegmentID != 0 {
		t.Fatalf("later cyclic segment IDs = %d/%d, want zero", modes[2].SegmentID, modes[len(modes)-1].SegmentID)
	}

	assignInterFrameStaticSegments(4, 10, 2, refreshCount, modes)
	if modes[0].SegmentID != 0 || modes[1].SegmentID != 0 {
		t.Fatalf("previous cyclic segment IDs = %d/%d, want cleared", modes[0].SegmentID, modes[1].SegmentID)
	}
	if modes[2].SegmentID != staticSegmentID || modes[3].SegmentID != staticSegmentID {
		t.Fatalf("rotated cyclic segment IDs = %d/%d, want refreshed", modes[2].SegmentID, modes[3].SegmentID)
	}

	assignInterFrameStaticSegments(4, 10, 39, refreshCount, modes)
	if modes[39].SegmentID != staticSegmentID || modes[0].SegmentID != staticSegmentID {
		t.Fatalf("wrapped cyclic segment IDs = %d/%d, want refreshed", modes[39].SegmentID, modes[0].SegmentID)
	}
	if modes[1].SegmentID != 0 || modes[38].SegmentID != 0 {
		t.Fatalf("wrapped neighbor segment IDs = %d/%d, want zero", modes[1].SegmentID, modes[38].SegmentID)
	}
}

func TestAssignInterFrameStaticSegmentsUsesCyclicRefreshMapEligibility(t *testing.T) {
	modes := make([]vp8enc.InterFrameMacroblockMode, 5)
	refreshMap := []int8{0, -1, 1, 0, 0}

	next := assignInterFrameStaticSegmentsWithMap(1, 5, 0, 2, refreshMap, modes)

	if next != 4 {
		t.Fatalf("next cyclic refresh index = %d, want 4 after skipped cooldown/dirty blocks", next)
	}
	if modes[0].SegmentID != staticSegmentID || modes[3].SegmentID != staticSegmentID {
		t.Fatalf("eligible segment IDs = %d/%d, want refreshed", modes[0].SegmentID, modes[3].SegmentID)
	}
	if modes[1].SegmentID != 0 || modes[2].SegmentID != 0 || modes[4].SegmentID != 0 {
		t.Fatalf("ineligible segment IDs = %d/%d/%d, want zero", modes[1].SegmentID, modes[2].SegmentID, modes[4].SegmentID)
	}
	if refreshMap[1] != 0 {
		t.Fatalf("cooldown map[1] = %d, want incremented to candidate 0", refreshMap[1])
	}
	if refreshMap[2] != 1 {
		t.Fatalf("dirty map[2] = %d, want unchanged", refreshMap[2])
	}
}

func TestCyclicRefreshStaticClassificationMasksSkinBlocks(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               160,
		Height:              64,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(160, 64)
	fillImage(src, 128, 128, 128)
	fillMacroblock(src, 0, 0, 120, 117, 150)
	modes := make([]vp8enc.InterFrameMacroblockMode, 40)

	next := e.assignInterFrameStaticSegments(sourceImageFromPublic(src), 4, 10, modes)

	if e.skinMap[0] != 1 {
		t.Fatalf("skinMap[0] = %d, want libvpx skin classification", e.skinMap[0])
	}
	if modes[0].SegmentID != 0 || modes[1].SegmentID != staticSegmentID || modes[2].SegmentID != staticSegmentID {
		t.Fatalf("segment IDs = %d/%d/%d, want skin block masked and next two refreshed", modes[0].SegmentID, modes[1].SegmentID, modes[2].SegmentID)
	}
	if next != 3 {
		t.Fatalf("next cyclic refresh index = %d, want 3 after masked skin block", next)
	}
}

func TestUpdateConsecutiveZeroLastMirrorsLibvpxCounter(t *testing.T) {
	counters := []uint8{0, 254, 7}
	modes := []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
	}

	updateConsecutiveZeroLast(modes, counters)
	updateConsecutiveZeroLast(modes, counters)

	want := []uint8{2, 255, 0}
	for i := range want {
		if counters[i] != want[i] {
			t.Fatalf("counter[%d] = %d, want %d", i, counters[i], want[i])
		}
	}
}

func TestUpdateCyclicRefreshMapFromInterFrameMirrorsLibvpxStates(t *testing.T) {
	refreshMap := []int8{0, 1, 0, 0}
	modes := []vp8enc.InterFrameMacroblockMode{
		{SegmentID: staticSegmentID, RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV},
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
	}

	updateCyclicRefreshMapFromInterFrame(modes, refreshMap)

	want := []int8{-1, 0, 1, 1}
	for i := range want {
		if refreshMap[i] != want[i] {
			t.Fatalf("refreshMap[%d] = %d, want %d", i, refreshMap[i], want[i])
		}
	}
}

func TestCyclicRefreshMaxMBsPerFrameMirrorsLibvpxLayerCadence(t *testing.T) {
	if got := cyclicRefreshMaxMBsPerFrameForLayers(8, 8, 1); got != 3 {
		t.Fatalf("one-layer cyclic refresh MBs = %d, want libvpx MBs/20", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForLayers(8, 8, 2); got != 6 {
		t.Fatalf("two-layer cyclic refresh MBs = %d, want libvpx MBs/10", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForLayers(8, 8, 3); got != 9 {
		t.Fatalf("three-layer cyclic refresh MBs = %d, want libvpx MBs/7", got)
	}
}

func TestCyclicRefreshMaxMBsPerFrameMirrorsLibvpxScreenContentCadence(t *testing.T) {
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 1, 100, 0, 0); got != 6 {
		t.Fatalf("screen-content high-q cyclic refresh MBs = %d, want libvpx MBs/10", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 2, 80, 0, 0); got != 6 {
		t.Fatalf("aggressive screen-content high-q cyclic refresh MBs = %d, want libvpx MBs/10", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 1, 19, 251, 61); got != 0 {
		t.Fatalf("screen-content stable low-q cyclic refresh MBs = %d, want disabled", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 1, 19, 251, 60); got != 3 {
		t.Fatalf("screen-content low-q cyclic refresh MBs = %d, want libvpx MBs/20", got)
	}
}

func TestEncodeIntoStaticThresholdRotatesCyclicRefreshSegments(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               80,
		Height:              64,
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
	packet := make([]byte, 65536)
	keySource := testImage(80, 64)
	fillImage(keySource, 128, 128, 128)
	key, err := e.EncodeInto(packet, keySource, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
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

	for frame := 0; frame < 3; frame++ {
		src := publicImageFromVP8(&e.lastRef.Img)
		inter, err := e.EncodeInto(packet, src, uint64(frame+1), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		if err := d.Decode(inter.Data); err != nil {
			t.Fatalf("inter %d Decode returned error: %v", frame, err)
		}
		if d.modes[frame].SegmentID != staticSegmentID {
			t.Fatalf("inter %d segment[%d] = %d, want cyclic refresh", frame, frame, d.modes[frame].SegmentID)
		}
		for i := 0; i <= frame+1 && i < len(d.modes); i++ {
			if i == frame {
				continue
			}
			if d.modes[i].SegmentID != 0 {
				t.Fatalf("inter %d segment[%d] = %d, want zero", frame, i, d.modes[i].SegmentID)
			}
		}
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("inter %d NextFrame returned no frame", frame)
		}
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

func TestEncodeIntoStaticThresholdSkipsTemporalEnhancementLayerSegmentation(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	if err := e.SetStaticThreshold(1); err != nil {
		t.Fatalf("SetStaticThreshold returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	packet := make([]byte, 4096)

	key, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled {
		t.Fatalf("key segmentation disabled, want base-layer cyclic refresh enabled")
	}

	enhancement, err := e.EncodeInto(packet, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("enhancement EncodeInto returned error: %v", err)
	}
	if enhancement.TemporalLayerID != 1 {
		t.Fatalf("enhancement temporal layer = %d, want 1", enhancement.TemporalLayerID)
	}
	enhancementState := packetState(t, enhancement.Data)
	if enhancementState.Segmentation.Enabled || enhancementState.Segmentation.UpdateMap || enhancementState.Segmentation.UpdateData {
		t.Fatalf("enhancement segmentation = %+v, want disabled like libvpx non-base temporal layer", enhancementState.Segmentation)
	}
}

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
	wantKeyBuffer := e.rc.bufferInitialBits - encodedSizeBits(key.SizeBytes)
	if wantKeyBuffer < 0 {
		wantKeyBuffer = 0
	}
	if key.BufferLevelBits != wantKeyBuffer || e.rc.bufferLevelBits != wantKeyBuffer {
		t.Fatalf("invisible key buffer = result:%d rc:%d, want %d", key.BufferLevelBits, e.rc.bufferLevelBits, wantKeyBuffer)
	}

	beforeInterBuffer := e.rc.bufferLevelBits
	inter, err := e.EncodeInto(dst, src, 1, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible inter EncodeInto returned error: %v", err)
	}
	wantInterBuffer := beforeInterBuffer - encodedSizeBits(inter.SizeBytes)
	if wantInterBuffer < 0 {
		wantInterBuffer = 0
	}
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

func TestResetRestoresRateControlQuantizerAverages(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 4096)
	for i := 0; i < 4; i++ {
		if _, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, i), uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
	}
	if e.rc.normalInterFrames == 0 {
		t.Fatalf("normalInterFrames = 0, want precondition inter history before reset")
	}
	e.rc.rateCorrectionFactor = 2.0
	e.rc.keyFrameCorrectionFactor = 3.0
	e.rc.goldenCorrectionFactor = 4.0
	e.rc.currentQuantizer = 40
	e.rc.lastQuantizer = 39
	e.rc.lastInterQuantizer = 38
	e.rc.frameTargetBits = 123

	e.Reset()

	if e.rc.avgFrameQuantizer != e.rc.maxQuantizer || e.rc.normalInterFrames != 0 || e.rc.normalInterQuantizerTotal != 0 || e.rc.normalInterAvgQuantizer != e.rc.maxQuantizer {
		t.Fatalf("quantizer averages after reset = avg:%d frames:%d total:%d normal:%d, want max/0/0/max", e.rc.avgFrameQuantizer, e.rc.normalInterFrames, e.rc.normalInterQuantizerTotal, e.rc.normalInterAvgQuantizer)
	}
	if e.rc.rollingActualBits != e.rc.bitsPerFrame || e.rc.rollingTargetBits != e.rc.bitsPerFrame ||
		e.rc.longRollingActualBits != e.rc.bitsPerFrame || e.rc.longRollingTargetBits != e.rc.bitsPerFrame {
		t.Fatalf("rolling bits after reset = short:%d/%d long:%d/%d, want libvpx per-frame bandwidth %d",
			e.rc.rollingActualBits, e.rc.rollingTargetBits, e.rc.longRollingActualBits, e.rc.longRollingTargetBits, e.rc.bitsPerFrame)
	}
	if e.rc.rateCorrectionFactor != 1.0 || e.rc.keyFrameCorrectionFactor != 1.0 || e.rc.goldenCorrectionFactor != 1.0 {
		t.Fatalf("correction factors after reset = %g/%g/%g, want 1/1/1", e.rc.rateCorrectionFactor, e.rc.keyFrameCorrectionFactor, e.rc.goldenCorrectionFactor)
	}
	if e.rc.currentQuantizer != e.rc.minQuantizer || e.rc.lastQuantizer != e.rc.minQuantizer || e.rc.lastInterQuantizer != e.rc.minQuantizer {
		t.Fatalf("quantizers after reset = current:%d last:%d lastInter:%d, want min %d", e.rc.currentQuantizer, e.rc.lastQuantizer, e.rc.lastInterQuantizer, e.rc.minQuantizer)
	}
	if e.rc.frameTargetBits != e.rc.bitsPerFrame {
		t.Fatalf("frame target after reset = %d, want bitsPerFrame %d", e.rc.frameTargetBits, e.rc.bitsPerFrame)
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
	if keyState.LoopFilter.Level != 9 || keyState.LoopFilter.SharpnessLevel != 0 {
		t.Fatalf("key loop filter = %+v, want level 9 sharpness 0", keyState.LoopFilter)
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
	if interState.LoopFilter.Level != 9 || interState.LoopFilter.SharpnessLevel != 3 {
		t.Fatalf("inter loop filter = %+v, want level 9 sharpness 3", interState.LoopFilter)
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	assertImagesEqual(t, "filtered inter current", decoded[1], publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "filtered inter last", decoded[1], publicImageFromVP8(&e.lastRef.Img))
}

func TestEncodeIntoDefaultSharpnessStillAppliesLoopFilter(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        20,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(32, 16)
	fillImage(src, 220, 90, 170)
	for row := 0; row < src.Height; row++ {
		for col := 16; col < src.Width; col++ {
			src.Y[row*src.YStride+col] = 40
		}
	}

	result, err := e.EncodeInto(make([]byte, 8192), src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	state := parseEncoderStateHeader(t, result.Data)
	if state.LoopFilter.Level != 9 || state.LoopFilter.SharpnessLevel != 0 {
		t.Fatalf("loop filter = %+v, want level 9 sharpness 0", state.LoopFilter)
	}
	decoded := decodeSingleFrame(t, result.Data)
	assertImagesEqual(t, "default filtered current", decoded, publicImageFromVP8(&e.current.Img))
}

func TestLibvpxInitialLoopFilterLevelUsesBaseQThreeEighths(t *testing.T) {
	tests := []struct {
		qIndex int
		want   int
	}{
		{qIndex: 0, want: 0},
		{qIndex: 6, want: 2},
		{qIndex: 16, want: 6},
		{qIndex: 20, want: 7},
		{qIndex: 127, want: 47},
		{qIndex: 1000, want: 63},
	}
	for _, tt := range tests {
		if got := libvpxInitialLoopFilterLevel(tt.qIndex); got != tt.want {
			t.Fatalf("q=%d loop filter level = %d, want %d", tt.qIndex, got, tt.want)
		}
	}
}

func TestEncoderLoopFilterUsesPreviousInterLevelWithLibvpxClamp(t *testing.T) {
	e := &VP8Encoder{
		opts:            EncoderOptions{Sharpness: 3},
		rc:              rateControlState{currentQuantizer: 40},
		loopFilterLevel: 13,
	}
	level, sharpness := e.encoderLoopFilter(vp8common.InterFrame)
	if level != 13 || sharpness != 3 {
		t.Fatalf("inter loop filter = level:%d sharpness:%d, want previous 13 sharpness 3", level, sharpness)
	}

	e.loopFilterLevel = 0
	level, _ = e.encoderLoopFilter(vp8common.InterFrame)
	if level != 5 {
		t.Fatalf("clamped inter loop filter level = %d, want libvpx min q/8 = 5", level)
	}

	level, sharpness = e.encoderLoopFilter(vp8common.KeyFrame)
	if level != 15 || sharpness != 0 {
		t.Fatalf("key loop filter = level:%d sharpness:%d, want q*3/8=15 sharpness 0", level, sharpness)
	}
}

func TestEncoderLoopFilterHeaderMirrorsLibvpxDefaultDeltasAcrossQualities(t *testing.T) {
	tests := []struct {
		name      string
		deadline  Deadline
		wantModes [vp8common.MaxModeLFDeltas]int8
	}{
		{name: "best quality", deadline: DeadlineBestQuality, wantModes: [vp8common.MaxModeLFDeltas]int8{4, -2, 2, 4}},
		{name: "good quality", deadline: DeadlineGoodQuality, wantModes: [vp8common.MaxModeLFDeltas]int8{4, -2, 2, 4}},
		{name: "realtime", deadline: DeadlineRealtime, wantModes: [vp8common.MaxModeLFDeltas]int8{4, -12, 2, 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline}}
			header := e.encoderLoopFilterHeader(17, 3)
			if !header.DeltaEnabled || !header.DeltaUpdate {
				t.Fatalf("delta flags = enabled:%t update:%t, want enabled update", header.DeltaEnabled, header.DeltaUpdate)
			}
			if wantRefs := ([vp8common.MaxRefLFDeltas]int8{2, 0, -2, -2}); header.RefDeltas != wantRefs {
				t.Fatalf("ref deltas = %v, want %v", header.RefDeltas, wantRefs)
			}
			if header.ModeDeltas != tt.wantModes {
				t.Fatalf("mode deltas = %v, want %v", header.ModeDeltas, tt.wantModes)
			}
		})
	}

	e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime}}
	if header := e.encoderLoopFilterHeader(0, 3); header.DeltaEnabled || header.DeltaUpdate {
		t.Fatalf("zero-level delta flags = enabled:%t update:%t, want disabled", header.DeltaEnabled, header.DeltaUpdate)
	}
}

func TestLoopFilterUsesFastSearchMirrorsLibvpxAutoFilterSpeedFeature(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     bool
	}{
		{name: "best quality uses full search", deadline: DeadlineBestQuality, cpuUsed: 8, want: false},
		{name: "good speed four uses full search", deadline: DeadlineGoodQuality, cpuUsed: 4, want: false},
		{name: "good speed five uses fast search", deadline: DeadlineGoodQuality, cpuUsed: 5, want: true},
		{name: "realtime speed two uses full search", deadline: DeadlineRealtime, cpuUsed: 2, want: false},
		{name: "realtime speed three uses fast search", deadline: DeadlineRealtime, cpuUsed: 3, want: true},
		{name: "realtime speed four uses full search", deadline: DeadlineRealtime, cpuUsed: 4, want: false},
		{name: "realtime speed five uses fast search", deadline: DeadlineRealtime, cpuUsed: 5, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			if got := e.loopFilterUsesFastSearch(); got != tt.want {
				t.Fatalf("fast search = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLoopFilterPartialFrameWindowMirrorsLibvpxMiddleSlice(t *testing.T) {
	tests := []struct {
		rows      int
		wantStart int
		wantCount int
	}{
		{rows: 0, wantStart: 0, wantCount: 0},
		{rows: 1, wantStart: 0, wantCount: 1},
		{rows: 2, wantStart: 1, wantCount: 1},
		{rows: 4, wantStart: 2, wantCount: 1},
		{rows: 8, wantStart: 4, wantCount: 1},
		{rows: 16, wantStart: 8, wantCount: 2},
	}
	for _, tt := range tests {
		start, count := loopFilterPartialFrameWindow(tt.rows)
		if start != tt.wantStart || count != tt.wantCount {
			t.Fatalf("rows=%d partial window = %d,%d want %d,%d", tt.rows, start, count, tt.wantStart, tt.wantCount)
		}
	}
}

func TestLoopFilterLumaSSEPartialScoresOnlyMiddleWindow(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 20, 128, 128)
	ref := testVP8Frame(t, 64, 64, 20, 128, 128)
	for row := 0; row < 16; row++ {
		for col := 0; col < 64; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = 100
		}
	}
	for row := 32; row < 48; row++ {
		for col := 0; col < 64; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = 23
		}
	}

	got := loopFilterLumaSSE(sourceImageFromPublic(src), &ref.Img, 4, 4, true)
	want := 4 * 16 * 16 * 3 * 3
	if got != want {
		t.Fatalf("partial luma SSE = %d, want %d", got, want)
	}
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

func TestEncodeIntoCanEmitSplitMVForQuadrantMotion(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(32, 32)
	fillImage(first, 0, 90, 170)
	for row := 0; row < first.Height; row++ {
		for col := 0; col < first.Width; col++ {
			first.Y[row*first.YStride+col] = byte((row*37 + col*13) & 255)
		}
	}
	keyPacket := make([]byte, 32768)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := decodeSingleFrame(t, key.Data)
	second := testImage(32, 32)
	fillImage(second, 13, 90, 170)
	copyShifted8x8FromImage(second, reconstructed, 0, 0, 0, 1)
	copyShifted8x8FromImage(second, reconstructed, 0, 8, 1, 0)
	copyShifted8x8FromImage(second, reconstructed, 8, 0, 0, 2)
	copyShifted8x8FromImage(second, reconstructed, 8, 8, 2, 0)
	interPacket := make([]byte, 32768)

	inter, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	mode := e.interFrameModes[0]
	if mode.Mode != vp8common.SplitMV || mode.Partition != 2 {
		t.Fatalf("mode[0] = %+v, want SPLITMV partition 2", mode)
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
	decoded, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "splitmv encoder current", decoded, publicImageFromVP8(&e.current.Img))
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

func TestEncodeIntoInterFrameCanChooseBPredIntraAfterRDScoring(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 32)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
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
	if e.interFrameModes[1].RefFrame != vp8common.IntraFrame || e.interFrameModes[1].Mode != vp8common.BPred {
		t.Fatalf("inter mode[1] = %+v, want libvpx-style B_PRED intra candidate after RD scoring", e.interFrameModes[1])
	}
}

func TestEncodeIntoInterFrameCodesLargeUniformResidual(t *testing.T) {
	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
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

func TestEncodeIntoCanForceGoldenAndAltRefRefresh(t *testing.T) {
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

	inter, err := e.EncodeInto(interPacket, second, 1, 1, EncodeForceGoldenFrame|EncodeForceAltRefFrame)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	state := packetState(t, inter.Data)
	if !state.Refresh.RefreshLast || !state.Refresh.RefreshGolden || !state.Refresh.RefreshAltRef {
		t.Fatalf("refresh flags = %+v, want last/golden/altref refresh", state.Refresh)
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertImagesEqual(t, "current", decoded[1], publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "last", decoded[1], publicImageFromVP8(&e.lastRef.Img))
	assertImagesEqual(t, "golden", decoded[1], publicImageFromVP8(&e.goldenRef.Img))
	assertImagesEqual(t, "alt", decoded[1], publicImageFromVP8(&e.altRef.Img))
	if planeEqual(keyFrame.Y, keyFrame.YStride, e.goldenRef.Img.Y, e.goldenRef.Img.YStride, keyFrame.Width, keyFrame.Height) {
		t.Fatalf("golden reference still matches keyframe after forced refresh")
	}
}

func TestEncodeIntoRejectsConflictingForceReferenceFlags(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 220, 90, 170)
	dst := make([]byte, 4096)

	tests := []struct {
		name  string
		flags EncodeFlags
	}{
		{name: "golden", flags: EncodeForceGoldenFrame | EncodeNoUpdateGolden},
		{name: "altref", flags: EncodeForceAltRefFrame | EncodeNoUpdateAltRef},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := e.EncodeInto(dst, src, 0, 1, tt.flags); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("EncodeInto error = %v, want ErrInvalidConfig", err)
			}
			if e.frameCount != 0 {
				t.Fatalf("frameCount = %d, want no mutation after invalid flags", e.frameCount)
			}
		})
	}
}

func TestBoostedReferenceRateControlFrameMirrorsLibvpxRefreshFlags(t *testing.T) {
	if !boostedReferenceRateControlFrame(true, 0) {
		t.Fatalf("golden CBR refresh = false, want boosted reference rate-control frame")
	}
	if !boostedReferenceRateControlFrame(false, EncodeForceGoldenFrame) {
		t.Fatalf("force golden refresh = false, want boosted reference rate-control frame")
	}
	if !boostedReferenceRateControlFrame(false, EncodeForceAltRefFrame) {
		t.Fatalf("force altref refresh = false, want boosted reference rate-control frame")
	}
	if boostedReferenceRateControlFrame(false, EncodeNoUpdateGolden|EncodeNoUpdateAltRef) {
		t.Fatalf("no-update flags = true, want normal inter rate-control frame")
	}
}

func TestShouldCopyOldGoldenToAltRefOnGoldenRefreshMirrorsLibvpxPolicy(t *testing.T) {
	if !shouldCopyOldGoldenToAltRefOnGoldenRefresh(false, true, 0) {
		t.Fatalf("internal GF refresh copy = false, want libvpx copy old GF to ARF")
	}
	if shouldCopyOldGoldenToAltRefOnGoldenRefresh(true, true, 0) {
		t.Fatalf("error-resilient GF refresh copy = true, want disabled")
	}
	if shouldCopyOldGoldenToAltRefOnGoldenRefresh(false, true, EncodeForceGoldenFrame) {
		t.Fatalf("user-forced GF refresh copy = true, want disabled for external refresh flags")
	}
	if shouldCopyOldGoldenToAltRefOnGoldenRefresh(false, true, EncodeNoUpdateLast) {
		t.Fatalf("user reference-update flags copy = true, want disabled for external refresh flags")
	}
	if shouldCopyOldGoldenToAltRefOnGoldenRefresh(false, false, 0) {
		t.Fatalf("non-GF-refresh copy = true, want disabled")
	}
}

func TestRefreshInterFrameReferencesCopiesOldGoldenToAltBeforeGoldenRefresh(t *testing.T) {
	e := newTestEncoder(t)
	fillVP8Image(&e.lastRef.Img, 10)
	fillVP8Image(&e.goldenRef.Img, 20)
	fillVP8Image(&e.altRef.Img, 30)
	fillVP8Image(&e.analysis.Img, 40)

	e.refreshInterFrameReferencesFromAnalysis(vp8enc.InterFrameStateConfig{
		RefreshLast:        true,
		RefreshGolden:      true,
		CopyBufferToAltRef: 2,
	})

	if e.altRef.Img.Y[0] != 20 {
		t.Fatalf("alt Y[0] = %d, want old golden 20", e.altRef.Img.Y[0])
	}
	if e.goldenRef.Img.Y[0] != 40 {
		t.Fatalf("golden Y[0] = %d, want current 40", e.goldenRef.Img.Y[0])
	}
	if e.lastRef.Img.Y[0] != 40 {
		t.Fatalf("last Y[0] = %d, want current 40", e.lastRef.Img.Y[0])
	}
}

func TestEncodeIntoAppliesTemporalScalabilityMode1(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	var results [4]EncodeResult
	for i := range results {
		result, err := e.EncodeInto(dst, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
		results[i] = result
		results[i].Data = append([]byte(nil), result.Data...)
	}

	wantLayerID := []int{0, 1, 0, 1}
	wantTL0 := []uint8{0, 0, 1, 1}
	wantLayerSync := []bool{false, true, false, false}
	wantTargetBits := []int{240000, 32000, 48000, 32000}
	wantLayerBitrate := []int{720, 480, 720, 480}
	wantCumulativeBitrate := []int{720, 1200, 720, 1200}
	for i := range results {
		if results[i].TemporalLayerID != wantLayerID[i] ||
			results[i].TemporalLayerCount != 2 ||
			results[i].TL0PICIDX != wantTL0[i] ||
			results[i].TemporalLayerSync != wantLayerSync[i] ||
			results[i].FrameTargetBits != wantTargetBits[i] ||
			results[i].TemporalLayerTargetBitrateKbps != wantLayerBitrate[i] ||
			results[i].TemporalLayerCumulativeBitrateKbps != wantCumulativeBitrate[i] {
			t.Fatalf("result[%d] temporal = id:%d count:%d tl0:%d sync:%t target:%d layerKbps:%d cumulativeKbps:%d, want %d/2/%d/%t/%d/%d/%d", i, results[i].TemporalLayerID, results[i].TemporalLayerCount, results[i].TL0PICIDX, results[i].TemporalLayerSync, results[i].FrameTargetBits, results[i].TemporalLayerTargetBitrateKbps, results[i].TemporalLayerCumulativeBitrateKbps, wantLayerID[i], wantTL0[i], wantLayerSync[i], wantTargetBits[i], wantLayerBitrate[i], wantCumulativeBitrate[i])
		}
	}
	if !results[0].KeyFrame || results[1].KeyFrame || results[2].KeyFrame || results[3].KeyFrame {
		t.Fatalf("keyframe flags = %t/%t/%t/%t, want only first keyframe", results[0].KeyFrame, results[1].KeyFrame, results[2].KeyFrame, results[3].KeyFrame)
	}

	enhancement := packetState(t, results[1].Data)
	if enhancement.Refresh.RefreshLast || !enhancement.Refresh.RefreshGolden || enhancement.Refresh.RefreshAltRef {
		t.Fatalf("enhancement refresh = %+v, want golden-only refresh", enhancement.Refresh)
	}
	base := packetState(t, results[2].Data)
	if !base.Refresh.RefreshLast || base.Refresh.RefreshGolden || base.Refresh.RefreshAltRef {
		t.Fatalf("base refresh = %+v, want last-only refresh", base.Refresh)
	}
	secondEnhancement := packetState(t, results[3].Data)
	if secondEnhancement.Refresh.RefreshLast || !secondEnhancement.Refresh.RefreshGolden || secondEnhancement.Refresh.RefreshAltRef {
		t.Fatalf("second enhancement refresh = %+v, want golden-only refresh", secondEnhancement.Refresh)
	}
}

func TestEncodeIntoTracksLibvpxTemporalLayerAccounting(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	var sizes [4]int
	layerBuffer := [2]int{288000, 480000}
	layerFrameBandwidth := [2]int{48000, 40000}
	layerMaximumBuffer := [2]int{432000, 720000}
	var layerInput [2]int
	var layerEncoded [2]int
	var layerTotal [2]int
	var layerBits [2]int
	for i := range sizes {
		result, err := e.EncodeInto(dst, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
		sizes[i] = encodedSizeBits(result.SizeBytes)
		layerInput[result.TemporalLayerID]++
		for layer := result.TemporalLayerID; layer < result.TemporalLayerCount; layer++ {
			layerTotal[layer]++
			layerBits[layer] += sizes[i]
			layerBuffer[layer] = temporalTestBufferAfterFrame(layerBuffer[layer], layerFrameBandwidth[layer], layerMaximumBuffer[layer], sizes[i])
		}
		if !result.KeyFrame {
			layerEncoded[result.TemporalLayerID]++
		}
		if result.TemporalLayerFrameBandwidthBits != layerFrameBandwidth[result.TemporalLayerID] ||
			result.TemporalLayerMaximumBufferBits != layerMaximumBuffer[result.TemporalLayerID] ||
			result.TemporalLayerBufferLevelBits != layerBuffer[result.TemporalLayerID] {
			t.Fatalf("result[%d] temporal buffer = frame:%d level:%d max:%d, want %d/%d/%d", i, result.TemporalLayerFrameBandwidthBits, result.TemporalLayerBufferLevelBits, result.TemporalLayerMaximumBufferBits, layerFrameBandwidth[result.TemporalLayerID], layerBuffer[result.TemporalLayerID], layerMaximumBuffer[result.TemporalLayerID])
		}
		if result.TemporalLayerInputFrames != layerInput[result.TemporalLayerID] ||
			result.TemporalLayerEncodedFrames != layerEncoded[result.TemporalLayerID] ||
			result.TemporalLayerTotalEncodedFrames != layerTotal[result.TemporalLayerID] ||
			result.TemporalLayerEncodedBits != layerBits[result.TemporalLayerID] {
			t.Fatalf("result[%d] temporal counters = input:%d encoded:%d total:%d bits:%d, want %d/%d/%d/%d", i, result.TemporalLayerInputFrames, result.TemporalLayerEncodedFrames, result.TemporalLayerTotalEncodedFrames, result.TemporalLayerEncodedBits, layerInput[result.TemporalLayerID], layerEncoded[result.TemporalLayerID], layerTotal[result.TemporalLayerID], layerBits[result.TemporalLayerID])
		}
	}

	wantLayer0 := temporalLayerAccounting{
		InputFrames:        2,
		EncodedFrames:      1,
		TotalEncodedFrames: 2,
		EncodedBits:        sizes[0] + sizes[2],
		FrameBandwidthBits: layerFrameBandwidth[0],
		MaximumBufferBits:  layerMaximumBuffer[0],
		BufferLevelBits:    layerBuffer[0],
	}
	wantLayer1 := temporalLayerAccounting{
		InputFrames:        2,
		EncodedFrames:      2,
		TotalEncodedFrames: 4,
		EncodedBits:        sizes[0] + sizes[1] + sizes[2] + sizes[3],
		FrameBandwidthBits: layerFrameBandwidth[1],
		MaximumBufferBits:  layerMaximumBuffer[1],
		BufferLevelBits:    layerBuffer[1],
	}
	if got := e.temporal.accounting[0]; got != wantLayer0 {
		t.Fatalf("layer0 accounting = %+v, want %+v", got, wantLayer0)
	}
	if got := e.temporal.accounting[1]; got != wantLayer1 {
		t.Fatalf("layer1 accounting = %+v, want %+v", got, wantLayer1)
	}

	e.Reset()
	if got := e.temporal.accounting; got != ([MaxTemporalLayers]temporalLayerAccounting{}) {
		t.Fatalf("accounting after reset = %+v, want zero", got)
	}
}

func TestEncodeIntoTracksTemporalLayerBufferOnDroppedFrame(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyBits := encodedSizeBits(key.SizeBytes)
	layer0Buffer := temporalTestBufferAfterFrame(288000, 48000, 432000, keyBits)
	layer1Buffer := temporalTestBufferAfterFrame(480000, 40000, 720000, keyBits)

	e.rc.bufferLevelBits = -1
	dropped, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("dropped EncodeInto returned error: %v", err)
	}
	if !dropped.Dropped || dropped.TemporalLayerID != 1 {
		t.Fatalf("dropped result = dropped:%t layer:%d, want dropped layer 1", dropped.Dropped, dropped.TemporalLayerID)
	}
	layer1Buffer = temporalTestBufferAfterFrame(layer1Buffer, 40000, 720000, 0)
	if dropped.TemporalLayerFrameBandwidthBits != 40000 || dropped.TemporalLayerMaximumBufferBits != 720000 || dropped.TemporalLayerBufferLevelBits != layer1Buffer {
		t.Fatalf("dropped temporal buffer = frame:%d level:%d max:%d, want 40000/%d/720000", dropped.TemporalLayerFrameBandwidthBits, dropped.TemporalLayerBufferLevelBits, dropped.TemporalLayerMaximumBufferBits, layer1Buffer)
	}
	if dropped.TemporalLayerInputFrames != 1 || dropped.TemporalLayerEncodedFrames != 0 ||
		dropped.TemporalLayerTotalEncodedFrames != 1 || dropped.TemporalLayerEncodedBits != keyBits {
		t.Fatalf("dropped temporal counters = input:%d encoded:%d total:%d bits:%d, want 1/0/1/%d", dropped.TemporalLayerInputFrames, dropped.TemporalLayerEncodedFrames, dropped.TemporalLayerTotalEncodedFrames, dropped.TemporalLayerEncodedBits, keyBits)
	}

	wantLayer0 := temporalLayerAccounting{
		InputFrames:        1,
		TotalEncodedFrames: 1,
		EncodedBits:        keyBits,
		FrameBandwidthBits: 48000,
		MaximumBufferBits:  432000,
		BufferLevelBits:    layer0Buffer,
	}
	wantLayer1 := temporalLayerAccounting{
		InputFrames:        1,
		TotalEncodedFrames: 1,
		EncodedBits:        keyBits,
		FrameBandwidthBits: 40000,
		MaximumBufferBits:  720000,
		BufferLevelBits:    layer1Buffer,
	}
	if got := e.temporal.accounting[0]; got != wantLayer0 {
		t.Fatalf("layer0 accounting after drop = %+v, want %+v", got, wantLayer0)
	}
	if got := e.temporal.accounting[1]; got != wantLayer1 {
		t.Fatalf("layer1 accounting after drop = %+v, want %+v", got, wantLayer1)
	}
}

func TestEncodeIntoInvisibleTemporalFrameUsesLibvpxLayerOverheadAccounting(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	result, err := e.EncodeInto(dst, src, 0, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible temporal EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame || result.TemporalLayerID != 0 {
		t.Fatalf("result = key:%t layer:%d, want invisible base keyframe", result.KeyFrame, result.TemporalLayerID)
	}
	bits := encodedSizeBits(result.SizeBytes)
	wantLayer0Buffer := 288000 - bits
	wantLayer1Buffer := temporalTestBufferAfterFrame(480000, 40000, 720000, bits)

	if result.TemporalLayerBufferLevelBits != wantLayer0Buffer || e.temporal.accounting[0].BufferLevelBits != wantLayer0Buffer {
		t.Fatalf("layer0 invisible buffer = result:%d accounting:%d, want %d", result.TemporalLayerBufferLevelBits, e.temporal.accounting[0].BufferLevelBits, wantLayer0Buffer)
	}
	if e.temporal.accounting[1].BufferLevelBits != wantLayer1Buffer {
		t.Fatalf("layer1 invisible propagated buffer = %d, want %d", e.temporal.accounting[1].BufferLevelBits, wantLayer1Buffer)
	}
}

func temporalTestBufferAfterFrame(level int, frameBandwidth int, maximum int, encodedBits int) int {
	level = saturatingAdd(level, frameBandwidth)
	level = saturatingSub(level, encodedBits)
	if level > maximum {
		return maximum
	}
	return level
}

func TestEncodeIntoTemporalOneLayerKeepsDefaultInterRefresh(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringOneLayer})
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
	if inter.TemporalLayerID != 0 || inter.TemporalLayerCount != 1 {
		t.Fatalf("temporal = id:%d count:%d, want 0/1", inter.TemporalLayerID, inter.TemporalLayerCount)
	}
	state := packetState(t, inter.Data)
	if !state.Refresh.RefreshLast || state.Refresh.RefreshGolden || state.Refresh.RefreshAltRef {
		t.Fatalf("one-layer refresh = %+v, want last-only default refresh", state.Refresh)
	}
}

func TestEncodeIntoTemporalPacketRefreshFlagsMatchLibvpxPatterns(t *testing.T) {
	tests := []struct {
		name   string
		cfg    TemporalScalabilityConfig
		frames int
	}{
		{name: "one-layer", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringOneLayer}, frames: 4},
		{name: "two-layers", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}, frames: 4},
		{name: "two-layers-three-frame", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayersThreeFrame}, frames: 5},
		{name: "three-layers-six-frame", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersSixFrame}, frames: 7},
		{name: "three-layers-no-inter-layer-prediction", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersNoInterLayerPrediction}, frames: 5},
		{name: "three-layers-layer-one-prediction", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersLayerOnePrediction}, frames: 5},
		{name: "three-layers", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayers}, frames: 5},
		{name: "five-layers", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringFiveLayers, LayerTargetBitrateKbps: [MaxTemporalLayers]int{200, 400, 700, 950, 1200}}, frames: 8},
		{name: "two-layers-with-sync", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayersWithSync}, frames: 9},
		{name: "three-layers-with-sync", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersWithSync}, frames: 9},
		{name: "three-layers-altref-with-sync", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersAltRefWithSync}, frames: 9},
		{name: "three-layers-one-reference", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersOneReference}, frames: 5},
		{name: "three-layers-no-sync", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersNoSync}, frames: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTemporalRefreshFlagTestEncoder(t, tt.cfg)
			pattern, ok := temporalLayeringPattern(tt.cfg.Mode)
			if !ok {
				t.Fatalf("temporalLayeringPattern returned false")
			}
			dst := make([]byte, 8192)
			for frame := 0; frame < tt.frames; frame++ {
				result, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, frame), uint64(frame), 1, 0)
				if err != nil {
					t.Fatalf("EncodeInto %d returned error: %v", frame, err)
				}
				if result.KeyFrame {
					continue
				}
				flags := pattern.Flags[frame%pattern.FlagPeriodicity]
				if tt.cfg.Mode != TemporalLayeringFiveLayers && frame > 0 && frame%pattern.FlagPeriodicity == 0 {
					flags &^= EncodeForceKeyFrame
				}
				state := packetState(t, result.Data)
				wantLast := flags&EncodeNoUpdateLast == 0
				wantGolden := pattern.Layers > 1 && flags&EncodeNoUpdateGolden == 0
				wantAltRef := pattern.Layers > 1 && flags&EncodeNoUpdateAltRef == 0
				wantEntropy := flags&EncodeNoUpdateEntropy == 0
				if state.Refresh.RefreshLast != wantLast || state.Refresh.RefreshGolden != wantGolden || state.Refresh.RefreshAltRef != wantAltRef || state.Refresh.RefreshEntropyProbs != wantEntropy {
					t.Fatalf("frame %d refresh = %+v, want last:%t golden:%t alt:%t entropy:%t", frame, state.Refresh, wantLast, wantGolden, wantAltRef, wantEntropy)
				}
			}
		})
	}
}

func TestEncodeIntoReportsLibvpxTemporalDroppableFrames(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersWithSync})
	src := testImage(16, 16)
	fillImage(src, 96, 120, 150)
	dst := make([]byte, 4096)

	results := make([]EncodeResult, 8)
	for i := range results {
		result, err := e.EncodeInto(dst, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
		results[i] = result
		results[i].Data = append([]byte(nil), result.Data...)
	}

	for i, result := range results[:7] {
		if result.Droppable {
			t.Fatalf("result[%d].Droppable = true, want false for reference/entropy-updating temporal frame", i)
		}
	}
	if !results[7].Droppable {
		t.Fatalf("result[7].Droppable = false, want libvpx droppable temporal frame")
	}
	state := packetState(t, results[7].Data)
	if state.Refresh.RefreshLast || state.Refresh.RefreshGolden || state.Refresh.RefreshAltRef || state.Refresh.RefreshEntropyProbs {
		t.Fatalf("droppable refresh = %+v, want no reference or entropy refresh", state.Refresh)
	}
}

func TestEncodeIntoRefreshesEntropyUnlessDisabled(t *testing.T) {
	e := newEntropyRefreshTestEncoder(t, false)
	first := testImage(16, 16)
	second := rateControlTestFrame(16, 16, 1)
	fillImage(first, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !packetState(t, key.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("key refresh entropy = false, want libvpx default true")
	}
	keyData := append([]byte(nil), key.Data...)
	inter, err := e.EncodeInto(dst, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	if !packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("inter refresh entropy = false, want libvpx default true")
	}
	interData := append([]byte(nil), inter.Data...)
	decoded := decodeFrameSequence(t, keyData, interData)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}

	e = newEntropyRefreshTestEncoder(t, false)
	key, err = e.EncodeInto(dst, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("second key EncodeInto returned error: %v", err)
	}
	keyData = append([]byte(nil), key.Data...)
	inter, err = e.EncodeInto(dst, second, 1, 1, EncodeNoUpdateEntropy)
	if err != nil {
		t.Fatalf("no-update-entropy inter EncodeInto returned error: %v", err)
	}
	if packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("no-update-entropy inter refresh entropy = true, want false")
	}
	interData = append([]byte(nil), inter.Data...)
	decoded = decodeFrameSequence(t, keyData, interData)
	if len(decoded) != 2 {
		t.Fatalf("no-update-entropy decoded frame count = %d, want 2", len(decoded))
	}
}

func TestEncodeIntoErrorResilientSuppressesEntropyRefresh(t *testing.T) {
	e := newEntropyRefreshTestEncoder(t, true)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if packetState(t, key.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient key refresh entropy = true, want false")
	}
	inter, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 2), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient inter refresh entropy = true, want false")
	}
}

func TestEncodeIntoTemporalBaseLayerIsDecodableWithoutEnhancementFrames(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	dst := make([]byte, 8192)
	basePackets := make([][]byte, 0, 3)

	for i := 0; i < 6; i++ {
		src := rateControlTestFrame(16, 16, i)
		result, err := e.EncodeInto(dst, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
		if result.TemporalLayerID == 0 {
			basePackets = append(basePackets, append([]byte(nil), result.Data...))
		}
	}
	if len(basePackets) != 3 {
		t.Fatalf("base packet count = %d, want 3", len(basePackets))
	}
	decoded := decodeFrameSequence(t, basePackets...)
	if len(decoded) != len(basePackets) {
		t.Fatalf("decoded base frame count = %d, want %d", len(decoded), len(basePackets))
	}
}

func TestSetTemporalScalabilityControlsNextFrames(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	plain, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("plain EncodeInto returned error: %v", err)
	}
	if plain.TemporalLayerID != 0 || plain.TemporalLayerCount != 1 {
		t.Fatalf("plain temporal = id:%d count:%d, want 0/1", plain.TemporalLayerID, plain.TemporalLayerCount)
	}
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}); err != nil {
		t.Fatalf("SetTemporalScalability returned error: %v", err)
	}

	key, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("temporal key EncodeInto returned error: %v", err)
	}
	enhancement, err := e.EncodeInto(dst, src, 2, 1, 0)
	if err != nil {
		t.Fatalf("temporal enhancement EncodeInto returned error: %v", err)
	}
	if !key.KeyFrame || key.TemporalLayerID != 0 || key.TL0PICIDX != 0 {
		t.Fatalf("first temporal result = key:%t id:%d tl0:%d, want key/0/0", key.KeyFrame, key.TemporalLayerID, key.TL0PICIDX)
	}
	if enhancement.KeyFrame || enhancement.TemporalLayerID != 1 || enhancement.TL0PICIDX != 0 || !enhancement.TemporalLayerSync {
		t.Fatalf("second temporal result = key:%t id:%d tl0:%d sync:%t, want inter/1/0/sync", enhancement.KeyFrame, enhancement.TemporalLayerID, enhancement.TL0PICIDX, enhancement.TemporalLayerSync)
	}
}

func TestSetTemporalLayerIDOverridesNextFrames(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !key.KeyFrame || key.TemporalLayerID != 0 || key.TL0PICIDX != 0 {
		t.Fatalf("key temporal = key:%t id:%d tl0:%d, want key/0/0", key.KeyFrame, key.TemporalLayerID, key.TL0PICIDX)
	}
	if err := e.SetTemporalLayerID(0); err != nil {
		t.Fatalf("SetTemporalLayerID returned error: %v", err)
	}
	base, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("manual base EncodeInto returned error: %v", err)
	}
	if base.TemporalLayerID != 0 || base.TL0PICIDX != 1 || base.TemporalLayerTargetBitrateKbps != 720 || base.TemporalLayerCumulativeBitrateKbps != 720 {
		t.Fatalf("manual base temporal = id:%d tl0:%d target:%d cumulative:%d, want 0/1/720/720", base.TemporalLayerID, base.TL0PICIDX, base.TemporalLayerTargetBitrateKbps, base.TemporalLayerCumulativeBitrateKbps)
	}
	if err := e.SetTemporalLayerID(1); err != nil {
		t.Fatalf("SetTemporalLayerID enhancement returned error: %v", err)
	}
	enhancement, err := e.EncodeInto(dst, src, 2, 1, 0)
	if err != nil {
		t.Fatalf("manual enhancement EncodeInto returned error: %v", err)
	}
	if enhancement.TemporalLayerID != 1 || enhancement.TL0PICIDX != 1 || enhancement.TemporalLayerTargetBitrateKbps != 480 || enhancement.TemporalLayerCumulativeBitrateKbps != 1200 {
		t.Fatalf("manual enhancement temporal = id:%d tl0:%d target:%d cumulative:%d, want 1/1/480/1200", enhancement.TemporalLayerID, enhancement.TL0PICIDX, enhancement.TemporalLayerTargetBitrateKbps, enhancement.TemporalLayerCumulativeBitrateKbps)
	}
}

func TestSetTemporalLayerIDValidation(t *testing.T) {
	e := newTestEncoder(t)
	if err := e.SetTemporalLayerID(0); err != nil {
		t.Fatalf("SetTemporalLayerID one-layer returned error: %v", err)
	}
	if err := e.SetTemporalLayerID(1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTemporalLayerID one-layer high error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}); err != nil {
		t.Fatalf("SetTemporalScalability returned error: %v", err)
	}
	if err := e.SetTemporalLayerID(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTemporalLayerID negative error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetTemporalLayerID(2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTemporalLayerID two-layer high error = %v, want ErrInvalidConfig", err)
	}
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
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
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
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
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

func TestEncodeIntoAdaptiveKeyFramesDetectsSceneCut(t *testing.T) {
	e := newAdaptiveSceneCutTestEncoder(t, true)
	first := testImage(32, 32)
	second := testImage(32, 32)
	fillImage(first, 20, 90, 170)
	fillImage(second, 230, 90, 170)
	dst := make([]byte, 8192)
	if _, err := e.EncodeInto(dst, first, 0, 1, 0); err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}

	result, err := e.EncodeInto(dst, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame || !result.SceneCut {
		t.Fatalf("adaptive result = key:%t sceneCut:%t, want scene-cut keyframe", result.KeyFrame, result.SceneCut)
	}
	info, err := PeekVP8StreamInfo(result.Data)
	if err != nil {
		t.Fatalf("PeekVP8StreamInfo returned error: %v", err)
	}
	if !info.KeyFrame {
		t.Fatalf("packet KeyFrame = false, want keyframe packet")
	}
}

func TestEncodeIntoAdaptiveKeyFramesDisabledByDefault(t *testing.T) {
	e := newAdaptiveSceneCutTestEncoder(t, false)
	first := testImage(32, 32)
	second := testImage(32, 32)
	fillImage(first, 20, 90, 170)
	fillImage(second, 230, 90, 170)
	dst := make([]byte, 8192)
	if _, err := e.EncodeInto(dst, first, 0, 1, 0); err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}

	result, err := e.EncodeInto(dst, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if result.KeyFrame || result.SceneCut {
		t.Fatalf("default result = key:%t sceneCut:%t, want legacy interframe", result.KeyFrame, result.SceneCut)
	}
}

func TestEncodeIntoLookaheadBuffersAndFlushes(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		KeyFrameInterval:    120,
		LookaheadFrames:     2,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	first := testImage(16, 16)
	second := testImage(16, 16)
	third := testImage(16, 16)
	fillImage(first, 30, 90, 170)
	fillImage(second, 50, 90, 170)
	fillImage(third, 70, 90, 170)

	if _, err := e.EncodeInto(dst, first, 10, 1, 0); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("first EncodeInto error = %v, want ErrFrameNotReady", err)
	}
	result, err := e.EncodeInto(dst, second, 11, 1, 0)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame || result.PTS != 10 || result.LookaheadDepth != 1 {
		t.Fatalf("second result = key:%t pts:%d depth:%d, want first queued keyframe with depth 1", result.KeyFrame, result.PTS, result.LookaheadDepth)
	}
	result, err = e.EncodeInto(dst, third, 12, 1, 0)
	if err != nil {
		t.Fatalf("third EncodeInto returned error: %v", err)
	}
	if result.PTS != 11 || result.LookaheadDepth != 1 {
		t.Fatalf("third result pts/depth = %d/%d, want second queued frame/depth 1", result.PTS, result.LookaheadDepth)
	}
	result, err = e.FlushInto(dst)
	if err != nil {
		t.Fatalf("FlushInto returned error: %v", err)
	}
	if result.PTS != 12 || result.LookaheadDepth != 0 {
		t.Fatalf("flush result pts/depth = %d/%d, want final queued frame/depth 0", result.PTS, result.LookaheadDepth)
	}
	if _, err := e.FlushInto(dst); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("empty FlushInto error = %v, want ErrFrameNotReady", err)
	}
}

func TestEncodeIntoARNRAndSpatialDenoiserReportPreprocessing(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
		LookaheadFrames:   2,
		ARNRMaxFrames:     3,
		ARNRStrength:      6,
		ARNRType:          2,
		NoiseSensitivity:  2,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	noisy := testImage(16, 16)
	clean := testImage(16, 16)
	for i := range noisy.Y {
		if i%2 == 0 {
			noisy.Y[i] = 40
		} else {
			noisy.Y[i] = 60
		}
	}
	fillImage(clean, 50, 90, 170)
	if _, err := e.EncodeInto(dst, noisy, 0, 1, 0); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("first EncodeInto error = %v, want ErrFrameNotReady", err)
	}
	result, err := e.EncodeInto(dst, clean, 1, 1, 0)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if !result.ARNRFiltered || !result.Denoised {
		t.Fatalf("preprocess flags = arnr:%t denoised:%t, want both", result.ARNRFiltered, result.Denoised)
	}
}

func TestCollectFirstPassStatsAndTwoPassSceneCut(t *testing.T) {
	const (
		width  = 128
		height = 128
	)
	firstPass, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
	})
	if err != nil {
		t.Fatalf("first-pass NewVP8Encoder returned error: %v", err)
	}
	frames := make([]Image, 12)
	stats := make([]FirstPassFrameStats, len(frames))
	for i := range frames {
		frames[i] = testImage(width, height)
		if i < 5 {
			fillImage(frames[i], 20, 90, 170)
		} else {
			fillImage(frames[i], 230, 90, 170)
		}
		stats[i], err = firstPass.CollectFirstPassStats(frames[i], uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats %d returned error: %v", i, err)
		}
	}
	if !libvpxTestCandidateKeyFrame(stats, 5) {
		t.Fatalf("first-pass stats did not satisfy libvpx candidate keyframe test at scene cut")
	}

	e, err := NewVP8Encoder(EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
		TwoPassStats:      stats,
		TwoPassMinPct:     50,
		TwoPassMaxPct:     200,
	})
	if err != nil {
		t.Fatalf("second-pass NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 256*1024)
	var result EncodeResult
	for i, frame := range frames[:6] {
		result, err = e.EncodeInto(dst, frame, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
	}
	if !result.KeyFrame || !result.SceneCut || result.PTS != 5 || result.TwoPassFrameTargetBits == 0 {
		t.Fatalf("scene-cut result = key:%t scene:%t pts:%d target:%d, want two-pass scene-cut keyframe", result.KeyFrame, result.SceneCut, result.PTS, result.TwoPassFrameTargetBits)
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
	temporal := TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}

	tests := []struct {
		name string
		fn   func()
	}{
		{name: "EncodeInto", fn: func() { _, _ = e.EncodeInto(dst, src, 0, 1, 0) }},
		{name: "SetBitrateKbps", fn: func() { _ = e.SetBitrateKbps(1200) }},
		{name: "SetRateControl", fn: func() { _ = e.SetRateControl(cfg) }},
		{name: "SetCQLevel", fn: func() { _ = e.SetCQLevel(10) }},
		{name: "SetMaxIntraBitratePct", fn: func() { _ = e.SetMaxIntraBitratePct(200) }},
		{name: "SetGFCBRBoostPct", fn: func() { _ = e.SetGFCBRBoostPct(100) }},
		{name: "SetTokenPartitions", fn: func() { _ = e.SetTokenPartitions(int(vp8common.EightPartition)) }},
		{name: "SetSharpness", fn: func() { _ = e.SetSharpness(3) }},
		{name: "SetStaticThreshold", fn: func() { _ = e.SetStaticThreshold(1) }},
		{name: "SetScreenContentMode", fn: func() { _ = e.SetScreenContentMode(1) }},
		{name: "SetRealtimeTarget", fn: func() { _ = e.SetRealtimeTarget(RealtimeTarget{FPS: 30}) }},
		{name: "SetTemporalScalability", fn: func() { _ = e.SetTemporalScalability(temporal) }},
		{name: "SetTemporalLayerID", fn: func() { _ = e.SetTemporalLayerID(1) }},
		{name: "SetDeadline", fn: func() { _ = e.SetDeadline(DeadlineRealtime) }},
		{name: "SetCPUUsed", fn: func() { _ = e.SetCPUUsed(8) }},
		{name: "SetKeyFrameInterval", fn: func() { _ = e.SetKeyFrameInterval(120) }},
		{name: "SetAdaptiveKeyFrames", fn: func() { _ = e.SetAdaptiveKeyFrames(true) }},
		{name: "SetNoiseSensitivity", fn: func() { _ = e.SetNoiseSensitivity(2) }},
		{name: "SetARNR", fn: func() { _ = e.SetARNR(3, 4, 3) }},
		{name: "SetTwoPassStats", fn: func() { _ = e.SetTwoPassStats(nil) }},
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

func TestEncodeIntoTemporalSuccessAllocatesZero(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	dst := make([]byte, 4096)
	src := testImage(16, 16)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = e.EncodeInto(dst, src, 0, 1, 0)
	})
	if allocs != 0 {
		t.Fatalf("temporal EncodeInto success allocs = %v, want 0", allocs)
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

func newTemporalTestEncoder(tb testing.TB, temporal TemporalScalabilityConfig) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
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
		TemporalScalability: temporal,
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func newTemporalRefreshFlagTestEncoder(tb testing.TB, temporal TemporalScalabilityConfig) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    true,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		TemporalScalability: temporal,
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func newAdaptiveSceneCutTestEncoder(tb testing.TB, adaptive bool) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		KeyFrameInterval:  120,
		AdaptiveKeyFrames: adaptive,
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func newEntropyRefreshTestEncoder(tb testing.TB, errorResilient bool) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		ErrorResilient:      errorResilient,
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

func fillMacroblock(img Image, mbRow int, mbCol int, y byte, u byte, v byte) {
	y0 := mbRow * 16
	x0 := mbCol * 16
	for row := y0; row < y0+16 && row < img.Height; row++ {
		for col := x0; col < x0+16 && col < img.Width; col++ {
			img.Y[row*img.YStride+col] = y
		}
	}
	uvHeight := (img.Height + 1) >> 1
	uvWidth := (img.Width + 1) >> 1
	uvY0 := mbRow * 8
	uvX0 := mbCol * 8
	for row := uvY0; row < uvY0+8 && row < uvHeight; row++ {
		for col := uvX0; col < uvX0+8 && col < uvWidth; col++ {
			img.U[row*img.UStride+col] = u
			img.V[row*img.VStride+col] = v
		}
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

func copyShifted8x8FromImage(dst Image, src Image, y int, x int, dy int, dx int) {
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			dst.Y[(y+row)*dst.YStride+x+col] = src.Y[(y+row+dy)*src.YStride+x+col+dx]
		}
	}
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
