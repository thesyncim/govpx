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
// the resulting packets and trace rows, and compares them. It logs
// the per-frame trace for failure triage and asserts byte parity
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
		vp9test.FormatRateTraceRows(govpxRows, libvpxRows))
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
