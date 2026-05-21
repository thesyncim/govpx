//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

func TestVP8OracleEncoderStreamByteParityRuntimeControls(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime-control byte-parity gate")
	}
	driver := coracletest.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 32, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	segmented64 := fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}
	temporalLayerOverrideIDs := []int{0, 0, 1, 1, 0, 1, 0, 1, 0, 0, 1, 1}
	temporalThreeLayerOverrideIDs := []int{0, 2, 1, 2, 0, 1, 2, 0, 2, 1, 0, 2}
	_, _, _, _, _ = panning32, panning64, segmented64, temporalLayerOverrideIDs, temporalThreeLayerOverrideIDs

	type runtimeCase struct {
		name        string
		fx          fixture
		opts        EncoderOptions
		flags       []EncodeFlags
		libvpxFlags []EncodeFlags
		script      []string
		apply       map[int]func(*testing.T, *VP8Encoder)
		extraArgs   []string
		// matchLimit caps how many leading frames the per-frame byte
		// compare asserts strictly; later frames are logged only. Used
		// for runtime-config transitions that exercise the libvpx
		// vp8_change_config Speed reset (oxcf.cpu_used) — the post-
		// reset auto-speed evolution can land on a slightly different
		// sample than libvpx because the carried-over
		// avg_pick_mode_time / avg_encode_time timers differ subtly
		// after the transition.
		matchLimit int
	}

	baseOpts := func(fx fixture) EncoderOptions {
		return EncoderOptions{
			Width:             fx.w,
			Height:            fx.h,
			FPS:               fps,
			RateControlMode:   RateControlCBR,
			TargetBitrateKbps: targetKbps,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			KeyFrameInterval:  999,
			Deadline:          DeadlineRealtime,
			CpuUsed:           0,
			Tuning:            TunePSNR,
		}
	}
	cases := []runtimeCase{
		{
			name: "bitrate-only-two-step",
			fx:   panning64,
			opts: baseOpts(panning64),
			// Direct bitrate changes use vpx_codec_enc_config_set under
			// libvpx. The next packet must carry the same forced LF-delta
			// update bit libvpx emits after vp8_change_config.
			script: runtimeControlScript(frames, map[int]string{
				3: "bitrate:300",
				7: "bitrate:1200",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetBitrateKbps", e.SetBitrateKbps(300))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetBitrateKbps", e.SetBitrateKbps(1200))
				},
			},
		},
		{
			name: "bitrate-bounds-runtime-in-range",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				3: "bitrate:600+minq:4+maxq:56+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000",
				7: "bitrate:900",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(bounded bitrate)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   600,
						MinBitrateKbps:      500,
						MaxBitrateKbps:      900,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						UndershootPct:       100,
						OvershootPct:        100,
						BufferSizeMs:        6000,
						BufferInitialSizeMs: 4000,
						BufferOptimalSizeMs: 5000,
					}))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetBitrateKbps(900)", e.SetBitrateKbps(900))
				},
			},
		},
		{
			name: "bitrate-runtime-bounds-boundary-values",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.MinBitrateKbps = 500
				opts.MaxBitrateKbps = 900
				return opts
			}(),
			script: runtimeControlScript(frames, map[int]string{
				3: "bitrate:500",
				7: "bitrate:900",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetBitrateKbps(min-bound)", e.SetBitrateKbps(500))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetBitrateKbps(max-bound)", e.SetBitrateKbps(900))
				},
			},
		},
		{
			name: "buffer-only-two-step",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				3: "bufsz:500+bufinit:100+bufopt:300",
				7: "bufsz:1000+bufinit:500+bufopt:600",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(buffer-low)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   targetKbps,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						BufferSizeMs:        500,
						BufferInitialSizeMs: 100,
						BufferOptimalSizeMs: 300,
					}))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(buffer-default)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   targetKbps,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						BufferSizeMs:        1000,
						BufferInitialSizeMs: 500,
						BufferOptimalSizeMs: 600,
					}))
				},
			},
		},
		{
			name: "q-band-only-two-step",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				3: "minq:10+maxq:50",
				7: "minq:4+maxq:56",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(q-band-tight)", e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 10, MaxQuantizer: 50}))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(q-band-default)", e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 4, MaxQuantizer: 56}))
				},
			},
		},
		{
			name: "fps-only-two-step",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				3: "fps:15",
				7: "fps:30",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(fps15)", e.SetRealtimeTarget(RealtimeTarget{FPS: 15}))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(fps30)", e.SetRealtimeTarget(RealtimeTarget{FPS: 30}))
				},
			},
		},
		{
			name: "undershoot-overshoot-only-two-step",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				3: "undershoot:10+overshoot:90",
				7: "undershoot:100+overshoot:100",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(under10-over90)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   targetKbps,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						UndershootPct:       10,
						OvershootPct:        90,
						BufferSizeMs:        6000,
						BufferInitialSizeMs: 4000,
						BufferOptimalSizeMs: 5000,
					}))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(under100-over100)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   targetKbps,
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
		},
		{
			name: "frame-drop-allowed-toggle-stored-watermark",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 300
				opts.BufferSizeMs = 500
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 300
				opts.DropFrameWaterMark = 60
				return opts
			}(),
			extraArgs: []string{"--target-bitrate=300", "--buf-sz=500", "--buf-initial-sz=100", "--buf-optimal-sz=300"},
			script: runtimeControlScript(frames, map[int]string{
				3: "drop:60",
				8: "drop:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetFrameDropAllowed(true)", e.SetFrameDropAllowed(true))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetFrameDropAllowed(false)", e.SetFrameDropAllowed(false))
				},
			},
		},
		{
			name: "frame-drop-allow-legacy-bool-stored-watermark",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 300
				opts.BufferSizeMs = 500
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 300
				opts.DropFrameWaterMark = 60
				return opts
			}(),
			extraArgs: []string{"--target-bitrate=300", "--buf-sz=500", "--buf-initial-sz=100", "--buf-optimal-sz=300"},
			script: runtimeControlScript(frames, map[int]string{
				3: "drop:60",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(FrameDropEnabled)", e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropEnabled}))
				},
			},
		},
		{
			name: "frame-drop-realtime-target-explicit-toggle",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 300
				opts.BufferSizeMs = 500
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 300
				opts.DropFrameWaterMark = 60
				return opts
			}(),
			extraArgs: []string{"--target-bitrate=300", "--buf-sz=500", "--buf-initial-sz=100", "--buf-optimal-sz=300"},
			script: runtimeControlScript(frames, map[int]string{
				3: "drop:60",
				8: "drop:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(FrameDropEnabled)", e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropEnabled}))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(FrameDropDisabled)", e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropDisabled}))
				},
			},
		},
		{
			name: "frame-drop-watermark-clamp-runtime",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 300
				opts.BufferSizeMs = 500
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 300
				return opts
			}(),
			extraArgs: []string{"--target-bitrate=300", "--buf-sz=500", "--buf-initial-sz=100", "--buf-optimal-sz=300"},
			script: runtimeControlScript(frames, map[int]string{
				3: "drop:100",
				8: "drop:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(drop clamp)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   300,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						BufferSizeMs:        500,
						BufferInitialSizeMs: 100,
						BufferOptimalSizeMs: 300,
						DropFrameAllowed:    true,
						DropFrameWaterMark:  150,
					}))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(drop off)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   300,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						BufferSizeMs:        500,
						BufferInitialSizeMs: 100,
						BufferOptimalSizeMs: 300,
					}))
				},
			},
		},
		{
			name: "bitrate-fps-q-buffer-drop-two-step",
			fx:   panning64,
			opts: baseOpts(panning64),
			// This bundles the vpx_codec_enc_config_set fields that WebRTC
			// senders change together on BWE updates. Each transition packet
			// must match, including the LF-delta update bit.
			script: runtimeControlScript(frames, map[int]string{
				3: "bitrate:300+fps:15+minq:10+maxq:50+drop:60+bufsz:500+bufinit:100+bufopt:300",
				7: "bitrate:1200+fps:30+minq:4+maxq:56+drop:0+bufsz:1000+bufinit:500+bufopt:600",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					if err := e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 300, FPS: 15, MinQuantizer: 10, MaxQuantizer: 50, FrameDrop: RealtimeFrameDropEnabled}); err != nil {
						t.Fatalf("frame3 SetRealtimeTarget: %v", err)
					}
					if err := e.SetRateControl(RateControlConfig{Mode: RateControlCBR, TargetBitrateKbps: 300, MinQuantizer: 10, MaxQuantizer: 50, BufferSizeMs: 500, BufferInitialSizeMs: 100, BufferOptimalSizeMs: 300, DropFrameAllowed: true, DropFrameWaterMark: 60}); err != nil {
						t.Fatalf("frame3 SetRateControl: %v", err)
					}
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					if err := e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 1200, FPS: 30, MinQuantizer: 4, MaxQuantizer: 56, FrameDrop: RealtimeFrameDropDisabled}); err != nil {
						t.Fatalf("frame7 SetRealtimeTarget: %v", err)
					}
					if err := e.SetRateControl(RateControlConfig{Mode: RateControlCBR, TargetBitrateKbps: 1200, MinQuantizer: 4, MaxQuantizer: 56, BufferSizeMs: 1000, BufferInitialSizeMs: 500, BufferOptimalSizeMs: 600}); err != nil {
						t.Fatalf("frame7 SetRateControl: %v", err)
					}
				},
			},
		},
		{
			name: "realtime-target-same-size-bwe-update",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				3: "resize:64x64+bitrate:500+fps:24+minq:8+maxq:48",
				7: "resize:64x64+bitrate:700+fps:30+minq:4+maxq:56",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(same-size-bwe-low)", e.SetRealtimeTarget(RealtimeTarget{Width: 64, Height: 64, BitrateKbps: 500, FPS: 24, MinQuantizer: 8, MaxQuantizer: 48}))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(same-size-bwe-default)", e.SetRealtimeTarget(RealtimeTarget{Width: 64, Height: 64, BitrateKbps: 700, FPS: 30, MinQuantizer: 4, MaxQuantizer: 56}))
				},
			},
		},
		{
			name: "rate-control-full-config-maxintra-gfboost",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: []EncodeFlags{
				0, 0, 0,
				EncodeForceKeyFrame,
				0, 0, 0,
				EncodeForceGoldenFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				3: "endusage:cbr+bitrate:500+minq:8+maxq:48+undershoot:20+overshoot:80+bufsz:800+bufinit:400+bufopt:600+maxintra:100+gfboost:50",
				7: "endusage:cbr+bitrate:700+minq:4+maxq:56+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000+maxintra:0+gfboost:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(full-low)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   500,
						MinQuantizer:        8,
						MaxQuantizer:        48,
						UndershootPct:       20,
						OvershootPct:        80,
						BufferSizeMs:        800,
						BufferInitialSizeMs: 400,
						BufferOptimalSizeMs: 600,
						MaxIntraBitratePct:  100,
						GFCBRBoostPct:       50,
					}))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(full-default)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   targetKbps,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						UndershootPct:       100,
						OvershootPct:        100,
						BufferSizeMs:        6000,
						BufferInitialSizeMs: 4000,
						BufferOptimalSizeMs: 5000,
						MaxIntraBitratePct:  0,
						GFCBRBoostPct:       0,
					}))
				},
			},
		},
		{
			name: "rate-control-mode-cbr-cq-q-transition",
			fx:   panning32,
			opts: baseOpts(panning32),
			// The static matrix covers CBR/CQ/Q as construction-time
			// choices; this row pins the runtime vpx_codec_enc_config_set
			// path between all three modes.
			script: runtimeControlScript(frames, map[int]string{
				3: "endusage:cq+cq:30+minq:4+maxq:56+bitrate:700+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000",
				7: "endusage:q+cq:20+minq:4+maxq:56+bitrate:700+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(CQ)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCQ,
						TargetBitrateKbps:   targetKbps,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						CQLevel:             30,
						UndershootPct:       100,
						OvershootPct:        100,
						BufferSizeMs:        6000,
						BufferInitialSizeMs: 4000,
						BufferOptimalSizeMs: 5000,
					}))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(Q)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlQ,
						TargetBitrateKbps:   targetKbps,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						CQLevel:             20,
						UndershootPct:       100,
						OvershootPct:        100,
						BufferSizeMs:        6000,
						BufferInitialSizeMs: 4000,
						BufferOptimalSizeMs: 5000,
					}))
				},
			},
		},
		{
			name: "rate-control-mode-cbr-vbr-cbr-transition",
			fx:   panning32,
			opts: baseOpts(panning32),
			script: runtimeControlScript(frames, map[int]string{
				3: "endusage:vbr+bitrate:700+minq:4+maxq:56+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000",
				7: "endusage:cbr+bitrate:700+minq:4+maxq:56+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(VBR)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlVBR,
						TargetBitrateKbps:   targetKbps,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						UndershootPct:       100,
						OvershootPct:        100,
						BufferSizeMs:        6000,
						BufferInitialSizeMs: 4000,
						BufferOptimalSizeMs: 5000,
					}))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl(CBR)", e.SetRateControl(RateControlConfig{
						Mode:                RateControlCBR,
						TargetBitrateKbps:   targetKbps,
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
		},
		{
			name: "deadline-best-quality-roundtrip",
			fx:   panning32,
			opts: baseOpts(panning32),
			// SetDeadline affects the per-call encode deadline rather than
			// codec cfg. The runtime path needs a direct best<->realtime
			// transition because the older row only covered good<->realtime.
			script: runtimeControlScript(frames, map[int]string{
				2: "deadline:best",
				7: "deadline:rt",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline(best)", e.SetDeadline(DeadlineBestQuality))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline(rt)", e.SetDeadline(DeadlineRealtime))
				},
			},
		},
		{
			name: "deadline-good-quality-roundtrip",
			fx:   panning32,
			opts: baseOpts(panning32),
			script: runtimeControlScript(frames, map[int]string{
				2: "deadline:good",
				7: "deadline:rt",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline(good)", e.SetDeadline(DeadlineGoodQuality))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline(rt)", e.SetDeadline(DeadlineRealtime))
				},
			},
		},
		{
			name: "deadline-best-quality-force-keyframe-roundtrip",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: []EncodeFlags{
				0, 0, 0, 0,
				EncodeForceKeyFrame,
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				2: "deadline:best",
				7: "deadline:rt",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline(best)", e.SetDeadline(DeadlineBestQuality))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline(rt)", e.SetDeadline(DeadlineRealtime))
				},
			},
		},
		{
			name: "keyframe-interval-only-two-step",
			fx:   panning32,
			opts: baseOpts(panning32),
			script: runtimeControlScript(frames, map[int]string{
				3: "kfmin:4+kfmax:4",
				7: "kfmin:999+kfmax:999",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetKeyFrameInterval(4)", e.SetKeyFrameInterval(4))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(true)", e.SetAdaptiveKeyFrames(true))
					mustRuntime(t, "SetKeyFrameInterval(999)", e.SetKeyFrameInterval(999))
				},
			},
		},
		{
			name: "keyframe-interval-shrink-past-age-forces-key",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				6: "kfmin:4+kfmax:4",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetKeyFrameInterval(4)", e.SetKeyFrameInterval(4))
				},
			},
		},
		{
			name: "keyframe-interval-grow-defers-key",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.KeyFrameInterval = 4
				return opts
			}(),
			extraArgs: []string{"--kf-min-dist=4", "--kf-max-dist=4"},
			script: runtimeControlScript(frames, map[int]string{
				3: "kfmin:999+kfmax:999",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetKeyFrameInterval(999)", e.SetKeyFrameInterval(999))
				},
			},
		},
		{
			name: "cpu-used-runtime-one-way-to-negative",
			fx:   panning32,
			opts: baseOpts(panning32),
			script: runtimeControlScript(frames, map[int]string{
				3: "cpu:-3",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCPUUsed(-3)", e.SetCPUUsed(-3))
				},
			},
		},
		{
			name: "cpu-used-runtime-roundtrip",
			fx:   panning32,
			opts: baseOpts(panning32),
			script: runtimeControlScript(frames, map[int]string{
				3: "cpu:-3",
				8: "cpu:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCPUUsed(-3)", e.SetCPUUsed(-3))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCPUUsed(0)", e.SetCPUUsed(0))
				},
			},
		},
		{
			name: "cpu-used-runtime-roundtrip-force-kf-at-return",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				8: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				3: "cpu:-3",
				8: "cpu:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCPUUsed(-3)", e.SetCPUUsed(-3))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCPUUsed(0)", e.SetCPUUsed(0))
				},
			},
		},
		{
			name: "cpu-used-runtime-minus8-roundtrip",
			fx:   panning32,
			opts: baseOpts(panning32),
			script: runtimeControlScript(frames, map[int]string{
				3: "cpu:-8",
				8: "cpu:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCPUUsed(-8)", e.SetCPUUsed(-8))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCPUUsed(0)", e.SetCPUUsed(0))
				},
			},
		},
		{
			name: "token-partitions-runtime-roundtrip",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "token:1",
				4: "token:2",
				6: "token:3",
				9: "token:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(1)", e.SetTokenPartitions(1))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(2)", e.SetTokenPartitions(2))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(3)", e.SetTokenPartitions(3))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(0)", e.SetTokenPartitions(0))
				},
			},
		},
		{
			name: "token-partitions-runtime-roundtrip-threads2",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.Threads = 2
				return opts
			}(),
			extraArgs: []string{"--threads=2"},
			script: runtimeControlScript(frames, map[int]string{
				2: "token:1",
				4: "token:2",
				6: "token:3",
				9: "token:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(1)", e.SetTokenPartitions(1))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(2)", e.SetTokenPartitions(2))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(3)", e.SetTokenPartitions(3))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(0)", e.SetTokenPartitions(0))
				},
			},
		},
		{
			name: "token-partitions-er3-runtime-roundtrip",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.ErrorResilient = true
				opts.ErrorResilientPartitions = true
				return opts
			}(),
			extraArgs: []string{"--error-resilient=3"},
			script: runtimeControlScript(frames, map[int]string{
				2: "token:1",
				4: "token:2",
				6: "token:3",
				9: "token:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(1)", e.SetTokenPartitions(1))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(2)", e.SetTokenPartitions(2))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(3)", e.SetTokenPartitions(3))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(0)", e.SetTokenPartitions(0))
				},
			},
		},
		{
			name: "token-partitions-force-keyframe-cycle",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: []EncodeFlags{
				0, 0,
				EncodeForceKeyFrame,
				0, 0, 0,
				EncodeForceKeyFrame,
				0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				2: "token:1",
				6: "token:3",
				9: "token:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(1)", e.SetTokenPartitions(1))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(3)", e.SetTokenPartitions(3))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTokenPartitions(0)", e.SetTokenPartitions(0))
				},
			},
		},
		{
			name: "max-intra-runtime-force-keyframe",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: []EncodeFlags{
				0, 0, 0,
				EncodeForceKeyFrame,
				0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				3: "maxintra:100",
				7: "maxintra:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetMaxIntraBitratePct(100)", e.SetMaxIntraBitratePct(100))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetMaxIntraBitratePct(0)", e.SetMaxIntraBitratePct(0))
				},
			},
		},
		{
			name: "gf-cbr-boost-runtime-force-golden",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: []EncodeFlags{
				0, 0, 0,
				EncodeForceGoldenFrame,
				0, 0, 0,
				EncodeForceGoldenFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				3: "gfboost:50",
				7: "gfboost:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetGFCBRBoostPct(50)", e.SetGFCBRBoostPct(50))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetGFCBRBoostPct(0)", e.SetGFCBRBoostPct(0))
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, sources, tc.flags, tc.apply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
			libvpxFlags := tc.flags
			if tc.libvpxFlags != nil {
				libvpxFlags = tc.libvpxFlags
			}
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources, libvpxFlags, extraArgs)
			assertSegmentByteParity(t, "runtime-controls", govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}
