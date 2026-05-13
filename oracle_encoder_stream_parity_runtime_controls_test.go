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
			name: "bitrate-fps-q-buffer-drop-two-step",
			fx:   panning64,
			opts: baseOpts(panning64),
			// The static prefix matches. Frames that apply vpx_codec_enc_config_set
			// still expose a header/first-partition transition gap, so pin the
			// last known-good frame before the first runtime update.
			matchLimit: 3,
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
			name: "codec-control-surface-toggle",
			fx:   panning64,
			opts: baseOpts(panning64),
			// Runtime codec controls apply before frame 2; the prior prefix
			// must stay byte-identical while transition frames remain logged.
			matchLimit: 2,
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
			name: "cq-level-transition",
			fx:   panning32,
			opts: func() EncoderOptions {
				opts := baseOpts(panning32)
				opts.RateControlMode = RateControlCQ
				opts.CQLevel = 20
				return opts
			}(),
			matchLimit: 4,
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
			// The deadline/config transition itself has the same first-packet
			// header drift as the other runtime config rows; the prefix and
			// post-transition recovery remain pinned by the row.
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
			matchLimit: 3,
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

func TestRuntimeControlScriptBuilder(t *testing.T) {
	got := strings.Join(runtimeControlScript(4, map[int]string{1: "bitrate:300", 3: "cpu:-3"}), ",")
	if want := "-,bitrate:300,-,cpu:-3"; got != want {
		t.Fatalf("runtimeControlScript = %s, want %s", strconv.Quote(got), strconv.Quote(want))
	}
}
