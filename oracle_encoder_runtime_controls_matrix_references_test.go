//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"strings"
	"testing"
)

func TestOracleEncoderStreamByteParityRuntimeControlsReferences(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime-control byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

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
			name: "rtc-external-active-roi-runtime-cross",
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
				1: "active:checker+roi:border1",
				4: "rtc:1",
				8: "active:off+roi:off",
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
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "set-reference-last-before-inter",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				return opts
			}(),
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:last:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					ref := encoderValidationPanningFrame(e.opts.Width, e.opts.Height, 8)
					mustRuntime(t, "SetReferenceFrame(last)", e.SetReferenceFrame(ReferenceLast, ref))
				},
			},
		},
		{
			name: "set-reference-last-before-inter-noise3",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--target-bitrate=1200", "--noise-sensitivity=3"},
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:last:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceLast, 8, "last"),
			},
		},
		{
			name: "set-reference-golden-before-inter-noise3",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--target-bitrate=1200", "--noise-sensitivity=3"},
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:golden:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceGolden, 8, "golden"),
			},
		},
		{
			name: "set-reference-altref-before-inter-noise3",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--target-bitrate=1200", "--noise-sensitivity=3"},
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceLast | EncodeNoReferenceGolden,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:altref:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceAltRef, 8, "altref"),
			},
		},
		{
			name: "roi-noise3-threads2-runtime-cross",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				opts.Threads = 2
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3", "--threads=2"},
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				7: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-checker-noise3-force-keyframe-clear",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:checker",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("checker"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-left1-noise3-force-keyframe-clear",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:left1",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("left1"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-border1-noise3-force-keyframe-clear",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-quadrants-noise3-force-keyframe-clear",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:quadrants",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("quadrants"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "set-reference-golden-before-inter",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				return opts
			}(),
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:golden:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceGolden, 8, "golden"),
			},
		},
		{
			name: "set-reference-altref-before-inter",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				return opts
			}(),
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceLast | EncodeNoReferenceGolden,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:altref:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceAltRef, 8, "altref"),
			},
		},
		{
			name: "set-reference-repeated-last-and-golden",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				return opts
			}(),
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
				0,
				0,
				EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:last:panning:8",
				4: "setref:golden:panning:12",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceLast, 8, "last"),
				4: setReferencePanningApply(ReferenceGolden, 12, "golden"),
			},
		},
		{
			name: "temporal-layer-id-manual-transition",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 1200
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled:                true,
					Mode:                   TemporalLayeringTwoLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{720, 1200},
				}
				return opts
			}(),
			flags:  temporalTwoLayerFlags(frames),
			script: temporalLayerIDScript(frames, temporalLayerOverrideIDs),
			apply:  temporalLayerIDApply(temporalLayerOverrideIDs),
			extraArgs: []string{
				"--temporal-layers=2",
				"--temporal-bitrates=720,1200",
				"--temporal-decimators=2,1",
				"--temporal-periodicity=2",
				"--temporal-layer-ids=0,1",
			},
		},
		{
			name: "temporal-layer-id-disabled-noop",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "tlid:0",
				7: "tlid:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalLayerID(0)", e.SetTemporalLayerID(0))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalLayerID(0)", e.SetTemporalLayerID(0))
				},
			},
		},
		{
			name: "temporal-layer-id-manual-three-layer-transition",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 1200
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled:                true,
					Mode:                   TemporalLayeringThreeLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{480, 720, 1200},
				}
				return opts
			}(),
			flags:  temporalThreeLayerFlags(frames),
			script: temporalLayerIDScript(frames, temporalThreeLayerOverrideIDs),
			apply:  temporalLayerIDApply(temporalThreeLayerOverrideIDs),
			extraArgs: []string{
				"--temporal-layers=3",
				"--temporal-bitrates=480,720,1200",
				"--temporal-decimators=4,2,1",
				"--temporal-periodicity=4",
				"--temporal-layer-ids=0,2,1,2",
			},
		},
		{
			name: "roi-map-quadrants-toggle",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				0: "roi:quadrants",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(quadrants)", e.SetROIMap(quadrantROIMap(e.opts.Width, e.opts.Height)))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-map-border-force-keyframe-toggle",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			flags: []EncodeFlags{
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				0: "roi:border1",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: roiMapApply("border1"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-map-pattern-switches",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				0: "roi:checker",
				3: "roi:left1",
				6: "roi:border1",
				9: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: roiMapApply("checker"),
				3: roiMapApply("left1"),
				6: roiMapApply("border1"),
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-map-custom-checker-set-clear",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				0: "roicustom:checker:0/-10/0/0:0/0/0/0:0/0/0/0",
				9: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(simple-checker)", e.SetROIMap(simpleCheckerROIMap(e.opts.Width, e.opts.Height)))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-map-custom-data-switches",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				0: "roicustom:checker:0/-10/0/0:0/0/0/0:0/0/0/0",
				5: "roicustom:quadrants:0/-10/8/-20:0/-3/2/5:0/500/0/1200",
				9: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(simple-checker)", e.SetROIMap(simpleCheckerROIMap(e.opts.Width, e.opts.Height)))
				},
				5: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(custom-quadrants)", e.SetROIMap(customQuadrantROIMap(e.opts.Width, e.opts.Height)))
				},
				9: func(t *testing.T, e *VP8Encoder) {
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
