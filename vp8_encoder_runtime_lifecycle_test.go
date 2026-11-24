package govpx

import (
	"errors"
	"testing"
)

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

func TestSetRealtimeTargetSameSizePreservesDimensions(t *testing.T) {
	e := newTestEncoder(t)

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

func TestRuntimeFixedKeyFrameIntervalZeroMirrorsLibvpx(t *testing.T) {
	e := VP8Encoder{opts: EncoderOptions{KeyFrameInterval: 0}}
	if got := e.applyFixedKeyFrameIntervalFlag(0); got&EncodeForceKeyFrame == 0 {
		t.Fatalf("fixed interval 0 flags = %v, want EncodeForceKeyFrame", got)
	}
	if e.fixedKeyFrameCounter != 1 {
		t.Fatalf("fixedKeyFrameCounter = %d, want 1", e.fixedKeyFrameCounter)
	}

	e = VP8Encoder{opts: EncoderOptions{KeyFrameInterval: 0}, keyFramesDisabled: true}
	if got := e.applyFixedKeyFrameIntervalFlag(0); got&EncodeForceKeyFrame != 0 {
		t.Fatalf("disabled fixed interval 0 flags = %v, want no force keyframe", got)
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
