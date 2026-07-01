package govpx

import "testing"

func TestNewVP8EncoderDefaultsARNRTypeToLibvpxCentered(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{Width: 640, Height: 480, FPS: 30, TargetBitrateKbps: 1200, NoiseSensitivity: 6, ARNRMaxFrames: 15})
	if err != nil {
		t.Fatalf("libvpx high denoise/ARNR bounds returned error: %v", err)
	}
	if e.opts.ARNRType != 3 {
		t.Fatalf("default ARNR type = %d, want libvpx centered type 3", e.opts.ARNRType)
	}
}

func TestVP8EncoderEncodeIntoAdvancesFrameCount(t *testing.T) {
	e := newTestEncoder(t)
	if _, err := e.EncodeInto(make([]byte, 4096), testImage(16, 16), 22, 3, 0); err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if e.frameCount != 1 {
		t.Fatalf("frameCount = %d, want 1", e.frameCount)
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

func TestSetRealtimeTargetFPSChangePreservesAutospeedTiming(t *testing.T) {
	e := newTestEncoder(t)
	e.autoSpeed = 12
	e.avgPickModeTime = 9000
	e.avgEncodeTime = 18000
	e.autoSpeedFrameStartNS = 12345

	if err := e.SetRealtimeTarget(RealtimeTarget{FPS: 60}); err != nil {
		t.Fatalf("SetRealtimeTarget returned error: %v", err)
	}

	// libvpx routes fps changes through vp8_change_config, which reseeds
	// cpi->Speed = oxcf.cpu_used but preserves avg_pick_mode_time /
	// avg_encode_time. applyChangeConfigSpeedReset mirrors that tail.
	if e.autoSpeed != e.opts.CpuUsed || e.avgPickModeTime != 9000 || e.avgEncodeTime != 18000 || e.autoSpeedFrameStartNS != 12345 {
		t.Fatalf("autospeed state = speed:%d pick:%d encode:%d start:%d, want speed=%d pick=9000 encode=18000 start=12345",
			e.autoSpeed, e.avgPickModeTime, e.avgEncodeTime, e.autoSpeedFrameStartNS, e.opts.CpuUsed)
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

func TestSetCPUUsedPreservesRuntimePickerState(t *testing.T) {
	e := newTestEncoder(t)
	e.autoSpeed = 9
	e.avgPickModeTime = 1000
	e.avgEncodeTime = 2000
	e.autoSpeedFrameStartNS = 3000
	e.interRDThreshMult[0] = 192
	e.interRDThreshTouched[0] = true
	e.interModeErrorBins[20] = 2
	e.interModeSpeedErrorBins[20] = 3
	beforeGen := e.interRDThreshBaselineGen

	if err := e.SetCPUUsed(-3); err != nil {
		t.Fatalf("SetCPUUsed(-3) returned error: %v", err)
	}
	if e.autoSpeed != -3 {
		t.Fatalf("autoSpeed after SetCPUUsed(-3) = %d, want -3", e.autoSpeed)
	}
	if e.avgPickModeTime != 1000 || e.avgEncodeTime != 2000 || e.autoSpeedFrameStartNS != 3000 {
		t.Fatalf("auto-speed timers after SetCPUUsed(-3) = pick:%d encode:%d start:%d, want preserved",
			e.avgPickModeTime, e.avgEncodeTime, e.autoSpeedFrameStartNS)
	}
	if e.interRDThreshMult[0] != 192 || !e.interRDThreshTouched[0] || e.interRDThreshBaselineGen == beforeGen {
		t.Fatalf("inter RD thresholds after SetCPUUsed(-3) = mult:%d touched:%t gen:%d, want preserved state and invalidated baseline gen past %d",
			e.interRDThreshMult[0], e.interRDThreshTouched[0], e.interRDThreshBaselineGen, beforeGen)
	}
	if e.interModeErrorBins[20] != 2 || e.interModeSpeedErrorBins[20] != 3 {
		t.Fatalf("speed error bins after SetCPUUsed(-3) = current:%d previous:%d, want preserved",
			e.interModeErrorBins[20], e.interModeSpeedErrorBins[20])
	}

	e.autoSpeed = 3
	e.avgPickModeTime = 1000
	e.avgEncodeTime = 2000
	e.autoSpeedFrameStartNS = 3000
	e.interRDThreshMult[0] = 192
	e.interRDThreshTouched[0] = true
	e.interModeErrorBins[20] = 2
	e.interModeSpeedErrorBins[20] = 3
	beforeGen = e.interRDThreshBaselineGen
	if err := e.SetCPUUsed(0); err != nil {
		t.Fatalf("SetCPUUsed(0) returned error: %v", err)
	}
	if e.autoSpeed != 0 {
		t.Fatalf("autoSpeed after SetCPUUsed(0) = %d, want 0", e.autoSpeed)
	}
	if e.avgPickModeTime != 1000 || e.avgEncodeTime != 2000 || e.autoSpeedFrameStartNS != 3000 {
		t.Fatalf("auto-speed timers after SetCPUUsed(0) = pick:%d encode:%d start:%d, want preserved",
			e.avgPickModeTime, e.avgEncodeTime, e.autoSpeedFrameStartNS)
	}
	if e.interRDThreshMult[0] != 192 || !e.interRDThreshTouched[0] || e.interRDThreshBaselineGen == beforeGen {
		t.Fatalf("inter RD thresholds after SetCPUUsed(0) = mult:%d touched:%t gen:%d, want preserved state and invalidated baseline gen past %d",
			e.interRDThreshMult[0], e.interRDThreshTouched[0], e.interRDThreshBaselineGen, beforeGen)
	}
	if e.interModeErrorBins[20] != 2 || e.interModeSpeedErrorBins[20] != 3 {
		t.Fatalf("speed error bins after SetCPUUsed(0) = current:%d previous:%d, want preserved",
			e.interModeErrorBins[20], e.interModeSpeedErrorBins[20])
	}
}

func TestSetDeadlinePreservesRuntimePickerState(t *testing.T) {
	e := newTestEncoder(t)
	e.autoSpeed = 9
	e.avgPickModeTime = 1000
	e.avgEncodeTime = 2000
	e.autoSpeedFrameStartNS = 3000
	e.interRDThreshMult[0] = 192
	e.interRDThreshTouched[0] = true
	beforeGen := e.interRDThreshBaselineGen

	if err := e.SetDeadline(DeadlineGoodQuality); err != nil {
		t.Fatalf("SetDeadline(good) returned error: %v", err)
	}
	if e.avgPickModeTime != 1000 || e.avgEncodeTime != 2000 || e.autoSpeedFrameStartNS != 3000 {
		t.Fatalf("auto-speed timers after SetDeadline(good) = pick:%d encode:%d start:%d, want preserved",
			e.avgPickModeTime, e.avgEncodeTime, e.autoSpeedFrameStartNS)
	}
	if e.interRDThreshMult[0] != 192 || !e.interRDThreshTouched[0] || e.interRDThreshBaselineGen == beforeGen {
		t.Fatalf("inter RD thresholds after SetDeadline(good) = mult:%d touched:%t gen:%d, want preserved state and invalidated baseline gen past %d",
			e.interRDThreshMult[0], e.interRDThreshTouched[0], e.interRDThreshBaselineGen, beforeGen)
	}
}

func TestSetDeadlineReclampsRawConfiguredCPUUsed(t *testing.T) {
	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineGoodQuality); err != nil {
		t.Fatalf("SetDeadline(good) returned error: %v", err)
	}
	if err := e.SetCPUUsed(-8); err != nil {
		t.Fatalf("SetCPUUsed(-8) returned error: %v", err)
	}
	if e.opts.CpuUsed != -5 {
		t.Fatalf("good-quality CpuUsed = %d, want mode-clamped -5", e.opts.CpuUsed)
	}
	if e.autoSpeed != -5 {
		t.Fatalf("good-quality autoSpeed = %d, want mode-clamped -5", e.autoSpeed)
	}

	if err := e.SetDeadline(DeadlineRealtime); err != nil {
		t.Fatalf("SetDeadline(realtime) returned error: %v", err)
	}
	if e.opts.CpuUsed != -8 {
		t.Fatalf("realtime CpuUsed = %d, want raw configured -8", e.opts.CpuUsed)
	}
	if e.autoSpeed != -8 {
		t.Fatalf("realtime autoSpeed = %d, want raw configured -8", e.autoSpeed)
	}
	if got := e.libvpxCPUUsed(); got != 8 {
		t.Fatalf("realtime speed feature = %d, want 8", got)
	}
}

func TestRealtimeAutoSpeedPositiveCPUStaysInFastEnoughBand(t *testing.T) {
	e := newSizedTestEncoder(t, 64, 64)
	e.opts.CpuUsed = 8

	e.libvpxAutoSelectSpeed()
	if e.autoSpeed != 4 {
		t.Fatalf("cold positive-cpu autospeed = %d, want speed-4 band", e.autoSpeed)
	}
	e.beginAutoSpeedTiming()
	e.finishAutoSpeedTiming(true)
	// libvpxAutoSelectSpeed keys its cold-start branch off e.frameCount==0
	// (mirroring libvpx's avg_pick_mode_time==0 sentinel without picking up
	// govpx-side timer noise). Simulate the post-frame-0 transition so the
	// follow-up call exercises the real auto-select branch rather than the
	// cold-start reset.
	e.frameCount = 1
	e.libvpxAutoSelectSpeed()
	if e.autoSpeed != 4 {
		t.Fatalf("post-key positive-cpu autospeed = %d, want speed-4 band", e.autoSpeed)
	}
	e.beginAutoSpeedTiming()
	e.finishAutoSpeedTiming(false)
	e.frameCount = 2
	e.libvpxAutoSelectSpeed()
	if e.autoSpeed != 4 {
		t.Fatalf("post-inter positive-cpu autospeed = %d, want speed-4 band", e.autoSpeed)
	}

	e = newSizedTestEncoder(t, 1280, 720)
	e.opts.CpuUsed = 16
	e.libvpxAutoSelectSpeed()
	if e.autoSpeed != 16 {
		t.Fatalf("cpu-used=16 autospeed = %d, want pinned aggressive band 16", e.autoSpeed)
	}
}

func TestRealtimeAutoSpeedKeyFrameTimingPinsStableRegion(t *testing.T) {
	// Every keyframe at mbs >= 200 pins its auto-speed duration sample to
	// budget/3 (the mediumAutoSpeedKeyFrameTimingCompensation stable-band
	// pin), so the next frame's vp8_auto_select_speed lands in the Speed--
	// branch and cpi->Speed clamps at the realtime floor of 4 -- the
	// trajectory the production (untraced) vpxenc follows on the reference
	// host, where even a 720p keyframe encodes below the rdopt.c:290
	// `Speed += 2` boundary of budget*100/95. A former 2*budget-2 pin for
	// 720p-class keyframes deliberately landed ABOVE that boundary (Speed 5
	// for the first inter frame); it was calibrated against
	// instrumented-oracle timings and caused the 720p RT cpu=8 frame-drop
	// divergence (see vp8_realtime_drop_parity_test.go).
	cases := []struct {
		name    string
		width   int
		height  int
		cpuUsed int
	}{
		{name: "720p-cpu8", width: 1280, height: 720, cpuUsed: 8},
		{name: "720p-cpu4", width: 1280, height: 720, cpuUsed: 4},
		{name: "854x480-cpu8", width: 854, height: 480, cpuUsed: 8},
		{name: "1024x576-cpu8", width: 1024, height: 576, cpuUsed: 8},
		{name: "svga-cpu8", width: 800, height: 600, cpuUsed: 8},
		{name: "svga-cpu4", width: 800, height: 600, cpuUsed: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSizedTestEncoder(t, tc.width, tc.height)
			e.opts.CpuUsed = tc.cpuUsed
			e.libvpxAutoSelectSpeed()

			budget := e.autoSpeedCompressionBudgetUS()
			e.autoSpeedFrameStartNS = nowMonotonicNS() - int64(10*budget)*1000
			e.finishAutoSpeedTiming(true)
			if want := budget / 3; e.avgEncodeTime != want || e.avgPickModeTime != want/2 {
				t.Fatalf("key autospeed timers = encode:%d pick:%d, want pinned encode:%d pick:%d",
					e.avgEncodeTime, e.avgPickModeTime, want, want/2)
			}
			// libvpxAutoSelectSpeed keys cold-start off e.frameCount==0 (see
			// libvpxCPUUsed comment). Simulate the post-frame-0 transition so
			// the follow-up call exercises the budget-vs-encode-time branch
			// rather than re-entering the cold-start reset. budget/3 sits
			// below the auto_speed_thresh stable band, so the Speed-- branch
			// fires and clamps at the realtime floor of 4.
			e.frameCount = 1
			e.libvpxAutoSelectSpeed()
			if e.autoSpeed != 4 {
				t.Fatalf("post-key autospeed = %d, want realtime floor 4", e.autoSpeed)
			}
		})
	}
}

func TestSetRealtimeTargetFrameDropMode(t *testing.T) {
	e := newTestEncoder(t)
	e.setFrameDropFromThresh(75)

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
	// libvpx only enables drop via an explicit rc_dropframe_thresh; restore
	// the threshold before re-enabling (equivalent to drop:75 config token).
	e.opts.DropFrameWaterMark = 75
	if err := e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropEnabled}); err != nil {
		t.Fatalf("enable frame drop returned error: %v", err)
	}
	if !e.rc.dropFrameAllowed || !e.opts.DropFrameAllowed || e.rc.dropFramesWaterMark != 75 {
		t.Fatalf("enabled frame drop = rc:%t opts:%t mark:%d, want true/true/75",
			e.rc.dropFrameAllowed, e.opts.DropFrameAllowed, e.rc.dropFramesWaterMark)
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
	e.opts.DropFrameWaterMark = defaultDropFramesWaterMark
	if err := e.SetFrameDropAllowed(true); err != nil {
		t.Fatalf("SetFrameDropAllowed(true) returned error: %v", err)
	}
	if !e.rc.dropFrameAllowed || !e.opts.DropFrameAllowed || e.rc.dropFramesWaterMark != defaultDropFramesWaterMark {
		t.Fatalf("enabled frame drop = rc:%t opts:%t mark:%d, want true/true/%d",
			e.rc.dropFrameAllowed, e.opts.DropFrameAllowed, e.rc.dropFramesWaterMark, defaultDropFramesWaterMark)
	}
}
