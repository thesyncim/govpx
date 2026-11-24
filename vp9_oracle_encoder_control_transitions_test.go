//go:build govpx_oracle_trace

package govpx_test

import (
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
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
// Each subtest also captures the per-frame trace rows so a
// failure mode that drives a row-level delta without crossing the
// strict byte threshold still surfaces in the test log.
//
// Strict byte parity is opt-in through
// GOVPX_VP9_TRANSITIONS_STRICT=1; the default build logs trace
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

	baseOpts := func(fx fixture) govpx.VP9EncoderOptions {
		return vp9oracle.CBROptions(fx.w, fx.h, target)
	}
	baseArgs := func() []string {
		return vp9oracle.CBRArgs(target, 600, 400, 500, 0)
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
				2:  vp9StepAQ(govpx.VP9AQVariance),
				5:  vp9StepAQ(govpx.VP9AQComplexity),
				8:  vp9StepAQ(govpx.VP9AQCyclicRefresh),
				11: vp9StepAQ(govpx.VP9AQNone),
			},
		},
		{
			name: "aq-off-equator360-perceptual-off",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3:  vp9StepAQ(govpx.VP9AQEquator360),
				6:  vp9StepAQ(govpx.VP9AQPerceptual),
				10: vp9StepAQ(govpx.VP9AQNone),
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
				4: vp9StepRateControlMode(govpx.RateControlVBR, target),
				9: vp9StepRateControlMode(govpx.RateControlCBR, target),
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
				3: vp9StepColorSpace(govpx.VP9ColorSpace(4)), // BT.709
				8: vp9StepColorRange(govpx.VP9ColorRangeFull),
			},
		},

		// ---- loopfilter toggle pair ----
		{
			name: "loopfilter-inter-then-all",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepDisableLoopfilter(govpx.VP9LoopfilterDisableInter),
				8: vp9StepDisableLoopfilter(govpx.VP9LoopfilterDisableAll),
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
				3: vp9StepAQ(govpx.VP9AQVariance),
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
				3: vp9StepDeadline(govpx.DeadlineGoodQuality),
				8: vp9StepCPUUsed(0),
			},
		},

		// ---- tuning + sharpness pair ----
		{
			name: "tuning-then-sharpness",
			fx:   fxBase,
			updates: map[int]vp9ControlStep{
				3: vp9StepTuning(govpx.TuneSSIM),
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
