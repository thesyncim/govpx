package govpx

import (
	"errors"
	"testing"
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

	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, RateControlMode: RateControlQ, TargetBitrateKbps: 1200, MinQuantizer: 4, MaxQuantizer: 56, CQLevel: 64})
	if !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("Q CQ level error = %v, want ErrInvalidQuantizer", err)
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

	_, err = NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, Tuning: Tuning(2)})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("tuning error = %v, want ErrInvalidConfig", err)
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

	// newTestEncoder constructs a 16x16/30fps@1200kbps encoder. libvpx
	// caps the *internal* target_bandwidth at the raw-24bpp envelope
	// (Width*Height*8*3*fps/1000 = 184 kbps for 16x16/30fps), so the
	// per-frame budget is 184_000/30 = 6133 bits/frame, not the
	// 1200kbps-derived 40000 bits/frame the original test asserted.
	// libvpxClampToRawTargetRate mirrors that clamp; the user-facing
	// targetBitrateKbps is still 1200.
	if e.rc.targetBitrateKbps != 1200 {
		t.Fatalf("targetBitrateKbps = %d, want user-facing 1200", e.rc.targetBitrateKbps)
	}
	if e.rc.bitsPerFrame != 6133 {
		t.Fatalf("bitsPerFrame = %d, want raw-capped 6133", e.rc.bitsPerFrame)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FPS: 60}); err != nil {
		t.Fatalf("SetRealtimeTarget returned error: %v", err)
	}
	// At 60fps the raw-target-rate cap rises to 16*16*8*3*60/1000 = 368
	// kbps, still below the requested 1200 kbps so the cap clips.
	// 368_000 / 60 = 6133 bits/frame (matches libvpx's truncation).
	if e.rc.bitsPerFrame != 6133 {
		t.Fatalf("60fps bitsPerFrame = %d, want raw-capped 6133", e.rc.bitsPerFrame)
	}
	if err := e.SetBitrateKbps(600); err != nil {
		t.Fatalf("SetBitrateKbps returned error: %v", err)
	}
	// 600 kbps requested but cap is still 368 kbps, so the per-frame
	// budget stays at the cap.
	if e.rc.bitsPerFrame != 6133 {
		t.Fatalf("post-SetBitrateKbps bitsPerFrame = %d, want raw-capped 6133", e.rc.bitsPerFrame)
	}
}

func TestSetRealtimeTargetFPSChangeResetsAutospeedTiming(t *testing.T) {
	e := newTestEncoder(t)
	e.autoSpeed = 12
	e.avgPickModeTime = 9000
	e.avgEncodeTime = 18000
	e.autoSpeedFrameStartNS = 12345

	if err := e.SetRealtimeTarget(RealtimeTarget{FPS: 60}); err != nil {
		t.Fatalf("SetRealtimeTarget returned error: %v", err)
	}

	if e.autoSpeed != 0 || e.avgPickModeTime != 0 || e.avgEncodeTime != 0 || e.autoSpeedFrameStartNS != 0 {
		t.Fatalf("autospeed state = speed:%d pick:%d encode:%d start:%d, want reset", e.autoSpeed, e.avgPickModeTime, e.avgEncodeTime, e.autoSpeedFrameStartNS)
	}
	if got := e.libvpxCPUUsed(); got != 4 {
		t.Fatalf("cold-start libvpxCPUUsed = %d, want 4", got)
	}
}

func TestSetRealtimeTargetSameFPSKeepsAutospeedTiming(t *testing.T) {
	e := newTestEncoder(t)
	e.autoSpeed = 8
	e.avgPickModeTime = 7000
	e.avgEncodeTime = 14000
	e.autoSpeedFrameStartNS = 12345

	if err := e.SetRealtimeTarget(RealtimeTarget{FPS: e.opts.FPS}); err != nil {
		t.Fatalf("SetRealtimeTarget returned error: %v", err)
	}

	if e.autoSpeed != 8 || e.avgPickModeTime != 7000 || e.avgEncodeTime != 14000 || e.autoSpeedFrameStartNS != 12345 {
		t.Fatalf("autospeed state changed on same-FPS target: speed:%d pick:%d encode:%d start:%d", e.autoSpeed, e.avgPickModeTime, e.avgEncodeTime, e.autoSpeedFrameStartNS)
	}
}

func TestSetRealtimeTargetFrameDropMode(t *testing.T) {
	e := newTestEncoder(t)
	e.rc.dropFrameAllowed = true
	e.rc.dropFramesWaterMark = 75
	e.opts.DropFrameAllowed = true
	e.opts.DropFrameWaterMark = 75

	if err := e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 900}); err != nil {
		t.Fatalf("bitrate-only SetRealtimeTarget returned error: %v", err)
	}
	if !e.rc.dropFrameAllowed || e.rc.dropFramesWaterMark != 75 {
		t.Fatalf("bitrate-only frame drop = allowed:%t mark:%d, want preserved true/75",
			e.rc.dropFrameAllowed, e.rc.dropFramesWaterMark)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropDisabled}); err != nil {
		t.Fatalf("disable frame drop returned error: %v", err)
	}
	if e.rc.dropFrameAllowed || e.opts.DropFrameAllowed || e.rc.dropFramesWaterMark != 0 {
		t.Fatalf("disabled frame drop = rc:%t opts:%t mark:%d, want false/false/0",
			e.rc.dropFrameAllowed, e.opts.DropFrameAllowed, e.rc.dropFramesWaterMark)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropEnabled}); err != nil {
		t.Fatalf("enable frame drop returned error: %v", err)
	}
	if !e.rc.dropFrameAllowed || !e.opts.DropFrameAllowed || e.rc.dropFramesWaterMark != 75 {
		t.Fatalf("enabled frame drop = rc:%t opts:%t mark:%d, want true/true/75",
			e.rc.dropFrameAllowed, e.opts.DropFrameAllowed, e.rc.dropFramesWaterMark)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropMode(99)}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid frame drop mode error = %v, want ErrInvalidConfig", err)
	}
}

func TestSetFrameDropAllowed(t *testing.T) {
	e := newTestEncoder(t)

	if err := e.SetFrameDropAllowed(false); err != nil {
		t.Fatalf("SetFrameDropAllowed(false) returned error: %v", err)
	}
	if e.rc.dropFrameAllowed || e.opts.DropFrameAllowed || e.rc.dropFramesWaterMark != 0 {
		t.Fatalf("disabled frame drop = rc:%t opts:%t mark:%d, want false/false/0",
			e.rc.dropFrameAllowed, e.opts.DropFrameAllowed, e.rc.dropFramesWaterMark)
	}
	e.opts.DropFrameWaterMark = 0
	if err := e.SetFrameDropAllowed(true); err != nil {
		t.Fatalf("SetFrameDropAllowed(true) returned error: %v", err)
	}
	if !e.rc.dropFrameAllowed || !e.opts.DropFrameAllowed || e.rc.dropFramesWaterMark != defaultDropFramesWaterMark {
		t.Fatalf("enabled frame drop = rc:%t opts:%t mark:%d, want true/true/%d",
			e.rc.dropFrameAllowed, e.opts.DropFrameAllowed, e.rc.dropFramesWaterMark, defaultDropFramesWaterMark)
	}
}
