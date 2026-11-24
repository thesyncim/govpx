//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil"
	"testing"
)

func vp8OracleRuntimeControlFuzzMatchLimit(_ string) int {
	// Runtime-control fuzz corpus replay is strict: every retained regression
	// seed must match libvpx for all frames. Historical prefix tolerances have
	// been removed so this helper is intentionally boring and exists only as a
	// named policy point for future corpus triage.
	return 0
}

type vp8OracleRuntimeControlFuzzCase struct {
	name       string
	opts       EncoderOptions
	targetKbps int
	sources    []Image
	flags      []EncodeFlags
	script     []string
	apply      map[int]func(*testing.T, *VP8Encoder)
	extraArgs  []string
	copyRefLog bool
}

func vp8OracleRuntimeControlFuzzCaseFromBytes(data []byte) vp8OracleRuntimeControlFuzzCase {
	if string(data) == "02000y0" {
		return vp8OracleRuntimeFPSBitrateReproFuzzCase()
	}
	if string(data) == "\xff" {
		return vp8OracleRuntimeKeyFrameIntervalZeroReproFuzzCase()
	}
	if bytes.Equal(data, vp8OracleRuntimeFullPermutationSeed) {
		r := testutil.NewByteCursor(data[1:])
		return vp8OracleRuntimeFullControlPermutationFuzzCase(&r)
	}
	r := testutil.NewByteCursor(data)
	switch r.Pick(3) {
	case 1:
		return vp8OracleRuntimeTemporalFuzzCase(&r)
	case 2:
		return vp8OracleRuntimeInvalidNoopFuzzCase(&r)
	default:
		return vp8OracleRuntimeGeneralFuzzCase(&r)
	}
}

func vp8OracleRuntimeKeyFrameIntervalZeroReproFuzzCase() vp8OracleRuntimeControlFuzzCase {
	targetKbps := 700
	frames := 8
	opts := vp8OracleRuntimeBaseFuzzOptions(64, 64, targetKbps, 0)
	script := runtimeControlScript(frames, map[int]string{
		2: "kfmin:0+kfmax:0",
		3: "kfdisabled:1+kfmin:0+kfmax:0",
	})
	apply := map[int]func(*testing.T, *VP8Encoder){
		2: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetKeyFrameInterval(0)", e.SetKeyFrameInterval(0))
		},
		3: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetAdaptiveKeyFrames(false)", e.SetAdaptiveKeyFrames(false))
			mustRuntime(t, "SetKeyFrameInterval(0)", e.SetKeyFrameInterval(0))
		},
	}
	return vp8OracleRuntimeControlFuzzCase{
		name:       "keyframe-interval-zero-repro",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    vp8OracleRuntimeFuzzSources(opts.Width, opts.Height, frames, 0),
		flags:      nil,
		script:     script,
		apply:      apply,
	}
}

func vp8OracleRuntimeBaseFuzzOptions(width, height, targetKbps, cpuUsed int) EncoderOptions {
	return EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           cpuUsed,
		KeyFrameInterval:  999,
		Tuning:            TunePSNR,
	}
}

func vp8OracleRuntimeFuzzSources(width, height, frames, kind int) []Image {
	sources := make([]Image, frames)
	for i := range sources {
		if kind&1 != 0 {
			sources[i] = encoderValidationSegmentedFrame(width, height, i)
		} else {
			sources[i] = encoderValidationPanningFrame(width, height, i)
		}
	}
	return sources
}

func vp8OracleRuntimeFPSBitrateReproFuzzCase() vp8OracleRuntimeControlFuzzCase {
	targetKbps := 300
	frames := 9
	opts := vp8OracleRuntimeBaseFuzzOptions(64, 64, targetKbps, 0)
	script := []string{
		"-",
		"-",
		"bitrate:300",
		"-",
		"bitrate:300+fps:15+minq:8+maxq:48+drop:0",
		"-",
		"-",
		"bitrate:300",
		"-",
	}
	flags := []EncodeFlags{
		0,
		EncodeForceKeyFrame,
		0,
		EncodeForceKeyFrame,
		0,
		EncodeNoUpdateEntropy,
		EncodeForceKeyFrame,
		0,
		EncodeForceKeyFrame,
	}
	apply := map[int]func(*testing.T, *VP8Encoder){
		2: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetBitrateKbps(300)", e.SetBitrateKbps(300))
		},
		4: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetRealtimeTarget(fps15-bitrate300)", e.SetRealtimeTarget(RealtimeTarget{
				BitrateKbps:  300,
				FPS:          15,
				MinQuantizer: 8,
				MaxQuantizer: 48,
				FrameDrop:    RealtimeFrameDropDisabled,
			}))
		},
		7: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetBitrateKbps(300)", e.SetBitrateKbps(300))
		},
	}
	return vp8OracleRuntimeControlFuzzCase{
		name:       "fps-bitrate-repro",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    vp8OracleRuntimeFuzzSources(opts.Width, opts.Height, frames, 1),
		flags:      flags,
		script:     script,
		apply:      apply,
	}
}

func vp8OracleRuntimeGeneralFuzzCase(r *testutil.ByteCursor) vp8OracleRuntimeControlFuzzCase {
	dims := [...]struct {
		w int
		h int
	}{
		{16, 16},
		{32, 16},
		{64, 64},
	}
	speeds := [...]int{0, -3, -8}
	targets := [...]int{300, 700, 1200}
	dim := dims[r.Pick(len(dims))]
	targetKbps := targets[r.Pick(len(targets))]
	frames := 6 + r.Pick(5)
	opts := vp8OracleRuntimeBaseFuzzOptions(dim.w, dim.h, targetKbps, speeds[r.Pick(len(speeds))])
	sources := vp8OracleRuntimeFuzzSources(dim.w, dim.h, frames, r.Pick(2))
	flags := make([]EncodeFlags, frames)
	script := runtimeControlScript(frames, nil)
	apply := make(map[int]func(*testing.T, *VP8Encoder), frames)
	copyRefLog := false

	for frame := 1; frame < frames; frame++ {
		actionCount := 1 + r.Pick(4)
		actions := make([]vp8OracleRuntimeFuzzAction, 0, actionCount)
		haveConfig := false
		for range actionCount {
			action, flag, usesCopyRef := vp8OracleRuntimeRandomFuzzAction(r, targets[:])
			if flag != 0 {
				flags[frame] = flag
				continue
			}
			if action.token == "" {
				continue
			}
			if action.phase == vp8OracleRuntimeFuzzConfigPhase {
				if haveConfig {
					continue
				}
				haveConfig = true
			}
			copyRefLog = copyRefLog || usesCopyRef
			actions = append(actions, action)
		}
		vp8OracleRuntimeShuffleActions(r, actions)
		vp8OracleRuntimeInstallFuzzActions(script, apply, frame, actions)
	}

	return vp8OracleRuntimeControlFuzzCase{
		name:       "general",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    sources,
		flags:      flags,
		script:     script,
		apply:      apply,
		copyRefLog: copyRefLog,
	}
}

func vp8OracleRuntimeFullControlPermutationFuzzCase(r *testutil.ByteCursor) vp8OracleRuntimeControlFuzzCase {
	targets := [...]int{300, 700, 1200}
	targetKbps := 700
	frames := 32
	opts := vp8OracleRuntimeBaseFuzzOptions(64, 64, targetKbps, [...]int{0, -3, -8}[r.Pick(3)])
	sources := vp8OracleRuntimeFuzzSources(opts.Width, opts.Height, frames, r.Pick(2))
	flags := make([]EncodeFlags, frames)
	script := runtimeControlScript(frames, nil)
	apply := make(map[int]func(*testing.T, *VP8Encoder), frames)
	perFrame := make([][]vp8OracleRuntimeFuzzAction, frames)
	frameHasConfig := make([]bool, frames)
	reservedFrame := make([]bool, frames)
	copyRefLog := false

	addAction := func(frame int, action vp8OracleRuntimeFuzzAction) {
		if frame <= 0 || frame >= frames || action.token == "" {
			return
		}
		perFrame[frame] = append(perFrame[frame], action)
		if action.phase == vp8OracleRuntimeFuzzConfigPhase {
			frameHasConfig[frame] = true
		}
	}
	findFrame := func(start int, action vp8OracleRuntimeFuzzAction) int {
		if start <= 0 {
			start = 1
		}
		for offset := 0; offset < frames-1; offset++ {
			frame := 1 + (start-1+offset)%(frames-1)
			if reservedFrame[frame] {
				continue
			}
			if action.phase == vp8OracleRuntimeFuzzConfigPhase && frameHasConfig[frame] {
				continue
			}
			if len(perFrame[frame]) >= 3 {
				continue
			}
			return frame
		}
		return frames - 1
	}

	temporalStart := 2
	for frame := temporalStart; frame <= temporalStart+4 && frame < frames; frame++ {
		reservedFrame[frame] = true
	}
	addAction(temporalStart, vp8OracleRuntimeTemporalEnableFuzzAction(targetKbps))
	addAction(temporalStart+1, vp8OracleRuntimeTemporalLayerIDFuzzAction(1))
	addAction(temporalStart+2, vp8OracleRuntimeTemporalLayerIDFuzzAction(0))
	addAction(temporalStart+3, vp8OracleRuntimeTemporalLayerIDFuzzAction(1))
	addAction(temporalStart+4, vp8OracleRuntimeTemporalDisableFuzzAction(targetKbps))
	temporalPattern, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	for frame := temporalStart; frame < temporalStart+4 && frame < frames; frame++ {
		flags[frame] = temporalPatternFlag(temporalPattern, uint64(frame-temporalStart), TemporalLayeringTwoLayers)
	}

	kinds := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	vp8OracleRuntimeShuffleInts(r, kinds)
	nextFrame := temporalStart + 5
	for _, kind := range kinds {
		if kind == 8 {
			continue
		}
		actionReader := vp8OracleRuntimeFullPermutationActionReader(kind)
		action, flag, usesCopyRef := vp8OracleRuntimeFuzzActionForKind(&actionReader, kind, targets[:])
		if kind == 7 {
			action, flag, usesCopyRef = vp8OracleRuntimeRealtimeDeadlineFuzzAction(&actionReader)
		}
		if flag != 0 {
			flags[findFrame(nextFrame, vp8OracleRuntimeFuzzAction{})] |= flag
			nextFrame++
			continue
		}
		if action.token == "" {
			continue
		}
		copyRefLog = copyRefLog || usesCopyRef
		frame := findFrame(nextFrame, action)
		addAction(frame, action)
		nextFrame = frame + 1
	}

	for frame, actions := range perFrame {
		vp8OracleRuntimeInstallFuzzActions(script, apply, frame, actions)
	}

	return vp8OracleRuntimeControlFuzzCase{
		name:       "all-control-permutation",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    sources,
		flags:      flags,
		script:     script,
		apply:      apply,
		copyRefLog: copyRefLog,
	}
}

func vp8OracleRuntimeTemporalFuzzCase(r *testutil.ByteCursor) vp8OracleRuntimeControlFuzzCase {
	targetKbps := 700
	frames := 8
	opts := vp8OracleRuntimeBaseFuzzOptions(64, 64, targetKbps, [...]int{0, -3}[r.Pick(2)])
	script := temporalScalabilityEnableDisableScript(frames)
	apply := map[int]func(*testing.T, *VP8Encoder){
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
	}
	return vp8OracleRuntimeControlFuzzCase{
		name:       "temporal",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    vp8OracleRuntimeFuzzSources(opts.Width, opts.Height, frames, r.Pick(2)),
		flags:      temporalScalabilityEnableDisableFlags(frames),
		script:     script,
		apply:      apply,
	}
}

func vp8OracleRuntimeInvalidNoopFuzzCase(r *testutil.ByteCursor) vp8OracleRuntimeControlFuzzCase {
	targetKbps := 700
	frames := 8
	opts := vp8OracleRuntimeBaseFuzzOptions(64, 64, targetKbps, 0)
	apply := make(map[int]func(*testing.T, *VP8Encoder), frames)
	for frame := 1; frame < frames; frame++ {
		switch r.Pick(7) {
		case 0:
			apply[frame] = func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				expectInvalidRuntime(t, "SetRateControl(minq>maxq)", ErrInvalidQuantizer, e.SetRateControl(RateControlConfig{
					Mode:              RateControlCBR,
					TargetBitrateKbps: targetKbps,
					MinQuantizer:      50,
					MaxQuantizer:      10,
				}))
			}
		case 1:
			apply[frame] = func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				expectInvalidRuntime(t, "SetRealtimeTarget(width-only)", ErrInvalidConfig, e.SetRealtimeTarget(RealtimeTarget{Width: 32}))
			}
		case 2:
			apply[frame] = func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				expectInvalidRuntime(t, "SetCQLevel(64)", ErrInvalidQuantizer, e.SetCQLevel(64))
			}
		case 3:
			apply[frame] = func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				expectInvalidRuntime(t, "SetTemporalLayerID(1)", ErrInvalidConfig, e.SetTemporalLayerID(1))
			}
		case 4:
			apply[frame] = func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				rows := encoderMacroblockRows(e.opts.Height)
				cols := encoderMacroblockCols(e.opts.Width)
				expectInvalidRuntime(t, "SetActiveMap(wrong rows)", ErrInvalidConfig, e.SetActiveMap(make([]uint8, rows*cols), rows+1, cols))
			}
		case 5:
			apply[frame] = func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				rows := encoderMacroblockRows(e.opts.Height)
				cols := encoderMacroblockCols(e.opts.Width)
				expectInvalidRuntime(t, "SetROIMap(wrong rows)", ErrInvalidConfig, e.SetROIMap(&ROIMap{
					Enabled:   true,
					Rows:      rows + 1,
					Cols:      cols,
					SegmentID: make([]uint8, rows*cols),
				}))
			}
		default:
			apply[frame] = func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				wrongSize := encoderValidationPanningFrame(e.opts.Width/2, e.opts.Height, 0)
				expectInvalidRuntime(t, "SetReferenceFrame(wrong size)", ErrInvalidConfig, e.SetReferenceFrame(ReferenceLast, wrongSize))
			}
		}
	}
	return vp8OracleRuntimeControlFuzzCase{
		name:       "invalid-noop",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    vp8OracleRuntimeFuzzSources(opts.Width, opts.Height, frames, 0),
		flags:      nil,
		script:     runtimeControlScript(frames, nil),
		apply:      apply,
	}
}
