package govpx

import (
	"bytes"
	"errors"
	"fmt"
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

func TestCPUUsedNormalizationMirrorsLibvpxDeadlineClamp(t *testing.T) {
	base := EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	}
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     int
	}{
		{name: "good clamps high", deadline: DeadlineGoodQuality, cpuUsed: 16, want: 5},
		{name: "good clamps low", deadline: DeadlineGoodQuality, cpuUsed: -16, want: -5},
		{name: "realtime keeps high", deadline: DeadlineRealtime, cpuUsed: 16, want: 16},
		{name: "best keeps high", deadline: DeadlineBestQuality, cpuUsed: 16, want: 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := base
			opts.Deadline = tt.deadline
			opts.CpuUsed = tt.cpuUsed
			e, err := NewVP8Encoder(opts)
			if err != nil {
				t.Fatalf("NewVP8Encoder returned error: %v", err)
			}
			if got := e.opts.CpuUsed; got != tt.want {
				t.Fatalf("CpuUsed = %d, want %d", got, tt.want)
			}
		})
	}

	e, err := NewVP8Encoder(base)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetDeadline(DeadlineGoodQuality); err != nil {
		t.Fatalf("SetDeadline(good) returned error: %v", err)
	}
	if err := e.SetCPUUsed(16); err != nil {
		t.Fatalf("SetCPUUsed(16) returned error: %v", err)
	}
	if got := e.opts.CpuUsed; got != 5 {
		t.Fatalf("good SetCPUUsed stored %d, want clamped 5", got)
	}
	if err := e.SetDeadline(DeadlineRealtime); err != nil {
		t.Fatalf("SetDeadline(realtime) returned error: %v", err)
	}
	if err := e.SetCPUUsed(16); err != nil {
		t.Fatalf("realtime SetCPUUsed(16) returned error: %v", err)
	}
	if got := e.opts.CpuUsed; got != 16 {
		t.Fatalf("realtime SetCPUUsed stored %d, want 16", got)
	}
	if err := e.SetDeadline(DeadlineGoodQuality); err != nil {
		t.Fatalf("SetDeadline(good) returned error: %v", err)
	}
	if got := e.opts.CpuUsed; got != 5 {
		t.Fatalf("SetDeadline(good) stored %d, want clamped 5", got)
	}
}

func TestLibvpxSpeedFeatureCPUUsedMirrorsRealtimeAutoSelect(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     int
	}{
		{name: "realtime zero auto-selects initial speed four", deadline: DeadlineRealtime, cpuUsed: 0, want: 4},
		{name: "realtime positive auto-selects initial speed four", deadline: DeadlineRealtime, cpuUsed: 16, want: 4},
		{name: "realtime negative is explicit speed", deadline: DeadlineRealtime, cpuUsed: -9, want: 9},
		{name: "good high clamps to five", deadline: DeadlineGoodQuality, cpuUsed: 16, want: 5},
		{name: "good low clamps to negative five", deadline: DeadlineGoodQuality, cpuUsed: -16, want: -5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := libvpxSpeedFeatureCPUUsed(tt.deadline, tt.cpuUsed); got != tt.want {
				t.Fatalf("libvpxSpeedFeatureCPUUsed(%v, %d) = %d, want %d", tt.deadline, tt.cpuUsed, got, tt.want)
			}
		})
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
	for i := range 20 {
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

func TestEncodeIntoOnePassVBRDoesNotRefreshGoldenImmediatelyAfterKey(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           3,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 64*64*3)
	if _, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, 0), 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	inter, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	state := packetState(t, inter.Data)
	if state.Refresh.RefreshGolden {
		t.Fatalf("frame-1 VBR refresh_golden = true, want false until libvpx DEFAULT_GF_INTERVAL countdown expires")
	}
	if e.rc.framesTillGFUpdateDue != libvpxDefaultGFInterval-2 {
		t.Fatalf("framesTillGFUpdateDue = %d, want %d after key decrement plus one inter frame",
			e.rc.framesTillGFUpdateDue, libvpxDefaultGFInterval-2)
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

func TestEncodeIntoWritesLibvpxFrameQuantDeltas(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        0,
		MaxQuantizer:        1,
		CQLevel:             0,
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
	keyQuant := packetState(t, key.Data).Quant
	if keyQuant.BaseQIndex != 0 || keyQuant.Y2DCDelta != 4 {
		t.Fatalf("key quant = %+v, want base Q 0 with Y2 DC delta 4", keyQuant)
	}

	screen, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             56,
		ScreenContentMode:   1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("screen NewVP8Encoder returned error: %v", err)
	}
	if _, err := screen.EncodeInto(dst, rateControlTestFrame(16, 16, 0), 0, 1, 0); err != nil {
		t.Fatalf("screen key EncodeInto returned error: %v", err)
	}
	inter, err := screen.EncodeInto(dst, rateControlTestFrame(16, 16, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	interQuant := packetState(t, inter.Data).Quant
	wantUVDelta := int8(-15)
	if interQuant.BaseQIndex != uint8(libvpxPublicQuantizerToQIndex(56)) || interQuant.UVDCDelta != wantUVDelta || interQuant.UVACDelta != wantUVDelta {
		t.Fatalf("inter quant = %+v, want screen-content UV deltas %d at qindex %d", interQuant, wantUVDelta, libvpxPublicQuantizerToQIndex(56))
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

func TestEncodeKeyFrameAttemptDefersEntropyCommit(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              32,
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
	e.coefProbs[0][0][0][0] = 77
	e.modeProbs.MV[0][0] = 99
	wantCoefProbs := e.coefProbs
	wantModeProbs := e.modeProbs

	rows := encoderMacroblockRows(32)
	cols := encoderMacroblockCols(32)
	attempt, err := e.encodeKeyFrameAttempt(make([]byte, 16384), sourceImageFromImage(rateControlTestFrame(32, 32, 0)), rows, cols, rows*cols, false, false)
	if err != nil {
		t.Fatalf("encodeKeyFrameAttempt returned error: %v", err)
	}
	if !attempt.RefreshEntropyProbs {
		t.Fatalf("key attempt RefreshEntropyProbs = false, want true")
	}
	if e.coefProbs != wantCoefProbs {
		t.Fatalf("encodeKeyFrameAttempt mutated coefficient probabilities before commit")
	}
	if e.modeProbs != wantModeProbs {
		t.Fatalf("encodeKeyFrameAttempt mutated mode probabilities before commit")
	}

	e.commitKeyFrameEntropy(attempt)
	if e.coefProbs != attempt.FrameCoefProbs {
		t.Fatalf("committed coefficient probabilities do not match accepted key attempt")
	}
	if e.modeProbs == wantModeProbs {
		t.Fatalf("committed keyframe mode probabilities still match pre-commit sentinel")
	}
}

func TestEncodeInterFrameAttemptDefersSkipFalseCommit(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	first := testImage(32, 32)
	second := testImage(32, 32)
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 90, 170)
	if _, err := e.EncodeInto(make([]byte, 16384), first, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}

	e.probSkipFalse = 91
	rows := encoderMacroblockRows(32)
	cols := encoderMacroblockCols(32)
	attempt, err := e.encodeInterFrameAttempt(make([]byte, 16384), sourceImageFromImage(second), rows, cols, rows*cols, 0, false, false, true, false, true)
	if err != nil {
		t.Fatalf("encodeInterFrameAttempt returned error: %v", err)
	}
	if e.probSkipFalse != 91 {
		t.Fatalf("inter attempt probSkipFalse = %d, want pre-attempt sentinel 91 before commit", e.probSkipFalse)
	}

	e.commitInterFrameAttempt(attempt)
	if e.probSkipFalse != attempt.Config.ProbSkipFalse {
		t.Fatalf("committed probSkipFalse = %d, want accepted attempt probability %d", e.probSkipFalse, attempt.Config.ProbSkipFalse)
	}
}

func TestCommitInterFrameEntropyRefreshesInterIntraModeProbs(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	vp8dec.ResetModeProbs(&e.modeProbs)
	original := e.modeProbs
	frameYModeProbs := vp8tables.DefaultYModeProbs
	frameYModeProbs[0] = 251
	frameUVModeProbs := vp8tables.DefaultUVModeProbs
	frameUVModeProbs[0] = 249
	frameMVProbs := vp8tables.DefaultMVContext
	frameMVProbs[0][0] = 99
	attempt := interFrameEncodeAttempt{
		Config:           vp8enc.InterFrameStateConfig{RefreshEntropyProbs: true},
		FrameCoefProbs:   e.coefProbs,
		FrameYModeProbs:  frameYModeProbs,
		FrameUVModeProbs: frameUVModeProbs,
		FrameMVProbs:     frameMVProbs,
	}

	e.commitInterFrameEntropy(attempt)

	if e.modeProbs.YMode != frameYModeProbs {
		t.Fatalf("committed Y mode probs = %v, want %v", e.modeProbs.YMode, frameYModeProbs)
	}
	if e.modeProbs.UVMode != frameUVModeProbs {
		t.Fatalf("committed UV mode probs = %v, want %v", e.modeProbs.UVMode, frameUVModeProbs)
	}
	if e.modeProbs.MV != frameMVProbs {
		t.Fatalf("committed MV probs = %v, want %v", e.modeProbs.MV, frameMVProbs)
	}

	e.modeProbs = original
	attempt.Config.RefreshEntropyProbs = false
	e.commitInterFrameEntropy(attempt)
	if e.modeProbs != original {
		t.Fatalf("mode probs changed on no-refresh commit: got %+v want %+v", e.modeProbs, original)
	}
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
		for col := range 16 {
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

	for frame := range 3 {
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

// TestEncodeIntoCyclicRefreshIndexPreservedAcrossKeyFrames pins libvpx
// vp8/encoder/onyx_if.c cyclic_background_refresh: the cyclic_refresh_mode_index
// is reset to 0 only on init (line 1213) and resize, not on each key frame
// (the iteration loop at line 534 is gated on frame_type != KEY_FRAME so a
// key frame leaves the index untouched). govpx must mirror that — resetting
// the index on each forced keyframe shifts the rolling refresh window
// relative to libvpx for every GOP after the first.
func TestEncodeIntoCyclicRefreshIndexPreservedAcrossKeyFrames(t *testing.T) {
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
	src := testImage(80, 64)
	fillImage(src, 128, 128, 128)
	if _, err := e.EncodeInto(packet, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	// Drive a few inter frames so the cyclic refresh index advances away
	// from zero. A 5x4 frame has 20 MBs and refreshCount = 20/20 = 1, so
	// each inter frame advances the index by exactly 1.
	for frame := range 3 {
		s := publicImageFromVP8(&e.lastRef.Img)
		if _, err := e.EncodeInto(packet, s, uint64(frame+1), 1, 0); err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
	}
	beforeKey := e.cyclicRefreshIndex
	if beforeKey == 0 {
		t.Fatalf("cyclicRefreshIndex stayed at 0 after 3 inter frames; expected forward progress")
	}
	// Force a second keyframe and confirm the index survives.
	e.ForceKeyFrame()
	src2 := testImage(80, 64)
	fillImage(src2, 128, 128, 128)
	if _, err := e.EncodeInto(packet, src2, 4, 1, 0); err != nil {
		t.Fatalf("forced key EncodeInto returned error: %v", err)
	}
	if e.cyclicRefreshIndex != beforeKey {
		t.Fatalf("cyclicRefreshIndex after key frame = %d, want libvpx-preserved %d", e.cyclicRefreshIndex, beforeKey)
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

// TestRecodeRestoresFullCodingContext mirrors libvpx
// vp8/encoder/onyx_if.c's vp8_save_coding_context / vp8_restore_coding_context
// contract: every field listed in CODING_CONTEXT must be restored to its
// pre-loop snapshot when the recode loop rejects an attempt.
func TestRecodeRestoresFullCodingContext(t *testing.T) {
	e := newTestEncoder(t)

	// Seed the encoder with non-default values across every libvpx-listed
	// CODING_CONTEXT field plus the ref/skip-prob siblings govpx restores.
	e.rc.framesSinceKeyframe = 4
	e.loopFilterLevel = 11
	e.rc.framesTillGFUpdateDue = 7
	e.framesSinceGolden = 3
	e.rc.thisFramePercentIntra = 42
	e.modeProbs.YMode[0] = 17
	e.modeProbs.UVMode[0] = 19
	e.modeProbs.BMode[0] = 23
	e.modeProbs.MV[0][0] = 31
	e.modeProbs.MV[1][0] = 37
	e.coefProbs[0][0][0][0] = 41
	e.refProbIntra = 71
	e.refProbLast = 73
	e.refProbGolden = 79
	e.probSkipFalse = 83
	e.lastSkipFalseProbs = [3]uint8{89, 97, 101}

	baseline := struct {
		framesSinceKey        int
		filterLevel           uint8
		framesTillGFUpdateDue int
		framesSinceGolden     int
		thisFramePercentIntra int
		yMode0                uint8
		uvMode0               uint8
		bMode0                uint8
		mv00                  uint8
		mv10                  uint8
		coef0000              uint8
		refIntra              uint8
		refLast               uint8
		refGolden             uint8
		probSkipFalse         uint8
		lastSkipFalseProbs    [3]uint8
	}{
		framesSinceKey:        e.rc.framesSinceKeyframe,
		filterLevel:           e.loopFilterLevel,
		framesTillGFUpdateDue: e.rc.framesTillGFUpdateDue,
		framesSinceGolden:     e.framesSinceGolden,
		thisFramePercentIntra: e.rc.thisFramePercentIntra,
		yMode0:                e.modeProbs.YMode[0],
		uvMode0:               e.modeProbs.UVMode[0],
		bMode0:                e.modeProbs.BMode[0],
		mv00:                  e.modeProbs.MV[0][0],
		mv10:                  e.modeProbs.MV[1][0],
		coef0000:              e.coefProbs[0][0][0][0],
		refIntra:              e.refProbIntra,
		refLast:               e.refProbLast,
		refGolden:             e.refProbGolden,
		probSkipFalse:         e.probSkipFalse,
		lastSkipFalseProbs:    e.lastSkipFalseProbs,
	}

	e.saveCodingContext()

	// Aggressively mutate every snapshotted field, simulating an attempt
	// that mutated the coding context before being rejected.
	e.rc.framesSinceKeyframe = 99
	e.loopFilterLevel = 200
	e.rc.framesTillGFUpdateDue = 0
	e.framesSinceGolden = 100
	e.rc.thisFramePercentIntra = 0
	e.modeProbs.YMode[0] = 0
	e.modeProbs.UVMode[0] = 0
	e.modeProbs.BMode[0] = 0
	e.modeProbs.MV[0][0] = 0
	e.modeProbs.MV[1][0] = 0
	e.coefProbs[0][0][0][0] = 0
	e.refProbIntra = 0
	e.refProbLast = 0
	e.refProbGolden = 0
	e.probSkipFalse = 0
	e.lastSkipFalseProbs = [3]uint8{0, 0, 0}

	e.restoreCodingContext()

	if e.rc.framesSinceKeyframe != baseline.framesSinceKey {
		t.Fatalf("framesSinceKeyframe = %d, want %d", e.rc.framesSinceKeyframe, baseline.framesSinceKey)
	}
	if e.loopFilterLevel != baseline.filterLevel {
		t.Fatalf("loopFilterLevel = %d, want %d", e.loopFilterLevel, baseline.filterLevel)
	}
	if e.rc.framesTillGFUpdateDue != baseline.framesTillGFUpdateDue {
		t.Fatalf("framesTillGFUpdateDue = %d, want %d", e.rc.framesTillGFUpdateDue, baseline.framesTillGFUpdateDue)
	}
	if e.framesSinceGolden != baseline.framesSinceGolden {
		t.Fatalf("framesSinceGolden = %d, want %d", e.framesSinceGolden, baseline.framesSinceGolden)
	}
	if e.rc.thisFramePercentIntra != baseline.thisFramePercentIntra {
		t.Fatalf("thisFramePercentIntra = %d, want %d", e.rc.thisFramePercentIntra, baseline.thisFramePercentIntra)
	}
	if e.modeProbs.YMode[0] != baseline.yMode0 {
		t.Fatalf("modeProbs.YMode[0] = %d, want %d", e.modeProbs.YMode[0], baseline.yMode0)
	}
	if e.modeProbs.UVMode[0] != baseline.uvMode0 {
		t.Fatalf("modeProbs.UVMode[0] = %d, want %d", e.modeProbs.UVMode[0], baseline.uvMode0)
	}
	if e.modeProbs.BMode[0] != baseline.bMode0 {
		t.Fatalf("modeProbs.BMode[0] = %d, want %d", e.modeProbs.BMode[0], baseline.bMode0)
	}
	if e.modeProbs.MV[0][0] != baseline.mv00 {
		t.Fatalf("modeProbs.MV[0][0] = %d, want %d", e.modeProbs.MV[0][0], baseline.mv00)
	}
	if e.modeProbs.MV[1][0] != baseline.mv10 {
		t.Fatalf("modeProbs.MV[1][0] = %d, want %d", e.modeProbs.MV[1][0], baseline.mv10)
	}
	if e.coefProbs[0][0][0][0] != baseline.coef0000 {
		t.Fatalf("coefProbs[0][0][0][0] = %d, want %d", e.coefProbs[0][0][0][0], baseline.coef0000)
	}
	if e.refProbIntra != baseline.refIntra || e.refProbLast != baseline.refLast || e.refProbGolden != baseline.refGolden {
		t.Fatalf("ref probs = intra:%d last:%d golden:%d, want %d/%d/%d",
			e.refProbIntra, e.refProbLast, e.refProbGolden,
			baseline.refIntra, baseline.refLast, baseline.refGolden)
	}
	if e.probSkipFalse != baseline.probSkipFalse {
		t.Fatalf("probSkipFalse = %d, want %d", e.probSkipFalse, baseline.probSkipFalse)
	}
	if e.lastSkipFalseProbs != baseline.lastSkipFalseProbs {
		t.Fatalf("lastSkipFalseProbs = %v, want %v", e.lastSkipFalseProbs, baseline.lastSkipFalseProbs)
	}
}

// TestRecodeForcedKeyFrameRetriesAtAdjustedQ exercises libvpx
// vp8/encoder/onyx_if.c's "Special case handling for forced key frames" branch
// in encode_frame_to_data_rate around line 4065. When the SS error of the
// forced-KF reconstruction is much larger than ambient_err, q_high is lowered
// and Q is set to (q_high + q_low) >> 1; the inverse holds when the KF SS
// error is much smaller. The unit test drives forcedKeyFrameRecodeQuantizer
// directly so the libvpx Q-adjustment formula is locked in.
func TestRecodeForcedKeyFrameRetriesAtAdjustedQ(t *testing.T) {
	e := newTestEncoder(t)
	rc := &e.rc

	// Case 1: kf_err > ambient_err * 7/8 -> lower q_high to (Q-1), Q := mid.
	rc.currentQuantizer = 60
	recode := frameSizeRecodeState{qLow: 10, qHigh: 80}
	q, recoded := rc.forcedKeyFrameRecodeQuantizer(8800, 1000, &recode)
	if !recoded {
		t.Fatalf("kf_err > ambient*7/8 should trigger recode (returned recoded=false)")
	}
	if recode.qHigh != 59 {
		t.Fatalf("qHigh after lossy KF branch = %d, want 59 (Q-1)", recode.qHigh)
	}
	if want := (recode.qHigh + recode.qLow) >> 1; q != want {
		t.Fatalf("q after lossy KF branch = %d, want (qHigh+qLow)>>1 = %d", q, want)
	}

	// Case 2: kf_err < ambient_err / 2 -> raise q_low to (Q+1), Q := mid+1.
	rc.currentQuantizer = 30
	recode = frameSizeRecodeState{qLow: 10, qHigh: 80}
	q, recoded = rc.forcedKeyFrameRecodeQuantizer(100, 1000, &recode)
	if !recoded {
		t.Fatalf("kf_err < ambient/2 should trigger recode (returned recoded=false)")
	}
	if recode.qLow != 31 {
		t.Fatalf("qLow after much-better KF branch = %d, want 31 (Q+1)", recode.qLow)
	}
	if want := (recode.qHigh + recode.qLow + 1) >> 1; q != want {
		t.Fatalf("q after much-better KF branch = %d, want (qHigh+qLow+1)>>1 = %d", q, want)
	}

	// Case 3: kf_err in the libvpx "neither too lossy nor much better"
	// window [ambient/2, ambient*7/8] -> no recode, Q unchanged.
	rc.currentQuantizer = 40
	recode = frameSizeRecodeState{qLow: 10, qHigh: 80}
	q, recoded = rc.forcedKeyFrameRecodeQuantizer(800, 1000, &recode)
	if recoded {
		t.Fatalf("kf_err in libvpx neutral band should not trigger recode")
	}
	if q != 40 {
		t.Fatalf("q with no recode = %d, want unchanged 40", q)
	}

	// Case 4: ambient_err <= 0 -> branch is disabled, no recode.
	rc.currentQuantizer = 20
	recode = frameSizeRecodeState{qLow: 0, qHigh: 80}
	q, recoded = rc.forcedKeyFrameRecodeQuantizer(123, 0, &recode)
	if recoded {
		t.Fatalf("ambient_err <= 0 should disable forced-KF branch")
	}
	if q != 20 {
		t.Fatalf("q with disabled branch = %d, want unchanged 20", q)
	}

	// Case 5: end-to-end through encodeKeyFrameWithQuantizerFeedback. Seed
	// ambient_err and engage thisKeyFrameForced; confirm that the recode
	// loop walks the q-bound state. We exercise the integration path by
	// running a real key-frame encode loop with a tiny image; the precise Q
	// outcome depends on the encoded SS error, but the loop must terminate
	// and the loop counter must be at least 1.
	enc := newTestEncoder(t)
	enc.thisKeyFrameForced = true
	enc.ambientErr = 1
	enc.frameCount = 1
	dst := make([]byte, 16384)
	src := rateControlTestFrame(16, 16, 11)
	if _, err := enc.EncodeInto(dst, src, 1, 1, EncodeForceKeyFrame); err != nil {
		t.Fatalf("forced-KF EncodeInto returned error: %v", err)
	}
	if enc.oracleTraceRecodeLoopCount < 1 {
		t.Fatalf("oracleTraceRecodeLoopCount = %d, want >=1 after forced-KF encode", enc.oracleTraceRecodeLoopCount)
	}
	if enc.thisKeyFrameForced {
		t.Fatalf("thisKeyFrameForced still set after forced-KF commit, want cleared")
	}
}

func TestResetRestoresRateControlQuantizerAverages(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 4096)
	for i := range 4 {
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

func TestEncoderLoopFilterHeaderUsesRealtimeSimpleFilterAtHighSpeed(t *testing.T) {
	tests := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		want     vp8dec.LoopFilterType
	}{
		{name: "realtime positive cpu-used auto-speed", deadline: DeadlineRealtime, cpuUsed: 14, want: vp8dec.NormalLoopFilter},
		{name: "realtime explicit speed thirteen", deadline: DeadlineRealtime, cpuUsed: -13, want: vp8dec.NormalLoopFilter},
		{name: "realtime explicit speed fourteen", deadline: DeadlineRealtime, cpuUsed: -14, want: vp8dec.SimpleLoopFilter},
		{name: "realtime explicit speed fifteen", deadline: DeadlineRealtime, cpuUsed: -15, want: vp8dec.SimpleLoopFilter},
		{name: "good quality speed fifteen", deadline: DeadlineGoodQuality, cpuUsed: 15, want: vp8dec.NormalLoopFilter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tt.deadline, CpuUsed: tt.cpuUsed}}
			header := e.encoderLoopFilterHeader(17, 3)
			if header.Type != tt.want {
				t.Fatalf("loop filter type = %d, want %d", header.Type, tt.want)
			}
		})
	}
}

func TestEncodeIntoRealtimeHighSpeedWritesSimpleLoopFilter(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		TargetBitrateKbps: 300,
		MinQuantizer:      20,
		MaxQuantizer:      20,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -14,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	keySource := testImage(32, 32)
	fillImage(keySource, 80, 128, 128)
	key, err := e.EncodeInto(make([]byte, 4096), keySource, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if keyState.LoopFilter.Type != vp8dec.SimpleLoopFilter {
		t.Fatalf("key loop filter type = %d, want simple", keyState.LoopFilter.Type)
	}

	interSource := testImage(32, 32)
	fillImage(interSource, 82, 128, 128)
	inter, err := e.EncodeInto(make([]byte, 4096), interSource, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.Dropped {
		t.Fatalf("inter frame dropped, want encoded interframe")
	}
	interState := packetState(t, inter.Data)
	if interState.LoopFilter.Type != vp8dec.SimpleLoopFilter {
		t.Fatalf("inter loop filter type = %d, want simple", interState.LoopFilter.Type)
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
		{name: "realtime positive cpu-used auto-speed uses full search", deadline: DeadlineRealtime, cpuUsed: 5, want: false},
		{name: "realtime explicit speed two uses full search", deadline: DeadlineRealtime, cpuUsed: -2, want: false},
		{name: "realtime explicit speed three uses fast search", deadline: DeadlineRealtime, cpuUsed: -3, want: true},
		{name: "realtime explicit speed four uses full search", deadline: DeadlineRealtime, cpuUsed: -4, want: false},
		{name: "realtime explicit speed five uses fast search", deadline: DeadlineRealtime, cpuUsed: -5, want: true},
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
	for row := range 16 {
		for col := range 64 {
			ref.Img.Y[row*ref.Img.YStride+col] = 100
		}
	}
	for row := 32; row < 48; row++ {
		for col := range 64 {
			ref.Img.Y[row*ref.Img.YStride+col] = 23
		}
	}

	got := loopFilterLumaSSE(sourceImageFromPublic(src), &ref.Img, 4, 4, true)
	want := 4 * 16 * 16 * 3 * 3
	if got != want {
		t.Fatalf("partial luma SSE = %d, want %d", got, want)
	}
}

func TestLoopFilterTrialLumaSSELevelZeroScoresAnalysisWithoutScratchCopy(t *testing.T) {
	const width, height = 64, 128
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(33 + (r*13+c*3)%170)
			src.U[(r/2)*src.UStride+(c/2)] = 128
			src.V[(r/2)*src.VStride+(c/2)] = 128
		}
	}

	e := newSizedTestEncoder(t, width, height)
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(57 + (r*5+c*11)%160)
		}
	}
	for i := range e.loopFilterPick.Img.Y {
		e.loopFilterPick.Img.Y[i] = 201
	}
	scratchBefore := append([]byte(nil), e.loopFilterPick.Img.Y...)

	srcImg := sourceImageFromPublic(src)
	for _, partial := range []bool{false, true} {
		want := loopFilterLumaSSE(srcImg, &e.analysis.Img, rows, cols, partial)
		got, err := e.loopFilterTrialLumaSSE(srcImg, vp8common.InterFrame, 0, 0, rows, cols, required, partial, vp8enc.SegmentationConfig{})
		if err != nil {
			t.Fatalf("level zero trial partial=%t returned error: %v", partial, err)
		}
		if got != want {
			t.Fatalf("level zero trial partial=%t SSE = %d, want direct analysis SSE %d", partial, got, want)
		}
		if !bytes.Equal(e.loopFilterPick.Img.Y, scratchBefore) {
			t.Fatalf("level zero trial partial=%t modified loop-filter scratch buffer", partial)
		}
	}
}

func TestLoopFilterTrialLumaSSEPartialMatchesFullFrameWindow(t *testing.T) {
	const width, height = 64, 128
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	fillImage(src, 96, 128, 128)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(40 + (r*7+c*11)%160)
		}
	}

	e := newSizedTestEncoder(t, width, height)
	// Seed the analysis buffer with reconstructed-like values that differ
	// macroblock-by-macroblock so the loop filter actually has work to do.
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(50 + (r*5+c*9)%180)
		}
	}
	for i := range e.analysis.Img.U {
		e.analysis.Img.U[i] = 128
	}
	for i := range e.analysis.Img.V {
		e.analysis.Img.V[i] = 128
	}
	if len(e.reconstructModes) < required {
		e.reconstructModes = make([]vp8dec.MacroblockMode, required)
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
	}

	srcImg := sourceImageFromPublic(src)
	for _, level := range []int{8, 24, 48} {
		partialErr, err := e.loopFilterTrialLumaSSE(srcImg, vp8common.InterFrame, level, 0, rows, cols, required, true, vp8enc.SegmentationConfig{})
		if err != nil {
			t.Fatalf("partial trial level=%d returned error: %v", level, err)
		}
		fullErr, err := e.loopFilterTrialLumaSSE(srcImg, vp8common.InterFrame, level, 0, rows, cols, required, false, vp8enc.SegmentationConfig{})
		if err != nil {
			t.Fatalf("full trial level=%d returned error: %v", level, err)
		}
		// The full path computes SSE over the whole frame; recompute the
		// partial-window SSE on the buffer left behind by the full filter so
		// we can compare against the partial path.
		fullPartialWindow := loopFilterLumaSSE(srcImg, &e.loopFilterPick.Img, rows, cols, true)
		_ = fullErr
		if partialErr != fullPartialWindow {
			t.Fatalf("level=%d partial SSE = %d, full-frame partial-window SSE = %d", level, partialErr, fullPartialWindow)
		}
	}
}

func TestPickLoopFilterLevelFastMatchesFullFrameBaseline(t *testing.T) {
	const width, height = 64, 128
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(40 + (r*7+c*11)%160)
			src.U[(r/2)*src.UStride+(c/2)] = 128
			src.V[(r/2)*src.VStride+(c/2)] = 128
		}
	}

	buildEncoder := func() *VP8Encoder {
		e := newSizedTestEncoder(t, width, height)
		for r := 0; r < e.analysis.Img.CodedHeight; r++ {
			for c := 0; c < e.analysis.Img.CodedWidth; c++ {
				e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(50 + (r*5+c*9)%180)
			}
		}
		for i := range e.analysis.Img.U {
			e.analysis.Img.U[i] = 128
		}
		for i := range e.analysis.Img.V {
			e.analysis.Img.V[i] = 128
		}
		if len(e.reconstructModes) < required {
			e.reconstructModes = make([]vp8dec.MacroblockMode, required)
		}
		for i := range required {
			e.reconstructModes[i] = vp8dec.MacroblockMode{
				Mode:     vp8common.DCPred,
				UVMode:   vp8common.DCPred,
				RefFrame: vp8common.LastFrame,
			}
		}
		e.rc.currentQuantizer = 60
		return e
	}

	srcImg := sourceImageFromPublic(src)
	ePartial := buildEncoder()
	got, err := ePartial.pickLoopFilterLevelFast(srcImg, vp8common.InterFrame, 24, 0, rows, cols, required, vp8enc.SegmentationConfig{})
	if err != nil {
		t.Fatalf("pickLoopFilterLevelFast returned error: %v", err)
	}

	// Reference: search the same neighborhood as fast search but using the
	// full-frame loop filter and partial-window SSE. Selected level must
	// match exactly.
	eRef := buildEncoder()
	minLevel := libvpxMinLoopFilterLevel(eRef.rc.currentQuantizer)
	maxLevel := libvpxMaxLoopFilterLevel(eRef.rc.currentQuantizer)
	level := clampLoopFilterPickLevel(24, minLevel, maxLevel)
	bestLevel := level
	score := func(lvl int) int {
		if _, err := eRef.loopFilterTrialLumaSSE(srcImg, vp8common.InterFrame, lvl, 0, rows, cols, required, false, vp8enc.SegmentationConfig{}); err != nil {
			t.Fatalf("reference trial returned error: %v", err)
		}
		return loopFilterLumaSSE(srcImg, &eRef.loopFilterPick.Img, rows, cols, true)
	}
	bestErr := score(level)
	filtLevel := level - loopFilterSearchStep(level)
	for filtLevel >= minLevel {
		filtErr := score(filtLevel)
		if filtErr < bestErr {
			bestErr = filtErr
			bestLevel = filtLevel
		} else {
			break
		}
		filtLevel -= loopFilterSearchStep(filtLevel)
	}
	filtLevel = level + loopFilterSearchStep(filtLevel)
	if bestLevel == level {
		bestErr -= bestErr >> 10
		for filtLevel < maxLevel {
			filtErr := score(filtLevel)
			if filtErr < bestErr {
				bestErr = filtErr - (filtErr >> 10)
				bestLevel = filtLevel
			} else {
				break
			}
			filtLevel += loopFilterSearchStep(filtLevel)
		}
	}
	want := uint8(clampLoopFilterPickLevel(bestLevel, minLevel, maxLevel))
	if got != want {
		t.Fatalf("fast pick = %d, full-frame baseline = %d", got, want)
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

func BenchmarkLoopFilterTrialLumaSSEPartialLargeFrame(b *testing.B) {
	const width, height = 1024, 1024
	rows := (height + 15) / 16
	cols := (width + 15) / 16
	required := rows * cols

	src := testImage(width, height)
	for r := range height {
		for c := range width {
			src.Y[r*src.YStride+c] = byte(40 + (r*7+c*11)%160)
		}
	}
	e := newSizedTestEncoder(b, width, height)
	for r := 0; r < e.analysis.Img.CodedHeight; r++ {
		for c := 0; c < e.analysis.Img.CodedWidth; c++ {
			e.analysis.Img.Y[r*e.analysis.Img.YStride+c] = byte(50 + (r*5+c*9)%180)
		}
	}
	for i := range e.analysis.Img.U {
		e.analysis.Img.U[i] = 128
	}
	for i := range e.analysis.Img.V {
		e.analysis.Img.V[i] = 128
	}
	if len(e.reconstructModes) < required {
		e.reconstructModes = make([]vp8dec.MacroblockMode, required)
	}
	for i := range required {
		e.reconstructModes[i] = vp8dec.MacroblockMode{
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
			RefFrame: vp8common.LastFrame,
		}
	}
	srcImg := sourceImageFromPublic(src)

	b.Run("partial", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := e.loopFilterTrialLumaSSE(srcImg, vp8common.InterFrame, 24, 0, rows, cols, required, true, vp8enc.SegmentationConfig{}); err != nil {
				b.Fatalf("partial trial returned error: %v", err)
			}
		}
	})
	b.Run("full", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := e.loopFilterTrialLumaSSE(srcImg, vp8common.InterFrame, 24, 0, rows, cols, required, false, vp8enc.SegmentationConfig{}); err != nil {
				b.Fatalf("full trial returned error: %v", err)
			}
		}
	})
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
	// Negative cpu_used pins explicit Speed=-cpu_used (libvpx encodeframe.c:686),
	// bypassing vp8_auto_select_speed; positive cpu_used is now an auto-budget
	// target rather than a fixed Speed.
	if err := e.SetCPUUsed(-3); err != nil {
		t.Fatalf("SetCPUUsed returned error: %v", err)
	}
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
	// This test pins residual inter coding for the normal entropy path.
	// Error-resilient key frames intentionally refresh independent coefficient
	// contexts like libvpx, which can make this synthetic single-MB fixture pick
	// an intra inter-frame mode instead.
	e := newEntropyRefreshTestEncoder(t, false)
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
	if e.interFrameModes[0].RefFrame != vp8common.LastFrame || e.interFrameModes[0].MBSkipCoeff || !e.interFrameModes[0].MV.IsZero() {
		t.Fatalf("mode[0] = %+v, want LAST zero-motion residual macroblock", e.interFrameModes[0])
	}
	if e.interFrameModes[0].Mode != vp8common.ZeroMV && e.interFrameModes[0].Mode != vp8common.NewMV {
		t.Fatalf("mode[0] = %+v, want LAST zero-motion residual mode", e.interFrameModes[0])
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

func TestEncodeIntoErrorResilientRefreshesKeyEntropyOnly(t *testing.T) {
	e := newEntropyRefreshTestEncoder(t, true)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient key refresh entropy = false, want libvpx forced true")
	}
	if keyState.Probability.UpdateCount == 0 {
		t.Fatalf("error-resilient key coefficient updates = 0, want independent-context updates")
	}
	committedKeyProbs := e.coefProbs
	if committedKeyProbs == vp8tables.DefaultCoefProbs {
		t.Fatalf("error-resilient key did not commit coefficient probabilities")
	}

	inter, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 2), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient inter refresh entropy = true, want false")
	}
	if e.coefProbs != committedKeyProbs {
		t.Fatalf("error-resilient inter committed transient coefficient probabilities")
	}
}

func TestCoefficientEntropySavingsUsesIndependentContextWhenErrorResilient(t *testing.T) {
	// The independent-context coefficient entropy-savings path mirrors
	// libvpx's VPX_ERROR_RESILIENT_PARTITIONS branch (bit 0x2). The plain
	// `--error-resilient=1` (DEFAULT, bit 0x1) does NOT enable that branch
	// in libvpx; only the partitions mode does. govpx exposes this as
	// EncoderOptions.ErrorResilientPartitions; the simpler ErrorResilient
	// bool stays on the default coef-savings path so the keyframe coef-prob
	// emission stays byte-equivalent with libvpx's `--error-resilient=1`.
	e := &VP8Encoder{
		opts: EncoderOptions{
			Width:                    16,
			Height:                   16,
			ErrorResilientPartitions: true,
		},
		coefProbs: vp8tables.DefaultCoefProbs,
		interFrameModes: []vp8enc.InterFrameMacroblockMode{{
			RefFrame: vp8common.LastFrame,
			Mode:     vp8common.ZeroMV,
		}},
		keyFrameCoeffs: make([]vp8enc.MacroblockCoefficients, 1),
		tokenAbove:     make([]vp8enc.TokenContextPlanes, 1),
	}
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					e.coefProbs[block][band][ctx][node] = 1
				}
			}
		}
	}
	e.keyFrameCoeffs[0].QCoeff[0][0] = 1
	e.keyFrameCoeffs[0].SetBlockEOB(0, 1)
	got := e.coefficientEntropySavingsBits(false, 1)
	above := make([]vp8enc.TokenContextPlanes, 1)
	want, err := vp8enc.InterCoefficientEntropySavingsIndependent(1, 1, e.interFrameModes, e.keyFrameCoeffs, above, &e.coefProbs)
	if err != nil {
		t.Fatalf("InterCoefficientEntropySavingsIndependent returned error: %v", err)
	}
	if got != want {
		t.Fatalf("error-resilient coefficient entropy savings = %d, want independent-context savings %d", got, want)
	}
	if got == 0 {
		t.Fatalf("error-resilient coefficient entropy savings = 0, want recode accounting to include independent-context branch")
	}
}

func TestEncodeIntoTemporalBaseLayerIsDecodableWithoutEnhancementFrames(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	dst := make([]byte, 8192)
	basePackets := make([][]byte, 0, 3)

	for i := range 6 {
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
	keySrc := testImage(16, 16)
	altSrc := testImage(16, 16)
	fillImage(keySrc, 220, 90, 170)
	fillImage(altSrc, 40, 91, 171)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, keySrc, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 4096)
	altInter, err := e.EncodeInto(interPacket, altSrc, 1, 1, EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden)
	if err != nil {
		t.Fatalf("alt refresh EncodeInto returned error: %v", err)
	}
	altState := packetState(t, altInter.Data)
	if altState.Refresh.RefreshLast || altState.Refresh.RefreshGolden || !altState.Refresh.RefreshAltRef {
		t.Fatalf("alt refresh flags = %+v, want alt-only refresh", altState.Refresh)
	}
	altData := append([]byte(nil), altInter.Data...)
	altDecoded := decodeFrameSequence(t, key.Data, altData)
	if len(altDecoded) != 2 {
		t.Fatalf("alt refresh decoded frame count = %d, want 2", len(altDecoded))
	}
	altFrame := altDecoded[1]

	result, err := e.EncodeInto(interPacket, altFrame, 2, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("KeyFrame = true, want interframe using altref")
	}
	if e.interFrameModes[0].RefFrame != vp8common.AltRefFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped ALTREF/ZEROMV", e.interFrameModes[0])
	}
	decoded := decodeFrameSequence(t, key.Data, altData, result.Data)
	if len(decoded) != 3 {
		t.Fatalf("decoded frame count = %d, want 3", len(decoded))
	}
	assertImagesEqual(t, "altref interframe", altFrame, decoded[2])
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
	if len(e.oracleTraceMBBuffer) != 0 {
		t.Fatalf("discarded inter-attempt MB trace rows = %d, want 0", len(e.oracleTraceMBBuffer))
	}
}

func TestEncodeIntoAdaptiveKeyFramesRecodeUsesLibvpxDecideKeyFrame(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             80,
		Height:            80,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           0,
		KeyFrameInterval:  120,
		AdaptiveKeyFrames: true,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	first := testImage(80, 80)
	second := testImage(80, 80)
	fillImage(first, 0, 90, 170)
	fillImage(second, 128, 90, 170)
	dst := make([]byte, 65536)
	if _, err := e.EncodeInto(dst, first, 0, 1, 0); err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}
	e.lastFramePercentIntra = 0

	result, err := e.EncodeInto(dst, second, 1, 1, EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame || !result.SceneCut {
		intra, last, golden, alt := countInterFrameRefUsage(e.interFrameModes)
		t.Fatalf("auto-key recode result = key:%t scene:%t refs:%d/%d/%d/%d, want key scene-cut",
			result.KeyFrame, result.SceneCut, intra, last, golden, alt)
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

func TestShouldRecodeInterAttemptAsKeyFrameMirrorsLibvpxGate(t *testing.T) {
	e := &VP8Encoder{
		opts:                  EncoderOptions{AdaptiveKeyFrames: true, Deadline: DeadlineGoodQuality},
		lastFramePercentIntra: 20,
		interFrameModes: []vp8enc.InterFrameMacroblockMode{
			{Mode: vp8common.DCPred},
			{Mode: vp8common.DCPred},
			{Mode: vp8common.DCPred},
			{Mode: vp8common.ZeroMV, RefFrame: vp8common.LastFrame},
		},
	}
	if pct, ok := e.shouldRecodeInterAttemptAsKeyFrame(4, false, false, false); pct != 75 || !ok {
		t.Fatalf("auto-key recode = pct:%d ok:%t, want pct75 true", pct, ok)
	}
	if pct, ok := e.shouldRecodeInterAttemptAsKeyFrame(4, true, false, false); pct != 75 || ok {
		t.Fatalf("golden-refresh auto-key recode = pct:%d ok:%t, want pct75 false", pct, ok)
	}

	e.interFrameModes[3] = vp8enc.InterFrameMacroblockMode{Mode: vp8common.DCPred}
	if pct, ok := e.shouldRecodeInterAttemptAsKeyFrame(4, true, false, false); pct != 100 || !ok {
		t.Fatalf("unconditional auto-key recode = pct:%d ok:%t, want pct100 true", pct, ok)
	}

	e.opts.Deadline = DeadlineRealtime
	if _, ok := e.shouldRecodeInterAttemptAsKeyFrame(4, false, false, false); ok {
		t.Fatalf("realtime auto-key recode = true, want false for compressor_speed 2")
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
	// libvpx vp8_temporal_filter_prepare_c only fires for the hidden alt-ref
	// source (gated on `cpi->source_alt_ref_pending`). govpx mirrors that by
	// running ARNR only when the encode flags carry the hidden-ARF combo
	// (EncodeForceAltRefFrame|EncodeInvisibleFrame). Drive the encoder with
	// AutoAltRef=true so the auto-ARF driver schedules a hidden frame on the
	// libvpx-faithful path; on that hidden frame both ARNR and the spatial
	// denoiser report having run.
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
		LookaheadFrames:   8,
		AutoAltRef:        true,
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
	for i := range noisy.Y {
		if i%2 == 0 {
			noisy.Y[i] = 40
		} else {
			noisy.Y[i] = 60
		}
	}
	clean := testImage(16, 16)
	fillImage(clean, 50, 90, 170)
	const totalFrames = 12
	frames := make([]Image, totalFrames)
	for i := range frames {
		if i == 0 {
			frames[i] = noisy
		} else {
			frames[i] = clean
		}
	}
	var sawARNR bool
	for i, src := range frames {
		result, err := e.EncodeInto(dst, src, uint64(i), 1, 0)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				continue
			}
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
		if result.ARNRFiltered {
			if !result.Denoised {
				t.Fatalf("frame %d arnr=true but denoised=false", i)
			}
			sawARNR = true
			break
		}
	}
	if !sawARNR {
		// Drain the lookahead so the hidden ARF can fire on flush.
		for {
			result, err := e.FlushInto(dst)
			if err != nil {
				if errors.Is(err, ErrFrameNotReady) {
					break
				}
				t.Fatalf("FlushInto returned error: %v", err)
			}
			if result.ARNRFiltered {
				if !result.Denoised {
					t.Fatalf("flush arnr=true but denoised=false")
				}
				sawARNR = true
				break
			}
		}
	}
	if !sawARNR {
		t.Fatalf("no encoded frame reported ARNR filtering: auto-ARF driver did not emit a hidden ARF on the configured fixture")
	}
}

func TestCollectFirstPassStatsAndTwoPassSceneCut(t *testing.T) {
	const (
		width  = 256
		height = 256
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
	fillScene := func(img Image, base int) {
		for y := 0; y < img.Height; y++ {
			for x := 0; x < img.Width; x++ {
				img.Y[y*img.YStride+x] = byte(base + ((x*17 + y*31 + x*y*3) & 63))
			}
		}
		for i := range img.U {
			img.U[i] = 90
			img.V[i] = 170
		}
	}
	for i := range frames {
		frames[i] = testImage(width, height)
		if i < 5 {
			fillScene(frames[i], 20)
		} else {
			fillScene(frames[i], 150)
		}
		stats[i], err = firstPass.CollectFirstPassStats(frames[i], uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("CollectFirstPassStats %d returned error: %v", i, err)
		}
	}
	if !libvpxTestCandidateKeyFrame(stats, 5) {
		t.Fatalf("first-pass stats did not satisfy libvpx candidate keyframe test at scene cut: prev=%+v cut=%+v next=%+v", stats[4], stats[5], stats[6])
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
	dst := make([]byte, 512*1024)
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

// TestEncodeIntoMultiSizeInterFrameAllocatesZero guards the per-frame
// reconstruction scratch pool added in parity-close-r10-d-allocs. The
// reconstruction builder used to allocate a fresh
// []vp8enc.TokenContextPlanes of length cols every frame, which the 16x16
// fixture happens to mask because cols==1 and the tiny slice can sit on the
// caller's stack. Anything wider (>=64x64) faithfully exercises the heap
// allocator and traps regressions in the per-row above-token scratch.
func TestEncodeIntoMultiSizeInterFrameAllocatesZero(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"64x64", 64, 64},
		{"128x128", 128, 128},
		{"320x240", 320, 240},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSizedTestEncoder(t, tc.w, tc.h)
			defer e.Close()
			if err := e.SetKeyFrameInterval(0); err != nil {
				t.Fatalf("SetKeyFrameInterval returned error: %v", err)
			}
			src := testImage(tc.w, tc.h)
			fillImage(src, 220, 90, 170)
			dst := make([]byte, tc.w*tc.h*6+4096)
			if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
				t.Fatalf("key EncodeInto returned error: %v", err)
			}
			// Warm any one-shot lazy state so AllocsPerRun's first iteration
			// does not double-count construction allocations.
			for i := range 4 {
				if _, err := e.EncodeInto(dst, src, uint64(i+1), 1, 0); err != nil {
					t.Fatalf("warmup EncodeInto returned error: %v", err)
				}
			}
			pts := uint64(64)
			allocs := testing.AllocsPerRun(64, func() {
				_, _ = e.EncodeInto(dst, src, pts, 1, 0)
				pts++
			})
			if allocs != 0 {
				t.Fatalf("inter-frame EncodeInto allocs = %v at %s, want 0", allocs, tc.name)
			}
		})
	}
}

// TestEncodeIntoMultiResolutionAllocatesZero is the parity-close-r15-d
// regression guard for steady-state zero allocations across the full
// resolution + cpu_used matrix exercised by govpx-bench. The earlier
// TestEncodeIntoMultiSizeInterFrameAllocatesZero capped at 320x240 and only
// exercised CpuUsed=8; this covers 320x240/640x480/1280x720/1920x1080 against
// CpuUsed in {0,3,5,8,15} (Speed bands feeding libvpx_auto_select_speed plus
// the static "RT highest speed" 15 ceiling). The frames are spatial-temporal
// gradients matching cmd/govpx-bench's makeBenchmarkFrame, exercising the
// inter-mode picker, encoder match path, and rate control loops the
// flat-fill fixture above does not.
func TestEncodeIntoMultiResolutionAllocatesZero(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multi-resolution alloc sweep in -short")
	}
	type resCase struct {
		name string
		w, h int
	}
	resolutions := []resCase{
		{"320x240", 320, 240},
		{"640x480", 640, 480},
		{"1280x720", 1280, 720},
		{"1920x1080", 1920, 1080},
	}
	cpuBands := []int{0, 3, 5, 8, 15}
	for _, rc := range resolutions {
		for _, cpu := range cpuBands {
			t.Run(fmt.Sprintf("%s/cpu=%d", rc.name, cpu), func(t *testing.T) {
				e := newSizedTestEncoder(t, rc.w, rc.h)
				defer e.Close()
				if err := e.SetCPUUsed(cpu); err != nil {
					t.Fatalf("SetCPUUsed(%d) returned error: %v", cpu, err)
				}
				if err := e.SetKeyFrameInterval(0); err != nil {
					t.Fatalf("SetKeyFrameInterval returned error: %v", err)
				}
				const frames = 6
				srcs := make([]Image, frames)
				for i := range srcs {
					srcs[i] = makeMultiResAllocFrame(rc.w, rc.h, i)
				}
				dst := make([]byte, rc.w*rc.h*6+4096)
				// Encode the keyframe + a few inter frames so that any
				// lazily-initialised per-frame state (recode scratch, segment
				// buffers, inter-mode bookkeeping) is warm before
				// AllocsPerRun starts counting.
				if _, err := e.EncodeInto(dst, srcs[0], 0, 1, 0); err != nil {
					t.Fatalf("key EncodeInto returned error: %v", err)
				}
				for i := 1; i < frames; i++ {
					if _, err := e.EncodeInto(dst, srcs[i], uint64(i), 1, 0); err != nil {
						t.Fatalf("warmup inter EncodeInto returned error: %v", err)
					}
				}
				pts := uint64(frames)
				idx := 0
				allocs := testing.AllocsPerRun(20, func() {
					_, _ = e.EncodeInto(dst, srcs[idx%frames], pts, 1, 0)
					idx++
					pts++
				})
				if allocs != 0 {
					t.Fatalf("inter-frame EncodeInto allocs = %v at %s cpu=%d, want 0", allocs, rc.name, cpu)
				}
			})
		}
	}
}

// makeMultiResAllocFrame mirrors cmd/govpx-bench/main.go::makeBenchmarkFrame
// so the alloc regression guard touches the same picker / rate-control paths
// as the bench harness that originally exposed the per-frame allocations.
// Keeping this helper local to encoder_test.go avoids importing the bench
// package and the resulting test-only dependency cycle.
func makeMultiResAllocFrame(width int, height int, index int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

// TestEncodeIntoMultiTokenPartitionAllocatesZero locks in the
// parity-close-r15-d-v2 partition-buffer pool (PartitionScratch on
// VP8Encoder). Multi-token-partition encodes (libvpx --token-parts=N>0)
// previously allocated N+1 objects per frame: one byte buffer per
// partition plus the closure passed into writePartitionedTokenPayload.
// The new prepare/finalize split routes through e.partScratch, so the
// steady-state alloc count is 0 across all four supported token-partition
// modes (1/2/4/8 partitions = TokenPartitions in {0,1,2,3}).
func TestEncodeIntoMultiTokenPartitionAllocatesZero(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multi-token-partition alloc sweep in -short")
	}
	cases := []struct {
		name      string
		partition int
	}{
		{"1part", 0},
		{"2parts", 1},
		{"4parts", 2},
		{"8parts", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSizedTestEncoder(t, 320, 240)
			defer e.Close()
			if err := e.SetTokenPartitions(tc.partition); err != nil {
				t.Fatalf("SetTokenPartitions(%d): %v", tc.partition, err)
			}
			if err := e.SetKeyFrameInterval(0); err != nil {
				t.Fatalf("SetKeyFrameInterval: %v", err)
			}
			src := testImage(320, 240)
			fillImage(src, 220, 90, 170)
			dst := make([]byte, 320*240*6+4096)
			if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
				t.Fatalf("key EncodeInto: %v", err)
			}
			// Warm the partition scratch + any other lazy state.
			for i := 1; i <= 6; i++ {
				if _, err := e.EncodeInto(dst, src, uint64(i), 1, 0); err != nil {
					t.Fatalf("warmup EncodeInto: %v", err)
				}
			}
			pts := uint64(7)
			allocs := testing.AllocsPerRun(20, func() {
				_, _ = e.EncodeInto(dst, src, pts, 1, 0)
				pts++
			})
			if allocs != 0 {
				t.Fatalf("EncodeInto allocs/op = %v at TokenPartitions=%d, want 0", allocs, tc.partition)
			}
		})
	}
}

// BenchmarkEncodeInto is the parity-close-r15-d alloc-tracking sweep across
// the same resolutions that TestEncodeIntoMultiResolutionAllocatesZero
// guards. Run with `-benchmem -count=10 -benchtime=200x` to confirm
// allocs/op == 0 at all sizes after the encoder is warm. Each subtest covers
// a single resolution with CpuUsed=8 (the bench-harness default); the
// AllocsPerRun test above sweeps the CpuUsed band.
func BenchmarkEncodeInto(b *testing.B) {
	resolutions := []struct {
		name string
		w, h int
	}{
		{"320x240", 320, 240},
		{"640x480", 640, 480},
		{"1280x720", 1280, 720},
		{"1920x1080", 1920, 1080},
	}
	for _, rc := range resolutions {
		b.Run(rc.name, func(b *testing.B) {
			e := newSizedTestEncoder(b, rc.w, rc.h)
			defer e.Close()
			if err := e.SetKeyFrameInterval(0); err != nil {
				b.Fatalf("SetKeyFrameInterval returned error: %v", err)
			}
			const cycle = 6
			srcs := make([]Image, cycle)
			for i := range srcs {
				srcs[i] = makeMultiResAllocFrame(rc.w, rc.h, i)
			}
			dst := make([]byte, rc.w*rc.h*6+4096)
			// Warm the encoder so the steady-state hot path is what the
			// benchmark measures (matches govpx-bench which also reads
			// MemStats only after a warm pre-pass).
			if _, err := e.EncodeInto(dst, srcs[0], 0, 1, 0); err != nil {
				b.Fatalf("key EncodeInto returned error: %v", err)
			}
			for i := 1; i < cycle; i++ {
				if _, err := e.EncodeInto(dst, srcs[i], uint64(i), 1, 0); err != nil {
					b.Fatalf("warmup EncodeInto returned error: %v", err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			pts := uint64(cycle)
			for i := 0; i < b.N; i++ {
				if _, err := e.EncodeInto(dst, srcs[i%cycle], pts, 1, 0); err != nil {
					b.Fatalf("steady-state EncodeInto returned error: %v", err)
				}
				pts++
			}
		})
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
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		// Keep this on a libvpx recode-enabled speed. Realtime speeds disable
		// the normal size recode loop, so they are covered by oracle smoke
		// rate-gap tests rather than this tight target assertion.
		Deadline:            DeadlineGoodQuality,
		CpuUsed:             0,
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
	for i := range frames {
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
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for row := range uvHeight {
		for col := range uvWidth {
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
	for row := range 8 {
		for col := range 8 {
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
	for row := range height {
		wantRow := want[row*wantStride : row*wantStride+width]
		gotRow := got[row*gotStride : row*gotStride+width]
		for col := range width {
			if gotRow[col] != wantRow[col] {
				t.Fatalf("%s[%d,%d] = %d, want %d", name, row, col, gotRow[col], wantRow[col])
			}
		}
	}
}

func assertMacroblockEqual(t *testing.T, name string, want Image, got Image, mbRow int, mbCol int) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height {
		t.Fatalf("%s dimensions = %dx%d, want %dx%d", name, got.Width, got.Height, want.Width, want.Height)
	}
	assertPlaneBlockEqual(t, name+" Y", want.Y, want.YStride, got.Y, got.YStride, want.Width, want.Height, mbRow*16, mbCol*16, 16, 16)
	uvWidth := (want.Width + 1) >> 1
	uvHeight := (want.Height + 1) >> 1
	assertPlaneBlockEqual(t, name+" U", want.U, want.UStride, got.U, got.UStride, uvWidth, uvHeight, mbRow*8, mbCol*8, 8, 8)
	assertPlaneBlockEqual(t, name+" V", want.V, want.VStride, got.V, got.VStride, uvWidth, uvHeight, mbRow*8, mbCol*8, 8, 8)
}

func assertMacroblockDifferent(t *testing.T, name string, a Image, b Image, mbRow int, mbCol int) {
	t.Helper()
	if a.Width != b.Width || a.Height != b.Height {
		t.Fatalf("%s dimensions differ: %dx%d vs %dx%d", name, a.Width, a.Height, b.Width, b.Height)
	}
	if macroblockEqual(a, b, mbRow, mbCol) {
		t.Fatalf("%s macroblock (%d,%d) matches previous frame; want active MB to update", name, mbRow, mbCol)
	}
}

func assertPlaneBlockEqual(t *testing.T, name string, want []byte, wantStride int, got []byte, gotStride int, planeWidth int, planeHeight int, startRow int, startCol int, blockWidth int, blockHeight int) {
	t.Helper()
	width := min(blockWidth, planeWidth-startCol)
	height := min(blockHeight, planeHeight-startRow)
	for row := range height {
		for col := range width {
			wantValue := want[(startRow+row)*wantStride+startCol+col]
			gotValue := got[(startRow+row)*gotStride+startCol+col]
			if gotValue != wantValue {
				t.Fatalf("%s[%d,%d] = %d, want %d", name, startRow+row, startCol+col, gotValue, wantValue)
			}
		}
	}
}

func macroblockEqual(a Image, b Image, mbRow int, mbCol int) bool {
	if !planeBlockEqual(a.Y, a.YStride, b.Y, b.YStride, a.Width, a.Height, mbRow*16, mbCol*16, 16, 16) {
		return false
	}
	uvWidth := (a.Width + 1) >> 1
	uvHeight := (a.Height + 1) >> 1
	return planeBlockEqual(a.U, a.UStride, b.U, b.UStride, uvWidth, uvHeight, mbRow*8, mbCol*8, 8, 8) &&
		planeBlockEqual(a.V, a.VStride, b.V, b.VStride, uvWidth, uvHeight, mbRow*8, mbCol*8, 8, 8)
}

func planeBlockEqual(a []byte, aStride int, b []byte, bStride int, planeWidth int, planeHeight int, startRow int, startCol int, blockWidth int, blockHeight int) bool {
	width := min(blockWidth, planeWidth-startCol)
	height := min(blockHeight, planeHeight-startRow)
	for row := range height {
		for col := range width {
			if a[(startRow+row)*aStride+startCol+col] != b[(startRow+row)*bStride+startCol+col] {
				return false
			}
		}
	}
	return true
}

func TestMacroblockCornerGradientMatchesLibvpxFormula(t *testing.T) {
	// 16x16 plane with stride 16: every value 50 except the top-left corner pixel = 90.
	// Top-left corner (offRow=0, offCol=0, sgnRow=1, sgnCol=1) should yield max(|90-50|, ...) = 40.
	plane := make([]byte, 16*16)
	for i := range plane {
		plane[i] = 50
	}
	plane[0] = 90
	if got := macroblockCornerGradient(plane, 16, 0, 0, 1, 1); got != 40 {
		t.Fatalf("top-left gradient = %d, want 40", got)
	}
	// Flat plane: all corners should yield 0.
	for i := range plane {
		plane[i] = 50
	}
	if got := macroblockCornerGradient(plane, 16, 0, 15, 1, -1); got != 0 {
		t.Fatalf("flat top-right gradient = %d, want 0", got)
	}
}

func TestDotArtifactCornerCandidateYDetectsSharpRefAndFlatSrc(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	last := vp8common.FrameBuffer{}
	if err := last.Resize(16, 16, 16, 16); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	// Flat last reference: not a candidate.
	for i := range last.Img.Y {
		last.Img.Y[i] = 128
	}
	if dotArtifactCornerCandidateY(sourceImageFromPublic(src), &last.Img, 0, 0) {
		t.Fatalf("flat last_ref should not be a dot-artifact candidate")
	}
	// Sharp gradient at top-left corner of last_ref: should be a candidate.
	last.Img.Y[0] = 200
	if !dotArtifactCornerCandidateY(sourceImageFromPublic(src), &last.Img, 0, 0) {
		t.Fatalf("sharp last_ref corner over flat src should be a candidate")
	}
	// If source also has sharp gradient, no longer a candidate.
	src.Y[0] = 200
	if dotArtifactCornerCandidateY(sourceImageFromPublic(src), &last.Img, 0, 0) {
		t.Fatalf("matching sharp source should suppress candidate")
	}
}

func TestCheckDotArtifactCandidateGatesOnLayerScreenContentAndConsecZeroLast(t *testing.T) {
	// Use a 64x64 encoder (16 MBs => cap = 16/10 = 1) so the cap is non-zero.
	e := newSizedTestEncoder(t, 64, 64)
	src := testImage(64, 64)
	fillImage(src, 128, 128, 128)
	for i := range e.lastRef.Img.Y {
		e.lastRef.Img.Y[i] = 128
	}
	// Sharp top-left corner of MB(0,0) on last_ref Y plane.
	e.lastRef.Img.Y[0] = 230

	// mvbias counter below threshold => not a candidate.
	e.consecZeroLastMVBias[0] = 5
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("low consec_zero_last_mvbias should not trigger dot-artifact bias")
	}
	if e.dotArtifactChecked[0] {
		t.Fatalf("ineligible MB should not set dotArtifactChecked")
	}
	// Above threshold => candidate; sets the per-MB checked flag.
	e.consecZeroLastMVBias[0] = 50
	e.mbsZeroLastDotSuppress = 0
	if !e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("high mvbias counter with sharp last_ref should be a candidate")
	}
	if !e.dotArtifactChecked[0] {
		t.Fatalf("eligible MB should set dotArtifactChecked")
	}
	if e.mbsZeroLastDotSuppress != 1 {
		t.Fatalf("mbsZeroLastDotSuppress = %d, want 1 after candidate", e.mbsZeroLastDotSuppress)
	}
	// Screen content disables it.
	e.mbsZeroLastDotSuppress = 0
	e.opts.ScreenContentMode = 1
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("screen content should disable dot-artifact bias")
	}
	e.opts.ScreenContentMode = 0
	// Non-base layer disables it.
	e.currentTemporalLayer = 1
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("non-base temporal layer should disable dot-artifact bias")
	}
	e.currentTemporalLayer = 0
	// Cap reached.
	e.mbsZeroLastDotSuppress = 1
	if e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("cap-reached suppression should disable further bias")
	}
}

func TestCheckDotArtifactCandidateChecksUVChannelsWhenYIsFlat(t *testing.T) {
	e := newSizedTestEncoder(t, 64, 64)
	src := testImage(64, 64)
	fillImage(src, 128, 128, 128)
	// Flat Y on last_ref so Y check returns false.
	for i := range e.lastRef.Img.Y {
		e.lastRef.Img.Y[i] = 128
	}
	// Sharp top-left corner on U plane only.
	for i := range e.lastRef.Img.U {
		e.lastRef.Img.U[i] = 128
	}
	for i := range e.lastRef.Img.V {
		e.lastRef.Img.V[i] = 128
	}
	e.lastRef.Img.U[0] = 230
	e.consecZeroLastMVBias[0] = 50
	if !e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("sharp U-plane corner should trigger dot-artifact bias when Y is flat")
	}
	// Reset and probe V plane only.
	e.lastRef.Img.U[0] = 128
	e.lastRef.Img.V[0] = 230
	e.mbsZeroLastDotSuppress = 0
	if !e.checkDotArtifactCandidate(sourceImageFromPublic(src), &e.lastRef.Img, 0, 0, 4, 4) {
		t.Fatalf("sharp V-plane corner should trigger dot-artifact bias when Y/U are flat")
	}
}

func TestComputeSkin8x8BlockNeedsTwoSubBlocksToTrigger(t *testing.T) {
	// (Y=120, U=117, V=150) is a known skin tuple per
	// TestCyclicRefreshStaticClassificationMasksSkinBlocks. Build a 16x16
	// MB where exactly one 8x8 sub-block has the skin tuple and the other
	// three are neutral grey. SKIN_8X8 requires two skin sub-blocks =>
	// this MB is not skin.
	src := testImage(16, 16)
	fillImage(src, 128, 128, 128)
	for row := range 8 {
		for col := range 8 {
			src.Y[row*src.YStride+col] = 120
		}
	}
	uvW := (src.Width + 1) >> 1
	uvH := (src.Height + 1) >> 1
	for row := range 4 {
		for col := range 4 {
			src.U[row*src.UStride+col] = 117
			src.V[row*src.VStride+col] = 150
		}
	}
	if computeSkin8x8Block(sourceImageFromPublic(src), uvW, uvH, 0, 0, 0) {
		t.Fatalf("single skin sub-block should not flag MB as skin under SKIN_8X8")
	}
	// Promote a second sub-block to skin colour: now MB qualifies.
	for row := range 8 {
		for col := 8; col < 16; col++ {
			src.Y[row*src.YStride+col] = 120
		}
	}
	for row := range 4 {
		for col := 4; col < 8; col++ {
			src.U[row*src.UStride+col] = 117
			src.V[row*src.VStride+col] = 150
		}
	}
	if !computeSkin8x8Block(sourceImageFromPublic(src), uvW, uvH, 0, 0, 0) {
		t.Fatalf("two skin sub-blocks should flag MB as skin under SKIN_8X8")
	}
	// Long zero-MV streak forces motion=0 and short-circuits past 60 frames.
	if computeSkin8x8Block(sourceImageFromPublic(src), uvW, uvH, 0, 0, 70) {
		t.Fatalf("consec_zero_last > 60 should suppress skin classification")
	}
}

func TestComputeSkinMapUsesSkin8x8ForSmallFramesAndSkin16x16ForLarge(t *testing.T) {
	makeSkinSrc := func(width int, height int) Image {
		src := testImage(width, height)
		// Y=120, U=117, V=150 is a known skin tuple.
		fillImage(src, 120, 117, 150)
		// Flip the top-left 8x8 Y sub-block of MB(0,0) to non-skin.
		for row := range 8 {
			for col := range 8 {
				src.Y[row*src.YStride+col] = 30
			}
		}
		return src
	}
	// Small frame: SKIN_8X8 with 3 of 4 sub-blocks skin classifies as skin.
	smallSrc := makeSkinSrc(16, 16)
	smallMap := make([]uint8, 1)
	computeSkinMap(sourceImageFromPublic(smallSrc), 1, 1, []uint8{0}, smallMap)
	if smallMap[0] != 1 {
		t.Fatalf("small-frame skin map = %d, want 1 (SKIN_8X8 path with majority skin sub-blocks)", smallMap[0])
	}
	// Width*Height > 352*288 selects SKIN_16X16. Use 384x288 (110592 > 101376).
	largeSrc := makeSkinSrc(384, 288)
	rows, cols := encoderMacroblockRows(288), encoderMacroblockCols(384)
	largeMap := make([]uint8, rows*cols)
	consec := make([]uint8, rows*cols)
	computeSkinMap(sourceImageFromPublic(largeSrc), rows, cols, consec, largeMap)
	if largeMap[0] != 1 {
		t.Fatalf("large-frame MB(0,0) skin map = %d, want 1 (SKIN_16X16 centre sample inside skin region)", largeMap[0])
	}
}

func TestUpdateConsecutiveZeroLastWithDotSuppressResetsCheckedMBs(t *testing.T) {
	counters := []uint8{40, 25}
	dotChecked := []bool{true, false}
	modes := []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
	}
	updateConsecutiveZeroLastWithDotSuppress(modes, counters, dotChecked)
	if counters[0] != 0 {
		t.Fatalf("dot-checked counter[0] = %d, want reset to 0", counters[0])
	}
	if counters[1] != 26 {
		t.Fatalf("non-checked counter[1] = %d, want incremented to 26", counters[1])
	}
}

func TestSetActiveMapValidation(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	mapBytes := make([]byte, 4)
	for i := range mapBytes {
		mapBytes[i] = 1
	}
	if err := e.SetActiveMap(mapBytes, 1, 4); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-row SetActiveMap error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetActiveMap(mapBytes, 2, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-col SetActiveMap error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetActiveMap(mapBytes[:1], 2, 2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("short-buffer SetActiveMap error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetActiveMap(mapBytes, 2, 2); err != nil {
		t.Fatalf("matching-size SetActiveMap error = %v", err)
	}
	if !e.activeMapEnabled {
		t.Fatalf("activeMapEnabled = false after SetActiveMap, want true")
	}
	if err := e.SetActiveMap(nil, 0, 0); err != nil {
		t.Fatalf("nil SetActiveMap error = %v", err)
	}
	if e.activeMapEnabled {
		t.Fatalf("activeMapEnabled = true after disabling, want false")
	}
}

func TestSetActiveMapInactiveInterMacroblocksAreSkippedZeroMVLast(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	first := testImage(32, 32)
	second := testImage(32, 32)
	// Distinct content per frame so inactive MBs would normally code residual.
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 90, 170)
	keyPacket := make([]byte, 8192)
	keyResult, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	rows := encoderMacroblockRows(32)
	cols := encoderMacroblockCols(32)
	activeMap := make([]byte, rows*cols)
	for i := range activeMap {
		activeMap[i] = 1
	}
	// Mark a single MB inactive.
	inactiveRow, inactiveCol := 1, 0
	inactiveIndex := inactiveRow*cols + inactiveCol
	activeMap[inactiveIndex] = 0
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap returned error: %v", err)
	}
	interPacket := make([]byte, 8192)
	interResult, err := e.EncodeInto(interPacket, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	mode := e.interFrameModes[inactiveIndex]
	if mode.RefFrame != vp8common.LastFrame || mode.Mode != vp8common.ZeroMV || !mode.MBSkipCoeff {
		t.Fatalf("inactive MB mode = %+v, want skipped LAST/ZEROMV", mode)
	}
	if mode.MV != (vp8enc.MotionVector{}) {
		t.Fatalf("inactive MB MV = %+v, want zero", mode.MV)
	}
	if mode.SegmentID != 0 {
		t.Fatalf("inactive MB SegmentID = %d, want 0", mode.SegmentID)
	}
	if !e.interFrameModes[inactiveIndex].MBSkipCoeff {
		t.Fatalf("inactive MB MBSkipCoeff = false, want true")
	}
	decoded := decodeFrameSequence(t, keyResult.Data, interResult.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertMacroblockEqual(t, "inactive active-map MB", decoded[0], decoded[1], inactiveRow, inactiveCol)
	assertMacroblockDifferent(t, "neighboring active-map MB", decoded[0], decoded[1], 0, 1)
}

func TestSetActiveMapDisabledLeavesModeDecisionFree(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 32)
	first := testImage(32, 32)
	second := testImage(32, 32)
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 90, 170)
	rows := encoderMacroblockRows(32)
	cols := encoderMacroblockCols(32)
	activeMap := make([]byte, rows*cols)
	if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
		t.Fatalf("SetActiveMap returned error: %v", err)
	}
	// Disable: subsequent inter encode should not force any MB skip.
	if err := e.SetActiveMap(nil, 0, 0); err != nil {
		t.Fatalf("nil SetActiveMap returned error: %v", err)
	}
	keyPacket := make([]byte, 8192)
	if _, err := e.EncodeInto(keyPacket, first, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 8192)
	if _, err := e.EncodeInto(interPacket, second, 1, 1, 0); err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	allSkipped := true
	for i := range e.interFrameModes {
		if !e.interFrameModes[i].MBSkipCoeff {
			allSkipped = false
			break
		}
	}
	if allSkipped {
		t.Fatalf("disabled active map still forced every MB to skip; want normal mode decision")
	}
}

func TestCyclicRefreshSegmentationConfigUsesAltLFUnderAggressiveDenoise(t *testing.T) {
	e := VP8Encoder{}
	e.rc.mode = RateControlCBR
	// Aggressive denoise (mode 3+) brings consec_zerolast=15 and qp_thresh=80.
	// Pick Q below qp_thresh and frames_since_key past 2*consec_zerolast=30.
	e.opts.NoiseSensitivity = 3
	e.rc.currentQuantizer = 40
	e.rc.framesSinceKeyframe = 100
	cfg := e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled {
		t.Fatalf("aggressive-denoise cyclic segmentation disabled, want enabled with alt-LF")
	}
	if cfg.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] {
		t.Fatalf("aggressive-denoise alt-Q feature still set, want suppressed in favour of alt-LF")
	}
	if !cfg.FeatureEnabled[vp8common.MBLvlAltLF][staticSegmentID] {
		t.Fatalf("aggressive-denoise alt-LF feature = false, want enabled")
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltLF][staticSegmentID]; got != -40 {
		t.Fatalf("aggressive-denoise alt-LF delta = %d, want libvpx -40", got)
	}

	// Q at or above qp_thresh: alt-Q path resumes.
	e.rc.currentQuantizer = 80
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if cfg.FeatureEnabled[vp8common.MBLvlAltLF][staticSegmentID] {
		t.Fatalf("Q>=qp_thresh alt-LF still set, want libvpx fallback to alt-Q delta")
	}
	if !cfg.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] {
		t.Fatalf("Q>=qp_thresh alt-Q feature = false, want enabled")
	}

	// Too soon after keyframe: alt-Q path resumes too.
	e.rc.currentQuantizer = 40
	e.rc.framesSinceKeyframe = 10
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if cfg.FeatureEnabled[vp8common.MBLvlAltLF][staticSegmentID] {
		t.Fatalf("frames_since_key<=2*consec_zerolast alt-LF still set, want fallback to alt-Q")
	}
	if !cfg.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] {
		t.Fatalf("frames_since_key<=2*consec_zerolast alt-Q feature = false, want enabled")
	}
}

func TestCyclicRefreshSegmentationConfigDisabledUnderForceMaxQuantizer(t *testing.T) {
	e := VP8Encoder{}
	e.rc.mode = RateControlCBR
	e.rc.currentQuantizer = 30
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("baseline CBR cyclic segmentation disabled, want enabled")
	}
	e.forceMaxQuantizer = true
	if cfg := e.cyclicRefreshSegmentationConfig(false); cfg.Enabled {
		t.Fatalf("force_maxqp cyclic segmentation = %+v, want disabled per libvpx force_maxqp gate", cfg)
	}
	e.forceMaxQuantizer = false
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("after clearing force_maxqp cyclic segmentation disabled, want enabled")
	}
}

func TestCyclicRefreshSegmentTransitionsClearOnNonZeroLast(t *testing.T) {
	// updateCyclicRefreshMapFromInterFrame is the per-MB segment-transition
	// recorder. After a frame:
	//   - Refreshed segment-1 MBs become -1 (cooldown).
	//   - Cooldown counters increment; ZEROMV-LAST flips a 1 to 0 (eligible).
	//   - Anything else sets the entry to 1 (dirty).
	refreshMap := []int8{-1, 1, 0, -1}
	modes := []vp8enc.InterFrameMacroblockMode{
		// MB0 was in segment 1 → final state -1
		{SegmentID: staticSegmentID, RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		// MB1 ZEROMV-LAST flips dirty→eligible
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		// MB2 NewMV last → dirty (1)
		{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV},
		// MB3 GOLDEN ZEROMV → dirty (1)
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
	}
	updateCyclicRefreshMapFromInterFrame(modes, refreshMap)
	want := []int8{-1, 0, 1, 1}
	for i := range want {
		if refreshMap[i] != want[i] {
			t.Fatalf("MB%d post-frame map = %d, want libvpx state %d", i, refreshMap[i], want[i])
		}
	}
}

// TestSetActiveMapOracleVectorPreservesEveryInactiveMB exercises a
// checkerboard active-map pattern and confirms libvpx's per-MB invariants
// across the whole frame: every inactive MB codes as ZEROMV-LAST with
// MBSkipCoeff=1 and segment 0, every inactive MB decodes back to the prior
// LAST reconstruction byte-for-byte, every active MB updates, and a second
// encode of the same source under the same active map is deterministic
// (decoder-stable). This is the active-map oracle vector for the
// single-threaded encodeframe path; govpx does not implement libvpx's
// row-threaded encodeframe loop so the threaded variant is N/A.
func TestSetActiveMapOracleVectorPreservesEveryInactiveMB(t *testing.T) {
	const width, height = 64, 64
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	first := testImage(width, height)
	second := testImage(width, height)
	fillImage(first, 60, 90, 170)
	fillImage(second, 200, 80, 180)

	// Checkerboard active map: ~half MBs inactive across the frame, including
	// boundary positions, so token-context resets at MB edges are exercised.
	activeMap := make([]byte, rows*cols)
	for row := range rows {
		for col := range cols {
			if (row+col)%2 == 0 {
				activeMap[row*cols+col] = 0
			} else {
				activeMap[row*cols+col] = 1
			}
		}
	}

	encodeRun := func() ([]Image, []vp8enc.InterFrameMacroblockMode) {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 30,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   1200,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
			Deadline:            DeadlineRealtime,
			CpuUsed:             8,
			KeyFrameInterval:    120,
		})
		if err != nil {
			t.Fatalf("NewVP8Encoder returned error: %v", err)
		}
		dst := make([]byte, 32*1024)
		key, err := e.EncodeInto(dst, first, 0, 1, 0)
		if err != nil {
			t.Fatalf("key EncodeInto returned error: %v", err)
		}
		keyData := append([]byte(nil), key.Data...)
		if err := e.SetActiveMap(activeMap, rows, cols); err != nil {
			t.Fatalf("SetActiveMap returned error: %v", err)
		}
		inter, err := e.EncodeInto(dst, second, 1, 1, 0)
		if err != nil {
			t.Fatalf("inter EncodeInto returned error: %v", err)
		}
		interData := append([]byte(nil), inter.Data...)
		modes := append([]vp8enc.InterFrameMacroblockMode(nil), e.interFrameModes[:rows*cols]...)
		return decodeFrameSequence(t, keyData, interData), modes
	}

	decoded, modes := encodeRun()
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			if activeMap[index] == 0 {
				m := modes[index]
				if m.RefFrame != vp8common.LastFrame || m.Mode != vp8common.ZeroMV || !m.MBSkipCoeff || m.SegmentID != 0 {
					t.Fatalf("inactive MB(%d,%d) mode = %+v, want skipped LAST/ZEROMV in segment 0", row, col, m)
				}
				if m.MV != (vp8enc.MotionVector{}) {
					t.Fatalf("inactive MB(%d,%d) MV = %+v, want zero", row, col, m.MV)
				}
				assertMacroblockEqual(t, "active-map oracle inactive", decoded[0], decoded[1], row, col)
			} else {
				assertMacroblockDifferent(t, "active-map oracle active", decoded[0], decoded[1], row, col)
			}
		}
	}

	// Determinism: a second encode of the same source under the same active
	// map yields decoder-equivalent output (per-MB pixels match exactly).
	decoded2, modes2 := encodeRun()
	if len(decoded2) != 2 {
		t.Fatalf("second decoded frame count = %d, want 2", len(decoded2))
	}
	for row := range rows {
		for col := range cols {
			index := row*cols + col
			if modes2[index].RefFrame != modes[index].RefFrame || modes2[index].Mode != modes[index].Mode || modes2[index].MBSkipCoeff != modes[index].MBSkipCoeff || modes2[index].SegmentID != modes[index].SegmentID {
				t.Fatalf("MB(%d,%d) modes diverged across runs: first=%+v second=%+v", row, col, modes[index], modes2[index])
			}
			assertMacroblockEqual(t, "active-map oracle determinism", decoded[1], decoded2[1], row, col)
		}
	}
}

func TestDenoiserModeMappingMatchesLibvpx(t *testing.T) {
	cases := []struct {
		level    int
		wantMode int
	}{
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 3},
		{4, 3},
		{5, 3},
		{6, 3},
	}
	for _, c := range cases {
		if got := denoiserModeForSensitivity(c.level); got != c.wantMode {
			t.Fatalf("noise_sensitivity %d -> mode %d, want %d", c.level, got, c.wantMode)
		}
	}
}

func TestDenoiserSetParametersMatchesLibvpxModes(t *testing.T) {
	for _, mode := range []int{1, 2} {
		kind, params := denoiserSetParameters(mode)
		if mode == 1 && kind != denoiserOnYOnly {
			t.Fatalf("mode=1 kind = %d, want denoiserOnYOnly", kind)
		}
		if mode == 2 && kind != denoiserOnYUV {
			t.Fatalf("mode=2 kind = %d, want denoiserOnYUV", kind)
		}
		if params.scaleSSEThresh != 1 || params.scaleMotionThresh != 8 || params.scaleIncreaseFilter != 0 || params.denoiseMVBias != 95 || params.pickmodeMVBias != 100 || params.qpThresh != 0 {
			t.Fatalf("non-aggressive params for mode=%d = %+v, want libvpx defaults", mode, params)
		}
	}
	kind, params := denoiserSetParameters(3)
	if kind != denoiserOnYUVAggressive {
		t.Fatalf("mode=3 kind = %d, want denoiserOnYUVAggressive", kind)
	}
	if params.scaleSSEThresh != 2 || params.scaleMotionThresh != 16 || params.scaleIncreaseFilter != 1 || params.denoiseMVBias != 95 && params.denoiseMVBias != 60 || params.pickmodeMVBias != 75 || params.qpThresh != 80 || params.consecZeroLast != 15 {
		t.Fatalf("aggressive params = %+v, want libvpx aggressive defaults", params)
	}
}

func TestDenoiserFilterYReturnsCopyForSharpDifference(t *testing.T) {
	// Pixels where source and mc_running_avg differ by huge amounts: filter
	// should return COPY_BLOCK (sum_diff above threshold and delta>=4).
	mc := make([]byte, 16*16)
	avg := make([]byte, 16*16)
	sig := make([]byte, 16*16)
	for i := range mc {
		mc[i] = 250
	}
	for i := range sig {
		sig[i] = 0
	}
	if got := denoiserFilterY(mc, 16, avg, 16, sig, 16, 0, false); got != denoiserCopyBlock {
		t.Fatalf("max-divergence filter decision = %d, want COPY_BLOCK", got)
	}
}

func TestDenoiserFilterYUsesMCWhenAbsdiffSmall(t *testing.T) {
	// |diff| <= 3 path: running_avg should be set to mc_running_avg, and the
	// filter should accept (FILTER_BLOCK).
	mc := make([]byte, 16*16)
	avg := make([]byte, 16*16)
	sig := make([]byte, 16*16)
	for i := range mc {
		mc[i] = 130
	}
	for i := range sig {
		sig[i] = 128
	}
	if got := denoiserFilterY(mc, 16, avg, 16, sig, 16, 0, false); got != denoiserFilterBlock {
		t.Fatalf("small-diff filter decision = %d, want FILTER_BLOCK", got)
	}
	for i := range avg {
		if avg[i] != 130 {
			t.Fatalf("avg[%d] = %d, want 130 (mc value taken when |diff|<=3)", i, avg[i])
		}
	}
}

func TestDenoiserFilterUVCopiesNearNeutralBlocks(t *testing.T) {
	// 8x8 block where chroma is near 128 across the board: libvpx returns
	// COPY without filtering because |sum_block - 128*64| < threshold.
	mc := make([]byte, 8*8)
	avg := make([]byte, 8*8)
	sig := make([]byte, 8*8)
	for i := range sig {
		sig[i] = 128
	}
	if got := denoiserFilterUV(mc, 8, avg, 8, sig, 8, 0, false); got != denoiserCopyBlock {
		t.Fatalf("near-neutral UV filter = %d, want COPY_BLOCK", got)
	}
}

func TestDenoiserPickmodeMVBiasReturns75ForAggressiveMode(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.opts.NoiseSensitivity = 0
	if got := e.denoiserPickmodeMVBias(); got != 100 {
		t.Fatalf("denoiser-off bias = %d, want 100", got)
	}
	e.opts.NoiseSensitivity = 2
	if got := e.denoiserPickmodeMVBias(); got != 100 {
		t.Fatalf("YUV mode bias = %d, want 100", got)
	}
	e.opts.NoiseSensitivity = 3
	if got := e.denoiserPickmodeMVBias(); got != 75 {
		t.Fatalf("aggressive bias = %d, want 75", got)
	}
}

func TestUpdateRefFrameProbsFromZeroReferenceMirrorsLibvpxConvertRFCT(t *testing.T) {
	modes := make([]vp8enc.InterFrameMacroblockMode, 4)
	fillZeroInterFrameModes(modes, vp8common.GoldenFrame)
	e := &VP8Encoder{
		interFrameModes: modes,
		refProbIntra:    63,
		refProbLast:     128,
		refProbGolden:   128,
	}

	e.updateRefFrameProbsFromAttempt(interFrameEncodeAttempt{ZeroReference: true})

	if e.refProbIntra != 1 || e.refProbLast != 1 || e.refProbGolden != 255 {
		t.Fatalf("zero-reference ref probs = %d/%d/%d, want libvpx RFCT 1/1/255",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}
}

func TestUpdateRefFrameProbsFromAttemptSkipsSingleLayerGFAndARFRefresh(t *testing.T) {
	modes := []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
	}
	e := &VP8Encoder{
		interFrameModes: modes,
		refProbIntra:    63,
		refProbLast:     99,
		refProbGolden:   77,
	}

	e.updateRefFrameProbsFromAttempt(interFrameEncodeAttempt{Config: vp8enc.InterFrameStateConfig{RefreshGolden: true}})
	if e.refProbIntra != 63 || e.refProbLast != 99 || e.refProbGolden != 77 {
		t.Fatalf("single-layer GF refresh converted refs = %d/%d/%d, want unchanged 63/99/77",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}

	e.updateRefFrameProbsFromAttempt(interFrameEncodeAttempt{Config: vp8enc.InterFrameStateConfig{RefreshAltRef: true}})
	if e.refProbIntra != 63 || e.refProbLast != 99 || e.refProbGolden != 77 {
		t.Fatalf("single-layer ARF refresh converted refs = %d/%d/%d, want unchanged 63/99/77",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}
}

func TestUpdateRefFrameProbsFromAttemptConvertsTemporalLayerRefresh(t *testing.T) {
	modes := []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
	}
	e := &VP8Encoder{
		opts:            EncoderOptions{TemporalScalability: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}},
		interFrameModes: modes,
		refProbIntra:    63,
		refProbLast:     99,
		refProbGolden:   77,
	}

	e.updateRefFrameProbsFromAttempt(interFrameEncodeAttempt{Config: vp8enc.InterFrameStateConfig{RefreshGolden: true}})

	if e.refProbIntra != 1 || e.refProbLast != 255 || e.refProbGolden != 128 {
		t.Fatalf("temporal GF refresh ref probs = %d/%d/%d, want libvpx RFCT 1/255/128",
			e.refProbIntra, e.refProbLast, e.refProbGolden)
	}
}

// TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxAltRefRefresh verifies the
// alt-ref-refresh branch of vp8/encoder/onyx_if.c update_rd_ref_frame_probs:
// prob_intra is bumped by 40 (clamped to 255), prob_last forced to 200, and
// prob_gf set to 1 only if source_alt_ref_active is true at RD time;
// otherwise the trailing `if (!source_alt_ref_active) prob_gf = 255` clamps
// prob_gf to 255 (since the alt-ref refresh transitions the flag *after* the
// frame's RD).
func TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxAltRefRefresh(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128}
	e.applyRdRefFrameProbHeuristics(true)
	if e.refProbIntra != 103 {
		t.Fatalf("alt-ref refresh prob_intra = %d, want 63+40=103", e.refProbIntra)
	}
	if e.refProbLast != 200 {
		t.Fatalf("alt-ref refresh prob_last = %d, want 200", e.refProbLast)
	}
	// source_alt_ref_active was false before this frame, so the trailing
	// libvpx override clamps prob_gf to 255.
	if e.refProbGolden != 255 {
		t.Fatalf("alt-ref refresh prob_gf = %d, want 255 (trailing clamp)", e.refProbGolden)
	}

	// When alt-ref was already active, the trailing clamp does not fire and
	// prob_gf stays at the heuristic-set 1.
	e2 := &VP8Encoder{refProbIntra: 230, refProbLast: 128, refProbGolden: 128, sourceAltRefActive: true}
	e2.applyRdRefFrameProbHeuristics(true)
	if e2.refProbIntra != 255 {
		t.Fatalf("alt-ref refresh prob_intra clamp = %d, want 255", e2.refProbIntra)
	}
	if e2.refProbGolden != 1 {
		t.Fatalf("alt-ref refresh prob_gf = %d, want 1", e2.refProbGolden)
	}
}

// TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxFramesSinceGolden verifies
// the frames_since_golden==0 / ==1 branches of update_rd_ref_frame_probs.
func TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxFramesSinceGolden(t *testing.T) {
	// frames_since_golden == 0: prob_last=214; trailing clamp forces
	// prob_gf=255 because source_alt_ref_active is false.
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128, framesSinceGolden: 0}
	e.applyRdRefFrameProbHeuristics(false)
	if e.refProbLast != 214 {
		t.Fatalf("frames_since_golden=0 prob_last = %d, want 214", e.refProbLast)
	}
	if e.refProbGolden != 255 {
		t.Fatalf("frames_since_golden=0 prob_gf = %d, want 255 (trailing clamp)", e.refProbGolden)
	}

	// frames_since_golden == 1: prob_last=192, prob_gf=220, but the trailing
	// clamp overrides prob_gf to 255 when source_alt_ref_active is false.
	e = &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128, framesSinceGolden: 1}
	e.applyRdRefFrameProbHeuristics(false)
	if e.refProbLast != 192 {
		t.Fatalf("frames_since_golden=1 prob_last = %d, want 192", e.refProbLast)
	}
	if e.refProbGolden != 255 {
		t.Fatalf("frames_since_golden=1 prob_gf = %d, want 255 (trailing clamp)", e.refProbGolden)
	}

	// frames_since_golden == 1, source_alt_ref_active=true: prob_gf stays at
	// the heuristic 220.
	e = &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 128, framesSinceGolden: 1, sourceAltRefActive: true}
	e.applyRdRefFrameProbHeuristics(false)
	if e.refProbGolden != 220 {
		t.Fatalf("alt-ref active frames_since_golden=1 prob_gf = %d, want 220", e.refProbGolden)
	}
}

// TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxAltRefActiveDecay verifies
// the source_alt_ref_active branch (frames_since_golden>=2): prob_gf decays
// by 20 per frame down to a floor of 10.
func TestApplyRdRefFrameProbHeuristicsMirrorsLibvpxAltRefActiveDecay(t *testing.T) {
	e := &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 100, framesSinceGolden: 5, sourceAltRefActive: true}
	e.applyRdRefFrameProbHeuristics(false)
	if e.refProbGolden != 80 {
		t.Fatalf("alt-ref active decay prob_gf = %d, want 100-20=80", e.refProbGolden)
	}

	// Floor clamp at 10.
	e = &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 15, framesSinceGolden: 5, sourceAltRefActive: true}
	e.applyRdRefFrameProbHeuristics(false)
	if e.refProbGolden != 10 {
		t.Fatalf("alt-ref active decay floor prob_gf = %d, want 10", e.refProbGolden)
	}

	// frames_since_golden>=2 with source_alt_ref_active=false: no branch
	// matched, trailing clamp sets prob_gf=255.
	e = &VP8Encoder{refProbIntra: 63, refProbLast: 128, refProbGolden: 100, framesSinceGolden: 5, sourceAltRefActive: false}
	e.applyRdRefFrameProbHeuristics(false)
	if e.refProbGolden != 255 {
		t.Fatalf("inactive alt-ref far-from-golden prob_gf = %d, want 255", e.refProbGolden)
	}
}

// TestUpdateGoldenFrameStatsMirrorsLibvpxCounter verifies the lifecycle of
// frames_since_golden / source_alt_ref_active matches libvpx's
// update_alt_ref_frame_stats / update_golden_frame_stats: refreshing alt-ref
// resets frames_since_golden and sets source_alt_ref_active=true; refreshing
// golden resets frames_since_golden and clears source_alt_ref_active; plain
// inter frames increment the counter.
func TestUpdateGoldenFrameStatsMirrorsLibvpxCounter(t *testing.T) {
	e := &VP8Encoder{}

	// Plain inter frame increments frames_since_golden.
	e.updateGoldenFrameStats(false, false)
	if e.framesSinceGolden != 1 || e.sourceAltRefActive {
		t.Fatalf("plain inter -> {framesSinceGolden:%d sourceAltRefActive:%v}, want {1 false}",
			e.framesSinceGolden, e.sourceAltRefActive)
	}
	e.updateGoldenFrameStats(false, false)
	if e.framesSinceGolden != 2 {
		t.Fatalf("two plain inters frames_since_golden = %d, want 2", e.framesSinceGolden)
	}

	// Refresh alt-ref: counter resets, alt-ref becomes active.
	e.updateGoldenFrameStats(false, true)
	if e.framesSinceGolden != 0 || !e.sourceAltRefActive {
		t.Fatalf("alt-ref refresh -> {%d %v}, want {0 true}", e.framesSinceGolden, e.sourceAltRefActive)
	}

	// Plain inter after alt-ref keeps alt-ref active and increments counter.
	e.updateGoldenFrameStats(false, false)
	if e.framesSinceGolden != 1 || !e.sourceAltRefActive {
		t.Fatalf("post-altref inter -> {%d %v}, want {1 true}", e.framesSinceGolden, e.sourceAltRefActive)
	}

	// Refresh golden: counter resets, alt-ref active clears (no auto-arf
	// pending in govpx).
	e.updateGoldenFrameStats(true, false)
	if e.framesSinceGolden != 0 || e.sourceAltRefActive {
		t.Fatalf("golden refresh -> {%d %v}, want {0 false}", e.framesSinceGolden, e.sourceAltRefActive)
	}
}

// TestResetGoldenFrameStatsMirrorsLibvpxKeyFrameBranch pins
// `resetGoldenFrameStats` to the libvpx
// `update_golden_frame_stats(refresh_golden_frame=1)` keyframe branch in
// vp8/encoder/onyx_if.c. Two regimes are exercised:
//
//  1. No ARF schedule armed: source_alt_ref_active is zeroed (libvpx
//     `if (!cpi->source_alt_ref_pending) cpi->source_alt_ref_active = 0`),
//     frames_since_golden is reset, and frames_till_gf_update_due is
//     decremented (libvpx `if (frames_till_gf_update_due > 0)
//     frames_till_gf_update_due--`).
//
//  2. ARF schedule armed during the keyframe's vp8_second_pass call:
//     source_alt_ref_pending and alt_ref_source are preserved so that the
//     next vp8_get_compressed_data ARF block can fire; only
//     frames_till_alt_ref_frame decrements per the libvpx update.
func TestResetGoldenFrameStatsMirrorsLibvpxKeyFrameBranch(t *testing.T) {
	t.Run("no-arf-schedule", func(t *testing.T) {
		e := &VP8Encoder{
			framesSinceGolden:     7,
			sourceAltRefActive:    true,
			sourceAltRefPending:   false,
			altRefSourceValid:     false,
			framesTillAltRefFrame: 5,
		}
		e.resetGoldenFrameStats()
		if e.framesSinceGolden != 0 || e.sourceAltRefActive ||
			e.framesTillAltRefFrame != 4 {
			t.Fatalf("post-keyframe state = {fsg:%d active:%v till:%d}, want {0 false 4}",
				e.framesSinceGolden, e.sourceAltRefActive, e.framesTillAltRefFrame)
		}
	})
	t.Run("preserves-arf-schedule", func(t *testing.T) {
		e := &VP8Encoder{
			framesSinceGolden:     3,
			sourceAltRefActive:    true,
			sourceAltRefPending:   true,
			altRefSourceValid:     true,
			altRefSourcePTS:       1234,
			framesTillAltRefFrame: 7,
		}
		e.resetGoldenFrameStats()
		if e.framesSinceGolden != 0 {
			t.Fatalf("framesSinceGolden = %d, want 0", e.framesSinceGolden)
		}
		if !e.sourceAltRefActive {
			t.Fatalf("sourceAltRefActive cleared while pending=true; libvpx only zeroes active when !pending")
		}
		if !e.sourceAltRefPending || !e.altRefSourceValid || e.altRefSourcePTS != 1234 {
			t.Fatalf("ARF schedule mutated: pending=%v valid=%v pts=%d, want true/true/1234",
				e.sourceAltRefPending, e.altRefSourceValid, e.altRefSourcePTS)
		}
		if e.framesTillAltRefFrame != 6 {
			t.Fatalf("framesTillAltRefFrame = %d, want 6 (decremented from 7)", e.framesTillAltRefFrame)
		}
	})
}

// TestClearAltRefScheduleDropsPendingState pins the lifecycle reset path used
// from Reset()/encoder init: dropping any in-flight ARF schedule entirely so
// that no leftover pending state survives into a fresh stream.
func TestClearAltRefScheduleDropsPendingState(t *testing.T) {
	e := &VP8Encoder{
		sourceAltRefPending:   true,
		altRefSourceValid:     true,
		altRefSourcePTS:       42,
		framesTillAltRefFrame: 5,
	}
	e.clearAltRefSchedule()
	if e.sourceAltRefPending || e.altRefSourceValid || e.framesTillAltRefFrame != 0 {
		t.Fatalf("post-clear state = {pending:%v valid:%v till:%d}, want {false false 0}",
			e.sourceAltRefPending, e.altRefSourceValid, e.framesTillAltRefFrame)
	}
}

// TestScheduleAltRefSourceArmsPendingFlagAndPTS pins the libvpx
// `cpi->source_alt_ref_pending = 1; cpi->alt_ref_source = source` set
// inside vp8_get_compressed_data: scheduling the ARF arms the pending
// flag, records the PTS, and primes frames_till_alt_ref_frame.
func TestScheduleAltRefSourceArmsPendingFlagAndPTS(t *testing.T) {
	var e VP8Encoder
	e.scheduleAltRefSource(1234, 7)
	if !e.sourceAltRefPending {
		t.Fatalf("sourceAltRefPending after schedule = false, want true")
	}
	if !e.altRefSourceValid || e.altRefSourcePTS != 1234 {
		t.Fatalf("altRefSourcePTS = %d valid=%v, want 1234 valid=true",
			e.altRefSourcePTS, e.altRefSourceValid)
	}
	if e.framesTillAltRefFrame != 7 {
		t.Fatalf("framesTillAltRefFrame = %d, want 7", e.framesTillAltRefFrame)
	}
}

// TestIsSrcFrameAltRefMatchesScheduledPTS pins the libvpx
// is_src_frame_alt_ref = (alt_ref_source != NULL && source ==
// alt_ref_source) check.
func TestIsSrcFrameAltRefMatchesScheduledPTS(t *testing.T) {
	var e VP8Encoder
	if e.isSrcFrameAltRef(1234) {
		t.Fatalf("unscheduled frame should not match")
	}
	e.scheduleAltRefSource(1234, 7)
	if !e.isSrcFrameAltRef(1234) {
		t.Fatalf("scheduled PTS should match")
	}
	if e.isSrcFrameAltRef(9999) {
		t.Fatalf("non-matching PTS should not be ARF source")
	}
}

// TestUpdateGoldenFrameStatsCountsDownAltRefFrame pins the libvpx
// `if (cpi->frames_till_alt_ref_frame) cpi->frames_till_alt_ref_frame--`
// counter.
func TestUpdateGoldenFrameStatsCountsDownAltRefFrame(t *testing.T) {
	var e VP8Encoder
	e.scheduleAltRefSource(1234, 3)
	e.updateGoldenFrameStats(false, false)
	if e.framesTillAltRefFrame != 2 {
		t.Fatalf("frames_till_alt_ref_frame after first inter = %d, want 2", e.framesTillAltRefFrame)
	}
	e.updateGoldenFrameStats(false, false)
	e.updateGoldenFrameStats(false, false)
	if e.framesTillAltRefFrame != 0 {
		t.Fatalf("frames_till_alt_ref_frame after 3 inters = %d, want 0", e.framesTillAltRefFrame)
	}
	// Counter should not go negative.
	e.updateGoldenFrameStats(false, false)
	if e.framesTillAltRefFrame != 0 {
		t.Fatalf("frames_till_alt_ref_frame after underflow = %d, want 0 floor", e.framesTillAltRefFrame)
	}
}

// TestUpdateGoldenFrameStatsAltRefRefreshClearsPending pins the libvpx
// update_alt_ref_frame_stats branch: a successful ARF refresh consumes
// the pending flag.
func TestUpdateGoldenFrameStatsAltRefRefreshClearsPending(t *testing.T) {
	e := &VP8Encoder{sourceAltRefPending: true, framesTillAltRefFrame: 2}
	e.updateGoldenFrameStats(false, true)
	if e.sourceAltRefPending {
		t.Fatalf("sourceAltRefPending after ARF refresh = true, want false (consumed)")
	}
	if !e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive after ARF refresh = false, want true")
	}
}

// TestUpdateGoldenFrameStatsGoldenRefreshKeepsActiveOnPending pins the
// libvpx `if (!source_alt_ref_pending) source_alt_ref_active = 0`
// branch: when an ARF is still pending, refreshing GOLDEN does not
// clear the active flag.
func TestUpdateGoldenFrameStatsGoldenRefreshKeepsActiveOnPending(t *testing.T) {
	e := &VP8Encoder{sourceAltRefActive: true, sourceAltRefPending: true}
	e.updateGoldenFrameStats(true, false)
	if !e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive after GF refresh with ARF pending = false, want true (gated)")
	}
}

func TestEncodeIntoAltRefSignBiasFollowsLibvpxSourceAltRefActive(t *testing.T) {
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
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	keySrc := testImage(16, 16)
	altSrc := testImage(16, 16)
	interSrc := testImage(16, 16)
	fillImage(keySrc, 220, 90, 170)
	fillImage(altSrc, 40, 91, 171)
	fillImage(interSrc, 60, 92, 172)
	dst := make([]byte, 4096)

	if _, err := e.EncodeInto(dst, keySrc, 0, 1, EncodeForceKeyFrame); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	altRefresh, err := e.EncodeInto(dst, altSrc, 1, 1, EncodeInvisibleFrame|EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden)
	if err != nil {
		t.Fatalf("alt refresh EncodeInto returned error: %v", err)
	}
	altState := packetState(t, altRefresh.Data)
	if altState.Refresh.AltRefSignBias {
		t.Fatalf("alt-refresh frame AltRefSignBias = true, want false before update_alt_ref_frame_stats activates ALTREF")
	}
	if !e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive = false after ALTREF refresh, want true")
	}
	if len(altRefresh.Data) == 0 {
		t.Fatalf("alt refresh wrote no packet data")
	}

	inter, err := e.EncodeInto(dst, interSrc, 2, 1, 0)
	if err != nil {
		t.Fatalf("post-altref inter EncodeInto returned error: %v", err)
	}
	interState := packetState(t, inter.Data)
	if !interState.Refresh.AltRefSignBias || interState.Refresh.GoldenSignBias {
		t.Fatalf("post-altref sign bias = golden:%v alt:%v, want golden:false alt:true", interState.Refresh.GoldenSignBias, interState.Refresh.AltRefSignBias)
	}

	golden, err := e.EncodeInto(dst, interSrc, 3, 1, EncodeForceGoldenFrame)
	if err != nil {
		t.Fatalf("golden refresh EncodeInto returned error: %v", err)
	}
	goldenState := packetState(t, golden.Data)
	if !goldenState.Refresh.AltRefSignBias {
		t.Fatalf("golden-refresh frame AltRefSignBias = false, want true while ALTREF was active for this frame")
	}
	if e.sourceAltRefActive {
		t.Fatalf("sourceAltRefActive = true after GOLDEN refresh, want false")
	}
}

// TestSignBiasEvolutionMatchesLibvpxAcrossGFAndARF drives a 12-frame sequence
// with AutoAltRef enabled so the encoder produces a key frame, several inter
// frames, a hidden ARF refresh, the matching deferred show frame, more inter
// frames, and a forced GOLDEN refresh. For each emitted packet it parses the
// (golden_sign_bias, altref_sign_bias) header bits and asserts they match the
// libvpx evolution rule out of vp8/encoder/onyx_if.c:
//
//   - GOLDEN sign bias is always 0: update_golden_frame_stats never flips
//     ref_frame_sign_bias[GOLDEN_FRAME].
//   - ALTREF sign bias at frame N equals cpi->source_alt_ref_active as seen
//     ENTERING frame N. update_alt_ref_frame_stats sets source_alt_ref_active
//     AFTER the hidden ARF refresh, so the refresh frame itself encodes the
//     prior bias (false). The first show frame after the hidden ARF then
//     encodes (false, true). update_golden_frame_stats clears the active
//     flag on a GOLDEN refresh ONLY if no ARF is pending; the GOLDEN refresh
//     frame still encodes the prior bias because the clear runs AFTER pack.
//
// The expected per-packet tuple is derived by replaying libvpx's two stat
// updates against each packet's RefreshAltRef / RefreshGolden bits, so any
// drift between govpx's interFrameSignBias() / updateGoldenFrameStats() and
// libvpx's update_alt_ref_frame_stats / update_golden_frame_stats surfaces
// here as a per-frame tuple mismatch with the failing frame index pinned.
func TestSignBiasEvolutionMatchesLibvpxAcrossGFAndARF(t *testing.T) {
	e := newAutoAltRefTestEncoder(t)
	const frameCount = 12
	const width = 32
	const height = 32
	dst := make([]byte, 1<<16)
	type emittedFrame struct {
		index      int
		pts        uint64
		key        bool
		show       bool
		refresh    vp8dec.RefreshHeader
		forcedGold bool
	}
	emitted := make([]emittedFrame, 0, frameCount+8)
	pushPacket := func(idx int, pts uint64, data []byte, forcedGold bool) {
		t.Helper()
		hdr, err := vp8dec.ParseFrameHeader(data)
		if err != nil {
			t.Fatalf("ParseFrameHeader frame %d (pts=%d): %v", idx, pts, err)
		}
		state := parseEncoderStateHeader(t, data)
		emitted = append(emitted, emittedFrame{
			index:      idx,
			pts:        pts,
			key:        hdr.KeyFrame(),
			show:       hdr.ShowFrame,
			refresh:    state.Refresh,
			forcedGold: forcedGold,
		})
	}
	// Drive frameCount source frames; force a GOLDEN refresh on frame 10 so
	// the evolution covers a forced GF refresh AFTER the auto-ARF has
	// activated (libvpx's "GOLDEN refresh while ALTREF active" branch). The
	// auto-ARF driver's hidden ARF and matching deferred show frame are
	// scheduled naturally by the lookahead during the early frames.
	for i := range frameCount {
		img := movingBarTestImage(width, height, i)
		var flags EncodeFlags
		forced := false
		if i == 10 {
			flags = EncodeForceGoldenFrame
			forced = true
		}
		result, err := e.EncodeInto(dst, img, uint64(i)*1000, 1000, flags)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				continue
			}
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		pushPacket(i, result.PTS, append([]byte(nil), result.Data...), forced)
	}
	for {
		result, err := e.FlushInto(dst)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				break
			}
			t.Fatalf("FlushInto: %v", err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		pushPacket(-1, result.PTS, append([]byte(nil), result.Data...), false)
	}
	if len(emitted) == 0 {
		t.Fatalf("no packets emitted")
	}
	// The test only buys parity coverage if at least one hidden ARF, one
	// deferred show frame, and one forced GOLDEN refresh actually fire in
	// the captured stream.
	hiddenSeen := false
	deferredShowSeen := false
	goldenRefreshSeen := false
	for i, p := range emitted {
		if !p.key && !p.show && p.refresh.RefreshAltRef {
			hiddenSeen = true
			// The deferred show frame is the next visible non-key packet.
			for j := i + 1; j < len(emitted); j++ {
				if !emitted[j].key && emitted[j].show {
					deferredShowSeen = true
					break
				}
			}
		}
		if !p.key && p.refresh.RefreshGolden && !p.refresh.RefreshAltRef {
			goldenRefreshSeen = true
		}
	}
	if !hiddenSeen {
		t.Fatalf("expected at least one hidden ARF in the captured stream; got %d packets", len(emitted))
	}
	if !deferredShowSeen {
		t.Fatalf("expected at least one deferred show frame after the hidden ARF")
	}
	if !goldenRefreshSeen {
		t.Fatalf("expected at least one GOLDEN refresh in the captured stream")
	}
	// Replay libvpx's per-frame sign-bias derivation against each packet.
	// State entering frame N is (active, pending). For each packet:
	//   1. Expected bias = (false, active) — the libvpx onyx_if.c
	//      pre-pack write at line 3397-3401 reads source_alt_ref_active
	//      and never flips GOLDEN.
	//   2. Update active/pending using update_alt_ref_frame_stats /
	//      update_golden_frame_stats semantics for the refresh bits in the
	//      packet (and reset to (false,false) on a key frame).
	active := false
	pending := false
	for i, p := range emitted {
		var wantGolden, wantAltRef bool
		if p.key {
			// Key frame's RefreshHeader has no sign-bias bits, so the
			// decoder leaves them as the zero value. After the key
			// frame libvpx clears source_alt_ref_active /
			// source_alt_ref_pending in resetGoldenFrameStats.
			wantGolden = false
			wantAltRef = false
		} else {
			wantGolden = false
			wantAltRef = active
		}
		gotGolden := p.refresh.GoldenSignBias
		gotAltRef := p.refresh.AltRefSignBias
		if gotGolden != wantGolden || gotAltRef != wantAltRef {
			t.Fatalf("packet %d (src=%d pts=%d key=%v show=%v refLast=%v refGold=%v refARF=%v forcedGold=%v) sign-bias = (golden=%v, altref=%v), want (golden=%v, altref=%v); state entering frame: active=%v pending=%v",
				i, p.index, p.pts, p.key, p.show,
				p.refresh.RefreshLast, p.refresh.RefreshGolden, p.refresh.RefreshAltRef,
				p.forcedGold,
				gotGolden, gotAltRef, wantGolden, wantAltRef,
				active, pending)
		}
		// Advance (active, pending) using the libvpx update rules.
		if p.key {
			active = false
			pending = false
			continue
		}
		if p.refresh.RefreshAltRef {
			// update_alt_ref_frame_stats: clears pending, sets active.
			active = true
			pending = false
			continue
		}
		if p.refresh.RefreshGolden {
			// update_golden_frame_stats: when no ARF is pending the
			// active flag clears; when one is pending it stays.
			if !pending {
				active = false
			}
		}
		// Non-refresh inter frames leave (active, pending) unchanged for
		// the purposes of the sign-bias derivation. govpx's auto-ARF
		// driver may set pending later via scheduleAltRefSource, but
		// pending alone never affects ref_frame_sign_bias[ALTREF_FRAME];
		// only update_alt_ref_frame_stats does, and that runs on a
		// hidden ARF commit.
		_ = pending
	}
}
