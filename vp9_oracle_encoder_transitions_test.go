//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"strconv"
	"strings"
	"testing"
)

// TestVP9OracleEncoderControlTransitions is the VP9 counterpart of
// VP8's TestOracleEncoderStreamByteParityRuntimeControls extended to
// multi-control transitions: each case toggles two (or three) VP9
// runtime controls at distinct frames in the same stream, and
// asserts byte-by-byte parity against the libvpx
// vpxenc-vp9-frameflags driver replaying the same --control-script
// schedule. The point is the *interaction* between sequential
// control updates: state-leak regressions (sticky ROI, lingering AQ,
// or temporal-pattern state surviving a count change) show up here
// even when each control is individually pinned by
// vp9_oracle_encoder_runtime_controls_test.go.
//
// Each subtest also captures the per-frame scoreboard rows so a
// failure mode that drives a row-level delta without crossing the
// strict byte threshold still surfaces in the test log.
//
// Strict byte parity is opt-in through
// GOVPX_VP9_TRANSITIONS_STRICT=1; the default build logs scoreboard
// deltas without failing so the test acts as a structured oracle
// that future parity work can ratchet.
func TestVP9OracleEncoderControlTransitions(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 control transition byte-parity gate")
	vp9test.RequireVpxencFrameFlags(t)

	const (
		fps    = 30
		target = 600
		frames = 12
	)

	type fixture struct {
		name string
		w, h int
	}
	fxSmall := fixture{name: "small-16x16", w: 16, h: 16}
	fxBase := fixture{name: "panning-64x64", w: 64, h: 64}
	fx320 := fixture{name: "panning-320x180", w: 320, h: 180}
	fxBig := fixture{name: "panning-640x480", w: 640, h: 480}
	fx720 := fixture{name: "panning-1280x720", w: 1280, h: 720}

	baseOpts := func(fx fixture) VP9EncoderOptions {
		return vp9OracleCBROptions(fx.w, fx.h, target)
	}
	baseArgs := func() []string {
		return vp9OracleCBRArgs(target, 600, 400, 500, 0)
	}

	cases := []vp9TransitionCase{
		// ---- bitrate down -> up across resolutions ----
		{
			name: "bitrate-down-up-16x16",
			fx:   fxSmall,
			updates: map[int]vp9ControlStep{
				3: vp9StepBitrate(300),
				8: vp9StepBitrate(1200),
			},
		},
		{
			name: "bitrate-down-up-64x64",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepBitrate(300),
				8: vp9StepBitrate(1200),
			},
		},
		{
			name: "bitrate-down-up-320x180",
			fx:   fx320,
			updates: map[int]vp9ControlStep{
				3: vp9StepBitrate(300),
				8: vp9StepBitrate(1200),
			},
		},
		{
			name: "bitrate-down-up-640x480",
			fx:   fxBig,
			updates: map[int]vp9ControlStep{
				3: vp9StepBitrate(300),
				8: vp9StepBitrate(1200),
			},
		},
		{
			name: "bitrate-down-up-1280x720",
			fx:   fx720,
			updates: map[int]vp9ControlStep{
				3: vp9StepBitrate(300),
				8: vp9StepBitrate(1200),
			},
		},

		// ---- AQ progression: off -> variance -> complexity -> cyclic -> off ----
		{
			name: "aq-off-variance-complexity-cyclic-off",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				2:  vp9StepAQ(VP9AQVariance),
				5:  vp9StepAQ(VP9AQComplexity),
				8:  vp9StepAQ(VP9AQCyclicRefresh),
				11: vp9StepAQ(VP9AQNone),
			},
		},
		{
			name: "aq-off-equator360-perceptual-off",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3:  vp9StepAQ(VP9AQEquator360),
				6:  vp9StepAQ(VP9AQPerceptual),
				10: vp9StepAQ(VP9AQNone),
			},
		},

		// ---- screen content off -> on -> off ----
		{
			name: "screen-content-off-on-off",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepScreenContent(1),
				9: vp9StepScreenContent(0),
			},
		},

		// ---- keyframe cadence change ----
		{
			name: "kf-cadence-30-90-30",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepKFInterval(90),
				9: vp9StepKFInterval(30),
			},
		},

		// ---- rate-control mode switches (CBR -> VBR -> CBR) ----
		{
			name: "rate-control-cbr-vbr-cbr",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				4: vp9StepRateControlMode(RateControlVBR, target),
				9: vp9StepRateControlMode(RateControlCBR, target),
			},
		},

		// ---- post-encode-drop + disable-overshoot-maxq pair ----
		{
			name: "postdrop-disovershoot-pair",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				4: vp9StepCompose(
					vp9StepPostEncodeDrop(true),
					vp9StepDisableOvershootMaxQCBR(true),
				),
				9: vp9StepCompose(
					vp9StepPostEncodeDrop(false),
					vp9StepDisableOvershootMaxQCBR(false),
				),
			},
		},

		// ---- target-level transitions ----
		{
			name: "target-level-unconstrained-then-30",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				4: vp9StepTargetLevel(30),
				9: vp9StepTargetLevel(255),
			},
		},

		// ---- GF interval pair ----
		{
			name: "gf-interval-min-then-max",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepMinGFInterval(4),
				8: vp9StepMaxGFInterval(12),
			},
		},

		// ---- color metadata pair ----
		{
			name: "color-space-then-range",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepColorSpace(VP9ColorSpace(4)), // BT.709
				8: vp9StepColorRange(VP9ColorRangeFull),
			},
		},

		// ---- loopfilter toggle pair ----
		{
			name: "loopfilter-inter-then-all",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepDisableLoopfilter(VP9LoopfilterDisableInter),
				8: vp9StepDisableLoopfilter(VP9LoopfilterDisableAll),
			},
		},

		// ---- triple toggle: bitrate + cpu + sharpness ----
		{
			name: "bitrate-cpu-sharpness-triple",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepBitrate(300),
				6: vp9StepCPUUsed(4),
				9: vp9StepSharpness(4),
			},
		},

		// ---- triple toggle: AQ + delta-Q UV + noise sensitivity ----
		{
			name: "aq-deltaquv-noise-triple",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepAQ(VP9AQVariance),
				6: vp9StepDeltaQUV(4),
				9: vp9StepNoiseSensitivity(2),
			},
		},

		// ---- bitrate + buffer pair ----
		{
			name: "bitrate-then-buffer",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepBitrate(400),
				8: vp9StepRateControlBuffer(8000, 5000, 6000),
			},
		},

		// ---- deadline + cpu pair ----
		{
			name: "deadline-then-cpu",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepDeadline(DeadlineGoodQuality),
				8: vp9StepCPUUsed(0),
			},
		},

		// ---- tuning + sharpness pair ----
		{
			name: "tuning-then-sharpness",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepTuning(TuneSSIM),
				8: vp9StepSharpness(4),
			},
		},

		// ---- delta-Q UV both signs ----
		{
			name: "deltaquv-positive-negative",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepDeltaQUV(6),
				8: vp9StepDeltaQUV(-6),
			},
		},

		// ---- maxinter then disable ----
		{
			name: "maxinter-200-then-0",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepMaxInterBitratePct(200),
				8: vp9StepMaxInterBitratePct(0),
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			runVP9TransitionCase(t, baseOpts(tc.fx), baseArgs(),
				tc.fx.w, tc.fx.h, frames, tc)
		})
	}
}

// TestVP9OracleEncoderResetTransitions pins encoder-lifetime transitions that
// are not represented by one-shot vpxenc invocations: Reset must match a
// cold start after warm state has been discarded. The VP8 oracle gate's
// equivalent surface is TestOracleEncoderStreamByteParityResetFlushTransitions.
// On the VP9 side we don't currently have a Reset() method, so the
// equivalent surface is "construct fresh after Close" — the test still
// expresses the invariant that the second-stream packets must match a
// cold-start encoding when run independently against the oracle.
func TestVP9OracleEncoderResetTransitions(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 reset/lifetime byte-parity gate")
	vp9test.RequireVpxencFrameFlags(t)

	const (
		width  = 64
		height = 64
		warm   = 6
		after  = 8
	)
	opts := vp9OracleCBROptions(width, height, 600)
	extraArgs := vp9OracleCBRArgs(600, 600, 400, 500, 0)

	t.Run("cold-start-matches-libvpx", func(t *testing.T) {
		coldSources := vp9OracleTransitionPanningSources(width, height, after, 0)
		// First encode through govpx without any warm state.
		_, govpxPackets := captureGovpxVP9StreamParityPacketRowsWithHooks(t,
			opts, coldSources, make([]EncodeFlags, after), nil)
		_, libvpxPackets := captureLibvpxVP9StreamParityPacketRows(t,
			coldSources, make([]EncodeFlags, after), extraArgs)
		matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
			libvpxPackets)
		t.Logf("VP9 cold-start parity: matches=%d/%d first_mismatch=%d",
			matches, len(govpxPackets), firstMismatch)
		if vp9test.StrictEnv("GOVPX_VP9_TRANSITIONS_STRICT") &&
			matches != len(govpxPackets) {
			t.Fatalf("strict VP9 cold-start parity: matches=%d/%d", matches, len(govpxPackets))
		}
	})

	t.Run("fresh-encoder-after-warmup-matches-cold-start", func(t *testing.T) {
		// Encode the warm sources then discard the encoder and start
		// a fresh one with the original options. Compare against a
		// cold-start oracle that never saw the warm phase.
		warmSources := vp9OracleTransitionPanningSources(width, height, warm, 0)
		coldSources := vp9OracleTransitionPanningSources(width, height, after, warm)

		// Drive the warm phase but ignore its packets.
		_, _ = captureGovpxVP9StreamParityPacketRowsWithHooks(t, opts,
			warmSources, make([]EncodeFlags, warm), nil)

		// Now drive a fresh encoder over the post-warmup sources.
		_, govpxPackets := captureGovpxVP9StreamParityPacketRowsWithHooks(t,
			opts, coldSources, make([]EncodeFlags, after), nil)
		_, libvpxPackets := captureLibvpxVP9StreamParityPacketRows(t,
			coldSources, make([]EncodeFlags, after), extraArgs)
		matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
			libvpxPackets)
		t.Logf("VP9 fresh-encoder-after-warmup parity: matches=%d/%d first_mismatch=%d",
			matches, len(govpxPackets), firstMismatch)
		if vp9test.StrictEnv("GOVPX_VP9_TRANSITIONS_STRICT") &&
			matches != len(govpxPackets) {
			t.Fatalf("strict VP9 fresh-encoder-after-warmup parity: matches=%d/%d",
				matches, len(govpxPackets))
		}
	})
}

// vp9TransitionCase describes one multi-control transition scenario.
type vp9TransitionCase struct {
	name string
	fx   struct {
		name string
		w, h int
	}
	extraArgs []string
	updates   map[int]vp9ControlStep
}

// vp9ControlStep is one (apply, scriptToken) pair representing a single
// control change at a single frame. Steps compose so a single frame can
// toggle multiple controls atomically.
type vp9ControlStep struct {
	apply       func(*testing.T, *VP9Encoder)
	scriptToken string
}

// vp9StepCompose returns a step that applies multiple sub-steps in order
// and emits their tokens joined by '+'. The libvpx driver consumes '+'-
// separated tokens per frame entry.
func vp9StepCompose(steps ...vp9ControlStep) vp9ControlStep {
	tokens := make([]string, 0, len(steps))
	calls := make([]func(*testing.T, *VP9Encoder), 0, len(steps))
	for _, s := range steps {
		if s.scriptToken != "" {
			tokens = append(tokens, s.scriptToken)
		}
		if s.apply != nil {
			calls = append(calls, s.apply)
		}
	}
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			for _, c := range calls {
				c(t, e)
			}
		},
		scriptToken: strings.Join(tokens, "+"),
	}
}

func vp9StepBitrate(kbps int) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetBitrateKbps", e.SetBitrateKbps(kbps))
		},
		scriptToken: "bitrate:" + strconv.Itoa(kbps),
	}
}

func vp9StepAQ(mode VP9AQMode) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetAQMode", e.SetAQMode(mode))
		},
		scriptToken: "aq:" + strconv.Itoa(int(mode)),
	}
}

func vp9StepScreenContent(mode int) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetScreenContentMode", e.SetScreenContentMode(mode))
		},
		scriptToken: "screen:" + strconv.Itoa(mode),
	}
}

func vp9StepKFInterval(frames int) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetKeyFrameInterval", e.SetKeyFrameInterval(frames))
		},
		scriptToken: "kfmax:" + strconv.Itoa(frames),
	}
}

func vp9StepRateControlMode(mode RateControlMode, kbps int) vp9ControlStep {
	endUsage := "cbr"
	switch mode {
	case RateControlVBR:
		endUsage = "vbr"
	case RateControlCQ:
		endUsage = "cq"
	case RateControlQ:
		endUsage = "q"
	}
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetRateControl", e.SetRateControl(RateControlConfig{
				Mode:                mode,
				TargetBitrateKbps:   kbps,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				UndershootPct:       100,
				OvershootPct:        100,
				BufferSizeMs:        6000,
				BufferInitialSizeMs: 4000,
				BufferOptimalSizeMs: 5000,
			}))
		},
		scriptToken: "endusage:" + endUsage + "+bitrate:" + strconv.Itoa(kbps) +
			"+minq:4+maxq:56+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000",
	}
}

func vp9StepPostEncodeDrop(on bool) vp9ControlStep {
	v := 0
	if on {
		v = 1
	}
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetPostEncodeDrop", e.SetPostEncodeDrop(on))
		},
		scriptToken: "postdrop:" + strconv.Itoa(v),
	}
}

func vp9StepDisableOvershootMaxQCBR(on bool) vp9ControlStep {
	v := 0
	if on {
		v = 1
	}
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetDisableOvershootMaxQCBR", e.SetDisableOvershootMaxQCBR(on))
		},
		scriptToken: "disovershoot:" + strconv.Itoa(v),
	}
}

func vp9StepTargetLevel(level int) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetTargetLevel", e.SetTargetLevel(level))
		},
		scriptToken: "targetlevel:" + strconv.Itoa(level),
	}
}

func vp9StepMinGFInterval(n int) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetMinGFInterval", e.SetMinGFInterval(n))
		},
		scriptToken: "mingf:" + strconv.Itoa(n),
	}
}

func vp9StepMaxGFInterval(n int) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetMaxGFInterval", e.SetMaxGFInterval(n))
		},
		scriptToken: "maxgf:" + strconv.Itoa(n),
	}
}

func vp9StepColorSpace(cs VP9ColorSpace) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetColorSpace", e.SetColorSpace(cs))
		},
		scriptToken: "colorspace:" + strconv.Itoa(int(cs)),
	}
}

func vp9StepColorRange(cr VP9ColorRange) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetColorRange", e.SetColorRange(cr))
		},
		scriptToken: "colorrange:" + strconv.Itoa(int(cr)),
	}
}

func vp9StepDisableLoopfilter(mode VP9DisableLoopfilter) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetDisableLoopfilter", e.SetDisableLoopfilter(mode))
		},
		scriptToken: "disableloopfilter:" + strconv.Itoa(int(mode)),
	}
}

func vp9StepCPUUsed(cpu int) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetCPUUsed", e.SetCPUUsed(cpu))
		},
		scriptToken: "cpu:" + strconv.Itoa(cpu),
	}
}

func vp9StepSharpness(s uint8) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetSharpness", e.SetSharpness(s))
		},
		scriptToken: "sharpness:" + strconv.Itoa(int(s)),
	}
}

func vp9StepDeltaQUV(d int) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetDeltaQUV", e.SetDeltaQUV(d))
		},
		scriptToken: "deltaquv:" + strconv.Itoa(d),
	}
}

func vp9StepNoiseSensitivity(n int) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetNoiseSensitivity", e.SetNoiseSensitivity(n))
		},
		scriptToken: "noise:" + strconv.Itoa(n),
	}
}

func vp9StepRateControlBuffer(sizeMs, initMs, optMs int) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetRateControlBuffer",
				e.SetRateControlBuffer(sizeMs, initMs, optMs))
		},
		scriptToken: "bufsz:" + strconv.Itoa(sizeMs) +
			"+bufinit:" + strconv.Itoa(initMs) +
			"+bufopt:" + strconv.Itoa(optMs),
	}
}

func vp9StepDeadline(d Deadline) vp9ControlStep {
	tok := "rt"
	switch d {
	case DeadlineGoodQuality:
		tok = "good"
	case DeadlineBestQuality:
		tok = "best"
	}
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetDeadline", e.SetDeadline(d))
		},
		scriptToken: "deadline:" + tok,
	}
}

func vp9StepTuning(tu Tuning) vp9ControlStep {
	tok := "psnr"
	if tu == TuneSSIM {
		tok = "ssim"
	}
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetTuning", e.SetTuning(tu))
		},
		scriptToken: "tune:" + tok,
	}
}

func vp9StepMaxInterBitratePct(p int) vp9ControlStep {
	return vp9ControlStep{
		apply: func(t *testing.T, e *VP9Encoder) {
			mustVP9Runtime(t, "SetMaxInterBitratePct", e.SetMaxInterBitratePct(p))
		},
		scriptToken: "maxinter:" + strconv.Itoa(p),
	}
}

// runVP9TransitionCase drives both encoders through tc.updates, captures
// the resulting packets and scoreboard rows, and compares them. It logs
// the per-frame scoreboard for failure triage and asserts byte parity
// under GOVPX_VP9_TRANSITIONS_STRICT=1.
func runVP9TransitionCase(t *testing.T, opts VP9EncoderOptions,
	extraArgs []string, width, height, frames int, tc vp9TransitionCase,
) {
	t.Helper()
	sources := vp9OracleTransitionPanningSources(width, height, frames, 0)
	flags := make([]EncodeFlags, frames)

	before := func(enc *VP9Encoder, frame int) {
		if step, ok := tc.updates[frame]; ok && step.apply != nil {
			step.apply(t, enc)
		}
	}

	govpxRows, govpxPackets := captureGovpxVP9StreamParityPacketRowsWithHooks(t,
		opts, sources, flags, before)

	scriptMap := make(map[int]string, len(tc.updates))
	for frame, step := range tc.updates {
		scriptMap[frame] = step.scriptToken
	}
	libvpxArgs := append([]string(nil), extraArgs...)
	libvpxArgs = append(libvpxArgs, tc.extraArgs...)
	libvpxArgs = append(libvpxArgs,
		"--control-script="+strings.Join(vp9RuntimeControlScript(frames, scriptMap), ","))

	libvpxRows, libvpxPackets := captureLibvpxVP9StreamParityPacketRows(t,
		sources, flags, libvpxArgs)

	stats := vp9test.CompareTransitionRows(t, govpxRows, libvpxRows, vp9OracleLibvpxFrameFlags)
	matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets, libvpxPackets)
	t.Logf("VP9 transition %s: matches=%d/%d first_mismatch=%d stats=%s",
		tc.name, matches, len(govpxPackets), firstMismatch, stats)
	t.Logf("VP9 transition %s rows:\n%s", tc.name,
		vp9test.FormatRateScoreboardRows(govpxRows, libvpxRows))
	if vp9test.StrictEnv("GOVPX_VP9_TRANSITIONS_STRICT") {
		assertVP9TransitionByteParity(t, tc.name, govpxPackets, libvpxPackets)
	}
}

// assertVP9TransitionByteParity is the strict-mode gate: every visible
// packet must match libvpx byte-for-byte and drop classifications must
// agree. Failure messages include the byte length, first-diff offset, and
// per-frame index so triage doesn't require a hex viewer.
func assertVP9TransitionByteParity(t *testing.T, label string, got, want [][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("VP9 transition %s packet count: got=%d want=%d",
			label, len(got), len(want))
	}
	for i := range got {
		gotEmpty := len(got[i]) == 0
		wantEmpty := len(want[i]) == 0
		if gotEmpty != wantEmpty {
			t.Errorf("VP9 transition %s frame %d drop mismatch: got_empty=%t want_empty=%t",
				label, i, gotEmpty, wantEmpty)
			continue
		}
		if gotEmpty {
			continue
		}
		if !bytes.Equal(got[i], want[i]) {
			diff := testutil.FirstByteDiff(got[i], want[i])
			t.Errorf("VP9 transition %s frame %d byte mismatch: got_len=%d want_len=%d first_diff=%d",
				label, i, len(got[i]), len(want[i]), diff)
		}
	}
}

// vp9OracleTransitionPanningSources builds a panning I420 sequence of
// `count` frames starting at the given frame offset. The frames have
// non-trivial motion so rate-control bookkeeping and AQ have something to
// chew on across frames.
func vp9OracleTransitionPanningSources(width, height, count, offset int) []*image.YCbCr {
	sources := make([]*image.YCbCr, count)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i+offset)
	}
	return sources
}
