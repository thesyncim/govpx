//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

func TestVP8OracleEncoderStreamByteParityRuntimeControlsMaps(t *testing.T) {
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
			name: "active-map-checker-toggle",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					rows := encoderMacroblockRows(e.opts.Height)
					cols := encoderMacroblockCols(e.opts.Width)
					mustRuntime(t, "SetActiveMap(checker)", e.SetActiveMap(activeMapPattern("checker", rows, cols), rows, cols))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-left-off-toggle-cpu-3",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.CpuUsed = -3
				return opts
			}(),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:left-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("left-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-right-off-toggle-cpu-3",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.CpuUsed = -3
				return opts
			}(),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:right-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("right-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-border-off-toggle-cpu-3",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.CpuUsed = -3
				return opts
			}(),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:border-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("border-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-checker-force-keyframe-toggle",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: []EncodeFlags{
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				7: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name:  "active-map-no-upd-last-no-ref-gf-arf",
			fx:    panning64,
			opts:  baseOpts(panning64),
			flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				8: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-checker-toggle-noise3-threads2",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.NoiseSensitivity = 3
				opts.Threads = 2
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3", "--threads=2"},
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-checker-noise3-force-keyframe-clear",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-left-off-noise3-force-keyframe-clear",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:left-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("left-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-right-off-noise3-force-keyframe-clear",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:right-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("right-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-border-off-noise3-force-keyframe-clear",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:border-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("border-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-pattern-switches",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:left-off",
				4: "active:right-off",
				7: "active:border-off",
				9: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("left-off"),
				4: activeMapApply("right-off"),
				7: activeMapApply("border-off"),
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "rtc-external-rate-control-runtime-toggle",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				2: "rtc:1",
				6: "rtc:0",
				9: "rtc:1",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
			},
		},
		{
			name: "rtc-external-disable-sticky-force-keyframe",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				2: "rtc:1",
				5: "rtc:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				5: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
			},
		},
		{
			name:  "rtc-external-no-ref-all-no-upd-entropy",
			fx:    panning64,
			opts:  baseOpts(panning64),
			flags: repeatFlag(frames-1, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef|EncodeNoUpdateEntropy),
			script: runtimeControlScript(frames, map[int]string{
				1: "rtc:1",
				8: "rtc:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
			},
		},
		{
			name: "active-map-roi-runtime-cross",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker+roi:border1",
				8: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
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
		},
		{
			name:  "roi-border-no-upd-entropy-no-upd-all",
			fx:    segmented64,
			opts:  baseOpts(segmented64),
			flags: repeatFlag(frames-1, EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				8: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "active-map-before-roi-runtime-order",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				2: "roi:border1",
				8: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				2: roiMapApply("border1"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-before-active-map-runtime-order",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				2: "active:checker",
				8: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				2: activeMapApply("checker"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "active-roi-disable-roi-first-order",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1:  "active:checker+roi:border1",
				8:  "roi:off",
				10: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
				10: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-roi-disable-active-first-order",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1:  "active:checker+roi:border1",
				8:  "active:off",
				10: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
				10: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-pattern-switch-under-active-map",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1:  "active:checker+roi:checker",
				4:  "roi:left1",
				7:  "roi:border1",
				10: "roi:off+active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("checker")(t, e)
				},
				4: roiMapApply("left1"),
				7: roiMapApply("border1"),
				10: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "rtc-external-active-map-runtime-cross",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				4: "rtc:1",
				8: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-roi-rtc-disable-order-cross",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				1:  "active:checker+roi:border1",
				4:  "rtc:1",
				7:  "rtc:0",
				10: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
				10: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "rtc-external-roi-runtime-cross",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				4: "rtc:1",
				8: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "rtc-external-roi-disable-on-force-keyframe",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			flags: []EncodeFlags{
				0, 0, 0, 0,
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				4: "rtc:1",
				8: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
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
