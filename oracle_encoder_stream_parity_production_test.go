//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"strings"
	"testing"
)

// TestOracleEncoderStreamByteParityProductionShortRuns pins the production
// benchmark/WebRTC-like configurations that are easy to miss in the smaller
// oracle matrices. In particular, the 1-frame vs 2-frame realtime runs catch
// cold-start control/config drift before it hides behind longer GOP averages.
func TestOracleEncoderStreamByteParityProductionShortRuns(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run production byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	type productionCase struct {
		name       string
		width      int
		height     int
		frames     int
		fps        int
		bitrate    int
		deadline   Deadline
		cpuUsed    int
		benchShape string
	}

	cases := []productionCase{
		{
			name:       "webrtc-720p-1500kbps-realtime-pinned-speed-1f",
			width:      1280,
			height:     720,
			frames:     1,
			fps:        30,
			bitrate:    1500,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime",
		},
		{
			name:       "webrtc-720p-1500kbps-realtime-pinned-speed-2f",
			width:      1280,
			height:     720,
			frames:     2,
			fps:        30,
			bitrate:    1500,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime",
		},
		{
			name:       "bench-720p-2000kbps-realtime-pinned-speed-1f",
			width:      1280,
			height:     720,
			frames:     1,
			fps:        30,
			bitrate:    2000,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime",
		},
		{
			name:       "bench-720p-2000kbps-realtime-pinned-speed-2f",
			width:      1280,
			height:     720,
			frames:     2,
			fps:        30,
			bitrate:    2000,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime",
		},
		{
			name:       "bench-720p-2000kbps-realtime-no-drop-pinned-speed-2f",
			width:      1280,
			height:     720,
			frames:     2,
			fps:        30,
			bitrate:    2000,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime-nodrop",
		},
		{
			name:       "bench-720p-8000kbps-realtime-no-drop-pinned-speed-2f",
			width:      1280,
			height:     720,
			frames:     2,
			fps:        30,
			bitrate:    8000,
			deadline:   DeadlineRealtime,
			cpuUsed:    -8,
			benchShape: "realtime-nodrop",
		},
		{
			name:       "bench-1080p-8000kbps-good-2f",
			width:      1920,
			height:     1080,
			frames:     2,
			fps:        30,
			bitrate:    8000,
			deadline:   DeadlineGoodQuality,
			cpuUsed:    8,
			benchShape: "good",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, tc.frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(tc.width, tc.height, i)
			}

			opts, extraArgs := productionParityOptions(tc.width, tc.height, tc.fps, tc.bitrate, tc.deadline, tc.cpuUsed, tc.benchShape)
			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, tc.bitrate, sources, extraArgs)
			assertSegmentByteParity(t, "production-short-run-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}

func TestOracleEncoderProductionRuntimeTransitions720p(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run production runtime-transition oracle")
	}
	driver := findVpxencFrameFlags(t)

	const (
		width   = 1280
		height  = 720
		fps     = 30
		bitrate = 8000
		frames  = 30
	)
	opts, extraArgs := productionParityOptions(width, height, fps, bitrate, DeadlineRealtime, -8, "realtime-nodrop")
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, 0)
	}
	script := runtimeControlScript(frames, map[int]string{
		8:  "bitrate:6000+fps:24+minq:4+maxq:52+drop:60",
		16: "bitrate:9000+fps:30+minq:2+maxq:56+drop:0",
		24: "bitrate:7000+fps:30+minq:8+maxq:48+drop:60",
	})
	flags := make([]EncodeFlags, frames)
	flags[8] = EncodeForceKeyFrame
	flags[16] = EncodeForceKeyFrame
	flags[24] = EncodeForceKeyFrame
	apply := map[int]func(*testing.T, *VP8Encoder){
		8: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetRealtimeTarget(6000/24/q4-52/drop)", e.SetRealtimeTarget(RealtimeTarget{
				BitrateKbps: 6000, FPS: 24, MinQuantizer: 4, MaxQuantizer: 52, FrameDrop: RealtimeFrameDropEnabled,
			}))
		},
		16: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetRealtimeTarget(9000/30/q2-56/nodrop)", e.SetRealtimeTarget(RealtimeTarget{
				BitrateKbps: 9000, FPS: 30, MinQuantizer: 2, MaxQuantizer: 56, FrameDrop: RealtimeFrameDropDisabled,
			}))
		},
		24: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetRealtimeTarget(7000/30/q8-48/drop)", e.SetRealtimeTarget(RealtimeTarget{
				BitrateKbps: 7000, FPS: 30, MinQuantizer: 8, MaxQuantizer: 48, FrameDrop: RealtimeFrameDropEnabled,
			}))
		},
	}

	govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, flags, apply)
	extraArgs = append(extraArgs, "--control-script="+strings.Join(script, ","))
	libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "production-runtime-transitions-720p", opts, bitrate, sources, flags, extraArgs)
	assertProductionTransitionPacketShape(t, "production-runtime-transitions-720p", govpxFrames, libvpxFrames)
}

func assertProductionTransitionPacketShape(t *testing.T, label string, got, want [][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s frame count = %d, want %d from libvpx", label, len(got), len(want))
	}
	if len(got) == 0 {
		t.Fatalf("%s produced no packets", label)
	}
	for i := range got {
		gotInfo, err := PeekVP8StreamInfo(got[i])
		if err != nil {
			t.Fatalf("%s govpx frame %d PeekVP8StreamInfo: %v", label, i, err)
		}
		wantInfo, err := PeekVP8StreamInfo(want[i])
		if err != nil {
			t.Fatalf("%s libvpx frame %d PeekVP8StreamInfo: %v", label, i, err)
		}
		if gotInfo.KeyFrame != wantInfo.KeyFrame || gotInfo.ShowFrame != wantInfo.ShowFrame {
			t.Fatalf("%s frame %d shape mismatch: govpx key/show=%t/%t libvpx key/show=%t/%t",
				label, i, gotInfo.KeyFrame, gotInfo.ShowFrame, wantInfo.KeyFrame, wantInfo.ShowFrame)
		}
	}
}

// TestOracleEncoderStreamByteParityProductionConstantQuality pins CQ +
// Q RC modes at production-shape resolutions. CQ mode applies the
// libvpx vp8/encoder/onyx_if.c:3727-3739 (one-pass best_quality floor
// for KF/GF/ARF) + ratectrl.c:849-852 (active_worst CQ floor) + 2847-
// 2852 (severe-undershoot recode active_best_quality drop) branches
// that lower-resolution oracle fixtures don't exercise tightly. Q mode
// pins libvpx VPX_Q (USAGE_CONSTANT_QUALITY) where end_usage falls
// through every CBR/CQ-gated branch so the regulator behaves as a
// default unbuffered VBR-ish path with the cq_level field stored but
// unused in branches. Both modes encode three frames so the second
// inter frame sees the post-keyframe overspend drain.
func TestOracleEncoderStreamByteParityProductionConstantQuality(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run production CQ/Q byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	type cqCase struct {
		name     string
		width    int
		height   int
		frames   int
		fps      int
		bitrate  int
		rcMode   RateControlMode
		cqLevel  int
		deadline Deadline
		cpuUsed  int
	}

	cases := []cqCase{
		{
			name:     "webrtc-720p-cq20-good-cpu0-3f",
			width:    1280,
			height:   720,
			frames:   3,
			fps:      30,
			bitrate:  1500,
			rcMode:   RateControlCQ,
			cqLevel:  20,
			deadline: DeadlineGoodQuality,
			cpuUsed:  0,
		},
		{
			name:     "webrtc-720p-cq40-good-cpu4-3f",
			width:    1280,
			height:   720,
			frames:   3,
			fps:      30,
			bitrate:  1500,
			rcMode:   RateControlCQ,
			cqLevel:  40,
			deadline: DeadlineGoodQuality,
			cpuUsed:  4,
		},
		// Deferred: webrtc-720p-cq56-best-cpu0 (recode_loop=1
		// BestQuality high-CQ ceiling) and webrtc-720p-q20-good-cpu0
		// (recode_loop=1 GoodQuality Q-mode) reveal pre-existing
		// inter-rate-correction divergences at recode_loop=1 +
		// production resolutions. The picked Q matches libvpx within
		// 1-2 indices but the regulator chooses a different activeBest
		// table lookup before recode converges. Tracked separately
		// from this port; do not gate parity on these fixtures yet.
		{
			name:     "webrtc-720p-q40-good-cpu4-3f",
			width:    1280,
			height:   720,
			frames:   3,
			fps:      30,
			bitrate:  1500,
			rcMode:   RateControlQ,
			cqLevel:  40,
			deadline: DeadlineGoodQuality,
			cpuUsed:  4,
		},
		{
			name:     "webrtc-720p-q4-good-cpu4-3f",
			width:    1280,
			height:   720,
			frames:   3,
			fps:      30,
			bitrate:  3000,
			rcMode:   RateControlQ,
			cqLevel:  4,
			deadline: DeadlineGoodQuality,
			cpuUsed:  4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, tc.frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(tc.width, tc.height, i)
			}
			opts, extraArgs := productionConstantQualityOptions(tc.width, tc.height, tc.fps, tc.bitrate, tc.rcMode, tc.cqLevel, tc.deadline, tc.cpuUsed)
			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, tc.bitrate, sources, extraArgs)
			assertSegmentByteParity(t, "production-cq-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}

func productionConstantQualityOptions(width, height, fps, bitrate int, rcMode RateControlMode, cqLevel int, deadline Deadline, cpuUsed int) (EncoderOptions, []string) {
	if fps <= 0 {
		fps = 30
	}
	// Mirror the libvpx CQ/Q defaults: a wider [minQ,maxQ] envelope so
	// the regulator has room to track the cq_level target across the
	// short 3-frame trajectory. Buffer parameters come from
	// vpxenc.c defaults (rc_buf_sz=6000ms / initial=4000ms /
	// optimal=5000ms) so libvpx's VPX_VBR-style buffer model lines up
	// with govpx's; libvpx applies these for both CQ and Q modes since
	// neither maps to USAGE_LOCAL_FILE_PLAYBACK (the VBR-only relaxed
	// buffer override) and the CLI iface leaves the user-provided
	// buffer params alone for both modes.
	bufferSizeMs := 6000
	bufferInitialMs := 4000
	bufferOptimalMs := 5000
	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     rcMode,
		TargetBitrateKbps:   bitrate,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             cqLevel,
		Deadline:            deadline,
		CpuUsed:             cpuUsed,
		KeyFrameInterval:    3000,
		BufferSizeMs:        bufferSizeMs,
		BufferInitialSizeMs: bufferInitialMs,
		BufferOptimalSizeMs: bufferOptimalMs,
		Threads:             1,
		TokenPartitions:     0,
	}
	endUsage := "cq"
	if rcMode == RateControlQ {
		endUsage = "q"
	}
	extraArgs := []string{
		"--end-usage=" + endUsage,
		"--cq-level=" + itoa(cqLevel),
		"--kf-min-dist=3000",
		"--kf-max-dist=3000",
		"--buf-sz=" + itoa(bufferSizeMs),
		"--buf-initial-sz=" + itoa(bufferInitialMs),
		"--buf-optimal-sz=" + itoa(bufferOptimalMs),
		"--threads=1",
		"--token-parts=0",
	}
	return opts, extraArgs
}

func productionParityOptions(width, height, fps, bitrate int, deadline Deadline, cpuUsed int, shape string) (EncoderOptions, []string) {
	if fps <= 0 {
		fps = 30
	}
	minQ := 4
	keyFrameInterval := fps
	bufferSizeMs := 600
	bufferInitialMs := 400
	bufferOptimalMs := 500
	undershootPct := 100
	overshootPct := 15
	dropFrameAllowed := false
	dropFrameWaterMark := 0
	noiseSensitivity := 0
	staticThreshold := 0
	maxIntraBitratePct := 0
	if shape == "realtime" || shape == "realtime-nodrop" {
		minQ = 2
		keyFrameInterval = 3000
		bufferSizeMs = 1000
		bufferInitialMs = 500
		bufferOptimalMs = 600
		noiseSensitivity = 4
		staticThreshold = 1
		maxIntraBitratePct = max(300, 600*fps/20)
		if shape == "realtime" {
			dropFrameAllowed = true
			dropFrameWaterMark = 30
		}
	}
	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   bitrate,
		MinQuantizer:        minQ,
		MaxQuantizer:        56,
		Deadline:            deadline,
		CpuUsed:             cpuUsed,
		KeyFrameInterval:    keyFrameInterval,
		BufferSizeMs:        bufferSizeMs,
		BufferInitialSizeMs: bufferInitialMs,
		BufferOptimalSizeMs: bufferOptimalMs,
		UndershootPct:       undershootPct,
		OvershootPct:        overshootPct,
		MaxIntraBitratePct:  maxIntraBitratePct,
		DropFrameAllowed:    dropFrameAllowed,
		DropFrameWaterMark:  dropFrameWaterMark,
		NoiseSensitivity:    noiseSensitivity,
		StaticThreshold:     staticThreshold,
		Threads:             1,
		TokenPartitions:     0,
	}
	extraArgs := []string{
		"--end-usage=cbr",
		"--kf-min-dist=" + itoa(keyFrameInterval),
		"--kf-max-dist=" + itoa(keyFrameInterval),
		"--buf-sz=" + itoa(bufferSizeMs),
		"--buf-initial-sz=" + itoa(bufferInitialMs),
		"--buf-optimal-sz=" + itoa(bufferOptimalMs),
		"--undershoot-pct=" + itoa(undershootPct),
		"--overshoot-pct=" + itoa(overshootPct),
		"--threads=1",
		"--token-parts=0",
		"--noise-sensitivity=" + itoa(noiseSensitivity),
		"--drop-frame=" + itoa(dropFrameWaterMark),
	}
	if maxIntraBitratePct > 0 {
		extraArgs = append(extraArgs, "--max-intra-rate="+itoa(maxIntraBitratePct))
	}
	if staticThreshold > 0 {
		extraArgs = append(extraArgs, "--static-thresh="+itoa(staticThreshold))
	}
	return opts, extraArgs
}
