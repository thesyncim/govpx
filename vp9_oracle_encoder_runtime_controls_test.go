//go:build govpx_oracle_trace

package govpx_test

import (
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
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
// at non-pinned controls are logged with the per-frame trace rows to
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

	baseOpts := func() govpx.VP9EncoderOptions {
		return vp9oracle.CBROptions(width, height, target)
	}
	baseArgs := func() []string {
		return vp9oracle.CBRArgs(target, 600, 400, 500, 0)
	}

	cases := []vp9oracle.RuntimeControlCase{
		{
			Name:        "set-bitrate-kbps",
			ApplyAt:     4,
			ScriptToken: "bitrate:300",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetBitrateKbps", e.SetBitrateKbps(300))
			},
		},
		{
			Name:    "set-rate-control-vbr",
			ApplyAt: 4,
			ScriptToken: "endusage:vbr+bitrate:" + strconv.Itoa(target) +
				"+minq:4+maxq:56+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetRateControl(VBR)", e.SetRateControl(govpx.RateControlConfig{
					Mode:                govpx.RateControlVBR,
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
			Name:        "set-cq-level",
			ApplyAt:     4,
			ScriptToken: "cq:30",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetCQLevel(30)", e.SetCQLevel(30))
			},
		},
		// AQ-mode cases apply the control at frame 0 (pre-stream). govpx's
		// VP9Encoder.SetAQMode only accepts the change before the first coded
		// frame and returns ErrInvalidConfig afterward (vp9_encoder_config.go
		// SetAQMode; contract pinned by vp9_encoder_runtime_ratecontrol_test.go),
		// because enabling/disabling AQ allocates or tears down its segment map.
		// libvpx's VP9E_SET_AQ_MODE (vp9/vp9_cx_iface.c:1054 ctrl_set_aq_mode ->
		// update_extra_cfg -> vp9_change_config) would accept it at any frame, but
		// govpx does not surface a mid-stream AQ reconfiguration, so the supported
		// path is to arm the mode before frame 0. The libvpx driver applies the
		// matching aq:N token at frame 0 as well.
		{
			Name:        "set-aq-mode-variance",
			ApplyAt:     0,
			ScriptToken: "aq:1",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetAQMode(Variance)", e.SetAQMode(govpx.VP9AQVariance))
			},
		},
		{
			Name:        "set-aq-mode-complexity",
			ApplyAt:     0,
			ScriptToken: "aq:2",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetAQMode(Complexity)", e.SetAQMode(govpx.VP9AQComplexity))
			},
		},
		{
			Name:        "set-aq-mode-cyclic",
			ApplyAt:     0,
			ScriptToken: "aq:3",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetAQMode(Cyclic)", e.SetAQMode(govpx.VP9AQCyclicRefresh))
			},
		},
		{
			Name:        "set-tuning-ssim",
			ApplyAt:     4,
			ScriptToken: "tune:ssim",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetTuning(SSIM)", e.SetTuning(govpx.TuneSSIM))
			},
		},
		{
			Name:        "set-sharpness",
			ApplyAt:     4,
			ScriptToken: "sharpness:4",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetSharpness(4)", e.SetSharpness(4))
			},
		},
		{
			Name:        "set-noise-sensitivity",
			ApplyAt:     4,
			ScriptToken: "noise:2",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetNoiseSensitivity(2)", e.SetNoiseSensitivity(2))
			},
		},
		{
			Name:        "set-static-threshold",
			ApplyAt:     4,
			ScriptToken: "static:200",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetStaticThreshold(200)", e.SetStaticThreshold(200))
			},
		},
		{
			Name:        "set-screen-content-on",
			ApplyAt:     4,
			ScriptToken: "screen:1",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetScreenContent(1)", e.SetScreenContentMode(1))
			},
		},
		{
			Name:        "set-deadline-good",
			ApplyAt:     4,
			ScriptToken: "deadline:good",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetDeadline(GoodQuality)", e.SetDeadline(govpx.DeadlineGoodQuality))
			},
		},
		{
			Name:        "set-cpu-used",
			ApplyAt:     4,
			ScriptToken: "cpu:4",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetCPUUsed(4)", e.SetCPUUsed(4))
			},
		},
		{
			Name:        "set-frame-parallel-off",
			ApplyAt:     4,
			ScriptToken: "frame-parallel:0",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetFrameParallelDecoding(false)", e.SetFrameParallelDecoding(false))
			},
		},
		{
			Name:        "set-rtc-external-rc",
			ApplyAt:     4,
			ScriptToken: "rtc:1",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
			},
		},
		{
			Name:        "set-color-space-bt709",
			ApplyAt:     4,
			ScriptToken: "colorspace:4",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetColorSpace(BT709)", e.SetColorSpace(govpx.VP9ColorSpace(4)))
			},
		},
		{
			Name:        "set-color-range-full",
			ApplyAt:     4,
			ScriptToken: "colorrange:1",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetColorRange(full)", e.SetColorRange(govpx.VP9ColorRangeFull))
			},
		},
		{
			// The frameflags driver parses rendersize as slash-separated ints
			// (parse_slash_ints, vpxenc_vp9_frameflags.c) and forwards them to
			// VP9E_SET_RENDER_SIZE as a {width, height} pair.
			Name:        "set-render-size",
			ApplyAt:     4,
			ScriptToken: "rendersize:64/64",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetRenderSize(64,64)", e.SetRenderSize(64, 64))
			},
		},
		{
			Name:        "set-target-level-unconstrained",
			ApplyAt:     4,
			ScriptToken: "targetlevel:255",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetTargetLevel(255)", e.SetTargetLevel(255))
			},
		},
		{
			Name:        "set-target-level-auto",
			ApplyAt:     4,
			ScriptToken: "targetlevel:1",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetTargetLevel(auto)",
					e.SetTargetLevel(govpx.VP9TargetLevelAuto))
			},
		},
		{
			Name:        "set-disable-loopfilter-inter",
			ApplyAt:     4,
			ScriptToken: "disableloopfilter:1",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetDisableLoopfilter(Inter)", e.SetDisableLoopfilter(govpx.VP9LoopfilterDisableInter))
			},
		},
		{
			Name:        "set-delta-q-uv",
			ApplyAt:     4,
			ScriptToken: "deltaquv:4",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetDeltaQUV(4)", e.SetDeltaQUV(4))
			},
		},
		{
			Name:        "set-max-inter-bitrate-pct",
			ApplyAt:     4,
			ScriptToken: "maxinter:200",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetMaxInterBitratePct(200)", e.SetMaxInterBitratePct(200))
			},
		},
		{
			Name:        "set-max-intra-bitrate-pct",
			ApplyAt:     4,
			ScriptToken: "maxintra:200",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetMaxIntraBitratePct(200)", e.SetMaxIntraBitratePct(200))
			},
		},
		{
			Name:        "set-gf-cbr-boost-pct",
			ApplyAt:     4,
			ScriptToken: "gfboost:50",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetGFCBRBoostPct(50)", e.SetGFCBRBoostPct(50))
			},
		},
		{
			Name:        "set-min-gf-interval",
			ApplyAt:     4,
			ScriptToken: "mingf:8",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetMinGFInterval(8)", e.SetMinGFInterval(8))
			},
		},
		{
			Name:        "set-max-gf-interval",
			ApplyAt:     4,
			ScriptToken: "maxgf:16",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetMaxGFInterval(16)", e.SetMaxGFInterval(16))
			},
		},
		{
			Name:        "set-frame-periodic-boost",
			ApplyAt:     4,
			ScriptToken: "periodicboost:1",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetFramePeriodicBoost(true)", e.SetFramePeriodicBoost(true))
			},
		},
		{
			Name:        "set-altref-aq",
			ApplyAt:     4,
			ScriptToken: "altrefaq:1",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetAltRefAQ(true)", e.SetAltRefAQ(true))
			},
		},
		{
			Name:        "set-postencode-drop",
			ApplyAt:     4,
			ScriptToken: "postdrop:1",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetPostEncodeDrop(true)", e.SetPostEncodeDrop(true))
			},
		},
		{
			Name:        "set-disable-overshoot-maxq-cbr",
			ApplyAt:     4,
			ScriptToken: "disovershoot:1",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetDisableOvershootMaxQCBR(true)", e.SetDisableOvershootMaxQCBR(true))
			},
		},
		{
			// libvpx VP9E_SET_QUANTIZER_ONE_PASS (vp9/vp9_cx_iface.c:2105
			// ctrl_set_quantizer_one_pass) takes a quantizer in [0, 63] and
			// rejects anything above 63 with VPX_CODEC_INVALID_PARAM. It then
			// maps that quantizer to an internal qindex via quantizer_to_qindex
			// (vp9/encoder/vp9_quantize.c:315), where quantizer 32 -> qindex 128.
			// govpx's SetNextFrameQIndex consumes a raw qindex in [0, 255], so we
			// drive the libvpx side at quantizer 32 and the govpx side at the
			// equivalent qindex 128 to target the same next-frame qindex.
			Name:        "set-next-frame-qindex",
			ApplyAt:     4,
			ScriptToken: "qonepass:32",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetNextFrameQIndex(128)", e.SetNextFrameQIndex(128))
			},
		},
		{
			Name:        "set-frame-drop-allowed",
			ApplyAt:     4,
			ScriptToken: "drop:60",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetFrameDropAllowed(true)", e.SetFrameDropAllowed(true))
			},
		},
		{
			Name:        "set-rate-control-buffer",
			ApplyAt:     4,
			ScriptToken: "bufsz:8000+bufinit:5000+bufopt:6000",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetRateControlBuffer", e.SetRateControlBuffer(8000, 5000, 6000))
			},
		},
		{
			Name:        "set-realtime-target-bitrate",
			ApplyAt:     4,
			ScriptToken: "bitrate:400",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetRealtimeTarget(bitrate)", e.SetRealtimeTarget(govpx.RealtimeTarget{BitrateKbps: 400}))
			},
		},
		{
			Name:        "set-realtime-target-quantizers",
			ApplyAt:     4,
			ScriptToken: "minq:32+maxq:32",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetRealtimeTarget(q)", e.SetRealtimeTarget(govpx.RealtimeTarget{MinQuantizer: 32, MaxQuantizer: 32}))
			},
		},
		{
			Name:        "set-realtime-target-fps",
			ApplyAt:     4,
			ScriptToken: "fps:15",
			Apply: func(t testing.TB, e *govpx.VP9Encoder) {
				vp9oracle.MustRuntime(t, "SetRealtimeTarget(fps)", e.SetRealtimeTarget(govpx.RealtimeTarget{FPS: 15}))
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			vp9oracle.RunRuntimeControlCase(t, baseOpts(), baseArgs(), width, height, frames, tc)
		})
	}
}
