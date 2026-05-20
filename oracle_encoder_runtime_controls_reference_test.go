//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

func TestOracleEncoderStreamByteParityRuntimeReferenceControlCrosses(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime reference-control byte-parity gate")
	}
	driver := coracletest.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 900
		frames     = 10
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 32, source: encoderValidationPanningFrame}
	panningOdd := fixture{name: "panning-33x17", w: 33, h: 17, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	segmented64 := fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}

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

	type referenceCase struct {
		name      string
		fx        fixture
		opts      EncoderOptions
		flags     []EncodeFlags
		script    []string
		apply     map[int]func(*testing.T, *VP8Encoder)
		extraArgs []string
	}

	cases := []referenceCase{
		{
			name: "set-golden-after-force-golden-only",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				2: EncodeForceGoldenFrame | EncodeNoUpdateLast | EncodeNoUpdateAltRef,
				3: EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			}),
			script: runtimeControlScript(frames, map[int]string{
				3: "setref:golden:panning:9",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: setReferencePanningApply(ReferenceGolden, 9, "golden"),
			},
		},
		{
			name: "set-altref-after-force-altref-only",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				2: EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden,
				3: EncodeNoReferenceLast | EncodeNoReferenceGolden,
			}),
			script: runtimeControlScript(frames, map[int]string{
				3: "setref:altref:panning:10",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: setReferencePanningApply(ReferenceAltRef, 10, "altref"),
			},
		},
		{
			name: "set-altref-after-hidden-altref-refresh",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				2: EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden,
				3: EncodeNoReferenceLast | EncodeNoReferenceGolden,
			}),
			script: runtimeControlScript(frames, map[int]string{
				3: "setref:altref:panning:11",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: setReferencePanningApply(ReferenceAltRef, 11, "altref"),
			},
		},
		{
			name: "set-last-after-force-golden-altref-same-frame",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				2: EncodeForceGoldenFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast,
				3: EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
			}),
			script: runtimeControlScript(frames, map[int]string{
				3: "setref:last:panning:9",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: setReferencePanningApply(ReferenceLast, 9, "last"),
			},
		},
		{
			name: "set-golden-under-two-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringTwoLayers)
				appendRuntimeControl(script, 4, "setref:golden:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceGolden, 12, "golden"),
			},
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		},
		{
			name: "set-last-under-two-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringTwoLayers)
				appendRuntimeControl(script, 4, "setref:last:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceLast, 12, "last"),
			},
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		},
		{
			name: "set-altref-under-two-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringTwoLayers)
				appendRuntimeControl(script, 4, "setref:altref:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceAltRef, 12, "altref"),
			},
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		},
		{
			name: "set-last-under-active-roi",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker+roi:border1",
				4: "setref:last:panning:12",
				7: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				4: setReferencePanningApply(ReferenceLast, 12, "last"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "set-golden-under-active-roi-odd-dims",
			fx:   panningOdd,
			opts: baseOpts(panningOdd),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				5: EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker+roi:border1",
				4: "setref:golden:panning:12",
				7: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				4: setReferencePanningApply(ReferenceGolden, 12, "golden"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "set-last-under-rtc-external",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "rtc:1",
				4: "setref:last:panning:12",
				7: "rtc:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				4: setReferencePanningApply(ReferenceLast, 12, "last"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
			},
		},
		{
			name: "set-altref-under-rtc-external-odd-dims",
			fx:   panningOdd,
			opts: baseOpts(panningOdd),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				5: EncodeNoReferenceLast | EncodeNoReferenceGolden,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "rtc:1",
				4: "setref:altref:panning:12",
				7: "rtc:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				4: setReferencePanningApply(ReferenceAltRef, 12, "altref"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
			},
		},
		{
			name: "set-golden-under-rtc-external",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				5: EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "rtc:1",
				4: "setref:golden:panning:12",
				7: "rtc:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				4: setReferencePanningApply(ReferenceGolden, 12, "golden"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
			},
		},
		{
			name: "set-altref-under-rtc-external",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				5: EncodeNoReferenceLast | EncodeNoReferenceGolden,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "rtc:1",
				4: "setref:altref:panning:12",
				7: "rtc:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				4: setReferencePanningApply(ReferenceAltRef, 12, "altref"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
			},
		},
		{
			name: "set-last-under-lookahead-auto-altref-odd-dims",
			fx:   panningOdd,
			opts: func() EncoderOptions {
				opts := baseOpts(panningOdd)
				opts.LookaheadFrames = 4
				opts.AutoAltRef = true
				return opts
			}(),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				1: EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				4: "setref:last:panning:12",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceLast, 12, "last"),
			},
			extraArgs: []string{"--lag-in-frames=4", "--auto-alt-ref=1"},
		},
		{
			name: "set-last-under-lookahead-auto-altref",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.LookaheadFrames = 4
				opts.AutoAltRef = true
				return opts
			}(),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				1: EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				4: "setref:last:panning:12",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceLast, 12, "last"),
			},
			extraArgs: []string{"--lag-in-frames=4", "--auto-alt-ref=1"},
		},
		{
			name: "set-golden-under-lookahead-auto-altref",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.LookaheadFrames = 4
				opts.AutoAltRef = true
				return opts
			}(),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				1: EncodeNoReferenceLast | EncodeNoReferenceAltRef,
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				4: "setref:golden:panning:12",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceGolden, 12, "golden"),
			},
			extraArgs: []string{"--lag-in-frames=4", "--auto-alt-ref=1"},
		},
		{
			name: "set-altref-under-lookahead-auto-altref",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.LookaheadFrames = 4
				opts.AutoAltRef = true
				return opts
			}(),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				1: EncodeNoReferenceLast | EncodeNoReferenceGolden,
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				4: "setref:altref:panning:12",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceAltRef, 12, "altref"),
			},
			extraArgs: []string{"--lag-in-frames=4", "--auto-alt-ref=1"},
		},
		{
			name: "set-last-under-three-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringThreeLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringThreeLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringThreeLayers)
				appendRuntimeControl(script, 4, "setref:last:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceLast, 12, "last"),
			},
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringThreeLayers, targetKbps),
		},
		{
			name: "set-golden-under-three-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringThreeLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringThreeLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringThreeLayers)
				appendRuntimeControl(script, 4, "setref:golden:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceGolden, 12, "golden"),
			},
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringThreeLayers, targetKbps),
		},
		{
			name: "set-altref-under-three-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringThreeLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringThreeLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringThreeLayers)
				appendRuntimeControl(script, 4, "setref:altref:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceAltRef, 12, "altref"),
			},
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringThreeLayers, targetKbps),
		},
		{
			name: "set-altref-after-runtime-resize",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				5: EncodeNoReferenceLast | EncodeNoReferenceGolden,
			}),
			script: runtimeControlScript(frames, map[int]string{
				4: "resize:32x32",
				5: "setref:altref:panning:13",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(32x32)", e.SetRealtimeTarget(RealtimeTarget{Width: 32, Height: 32}))
				},
				5: setReferencePanningApply(ReferenceAltRef, 13, "altref"),
			},
		},
		{
			name: "set-last-after-runtime-resize",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				5: EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
			}),
			script: runtimeControlScript(frames, map[int]string{
				4: "resize:32x32",
				5: "setref:last:panning:13",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(32x32)", e.SetRealtimeTarget(RealtimeTarget{Width: 32, Height: 32}))
				},
				5: setReferencePanningApply(ReferenceLast, 13, "last"),
			},
		},
		{
			name: "set-golden-after-runtime-resize",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				5: EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			}),
			script: runtimeControlScript(frames, map[int]string{
				4: "resize:32x32",
				5: "setref:golden:panning:13",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(32x32)", e.SetRealtimeTarget(RealtimeTarget{Width: 32, Height: 32}))
				},
				5: setReferencePanningApply(ReferenceGolden, 13, "golden"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				w, h := tc.fx.w, tc.fx.h
				if strings.Contains(tc.name, "after-runtime-resize") && i >= 4 {
					w, h = 32, 32
				}
				sources[i] = tc.fx.source(w, h, i)
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, sources, tc.flags, tc.apply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "runtime-reference-"+tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources, tc.flags, extraArgs)
			assertSegmentByteParity(t, "runtime-reference-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}
