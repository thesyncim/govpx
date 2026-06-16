//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil"
	"strconv"
	"strings"
	"testing"
)

type vp8OracleRuntimeFuzzAction struct {
	token       string
	phase       uint8
	apply       func(*testing.T, *VP8Encoder)
	applyConfig func(*testing.T, *VP8Encoder)
	applyCodec  func(*testing.T, *VP8Encoder)
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
		mask := r.Pick(4)
		return vp8OracleRuntimeFuzzAction{
			token: "error:" + strconv.Itoa(mask),
			phase: vp8OracleRuntimeFuzzConfigPhase,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetErrorResilient", e.SetErrorResilient(mask&1 != 0, mask&2 != 0))
			},
		}, 0, false
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
