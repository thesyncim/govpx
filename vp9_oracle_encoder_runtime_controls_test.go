//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
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
	coracletest.SkipWithoutOracle(t, "VP9 runtime-controls byte-parity gate")
	coracletest.VpxencVP9FrameFlags(t)

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

// TestVP9OracleEncoderRuntimeControlsAllocationGate pins the steady-state
// allocation profile of the runtime Set* surface. After warmup, calling each
// covered setter must not allocate. This complements the scoreboard/byte
// parity check by guarding regressions in the runtime control hot path.
//
// The test is gated under the oracle build tag for source colocation with
// the rest of the runtime-control coverage, but it does not actually run
// the libvpx oracle and therefore costs <1s.
func TestVP9OracleEncoderRuntimeControlsAllocationGate(t *testing.T) {
	const width, height = 64, 64

	makeEncoder := func(t *testing.T) *VP9Encoder {
		t.Helper()
		opts := vp9OracleCBROptions(width, height, 600)
		e, err := NewVP9Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		size, err := vp9AllocatingEncodeBufferSize(width, height)
		if err != nil {
			t.Fatalf("vp9AllocatingEncodeBufferSize: %v", err)
		}
		dst := make([]byte, size)
		img := vp9test.NewPanningYCbCr(width, height, 0)
		if _, err := e.EncodeIntoWithResult(img, dst); err != nil {
			t.Fatalf("EncodeIntoWithResult warm: %v", err)
		}
		return e
	}

	allocCases := []struct {
		name string
		call func(t *testing.T, e *VP9Encoder)
	}{
		{"SetBitrateKbps", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetBitrateKbps(700); err != nil {
				t.Fatalf("SetBitrateKbps: %v", err)
			}
		}},
		{"SetCQLevel", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetCQLevel(30); err != nil {
				t.Fatalf("SetCQLevel: %v", err)
			}
		}},
		{"SetCPUUsed", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetCPUUsed(4); err != nil {
				t.Fatalf("SetCPUUsed: %v", err)
			}
		}},
		{"SetSharpness", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetSharpness(4); err != nil {
				t.Fatalf("SetSharpness: %v", err)
			}
		}},
		{"SetStaticThreshold", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetStaticThreshold(200); err != nil {
				t.Fatalf("SetStaticThreshold: %v", err)
			}
		}},
		{"SetMinGFInterval", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetMinGFInterval(8); err != nil {
				t.Fatalf("SetMinGFInterval: %v", err)
			}
		}},
		{"SetMaxGFInterval", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetMaxGFInterval(16); err != nil {
				t.Fatalf("SetMaxGFInterval: %v", err)
			}
		}},
		{"SetMaxInterBitratePct", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetMaxInterBitratePct(200); err != nil {
				t.Fatalf("SetMaxInterBitratePct: %v", err)
			}
		}},
		{"SetMaxIntraBitratePct", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetMaxIntraBitratePct(200); err != nil {
				t.Fatalf("SetMaxIntraBitratePct: %v", err)
			}
		}},
		{"SetGFCBRBoostPct", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetGFCBRBoostPct(50); err != nil {
				t.Fatalf("SetGFCBRBoostPct: %v", err)
			}
		}},
		{"SetFramePeriodicBoost", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetFramePeriodicBoost(true); err != nil {
				t.Fatalf("SetFramePeriodicBoost: %v", err)
			}
		}},
		{"SetAltRefAQ", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetAltRefAQ(true); err != nil {
				t.Fatalf("SetAltRefAQ: %v", err)
			}
		}},
		{"SetPostEncodeDrop", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetPostEncodeDrop(true); err != nil {
				t.Fatalf("SetPostEncodeDrop: %v", err)
			}
		}},
		{"SetDisableOvershootMaxQCBR", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetDisableOvershootMaxQCBR(true); err != nil {
				t.Fatalf("SetDisableOvershootMaxQCBR: %v", err)
			}
		}},
		{"SetNextFrameQIndex", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetNextFrameQIndex(128); err != nil {
				t.Fatalf("SetNextFrameQIndex: %v", err)
			}
		}},
		{"SetDeltaQUV", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetDeltaQUV(4); err != nil {
				t.Fatalf("SetDeltaQUV: %v", err)
			}
		}},
		{"SetDisableLoopfilter", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetDisableLoopfilter(VP9LoopfilterDisableInter); err != nil {
				t.Fatalf("SetDisableLoopfilter: %v", err)
			}
		}},
		{"SetFrameParallelDecoding", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetFrameParallelDecoding(false); err != nil {
				t.Fatalf("SetFrameParallelDecoding: %v", err)
			}
		}},
		{"SetRTCExternalRateControl", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetRTCExternalRateControl(true); err != nil {
				t.Fatalf("SetRTCExternalRateControl: %v", err)
			}
		}},
		{"SetColorSpace", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetColorSpace(VP9ColorSpace(4)); err != nil {
				t.Fatalf("SetColorSpace: %v", err)
			}
		}},
		{"SetColorRange", func(t *testing.T, e *VP9Encoder) {
			if err := e.SetColorRange(VP9ColorRangeFull); err != nil {
				t.Fatalf("SetColorRange: %v", err)
			}
		}},
	}

	for _, ac := range allocCases {
		ac := ac
		t.Run(ac.name, func(t *testing.T) {
			e := makeEncoder(t)
			ac.call(t, e)
			allocs := testing.AllocsPerRun(50, func() {
				ac.call(t, e)
			})
			if allocs != 0 {
				t.Errorf("steady-state allocations for %s = %v, want 0",
					ac.name, allocs)
			}
		})
	}
}

// vp9RuntimeControlCase describes one runtime-control scenario for the
// byte-parity gate above.
type vp9RuntimeControlCase struct {
	name      string
	applyAt   int      // frame index at which apply runs (govpx side)
	scriptTok string   // libvpx-side control-script token applied at the same frame
	extraArgs []string // optional extra libvpx CLI args
	apply     func(*testing.T, *VP9Encoder)
}

// runVP9RuntimeControlCase encodes `frames` frames with the govpx VP9 encoder
// while applying tc.apply at frame tc.applyAt, then runs the libvpx oracle
// with the matching --control-script= entry and compares both scoreboard rows
// and raw packet bytes. The byte-parity assertion mirrors the VP8 runtime-
// controls gate: every visible packet must match libvpx byte-for-byte. Test
// failure logs the row-level scoreboard so regressions are easy to triage.
func runVP9RuntimeControlCase(t *testing.T, opts VP9EncoderOptions,
	extraArgs []string, width, height, frames int, tc vp9RuntimeControlCase,
) {
	t.Helper()
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	flags := make([]EncodeFlags, frames)

	before := func(enc *VP9Encoder, frame int) {
		if frame == tc.applyAt && tc.apply != nil {
			tc.apply(t, enc)
		}
	}

	govpxRows, govpxPackets := captureGovpxVP9StreamParityPacketRowsWithHooks(t,
		opts, sources, flags, before)

	libvpxArgs := append([]string(nil), extraArgs...)
	libvpxArgs = append(libvpxArgs, tc.extraArgs...)
	script := vp9RuntimeControlScript(frames, map[int]string{tc.applyAt: tc.scriptTok})
	libvpxArgs = append(libvpxArgs, "--control-script="+strings.Join(script, ","))

	libvpxRows, libvpxPackets := captureLibvpxVP9StreamParityPacketRows(t,
		sources, flags, libvpxArgs)

	stats := compareVP9OracleTransitionRows(t, govpxRows, libvpxRows)
	matches, firstMismatch := vp9test.CountByteParityMatches(govpxPackets,
		libvpxPackets)
	t.Logf("VP9 runtime control %s: matches=%d/%d first_mismatch=%d stats=%s",
		tc.name, matches, len(govpxPackets), firstMismatch, stats)
	t.Logf("VP9 runtime control %s rows:\n%s",
		tc.name, formatVP9RateScoreboardRows(govpxRows, libvpxRows))
	if os.Getenv("GOVPX_VP9_RUNTIME_CONTROLS_STRICT") == "1" {
		assertVP9RuntimeControlByteParity(t, tc.name, govpxPackets, libvpxPackets)
	}
}

// vp9RuntimeControlScript builds the per-frame --control-script CSV that the
// libvpx vpxenc-vp9-frameflags driver consumes. Frames not listed in updates
// emit "-" so the driver leaves the live config alone.
func vp9RuntimeControlScript(frames int, updates map[int]string) []string {
	script := make([]string, frames)
	for i := range script {
		script[i] = "-"
	}
	for frame, update := range updates {
		if frame >= 0 && frame < frames && update != "" {
			script[frame] = update
		}
	}
	return script
}

// assertVP9RuntimeControlByteParity asserts every visible packet matches
// libvpx byte-for-byte and that drop classifications agree. It is reached
// only under GOVPX_VP9_RUNTIME_CONTROLS_STRICT=1 because the broader VP9
// runtime-control surface is still being pinned.
func assertVP9RuntimeControlByteParity(t *testing.T, label string,
	got, want [][]byte,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("VP9 runtime control %s packet count: got=%d want=%d",
			label, len(got), len(want))
	}
	for i := range got {
		gotEmpty := len(got[i]) == 0
		wantEmpty := len(want[i]) == 0
		if gotEmpty != wantEmpty {
			t.Errorf("VP9 runtime control %s frame %d drop mismatch: got_empty=%t want_empty=%t",
				label, i, gotEmpty, wantEmpty)
			continue
		}
		if gotEmpty {
			continue
		}
		if !bytes.Equal(got[i], want[i]) {
			diff := vp9test.FirstPacketDiff(got[i], want[i])
			t.Errorf("VP9 runtime control %s frame %d byte mismatch: got_len=%d want_len=%d first_diff=%d",
				label, i, len(got[i]), len(want[i]), diff)
		}
	}
}
