//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"strings"
	"testing"
)

func TestOracleEncoderStreamByteParityRuntimeControls(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime-control byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

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
					mustRuntime(t, "SetRealtimeTarget(AllowFrameDrop)", e.SetRealtimeTarget(RealtimeTarget{AllowFrameDrop: true}))
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
		{
			name:   "temporal-scalability-enable-disable-transition",
			fx:     panning64,
			opts:   baseOpts(panning64),
			flags:  temporalScalabilityEnableDisableFlags(frames),
			script: temporalScalabilityEnableDisableScript(frames),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(two-layer)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled: true,
						Mode:    TemporalLayeringTwoLayers,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{
							420, targetKbps,
						},
					}))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
		{
			name: "temporal-scalability-two-to-three-layer-transition",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled: true,
					Mode:    TemporalLayeringTwoLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{
						420, targetKbps,
					},
				}
				return opts
			}(),
			flags:  temporalScalabilityTwoToThreeFlags(frames),
			script: temporalScalabilityTwoToThreeScript(frames),
			extraArgs: []string{
				"--temporal-layers=2",
				"--temporal-bitrates=420,700",
				"--temporal-decimators=2,1",
				"--temporal-periodicity=2",
				"--temporal-layer-ids=0,1",
			},
			apply: map[int]func(*testing.T, *VP8Encoder){
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(three-layer)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled: true,
						Mode:    TemporalLayeringThreeLayers,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{
							280, 420, targetKbps,
						},
					}))
				},
			},
		},
		{
			name: "temporal-scalability-three-to-two-layer-transition",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled:                true,
					Mode:                   TemporalLayeringThreeLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{280, 420, targetKbps},
				}
				return opts
			}(),
			flags:  temporalScalabilityThreeToTwoFlags(frames),
			script: temporalScalabilityThreeToTwoScript(frames),
			extraArgs: []string{
				"--temporal-layers=3",
				"--temporal-bitrates=280,420,700",
				"--temporal-decimators=4,2,1",
				"--temporal-periodicity=4",
				"--temporal-layer-ids=0,2,1,2",
			},
			apply: map[int]func(*testing.T, *VP8Encoder){
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(two-layer)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled:                true,
						Mode:                   TemporalLayeringTwoLayers,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{420, targetKbps},
					}))
				},
			},
		},
		{
			name: "temporal-scalability-same-layer-bitrate-redistribution",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled:                true,
					Mode:                   TemporalLayeringTwoLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{350, targetKbps},
				}
				return opts
			}(),
			flags:     temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 6),
			script:    temporalScalabilityReconfigureScript(frames, TemporalLayeringTwoLayers, 6, "tslayers:2+tsperiodicity:2+tsbitrates:420/700+tsdecimators:2/1+tsids:0/1"),
			extraArgs: []string{"--temporal-layers=2", "--temporal-bitrates=350,700", "--temporal-decimators=2,1", "--temporal-periodicity=2", "--temporal-layer-ids=0,1"},
			apply: map[int]func(*testing.T, *VP8Encoder){
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(two-layer-redistribution)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled:                true,
						Mode:                   TemporalLayeringTwoLayers,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{420, targetKbps},
					}))
				},
			},
		},
		{
			name:   "temporal-scalability-five-layer-enable-disable",
			fx:     panning64,
			opts:   baseOpts(panning64),
			flags:  temporalScalabilityWindowFlags(frames, TemporalLayeringFiveLayers, 2, 8),
			script: temporalScalabilityWindowScript(frames, TemporalLayeringFiveLayers, 2, 8, "tslayers:5+tsperiodicity:16+tsbitrates:100/220/360/520/700+tsdecimators:16/8/4/2/1+tsids:0/4/3/4/2/4/3/4/1/4/3/4/2/4/3/4"),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(five-layer)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled:                true,
						Mode:                   TemporalLayeringFiveLayers,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{100, 220, 360, 520, targetKbps},
					}))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
		{
			name:   "temporal-scalability-mode12-enable-disable",
			fx:     panning64,
			opts:   baseOpts(panning64),
			flags:  temporalScalabilityWindowFlags(frames, TemporalLayeringThreeLayersNoSync, 2, 8),
			script: temporalScalabilityWindowScript(frames, TemporalLayeringThreeLayersNoSync, 2, 8, "tslayers:3+tsperiodicity:4+tsbitrates:280/420/700+tsdecimators:4/2/1+tsids:0/2/1/2"),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(mode12)", e.SetTemporalScalability(TemporalScalabilityConfig{
						Enabled:                true,
						Mode:                   TemporalLayeringThreeLayersNoSync,
						LayerTargetBitrateKbps: [MaxTemporalLayers]int{280, 420, targetKbps},
					}))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
		{
			name: "codec-control-surface-toggle",
			fx:   panning64,
			opts: baseOpts(panning64),
			// Runtime codec controls route through libvpx update_extracfg,
			// which forces an LF-delta update on the next packet.
			script: runtimeControlScript(frames, map[int]string{
				2: "sharpness:4+static:1+screen:1+gfboost:50+maxintra:100+token:2",
				6: "sharpness:0+static:0+screen:0+gfboost:0+maxintra:0+token:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetSharpness", e.SetSharpness(4))
					mustRuntime(t, "SetStaticThreshold", e.SetStaticThreshold(1))
					mustRuntime(t, "SetScreenContentMode", e.SetScreenContentMode(1))
					mustRuntime(t, "SetGFCBRBoostPct", e.SetGFCBRBoostPct(50))
					mustRuntime(t, "SetMaxIntraBitratePct", e.SetMaxIntraBitratePct(100))
					mustRuntime(t, "SetTokenPartitions", e.SetTokenPartitions(2))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetSharpness", e.SetSharpness(0))
					mustRuntime(t, "SetStaticThreshold", e.SetStaticThreshold(0))
					mustRuntime(t, "SetScreenContentMode", e.SetScreenContentMode(0))
					mustRuntime(t, "SetGFCBRBoostPct", e.SetGFCBRBoostPct(0))
					mustRuntime(t, "SetMaxIntraBitratePct", e.SetMaxIntraBitratePct(0))
					mustRuntime(t, "SetTokenPartitions", e.SetTokenPartitions(0))
				},
			},
		},
		{
			name: "sharpness-only-two-step",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "sharpness:4",
				6: "sharpness:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetSharpness(4)", e.SetSharpness(4))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetSharpness(0)", e.SetSharpness(0))
				},
			},
		},
		{
			name: "static-threshold-only-two-step",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				2: "static:500",
				6: "static:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(500)", e.SetStaticThreshold(500))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(0)", e.SetStaticThreshold(0))
				},
			},
		},
		{
			name: "static-threshold-500-noise3-runtime-roundtrip",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			script: runtimeControlScript(frames, map[int]string{
				2: "static:500",
				6: "static:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(500)", e.SetStaticThreshold(500))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetStaticThreshold(0)", e.SetStaticThreshold(0))
				},
			},
		},
		{
			name: "screen-content-1-2-roundtrip",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				2: "screen:1",
				5: "screen:2",
				8: "screen:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetScreenContentMode(1)", e.SetScreenContentMode(1))
				},
				5: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetScreenContentMode(2)", e.SetScreenContentMode(2))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetScreenContentMode(0)", e.SetScreenContentMode(0))
				},
			},
		},
		{
			name: "screen-content-2-noise3-runtime-roundtrip",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			script: runtimeControlScript(frames, map[int]string{
				2: "screen:2",
				8: "screen:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetScreenContentMode(2)", e.SetScreenContentMode(2))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetScreenContentMode(0)", e.SetScreenContentMode(0))
				},
			},
		},
		{
			name: "tuning-ssim-roundtrip",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "tune:ssim",
				7: "tune:psnr",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTuning(TuneSSIM)", e.SetTuning(TuneSSIM))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTuning(TunePSNR)", e.SetTuning(TunePSNR))
				},
			},
		},
		{
			name: "noise-sensitivity-1-enable-only",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "noise:1",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(1)", e.SetNoiseSensitivity(1))
				},
			},
		},
		{
			name: "noise-sensitivity-1-3-6-roundtrip",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "noise:1",
				4: "noise:3",
				6: "noise:6",
				9: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(1)", e.SetNoiseSensitivity(1))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(6)", e.SetNoiseSensitivity(6))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-6-3-sticky-yuv",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:6",
				4: "noise:3",
				8: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(6)", e.SetNoiseSensitivity(6))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-3-6-sticky-aggressive",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:3",
				4: "noise:6",
				8: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(6)", e.SetNoiseSensitivity(6))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-3-disable-after-inter",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:3",
				7: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-3-disable-after-force-keyframe",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				7: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:3",
				7: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-3-disable-threads2-token4",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.Threads = 2
				opts.TokenPartitions = 2
				return opts
			}(),
			extraArgs: []string{"--threads=2"},
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:3",
				7: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(3)", e.SetNoiseSensitivity(3))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-2-4-5-roundtrip",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "noise:2",
				4: "noise:4",
				6: "noise:5",
				9: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(2)", e.SetNoiseSensitivity(2))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(4)", e.SetNoiseSensitivity(4))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(5)", e.SetNoiseSensitivity(5))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "noise-sensitivity-4-disable-after-inter",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "noise:4",
				7: "noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(4)", e.SetNoiseSensitivity(4))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "cq-level-transition",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.RateControlMode = RateControlCQ
				opts.CQLevel = 20
				return opts
			}(),
			script: runtimeControlScript(frames, map[int]string{
				4: "cq:35+minq:4+maxq:56",
				8: "cq:20",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget", e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 4, MaxQuantizer: 56}))
					mustRuntime(t, "SetCQLevel", e.SetCQLevel(35))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCQLevel", e.SetCQLevel(20))
				},
			},
		},
		{
			name: "q-mode-cq-level-transition",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.RateControlMode = RateControlQ
				opts.CQLevel = 20
				return opts
			}(),
			script: runtimeControlScript(frames, map[int]string{
				4: "cq:35+minq:4+maxq:56",
				8: "cq:20",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget", e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 4, MaxQuantizer: 56}))
					mustRuntime(t, "SetCQLevel", e.SetCQLevel(35))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCQLevel", e.SetCQLevel(20))
				},
			},
		},
		{
			name: "deadline-rc-mode-key-interval-transition",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				3: "deadline:good+endusage:vbr+kfmin:4+kfmax:4+undershoot:50+overshoot:50+bufsz:6000+bufinit:4000+bufopt:5000",
				7: "deadline:rt+endusage:cbr+kfmin:999+kfmax:999+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline", e.SetDeadline(DeadlineGoodQuality))
					mustRuntime(t, "SetRateControl", e.SetRateControl(RateControlConfig{
						Mode:                RateControlVBR,
						TargetBitrateKbps:   targetKbps,
						MinQuantizer:        4,
						MaxQuantizer:        56,
						UndershootPct:       50,
						OvershootPct:        50,
						BufferSizeMs:        6000,
						BufferInitialSizeMs: 4000,
						BufferOptimalSizeMs: 5000,
					}))
					mustRuntime(t, "SetKeyFrameInterval", e.SetKeyFrameInterval(4))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetDeadline", e.SetDeadline(DeadlineRealtime))
					mustRuntime(t, "SetRateControl", e.SetRateControl(RateControlConfig{
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
					mustRuntime(t, "SetKeyFrameInterval", e.SetKeyFrameInterval(999))
				},
			},
		},
		{
			name: "speed-tuning-denoiser-transition-with-force-kf",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: []EncodeFlags{
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				3: "cpu:-3+tune:ssim+noise:3",
				8: "cpu:0+tune:psnr+noise:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCPUUsed", e.SetCPUUsed(-3))
					mustRuntime(t, "SetTuning", e.SetTuning(TuneSSIM))
					mustRuntime(t, "SetNoiseSensitivity", e.SetNoiseSensitivity(3))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetCPUUsed", e.SetCPUUsed(0))
					mustRuntime(t, "SetTuning", e.SetTuning(TunePSNR))
					mustRuntime(t, "SetNoiseSensitivity", e.SetNoiseSensitivity(0))
				},
			},
		},
		{
			name: "arnr-runtime-no-arf",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.LookaheadFrames = 4
				opts.AutoAltRef = false
				return opts
			}(),
			extraArgs: []string{"--lag-in-frames=4", "--auto-alt-ref=0"},
			script: runtimeControlScript(frames, map[int]string{
				2: "arnrmax:7+arnrstrength:6+arnrtype:3",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(7, 6, 3))
				},
			},
		},
		{
			name: "arnr-runtime-transition-auto-alt-ref",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.RateControlMode = RateControlVBR
				opts.Deadline = DeadlineGoodQuality
				opts.CpuUsed = 4
				opts.LookaheadFrames = 8
				opts.AutoAltRef = true
				return opts
			}(),
			extraArgs: []string{"--end-usage=vbr", "--lag-in-frames=8", "--auto-alt-ref=1"},
			script: runtimeControlScript(frames, map[int]string{
				2: "arnrmax:7+arnrstrength:6+arnrtype:3",
				7: "arnrmax:3+arnrstrength:1+arnrtype:1",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(7, 6, 3))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(3, 1, 1))
				},
			},
		},
		{
			name: "arnr-runtime-auto-alt-ref-maxframes-only",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.RateControlMode = RateControlVBR
				opts.Deadline = DeadlineGoodQuality
				opts.CpuUsed = 4
				opts.LookaheadFrames = 8
				opts.AutoAltRef = true
				opts.ARNRMaxFrames = 7
				opts.ARNRStrength = 6
				opts.ARNRType = 3
				return opts
			}(),
			extraArgs: []string{"--end-usage=vbr", "--lag-in-frames=8", "--auto-alt-ref=1", "--arnr-maxframes=7", "--arnr-strength=6", "--arnr-type=3"},
			script: runtimeControlScript(frames, map[int]string{
				7: "arnrmax:3+arnrstrength:6+arnrtype:3",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(3, 6, 3))
				},
			},
		},
		{
			name: "arnr-runtime-auto-alt-ref-strength-only",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.RateControlMode = RateControlVBR
				opts.Deadline = DeadlineGoodQuality
				opts.CpuUsed = 4
				opts.LookaheadFrames = 8
				opts.AutoAltRef = true
				opts.ARNRMaxFrames = 7
				opts.ARNRStrength = 6
				opts.ARNRType = 3
				return opts
			}(),
			extraArgs: []string{"--end-usage=vbr", "--lag-in-frames=8", "--auto-alt-ref=1", "--arnr-maxframes=7", "--arnr-strength=6", "--arnr-type=3"},
			script: runtimeControlScript(frames, map[int]string{
				7: "arnrmax:7+arnrstrength:1+arnrtype:3",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(7, 1, 3))
				},
			},
		},
		{
			name: "arnr-runtime-auto-alt-ref-type-only",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.RateControlMode = RateControlVBR
				opts.Deadline = DeadlineGoodQuality
				opts.CpuUsed = 4
				opts.LookaheadFrames = 8
				opts.AutoAltRef = true
				opts.ARNRMaxFrames = 7
				opts.ARNRStrength = 6
				opts.ARNRType = 3
				return opts
			}(),
			extraArgs: []string{"--end-usage=vbr", "--lag-in-frames=8", "--auto-alt-ref=1", "--arnr-maxframes=7", "--arnr-strength=6", "--arnr-type=3"},
			script: runtimeControlScript(frames, map[int]string{
				7: "arnrmax:7+arnrstrength:6+arnrtype:1",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetARNR", e.SetARNR(7, 6, 1))
				},
			},
		},
		{
			name: "keyframe-disabled-runtime-toggle",
			fx:   panning32,
			opts: baseOpts(panning32),
			script: runtimeControlScript(frames, map[int]string{
				3: "kfdisabled:1+kfmin:0+kfmax:120",
				8: "kfdisabled:0+kfmin:0+kfmax:4",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(false)", e.SetAdaptiveKeyFrames(false))
					mustRuntime(t, "SetKeyFrameInterval(0)", e.SetKeyFrameInterval(0))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(true)", e.SetAdaptiveKeyFrames(true))
					mustRuntime(t, "SetKeyFrameInterval(4)", e.SetKeyFrameInterval(4))
				},
			},
		},
		{
			name: "adaptive-keyframes-scene-disable-reenable",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.AdaptiveKeyFrames = true
				return opts
			}(),
			extraArgs: []string{"--kf-min-dist=0", "--kf-max-dist=999"},
			script: runtimeControlScript(frames, map[int]string{
				3: "kfdisabled:1+kfmin:0+kfmax:999",
				8: "kfdisabled:0+kfmin:0+kfmax:999",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(false)", e.SetAdaptiveKeyFrames(false))
					mustRuntime(t, "SetKeyFrameInterval(0)", e.SetKeyFrameInterval(0))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(true)", e.SetAdaptiveKeyFrames(true))
					mustRuntime(t, "SetKeyFrameInterval(999)", e.SetKeyFrameInterval(999))
				},
			},
		},
		{
			name: "force-keyframe-while-keyframes-disabled",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: []EncodeFlags{
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				2: "kfdisabled:1+kfmin:0+kfmax:120",
				7: "kfdisabled:0+kfmin:999+kfmax:999",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(false)", e.SetAdaptiveKeyFrames(false))
					mustRuntime(t, "SetKeyFrameInterval(0)", e.SetKeyFrameInterval(0))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(true)", e.SetAdaptiveKeyFrames(true))
					mustRuntime(t, "SetKeyFrameInterval(999)", e.SetKeyFrameInterval(999))
				},
			},
		},
		{
			name: "force-keyframe-method-while-keyframes-disabled",
			fx:   panning32,
			opts: baseOpts(panning32),
			libvpxFlags: []EncodeFlags{
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				2: "kfdisabled:1+kfmin:0+kfmax:120",
				7: "kfdisabled:0+kfmin:999+kfmax:999",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetAdaptiveKeyFrames(false)", e.SetAdaptiveKeyFrames(false))
					mustRuntime(t, "SetKeyFrameInterval(0)", e.SetKeyFrameInterval(0))
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					e.ForceKeyFrame()
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetKeyFrameInterval(999)", e.SetKeyFrameInterval(999))
				},
			},
		},
		{
			name: "active-map-checker-toggle",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					rows := encoderMacroblockRows(e.opts.Height)
					cols := encoderMacroblockCols(e.opts.Width)
					mustRuntime(t, "SetActiveMap(checker)", e.SetActiveMap(activeMapPattern("checker", rows, cols), rows, cols))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-left-off-toggle-cpu-3",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.CpuUsed = -3
				return opts
			}(),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:left-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("left-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-right-off-toggle-cpu-3",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.CpuUsed = -3
				return opts
			}(),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:right-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("right-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-border-off-toggle-cpu-3",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.CpuUsed = -3
				return opts
			}(),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:border-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("border-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-checker-force-keyframe-toggle",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: []EncodeFlags{
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				7: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name:  "active-map-no-upd-last-no-ref-gf-arf",
			fx:    panning64,
			opts:  baseOpts(panning64),
			flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				8: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-checker-toggle-noise3-threads2",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.NoiseSensitivity = 3
				opts.Threads = 2
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3", "--threads=2"},
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-checker-noise3-force-keyframe-clear",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-left-off-noise3-force-keyframe-clear",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:left-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("left-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-right-off-noise3-force-keyframe-clear",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:right-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("right-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-border-off-noise3-force-keyframe-clear",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:border-off",
				6: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("border-off"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-map-pattern-switches",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:left-off",
				4: "active:right-off",
				7: "active:border-off",
				9: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("left-off"),
				4: activeMapApply("right-off"),
				7: activeMapApply("border-off"),
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "rtc-external-rate-control-runtime-toggle",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				2: "rtc:1",
				6: "rtc:0",
				9: "rtc:1",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
			},
		},
		{
			name: "rtc-external-disable-sticky-force-keyframe",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				2: "rtc:1",
				5: "rtc:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				5: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
			},
		},
		{
			name:  "rtc-external-no-ref-all-no-upd-entropy",
			fx:    panning64,
			opts:  baseOpts(panning64),
			flags: repeatFlag(frames-1, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef|EncodeNoUpdateEntropy),
			script: runtimeControlScript(frames, map[int]string{
				1: "rtc:1",
				8: "rtc:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
			},
		},
		{
			name: "active-map-roi-runtime-cross",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker+roi:border1",
				8: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name:  "roi-border-no-upd-entropy-no-upd-all",
			fx:    segmented64,
			opts:  baseOpts(segmented64),
			flags: repeatFlag(frames-1, EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				8: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "active-map-before-roi-runtime-order",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				2: "roi:border1",
				8: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				2: roiMapApply("border1"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-before-active-map-runtime-order",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				2: "active:checker",
				8: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				2: activeMapApply("checker"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "active-roi-disable-roi-first-order",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1:  "active:checker+roi:border1",
				8:  "roi:off",
				10: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
				10: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-roi-disable-active-first-order",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1:  "active:checker+roi:border1",
				8:  "active:off",
				10: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
				10: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-pattern-switch-under-active-map",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1:  "active:checker+roi:checker",
				4:  "roi:left1",
				7:  "roi:border1",
				10: "roi:off+active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("checker")(t, e)
				},
				4: roiMapApply("left1"),
				7: roiMapApply("border1"),
				10: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "rtc-external-active-map-runtime-cross",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker",
				4: "rtc:1",
				8: "active:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: activeMapApply("checker"),
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
				},
			},
		},
		{
			name: "active-roi-rtc-disable-order-cross",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				1:  "active:checker+roi:border1",
				4:  "rtc:1",
				7:  "rtc:0",
				10: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
				10: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "rtc-external-roi-runtime-cross",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				4: "rtc:1",
				8: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "rtc-external-roi-disable-on-force-keyframe",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			flags: []EncodeFlags{
				0, 0, 0, 0,
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				4: "rtc:1",
				8: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "rtc-external-active-roi-runtime-cross",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.TargetBitrateKbps = 400
				opts.BufferSizeMs = 200
				opts.BufferInitialSizeMs = 100
				opts.BufferOptimalSizeMs = 150
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 50
				return opts
			}(),
			extraArgs: []string{
				"--target-bitrate=400",
				"--buf-sz=200",
				"--buf-initial-sz=100",
				"--buf-optimal-sz=150",
				"--drop-frame=50",
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker+roi:border1",
				4: "rtc:1",
				8: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "set-reference-last-before-inter",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				return opts
			}(),
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:last:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					ref := encoderValidationPanningFrame(e.opts.Width, e.opts.Height, 8)
					mustRuntime(t, "SetReferenceFrame(last)", e.SetReferenceFrame(ReferenceLast, ref))
				},
			},
		},
		{
			name: "set-reference-last-before-inter-noise3",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--target-bitrate=1200", "--noise-sensitivity=3"},
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:last:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceLast, 8, "last"),
			},
		},
		{
			name: "set-reference-golden-before-inter-noise3",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--target-bitrate=1200", "--noise-sensitivity=3"},
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:golden:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceGolden, 8, "golden"),
			},
		},
		{
			name: "set-reference-altref-before-inter-noise3",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--target-bitrate=1200", "--noise-sensitivity=3"},
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceLast | EncodeNoReferenceGolden,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:altref:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceAltRef, 8, "altref"),
			},
		},
		{
			name: "roi-noise3-threads2-runtime-cross",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				opts.Threads = 2
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3", "--threads=2"},
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				7: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-checker-noise3-force-keyframe-clear",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:checker",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("checker"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-left1-noise3-force-keyframe-clear",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:left1",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("left1"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-border1-noise3-force-keyframe-clear",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:border1",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("border1"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-quadrants-noise3-force-keyframe-clear",
			fx:   segmented64,
			opts: func() EncoderOptions {
				opts := baseOpts(segmented64)
				opts.NoiseSensitivity = 3
				return opts
			}(),
			extraArgs: []string{"--noise-sensitivity=3"},
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				6: EncodeForceKeyFrame,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "roi:quadrants",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: roiMapApply("quadrants"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "set-reference-golden-before-inter",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				return opts
			}(),
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:golden:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceGolden, 8, "golden"),
			},
		},
		{
			name: "set-reference-altref-before-inter",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				return opts
			}(),
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceLast | EncodeNoReferenceGolden,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:altref:panning:8",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceAltRef, 8, "altref"),
			},
		},
		{
			name: "set-reference-repeated-last-and-golden",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.TargetBitrateKbps = 1200
				return opts
			}(),
			flags: []EncodeFlags{
				0,
				EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
				0,
				0,
				EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			},
			script: runtimeControlScript(frames, map[int]string{
				1: "setref:last:panning:8",
				4: "setref:golden:panning:12",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceLast, 8, "last"),
				4: setReferencePanningApply(ReferenceGolden, 12, "golden"),
			},
		},
		{
			name: "temporal-layer-id-manual-transition",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 1200
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled:                true,
					Mode:                   TemporalLayeringTwoLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{720, 1200},
				}
				return opts
			}(),
			flags:  temporalTwoLayerFlags(frames),
			script: temporalLayerIDScript(frames, temporalLayerOverrideIDs),
			apply:  temporalLayerIDApply(temporalLayerOverrideIDs),
			extraArgs: []string{
				"--temporal-layers=2",
				"--temporal-bitrates=720,1200",
				"--temporal-decimators=2,1",
				"--temporal-periodicity=2",
				"--temporal-layer-ids=0,1",
			},
		},
		{
			name: "temporal-layer-id-disabled-noop",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				2: "tlid:0",
				7: "tlid:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalLayerID(0)", e.SetTemporalLayerID(0))
				},
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalLayerID(0)", e.SetTemporalLayerID(0))
				},
			},
		},
		{
			name: "temporal-layer-id-manual-three-layer-transition",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TargetBitrateKbps = 1200
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled:                true,
					Mode:                   TemporalLayeringThreeLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{480, 720, 1200},
				}
				return opts
			}(),
			flags:  temporalThreeLayerFlags(frames),
			script: temporalLayerIDScript(frames, temporalThreeLayerOverrideIDs),
			apply:  temporalLayerIDApply(temporalThreeLayerOverrideIDs),
			extraArgs: []string{
				"--temporal-layers=3",
				"--temporal-bitrates=480,720,1200",
				"--temporal-decimators=4,2,1",
				"--temporal-periodicity=4",
				"--temporal-layer-ids=0,2,1,2",
			},
		},
		{
			name: "roi-map-quadrants-toggle",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				0: "roi:quadrants",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(quadrants)", e.SetROIMap(quadrantROIMap(e.opts.Width, e.opts.Height)))
				},
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-map-border-force-keyframe-toggle",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			flags: []EncodeFlags{
				0, 0, 0, 0,
				EncodeForceKeyFrame,
			},
			script: runtimeControlScript(frames, map[int]string{
				0: "roi:border1",
				6: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: roiMapApply("border1"),
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-map-pattern-switches",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				0: "roi:checker",
				3: "roi:left1",
				6: "roi:border1",
				9: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: roiMapApply("checker"),
				3: roiMapApply("left1"),
				6: roiMapApply("border1"),
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-map-custom-checker-set-clear",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				0: "roicustom:checker:0/-10/0/0:0/0/0/0:0/0/0/0",
				9: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(simple-checker)", e.SetROIMap(simpleCheckerROIMap(e.opts.Width, e.opts.Height)))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "roi-map-custom-data-switches",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				0: "roicustom:checker:0/-10/0/0:0/0/0/0:0/0/0/0",
				5: "roicustom:quadrants:0/-10/8/-20:0/-3/2/5:0/500/0/1200",
				9: "roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(simple-checker)", e.SetROIMap(simpleCheckerROIMap(e.opts.Width, e.opts.Height)))
				},
				5: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(custom-quadrants)", e.SetROIMap(customQuadrantROIMap(e.opts.Width, e.opts.Height)))
				},
				9: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
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
