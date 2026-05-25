//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"strconv"
	"testing"
)

// TestVP9OracleEncoderRuntimeControls is the VP9 mirror of VP8's
// TestOracleEncoderStreamByteParityRuntimeControls. Each subtest exercises one
// runtime VP9Encoder.Set* method mid-stream and asserts byte-by-byte parity
// against the libvpx vpxenc-vp9-frameflags driver. The driver applies the
// equivalent libvpx control through its --control-script= token at the same
// frame index.
//
// Single-control coverage lives here; multi-control transition matrices live
// in vp9_oracle_encoder_transitions_test.go.
//
// Strict parity is gated by GOVPX_VP9_RUNTIME_CONTROLS_STRICT=1; the default
// build runs the gate and logs row deltas so per-control regressions show up
// in test output even when the build is not in strict mode. Byte mismatches
// at non-pinned controls are logged with the per-frame scoreboard rows to
// steer parity work.
func TestVP9OracleEncoderRuntimeControls(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 runtime-controls byte-parity gate")
	vp9test.RequireVpxencFrameFlags(t)

	const (
		width  = 64
		height = 64
		frames = 10
		target = 600
	)

	baseOpts := func() VP9EncoderOptions {
		return vp9OracleCBROptions(width, height, target)
	}
	baseArgs := func() []string {
		return vp9OracleCBRArgs(target, 600, 400, 500, 0)
	}

	cases := []vp9RuntimeControlCase{
		{
			name:      "set-bitrate-kbps",
			applyAt:   4,
			scriptTok: "bitrate:300",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetBitrateKbps", e.SetBitrateKbps(300))
			},
		},
		{
			name:    "set-rate-control-vbr",
			applyAt: 4,
			scriptTok: "endusage:vbr+bitrate:" + strconv.Itoa(target) +
				"+minq:4+maxq:56+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetRateControl(VBR)", e.SetRateControl(RateControlConfig{
					Mode:                RateControlVBR,
					TargetBitrateKbps:   target,
					MinQuantizer:        4,
					MaxQuantizer:        56,
					UndershootPct:       100,
					OvershootPct:        100,
					BufferSizeMs:        6000,
					BufferInitialSizeMs: 4000,
					BufferOptimalSizeMs: 5000,
				}))
			},
		},
		{
			name:      "set-cq-level",
			applyAt:   4,
			scriptTok: "cq:30",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetCQLevel(30)", e.SetCQLevel(30))
			},
		},
		{
			name:      "set-aq-mode-variance",
			applyAt:   4,
			scriptTok: "aq:1",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetAQMode(Variance)", e.SetAQMode(VP9AQVariance))
			},
		},
		{
			name:      "set-aq-mode-complexity",
			applyAt:   4,
			scriptTok: "aq:2",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetAQMode(Complexity)", e.SetAQMode(VP9AQComplexity))
			},
		},
		{
			name:      "set-aq-mode-cyclic",
			applyAt:   4,
			scriptTok: "aq:3",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetAQMode(Cyclic)", e.SetAQMode(VP9AQCyclicRefresh))
			},
		},
		{
			name:      "set-tuning-ssim",
			applyAt:   4,
			scriptTok: "tune:ssim",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetTuning(SSIM)", e.SetTuning(TuneSSIM))
			},
		},
		{
			name:      "set-sharpness",
			applyAt:   4,
			scriptTok: "sharpness:4",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetSharpness(4)", e.SetSharpness(4))
			},
		},
		{
			name:      "set-noise-sensitivity",
			applyAt:   4,
			scriptTok: "noise:2",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetNoiseSensitivity(2)", e.SetNoiseSensitivity(2))
			},
		},
		{
			name:      "set-static-threshold",
			applyAt:   4,
			scriptTok: "static:200",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetStaticThreshold(200)", e.SetStaticThreshold(200))
			},
		},
		{
			name:      "set-screen-content-on",
			applyAt:   4,
			scriptTok: "screen:1",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetScreenContent(1)", e.SetScreenContentMode(1))
			},
		},
		{
			name:      "set-deadline-good",
			applyAt:   4,
			scriptTok: "deadline:good",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetDeadline(GoodQuality)", e.SetDeadline(DeadlineGoodQuality))
			},
		},
		{
			name:      "set-cpu-used",
			applyAt:   4,
			scriptTok: "cpu:4",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetCPUUsed(4)", e.SetCPUUsed(4))
			},
		},
		{
			name:      "set-frame-parallel-off",
			applyAt:   4,
			scriptTok: "frame-parallel:0",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetFrameParallelDecoding(false)", e.SetFrameParallelDecoding(false))
			},
		},
		{
			name:      "set-rtc-external-rc",
			applyAt:   4,
			scriptTok: "rtc:1",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
			},
		},
		{
			name:      "set-color-space-bt709",
			applyAt:   4,
			scriptTok: "colorspace:4",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetColorSpace(BT709)", e.SetColorSpace(VP9ColorSpace(4)))
			},
		},
		{
			name:      "set-color-range-full",
			applyAt:   4,
			scriptTok: "colorrange:1",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetColorRange(full)", e.SetColorRange(VP9ColorRangeFull))
			},
		},
		{
			name:      "set-render-size",
			applyAt:   4,
			scriptTok: "rendersize:64x64",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetRenderSize(64,64)", e.SetRenderSize(64, 64))
			},
		},
		{
			name:      "set-target-level-unconstrained",
			applyAt:   4,
			scriptTok: "targetlevel:255",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetTargetLevel(255)", e.SetTargetLevel(255))
			},
		},
		{
			name:      "set-target-level-auto",
			applyAt:   4,
			scriptTok: "targetlevel:0",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetTargetLevel(0=auto)", e.SetTargetLevel(0))
			},
		},
		{
			name:      "set-disable-loopfilter-inter",
			applyAt:   4,
			scriptTok: "disableloopfilter:1",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetDisableLoopfilter(Inter)", e.SetDisableLoopfilter(VP9LoopfilterDisableInter))
			},
		},
		{
			name:      "set-delta-q-uv",
			applyAt:   4,
			scriptTok: "deltaquv:4",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetDeltaQUV(4)", e.SetDeltaQUV(4))
			},
		},
		{
			name:      "set-max-inter-bitrate-pct",
			applyAt:   4,
			scriptTok: "maxinter:200",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetMaxInterBitratePct(200)", e.SetMaxInterBitratePct(200))
			},
		},
		{
			name:      "set-max-intra-bitrate-pct",
			applyAt:   4,
			scriptTok: "maxintra:200",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetMaxIntraBitratePct(200)", e.SetMaxIntraBitratePct(200))
			},
		},
		{
			name:      "set-gf-cbr-boost-pct",
			applyAt:   4,
			scriptTok: "gfboost:50",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetGFCBRBoostPct(50)", e.SetGFCBRBoostPct(50))
			},
		},
		{
			name:      "set-min-gf-interval",
			applyAt:   4,
			scriptTok: "mingf:8",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetMinGFInterval(8)", e.SetMinGFInterval(8))
			},
		},
		{
			name:      "set-max-gf-interval",
			applyAt:   4,
			scriptTok: "maxgf:16",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetMaxGFInterval(16)", e.SetMaxGFInterval(16))
			},
		},
		{
			name:      "set-frame-periodic-boost",
			applyAt:   4,
			scriptTok: "periodicboost:1",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetFramePeriodicBoost(true)", e.SetFramePeriodicBoost(true))
			},
		},
		{
			name:      "set-altref-aq",
			applyAt:   4,
			scriptTok: "altrefaq:1",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetAltRefAQ(true)", e.SetAltRefAQ(true))
			},
		},
		{
			name:      "set-postencode-drop",
			applyAt:   4,
			scriptTok: "postdrop:1",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetPostEncodeDrop(true)", e.SetPostEncodeDrop(true))
			},
		},
		{
			name:      "set-disable-overshoot-maxq-cbr",
			applyAt:   4,
			scriptTok: "disovershoot:1",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetDisableOvershootMaxQCBR(true)", e.SetDisableOvershootMaxQCBR(true))
			},
		},
		{
			name:      "set-next-frame-qindex",
			applyAt:   4,
			scriptTok: "qonepass:128",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetNextFrameQIndex(128)", e.SetNextFrameQIndex(128))
			},
		},
		{
			name:      "set-frame-drop-allowed",
			applyAt:   4,
			scriptTok: "drop:60",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetFrameDropAllowed(true)", e.SetFrameDropAllowed(true))
			},
		},
		{
			name:      "set-rate-control-buffer",
			applyAt:   4,
			scriptTok: "bufsz:8000+bufinit:5000+bufopt:6000",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetRateControlBuffer", e.SetRateControlBuffer(8000, 5000, 6000))
			},
		},
		{
			name:      "set-realtime-target-bitrate",
			applyAt:   4,
			scriptTok: "bitrate:400",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetRealtimeTarget(bitrate)", e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 400}))
			},
		},
		{
			name:      "set-realtime-target-quantizers",
			applyAt:   4,
			scriptTok: "minq:32+maxq:32",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetRealtimeTarget(q)", e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 32, MaxQuantizer: 32}))
			},
		},
		{
			name:      "set-realtime-target-fps",
			applyAt:   4,
			scriptTok: "fps:15",
			apply: func(t *testing.T, e *VP9Encoder) {
				mustVP9Runtime(t, "SetRealtimeTarget(fps)", e.SetRealtimeTarget(RealtimeTarget{FPS: 15}))
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			runVP9RuntimeControlCase(t, baseOpts(), baseArgs(), width, height, frames, tc)
		})
	}
}
