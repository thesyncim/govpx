//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"strings"
	"testing"
)

func TestVP8OracleEncoderStreamByteParityResizeNonDefaultControls(t *testing.T) {
	vp8test.RequireOracle(t, "encoder resize-control byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 8
		w1         = 64
		h1         = 64
		w2         = 96
		h2         = 96
	)
	seg1 := makePanningSources(w1, h1, frames, 0)
	seg2 := makePanningSources(w2, h2, frames, frames)
	baseOpts := EncoderOptions{
		Width:             w1,
		Height:            h1,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		Tuning:            TunePSNR,
	}

	cases := []struct {
		name      string
		mutate    func(*EncoderOptions)
		extraArgs []string
		coldLimit int
	}{
		{
			name: "denoiser-threads-token-ssim",
			mutate: func(opts *EncoderOptions) {
				opts.NoiseSensitivity = 3
				opts.Threads = 2
				opts.TokenPartitions = 2
				opts.Tuning = TuneSSIM
			},
			extraArgs: []string{"--noise-sensitivity=3", "--threads=2", "--token-parts=2", "--tune=ssim"},
		},
		{
			name: "screen-static-sharpness",
			mutate: func(opts *EncoderOptions) {
				opts.ScreenContentMode = 1
				opts.StaticThreshold = 50
				opts.Sharpness = 4
			},
			extraArgs: []string{"--screen-content-mode=1", "--static-thresh=50", "--sharpness=4"},
		},
		{
			name: "lookahead4-auto-alt-ref",
			mutate: func(opts *EncoderOptions) {
				opts.LookaheadFrames = 4
				opts.AutoAltRef = true
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts1 := baseOpts
			tc.mutate(&opts1)
			opts2 := opts1
			opts2.Width = w2
			opts2.Height = h2

			govpx1Cold := encodeFramesWithGovpx(t, opts1, seg1)
			govpx2Cold := encodeFramesWithGovpx(t, opts2, seg2)
			extraArgs := libvpxEndUsageArgs(tc.extraArgs)
			libvpx1 := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name+"-seg1", opts1, targetKbps, seg1, extraArgs)
			libvpx2 := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name+"-seg2", opts2, targetKbps, seg2, extraArgs)

			assertSegmentByteParity(t, "cold-seg1-"+tc.name, govpx1Cold, libvpx1, tc.coldLimit)
			assertSegmentByteParity(t, "cold-seg2-"+tc.name, govpx2Cold, libvpx2, tc.coldLimit)

			govpx1Resize, govpx2Resize := encodeWithMidStreamResize(t, opts1, w2, h2, seg1, seg2)
			assertSegmentByteParity(t, "resize-seg1-vs-cold-govpx-"+tc.name, govpx1Resize, govpx1Cold, 0)
			assertFirstFrameByteParity(t, "resize-seg2-forced-key-"+tc.name, govpx2Resize, libvpx2)
		})
	}
}

func TestVP8OracleEncoderStreamByteParityRuntimeResizeControlCrosses(t *testing.T) {
	vp8test.RequireOracle(t, "encoder runtime-resize control byte-parity gate")
	frameFlagsDriver := vp8test.VpxencFrameFlags(t)

	const (
		fps          = 30
		targetKbps   = 700
		framesPerSeg = 4
	)
	cases := []struct {
		name          string
		controlScript string
		apply         func(*testing.T, *VP8Encoder)
		flags         []EncodeFlags
		script        []string
		globalApply   map[int]func(*testing.T, *VP8Encoder)
		resizeApply   func(*testing.T, *VP8Encoder, int, int)
		mutate        func(*EncoderOptions)
		extraArgs     []string
		limit         int
	}{
		{
			name:          "active-checker",
			controlScript: "active:checker",
			apply:         activeMapApply("checker"),
		},
		{
			name:          "roi-border1",
			controlScript: "roi:border1",
			apply:         roiMapApply("border1"),
		},
		{
			name:          "roi-checker",
			controlScript: "roi:checker",
			apply:         roiMapApply("checker"),
		},
		{
			name:          "token-partitions-4",
			controlScript: "token:2",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetTokenPartitions(2)", e.SetTokenPartitions(2))
			},
		},
		{
			name:          "rtc-external",
			controlScript: "rtc:1",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
			},
		},
		{
			name:          "drop-frame-low-buffer",
			controlScript: "bitrate:300+bufsz:500+bufinit:100+bufopt:300+drop:60",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(drop-low-buffer)", e.SetRateControl(RateControlConfig{
					Mode:                RateControlCBR,
					TargetBitrateKbps:   300,
					MinQuantizer:        4,
					MaxQuantizer:        56,
					BufferSizeMs:        500,
					BufferInitialSizeMs: 100,
					BufferOptimalSizeMs: 300,
					DropFrameAllowed:    true,
					DropFrameWaterMark:  60,
				}))
			},
		},
		{
			name:          "frame-drop-toggle",
			controlScript: "drop:60",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetFrameDropAllowed(true)", e.SetFrameDropAllowed(true))
			},
		},
		{
			name:          "resize-bwe-fps-q-drop",
			controlScript: "bitrate:500+fps:24+minq:8+maxq:48+drop:60",
			resizeApply: func(t *testing.T, e *VP8Encoder, w, h int) {
				t.Helper()
				mustRuntime(t, "SetRealtimeTarget(resize-bwe-fps-q-drop)", e.SetRealtimeTarget(RealtimeTarget{
					Width:        w,
					Height:       h,
					BitrateKbps:  500,
					FPS:          24,
					MinQuantizer: 8,
					MaxQuantizer: 48,
					FrameDrop:    RealtimeFrameDropEnabled,
				}))
			},
		},
		{
			name:          "active-checker-noise3-threads2",
			controlScript: "active:checker",
			apply:         activeMapApply("checker"),
			mutate: func(opts *EncoderOptions) {
				opts.NoiseSensitivity = 3
				opts.Threads = 2
			},
			extraArgs: []string{"--noise-sensitivity=3", "--threads=2"},
		},
		{
			name:          "denoiser-disable-after-resize",
			controlScript: "noise:0",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
			},
			mutate: func(opts *EncoderOptions) {
				opts.NoiseSensitivity = 3
			},
			extraArgs: []string{"--noise-sensitivity=3"},
		},
		{
			name:          "roi-border1-er2-token4",
			controlScript: "roi:border1",
			apply:         roiMapApply("border1"),
			mutate: func(opts *EncoderOptions) {
				opts.ErrorResilientPartitions = true
				opts.TokenPartitions = 2
			},
			extraArgs: []string{"--error-resilient=2", "--token-parts=2"},
		},
		{
			name:          "token-partitions-8-er3",
			controlScript: "token:3",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetTokenPartitions(3)", e.SetTokenPartitions(3))
			},
			mutate: func(opts *EncoderOptions) {
				opts.ErrorResilient = true
				opts.ErrorResilientPartitions = true
			},
			extraArgs: []string{"--error-resilient=3"},
		},
		{
			name:          "rtc-external-roi-checker",
			controlScript: "rtc:1+roi:checker",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				roiMapApply("checker")(t, e)
			},
		},
		{
			name:          "drop-frame-active-left-off",
			controlScript: "bitrate:300+bufsz:500+bufinit:100+bufopt:300+drop:60+active:left-off",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(drop-low-buffer)", e.SetRateControl(RateControlConfig{
					Mode:                RateControlCBR,
					TargetBitrateKbps:   300,
					MinQuantizer:        4,
					MaxQuantizer:        56,
					BufferSizeMs:        500,
					BufferInitialSizeMs: 100,
					BufferOptimalSizeMs: 300,
					DropFrameAllowed:    true,
					DropFrameWaterMark:  60,
				}))
				activeMapApply("left-off")(t, e)
			},
		},
		{
			name:          "drop-frame-roi-border1",
			controlScript: "bitrate:300+bufsz:500+bufinit:100+bufopt:300+drop:60+roi:border1",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(drop-low-buffer)", e.SetRateControl(RateControlConfig{
					Mode:                RateControlCBR,
					TargetBitrateKbps:   300,
					MinQuantizer:        4,
					MaxQuantizer:        56,
					BufferSizeMs:        500,
					BufferInitialSizeMs: 100,
					BufferOptimalSizeMs: 300,
					DropFrameAllowed:    true,
					DropFrameWaterMark:  60,
				}))
				roiMapApply("border1")(t, e)
			},
		},
		{
			name:          "deadline-good-cpu-3",
			controlScript: "deadline:good+cpu:-3",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetDeadline(good)", e.SetDeadline(DeadlineGoodQuality))
				mustRuntime(t, "SetCPUUsed(-3)", e.SetCPUUsed(-3))
			},
		},
		{
			name:          "cq-mode",
			controlScript: runtimeRateControlModeControlToken(RateControlCQ, targetKbps),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(CQ)", e.SetRateControl(runtimeRateControlModeConfig(RateControlCQ, targetKbps)))
			},
		},
		{
			name:          "q-mode",
			controlScript: runtimeRateControlModeControlToken(RateControlQ, targetKbps),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(Q)", e.SetRateControl(runtimeRateControlModeConfig(RateControlQ, targetKbps)))
			},
		},
		{
			name:          "sharpness7-screen2-static500",
			controlScript: "sharpness:7+screen:2+static:500",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetSharpness(7)", e.SetSharpness(7))
				mustRuntime(t, "SetScreenContentMode(2)", e.SetScreenContentMode(2))
				mustRuntime(t, "SetStaticThreshold(500)", e.SetStaticThreshold(500))
			},
		},
		{
			name:          "max-intra-gf-boost",
			controlScript: "maxintra:500+gfboost:500",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetMaxIntraBitratePct(500)", e.SetMaxIntraBitratePct(500))
				mustRuntime(t, "SetGFCBRBoostPct(500)", e.SetGFCBRBoostPct(500))
			},
		},
		{
			name:          "active-checker-roi-border1",
			controlScript: "active:checker+roi:border1",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				activeMapApply("checker")(t, e)
				roiMapApply("border1")(t, e)
			},
		},
		{
			name:        "temporal-two-layer-enable",
			flags:       temporalScalabilityWindowFlags(framesPerSeg*2, TemporalLayeringTwoLayers, framesPerSeg, framesPerSeg*2),
			script:      temporalScalabilityWindowScript(framesPerSeg*2, TemporalLayeringTwoLayers, framesPerSeg, framesPerSeg*2, "resize:32x32+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps)),
			globalApply: map[int]func(*testing.T, *VP8Encoder){framesPerSeg: runtimeTemporalApply(TemporalLayeringTwoLayers, targetKbps, "two-layer")},
		},
		{
			name:        "temporal-three-layer-enable",
			flags:       temporalScalabilityWindowFlags(framesPerSeg*2, TemporalLayeringThreeLayers, framesPerSeg, framesPerSeg*2),
			script:      temporalScalabilityWindowScript(framesPerSeg*2, TemporalLayeringThreeLayers, framesPerSeg, framesPerSeg*2, "resize:32x32+"+runtimeTemporalControlToken(TemporalLayeringThreeLayers, targetKbps)),
			globalApply: map[int]func(*testing.T, *VP8Encoder){framesPerSeg: runtimeTemporalApply(TemporalLayeringThreeLayers, targetKbps, "three-layer")},
		},
		{
			name:  "temporal-two-layer-active-checker-enable",
			flags: temporalScalabilityWindowFlags(framesPerSeg*2, TemporalLayeringTwoLayers, framesPerSeg, framesPerSeg*2),
			script: temporalScalabilityWindowScript(
				framesPerSeg*2,
				TemporalLayeringTwoLayers,
				framesPerSeg,
				framesPerSeg*2,
				"resize:32x32+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps)+"+active:checker",
			),
			globalApply: map[int]func(*testing.T, *VP8Encoder){
				framesPerSeg: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					runtimeTemporalApply(TemporalLayeringTwoLayers, targetKbps, "two-layer")(t, e)
					activeMapApply("checker")(t, e)
				},
			},
		},
		{
			name:  "temporal-two-layer-roi-border-enable",
			flags: temporalScalabilityWindowFlags(framesPerSeg*2, TemporalLayeringTwoLayers, framesPerSeg, framesPerSeg*2),
			script: temporalScalabilityWindowScript(
				framesPerSeg*2,
				TemporalLayeringTwoLayers,
				framesPerSeg,
				framesPerSeg*2,
				"resize:32x32+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps)+"+roi:border1",
			),
			globalApply: map[int]func(*testing.T, *VP8Encoder){
				framesPerSeg: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					runtimeTemporalApply(TemporalLayeringTwoLayers, targetKbps, "two-layer")(t, e)
					roiMapApply("border1")(t, e)
				},
			},
		},
		{
			name:  "temporal-two-layer-disable-after-resize",
			flags: temporalScalabilityWindowFlags(framesPerSeg*2, TemporalLayeringTwoLayers, 0, framesPerSeg),
			script: func() []string {
				script := runtimeTemporalDisableScript(framesPerSeg*2, TemporalLayeringTwoLayers, framesPerSeg, targetKbps)
				script[framesPerSeg] = "resize:32x32+" + runtimeTemporalOffControlToken(targetKbps)
				return script
			}(),
			globalApply: map[int]func(*testing.T, *VP8Encoder){
				framesPerSeg: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
			mutate: func(opts *EncoderOptions) {
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)
			},
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seg1 := makePanningSources(64, 64, framesPerSeg, 0)
			seg2 := makePanningSources(32, 32, framesPerSeg, framesPerSeg)
			opts := EncoderOptions{
				Width:             64,
				Height:            64,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineRealtime,
				CpuUsed:           0,
			}
			if tc.mutate != nil {
				tc.mutate(&opts)
			}
			sources := append(append([]Image(nil), seg1...), seg2...)
			script := append([]string(nil), tc.script...)
			if script == nil {
				script = make([]string, len(sources))
				for i := range script {
					script[i] = "-"
				}
				script[framesPerSeg] = "resize:32x32+" + tc.controlScript
			}
			extraArgs := append([]string(nil), tc.extraArgs...)
			extraArgs = append(extraArgs, "--control-script="+strings.Join(script, ","))
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "runtime-resize-control-"+tc.name, opts, opts.TargetBitrateKbps, sources, tc.flags, extraArgs)

			var govpxFrames [][]byte
			if tc.resizeApply != nil {
				govpxFrames = encodeWithMidStreamResizeGlobalControlsAndResize(t, opts, 32, 32, seg1, seg2, tc.flags, tc.globalApply, tc.resizeApply)
			} else if tc.globalApply != nil || tc.flags != nil {
				govpxFrames = encodeWithMidStreamResizeGlobalControls(t, opts, 32, 32, seg1, seg2, tc.flags, tc.globalApply)
			} else {
				govpxFrames = encodeWithMidStreamResizeAndControl(t, opts, 32, 32, seg1, seg2, tc.apply)
			}
			assertSegmentByteParity(t, "runtime-resize-control-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}
