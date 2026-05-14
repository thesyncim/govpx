//go:build govpx_oracle_trace

package govpx

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestOracleEncoderStreamByteParityRuntimeControls pins mid-stream control
// transitions against the companion libvpx frame-flags driver. The static
// oracle matrices cover the same knobs at encoder construction time; this
// test exercises the runtime setter path before selected input frames.
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
		matchLimit  int
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
			name: "frame-drop-allowed-toggle-default-watermark",
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
			name: "frame-drop-allow-legacy-bool",
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
			// The switch to cpu-used=-3 is byte-identical; returning to
			// cpu-used=0 still exposes a post-speed-reset drift.
			matchLimit: 8,
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
			// The enable transition keyframe matches. Subsequent layer
			// packets still expose temporal layer-context drift, and the
			// disable transition logs the recovery surface.
			matchLimit: 3,
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
			name:       "temporal-scalability-five-layer-enable-disable",
			fx:         panning64,
			opts:       baseOpts(panning64),
			flags:      temporalScalabilityWindowFlags(frames, TemporalLayeringFiveLayers, 2, 8),
			script:     temporalScalabilityWindowScript(frames, TemporalLayeringFiveLayers, 2, 8, "tslayers:5+tsperiodicity:16+tsbitrates:100/220/360/520/700+tsdecimators:16/8/4/2/1+tsids:0/4/3/4/2/4/3/4/1/4/3/4/2/4/3/4"),
			matchLimit: 3,
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
			name:       "temporal-scalability-mode12-enable-disable",
			fx:         panning64,
			opts:       baseOpts(panning64),
			flags:      temporalScalabilityWindowFlags(frames, TemporalLayeringThreeLayersNoSync, 2, 8),
			script:     temporalScalabilityWindowScript(frames, TemporalLayeringThreeLayersNoSync, 2, 8, "tslayers:3+tsperiodicity:4+tsbitrates:280/420/700+tsdecimators:4/2/1+tsids:0/2/1/2"),
			matchLimit: 3,
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
			// Static-threshold toggles with an already-active denoiser match
			// through the first static inter packets, then drift in the
			// denoiser/static segmentation interaction.
			matchLimit: 4,
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
			// Screen-content mode 2 follows the same active-denoiser
			// transition gap as static-threshold: keep the prefix strict
			// while logging the remaining drift.
			matchLimit: 4,
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
			// Level 1 matches; escalating to chroma denoise levels exposes
			// an existing denoiser-state drift, so keep the clean prefix
			// strict while logging levels 3/6 and teardown.
			matchLimit: 4,
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
			name: "noise-sensitivity-3-disable-after-inter",
			fx:   panning64,
			opts: baseOpts(panning64),
			// Direct mode-3 enable still diverges after the first denoised
			// inter packet, but this pins the common on/off teardown path
			// separately from the 1->3->6 escalation row above.
			matchLimit: 2,
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
			// Static ARNR-without-ARF rows are strict, but the runtime
			// setter still changes the first affected packet differently.
			// Later packets rejoin byte parity, so keep this transition
			// visible while pinning the matching prefix.
			matchLimit: 2,
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
			extraArgs:  []string{"--end-usage=vbr", "--lag-in-frames=8", "--auto-alt-ref=1"},
			matchLimit: 2,
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
			// The keyframe matches; denoiser/threaded active-map inter
			// packets still drift and are kept logged as a cross-control
			// gap.
			matchLimit: 1,
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
			// Strict through the RTC round-trip; disabling active+ROI after
			// the RTC transition still leaves a teardown drift.
			matchLimit: 10,
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
			// RTC+ROI is strict while ROI is enabled. Disabling ROI under
			// sticky RTC exposes the remaining segmentation teardown gap.
			matchLimit: 8,
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
			// A forced keyframe does not reset the sticky RTC+ROI
			// segmentation teardown drift; keep the full transition
			// covered while asserting the matching pre-disable prefix.
			matchLimit: 8,
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
			// Strict while active+ROI is enabled; teardown under sticky RTC
			// shares the remaining RTC+ROI disable gap.
			matchLimit: 8,
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
			extraArgs:  []string{"--noise-sensitivity=3", "--threads=2"},
			matchLimit: 1,
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
			// The initial custom ROI setup is strict; replacing it with a
			// different custom ROI map still has a one-byte header drift.
			matchLimit: 5,
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

func TestOracleEncoderStreamByteParityRuntimeReferenceControlCrosses(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime reference-control byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 900
		frames     = 10
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 32, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	segmented64 := fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}

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

	type referenceCase struct {
		name       string
		fx         fixture
		opts       EncoderOptions
		flags      []EncodeFlags
		script     []string
		apply      map[int]func(*testing.T, *VP8Encoder)
		extraArgs  []string
		matchLimit int
	}

	cases := []referenceCase{
		{
			name: "set-golden-after-force-golden-only",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				2: EncodeForceGoldenFrame | EncodeNoUpdateLast | EncodeNoUpdateAltRef,
				3: EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			}),
			script: runtimeControlScript(frames, map[int]string{
				3: "setref:golden:panning:9",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: setReferencePanningApply(ReferenceGolden, 9, "golden"),
			},
		},
		{
			name: "set-altref-after-force-altref-only",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				2: EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden,
				3: EncodeNoReferenceLast | EncodeNoReferenceGolden,
			}),
			script: runtimeControlScript(frames, map[int]string{
				3: "setref:altref:panning:10",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: setReferencePanningApply(ReferenceAltRef, 10, "altref"),
			},
		},
		{
			name: "set-altref-after-hidden-altref-refresh",
			fx:   panning32,
			opts: baseOpts(panning32),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				2: EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden,
				3: EncodeNoReferenceLast | EncodeNoReferenceGolden,
			}),
			script: runtimeControlScript(frames, map[int]string{
				3: "setref:altref:panning:11",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				3: setReferencePanningApply(ReferenceAltRef, 11, "altref"),
			},
		},
		{
			name: "set-golden-under-two-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringTwoLayers)
				appendRuntimeControl(script, 4, "setref:golden:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceGolden, 12, "golden"),
			},
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		},
		{
			name: "set-last-under-two-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringTwoLayers)
				appendRuntimeControl(script, 4, "setref:last:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceLast, 12, "last"),
			},
			extraArgs:  runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
			matchLimit: 4,
		},
		{
			name: "set-altref-under-two-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringTwoLayers)
				appendRuntimeControl(script, 4, "setref:altref:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceAltRef, 12, "altref"),
			},
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		},
		{
			name: "set-last-under-active-roi",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			script: runtimeControlScript(frames, map[int]string{
				1: "active:checker+roi:border1",
				4: "setref:last:panning:12",
				7: "active:off+roi:off",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
				4: setReferencePanningApply(ReferenceLast, 12, "last"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
					mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
				},
			},
		},
		{
			name: "set-last-under-rtc-external",
			fx:   panning64,
			opts: baseOpts(panning64),
			script: runtimeControlScript(frames, map[int]string{
				1: "rtc:1",
				4: "setref:last:panning:12",
				7: "rtc:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				4: setReferencePanningApply(ReferenceLast, 12, "last"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
			},
			matchLimit: 4,
		},
		{
			name: "set-golden-under-rtc-external",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				5: EncodeNoReferenceLast | EncodeNoReferenceAltRef,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "rtc:1",
				4: "setref:golden:panning:12",
				7: "rtc:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				4: setReferencePanningApply(ReferenceGolden, 12, "golden"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
			},
		},
		{
			name: "set-altref-under-rtc-external",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				5: EncodeNoReferenceLast | EncodeNoReferenceGolden,
			}),
			script: runtimeControlScript(frames, map[int]string{
				1: "rtc:1",
				4: "setref:altref:panning:12",
				7: "rtc:0",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				4: setReferencePanningApply(ReferenceAltRef, 12, "altref"),
				7: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
				},
			},
		},
		{
			name: "set-last-under-three-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringThreeLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringThreeLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringThreeLayers)
				appendRuntimeControl(script, 4, "setref:last:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceLast, 12, "last"),
			},
			extraArgs:  runtimeTemporalExtraArgs(TemporalLayeringThreeLayers, targetKbps),
			matchLimit: 4,
		},
		{
			name: "set-golden-under-three-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringThreeLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringThreeLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringThreeLayers)
				appendRuntimeControl(script, 4, "setref:golden:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceGolden, 12, "golden"),
			},
			extraArgs:  runtimeTemporalExtraArgs(TemporalLayeringThreeLayers, targetKbps),
			matchLimit: 4,
		},
		{
			name: "set-altref-under-three-layer-temporal",
			fx:   panning64,
			opts: func() EncoderOptions {
				opts := baseOpts(panning64)
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringThreeLayers, targetKbps)
				return opts
			}(),
			flags: temporalScalabilityReconfigureFlags(frames, TemporalLayeringThreeLayers, 0),
			script: func() []string {
				script := runtimeTemporalLayerIDScript(frames, TemporalLayeringThreeLayers)
				appendRuntimeControl(script, 4, "setref:altref:panning:12")
				return script
			}(),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: setReferencePanningApply(ReferenceAltRef, 12, "altref"),
			},
			extraArgs:  runtimeTemporalExtraArgs(TemporalLayeringThreeLayers, targetKbps),
			matchLimit: 4,
		},
		{
			name: "set-altref-after-runtime-resize",
			fx:   panning64,
			opts: baseOpts(panning64),
			flags: indexedResizeFlags(frames, map[int]EncodeFlags{
				5: EncodeNoReferenceLast | EncodeNoReferenceGolden,
			}),
			script: runtimeControlScript(frames, map[int]string{
				4: "resize:32x32",
				5: "setref:altref:panning:13",
			}),
			apply: map[int]func(*testing.T, *VP8Encoder){
				4: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRealtimeTarget(32x32)", e.SetRealtimeTarget(RealtimeTarget{Width: 32, Height: 32}))
				},
				5: setReferencePanningApply(ReferenceAltRef, 13, "altref"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				w, h := tc.fx.w, tc.fx.h
				if tc.name == "set-altref-after-runtime-resize" && i >= 4 {
					w, h = 32, 32
				}
				sources[i] = tc.fx.source(w, h, i)
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, sources, tc.flags, tc.apply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "runtime-reference-"+tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources, tc.flags, extraArgs)
			assertSegmentByteParity(t, "runtime-reference-"+tc.name, govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}

func TestOracleEncoderStreamByteParityRuntimeRateControlModeTransitions(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime rate-control mode-transition byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps         = 30
		targetKbps  = 700
		frames      = 8
		switchFrame = 3
	)
	modes := []RateControlMode{
		RateControlCBR,
		RateControlVBR,
		RateControlCQ,
		RateControlQ,
	}

	for _, from := range modes {
		for _, to := range modes {
			if from == to {
				continue
			}
			for _, forceKeyFrame := range []bool{false, true} {
				name := runtimeRateControlModeName(from) + "-to-" + runtimeRateControlModeName(to)
				if forceKeyFrame {
					name += "-force-kf"
				}
				t.Run(name, func(t *testing.T) {
					opts := EncoderOptions{
						Width:             32,
						Height:            32,
						FPS:               fps,
						RateControlMode:   from,
						TargetBitrateKbps: targetKbps,
						MinQuantizer:      4,
						MaxQuantizer:      56,
						CQLevel:           runtimeRateControlModeCQLevel(from),
						KeyFrameInterval:  999,
						Deadline:          DeadlineRealtime,
						CpuUsed:           0,
						Tuning:            TunePSNR,
					}
					flags := make([]EncodeFlags, frames)
					if forceKeyFrame {
						flags[switchFrame] = EncodeForceKeyFrame
					}
					script := runtimeControlScript(frames, map[int]string{
						switchFrame: runtimeRateControlModeControlToken(to, targetKbps),
					})
					apply := map[int]func(*testing.T, *VP8Encoder){
						switchFrame: func(t *testing.T, e *VP8Encoder) {
							t.Helper()
							mustRuntime(t, "SetRateControl("+runtimeRateControlModeName(to)+")", e.SetRateControl(runtimeRateControlModeConfig(to, targetKbps)))
						},
					}
					sources := make([]Image, frames)
					for i := range sources {
						sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
					}
					govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, flags, apply)
					libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, name, opts, targetKbps, sources, flags, []string{
						"--control-script=" + strings.Join(script, ","),
					})
					matchLimit := runtimeRateControlModeTransitionMatchLimit(from, to, forceKeyFrame, switchFrame)
					assertSegmentByteParity(t, "runtime-rc-mode-"+name, govpxFrames, libvpxFrames, matchLimit)
				})
			}
		}
	}
}

func TestOracleEncoderStreamByteParityRuntimeRateControlModeControlCrosses(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime rate-control mode/control cross byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps         = 30
		targetKbps  = 700
		frames      = 10
		switchFrame = 4
	)

	baseOpts := func(from RateControlMode) EncoderOptions {
		return EncoderOptions{
			Width:             32,
			Height:            32,
			FPS:               fps,
			RateControlMode:   from,
			TargetBitrateKbps: targetKbps,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			CQLevel:           runtimeRateControlModeCQLevel(from),
			KeyFrameInterval:  999,
			Deadline:          DeadlineRealtime,
			CpuUsed:           0,
			Tuning:            TunePSNR,
		}
	}
	rateControlApply := func(mode RateControlMode) func(*testing.T, *VP8Encoder) {
		return func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetRateControl("+runtimeRateControlModeName(mode)+")", e.SetRateControl(runtimeRateControlModeConfig(mode, targetKbps)))
		}
	}

	type rateControlCrossCase struct {
		name          string
		opts          EncoderOptions
		to            RateControlMode
		flags         []EncodeFlags
		script        []string
		apply         map[int]func(*testing.T, *VP8Encoder)
		extraArgs     []string
		forceKeyFrame bool
	}
	cases := []rateControlCrossCase{
		{
			name: "cbr-to-vbr-threads2-token4",
			opts: func() EncoderOptions {
				opts := baseOpts(RateControlCBR)
				opts.Threads = 2
				opts.TokenPartitions = 2
				return opts
			}(),
			to:        RateControlVBR,
			extraArgs: []string{"--threads=2"},
		},
		{
			name: "cbr-to-cq-screen2-static500",
			opts: func() EncoderOptions {
				opts := baseOpts(RateControlCBR)
				opts.ScreenContentMode = 2
				opts.StaticThreshold = 500
				return opts
			}(),
			to:        RateControlCQ,
			extraArgs: []string{"--screen-content-mode=2", "--static-thresh=500"},
		},
		{
			name: "vbr-to-q-force-kf-token4",
			opts: func() EncoderOptions {
				opts := baseOpts(RateControlVBR)
				opts.TokenPartitions = 2
				return opts
			}(),
			to:            RateControlQ,
			forceKeyFrame: true,
		},
		{
			name: "q-to-vbr-threads2",
			opts: func() EncoderOptions {
				opts := baseOpts(RateControlQ)
				opts.Threads = 2
				return opts
			}(),
			to:        RateControlVBR,
			extraArgs: []string{"--threads=2"},
		},
	}

	for _, tc := range cases {
		if tc.script == nil {
			tc.script = runtimeControlScript(frames, map[int]string{
				switchFrame: runtimeRateControlModeControlToken(tc.to, targetKbps),
			})
		}
		if tc.apply == nil {
			tc.apply = map[int]func(*testing.T, *VP8Encoder){
				switchFrame: rateControlApply(tc.to),
			}
		}
		if tc.forceKeyFrame && tc.flags == nil {
			tc.flags = make([]EncodeFlags, frames)
			tc.flags[switchFrame] = EncodeForceKeyFrame
		}
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(tc.opts.Width, tc.opts.Height, i)
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, sources, tc.flags, tc.apply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "runtime-rc-mode-cross-"+tc.name, tc.opts, targetKbps, sources, tc.flags, extraArgs)
			assertSegmentByteParity(t, "runtime-rc-mode-cross-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}

func TestOracleEncoderStreamByteParityRuntimeRateControlModeLongTailTransitions(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime rate-control mode-transition long-tail byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps         = 30
		targetKbps  = 700
		frames      = 16
		switchFrame = 3
	)
	cases := []struct {
		name          string
		from          RateControlMode
		to            RateControlMode
		forceKeyFrame bool
		matchLimit    int
	}{
		{name: "vbr-to-cbr-long-tail", from: RateControlVBR, to: RateControlCBR},
		{name: "cq-to-cbr-no-force-long-tail", from: RateControlCQ, to: RateControlCBR},
		{name: "cq-to-cbr-force-kf-long-tail", from: RateControlCQ, to: RateControlCBR, forceKeyFrame: true},
		{name: "q-to-cbr-no-force-kf-long-tail", from: RateControlQ, to: RateControlCBR},
		{name: "cq-to-q-post-switch-tail", from: RateControlCQ, to: RateControlQ},
	}
	for _, tc := range cases {
		if tc.matchLimit == 0 {
			tc.matchLimit = runtimeRateControlModeTransitionMatchLimit(tc.from, tc.to, tc.forceKeyFrame, switchFrame)
		}
		t.Run(tc.name, func(t *testing.T) {
			opts := EncoderOptions{
				Width:             32,
				Height:            32,
				FPS:               fps,
				RateControlMode:   tc.from,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				CQLevel:           runtimeRateControlModeCQLevel(tc.from),
				KeyFrameInterval:  999,
				Deadline:          DeadlineRealtime,
				CpuUsed:           0,
				Tuning:            TunePSNR,
			}
			flags := make([]EncodeFlags, frames)
			if tc.forceKeyFrame {
				flags[switchFrame] = EncodeForceKeyFrame
			}
			script := runtimeControlScript(frames, map[int]string{
				switchFrame: runtimeRateControlModeControlToken(tc.to, targetKbps),
			})
			apply := map[int]func(*testing.T, *VP8Encoder){
				switchFrame: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRateControl("+runtimeRateControlModeName(tc.to)+")", e.SetRateControl(runtimeRateControlModeConfig(tc.to, targetKbps)))
				},
			}
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, flags, apply)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, tc.name, opts, targetKbps, sources, flags, []string{
				"--control-script=" + strings.Join(script, ","),
			})
			assertSegmentByteParity(t, "runtime-rc-mode-long-tail-"+tc.name, govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}

func TestOracleEncoderStreamByteParityRuntimeTemporalControlCrosses(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run runtime temporal-control byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	segmented64 := fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}

	baseOpts := func(fx fixture, cpuUsed int) EncoderOptions {
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
			CpuUsed:           cpuUsed,
			Tuning:            TunePSNR,
		}
	}
	temporalOpts := func(fx fixture, cpuUsed int, mode TemporalLayeringMode) EncoderOptions {
		opts := baseOpts(fx, cpuUsed)
		opts.TemporalScalability = runtimeTemporalConfig(mode, targetKbps)
		return opts
	}
	autoTemporalOpts := func(fx fixture, cpuUsed int, mode TemporalLayeringMode) EncoderOptions {
		opts := baseOpts(fx, cpuUsed)
		opts.TemporalScalability = TemporalScalabilityConfig{Enabled: true, Mode: mode}
		return opts
	}

	type temporalCase struct {
		name       string
		fx         fixture
		frames     int
		opts       EncoderOptions
		flags      []EncodeFlags
		script     []string
		apply      map[int]func(*testing.T, *VP8Encoder)
		extraArgs  []string
		matchLimit int
	}

	twoLayerEnableScript := temporalScalabilityWindowScript(12, TemporalLayeringTwoLayers, 2, 12, runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps))
	twoLayerDisableScript := runtimeTemporalDisableScript(12, TemporalLayeringTwoLayers, 6, targetKbps)

	cases := []temporalCase{
		{
			name:   "two-layer-enable-only",
			fx:     panning64,
			frames: 12,
			opts:   baseOpts(panning64, 0),
			flags:  temporalScalabilityWindowFlags(12, TemporalLayeringTwoLayers, 2, 12),
			script: twoLayerEnableScript,
			// The enable keyframe matches; the first inter-layer packet
			// after enabling exposes the existing temporal context drift.
			matchLimit: 3,
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: runtimeTemporalApply(TemporalLayeringTwoLayers, targetKbps, "two-layer"),
			},
		},
		{
			name:      "two-layer-disable-only",
			fx:        panning64,
			frames:    12,
			opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
			flags:     temporalScalabilityWindowFlags(12, TemporalLayeringTwoLayers, 0, 6),
			script:    twoLayerDisableScript,
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
			// The pure temporal stream matches until the disable packet.
			matchLimit: 6,
			apply: map[int]func(*testing.T, *VP8Encoder){
				6: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
		{
			name:       "three-layer-enable-disable-only",
			fx:         panning64,
			frames:     12,
			opts:       baseOpts(panning64, 0),
			flags:      temporalScalabilityWindowFlags(12, TemporalLayeringThreeLayers, 2, 8),
			script:     temporalScalabilityWindowScript(12, TemporalLayeringThreeLayers, 2, 8, runtimeTemporalControlToken(TemporalLayeringThreeLayers, targetKbps)),
			matchLimit: 3,
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: runtimeTemporalApply(TemporalLayeringThreeLayers, targetKbps, "three-layer"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
		{
			name:       "two-layer-with-sync-enable-disable-only",
			fx:         panning64,
			frames:     12,
			opts:       baseOpts(panning64, 0),
			flags:      temporalScalabilityWindowFlags(12, TemporalLayeringTwoLayersWithSync, 2, 8),
			script:     temporalScalabilityWindowScript(12, TemporalLayeringTwoLayersWithSync, 2, 8, runtimeTemporalControlToken(TemporalLayeringTwoLayersWithSync, targetKbps)),
			matchLimit: 3,
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: runtimeTemporalApply(TemporalLayeringTwoLayersWithSync, targetKbps, "two-layer-with-sync"),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		},
	}

	for _, tc := range []struct {
		name string
		mode TemporalLayeringMode
	}{
		{name: "three-layer-no-inter-layer-prediction-enable-disable-only", mode: TemporalLayeringThreeLayersNoInterLayerPrediction},
		{name: "three-layer-layer-one-prediction-enable-disable-only", mode: TemporalLayeringThreeLayersLayerOnePrediction},
		{name: "three-layer-altref-sync-enable-disable-only", mode: TemporalLayeringThreeLayersAltRefWithSync},
		{name: "three-layer-one-reference-enable-disable-only", mode: TemporalLayeringThreeLayersOneReference},
	} {
		modeName := tc.name
		mode := tc.mode
		cases = append(cases, temporalCase{
			name:       modeName,
			fx:         panning64,
			frames:     12,
			opts:       baseOpts(panning64, 0),
			flags:      temporalScalabilityWindowFlags(12, mode, 2, 8),
			script:     temporalScalabilityWindowScript(12, mode, 2, 8, runtimeTemporalControlToken(mode, targetKbps)),
			matchLimit: 3,
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: runtimeTemporalApply(mode, targetKbps, modeName),
				8: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		})
	}

	for _, cpuUsed := range []int{0, -3, -8} {
		frames := 18
		mode := TemporalLayeringFiveLayers
		script := temporalScalabilityWindowScript(frames, mode, 2, frames, runtimeTemporalControlToken(mode, targetKbps))
		cases = append(cases, temporalCase{
			name:       "five-layer-enable-only-cpu" + strconv.Itoa(cpuUsed),
			fx:         panning64,
			frames:     frames,
			opts:       baseOpts(panning64, cpuUsed),
			flags:      temporalScalabilityWindowFlags(frames, mode, 2, frames),
			script:     script,
			matchLimit: 3,
			apply: map[int]func(*testing.T, *VP8Encoder){
				2: runtimeTemporalApply(mode, targetKbps, "five-layer"),
			},
		})
	}

	{
		frames := 18
		mode := TemporalLayeringFiveLayers
		cases = append(cases, temporalCase{
			name:       "five-layer-disable-only-cpu-3",
			fx:         panning64,
			frames:     frames,
			opts:       temporalOpts(panning64, -3, mode),
			flags:      temporalScalabilityWindowFlags(frames, mode, 0, 10),
			script:     runtimeTemporalDisableScript(frames, mode, 10, targetKbps),
			extraArgs:  runtimeTemporalExtraArgs(mode, targetKbps),
			matchLimit: 8,
			apply: map[int]func(*testing.T, *VP8Encoder){
				10: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
		})
	}

	{
		frames := 18
		cases = append(cases,
			temporalCase{
				name:       "two-layer-to-five-layer-transition",
				fx:         panning64,
				frames:     frames,
				opts:       temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
				flags:      temporalScalabilityModeSwitchFlags(frames, TemporalLayeringTwoLayers, TemporalLayeringFiveLayers, 8),
				script:     temporalScalabilityModeSwitchScript(frames, TemporalLayeringTwoLayers, TemporalLayeringFiveLayers, 8, targetKbps),
				extraArgs:  runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
				matchLimit: 8,
				apply: map[int]func(*testing.T, *VP8Encoder){
					8: runtimeTemporalApply(TemporalLayeringFiveLayers, targetKbps, "five-layer"),
				},
			},
			temporalCase{
				name:       "five-layer-to-two-layer-transition",
				fx:         panning64,
				frames:     frames,
				opts:       temporalOpts(panning64, 0, TemporalLayeringFiveLayers),
				flags:      temporalScalabilityModeSwitchFlags(frames, TemporalLayeringFiveLayers, TemporalLayeringTwoLayers, 10),
				script:     temporalScalabilityModeSwitchScript(frames, TemporalLayeringFiveLayers, TemporalLayeringTwoLayers, 10, targetKbps),
				extraArgs:  runtimeTemporalExtraArgs(TemporalLayeringFiveLayers, targetKbps),
				matchLimit: 8,
				apply: map[int]func(*testing.T, *VP8Encoder){
					10: runtimeTemporalApply(TemporalLayeringTwoLayers, targetKbps, "two-layer"),
				},
			},
		)
	}

	twoLayerScript := func(frames int) []string {
		return runtimeTemporalLayerIDScript(frames, TemporalLayeringTwoLayers)
	}
	twoLayerFlags := func(frames int) []EncodeFlags {
		return temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 0)
	}

	twoLayerCPUScript := twoLayerScript(12)
	appendRuntimeControl(twoLayerCPUScript, 4, "cpu:-3")
	appendRuntimeControl(twoLayerCPUScript, 8, "cpu:0")
	cases = append(cases, temporalCase{
		name:       "two-layer-cpu-used-roundtrip",
		fx:         panning64,
		frames:     12,
		opts:       temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:      twoLayerFlags(12),
		script:     twoLayerCPUScript,
		extraArgs:  runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		matchLimit: 8,
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetCPUUsed(-3)", e.SetCPUUsed(-3))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetCPUUsed(0)", e.SetCPUUsed(0))
			},
		},
	})

	twoLayerDeadlineScript := twoLayerScript(12)
	appendRuntimeControl(twoLayerDeadlineScript, 4, "deadline:good")
	appendRuntimeControl(twoLayerDeadlineScript, 8, "deadline:rt")
	cases = append(cases, temporalCase{
		name:      "two-layer-deadline-good-rt-roundtrip",
		fx:        panning64,
		frames:    12,
		opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    twoLayerDeadlineScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetDeadline(good)", e.SetDeadline(DeadlineGoodQuality))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetDeadline(rt)", e.SetDeadline(DeadlineRealtime))
			},
		},
	})

	for _, tc := range []struct {
		name    string
		mode    RateControlMode
		cqLevel int
	}{
		{name: "two-layer-vbr", mode: RateControlVBR},
		{name: "two-layer-cq20", mode: RateControlCQ, cqLevel: 20},
		{name: "two-layer-q20", mode: RateControlQ, cqLevel: 20},
	} {
		rcMode := tc.mode
		cqLevel := tc.cqLevel
		cases = append(cases, temporalCase{
			name:   tc.name,
			fx:     panning64,
			frames: 12,
			opts: func() EncoderOptions {
				opts := temporalOpts(panning64, 0, TemporalLayeringTwoLayers)
				opts.RateControlMode = rcMode
				opts.CQLevel = cqLevel
				return opts
			}(),
			flags:     twoLayerFlags(12),
			script:    twoLayerScript(12),
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		})
	}

	fiveLayerCPUScript := runtimeTemporalLayerIDScript(18, TemporalLayeringFiveLayers)
	appendRuntimeControl(fiveLayerCPUScript, 6, "cpu:-3")
	appendRuntimeControl(fiveLayerCPUScript, 12, "cpu:0")
	cases = append(cases, temporalCase{
		name:       "five-layer-cpu-used-roundtrip",
		fx:         panning64,
		frames:     18,
		opts:       temporalOpts(panning64, 0, TemporalLayeringFiveLayers),
		flags:      temporalScalabilityReconfigureFlags(18, TemporalLayeringFiveLayers, 0),
		script:     fiveLayerCPUScript,
		extraArgs:  runtimeTemporalExtraArgs(TemporalLayeringFiveLayers, targetKbps),
		matchLimit: 8,
		apply: map[int]func(*testing.T, *VP8Encoder){
			6: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetCPUUsed(-3)", e.SetCPUUsed(-3))
			},
			12: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetCPUUsed(0)", e.SetCPUUsed(0))
			},
		},
	})

	dropScript := twoLayerScript(12)
	appendRuntimeControl(dropScript, 4, "drop:60")
	appendRuntimeControl(dropScript, 8, "drop:0")
	cases = append(cases, temporalCase{
		name:      "two-layer-frame-drop-toggle",
		fx:        panning64,
		frames:    12,
		opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    dropScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetFrameDropAllowed(true)", e.SetFrameDropAllowed(true))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetFrameDropAllowed(false)", e.SetFrameDropAllowed(false))
			},
		},
	})

	bitrateScript := twoLayerScript(12)
	appendRuntimeControl(bitrateScript, 4, "bitrate:400+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, 400))
	appendRuntimeControl(bitrateScript, 8, "bitrate:900+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, 900))
	cases = append(cases, temporalCase{
		name:       "two-layer-bitrate-reconfigure-low-high",
		fx:         panning64,
		frames:     12,
		opts:       autoTemporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:      twoLayerFlags(12),
		script:     bitrateScript,
		extraArgs:  runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		matchLimit: 5,
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetBitrateKbps(400)", e.SetBitrateKbps(400))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetBitrateKbps(900)", e.SetBitrateKbps(900))
			},
		},
	})

	realtimeScript := twoLayerScript(12)
	appendRuntimeControl(realtimeScript, 4, "fps:24+bitrate:500+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, 500))
	appendRuntimeControl(realtimeScript, 8, "fps:30+bitrate:700+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps))
	cases = append(cases, temporalCase{
		name:      "two-layer-realtime-target-fps-bitrate-reconfigure",
		fx:        panning64,
		frames:    12,
		opts:      autoTemporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    realtimeScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRealtimeTarget(fps24-bitrate500)", e.SetRealtimeTarget(RealtimeTarget{FPS: 24, BitrateKbps: 500}))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRealtimeTarget(fps30-bitrate700)", e.SetRealtimeTarget(RealtimeTarget{FPS: 30, BitrateKbps: targetKbps}))
			},
		},
	})

	tokenERScript := twoLayerScript(12)
	cases = append(cases, temporalCase{
		name:   "two-layer-token8-er3",
		fx:     panning64,
		frames: 12,
		opts: func() EncoderOptions {
			opts := temporalOpts(panning64, 0, TemporalLayeringTwoLayers)
			opts.ErrorResilient = true
			opts.ErrorResilientPartitions = true
			opts.TokenPartitions = 3
			return opts
		}(),
		flags:     twoLayerFlags(12),
		script:    tokenERScript,
		extraArgs: append(runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps), "--error-resilient=3"),
	})

	screenStaticScript := twoLayerScript(12)
	cases = append(cases, temporalCase{
		name:   "two-layer-screen2-static500",
		fx:     panning64,
		frames: 12,
		opts: func() EncoderOptions {
			opts := temporalOpts(panning64, 0, TemporalLayeringTwoLayers)
			opts.ScreenContentMode = 2
			opts.StaticThreshold = 500
			return opts
		}(),
		flags:     twoLayerFlags(12),
		script:    screenStaticScript,
		extraArgs: append(runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps), "--screen-content-mode=2", "--static-thresh=500"),
	})

	noiseScript := twoLayerScript(12)
	cases = append(cases, temporalCase{
		name:   "two-layer-noise3",
		fx:     panning64,
		frames: 12,
		opts: func() EncoderOptions {
			opts := temporalOpts(panning64, 0, TemporalLayeringTwoLayers)
			opts.NoiseSensitivity = 3
			return opts
		}(),
		flags:     twoLayerFlags(12),
		script:    noiseScript,
		extraArgs: append(runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps), "--noise-sensitivity=3"),
	})

	rtcScript := twoLayerScript(12)
	appendRuntimeControl(rtcScript, 4, "rtc:1")
	appendRuntimeControl(rtcScript, 8, "rtc:0")
	cases = append(cases, temporalCase{
		name:      "two-layer-rtc-external-toggle",
		fx:        panning64,
		frames:    12,
		opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    rtcScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		apply: map[int]func(*testing.T, *VP8Encoder){
			4: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
			},
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(false)", e.SetRTCExternalRateControl(false))
			},
		},
	})

	activeScript := twoLayerScript(12)
	appendRuntimeControl(activeScript, 2, "active:checker")
	appendRuntimeControl(activeScript, 8, "active:off")
	cases = append(cases, temporalCase{
		name:      "two-layer-active-map-checker-toggle",
		fx:        panning64,
		frames:    12,
		opts:      temporalOpts(panning64, 0, TemporalLayeringTwoLayers),
		flags:     twoLayerFlags(12),
		script:    activeScript,
		extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		// Active-map setup matches for both base and first enhancement
		// layer packets; later temporal-layer context diverges.
		matchLimit: 4,
		apply: map[int]func(*testing.T, *VP8Encoder){
			2: activeMapApply("checker"),
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
			},
		},
	})

	roiScript := twoLayerScript(12)
	appendRuntimeControl(roiScript, 2, "roi:border1")
	appendRuntimeControl(roiScript, 8, "roi:off")
	cases = append(cases, temporalCase{
		name:       "two-layer-roi-border-toggle",
		fx:         segmented64,
		frames:     12,
		opts:       temporalOpts(segmented64, 0, TemporalLayeringTwoLayers),
		flags:      twoLayerFlags(12),
		script:     roiScript,
		extraArgs:  runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		matchLimit: 2,
		apply: map[int]func(*testing.T, *VP8Encoder){
			2: roiMapApply("border1"),
			8: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetROIMap(nil)", e.SetROIMap(nil))
			},
		},
	})

	activeROIScript := twoLayerScript(12)
	appendRuntimeControl(activeROIScript, 2, "active:checker+roi:border1")
	appendRuntimeControl(activeROIScript, 8, "active:off+roi:off")
	cases = append(cases, temporalCase{
		name:       "two-layer-active-roi-toggle",
		fx:         segmented64,
		frames:     12,
		opts:       temporalOpts(segmented64, 0, TemporalLayeringTwoLayers),
		flags:      twoLayerFlags(12),
		script:     activeROIScript,
		extraArgs:  runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
		matchLimit: 2,
		apply: map[int]func(*testing.T, *VP8Encoder){
			2: func(t *testing.T, e *VP8Encoder) {
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
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, tc.frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, tc.opts, sources, tc.flags, tc.apply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.script, ","))
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "runtime-temporal-"+tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources, tc.flags, extraArgs)
			assertSegmentByteParity(t, "runtime-temporal-"+tc.name, govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}

func runtimeControlScript(frames int, updates map[int]string) []string {
	script := make([]string, frames)
	for i := range script {
		script[i] = "-"
	}
	for frame, update := range updates {
		if frame >= 0 && frame < frames {
			script[frame] = update
		}
	}
	return script
}

func runtimeRateControlModeName(mode RateControlMode) string {
	switch mode {
	case RateControlCBR:
		return "cbr"
	case RateControlVBR:
		return "vbr"
	case RateControlCQ:
		return "cq"
	case RateControlQ:
		return "q"
	default:
		panic("unknown rate-control mode")
	}
}

func runtimeRateControlModeCQLevel(mode RateControlMode) int {
	switch mode {
	case RateControlCQ:
		return 30
	case RateControlQ:
		return 20
	default:
		return 0
	}
}

func runtimeRateControlModeControlToken(mode RateControlMode, targetKbps int) string {
	token := "endusage:" + runtimeRateControlModeName(mode) +
		"+bitrate:" + strconv.Itoa(targetKbps) +
		"+minq:4+maxq:56+undershoot:100+overshoot:100+bufsz:6000+bufinit:4000+bufopt:5000"
	if cqLevel := runtimeRateControlModeCQLevel(mode); cqLevel > 0 {
		token += "+cq:" + strconv.Itoa(cqLevel)
	}
	return token
}

func runtimeRateControlModeConfig(mode RateControlMode, targetKbps int) RateControlConfig {
	return RateControlConfig{
		Mode:                mode,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             runtimeRateControlModeCQLevel(mode),
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}
}

func runtimeRateControlModeTransitionMatchLimit(from, to RateControlMode, forceKeyFrame bool, switchFrame int) int {
	if from == RateControlCBR {
		return 0
	}
	if to == RateControlCBR {
		if forceKeyFrame {
			if from == RateControlCQ {
				return switchFrame
			}
			return switchFrame + 1
		}
		return 0
	}
	if forceKeyFrame && from == RateControlCQ {
		return switchFrame
	}
	return 0
}

func runtimeTemporalConfig(mode TemporalLayeringMode, targetKbps int) TemporalScalabilityConfig {
	return TemporalScalabilityConfig{
		Enabled:                true,
		Mode:                   mode,
		LayerTargetBitrateKbps: runtimeTemporalBitrates(mode, targetKbps),
	}
}

func runtimeTemporalBitrates(mode TemporalLayeringMode, targetKbps int) [MaxTemporalLayers]int {
	switch mode {
	case TemporalLayeringTwoLayers, TemporalLayeringTwoLayersThreeFrame, TemporalLayeringTwoLayersWithSync:
		return [MaxTemporalLayers]int{targetKbps * 3 / 5, targetKbps}
	case TemporalLayeringFiveLayers:
		return [MaxTemporalLayers]int{targetKbps / 7, targetKbps * 11 / 35, targetKbps * 18 / 35, targetKbps * 26 / 35, targetKbps}
	default:
		return [MaxTemporalLayers]int{targetKbps * 2 / 5, targetKbps * 3 / 5, targetKbps}
	}
}

func runtimeTemporalApply(mode TemporalLayeringMode, targetKbps int, name string) func(*testing.T, *VP8Encoder) {
	return func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		mustRuntime(t, "SetTemporalScalability("+name+")", e.SetTemporalScalability(runtimeTemporalConfig(mode, targetKbps)))
	}
}

func runtimeTemporalControlToken(mode TemporalLayeringMode, targetKbps int) string {
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	bitrates := runtimeTemporalBitrates(mode, targetKbps)
	return "tslayers:" + strconv.Itoa(pattern.Layers) +
		"+tsperiodicity:" + strconv.Itoa(pattern.Periodicity) +
		"+tsbitrates:" + joinRuntimeInts(bitrates[:pattern.Layers], "/") +
		"+tsdecimators:" + joinRuntimeInts(pattern.RateDecimator[:pattern.Layers], "/") +
		"+tsids:" + joinRuntimeInts(pattern.LayerID[:pattern.Periodicity], "/")
}

func runtimeTemporalOffControlToken(targetKbps int) string {
	return "tslayers:1+tsperiodicity:1+tsbitrates:" + strconv.Itoa(targetKbps) + "+tsdecimators:1+tsids:0"
}

func runtimeTemporalExtraArgs(mode TemporalLayeringMode, targetKbps int) []string {
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	bitrates := runtimeTemporalBitrates(mode, targetKbps)
	return []string{
		"--temporal-layers=" + strconv.Itoa(pattern.Layers),
		"--temporal-bitrates=" + joinRuntimeInts(bitrates[:pattern.Layers], ","),
		"--temporal-decimators=" + joinRuntimeInts(pattern.RateDecimator[:pattern.Layers], ","),
		"--temporal-periodicity=" + strconv.Itoa(pattern.Periodicity),
		"--temporal-layer-ids=" + joinRuntimeInts(pattern.LayerID[:pattern.Periodicity], ","),
	}
}

func runtimeTemporalLayerIDScript(frames int, mode TemporalLayeringMode) []string {
	script := runtimeControlScript(frames, nil)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		script[frame] = "tlid:" + strconv.Itoa(temporalPatternLayerID(pattern, uint64(frame)))
	}
	return script
}

func runtimeTemporalDisableScript(frames int, mode TemporalLayeringMode, disableFrame int, targetKbps int) []string {
	script := runtimeControlScript(frames, nil)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	for frame := 0; frame < frames && frame < disableFrame; frame++ {
		script[frame] = "tlid:" + strconv.Itoa(temporalPatternLayerID(pattern, uint64(frame)))
	}
	if disableFrame >= 0 && disableFrame < frames {
		script[disableFrame] = runtimeTemporalOffControlToken(targetKbps)
	}
	return script
}

func appendRuntimeControl(script []string, frame int, token string) {
	if frame < 0 || frame >= len(script) {
		return
	}
	if script[frame] == "" || script[frame] == "-" {
		script[frame] = token
		return
	}
	script[frame] += "+" + token
}

func joinRuntimeInts(values []int, sep string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, sep)
}

func temporalTwoLayerFlags(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for i := range flags {
		if i%2 == 0 {
			flags[i] = EncodeNoUpdateGolden | EncodeNoUpdateAltRef | EncodeNoReferenceGolden | EncodeNoReferenceAltRef
			if i == 0 {
				flags[i] |= EncodeForceKeyFrame
			}
			continue
		}
		flags[i] = EncodeNoUpdateAltRef | EncodeNoUpdateLast | EncodeNoReferenceAltRef
	}
	return flags
}

func temporalThreeLayerFlags(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	pattern, ok := temporalLayeringPattern(TemporalLayeringThreeLayers)
	if !ok {
		panic("missing three-layer temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		flags[frame] = temporalPatternFlag(pattern, uint64(frame), TemporalLayeringThreeLayers)
	}
	return flags
}

func temporalLayerIDScript(frames int, ids []int) []string {
	script := runtimeControlScript(frames, nil)
	for frame, id := range ids {
		if frame >= 0 && frame < frames {
			script[frame] = "tlid:" + strconv.Itoa(id)
		}
	}
	return script
}

func temporalLayerIDApply(ids []int) map[int]func(*testing.T, *VP8Encoder) {
	apply := make(map[int]func(*testing.T, *VP8Encoder), len(ids))
	for frame, id := range ids {
		layerID := id
		apply[frame] = func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetTemporalLayerID", e.SetTemporalLayerID(layerID))
		}
	}
	return apply
}

func temporalScalabilityEnableDisableFlags(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	pattern, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	for frame := 2; frame < 6 && frame < frames; frame++ {
		flags[frame] = temporalPatternFlag(pattern, uint64(frame-2), TemporalLayeringTwoLayers)
	}
	return flags
}

func temporalScalabilityEnableDisableScript(frames int) []string {
	script := runtimeControlScript(frames, nil)
	if frames > 2 {
		script[2] = "tslayers:2+tsperiodicity:2+tsbitrates:420/700+tsdecimators:2/1+tsids:0/1+tlid:0"
	}
	if frames > 3 {
		script[3] = "tlid:1"
	}
	if frames > 4 {
		script[4] = "tlid:0"
	}
	if frames > 5 {
		script[5] = "tlid:1"
	}
	if frames > 6 {
		script[6] = "tslayers:1+tsperiodicity:1+tsbitrates:700+tsdecimators:1+tsids:0"
	}
	return script
}

func temporalScalabilityThreeToTwoFlags(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	threeLayer, ok := temporalLayeringPattern(TemporalLayeringThreeLayers)
	if !ok {
		panic("missing three-layer temporal pattern")
	}
	twoLayer, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < 6 {
			flags[frame] = temporalPatternFlag(threeLayer, uint64(frame), TemporalLayeringThreeLayers)
			continue
		}
		flags[frame] = temporalPatternFlag(twoLayer, uint64(frame-6), TemporalLayeringTwoLayers)
	}
	return flags
}

func temporalScalabilityThreeToTwoScript(frames int) []string {
	script := runtimeControlScript(frames, nil)
	threeLayer, ok := temporalLayeringPattern(TemporalLayeringThreeLayers)
	if !ok {
		panic("missing three-layer temporal pattern")
	}
	twoLayer, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < 6 {
			layerID := temporalPatternLayerID(threeLayer, uint64(frame))
			script[frame] = "tlid:" + strconv.Itoa(layerID)
			continue
		}
		layerID := temporalPatternLayerID(twoLayer, uint64(frame-6))
		token := "tlid:" + strconv.Itoa(layerID)
		if frame == 6 {
			token = "tslayers:2+tsperiodicity:2+tsbitrates:420/700+tsdecimators:2/1+tsids:0/1+" + token
		}
		script[frame] = token
	}
	return script
}

func temporalScalabilityTwoToThreeFlags(frames int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	twoLayer, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	threeLayer, ok := temporalLayeringPattern(TemporalLayeringThreeLayers)
	if !ok {
		panic("missing three-layer temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < 6 {
			flags[frame] = temporalPatternFlag(twoLayer, uint64(frame), TemporalLayeringTwoLayers)
			continue
		}
		flags[frame] = temporalPatternFlag(threeLayer, uint64(frame-6), TemporalLayeringThreeLayers)
	}
	return flags
}

func temporalScalabilityTwoToThreeScript(frames int) []string {
	script := runtimeControlScript(frames, nil)
	twoLayer, ok := temporalLayeringPattern(TemporalLayeringTwoLayers)
	if !ok {
		panic("missing two-layer temporal pattern")
	}
	threeLayer, ok := temporalLayeringPattern(TemporalLayeringThreeLayers)
	if !ok {
		panic("missing three-layer temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < 6 {
			layerID := temporalPatternLayerID(twoLayer, uint64(frame))
			script[frame] = "tlid:" + strconv.Itoa(layerID)
			continue
		}
		layerID := temporalPatternLayerID(threeLayer, uint64(frame-6))
		token := "tlid:" + strconv.Itoa(layerID)
		if frame == 6 {
			token = "tslayers:3+tsperiodicity:4+tsbitrates:280/420/700+tsdecimators:4/2/1+tsids:0/2/1/2+" + token
		}
		script[frame] = token
	}
	return script
}

func temporalScalabilityModeSwitchFlags(frames int, from TemporalLayeringMode, to TemporalLayeringMode, switchFrame int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	fromPattern, ok := temporalLayeringPattern(from)
	if !ok {
		panic("missing source temporal pattern")
	}
	toPattern, ok := temporalLayeringPattern(to)
	if !ok {
		panic("missing destination temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < switchFrame {
			flags[frame] = temporalPatternFlag(fromPattern, uint64(frame), from)
			continue
		}
		flags[frame] = temporalPatternFlag(toPattern, uint64(frame-switchFrame), to)
	}
	return flags
}

func temporalScalabilityModeSwitchScript(frames int, from TemporalLayeringMode, to TemporalLayeringMode, switchFrame int, targetKbps int) []string {
	script := runtimeControlScript(frames, nil)
	fromPattern, ok := temporalLayeringPattern(from)
	if !ok {
		panic("missing source temporal pattern")
	}
	toPattern, ok := temporalLayeringPattern(to)
	if !ok {
		panic("missing destination temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		if frame < switchFrame {
			script[frame] = "tlid:" + strconv.Itoa(temporalPatternLayerID(fromPattern, uint64(frame)))
			continue
		}
		token := "tlid:" + strconv.Itoa(temporalPatternLayerID(toPattern, uint64(frame-switchFrame)))
		if frame == switchFrame {
			token = runtimeTemporalControlToken(to, targetKbps) + "+" + token
		}
		script[frame] = token
	}
	return script
}

func temporalScalabilityReconfigureFlags(frames int, mode TemporalLayeringMode, switchFrame int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		offset := frame
		if frame >= switchFrame {
			offset = frame - switchFrame
		}
		flags[frame] = temporalPatternFlag(pattern, uint64(offset), mode)
	}
	return flags
}

func temporalScalabilityReconfigureScript(frames int, mode TemporalLayeringMode, switchFrame int, configToken string) []string {
	script := runtimeControlScript(frames, nil)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	for frame := 0; frame < frames; frame++ {
		offset := frame
		if frame >= switchFrame {
			offset = frame - switchFrame
		}
		token := "tlid:" + strconv.Itoa(temporalPatternLayerID(pattern, uint64(offset)))
		if frame == switchFrame {
			token = configToken + "+" + token
		}
		script[frame] = token
	}
	return script
}

func temporalScalabilityWindowFlags(frames int, mode TemporalLayeringMode, start int, end int) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	if start < 0 {
		start = 0
	}
	if end > frames {
		end = frames
	}
	for frame := start; frame < end; frame++ {
		flags[frame] = temporalPatternFlag(pattern, uint64(frame-start), mode)
	}
	return flags
}

func temporalScalabilityWindowScript(frames int, mode TemporalLayeringMode, start int, end int, configToken string) []string {
	script := runtimeControlScript(frames, nil)
	pattern, ok := temporalLayeringPattern(mode)
	if !ok {
		panic("missing temporal pattern")
	}
	if start < 0 {
		start = 0
	}
	if end > frames {
		end = frames
	}
	for frame := start; frame < end; frame++ {
		token := "tlid:" + strconv.Itoa(temporalPatternLayerID(pattern, uint64(frame-start)))
		if frame == start {
			token = configToken + "+" + token
		}
		script[frame] = token
	}
	if end >= 0 && end < frames {
		script[end] = "tslayers:1+tsperiodicity:1+tsbitrates:700+tsdecimators:1+tsids:0"
	}
	return script
}

func temporalPatternFlag(pattern temporalPattern, frameIndex uint64, mode TemporalLayeringMode) EncodeFlags {
	flagIndex := int(frameIndex % uint64(pattern.FlagPeriodicity))
	flags := pattern.Flags[flagIndex]
	if mode != TemporalLayeringFiveLayers && frameIndex > 0 && flagIndex == 0 {
		flags &^= EncodeForceKeyFrame
	}
	return flags
}

func temporalPatternLayerID(pattern temporalPattern, frameIndex uint64) int {
	return pattern.LayerID[int(frameIndex%uint64(pattern.Periodicity))]
}

func encodeFramesWithGovpxRuntimeControls(t *testing.T, opts EncoderOptions, sources []Image, flags []EncodeFlags, apply map[int]func(*testing.T, *VP8Encoder)) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		if fn := apply[i]; fn != nil {
			fn(t, enc)
		}
		var f EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, f)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if !result.Dropped {
			out = append(out, append([]byte(nil), result.Data...))
		}
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		if !result.Dropped {
			out = append(out, append([]byte(nil), result.Data...))
		}
	}
	return out
}

func mustRuntime(t *testing.T, name string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s returned error: %v", name, err)
	}
}

func activeMapApply(pattern string) func(*testing.T, *VP8Encoder) {
	return func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		rows := encoderMacroblockRows(e.opts.Height)
		cols := encoderMacroblockCols(e.opts.Width)
		mustRuntime(t, "SetActiveMap("+pattern+")", e.SetActiveMap(activeMapPattern(pattern, rows, cols), rows, cols))
	}
}

func activeMapPattern(pattern string, rows, cols int) []uint8 {
	out := make([]uint8, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			switch pattern {
			case "all":
				out[r*cols+c] = 1
			case "checker":
				if (r+c)&1 == 0 {
					out[r*cols+c] = 1
				}
			case "left-off":
				if c != 0 {
					out[r*cols+c] = 1
				}
			case "right-off":
				if c != cols-1 {
					out[r*cols+c] = 1
				}
			case "border-off":
				if r != 0 && c != 0 && r != rows-1 && c != cols-1 {
					out[r*cols+c] = 1
				}
			default:
				panic("unknown active-map pattern: " + pattern)
			}
		}
	}
	return out
}

func setReferencePanningApply(ref ReferenceFrame, index int, name string) func(*testing.T, *VP8Encoder) {
	return func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		img := encoderValidationPanningFrame(e.opts.Width, e.opts.Height, index)
		mustRuntime(t, "SetReferenceFrame("+name+")", e.SetReferenceFrame(ref, img))
	}
}

func roiMapApply(pattern string) func(*testing.T, *VP8Encoder) {
	return func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		mustRuntime(t, "SetROIMap("+pattern+")", e.SetROIMap(roiMapPattern(e.opts.Width, e.opts.Height, pattern)))
	}
}

func roiMapPattern(width, height int, pattern string) *ROIMap {
	if pattern == "off" {
		return nil
	}
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	roi := &ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: make([]uint8, rows*cols),
	}
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			var segment uint8
			switch pattern {
			case "checker":
				segment = uint8((r + c) & 1)
			case "left1":
				if c < (cols+1)/2 {
					segment = 1
				}
			case "quadrants":
				if c >= cols/2 {
					segment++
				}
				if r >= rows/2 {
					segment += 2
				}
			case "border1":
				if r == 0 || c == 0 || r == rows-1 || c == cols-1 {
					segment = 1
				}
			default:
				panic("unknown ROI pattern: " + pattern)
			}
			roi.SegmentID[r*cols+c] = segment
		}
	}
	switch pattern {
	case "checker", "left1":
		roi.DeltaQuantizer[1] = -10
		roi.DeltaLoopFilter[1] = -3
	case "quadrants":
		roi.DeltaQuantizer[1] = -8
		roi.DeltaQuantizer[2] = 8
		roi.DeltaLoopFilter[3] = 4
		roi.StaticThreshold[2] = 500
	case "border1":
		roi.DeltaQuantizer[1] = -6
		roi.StaticThreshold[1] = 900
	}
	return roi
}

func quadrantROIMap(width, height int) *ROIMap {
	return roiMapPattern(width, height, "quadrants")
}

func TestRuntimeControlScriptBuilder(t *testing.T) {
	got := strings.Join(runtimeControlScript(4, map[int]string{1: "bitrate:300", 3: "cpu:-3"}), ",")
	if want := "-,bitrate:300,-,cpu:-3"; got != want {
		t.Fatalf("runtimeControlScript = %s, want %s", strconv.Quote(got), strconv.Quote(want))
	}
}
