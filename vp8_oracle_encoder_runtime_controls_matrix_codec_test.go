//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

func TestVP8OracleEncoderStreamByteParityRuntimeControlsCodec(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime-control byte-parity gate")
	}
	driver := coracletest.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 32, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	segmented64 := fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}
	temporalLayerOverrideIDs := []int{0, 0, 1, 1, 0, 1, 0, 1, 0, 0, 1, 1}
	temporalThreeLayerOverrideIDs := []int{0, 2, 1, 2, 0, 1, 2, 0, 2, 1, 0, 2}
	_, _, _, _, _ = panning32, panning64, segmented64, temporalLayerOverrideIDs, temporalThreeLayerOverrideIDs

	type runtimeCase struct {
		name        string
		fx          fixture
		opts        EncoderOptions
		flags       []EncodeFlags
		libvpxFlags []EncodeFlags
		script      []string
		apply       map[int]func(*testing.T, *VP8Encoder)
		extraArgs   []string
		// matchLimit caps how many leading frames the per-frame byte
		// compare asserts strictly; later frames are logged only. Used
		// for runtime-config transitions that exercise the libvpx
		// vp8_change_config Speed reset (oxcf.cpu_used) — the post-
		// reset auto-speed evolution can land on a slightly different
		// sample than libvpx because the carried-over
		// avg_pick_mode_time / avg_encode_time timers differ subtly
		// after the transition.
		matchLimit int
	}

	baseOpts := func(fx fixture) EncoderOptions {
		return EncoderOptions{
			Width:             fx.w,
			Height:            fx.h,
			FPS:               fps,
			RateControlMode:   RateControlCBR,
			TargetBitrateKbps: targetKbps,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			KeyFrameInterval:  999,
			Deadline:          DeadlineRealtime,
			CpuUsed:           0,
			Tuning:            TunePSNR,
		}
	}
	cases := []runtimeCase{
		{
			name:   "temporal-scalability-enable-disable-transition",
			fx:     panning64,
			opts:   baseOpts(panning64),
			flags:  temporalScalabilityEnableDisableFlags(frames),
			script: temporalScalabilityEnableDisableScript(frames),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(two-layer)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled: true,
						Mode:    TemporalLayeringTwoLayers,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{
							420, targetKbps,
						},
					}))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
		{
			name: "temporal-scalability-two-to-three-layer-transition",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled: true,
					Mode:    TemporalLayeringTwoLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{
						420, targetKbps,
					},
				}
				return opts
			}(),
			flags:  temporalScalabilityTwoToThreeFlags(frames),
			script: temporalScalabilityTwoToThreeScript(frames),
			extraArgs: []string{
				"--temporal-layers=2",
				"--temporal-bitrates=420,700",
				"--temporal-decimators=2,1",
				"--temporal-periodicity=2",
				"--temporal-layer-ids=0,1",
			},
			apply: map[int]func(*testing.T, *VP8Encoder){
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(three-layer)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled: true,
						Mode:    TemporalLayeringThreeLayers,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{
							280, 420, targetKbps,
						},
					}))
				},
			},
		},
		{
			name: "temporal-scalability-three-to-two-layer-transition",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled:                true,
					Mode:                   TemporalLayeringThreeLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{280, 420, targetKbps},
				}
				return opts
			}(),
			flags:  temporalScalabilityThreeToTwoFlags(frames),
			script: temporalScalabilityThreeToTwoScript(frames),
			extraArgs: []string{
				"--temporal-layers=3",
				"--temporal-bitrates=280,420,700",
				"--temporal-decimators=4,2,1",
				"--temporal-periodicity=4",
				"--temporal-layer-ids=0,2,1,2",
			},
			apply: map[int]func(*testing.T, *VP8Encoder){
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(two-layer)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled:                true,
						Mode:                   TemporalLayeringTwoLayers,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{420, targetKbps},
					}))
				},
			},
		},
		{
			name: "temporal-scalability-same-layer-bitrate-redistribution",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled:                true,
					Mode:                   TemporalLayeringTwoLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{350, targetKbps},
				}
				return opts
			}(),
			flags:     temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 6),
			script:    temporalScalabilityReconfigureScript(frames, TemporalLayeringTwoLayers, 6, "tslayers:2+tsperiodicity:2+tsbitrates:420/700+tsdecimators:2/1+tsids:0/1"),
			extraArgs: []string{"--temporal-layers=2", "--temporal-bitrates=350,700", "--temporal-decimators=2,1", "--temporal-periodicity=2", "--temporal-layer-ids=0,1"},
			apply: map[int]func(*testing.T, *VP8Encoder){
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(two-layer-redistribution)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled:                true,
						Mode:                   TemporalLayeringTwoLayers,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{420, targetKbps},
					}))
				},
			},
		},
		{
			name:   "temporal-scalability-five-layer-enable-disable",
			fx:     panning64,
			opts:   baseOpts(panning64),
			flags:  temporalScalabilityWindowFlags(frames, TemporalLayeringFiveLayers, 2, 8),
			script: temporalScalabilityWindowScript(frames, TemporalLayeringFiveLayers, 2, 8, "tslayers:5+tsperiodicity:16+tsbitrates:100/220/360/520/700+tsdecimators:16/8/4/2/1+tsids:0/4/3/4/2/4/3/4/1/4/3/4/2/4/3/4"),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(five-layer)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled:                true,
						Mode:                   TemporalLayeringFiveLayers,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{100, 220, 360, 520, targetKbps},
					}))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
		{
			name:   "temporal-scalability-mode12-enable-disable",
			fx:     panning64,
			opts:   baseOpts(panning64),
			flags:  temporalScalabilityWindowFlags(frames, TemporalLayeringThreeLayersNoSync, 2, 8),
			script: temporalScalabilityWindowScript(frames, TemporalLayeringThreeLayersNoSync, 2, 8, "tslayers:3+tsperiodicity:4+tsbitrates:280/420/700+tsdecimators:4/2/1+tsids:0/2/1/2"),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(mode12)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled:                true,
						Mode:                   TemporalLayeringThreeLayersNoSync,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{280, 420, targetKbps},
					}))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
		{
			name: "codec-control-surface-toggle",
			fx:   panning64,
			opts: baseOpts(panning64),
			// Runtime codec controls route through libvpx update_extracfg,
			// which forces an LF-delta update on the next packet.
			script: runtimeControlScript(frames, map[int]string{
				2: "sharpness:4+static:1+screen:1+gfboost:50+maxintra:100+token:2",
				6: "sharpness:0+static:0+screen:0+gfboost:0+maxintra:0+token:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetSharpness", e.SetSharpness(4))
					mustRuntime(t, "SetStaticThreshold", e.SetStaticThreshold(1))
					mustRuntime(t, "SetScreenContentMode", e.SetScreenContentMode(1))
					mustRuntime(t, "SetGFCBRBoostPct", e.SetGFCBRBoostPct(50))
					mustRuntime(t, "SetMaxIntraBitratePct", e.SetMaxIntraBitratePct(100))
					mustRuntime(t, "SetTokenPartitions", e.SetTokenPartitions(2))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetSharpness", e.SetSharpness(0))
					mustRuntime(t, "SetStaticThreshold", e.SetStaticThreshold(0))
					mustRuntime(t, "SetScreenContentMode", e.SetScreenContentMode(0))
					mustRuntime(t, "SetGFCBRBoostPct", e.SetGFCBRBoostPct(0))
					mustRuntime(t, "SetMaxIntraBitratePct", e.SetMaxIntraBitratePct(0))
					mustRuntime(t, "SetTokenPartitions", e.SetTokenPartitions(0))
				},
			},
		},
		{
			name: "sharpness-only-two-step",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "sharpness:4",
				6: "sharpness:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetSharpness(4)", e.SetSharpness(4))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetSharpness(0)", e.SetSharpness(0))
				},
			},
		},
		{
			name: "static-threshold-only-two-step",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				2: "static:500",
				6: "static:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(500)", e.SetStaticThreshold(500))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(0)", e.SetStaticThreshold(0))
				},
			},
		},
		{
			name: "static-threshold-500-noise3-runtime-roundtrip",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			script: runtimeControlScript(frames, map[int]string{
				2: "static:500",
				6: "static:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(500)", e.SetStaticThreshold(500))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(0)", e.SetStaticThreshold(0))
				},
			},
		},
		{
			name: "screen-content-1-2-roundtrip",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				2: "screen:1",
				5: "screen:2",
				8: "screen:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetScreenContentMode(1)", e.SetScreenContentMode(1))
				},
				5: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetScreenContentMode(2)", e.SetScreenContentMode(2))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetScreenContentMode(0)", e.SetScreenContentMode(0))
				},
			},
		},
		{
			name: "screen-content-2-noise3-runtime-roundtrip",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			script: runtimeControlScript(frames, map[int]string{
				2: "screen:2",
				8: "screen:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetScreenContentMode(2)", e.SetScreenContentMode(2))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetScreenContentMode(0)", e.SetScreenContentMode(0))
				},
			},
		},
		{
			name: "tuning-ssim-roundtrip",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "tune:ssim",
				7: "tune:psnr",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTuning(TuneSSIM)", e.SetTuning(TuneSSIM))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTuning(TunePSNR)", e.SetTuning(TunePSNR))
				},
			},
		},
		{
			name: "noise-sensitivity-1-enable-only",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "noise:1",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(1)", e.SetNoiseSensitivity(1))
				},
			},
		},
		{
			name: "noise-sensitivity-1-3-6-roundtrip",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "noise:1",
				4: "noise:3",
				6: "noise:6",
				9: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(1)", e.SetNoiseSensitivity(1))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(6)", e.SetNoiseSensitivity(6))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-6-3-sticky-yuv",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:6",
				4: "noise:3",
				8: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(6)", e.SetNoiseSensitivity(6))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-3-6-sticky-aggressive",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:3",
				4: "noise:6",
				8: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(6)", e.SetNoiseSensitivity(6))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-3-disable-after-inter",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:3",
				7: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-3-disable-after-force-keyframe",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				7: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:3",
				7: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-3-disable-threads2-token4",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.Threads = 2
				opts.TokenPartitions = 2
				return opts
			}(),
			extraArgs: []string{"--threads=2"},
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:3",
				7: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-2-4-5-roundtrip",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "noise:2",
				4: "noise:4",
				6: "noise:5",
				9: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(2)", e.SetNoiseSensitivity(2))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(4)", e.SetNoiseSensitivity(4))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(5)", e.SetNoiseSensitivity(5))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-4-disable-after-inter",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:4",
				7: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(4)", e.SetNoiseSensitivity(4))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "cq-level-transition",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.RateControlMode = RateControlCQ
				opts.CQLevel = 20
				return opts
			}(),
			script: runtimeControlScript(frames, map[int]string{
				4: "cq:35+minq:4+maxq:56",
				8: "cq:20",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget", e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 4, MaxQuantizer: 56}))
					mustRuntime(t, "SetCQLevel", e.SetCQLevel(35))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCQLevel", e.SetCQLevel(20))
				},
			},
		},
		{
			name: "q-mode-cq-level-transition",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.RateControlMode = RateControlQ
				opts.CQLevel = 20
				return opts
			}(),
			script: runtimeControlScript(frames, map[int]string{
				4: "cq:35+minq:4+maxq:56",
				8: "cq:20",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget", e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 4, MaxQuantizer: 56}))
					mustRuntime(t, "SetCQLevel", e.SetCQLevel(35))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCQLevel", e.SetCQLevel(20))
				},
			},
		},
		{
			name: "deadline-rc-mode-key-interval-transition",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				3: "deadline:good+endusage:vbr+kfmin:4+kfmax:4+undershoot:50+overshoot:50+bufsz:6000+bufinit:4000+bufopt:5000",
				7: "deadline:rt+endusage:cbr+kfmin:999+kfmax:999+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline", e.SetDeadline(DeadlineGoodQuality))
					mustRuntime(t, "SetRateControl", e.SetRateControl(RateControlConfig{
						Mode:                RateControlVBR,
						TargetBitrateKbps:   targetKbps,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						UndershootPct:       50,
						OvershootPct:        50,
						BufferSizeMs:        6000,
						BufferInitialSizeMs: 4000,
						BufferOptimalSizeMs: 5000,
					}))
					mustRuntime(t, "SetKeyFrameInterval", e.SetKeyFrameInterval(4))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline", e.SetDeadline(DeadlineRealtime))
					mustRuntime(t, "SetRateControl", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   targetKbps,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						UndershootPct:       100,
						OvershootPct:        100,
						BufferSizeMs:        6000,
						BufferInitialSizeMs: 4000,
						BufferOptimalSizeMs: 5000,
					}))
					mustRuntime(t, "SetKeyFrameInterval", e.SetKeyFrameInterval(999))
				},
			},
		},
		{
			name: "speed-tuning-denoiser-transition-with-force-kf",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: []EncodeFlags{
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				3: "cpu:-3+tune:ssim+noise:3",
				8: "cpu:0+tune:psnr+noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCPUUsed", e.SetCPUUsed(-3))
					mustRuntime(t, "SetTuning", e.SetTuning(TuneSSIM))
					mustRuntime(t, "SetNoiseSensitivity", e.SetNoiseSensitivity(3))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCPUUsed", e.SetCPUUsed(0))
					mustRuntime(t, "SetTuning", e.SetTuning(TunePSNR))
					mustRuntime(t, "SetNoiseSensitivity", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "arnr-runtime-no-arf",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.LookaheadFrames = 4
				opts.AutoAltRef = false
				return opts
			}(),
			extraArgs: []string{"--lag-in-frames=4", "--auto-alt-ref=0"},
			script: runtimeControlScript(frames, map[int]string{
				2: "arnrmax:7+arnrstrength:6+arnrtype:3",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(7, 6, 3))
				},
			},
		},
		{
			name: "arnr-runtime-transition-auto-alt-ref",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.RateControlMode = RateControlVBR
				opts.Deadline = DeadlineGoodQuality
				opts.CpuUsed = 4
				opts.LookaheadFrames = 8
				opts.AutoAltRef = true
				return opts
			}(),
			extraArgs: []string{"--end-usage=vbr", "--lag-in-frames=8", "--auto-alt-ref=1"},
			script: runtimeControlScript(frames, map[int]string{
				2: "arnrmax:7+arnrstrength:6+arnrtype:3",
				7: "arnrmax:3+arnrstrength:1+arnrtype:1",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(7, 6, 3))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(3, 1, 1))
				},
			},
		},
		{
			name: "arnr-runtime-auto-alt-ref-maxframes-only",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.RateControlMode = RateControlVBR
				opts.Deadline = DeadlineGoodQuality
				opts.CpuUsed = 4
				opts.LookaheadFrames = 8
				opts.AutoAltRef = true
				opts.ARNRMaxFrames = 7
				opts.ARNRStrength = 6
				opts.ARNRType = 3
				return opts
			}(),
			extraArgs: []string{"--end-usage=vbr", "--lag-in-frames=8", "--auto-alt-ref=1", "--arnr-maxframes=7", "--arnr-strength=6", "--arnr-type=3"},
			script: runtimeControlScript(frames, map[int]string{
				7: "arnrmax:3+arnrstrength:6+arnrtype:3",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(3, 6, 3))
				},
			},
		},
		{
			name: "arnr-runtime-auto-alt-ref-strength-only",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.RateControlMode = RateControlVBR
				opts.Deadline = DeadlineGoodQuality
				opts.CpuUsed = 4
				opts.LookaheadFrames = 8
				opts.AutoAltRef = true
				opts.ARNRMaxFrames = 7
				opts.ARNRStrength = 6
				opts.ARNRType = 3
				return opts
			}(),
			extraArgs: []string{"--end-usage=vbr", "--lag-in-frames=8", "--auto-alt-ref=1", "--arnr-maxframes=7", "--arnr-strength=6", "--arnr-type=3"},
			script: runtimeControlScript(frames, map[int]string{
				7: "arnrmax:7+arnrstrength:1+arnrtype:3",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(7, 1, 3))
				},
			},
		},
		{
			name: "arnr-runtime-auto-alt-ref-type-only",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.RateControlMode = RateControlVBR
				opts.Deadline = DeadlineGoodQuality
				opts.CpuUsed = 4
				opts.LookaheadFrames = 8
				opts.AutoAltRef = true
				opts.ARNRMaxFrames = 7
				opts.ARNRStrength = 6
				opts.ARNRType = 3
				return opts
			}(),
			extraArgs: []string{"--end-usage=vbr", "--lag-in-frames=8", "--auto-alt-ref=1", "--arnr-maxframes=7", "--arnr-strength=6", "--arnr-type=3"},
			script: runtimeControlScript(frames, map[int]string{
				7: "arnrmax:7+arnrstrength:6+arnrtype:1",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(7, 6, 1))
				},
			},
		},
		{
			name: "keyframe-disabled-runtime-toggle",
			fx:   panning32,
			opts: baseOpts(panning32),
			script: runtimeControlScript(frames, map[int]string{
				3: "kfdisabled:1+kfmin:0+kfmax:120",
				8: "kfdisabled:0+kfmin:0+kfmax:4",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(false)", e.SetAdaptiveKeyFrames(false))
					mustRuntime(t, "SetKeyFrameInterval(0)", e.SetKeyFrameInterval(0))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(true)", e.SetAdaptiveKeyFrames(true))
					mustRuntime(t, "SetKeyFrameInterval(4)", e.SetKeyFrameInterval(4))
				},
			},
		},
		{
			name: "adaptive-keyframes-scene-disable-reenable",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.AdaptiveKeyFrames = true
				return opts
			}(),
			extraArgs: []string{"--kf-min-dist=0", "--kf-max-dist=999"},
			script: runtimeControlScript(frames, map[int]string{
				3: "kfdisabled:1+kfmin:0+kfmax:999",
				8: "kfdisabled:0+kfmin:0+kfmax:999",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(false)", e.SetAdaptiveKeyFrames(false))
					mustRuntime(t, "SetKeyFrameInterval(0)", e.SetKeyFrameInterval(0))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(true)", e.SetAdaptiveKeyFrames(true))
					mustRuntime(t, "SetKeyFrameInterval(999)", e.SetKeyFrameInterval(999))
				},
			},
		},
		{
			name: "force-keyframe-while-keyframes-disabled",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: []EncodeFlags{
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				2: "kfdisabled:1+kfmin:0+kfmax:120",
				7: "kfdisabled:0+kfmin:999+kfmax:999",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(false)", e.SetAdaptiveKeyFrames(false))
					mustRuntime(t, "SetKeyFrameInterval(0)", e.SetKeyFrameInterval(0))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(true)", e.SetAdaptiveKeyFrames(true))
					mustRuntime(t, "SetKeyFrameInterval(999)", e.SetKeyFrameInterval(999))
				},
			},
		},
		{
			name: "force-keyframe-method-while-keyframes-disabled",
			fx:   panning32,
			opts: baseOpts(panning32),
			libvpxFlags: []EncodeFlags{
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				2: "kfdisabled:1+kfmin:0+kfmax:120",
				7: "kfdisabled:0+kfmin:999+kfmax:999",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(false)", e.SetAdaptiveKeyFrames(false))
					mustRuntime(t, "SetKeyFrameInterval(0)", e.SetKeyFrameInterval(0))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					e.ForceKeyFrame()
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetKeyFrameInterval(999)", e.SetKeyFrameInterval(999))
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, sources, tc.flags, tc.apply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
			libvpxFlags := tc.flags
			if tc.libvpxFlags != nil {
				libvpxFlags = tc.libvpxFlags
			}
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources, libvpxFlags, extraArgs)
			assertSegmentByteParity(t, "runtime-controls", govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}
