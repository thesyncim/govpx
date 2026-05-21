//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

func TestVP8OracleEncoderStreamByteParityRuntimeTemporalControlCrosses(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime temporal-control byte-parity gate")
	}
	driver := coracletest.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	segmented64 := fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}

	baseOpts := func(fx fixture, cpuUsed int) EncoderOptions {
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
			CpuUsed:           cpuUsed,
			Tuning:            TunePSNR,
		}
	}
	temporalOpts := func(fx fixture, cpuUsed int, mode TemporalLayeringMode) EncoderOptions {
		opts := baseOpts(fx, cpuUsed)
		opts.TemporalScalability = runtimeTemporalConfig(mode, targetKbps)
		return opts
	}
	autoTemporalOpts := func(fx fixture, cpuUsed int, mode TemporalLayeringMode) EncoderOptions {
		opts := baseOpts(fx, cpuUsed)
		opts.TemporalScalability = TemporalScalabilityConfig{Enabled: true, Mode: mode}
		return opts
	}

	type temporalCase struct {
		name       string
		fx         fixture
		frames     int
		opts       EncoderOptions
		flags      []EncodeFlags
		script     []string
		apply      map[int]func(*testing.T, *VP8Encoder)
		extraArgs  []string
		matchLimit int
	}

	twoLayerEnableScript := temporalScalabilityWindowScript(12, TemporalLayeringTwoLayers, 2, 12, runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps))
	twoLayerDisableScript := runtimeTemporalDisableScript(12, TemporalLayeringTwoLayers, 6, targetKbps)

	cases := []temporalCase{
		{
			name:   "two-layer-enable-only",
			fx:     panning64,
			frames: 12,
			opts:   baseOpts(panning64, 0),
			flags:  temporalScalabilityWindowFlags(12, TemporalLayeringTwoLayers, 2, 12),
			script: twoLayerEnableScript,
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: runtimeTemporalApply(TemporalLayeringTwoLayers, targetKbps, "two-layer"),
			},
		},
		{
			name:      "two-layer-disable-only",
			fx:        panning64,
			frames:    12,
			opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
			flags:     temporalScalabilityWindowFlags(12, TemporalLayeringTwoLayers, 0, 6),
			script:    twoLayerDisableScript,
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
			apply: map[int]func(*testing.T, *VP8Encoder){
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
		{
			name:   "three-layer-enable-disable-only",
			fx:     panning64,
			frames: 12,
			opts:   baseOpts(panning64, 0),
			flags:  temporalScalabilityWindowFlags(12, TemporalLayeringThreeLayers, 2, 8),
			script: temporalScalabilityWindowScript(12, TemporalLayeringThreeLayers, 2, 8, runtimeTemporalControlToken(TemporalLayeringThreeLayers, targetKbps)),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: runtimeTemporalApply(TemporalLayeringThreeLayers, targetKbps, "three-layer"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
		{
			name:   "two-layer-with-sync-enable-disable-only",
			fx:     panning64,
			frames: 12,
			opts:   baseOpts(panning64, 0),
			flags:  temporalScalabilityWindowFlags(12, TemporalLayeringTwoLayersWithSync, 2, 8),
			script: temporalScalabilityWindowScript(12, TemporalLayeringTwoLayersWithSync, 2, 8, runtimeTemporalControlToken(TemporalLayeringTwoLayersWithSync, targetKbps)),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: runtimeTemporalApply(TemporalLayeringTwoLayersWithSync, targetKbps, "two-layer-with-sync"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
	}

	for _, tc := range []struct {
		name string
		mode TemporalLayeringMode
	}{
		{name: "three-layer-no-inter-layer-prediction-enable-disable-only", mode: TemporalLayeringThreeLayersNoInterLayerPrediction},
		{name: "three-layer-layer-one-prediction-enable-disable-only", mode: TemporalLayeringThreeLayersLayerOnePrediction},
		{name: "three-layer-altref-sync-enable-disable-only", mode: TemporalLayeringThreeLayersAltRefWithSync},
		{name: "three-layer-one-reference-enable-disable-only", mode: TemporalLayeringThreeLayersOneReference},
	} {
		modeName := tc.name
		mode := tc.mode
		cases = append(cases, temporalCase{
			name:   modeName,
			fx:     panning64,
			frames: 12,
			opts:   baseOpts(panning64, 0),
			flags:  temporalScalabilityWindowFlags(12, mode, 2, 8),
			script: temporalScalabilityWindowScript(12, mode, 2, 8, runtimeTemporalControlToken(mode, targetKbps)),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: runtimeTemporalApply(mode, targetKbps, modeName),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		})
	}

	for _, cpuUsed := range []int{0, -3, -8} {
		frames := 18
		mode := TemporalLayeringFiveLayers
		script := temporalScalabilityWindowScript(frames, mode, 2, frames, runtimeTemporalControlToken(mode, targetKbps))
		cases = append(cases, temporalCase{
			name:   "five-layer-enable-only-cpu" + strconv.Itoa(cpuUsed),
			fx:     panning64,
			frames: frames,
			opts:   baseOpts(panning64, cpuUsed),
			flags:  temporalScalabilityWindowFlags(frames, mode, 2, frames),
			script: script,
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: runtimeTemporalApply(mode, targetKbps, "five-layer"),
			},
		})
	}

	{
		frames := 18
		mode := TemporalLayeringFiveLayers
		cases = append(cases, temporalCase{
			name:      "five-layer-disable-only-cpu-3",
			fx:        panning64,
			frames:    frames,
			opts:      temporalOpts(panning64, -3, mode),
			flags:     temporalScalabilityWindowFlags(frames, mode, 0, 10),
			script:    runtimeTemporalDisableScript(frames, mode, 10, targetKbps),
			extraArgs: runtimeTemporalExtraArgs(mode, targetKbps),
			apply: map[int]func(*testing.T, *VP8Encoder){
				10: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		})
	}

	{
		frames := 18
		cases = append(cases,
			temporalCase{
				name:      "two-layer-to-five-layer-transition",
				fx:        panning64,
				frames:    frames,
				opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
				flags:     temporalScalabilityModeSwitchFlags(frames, TemporalLayeringTwoLayers, TemporalLayeringFiveLayers, 8),
				script:    temporalScalabilityModeSwitchScript(frames, TemporalLayeringTwoLayers, TemporalLayeringFiveLayers, 8, targetKbps),
				extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
				apply: map[int]func(*testing.T, *VP8Encoder){
					8: runtimeTemporalApply(TemporalLayeringFiveLayers, targetKbps, "five-layer"),
				},
			},
			temporalCase{
				name:      "five-layer-to-two-layer-transition",
				fx:        panning64,
				frames:    frames,
				opts:      temporalOpts(panning64, 0, TemporalLayeringFiveLayers),
				flags:     temporalScalabilityModeSwitchFlags(frames, TemporalLayeringFiveLayers, TemporalLayeringTwoLayers, 10),
				script:    temporalScalabilityModeSwitchScript(frames, TemporalLayeringFiveLayers, TemporalLayeringTwoLayers, 10, targetKbps),
				extraArgs: runtimeTemporalExtraArgs(TemporalLayeringFiveLayers, targetKbps),
				apply: map[int]func(*testing.T, *VP8Encoder){
					10: runtimeTemporalApply(TemporalLayeringTwoLayers, targetKbps, "two-layer"),
				},
			},
		)
	}

	twoLayerScript := func(frames int) []string {
		return runtimeTemporalLayerIDScript(frames, TemporalLayeringTwoLayers)
	}
	twoLayerFlags := func(frames int) []EncodeFlags {
		return temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 0)
	}

	twoLayerCPUScript := twoLayerScript(12)
	appendRuntimeControl(twoLayerCPUScript, 4, "cpu:-3")
	appendRuntimeControl(twoLayerCPUScript, 8, "cpu:0")
	cases = append(cases, temporalCase{
		name:      "two-layer-cpu-used-roundtrip",
		fx:        panning64,
		frames:    12,
		opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    twoLayerCPUScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetCPUUsed(-3)", e.SetCPUUsed(-3))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetCPUUsed(0)", e.SetCPUUsed(0))
			},
		},
	})

	twoLayerDeadlineScript := twoLayerScript(12)
	appendRuntimeControl(twoLayerDeadlineScript, 4, "deadline:good")
	appendRuntimeControl(twoLayerDeadlineScript, 8, "deadline:rt")
	cases = append(cases, temporalCase{
		name:      "two-layer-deadline-good-rt-roundtrip",
		fx:        panning64,
		frames:    12,
		opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    twoLayerDeadlineScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetDeadline(good)", e.SetDeadline(DeadlineGoodQuality))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetDeadline(rt)", e.SetDeadline(DeadlineRealtime))
			},
		},
	})

	twoLayerBestDeadlineScript := twoLayerScript(12)
	appendRuntimeControl(twoLayerBestDeadlineScript, 4, "deadline:best")
	appendRuntimeControl(twoLayerBestDeadlineScript, 8, "deadline:rt")
	cases = append(cases, temporalCase{
		name:      "two-layer-deadline-best-rt-roundtrip",
		fx:        panning64,
		frames:    12,
		opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    twoLayerBestDeadlineScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetDeadline(best)", e.SetDeadline(DeadlineBestQuality))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetDeadline(rt)", e.SetDeadline(DeadlineRealtime))
			},
		},
	})

	for _, tc := range []struct {
		name    string
		mode    RateControlMode
		cqLevel int
	}{
		{name: "two-layer-vbr", mode: RateControlVBR},
		{name: "two-layer-cq20", mode: RateControlCQ, cqLevel: 20},
		{name: "two-layer-q20", mode: RateControlQ, cqLevel: 20},
	} {
		rcMode := tc.mode
		cqLevel := tc.cqLevel
		cases = append(cases, temporalCase{
			name:   tc.name,
			fx:     panning64,
			frames: 12,
			opts: func() EncoderOptions {
				opts := temporalOpts(panning64, 0, TemporalLayeringTwoLayers)
				opts.RateControlMode = rcMode
				opts.CQLevel = cqLevel
				return opts
			}(),
			flags:     twoLayerFlags(12),
			script:    twoLayerScript(12),
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		})
	}

	fiveLayerCPUScript := runtimeTemporalLayerIDScript(18, TemporalLayeringFiveLayers)
	appendRuntimeControl(fiveLayerCPUScript, 6, "cpu:-3")
	appendRuntimeControl(fiveLayerCPUScript, 12, "cpu:0")
	cases = append(cases, temporalCase{
		name:      "five-layer-cpu-used-roundtrip",
		fx:        panning64,
		frames:    18,
		opts:      temporalOpts(panning64, 0, TemporalLayeringFiveLayers),
		flags:     temporalScalabilityReconfigureFlags(18, TemporalLayeringFiveLayers, 0),
		script:    fiveLayerCPUScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringFiveLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			6: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetCPUUsed(-3)", e.SetCPUUsed(-3))
			},
			12: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetCPUUsed(0)", e.SetCPUUsed(0))
			},
		},
	})

	dropScript := twoLayerScript(12)
	appendRuntimeControl(dropScript, 4, "drop:60")
	appendRuntimeControl(dropScript, 8, "drop:0")
	cases = append(cases, temporalCase{
		name:      "two-layer-frame-drop-toggle",
		fx:        panning64,
		frames:    12,
		opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    dropScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetFrameDropAllowed(true)", e.SetFrameDropAllowed(true))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetFrameDropAllowed(false)", e.SetFrameDropAllowed(false))
			},
		},
	})

	bitrateScript := twoLayerScript(12)
	appendRuntimeControl(bitrateScript, 4, "bitrate:400+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, 400))
	appendRuntimeControl(bitrateScript, 8, "bitrate:900+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, 900))
	cases = append(cases, temporalCase{
		name:      "two-layer-bitrate-reconfigure-low-high",
		fx:        panning64,
		frames:    12,
		opts:      autoTemporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    bitrateScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetBitrateKbps(400)", e.SetBitrateKbps(400))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetBitrateKbps(900)", e.SetBitrateKbps(900))
			},
		},
	})

	realtimeScript := twoLayerScript(12)
	appendRuntimeControl(realtimeScript, 4, "fps:24+bitrate:500+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, 500))
	appendRuntimeControl(realtimeScript, 8, "fps:30+bitrate:700+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps))
	cases = append(cases, temporalCase{
		name:      "two-layer-realtime-target-fps-bitrate-reconfigure",
		fx:        panning64,
		frames:    12,
		opts:      autoTemporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    realtimeScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRealtimeTarget(fps24-bitrate500)", e.SetRealtimeTarget(RealtimeTarget{FPS: 24, BitrateKbps: 500}))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRealtimeTarget(fps30-bitrate700)", e.SetRealtimeTarget(RealtimeTarget{FPS: 30, BitrateKbps: targetKbps}))
			},
		},
	})

	tokenERScript := twoLayerScript(12)
	cases = append(cases, temporalCase{
		name:   "two-layer-token8-er3",
		fx:     panning64,
		frames: 12,
		opts: func() EncoderOptions {
			opts := temporalOpts(panning64, 0, TemporalLayeringTwoLayers)
			opts.ErrorResilient = true
			opts.ErrorResilientPartitions = true
			opts.TokenPartitions = 3
			return opts
		}(),
		flags:     twoLayerFlags(12),
		script:    tokenERScript,
		extraArgs: append(runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps), "--error-resilient=3"),
	})

	screenStaticScript := twoLayerScript(12)
	cases = append(cases, temporalCase{
		name:   "two-layer-screen2-static500",
		fx:     panning64,
		frames: 12,
		opts: func() EncoderOptions {
			opts := temporalOpts(panning64, 0, TemporalLayeringTwoLayers)
			opts.ScreenContentMode = 2
			opts.StaticThreshold = 500
			return opts
		}(),
		flags:     twoLayerFlags(12),
		script:    screenStaticScript,
		extraArgs: append(runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps), "--screen-content-mode=2", "--static-thresh=500"),
	})

	noiseScript := twoLayerScript(12)
	cases = append(cases, temporalCase{
		name:   "two-layer-noise3",
		fx:     panning64,
		frames: 12,
		opts: func() EncoderOptions {
			opts := temporalOpts(panning64, 0, TemporalLayeringTwoLayers)
			opts.NoiseSensitivity = 3
			return opts
		}(),
		flags:     twoLayerFlags(12),
		script:    noiseScript,
		extraArgs: append(runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps), "--noise-sensitivity=3"),
	})

	rtcScript := twoLayerScript(12)
	appendRuntimeControl(rtcScript, 4, "rtc:1")
	appendRuntimeControl(rtcScript, 8, "rtc:0")
	cases = append(cases, temporalCase{
		name:      "two-layer-rtc-external-toggle",
		fx:        panning64,
		frames:    12,
		opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    rtcScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
			},
		},
	})

	activeScript := twoLayerScript(12)
	appendRuntimeControl(activeScript, 2, "active:checker")
	appendRuntimeControl(activeScript, 8, "active:off")
	cases = append(cases, temporalCase{
		name:      "two-layer-active-map-checker-toggle",
		fx:        panning64,
		frames:    12,
		opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    activeScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			2: activeMapApply("checker"),
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
			},
		},
	})

	roiScript := twoLayerScript(12)
	appendRuntimeControl(roiScript, 2, "roi:border1")
	appendRuntimeControl(roiScript, 8, "roi:off")
	cases = append(cases, temporalCase{
		name:      "two-layer-roi-border-toggle",
		fx:        segmented64,
		frames:    12,
		opts:      temporalOpts(segmented64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    roiScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			2: roiMapApply("border1"),
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
			},
		},
	})

	activeROIScript := twoLayerScript(12)
	appendRuntimeControl(activeROIScript, 2, "active:checker+roi:border1")
	appendRuntimeControl(activeROIScript, 8, "active:off+roi:off")
	cases = append(cases, temporalCase{
		name:      "two-layer-active-roi-toggle",
		fx:        segmented64,
		frames:    12,
		opts:      temporalOpts(segmented64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    activeROIScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			2: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				activeMapApply("checker")(t, e)
				roiMapApply("border1")(t, e)
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
			},
		},
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, tc.frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, sources, tc.flags, tc.apply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "runtime-temporal-"+tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources, tc.flags, extraArgs)
			assertSegmentByteParity(t, "runtime-temporal-"+tc.name, govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}
