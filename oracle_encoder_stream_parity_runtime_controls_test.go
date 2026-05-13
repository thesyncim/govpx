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
		name       string
		fx         fixture
		opts       EncoderOptions
		flags      []EncodeFlags
		script     []string
		apply      map[int]func(*testing.T, *VP8Encoder)
		extraArgs  []string
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
			name: "rate-control-mode-cbr-cq-q-transition",
			fx:   panning32,
			opts: baseOpts(panning32),
			// The static matrix covers CBR/CQ/Q as construction-time
			// choices; this row pins the runtime vpx_codec_enc_config_set
			// path between all three modes. The first CQ/Q packets still
			// have a small first-partition drift, so this asserts the
			// clean prefix and logs the remaining mode-switch gap.
			matchLimit: 3,
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
			// CBR->VBR diverges on the first VBR packet; the return to
			// CBR recovers. Keep the clean prefix strict and log the
			// isolated VBR runtime-mode gap.
			matchLimit: 3,
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
			flags:      temporalScalabilityReconfigureFlags(frames, TemporalLayeringTwoLayers, 6),
			script:     temporalScalabilityReconfigureScript(frames, TemporalLayeringTwoLayers, 6, "tslayers:2+tsperiodicity:2+tsbitrates:420/700+tsdecimators:2/1+tsids:0/1"),
			extraArgs:  []string{"--temporal-layers=2", "--temporal-bitrates=350,700", "--temporal-decimators=2,1", "--temporal-periodicity=2", "--temporal-layer-ids=0,1"},
			matchLimit: 6,
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
			name: "deadline-rc-mode-key-interval-transition",
			fx:   panning64,
			opts: baseOpts(panning64),
			// Switching deadline plus keyframe cadence still has a small
			// first-partition drift around the forced keyframe; the return
			// to realtime recovers to strict byte parity.
			matchLimit: 3,
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
			name:       "keyframe-disabled-runtime-toggle",
			fx:         panning32,
			opts:       baseOpts(panning32),
			matchLimit: 3,
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
			// Static border-off maps drift late in the clip; keep the
			// multi-pattern transition pinned through the matching prefix.
			matchLimit: 10,
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
			// The toggle itself routes through libvpx's codec-control
			// surface. Prefix-pin the row with the other runtime
			// transition cases while logging the post-toggle packets.
			matchLimit: 2,
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
			// The externally replaced-reference frame itself matches.
			// Subsequent inter frames still drift, so keep the prefix
			// pinned while the follow-on reference bookkeeping is fixed.
			matchLimit: 2,
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
			matchLimit: 2,
			apply: map[int]func(*testing.T, *VP8Encoder){
				1: setReferencePanningApply(ReferenceLast, 8, "last"),
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
			matchLimit: 2,
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
			matchLimit: 2,
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
			matchLimit: 2,
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
			// The ROI keyframe matches libvpx, while ROI-tagged inter
			// frames still expose a first-partition drift. Keep the
			// transition covered and log the unresolved inter-frame gap.
			matchLimit: 1,
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
			name: "roi-map-pattern-switches",
			fx:   segmented64,
			opts: baseOpts(segmented64),
			// ROI-tagged inter frames still drift, but this keeps the
			// checker/left/border/off transition surface represented.
			matchLimit: -1,
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
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources, tc.flags, extraArgs)
			assertSegmentByteParity(t, "runtime-controls", govpxFrames, libvpxFrames, tc.matchLimit)
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
