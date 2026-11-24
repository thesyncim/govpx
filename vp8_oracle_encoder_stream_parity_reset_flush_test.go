//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"strings"
	"testing"
)

// TestVP8OracleEncoderStreamByteParityResetFlushTransitions pins encoder-lifetime
// transitions that are not represented by one-shot vpxenc invocations:
// Reset must match a cold start after warm state is discarded, and FlushInto
// must not perturb the encoded stream when callers drain between input bursts.
func TestVP8OracleEncoderStreamByteParityResetFlushTransitions(t *testing.T) {
	vp8test.RequireOracle(t, "reset/flush byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)
	frameFlagsDriver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
	)
	baseOpts := EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
	}

	t.Run("reset-after-warmup-matches-cold-start", func(t *testing.T) {
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpx(t, baseOpts, warm, afterReset)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-warmup", baseOpts, targetKbps, afterReset, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "post-reset", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-denoiser-threads-token-ssim-matches-cold-start", func(t *testing.T) {
		opts := baseOpts
		opts.NoiseSensitivity = 3
		opts.Threads = 2
		opts.TokenPartitions = 2
		opts.Tuning = TuneSSIM
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpx(t, opts, warm, afterReset)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-nondefault-warmup", opts, targetKbps, afterReset, []string{"--end-usage=cbr", "--noise-sensitivity=3", "--threads=2", "--token-parts=2", "--tune=ssim"})
		assertSegmentByteParity(t, "post-reset-nondefault", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-denoiser-matches-cold-start", func(t *testing.T) {
		opts := baseOpts
		opts.NoiseSensitivity = 3
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpx(t, opts, warm, afterReset)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-denoiser", opts, targetKbps, afterReset, []string{"--end-usage=cbr", "--noise-sensitivity=3"})
		assertSegmentByteParity(t, "post-reset-denoiser", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-threads-token-matches-cold-start", func(t *testing.T) {
		opts := baseOpts
		opts.Threads = 2
		opts.TokenPartitions = 2
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpx(t, opts, warm, afterReset)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-threads-token", opts, targetKbps, afterReset, []string{"--end-usage=cbr", "--threads=2", "--token-parts=2"})
		assertSegmentByteParity(t, "post-reset-threads-token", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-tune-ssim-matches-cold-start", func(t *testing.T) {
		opts := baseOpts
		opts.Tuning = TuneSSIM
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpx(t, opts, warm, afterReset)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-tune-ssim", opts, targetKbps, afterReset, []string{"--end-usage=cbr", "--tune=ssim"})
		assertSegmentByteParity(t, "post-reset-tune-ssim", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-denoiser-threads-token-matches-cold-start", func(t *testing.T) {
		opts := baseOpts
		opts.NoiseSensitivity = 3
		opts.Threads = 2
		opts.TokenPartitions = 2
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpx(t, opts, warm, afterReset)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-denoiser-threads-token", opts, targetKbps, afterReset, []string{"--end-usage=cbr", "--noise-sensitivity=3", "--threads=2", "--token-parts=2"})
		assertSegmentByteParity(t, "post-reset-denoiser-threads-token", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-denoiser-ssim-matches-cold-start", func(t *testing.T) {
		opts := baseOpts
		opts.NoiseSensitivity = 3
		opts.Tuning = TuneSSIM
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpx(t, opts, warm, afterReset)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-denoiser-ssim", opts, targetKbps, afterReset, []string{"--end-usage=cbr", "--noise-sensitivity=3", "--tune=ssim"})
		assertSegmentByteParity(t, "post-reset-denoiser-ssim", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-threads-token-ssim-matches-cold-start", func(t *testing.T) {
		opts := baseOpts
		opts.Threads = 2
		opts.TokenPartitions = 2
		opts.Tuning = TuneSSIM
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpx(t, opts, warm, afterReset)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-threads-token-ssim", opts, targetKbps, afterReset, []string{"--end-usage=cbr", "--threads=2", "--token-parts=2", "--tune=ssim"})
		assertSegmentByteParity(t, "post-reset-threads-token-ssim", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-runtime-vbr-good-cpu-mutations-matches-cold-start", func(t *testing.T) {
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpxMutations(t, baseOpts, warm, afterReset, nil,
			func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(VBR)", e.SetRateControl(RateControlConfig{
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
				mustRuntime(t, "SetDeadline(good)", e.SetDeadline(DeadlineGoodQuality))
				mustRuntime(t, "SetCPUUsed(4)", e.SetCPUUsed(4))
			})
		coldOpts := baseOpts
		coldOpts.RateControlMode = RateControlVBR
		coldOpts.UndershootPct = 50
		coldOpts.OvershootPct = 50
		coldOpts.BufferSizeMs = 6000
		coldOpts.BufferInitialSizeMs = 4000
		coldOpts.BufferOptimalSizeMs = 5000
		coldOpts.Deadline = DeadlineGoodQuality
		coldOpts.CpuUsed = 4
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-runtime-vbr-good-cpu", coldOpts, targetKbps, afterReset, []string{
			"--end-usage=vbr",
			"--undershoot-pct=50",
			"--overshoot-pct=50",
			"--buf-sz=6000",
			"--buf-initial-sz=4000",
			"--buf-optimal-sz=5000",
		})
		assertSegmentByteParity(t, "post-reset-runtime-vbr-good-cpu", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-runtime-cq-arnr-mutations-matches-cold-start", func(t *testing.T) {
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpxMutations(t, baseOpts, warm, afterReset, nil,
			func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(CQ)", e.SetRateControl(RateControlConfig{
					Mode:                RateControlCQ,
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
				mustRuntime(t, "SetARNR", e.SetARNR(7, 6, 3))
			})
		coldOpts := baseOpts
		coldOpts.RateControlMode = RateControlCQ
		coldOpts.CQLevel = 20
		coldOpts.UndershootPct = 100
		coldOpts.OvershootPct = 100
		coldOpts.BufferSizeMs = 6000
		coldOpts.BufferInitialSizeMs = 4000
		coldOpts.BufferOptimalSizeMs = 5000
		coldOpts.ARNRMaxFrames = 7
		coldOpts.ARNRStrength = 6
		coldOpts.ARNRType = 3
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-runtime-cq-arnr", coldOpts, targetKbps, afterReset, []string{
			"--end-usage=cq",
			"--cq-level=20",
			"--undershoot-pct=100",
			"--overshoot-pct=100",
			"--buf-sz=6000",
			"--buf-initial-sz=4000",
			"--buf-optimal-sz=5000",
			"--arnr-maxframes=7",
			"--arnr-strength=6",
			"--arnr-type=3",
		})
		assertSegmentByteParity(t, "post-reset-runtime-cq-arnr", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-active-map-matches-cold-start", func(t *testing.T) {
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpxMutations(t, baseOpts, warm, afterReset,
			func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				rows := encoderMacroblockRows(e.opts.Height)
				cols := encoderMacroblockCols(e.opts.Width)
				mustRuntime(t, "SetActiveMap(checker)", e.SetActiveMap(activeMapPattern("checker", rows, cols), rows, cols))
			}, nil)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-active-map", baseOpts, targetKbps, afterReset, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "post-reset-active-map", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-roi-map-matches-cold-start", func(t *testing.T) {
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpxMutations(t, baseOpts, warm, afterReset,
			func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetROIMap(quadrants)", e.SetROIMap(quadrantROIMap(e.opts.Width, e.opts.Height)))
			}, nil)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-roi-map", baseOpts, targetKbps, afterReset, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "post-reset-roi-map", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-twopass-warmup-matches-cold-second-pass", func(t *testing.T) {
		vpxenc := vp8test.Vpxenc(t)
		const frames = 8
		warm := make([]Image, 4)
		for i := range warm {
			warm[i] = encoderValidationSegmentedFrame(64, 64, i)
		}
		afterReset := make([]Image, frames)
		for i := range afterReset {
			afterReset[i] = encoderValidationSegmentedFrame(64, 64, i+len(warm))
		}
		twoPassOpts := EncoderOptions{
			Width:             64,
			Height:            64,
			FPS:               fps,
			RateControlMode:   RateControlVBR,
			TargetBitrateKbps: targetKbps,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			KeyFrameInterval:  60,
			Deadline:          DeadlineGoodQuality,
			CpuUsed:           0,
		}
		govpxOpts := twoPassOpts
		govpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, twoPassOpts, afterReset)
		govpxFrames := encodePostResetWithGovpx(t, govpxOpts, warm, afterReset)
		libvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "reset-after-twopass-warmup", twoPassOpts, targetKbps, afterReset)
		assertSegmentByteParity(t, "post-reset-twopass-warmup", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-set-reference-matches-cold-start", func(t *testing.T) {
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpxMutations(t, baseOpts, warm, afterReset, nil,
			func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				ref := encoderValidationPanningFrame(e.opts.Width, e.opts.Height, 12)
				mustRuntime(t, "SetReferenceFrame(last)", e.SetReferenceFrame(ReferenceLast, ref))
			})
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-set-reference", baseOpts, targetKbps, afterReset, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "post-reset-set-reference", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-rtc-external-matches-cold-start-with-rtc", func(t *testing.T) {
		opts := baseOpts
		opts.TargetBitrateKbps = 400
		opts.BufferSizeMs = 200
		opts.BufferInitialSizeMs = 100
		opts.BufferOptimalSizeMs = 150
		opts.DropFrameAllowed = true
		opts.DropFrameWaterMark = 50
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpxMutations(t, opts, warm, afterReset,
			func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
			}, nil)
		coldOpts := opts
		coldOpts.RTCExternalRateControl = true
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "reset-after-rtc-external", coldOpts, coldOpts.TargetBitrateKbps, afterReset, nil, []string{
			"--end-usage=cbr",
			"--buf-sz=200",
			"--buf-initial-sz=100",
			"--buf-optimal-sz=150",
			"--drop-frame=50",
			"--rtc-external=1",
		})
		assertSegmentByteParity(t, "post-reset-rtc-external", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-active-roi-rtc-matches-cold-start-with-rtc", func(t *testing.T) {
		opts := baseOpts
		opts.TargetBitrateKbps = 400
		opts.BufferSizeMs = 200
		opts.BufferInitialSizeMs = 100
		opts.BufferOptimalSizeMs = 150
		opts.DropFrameAllowed = true
		opts.DropFrameWaterMark = 50
		warm := make([]Image, 6)
		for i := range warm {
			warm[i] = encoderValidationSegmentedFrame(64, 64, i)
		}
		afterReset := make([]Image, 8)
		for i := range afterReset {
			afterReset[i] = encoderValidationSegmentedFrame(64, 64, i+len(warm))
		}
		govpxFrames := encodePostResetWithGovpxMutations(t, opts, warm, afterReset,
			func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				activeMapApply("checker")(t, e)
				roiMapApply("border1")(t, e)
			},
			func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
			})
		coldOpts := opts
		coldOpts.RTCExternalRateControl = true
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "reset-after-active-roi-rtc", coldOpts, coldOpts.TargetBitrateKbps, afterReset, nil, []string{
			"--end-usage=cbr",
			"--buf-sz=200",
			"--buf-initial-sz=100",
			"--buf-optimal-sz=150",
			"--drop-frame=50",
			"--rtc-external=1",
		})
		assertSegmentByteParity(t, "post-reset-active-roi-rtc", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-temporal-svc-matches-cold-start", func(t *testing.T) {
		cfg := TemporalScalabilityConfig{
			Enabled:                true,
			Mode:                   TemporalLayeringTwoLayers,
			LayerTargetBitrateKbps: [MaxTemporalLayers]int{420, targetKbps},
		}
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpxMutations(t, baseOpts, warm, afterReset,
			func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetTemporalScalability(two-layer)", e.SetTemporalScalability(cfg))
			}, nil)
		coldOpts := baseOpts
		coldOpts.TemporalScalability = cfg
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "reset-after-temporal-svc", coldOpts, targetKbps, afterReset, temporalTwoLayerFlags(len(afterReset)), []string{
			"--temporal-layers=2",
			"--temporal-bitrates=420,700",
			"--temporal-decimators=2,1",
			"--temporal-periodicity=2",
			"--temporal-layer-ids=0,1",
		})
		assertSegmentByteParity(t, "post-reset-temporal-svc", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-lookahead4-auto-alt-ref-matches-cold-start", func(t *testing.T) {
		opts := baseOpts
		opts.LookaheadFrames = 4
		opts.AutoAltRef = true
		warm := makePanningSources(64, 64, 8, 0)
		afterReset := makePanningSources(64, 64, 10, 8)
		govpxFrames := encodePostResetWithGovpx(t, opts, warm, afterReset)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-lookahead4-auto-alt-ref", opts, targetKbps, afterReset, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "post-reset-lookahead4-auto-alt-ref", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-resize-matches-cold-start-at-new-size", func(t *testing.T) {
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(96, 96, 8, 6)
		newOpts := baseOpts
		newOpts.Width = 96
		newOpts.Height = 96
		govpxFrames := encodePostResizeResetWithGovpx(t, baseOpts, warm, newOpts, afterReset)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-resize-96x96", newOpts, targetKbps, afterReset, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "post-reset-resize-96x96", govpxFrames, libvpxFrames, 0)
	})

	t.Run("reset-after-pending-force-key-clears-request", func(t *testing.T) {
		warm := makePanningSources(64, 64, 6, 0)
		afterReset := makePanningSources(64, 64, 8, 6)
		govpxFrames := encodePostResetWithGovpxMutations(t, baseOpts, warm, afterReset, nil,
			func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				e.ForceKeyFrame()
			})
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "reset-after-pending-force-key", baseOpts, targetKbps, afterReset, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "post-reset-pending-force-key", govpxFrames, libvpxFrames, 0)
	})

	t.Run("flush-no-lookahead-resume-matches-single-oracle-stream", func(t *testing.T) {
		sources := makePanningSources(64, 64, 10, 0)
		govpxFrames := encodeWithMidStreamFlush(t, baseOpts, sources, 4)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "flush-no-lookahead", baseOpts, targetKbps, sources, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "flush-no-lookahead", govpxFrames, libvpxFrames, 0)
	})

	for _, tc := range []struct {
		name      string
		opts      EncoderOptions
		extraArgs []string
		limit     int
	}{
		{
			name: "flush-cq-mode-resume",
			opts: func() EncoderOptions {
				opts := baseOpts
				opts.RateControlMode = RateControlCQ
				opts.CQLevel = 20
				return opts
			}(),
			extraArgs: []string{"--end-usage=cq", "--cq-level=20"},
		},
		{
			name: "flush-q-mode-resume",
			opts: func() EncoderOptions {
				opts := baseOpts
				opts.RateControlMode = RateControlQ
				opts.CQLevel = 20
				return opts
			}(),
			extraArgs: []string{"--end-usage=q", "--cq-level=20"},
		},
		{
			name: "flush-vbr-good-resume",
			opts: func() EncoderOptions {
				opts := baseOpts
				opts.RateControlMode = RateControlVBR
				opts.Deadline = DeadlineGoodQuality
				opts.CpuUsed = 0
				return opts
			}(),
			extraArgs: []string{"--end-usage=vbr"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sources := makePanningSources(tc.opts.Width, tc.opts.Height, 10, 0)
			govpxFrames := encodeWithMidStreamFlush(t, tc.opts, sources, 4)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources, tc.extraArgs)
			assertSegmentByteParity(t, tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}

	t.Run("flush-lookahead-drain-resume-matches-single-oracle-stream", func(t *testing.T) {
		opts := baseOpts
		opts.LookaheadFrames = 2
		sources := makePanningSources(64, 64, 10, 0)
		govpxFrames := encodeWithMidStreamFlush(t, opts, sources, 4)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "flush-lookahead", opts, targetKbps, sources, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "flush-lookahead", govpxFrames, libvpxFrames, 0)
	})

	t.Run("flush-lookahead-denoiser-threads-token-resume-matches-single-oracle-stream", func(t *testing.T) {
		opts := baseOpts
		opts.LookaheadFrames = 2
		opts.NoiseSensitivity = 3
		opts.Threads = 2
		opts.TokenPartitions = 2
		sources := makePanningSources(64, 64, 10, 0)
		govpxFrames := encodeWithMidStreamFlush(t, opts, sources, 4)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "flush-lookahead-denoiser-threads-token", opts, targetKbps, sources, []string{"--end-usage=cbr", "--noise-sensitivity=3", "--threads=2", "--token-parts=2"})
		assertSegmentByteParity(t, "flush-lookahead-denoiser-threads-token", govpxFrames, libvpxFrames, 0)
	})

	t.Run("flush-lookahead4-auto-alt-ref-resume-matches-single-oracle-stream", func(t *testing.T) {
		opts := baseOpts
		opts.LookaheadFrames = 4
		opts.AutoAltRef = true
		sources := makePanningSources(64, 64, 12, 0)
		govpxFrames := encodeWithMidStreamFlush(t, opts, sources, 5)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "flush-lookahead4-auto-alt-ref", opts, targetKbps, sources, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "flush-lookahead4-auto-alt-ref", govpxFrames, libvpxFrames, 0)
	})

	t.Run("flush-active-map-resume", func(t *testing.T) {
		sources := makePanningSources(64, 64, 10, 0)
		govpxFrames := encodeWithMidStreamFlushRuntimeControls(t, baseOpts, sources, 4, nil,
			map[int]func(*testing.T, *VP8Encoder){0: activeMapApply("checker")}, nil)
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "flush-active-map", baseOpts, targetKbps, sources, nil, []string{"--active-map=checker"})
		assertSegmentByteParity(t, "flush-active-map", govpxFrames, libvpxFrames, 0)
	})

	t.Run("flush-roi-map-resume", func(t *testing.T) {
		sources := makePanningSources(64, 64, 10, 0)
		govpxFrames := encodeWithMidStreamFlushRuntimeControls(t, baseOpts, sources, 4, nil,
			map[int]func(*testing.T, *VP8Encoder){0: roiMapApply("checker")}, nil)
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "flush-roi-map", baseOpts, targetKbps, sources, nil, []string{"--roi-map=checker"})
		assertSegmentByteParity(t, "flush-roi-map", govpxFrames, libvpxFrames, 0)
	})

	t.Run("flush-active-roi-resume", func(t *testing.T) {
		sources := make([]Image, 10)
		for i := range sources {
			sources[i] = encoderValidationSegmentedFrame(64, 64, i)
		}
		govpxFrames := encodeWithMidStreamFlushRuntimeControls(t, baseOpts, sources, 4, nil,
			map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					activeMapApply("checker")(t, e)
					roiMapApply("border1")(t, e)
				},
			}, nil)
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "flush-active-roi", baseOpts, targetKbps, sources, nil, []string{"--active-map=checker", "--roi-map=border1"})
		assertSegmentByteParity(t, "flush-active-roi", govpxFrames, libvpxFrames, 0)
	})

	t.Run("flush-temporal-two-layer-resume", func(t *testing.T) {
		opts := baseOpts
		opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)
		sources := makePanningSources(64, 64, 10, 0)
		flags := temporalScalabilityReconfigureFlags(len(sources), TemporalLayeringTwoLayers, 0)
		govpxFrames := encodeWithMidStreamFlushRuntimeControls(t, opts, sources, 4, nil, nil, flags)
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "flush-temporal-two-layer", opts, targetKbps, sources, flags, runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps))
		assertSegmentByteParity(t, "flush-temporal-two-layer", govpxFrames, libvpxFrames, 0)
	})

	t.Run("flush-set-reference-resume", func(t *testing.T) {
		sources := makePanningSources(64, 64, 10, 0)
		flags := []EncodeFlags{
			0,
			EncodeNoReferenceGolden | EncodeNoReferenceAltRef,
		}
		apply := map[int]func(*testing.T, *VP8Encoder){
			1: setReferencePanningApply(ReferenceLast, 8, "last"),
		}
		script := runtimeControlScript(len(sources), map[int]string{
			1: "setref:last:panning:8",
		})
		govpxFrames := encodeWithMidStreamFlushRuntimeControls(t, baseOpts, sources, 4, nil, apply, flags)
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "flush-set-reference", baseOpts, targetKbps, sources, flags, []string{
			"--control-script=" + strings.Join(script, ","),
		})
		assertSegmentByteParity(t, "flush-set-reference", govpxFrames, libvpxFrames, 0)
	})

	t.Run("flush-tight-buffer-drop-resume", func(t *testing.T) {
		opts := baseOpts
		opts.TargetBitrateKbps = 50
		opts.BufferSizeMs = 200
		opts.BufferInitialSizeMs = 100
		opts.BufferOptimalSizeMs = 150
		opts.DropFrameAllowed = true
		opts.DropFrameWaterMark = 60
		sources := makePanningSources(64, 64, 18, 0)
		govpxFrames := encodeWithMidStreamFlush(t, opts, sources, 7)
		libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "flush-tight-buffer-drop", opts, opts.TargetBitrateKbps, sources, nil, []string{
			"--target-bitrate=50",
			"--buf-sz=200",
			"--buf-initial-sz=100",
			"--buf-optimal-sz=150",
			"--drop-frame=60",
		})
		assertSegmentByteParity(t, "flush-tight-buffer-drop", govpxFrames, libvpxFrames, 0)
	})
}
