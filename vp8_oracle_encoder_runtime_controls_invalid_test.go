//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func TestVP8OracleEncoderStreamByteParityInvalidRuntimeControlsNoop(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run invalid runtime-control byte-parity gate")
	}
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 10
		width      = 64
		height     = 64
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
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
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	apply := map[int]func(*testing.T, *VP8Encoder){
		1: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			expectInvalidRuntime(t, "SetRateControl(minq>maxq)", ErrInvalidQuantizer, e.SetRateControl(RateControlConfig{
				Mode:              RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      50,
				MaxQuantizer:      10,
			}))
		},
		2: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			expectInvalidRuntime(t, "SetRealtimeTarget(width-only)", ErrInvalidConfig, e.SetRealtimeTarget(RealtimeTarget{Width: 32}))
		},
		3: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			expectInvalidRuntime(t, "SetCQLevel(64)", ErrInvalidQuantizer, e.SetCQLevel(64))
		},
		4: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			expectInvalidRuntime(t, "SetTemporalLayerID(1)", ErrInvalidConfig, e.SetTemporalLayerID(1))
		},
		5: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			rows := encoderMacroblockRows(e.opts.Height)
			cols := encoderMacroblockCols(e.opts.Width)
			expectInvalidRuntime(t, "SetActiveMap(wrong rows)", ErrInvalidConfig, e.SetActiveMap(make([]uint8, rows*cols), rows+1, cols))
		},
		6: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			rows := encoderMacroblockRows(e.opts.Height)
			cols := encoderMacroblockCols(e.opts.Width)
			expectInvalidRuntime(t, "SetROIMap(wrong rows)", ErrInvalidConfig, e.SetROIMap(&ROIMap{
				Enabled:   true,
				Rows:      rows + 1,
				Cols:      cols,
				SegmentID: make([]uint8, rows*cols),
			}))
		},
		7: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			wrongSize := encoderValidationPanningFrame(width/2, height, 0)
			expectInvalidRuntime(t, "SetReferenceFrame(wrong size)", ErrInvalidConfig, e.SetReferenceFrame(ReferenceLast, wrongSize))
		},
	}

	govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, apply)
	libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "invalid-runtime-controls-noop", opts, targetKbps, sources, nil, nil)
	assertSegmentByteParity(t, "invalid-runtime-controls-noop", govpxFrames, libvpxFrames, 0)
}

func TestVP8OracleEncoderStreamByteParityRuntimeControlsLongTail(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run long-tail runtime-control byte-parity gate")
	}
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 32
	)
	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
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
	type longTailCase struct {
		name      string
		fx        fixture
		opts      EncoderOptions
		flags     []EncodeFlags
		script    []string
		apply     map[int]func(*testing.T, *VP8Encoder)
		extraArgs []string
	}
	cases := []longTailCase{
		{
			name:   "active-roi-toggle-state-drift",
			fx:     segmented64,
			opts:   baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{2: "active:checker+roi:border1", 16: "active:off+roi:off"}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				16: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name:   "reference-and-denoise-state-drift",
			fx:     panning64,
			opts:   baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{1: "noise:4", 5: "setref:last:panning:9", 18: "noise:0", 24: "setref:golden:panning:10"}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(4)", e.SetNoiseSensitivity(4))
				},
				5: setReferencePanningApply(ReferenceLast, 9, "last"),
				18: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
				24: setReferencePanningApply(ReferenceGolden, 10, "golden"),
			},
		},
		{
			name:   "mode-control-state-drift",
			fx:     panning64,
			opts:   baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{4: "static:500+screen:1+sharpness:4", 12: "deadline:good+cpu:4", 20: "deadline:rt+cpu:-8", 24: "static:0+screen:0+sharpness:0"}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(500)", e.SetStaticThreshold(500))
					mustRuntime(t, "SetScreenContentMode(1)", e.SetScreenContentMode(1))
					mustRuntime(t, "SetSharpness(4)", e.SetSharpness(4))
				},
				12: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline(good)", e.SetDeadline(DeadlineGoodQuality))
					mustRuntime(t, "SetCPUUsed(4)", e.SetCPUUsed(4))
				},
				20: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline(rt)", e.SetDeadline(DeadlineRealtime))
					mustRuntime(t, "SetCPUUsed(-8)", e.SetCPUUsed(-8))
				},
				24: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(0)", e.SetStaticThreshold(0))
					mustRuntime(t, "SetScreenContentMode(0)", e.SetScreenContentMode(0))
					mustRuntime(t, "SetSharpness(0)", e.SetSharpness(0))
				},
			},
		},
		{
			name:   "temporal-enable-disable-state-drift",
			fx:     panning64,
			opts:   baseOpts(panning64),
			flags:  temporalScalabilityEnableDisableFlags(frames),
			script: temporalScalabilityEnableDisableScript(frames),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(two-layer)", e.SetTemporalScalability(runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)))
					mustRuntime(t, "SetTemporalLayerID(0)", e.SetTemporalLayerID(0))
				},
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalLayerID(1)", e.SetTemporalLayerID(1))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalLayerID(0)", e.SetTemporalLayerID(0))
				},
				5: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalLayerID(1)", e.SetTemporalLayerID(1))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
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
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "runtime-long-tail-"+tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources, tc.flags, extraArgs)
			assertSegmentByteParity(t, "runtime-long-tail-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}
