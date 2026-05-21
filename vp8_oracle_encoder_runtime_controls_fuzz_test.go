//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

// FuzzVP8OracleEncoderRuntimeControlTransitions compares generated runtime-control
// schedules against the libvpx frame-flags driver. Go writes failing fuzz inputs
// to testdata/fuzz/FuzzVP8OracleEncoderRuntimeControlTransitions, and those corpus
// files are replayed by ordinary go test runs as regression tests.
func FuzzVP8OracleEncoderRuntimeControlTransitions(f *testing.F) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		f.Skip("set GOVPX_WITH_ORACLE=1 to run runtime-control fuzz parity")
	}
	seeds := [][]byte{
		{0, 0, 1, 2, 3, 4, 5, 6, 7, 8},
		{0, 2, 7, 7, 7, 3, 5, 1, 4, 6, 8},
		{0, 4, 3, 4, 8, 2, 2, 5, 6, 7, 1},
		{1, 0, 0, 0},
		{2, 0, 1, 2, 3, 4, 5, 6},
		{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		driver := coracletest.VpxencFrameFlags(t)
		tc := vp8OracleRuntimeControlFuzzCaseFromBytes(data)
		sum := sha256.Sum256(data)
		label := "fuzz-runtime-controls-" + tc.name + "-" + hex.EncodeToString(sum[:4])
		t.Logf("%s script=%s", label, strings.Join(tc.script, ","))

		govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, tc.sources, tc.flags, tc.apply)
		extraArgs := append([]string(nil), tc.extraArgs...)
		if tc.copyRefLog {
			extraArgs = append(extraArgs, "--copy-ref-log="+filepath.Join(t.TempDir(), "copy-reference.log"))
		}
		extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, label, tc.opts, tc.targetKbps, tc.sources, tc.flags, extraArgs)
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames,
			vp8OracleRuntimeControlFuzzMatchLimit(t.Name()))
	})
}

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

var vp8OracleRuntimeFullPermutationSeed = []byte{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

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

type vp8OracleRuntimeFuzzAction struct {
	token       string
	phase       uint8
	apply       func(*testing.T, *VP8Encoder)
	applyConfig func(*testing.T, *VP8Encoder)
	applyCodec  func(*testing.T, *VP8Encoder)
}

const (
	vp8OracleRuntimeFuzzConfigPhase uint8 = iota
	vp8OracleRuntimeFuzzCodecPhase
)

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

func vp8OracleRuntimeFullPermutationActionReader(kind int) testutil.ByteCursor {
	switch kind {
	case 3, 4:
		return testutil.NewByteCursor([]byte{2})
	case 8:
		return testutil.NewByteCursor([]byte{})
	case 17, 19:
		return testutil.NewByteCursor([]byte{1})
	default:
		return testutil.NewByteCursor([]byte{0})
	}
}

func vp8OracleRuntimeTemporalEnableFuzzAction(targetKbps int) vp8OracleRuntimeFuzzAction {
	return vp8OracleRuntimeFuzzAction{
		token: runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps) + "+tlid:0",
		phase: vp8OracleRuntimeFuzzConfigPhase,
		applyConfig: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetTemporalScalability(two-layer)", e.SetTemporalScalability(runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)))
		},
		applyCodec: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetTemporalLayerID(0)", e.SetTemporalLayerID(0))
		},
	}
}

func vp8OracleRuntimeTemporalLayerIDFuzzAction(layerID int) vp8OracleRuntimeFuzzAction {
	return vp8OracleRuntimeFuzzAction{
		token: "tlid:" + strconv.Itoa(layerID),
		phase: vp8OracleRuntimeFuzzCodecPhase,
		apply: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetTemporalLayerID", e.SetTemporalLayerID(layerID))
		},
	}
}

func vp8OracleRuntimeTemporalDisableFuzzAction(targetKbps int) vp8OracleRuntimeFuzzAction {
	return vp8OracleRuntimeFuzzAction{
		token: runtimeTemporalOffControlToken(targetKbps),
		phase: vp8OracleRuntimeFuzzConfigPhase,
		apply: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
		},
	}
}

func vp8OracleRuntimeShuffleInts(r *testutil.ByteCursor, values []int) {
	for i := len(values) - 1; i > 0; i-- {
		j := r.Pick(i + 1)
		values[i], values[j] = values[j], values[i]
	}
}

func vp8OracleRuntimeRandomFuzzAction(r *testutil.ByteCursor, targets []int) (vp8OracleRuntimeFuzzAction, EncodeFlags, bool) {
	return vp8OracleRuntimeFuzzActionForKind(r, r.Pick(18), targets)
}

func vp8OracleRuntimeFuzzActionForKind(r *testutil.ByteCursor, kind int, targets []int) (vp8OracleRuntimeFuzzAction, EncodeFlags, bool) {
	switch kind {
	case 0:
		value := targets[r.Pick(len(targets))]
		return vp8OracleRuntimeFuzzAction{
			token: "bitrate:" + strconv.Itoa(value),
			phase: vp8OracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetBitrateKbps", e.SetBitrateKbps(value))
			},
		}, 0, false
	case 1:
		bitrate := targets[r.Pick(len(targets))]
		fps := [...]int{15, 24, 30}[r.Pick(3)]
		minQ := [...]int{2, 4, 8}[r.Pick(3)]
		maxQ := [...]int{48, 52, 56}[r.Pick(3)]
		drop := [...]int{0, defaultDropFramesWaterMark}[r.Pick(2)]
		return vp8OracleRuntimeFuzzAction{
			token: "bitrate:" + strconv.Itoa(bitrate) +
				"+fps:" + strconv.Itoa(fps) +
				"+minq:" + strconv.Itoa(minQ) +
				"+maxq:" + strconv.Itoa(maxQ) +
				"+drop:" + strconv.Itoa(drop),
			phase: vp8OracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				// Mirror one vpx_codec_enc_config_set for the bundled
				// bitrate/fps/minq/maxq/drop tokens (frameflags driver).
				cfg := vp8OracleRuntimeCurrentRateControlConfig(e)
				cfg.TargetBitrateKbps = bitrate
				cfg.MinQuantizer = minQ
				cfg.MaxQuantizer = maxQ
				cfg.DropFrameWaterMark = drop
				cfg.DropFrameAllowed = drop > 0
				mustRuntime(t, "SetRateControl(bundle)", e.SetRateControl(cfg))
				// libvpx stores g_timebase in oxcf but vp8_change_config calls
				// vp8_new_framerate(cpi, cpi->framerate) without recomputing
				// cpi->framerate from the new timebase.
				e.opts.FPS = fps
				e.opts.TimebaseNum = 1
				e.opts.TimebaseDen = fps
				e.timing = timingFromEncoderOptions(e.opts)
			},
		}, 0, false
	case 2:
		noise := 0
		return vp8OracleRuntimeFuzzAction{
			token: "noise:" + strconv.Itoa(noise),
			phase: vp8OracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetNoiseSensitivity", e.SetNoiseSensitivity(noise))
			},
		}, 0, false
	case 3:
		return vp8OracleRuntimeFuzzAction{}, 0, false
	case 4:
		return vp8OracleRuntimeFuzzAction{}, 0, false
	case 5:
		refNames := [...]string{"last", "golden", "altref"}
		refs := [...]ReferenceFrame{ReferenceLast, ReferenceGolden, ReferenceAltRef}
		idx := r.Pick(len(refs))
		imageIndex := 8 + r.Pick(8)
		return vp8OracleRuntimeFuzzAction{
			token: "setref:" + refNames[idx] + ":panning:" + strconv.Itoa(imageIndex),
			phase: vp8OracleRuntimeFuzzCodecPhase,
			apply: setReferencePanningApply(refs[idx], imageIndex, refNames[idx]),
		}, 0, false
	case 6:
		staticThreshold := [...]int{0, 1, 500}[r.Pick(3)]
		screenMode := [...]int{0, 1, 2}[r.Pick(3)]
		sharpness := [...]int{0, 4, 7}[r.Pick(3)]
		return vp8OracleRuntimeFuzzAction{
			token: "static:" + strconv.Itoa(staticThreshold) +
				"+screen:" + strconv.Itoa(screenMode) +
				"+sharpness:" + strconv.Itoa(sharpness),
			phase: vp8OracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetStaticThreshold", e.SetStaticThreshold(staticThreshold))
				mustRuntime(t, "SetScreenContentMode", e.SetScreenContentMode(screenMode))
				mustRuntime(t, "SetSharpness", e.SetSharpness(sharpness))
			},
		}, 0, false
	case 7:
		good := r.Pick(2) == 0
		deadlineToken := "rt"
		deadline := DeadlineRealtime
		cpu := [...]int{0, -3, -8}[r.Pick(3)]
		if good {
			deadlineToken = "good"
			deadline = DeadlineGoodQuality
			cpu = [...]int{0, 4, 8}[r.Pick(3)]
		}
		return vp8OracleRuntimeFuzzAction{
			token: "deadline:" + deadlineToken + "+cpu:" + strconv.Itoa(cpu),
			phase: vp8OracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				// vpx_codec_encode applies the new deadline after runtime
				// codec controls queued for this input frame. Apply CPU first
				// so same-frame deadline+cpu fuzz cases mirror the frame-flags
				// driver instead of using a local-only ordering.
				mustRuntime(t, "SetCPUUsed", e.SetCPUUsed(cpu))
				mustRuntime(t, "SetDeadline", e.SetDeadline(deadline))
			},
		}, 0, false
	case 8:
		var flag EncodeFlags
		switch r.Pick(6) {
		case 0:
			flag = EncodeForceKeyFrame
		case 1:
			flag = EncodeNoUpdateEntropy
		case 2:
			flag = EncodeNoUpdateLast | EncodeNoReferenceGolden | EncodeNoReferenceAltRef
		case 3:
			flag = EncodeNoReferenceLast | EncodeNoUpdateGolden
		case 4:
			flag = EncodeForceGoldenFrame
		default:
			flag = EncodeForceAltRefFrame
		}
		return vp8OracleRuntimeFuzzAction{}, flag, false
	case 9:
		bitrate := targets[r.Pick(len(targets))]
		minQ := [...]int{2, 4, 8}[r.Pick(3)]
		maxQ := [...]int{48, 52, 56}[r.Pick(3)]
		undershoot := [...]int{50, 75, 100}[r.Pick(3)]
		overshoot := [...]int{50, 75, 100}[r.Pick(3)]
		return vp8OracleRuntimeFuzzAction{
			token: "endusage:cbr+bitrate:" + strconv.Itoa(bitrate) +
				"+minq:" + strconv.Itoa(minQ) +
				"+maxq:" + strconv.Itoa(maxQ) +
				"+undershoot:" + strconv.Itoa(undershoot) +
				"+overshoot:" + strconv.Itoa(overshoot) +
				"+bufsz:6000+bufinit:4000+bufopt:5000",
			phase: vp8OracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				dropAllowed, dropWaterMark := vp8OracleRuntimeCurrentDropConfig(e)
				mustRuntime(t, "SetRateControl", e.SetRateControl(RateControlConfig{
					Mode:                RateControlCBR,
					TargetBitrateKbps:   bitrate,
					MinQuantizer:        minQ,
					MaxQuantizer:        maxQ,
					UndershootPct:       undershoot,
					OvershootPct:        overshoot,
					BufferSizeMs:        6000,
					BufferInitialSizeMs: 4000,
					BufferOptimalSizeMs: 5000,
					DropFrameAllowed:    dropAllowed,
					DropFrameWaterMark:  dropWaterMark,
				}))
			},
		}, 0, false
	case 10:
		tuneName := [...]string{"psnr", "ssim"}[r.Pick(2)]
		tuning := TunePSNR
		if tuneName == "ssim" {
			tuning = TuneSSIM
		}
		return vp8OracleRuntimeFuzzAction{
			token: "tune:" + tuneName,
			phase: vp8OracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetTuning", e.SetTuning(tuning))
			},
		}, 0, false
	case 11:
		partitions := r.Pick(4)
		return vp8OracleRuntimeFuzzAction{
			token: "token:" + strconv.Itoa(partitions),
			phase: vp8OracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetTokenPartitions", e.SetTokenPartitions(partitions))
			},
		}, 0, false
	case 12:
		maxIntra := 0
		gfBoost := 0
		cq := 4
		return vp8OracleRuntimeFuzzAction{
			token: "maxintra:" + strconv.Itoa(maxIntra) +
				"+gfboost:" + strconv.Itoa(gfBoost) +
				"+cq:" + strconv.Itoa(cq),
			phase: vp8OracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetMaxIntraBitratePct", e.SetMaxIntraBitratePct(maxIntra))
				mustRuntime(t, "SetGFCBRBoostPct", e.SetGFCBRBoostPct(gfBoost))
				mustRuntime(t, "SetCQLevel", e.SetCQLevel(cq))
			},
		}, 0, false
	case 13:
		maxFrames := 0
		strength := 0
		filterType := 1
		return vp8OracleRuntimeFuzzAction{
			token: "arnrmax:" + strconv.Itoa(maxFrames) +
				"+arnrstrength:" + strconv.Itoa(strength) +
				"+arnrtype:" + strconv.Itoa(filterType),
			phase: vp8OracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "VP8E_SET_ARNR_MAXFRAMES", e.setARNRMaxFrames(maxFrames))
				mustRuntime(t, "VP8E_SET_ARNR_STRENGTH", e.setARNRStrength(strength))
				mustRuntime(t, "VP8E_SET_ARNR_TYPE", e.setARNRType(filterType))
			},
		}, 0, false
	case 14:
		enabled := false
		value := 0
		if enabled {
			value = 1
		}
		return vp8OracleRuntimeFuzzAction{
			token: "rtc:" + strconv.Itoa(value),
			phase: vp8OracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl", e.SetRTCExternalRateControl(enabled))
			},
		}, 0, false
	case 15:
		refNames := [...]string{"last", "golden", "altref"}
		refs := [...]ReferenceFrame{ReferenceLast, ReferenceGolden, ReferenceAltRef}
		idx := r.Pick(len(refs))
		return vp8OracleRuntimeFuzzAction{
			token: "copyref:" + refNames[idx],
			phase: vp8OracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				dst := newTestImage(e.opts.Width, e.opts.Height)
				mustRuntime(t, "CopyReferenceFrame("+refNames[idx]+")", e.CopyReferenceFrame(refs[idx], &dst))
			},
		}, 0, true
	case 16:
		enabled := r.Pick(2) == 1
		drop := 0
		if enabled {
			drop = 60
		}
		return vp8OracleRuntimeFuzzAction{
			token: "drop:" + strconv.Itoa(drop),
			phase: vp8OracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				// Mirror vpx_codec_enc_config_set rc_dropframe_thresh only.
				cfg := vp8OracleRuntimeCurrentRateControlConfig(e)
				cfg.DropFrameWaterMark = drop
				cfg.DropFrameAllowed = drop > 0
				mustRuntime(t, "SetRateControl(drop)", e.SetRateControl(cfg))
			},
		}, 0, false
	case 18:
		interval := 999
		return vp8OracleRuntimeFuzzAction{
			token: "kfmin:" + strconv.Itoa(interval) + "+kfmax:" + strconv.Itoa(interval),
			phase: vp8OracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetKeyFrameInterval", e.SetKeyFrameInterval(interval))
			},
		}, 0, false
	case 19:
		enabled := r.Pick(2) == 1
		disabled := 1
		if enabled {
			disabled = 0
		}
		interval := 999
		if !enabled {
			interval = 0
		}
		return vp8OracleRuntimeFuzzAction{
			token: "kfdisabled:" + strconv.Itoa(disabled) + "+kfmin:" + strconv.Itoa(interval) + "+kfmax:" + strconv.Itoa(interval),
			phase: vp8OracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetAdaptiveKeyFrames", e.SetAdaptiveKeyFrames(enabled))
				mustRuntime(t, "SetKeyFrameInterval", e.SetKeyFrameInterval(interval))
			},
		}, 0, false
	default:
		modes := [...]RateControlMode{RateControlCBR}
		mode := modes[r.Pick(len(modes))]
		bitrate := targets[r.Pick(len(targets))]
		cqLevel := runtimeRateControlModeCQLevel(mode)
		return vp8OracleRuntimeFuzzAction{
			token: runtimeRateControlModeControlToken(mode, bitrate),
			phase: vp8OracleRuntimeFuzzConfigPhase,
			applyConfig: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				dropAllowed, dropWaterMark := vp8OracleRuntimeCurrentDropConfig(e)
				mustRuntime(t, "SetRateControl(mode)", e.SetRateControl(RateControlConfig{
					Mode:                mode,
					TargetBitrateKbps:   bitrate,
					MinQuantizer:        4,
					MaxQuantizer:        56,
					CQLevel:             cqLevel,
					UndershootPct:       100,
					OvershootPct:        100,
					BufferSizeMs:        6000,
					BufferInitialSizeMs: 4000,
					BufferOptimalSizeMs: 5000,
					DropFrameAllowed:    dropAllowed,
					DropFrameWaterMark:  dropWaterMark,
				}))
			},
			applyCodec: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				if cqLevel > 0 {
					mustRuntime(t, "SetCQLevel(mode)", e.SetCQLevel(cqLevel))
				}
			},
		}, 0, false
	}
}

func vp8OracleRuntimeRealtimeDeadlineFuzzAction(r *testutil.ByteCursor) (vp8OracleRuntimeFuzzAction, EncodeFlags, bool) {
	cpu := [...]int{0, -3, -8}[r.Pick(3)]
	return vp8OracleRuntimeFuzzAction{
		token: "deadline:rt+cpu:" + strconv.Itoa(cpu),
		phase: vp8OracleRuntimeFuzzCodecPhase,
		apply: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetCPUUsed", e.SetCPUUsed(cpu))
			mustRuntime(t, "SetDeadline", e.SetDeadline(DeadlineRealtime))
		},
	}, 0, false
}

func vp8OracleRuntimeCurrentDropConfig(e *VP8Encoder) (bool, int) {
	if e == nil || !e.opts.DropFrameAllowed {
		return false, 0
	}
	return true, e.opts.DropFrameWaterMark
}

func vp8OracleRuntimeCurrentRateControlConfig(e *VP8Encoder) RateControlConfig {
	if e == nil {
		return RateControlConfig{}
	}
	return RateControlConfig{
		Mode:                e.rc.mode,
		TargetBitrateKbps:   e.rc.targetBitrateKbps,
		MinBitrateKbps:      e.rc.minBitrateKbps,
		MaxBitrateKbps:      e.rc.maxBitrateKbps,
		MinQuantizer:        vp8common.QIndexToPublicQuantizer(e.rc.minQuantizer),
		MaxQuantizer:        vp8common.QIndexToPublicQuantizer(e.rc.maxQuantizer),
		CQLevel:             e.opts.CQLevel,
		UndershootPct:       e.rc.undershootPct,
		OvershootPct:        e.rc.overshootPct,
		BufferSizeMs:        e.rc.bufferSizeMs,
		BufferInitialSizeMs: e.rc.bufferInitialSizeMs,
		BufferOptimalSizeMs: e.rc.bufferOptimalSizeMs,
		DropFrameAllowed:    e.rc.dropFramesWaterMark > 0,
		DropFrameWaterMark:  e.rc.dropFramesWaterMark,
		MaxIntraBitratePct:  e.rc.maxIntraBitratePct,
		GFCBRBoostPct:       e.rc.gfCBRBoostPct,
	}
}

func vp8OracleRuntimeShuffleActions(r *testutil.ByteCursor, actions []vp8OracleRuntimeFuzzAction) {
	for i := len(actions) - 1; i > 0; i-- {
		j := r.Pick(i + 1)
		actions[i], actions[j] = actions[j], actions[i]
	}
}

func vp8OracleRuntimeInstallFuzzActions(script []string, apply map[int]func(*testing.T, *VP8Encoder), frame int, actions []vp8OracleRuntimeFuzzAction) {
	if len(actions) == 0 {
		return
	}
	tokens := make([]string, 0, len(actions))
	for _, action := range actions {
		tokens = append(tokens, action.token)
	}
	script[frame] = strings.Join(tokens, "+")
	apply[frame] = func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		for _, action := range actions {
			if action.applyConfig != nil {
				action.applyConfig(t, e)
			} else if action.phase == vp8OracleRuntimeFuzzConfigPhase {
				action.apply(t, e)
			}
		}
		for _, action := range actions {
			if action.applyCodec != nil {
				action.applyCodec(t, e)
			} else if action.phase == vp8OracleRuntimeFuzzCodecPhase {
				action.apply(t, e)
			}
		}
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
