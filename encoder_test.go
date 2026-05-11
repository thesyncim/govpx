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
