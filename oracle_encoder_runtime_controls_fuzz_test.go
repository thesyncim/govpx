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
)

// FuzzOracleEncoderRuntimeControlTransitions compares generated runtime-control
// schedules against the libvpx frame-flags driver. Go writes failing fuzz inputs
// to testdata/fuzz/FuzzOracleEncoderRuntimeControlTransitions, and those corpus
// files are replayed by ordinary go test runs as regression tests.
func FuzzOracleEncoderRuntimeControlTransitions(f *testing.F) {
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
		driver := findVpxencFrameFlags(t)
		tc := oracleRuntimeControlFuzzCaseFromBytes(data)
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
			oracleRuntimeControlFuzzMatchLimit(t.Name()))
	})
}

func oracleRuntimeControlFuzzMatchLimit(_ string) int {
	// Seed 77952f43 historically diverged at frame 8 inside the partially
	// ported good-quality inter recode loop (govpx first_partition=58 vs
	// libvpx=65). The recode-loop and change-config rate-model alignment
	// shipped as commit 45ded7d5 ("vp8: land libvpx-aligned recode_loop +
	// change-config rate-model + segmentation-method WIP") closed the
	// remaining bookkeeping, so frames 0-8 now match byte-for-byte. The
	// strict gate (limit=0, full-length match) replaces the previous
	// limit=7 tolerance to catch any regression in the matched suffix.
	// This corpus enters the same partial good-quality inter recode area
	// after a golden-reference overwrite. Frames 0-3 still pin the
	// runtime-control setup before the known recode divergence. Task #187
	// audit confirms (vp8_golden_overwrite_frame4_audit_test.go) that the
	// libvpx golden-frame copy paths (vp8_set_reference,
	// update_reference_frames, copy_buffer_to_{arf,gf}) and the post-task-#181
	// last_*_lf_deltas zeroing are byte-faithful for frames 0-3; the residual
	// gap belongs to the same per-MB picker state cohort as task #173 but
	// expressed via the deadline:good cluster following the
	// setref:golden:panning:8 control burst.
	//
	// Task #211 (vp8_task211_bb41d74_recode_loop_audit_test.go) drills
	// further: frame 4's recode loop converges to a DIFFERENT final
	// quantizer (govpx q=9, libvpx q=7) which cascades into per-MB picker
	// divergence (govpx silently skips SPLITMV at MB(0,0) because its
	// bestYRD=45362 cuts off SPLITMV partition shapes that libvpx accepts
	// at bestYRD=60198 — the q delta drives the RDMULT delta drives the
	// YRD delta drives the SPLITMV cutoff). Closing the gap requires
	// per-recode-iter projected_frame_size instrumentation on both sides
	// to pin the first diverging iteration.
	//
	// Task #212 (current open issue): recode-Q convergence on 0bb41d74 —
	// continuation of the task #211 audit. Frames 0-3 remain byte-exact
	// after today's wire-level closes (#172/#184/#192/#201/#183/#200/#202/
	// #198/#173/#174/#206); the matchLimit=4 pin keeps the asserted
	// prefix at the maximum currently green so any regression in the
	// matched suffix is caught while the open #212 work threads
	// projected_frame_size deltas through the recode regulator. Update
	// this gate to limit=0 (or remove the pin) the moment #212 closes
	// and frames 4-8 match byte-for-byte.
	// Task #218 (CLOSED for 0bb41d74): the bb41d74 frame-4 SPLITMV skip-
	// backout port (vp8_encoder_inter_modes_rd_split.go: drop the spurious
	// `&& stats.rateUV == 0` clause from mbSkipCoeff) closes frames 0-8
	// byte-exact. The matchLimit=4 carveout is removed.
	//
	// Task #237 (CLOSED for aebef841): rd_check_segment's SPLITMV per-label
	// inner loop leaves xd->eobs[i] holding the LAST-iterated mode's per-
	// block eob registers (not the RD-winning mode's), because
	// vp8/encoder/rdopt.c:1124-1158 restores only the entropy contexts after
	// the winning mode is re-installed by labels2mode. bsi->eobs[i] =
	// xd->eobs[i] at rdopt.c:1180 then captures that stale snapshot, and
	// calculate_final_rd_costs reads tteob through those stale eobs
	// (rdopt.c:1689-1697) when applying the SPLITMV skip-backout. govpx's
	// selectMotion now mirrors that side-effect via lastTTEOB, dropping the
	// matchLimit=6 carveout to 0 for byte-exact parity across all 9 frames
	// on the aebef841 corpus.
	//
	// Task #259 (CLOSED for regression_general_e5f453c6): runtime-control
	// script `arnrmax:0+arnrstrength:0+arnrtype:1` followed by repeated
	// `cq:4+maxintra:0+gfboost:0` transitions (#240 capture, hypothesised
	// #235-adjacent). Bisection of a1b1f62c..HEAD (frameflags-oracle and
	// frameflags binaries both rebuilt from internal/coracle build scripts)
	// shows the seed already passes byte-exact for all 10 frames at the
	// capture commit a1b1f62c and at every commit through current HEAD --
	// no code change required. The frame-5 mismatch reported in the #240
	// capture message (got_len=906 want_len=960) reflects a stale oracle
	// binary built before #235's CBR baseline_gf_interval and #236's
	// b->zbin_extra fixes had been pulled into the libvpx oracle build
	// tree; rebuilding via internal/coracle/build_vpxenc_frameflags{,_oracle}.sh
	// against libvpx v1.16.0 yields a driver that agrees with govpx. The
	// #235 baseline_gf_interval port (e72887d9, c89423ac) and the #236
	// intra RD zbin_extra carry remain the active fixes for this cohort;
	// the e5f453c6 seed is pinned as a runtime-control transition sentinel.
	return 0
}

type oracleRuntimeControlFuzzCase struct {
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

type oracleRuntimeControlFuzzBytes struct {
	data []byte
	pos  int
}

func (r *oracleRuntimeControlFuzzBytes) next() byte {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[r.pos%len(r.data)]
	r.pos++
	return b
}

func (r *oracleRuntimeControlFuzzBytes) pick(n int) int {
	if n <= 1 {
		return 0
	}
	return int(r.next()) % n
}

func oracleRuntimeControlFuzzCaseFromBytes(data []byte) oracleRuntimeControlFuzzCase {
	if string(data) == "02000y0" {
		return oracleRuntimeFPSBitrateReproFuzzCase()
	}
	if string(data) == "\xff" {
		return oracleRuntimeKeyFrameIntervalZeroReproFuzzCase()
	}
	if bytes.Equal(data, oracleRuntimeFullPermutationSeed) {
		r := oracleRuntimeControlFuzzBytes{data: data[1:]}
		return oracleRuntimeFullControlPermutationFuzzCase(&r)
	}
	r := oracleRuntimeControlFuzzBytes{data: data}
	switch r.pick(3) {
	case 1:
		return oracleRuntimeTemporalFuzzCase(&r)
	case 2:
		return oracleRuntimeInvalidNoopFuzzCase(&r)
	default:
		return oracleRuntimeGeneralFuzzCase(&r)
	}
}

var oracleRuntimeFullPermutationSeed = []byte{0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

func oracleRuntimeKeyFrameIntervalZeroReproFuzzCase() oracleRuntimeControlFuzzCase {
	targetKbps := 700
	frames := 8
	opts := oracleRuntimeBaseFuzzOptions(64, 64, targetKbps, 0)
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
	return oracleRuntimeControlFuzzCase{
		name:       "keyframe-interval-zero-repro",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    oracleRuntimeFuzzSources(opts.Width, opts.Height, frames, 0),
		flags:      nil,
		script:     script,
		apply:      apply,
	}
}

func oracleRuntimeBaseFuzzOptions(width, height, targetKbps, cpuUsed int) EncoderOptions {
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

func oracleRuntimeFuzzSources(width, height, frames, kind int) []Image {
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

func oracleRuntimeFPSBitrateReproFuzzCase() oracleRuntimeControlFuzzCase {
	targetKbps := 300
	frames := 9
	opts := oracleRuntimeBaseFuzzOptions(64, 64, targetKbps, 0)
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
	return oracleRuntimeControlFuzzCase{
		name:       "fps-bitrate-repro",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    oracleRuntimeFuzzSources(opts.Width, opts.Height, frames, 1),
		flags:      flags,
		script:     script,
		apply:      apply,
	}
}

type oracleRuntimeFuzzAction struct {
	token       string
	phase       uint8
	apply       func(*testing.T, *VP8Encoder)
	applyConfig func(*testing.T, *VP8Encoder)
	applyCodec  func(*testing.T, *VP8Encoder)
}

const (
	oracleRuntimeFuzzConfigPhase uint8 = iota
	oracleRuntimeFuzzCodecPhase
)

func oracleRuntimeGeneralFuzzCase(r *oracleRuntimeControlFuzzBytes) oracleRuntimeControlFuzzCase {
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
	dim := dims[r.pick(len(dims))]
	targetKbps := targets[r.pick(len(targets))]
	frames := 6 + r.pick(5)
	opts := oracleRuntimeBaseFuzzOptions(dim.w, dim.h, targetKbps, speeds[r.pick(len(speeds))])
	sources := oracleRuntimeFuzzSources(dim.w, dim.h, frames, r.pick(2))
	flags := make([]EncodeFlags, frames)
	script := runtimeControlScript(frames, nil)
	apply := make(map[int]func(*testing.T, *VP8Encoder), frames)
	copyRefLog := false

	for frame := 1; frame < frames; frame++ {
		actionCount := 1 + r.pick(4)
		actions := make([]oracleRuntimeFuzzAction, 0, actionCount)
		haveConfig := false
		for range actionCount {
			action, flag, usesCopyRef := oracleRuntimeRandomFuzzAction(r, targets[:])
			if flag != 0 {
				flags[frame] = flag
				continue
			}
			if action.token == "" {
				continue
			}
			if action.phase == oracleRuntimeFuzzConfigPhase {
				if haveConfig {
					continue
				}
				haveConfig = true
			}
			copyRefLog = copyRefLog || usesCopyRef
			actions = append(actions, action)
		}
		oracleRuntimeShuffleActions(r, actions)
		oracleRuntimeInstallFuzzActions(script, apply, frame, actions)
	}

	return oracleRuntimeControlFuzzCase{
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

func oracleRuntimeFullControlPermutationFuzzCase(r *oracleRuntimeControlFuzzBytes) oracleRuntimeControlFuzzCase {
	targets := [...]int{300, 700, 1200}
	targetKbps := 700
	frames := 32
	opts := oracleRuntimeBaseFuzzOptions(64, 64, targetKbps, [...]int{0, -3, -8}[r.pick(3)])
	sources := oracleRuntimeFuzzSources(opts.Width, opts.Height, frames, r.pick(2))
	flags := make([]EncodeFlags, frames)
	script := runtimeControlScript(frames, nil)
	apply := make(map[int]func(*testing.T, *VP8Encoder), frames)
	perFrame := make([][]oracleRuntimeFuzzAction, frames)
	frameHasConfig := make([]bool, frames)
	reservedFrame := make([]bool, frames)
	copyRefLog := false

	addAction := func(frame int, action oracleRuntimeFuzzAction) {
		if frame <= 0 || frame >= frames || action.token == "" {
			return
		}
		perFrame[frame] = append(perFrame[frame], action)
		if action.phase == oracleRuntimeFuzzConfigPhase {
			frameHasConfig[frame] = true
		}
	}
	findFrame := func(start int, action oracleRuntimeFuzzAction) int {
		if start <= 0 {
			start = 1
		}
		for offset := 0; offset < frames-1; offset++ {
			frame := 1 + (start-1+offset)%(frames-1)
			if reservedFrame[frame] {
				continue
			}
			if action.phase == oracleRuntimeFuzzConfigPhase && frameHasConfig[frame] {
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
	addAction(temporalStart, oracleRuntimeTemporalEnableFuzzAction(targetKbps))
	addAction(temporalStart+1, oracleRuntimeTemporalLayerIDFuzzAction(1))
	addAction(temporalStart+2, oracleRuntimeTemporalLayerIDFuzzAction(0))
	addAction(temporalStart+3, oracleRuntimeTemporalLayerIDFuzzAction(1))
	addAction(temporalStart+4, oracleRuntimeTemporalDisableFuzzAction(targetKbps))
	temporalPattern, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	for frame := temporalStart; frame < temporalStart+4 && frame < frames; frame++ {
		flags[frame] = temporalPatternFlag(temporalPattern, uint64(frame-temporalStart), TemporalLayeringTwoLayers)
	}

	kinds := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	oracleRuntimeShuffleInts(r, kinds)
	nextFrame := temporalStart + 5
	for _, kind := range kinds {
		if kind == 8 {
			continue
		}
		actionReader := oracleRuntimeFullPermutationActionReader(kind)
		action, flag, usesCopyRef := oracleRuntimeFuzzActionForKind(&actionReader, kind, targets[:])
		if kind == 7 {
			action, flag, usesCopyRef = oracleRuntimeRealtimeDeadlineFuzzAction(&actionReader)
		}
		if flag != 0 {
			flags[findFrame(nextFrame, oracleRuntimeFuzzAction{})] |= flag
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
		oracleRuntimeInstallFuzzActions(script, apply, frame, actions)
	}

	return oracleRuntimeControlFuzzCase{
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

func oracleRuntimeFullPermutationActionReader(kind int) oracleRuntimeControlFuzzBytes {
	switch kind {
	case 3, 4:
		return oracleRuntimeControlFuzzBytes{data: []byte{2}}
	case 8:
		return oracleRuntimeControlFuzzBytes{data: []byte{}}
	case 17, 19:
		return oracleRuntimeControlFuzzBytes{data: []byte{1}}
	default:
		return oracleRuntimeControlFuzzBytes{data: []byte{0}}
	}
}

func oracleRuntimeTemporalEnableFuzzAction(targetKbps int) oracleRuntimeFuzzAction {
	return oracleRuntimeFuzzAction{
		token: runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps) + "+tlid:0",
		phase: oracleRuntimeFuzzConfigPhase,
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

func oracleRuntimeTemporalLayerIDFuzzAction(layerID int) oracleRuntimeFuzzAction {
	return oracleRuntimeFuzzAction{
		token: "tlid:" + strconv.Itoa(layerID),
		phase: oracleRuntimeFuzzCodecPhase,
		apply: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetTemporalLayerID", e.SetTemporalLayerID(layerID))
		},
	}
}

func oracleRuntimeTemporalDisableFuzzAction(targetKbps int) oracleRuntimeFuzzAction {
	return oracleRuntimeFuzzAction{
		token: runtimeTemporalOffControlToken(targetKbps),
		phase: oracleRuntimeFuzzConfigPhase,
		apply: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
		},
	}
}

func oracleRuntimeShuffleInts(r *oracleRuntimeControlFuzzBytes, values []int) {
	for i := len(values) - 1; i > 0; i-- {
		j := r.pick(i + 1)
		values[i], values[j] = values[j], values[i]
	}
}

func oracleRuntimeRandomFuzzAction(r *oracleRuntimeControlFuzzBytes, targets []int) (oracleRuntimeFuzzAction, EncodeFlags, bool) {
	return oracleRuntimeFuzzActionForKind(r, r.pick(18), targets)
}

func oracleRuntimeFuzzActionForKind(r *oracleRuntimeControlFuzzBytes, kind int, targets []int) (oracleRuntimeFuzzAction, EncodeFlags, bool) {
	switch kind {
	case 0:
		value := targets[r.pick(len(targets))]
		return oracleRuntimeFuzzAction{
			token: "bitrate:" + strconv.Itoa(value),
			phase: oracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetBitrateKbps", e.SetBitrateKbps(value))
			},
		}, 0, false
	case 1:
		bitrate := targets[r.pick(len(targets))]
		fps := [...]int{15, 24, 30}[r.pick(3)]
		minQ := [...]int{2, 4, 8}[r.pick(3)]
		maxQ := [...]int{48, 52, 56}[r.pick(3)]
		drop := [...]int{0, defaultDropFramesWaterMark}[r.pick(2)]
		return oracleRuntimeFuzzAction{
			token: "bitrate:" + strconv.Itoa(bitrate) +
				"+fps:" + strconv.Itoa(fps) +
				"+minq:" + strconv.Itoa(minQ) +
				"+maxq:" + strconv.Itoa(maxQ) +
				"+drop:" + strconv.Itoa(drop),
			phase: oracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				// Mirror one vpx_codec_enc_config_set for the bundled
				// bitrate/fps/minq/maxq/drop tokens (frameflags driver).
				cfg := oracleRuntimeCurrentRateControlConfig(e)
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
		return oracleRuntimeFuzzAction{
			token: "noise:" + strconv.Itoa(noise),
			phase: oracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetNoiseSensitivity", e.SetNoiseSensitivity(noise))
			},
		}, 0, false
	case 3:
		return oracleRuntimeFuzzAction{}, 0, false
	case 4:
		return oracleRuntimeFuzzAction{}, 0, false
	case 5:
		refNames := [...]string{"last", "golden", "altref"}
		refs := [...]ReferenceFrame{ReferenceLast, ReferenceGolden, ReferenceAltRef}
		idx := r.pick(len(refs))
		imageIndex := 8 + r.pick(8)
		return oracleRuntimeFuzzAction{
			token: "setref:" + refNames[idx] + ":panning:" + strconv.Itoa(imageIndex),
			phase: oracleRuntimeFuzzCodecPhase,
			apply: setReferencePanningApply(refs[idx], imageIndex, refNames[idx]),
		}, 0, false
	case 6:
		staticThreshold := [...]int{0, 1, 500}[r.pick(3)]
		screenMode := [...]int{0, 1, 2}[r.pick(3)]
		sharpness := [...]int{0, 4, 7}[r.pick(3)]
		return oracleRuntimeFuzzAction{
			token: "static:" + strconv.Itoa(staticThreshold) +
				"+screen:" + strconv.Itoa(screenMode) +
				"+sharpness:" + strconv.Itoa(sharpness),
			phase: oracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetStaticThreshold", e.SetStaticThreshold(staticThreshold))
				mustRuntime(t, "SetScreenContentMode", e.SetScreenContentMode(screenMode))
				mustRuntime(t, "SetSharpness", e.SetSharpness(sharpness))
			},
		}, 0, false
	case 7:
		good := r.pick(2) == 0
		deadlineToken := "rt"
		deadline := DeadlineRealtime
		cpu := [...]int{0, -3, -8}[r.pick(3)]
		if good {
			deadlineToken = "good"
			deadline = DeadlineGoodQuality
			cpu = [...]int{0, 4, 8}[r.pick(3)]
		}
		return oracleRuntimeFuzzAction{
			token: "deadline:" + deadlineToken + "+cpu:" + strconv.Itoa(cpu),
			phase: oracleRuntimeFuzzCodecPhase,
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
		switch r.pick(6) {
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
		return oracleRuntimeFuzzAction{}, flag, false
	case 9:
		bitrate := targets[r.pick(len(targets))]
		minQ := [...]int{2, 4, 8}[r.pick(3)]
		maxQ := [...]int{48, 52, 56}[r.pick(3)]
		undershoot := [...]int{50, 75, 100}[r.pick(3)]
		overshoot := [...]int{50, 75, 100}[r.pick(3)]
		return oracleRuntimeFuzzAction{
			token: "endusage:cbr+bitrate:" + strconv.Itoa(bitrate) +
				"+minq:" + strconv.Itoa(minQ) +
				"+maxq:" + strconv.Itoa(maxQ) +
				"+undershoot:" + strconv.Itoa(undershoot) +
				"+overshoot:" + strconv.Itoa(overshoot) +
				"+bufsz:6000+bufinit:4000+bufopt:5000",
			phase: oracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				dropAllowed, dropWaterMark := oracleRuntimeCurrentDropConfig(e)
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
		tuneName := [...]string{"psnr", "ssim"}[r.pick(2)]
		tuning := TunePSNR
		if tuneName == "ssim" {
			tuning = TuneSSIM
		}
		return oracleRuntimeFuzzAction{
			token: "tune:" + tuneName,
			phase: oracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetTuning", e.SetTuning(tuning))
			},
		}, 0, false
	case 11:
		partitions := r.pick(4)
		return oracleRuntimeFuzzAction{
			token: "token:" + strconv.Itoa(partitions),
			phase: oracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetTokenPartitions", e.SetTokenPartitions(partitions))
			},
		}, 0, false
	case 12:
		maxIntra := 0
		gfBoost := 0
		cq := 4
		return oracleRuntimeFuzzAction{
			token: "maxintra:" + strconv.Itoa(maxIntra) +
				"+gfboost:" + strconv.Itoa(gfBoost) +
				"+cq:" + strconv.Itoa(cq),
			phase: oracleRuntimeFuzzCodecPhase,
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
		return oracleRuntimeFuzzAction{
			token: "arnrmax:" + strconv.Itoa(maxFrames) +
				"+arnrstrength:" + strconv.Itoa(strength) +
				"+arnrtype:" + strconv.Itoa(filterType),
			phase: oracleRuntimeFuzzCodecPhase,
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
		return oracleRuntimeFuzzAction{
			token: "rtc:" + strconv.Itoa(value),
			phase: oracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl", e.SetRTCExternalRateControl(enabled))
			},
		}, 0, false
	case 15:
		refNames := [...]string{"last", "golden", "altref"}
		refs := [...]ReferenceFrame{ReferenceLast, ReferenceGolden, ReferenceAltRef}
		idx := r.pick(len(refs))
		return oracleRuntimeFuzzAction{
			token: "copyref:" + refNames[idx],
			phase: oracleRuntimeFuzzCodecPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				dst := newTestImage(e.opts.Width, e.opts.Height)
				mustRuntime(t, "CopyReferenceFrame("+refNames[idx]+")", e.CopyReferenceFrame(refs[idx], &dst))
			},
		}, 0, true
	case 16:
		enabled := r.pick(2) == 1
		drop := 0
		if enabled {
			drop = 60
		}
		return oracleRuntimeFuzzAction{
			token: "drop:" + strconv.Itoa(drop),
			phase: oracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				// Mirror vpx_codec_enc_config_set rc_dropframe_thresh only.
				cfg := oracleRuntimeCurrentRateControlConfig(e)
				cfg.DropFrameWaterMark = drop
				cfg.DropFrameAllowed = drop > 0
				mustRuntime(t, "SetRateControl(drop)", e.SetRateControl(cfg))
			},
		}, 0, false
	case 18:
		interval := 999
		return oracleRuntimeFuzzAction{
			token: "kfmin:" + strconv.Itoa(interval) + "+kfmax:" + strconv.Itoa(interval),
			phase: oracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetKeyFrameInterval", e.SetKeyFrameInterval(interval))
			},
		}, 0, false
	case 19:
		enabled := r.pick(2) == 1
		disabled := 1
		if enabled {
			disabled = 0
		}
		interval := 999
		if !enabled {
			interval = 0
		}
		return oracleRuntimeFuzzAction{
			token: "kfdisabled:" + strconv.Itoa(disabled) + "+kfmin:" + strconv.Itoa(interval) + "+kfmax:" + strconv.Itoa(interval),
			phase: oracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetAdaptiveKeyFrames", e.SetAdaptiveKeyFrames(enabled))
				mustRuntime(t, "SetKeyFrameInterval", e.SetKeyFrameInterval(interval))
			},
		}, 0, false
	default:
		modes := [...]RateControlMode{RateControlCBR}
		mode := modes[r.pick(len(modes))]
		bitrate := targets[r.pick(len(targets))]
		cqLevel := runtimeRateControlModeCQLevel(mode)
		return oracleRuntimeFuzzAction{
			token: runtimeRateControlModeControlToken(mode, bitrate),
			phase: oracleRuntimeFuzzConfigPhase,
			applyConfig: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				dropAllowed, dropWaterMark := oracleRuntimeCurrentDropConfig(e)
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

func oracleRuntimeRealtimeDeadlineFuzzAction(r *oracleRuntimeControlFuzzBytes) (oracleRuntimeFuzzAction, EncodeFlags, bool) {
	cpu := [...]int{0, -3, -8}[r.pick(3)]
	return oracleRuntimeFuzzAction{
		token: "deadline:rt+cpu:" + strconv.Itoa(cpu),
		phase: oracleRuntimeFuzzCodecPhase,
		apply: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetCPUUsed", e.SetCPUUsed(cpu))
			mustRuntime(t, "SetDeadline", e.SetDeadline(DeadlineRealtime))
		},
	}, 0, false
}

func oracleRuntimeCurrentDropConfig(e *VP8Encoder) (bool, int) {
	if e == nil || !e.opts.DropFrameAllowed {
		return false, 0
	}
	return true, e.opts.DropFrameWaterMark
}

func oracleRuntimeCurrentRateControlConfig(e *VP8Encoder) RateControlConfig {
	if e == nil {
		return RateControlConfig{}
	}
	return RateControlConfig{
		Mode:                e.rc.mode,
		TargetBitrateKbps:   e.rc.targetBitrateKbps,
		MinBitrateKbps:      e.rc.minBitrateKbps,
		MaxBitrateKbps:      e.rc.maxBitrateKbps,
		MinQuantizer:        libvpxQIndexToPublicQuantizer(e.rc.minQuantizer),
		MaxQuantizer:        libvpxQIndexToPublicQuantizer(e.rc.maxQuantizer),
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

func oracleRuntimeShuffleActions(r *oracleRuntimeControlFuzzBytes, actions []oracleRuntimeFuzzAction) {
	for i := len(actions) - 1; i > 0; i-- {
		j := r.pick(i + 1)
		actions[i], actions[j] = actions[j], actions[i]
	}
}

func oracleRuntimeInstallFuzzActions(script []string, apply map[int]func(*testing.T, *VP8Encoder), frame int, actions []oracleRuntimeFuzzAction) {
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
			} else if action.phase == oracleRuntimeFuzzConfigPhase {
				action.apply(t, e)
			}
		}
		for _, action := range actions {
			if action.applyCodec != nil {
				action.applyCodec(t, e)
			} else if action.phase == oracleRuntimeFuzzCodecPhase {
				action.apply(t, e)
			}
		}
	}
}

func oracleRuntimeTemporalFuzzCase(r *oracleRuntimeControlFuzzBytes) oracleRuntimeControlFuzzCase {
	targetKbps := 700
	frames := 8
	opts := oracleRuntimeBaseFuzzOptions(64, 64, targetKbps, [...]int{0, -3}[r.pick(2)])
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
	return oracleRuntimeControlFuzzCase{
		name:       "temporal",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    oracleRuntimeFuzzSources(opts.Width, opts.Height, frames, r.pick(2)),
		flags:      temporalScalabilityEnableDisableFlags(frames),
		script:     script,
		apply:      apply,
	}
}

func oracleRuntimeInvalidNoopFuzzCase(r *oracleRuntimeControlFuzzBytes) oracleRuntimeControlFuzzCase {
	targetKbps := 700
	frames := 8
	opts := oracleRuntimeBaseFuzzOptions(64, 64, targetKbps, 0)
	apply := make(map[int]func(*testing.T, *VP8Encoder), frames)
	for frame := 1; frame < frames; frame++ {
		switch r.pick(7) {
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
	return oracleRuntimeControlFuzzCase{
		name:       "invalid-noop",
		opts:       opts,
		targetKbps: targetKbps,
		sources:    oracleRuntimeFuzzSources(opts.Width, opts.Height, frames, 0),
		flags:      nil,
		script:     runtimeControlScript(frames, nil),
		apply:      apply,
	}
}
