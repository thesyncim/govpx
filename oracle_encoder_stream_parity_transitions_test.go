//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestOracleEncoderStreamByteParityResetFlushTransitions pins encoder-lifetime
// transitions that are not represented by one-shot vpxenc invocations:
// Reset must match a cold start after warm state is discarded, and FlushInto
// must not perturb the encoded stream when callers drain between input bursts.
func TestOracleEncoderStreamByteParityResetFlushTransitions(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run reset/flush byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)
	frameFlagsDriver := findVpxencFrameFlags(t)

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
		// Reset clears the warmed state enough to match through the
		// keyframe and first inter packet. The later denoiser/threaded
		// partition path still carries a byte-level gap.
		assertSegmentByteParity(t, "post-reset-nondefault", govpxFrames, libvpxFrames, 2)
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
		// Denoiser + threaded token partitions matches the cold-start
		// keyframe and first inter packet, then exposes the same threaded
		// denoiser packet-writer drift seen by the larger nondefault row.
		assertSegmentByteParity(t, "post-reset-denoiser-threads-token", govpxFrames, libvpxFrames, 2)
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
		// Reset clears warm state and reaches the retained VBR/good/cpu
		// cold-start keyframe exactly. The following VBR inter frames still
		// carry the existing good-quality post-key drift, so pin the strict
		// prefix and keep the transition gap visible in logs.
		assertSegmentByteParity(t, "post-reset-runtime-vbr-good-cpu", govpxFrames, libvpxFrames, 1)
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
		vpxenc := findVpxenc(t)
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
		// Reset reaches the temporal cold-start keyframe exactly; later
		// layer-context packets keep the existing temporal drift visible.
		assertSegmentByteParity(t, "post-reset-temporal-svc", govpxFrames, libvpxFrames, 1)
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

func TestOracleEncoderStreamByteParityTwoPassEndToEnd(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run two-pass stream byte-parity gate")
	}
	vpxenc := findVpxenc(t)
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 32
		height     = 32
		fps        = 30
		targetKbps = 400
		frames     = 8
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = firstPassOracleRampFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  60,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           0,
	}
	encodeTwoPass := func(name string, caseOpts EncoderOptions, caseSources []Image) ([][]byte, [][]byte) {
		t.Helper()
		govpxOpts := caseOpts
		govpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, caseOpts, caseSources)
		govpxFrames := encodeFramesWithGovpx(t, govpxOpts, caseSources)
		libvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, name, caseOpts, caseOpts.TargetBitrateKbps, caseSources)
		return govpxFrames, libvpxFrames
	}

	govpxOpts := opts
	govpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, opts, sources)
	govpxFrames := encodeFramesWithGovpx(t, govpxOpts, sources)
	libvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-ramp", opts, targetKbps, sources)
	// The first keyframe has a known one-byte first-partition drift in the
	// two-pass startup header. The following inter frames byte-match and are
	// the transition coverage this row is meant to pin.
	assertSegmentByteParityFrom(t, "twopass-e2e", govpxFrames, libvpxFrames, 1)

	setterGovpxFrames := encodeFramesWithGovpxTwoPassStatsSetter(t, opts, govpxOpts.TwoPassStats, sources, false)
	assertSegmentByteParity(t, "twopass-e2e-setter-vs-options", setterGovpxFrames, govpxFrames, 0)
	assertSegmentByteParityFrom(t, "twopass-e2e-setter", setterGovpxFrames, libvpxFrames, 1)

	disabledGovpxFrames := encodeFramesWithGovpxTwoPassStatsSetter(t, govpxOpts, nil, sources, true)
	onePassGovpxFrames := encodeFramesWithGovpx(t, opts, sources)
	disabledLibvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "twopass-e2e-disabled-before-frame0", opts, targetKbps, sources, []string{"--end-usage=vbr"})
	assertSegmentByteParity(t, "twopass-e2e-disabled-vs-one-pass-govpx", disabledGovpxFrames, onePassGovpxFrames, 0)
	assertSegmentByteParityFrom(t, "twopass-e2e-disabled-before-frame0", disabledGovpxFrames, disabledLibvpxFrames, 1)

	sectionOpts := opts
	sectionOpts.TwoPassVBRBiasPct = 80
	sectionOpts.TwoPassMinPct = 50
	sectionOpts.TwoPassMaxPct = 200
	sectionGovpxOpts := sectionOpts
	sectionGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, sectionOpts, sources)
	sectionGovpxFrames := encodeFramesWithGovpx(t, sectionGovpxOpts, sources)
	sectionLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-ramp-sections", sectionOpts, targetKbps, sources)
	assertSegmentByteParityFrom(t, "twopass-e2e-sections", sectionGovpxFrames, sectionLibvpxFrames, 1)

	panningSources := makePanningSources(64, 64, frames, 0)
	panningOpts := opts
	panningOpts.Width = 64
	panningOpts.Height = 64
	panningOpts.TargetBitrateKbps = 700
	panningGovpxOpts := panningOpts
	panningGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, panningOpts, panningSources)
	panningGovpxFrames := encodeFramesWithGovpx(t, panningGovpxOpts, panningSources)
	panningLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-panning64", panningOpts, panningOpts.TargetBitrateKbps, panningSources)
	// The panning fixture matches through the keyframe and first two
	// inter frames, then exposes a second-pass content-shape drift.
	assertSegmentByteParity(t, "twopass-e2e-panning64", panningGovpxFrames, panningLibvpxFrames, 3)

	kf4Opts := opts
	kf4Opts.KeyFrameInterval = 4
	kf4GovpxOpts := kf4Opts
	kf4GovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, kf4Opts, sources)
	kf4GovpxFrames := encodeFramesWithGovpx(t, kf4GovpxOpts, sources)
	kf4LibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-kf4", kf4Opts, targetKbps, sources)
	// Short GOP two-pass exposes a periodic keyframe/header drift; keep the
	// row in the matrix so the cadence-specific gap is logged.
	assertSegmentByteParity(t, "twopass-e2e-kf4", kf4GovpxFrames, kf4LibvpxFrames, -1)

	segmentedSources := make([]Image, frames)
	for i := range segmentedSources {
		segmentedSources[i] = encoderValidationSegmentedFrame(64, 64, i)
	}
	segmentedOpts := opts
	segmentedOpts.Width = 64
	segmentedOpts.Height = 64
	segmentedOpts.TargetBitrateKbps = 700
	segmentedGovpxOpts := segmentedOpts
	segmentedGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, segmentedOpts, segmentedSources)
	segmentedGovpxFrames := encodeFramesWithGovpx(t, segmentedGovpxOpts, segmentedSources)
	segmentedLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-segmented64", segmentedOpts, segmentedOpts.TargetBitrateKbps, segmentedSources)
	assertSegmentByteParity(t, "twopass-e2e-segmented64", segmentedGovpxFrames, segmentedLibvpxFrames, 0)

	segmentedDropOpts := segmentedOpts
	segmentedDropOpts.DropFrameAllowed = true
	segmentedDropOpts.DropFrameWaterMark = 60
	segmentedDropGovpxFrames, segmentedDropLibvpxFrames := encodeTwoPass("twopass-e2e-segmented64-drop-frame60", segmentedDropOpts, segmentedSources)
	assertSegmentByteParity(t, "twopass-e2e-segmented64-drop-frame60", segmentedDropGovpxFrames, segmentedDropLibvpxFrames, 0)

	segmentedMaxIntraOpts := segmentedOpts
	segmentedMaxIntraOpts.MaxIntraBitratePct = 500
	segmentedMaxIntraGovpxFrames, segmentedMaxIntraLibvpxFrames := encodeTwoPass("twopass-e2e-segmented64-max-intra-rate500", segmentedMaxIntraOpts, segmentedSources)
	assertSegmentByteParity(t, "twopass-e2e-segmented64-max-intra-rate500", segmentedMaxIntraGovpxFrames, segmentedMaxIntraLibvpxFrames, 0)

	segmentedGFBoostOpts := segmentedOpts
	segmentedGFBoostOpts.GFCBRBoostPct = 500
	segmentedGFBoostGovpxFrames, segmentedGFBoostLibvpxFrames := encodeTwoPass("twopass-e2e-segmented64-gf-cbr-boost500", segmentedGFBoostOpts, segmentedSources)
	assertSegmentByteParity(t, "twopass-e2e-segmented64-gf-cbr-boost500", segmentedGFBoostGovpxFrames, segmentedGFBoostLibvpxFrames, 0)

	segmentedSectionOpts := segmentedOpts
	segmentedSectionOpts.TwoPassVBRBiasPct = 80
	segmentedSectionOpts.TwoPassMinPct = 50
	segmentedSectionOpts.TwoPassMaxPct = 200
	segmentedSectionGovpxOpts := segmentedSectionOpts
	segmentedSectionGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, segmentedSectionOpts, segmentedSources)
	segmentedSectionGovpxFrames := encodeFramesWithGovpx(t, segmentedSectionGovpxOpts, segmentedSources)
	segmentedSectionSetterFrames := encodeFramesWithGovpxTwoPassStatsSetter(t, segmentedSectionOpts, segmentedSectionGovpxOpts.TwoPassStats, segmentedSources, false)
	segmentedSectionLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-segmented64-sections", segmentedSectionOpts, segmentedSectionOpts.TargetBitrateKbps, segmentedSources)
	// Nondefault second-pass section limits now have an explicit
	// bytestream row on a fixture whose default two-pass stream is strict.
	// The keyframe still matches, and setter-vs-options is exact, but the
	// post-key allocation path diverges from the reference stream.
	assertSegmentByteParity(t, "twopass-e2e-segmented64-sections", segmentedSectionGovpxFrames, segmentedSectionLibvpxFrames, 1)
	assertSegmentByteParity(t, "twopass-e2e-segmented64-sections-setter-vs-options", segmentedSectionSetterFrames, segmentedSectionGovpxFrames, 0)
	assertSegmentByteParity(t, "twopass-e2e-segmented64-sections-setter", segmentedSectionSetterFrames, segmentedSectionLibvpxFrames, 1)

	tokenOpts := panningOpts
	tokenOpts.TokenPartitions = 2
	tokenGovpxOpts := tokenOpts
	tokenGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, tokenOpts, panningSources)
	tokenGovpxFrames := encodeFramesWithGovpx(t, tokenGovpxOpts, panningSources)
	tokenLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-token-parts2", tokenOpts, tokenOpts.TargetBitrateKbps, panningSources)
	// Token partitions should not change the two-pass allocation path; this
	// row pins the packet-writer side of second-pass VBR.
	assertSegmentByteParity(t, "twopass-e2e-token-parts2", tokenGovpxFrames, tokenLibvpxFrames, 3)

	erTokenOpts := panningOpts
	erTokenOpts.ErrorResilient = true
	erTokenOpts.ErrorResilientPartitions = true
	erTokenOpts.TokenPartitions = 3
	erTokenGovpxOpts := erTokenOpts
	erTokenGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, erTokenOpts, panningSources)
	erTokenGovpxFrames := encodeFramesWithGovpx(t, erTokenGovpxOpts, panningSources)
	erTokenLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-er3-token-parts3", erTokenOpts, erTokenOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-er3-token-parts3", erTokenGovpxFrames, erTokenLibvpxFrames, 3)

	threadTokenOpts := panningOpts
	threadTokenOpts.Threads = 2
	threadTokenOpts.TokenPartitions = 3
	threadTokenGovpxOpts := threadTokenOpts
	threadTokenGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, threadTokenOpts, panningSources)
	threadTokenGovpxFrames := encodeFramesWithGovpx(t, threadTokenGovpxOpts, panningSources)
	threadTokenLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-threads2-token-parts3-panning64", threadTokenOpts, threadTokenOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-threads2-token-parts3-panning64", threadTokenGovpxFrames, threadTokenLibvpxFrames, 3)

	erThreadTokenOpts := panningOpts
	erThreadTokenOpts.ErrorResilient = true
	erThreadTokenOpts.ErrorResilientPartitions = true
	erThreadTokenOpts.Threads = 2
	erThreadTokenOpts.TokenPartitions = 3
	erThreadTokenGovpxOpts := erThreadTokenOpts
	erThreadTokenGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, erThreadTokenOpts, panningSources)
	erThreadTokenGovpxFrames := encodeFramesWithGovpx(t, erThreadTokenGovpxOpts, panningSources)
	erThreadTokenLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-er3-threads2-token-parts3", erThreadTokenOpts, erThreadTokenOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-er3-threads2-token-parts3", erThreadTokenGovpxFrames, erThreadTokenLibvpxFrames, 3)

	screenStaticOpts := panningOpts
	screenStaticOpts.ScreenContentMode = 2
	screenStaticOpts.StaticThreshold = 500
	screenStaticGovpxOpts := screenStaticOpts
	screenStaticGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, screenStaticOpts, panningSources)
	screenStaticGovpxFrames := encodeFramesWithGovpx(t, screenStaticGovpxOpts, panningSources)
	screenStaticLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-screen-content2-static-thresh500", screenStaticOpts, screenStaticOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-screen-content2-static-thresh500", screenStaticGovpxFrames, screenStaticLibvpxFrames, 3)

	sharpNoiseOpts := panningOpts
	sharpNoiseOpts.Sharpness = 4
	sharpNoiseOpts.NoiseSensitivity = 3
	sharpNoiseGovpxOpts := sharpNoiseOpts
	sharpNoiseGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, sharpNoiseOpts, panningSources)
	sharpNoiseGovpxFrames := encodeFramesWithGovpx(t, sharpNoiseGovpxOpts, panningSources)
	sharpNoiseLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-sharpness4-noise3", sharpNoiseOpts, sharpNoiseOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-sharpness4-noise3", sharpNoiseGovpxFrames, sharpNoiseLibvpxFrames, 1)

	speedOpts := panningOpts
	speedOpts.CpuUsed = -3
	speedGovpxOpts := speedOpts
	speedGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, speedOpts, panningSources)
	speedGovpxFrames := encodeFramesWithGovpx(t, speedGovpxOpts, panningSources)
	speedLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-cpu-3", speedOpts, speedOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-cpu-3", speedGovpxFrames, speedLibvpxFrames, 3)

	ssimOpts := panningOpts
	ssimOpts.Tuning = TuneSSIM
	ssimGovpxOpts := ssimOpts
	ssimGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, ssimOpts, panningSources)
	ssimGovpxFrames := encodeFramesWithGovpx(t, ssimGovpxOpts, panningSources)
	ssimLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-tune-ssim", ssimOpts, ssimOpts.TargetBitrateKbps, panningSources)
	// Two-pass SSIM changes the activity-map/partition decision surface from
	// the keyframe onward. Keep the row in the matrix to log that control.
	assertSegmentByteParity(t, "twopass-e2e-tune-ssim", ssimGovpxFrames, ssimLibvpxFrames, -1)

	arnrSources := makePanningSources(64, 64, 16, 0)
	arnrOpts := panningOpts
	arnrOpts.LookaheadFrames = 8
	arnrOpts.AutoAltRef = true
	arnrOpts.ARNRMaxFrames = 5
	arnrOpts.ARNRStrength = 3
	arnrOpts.ARNRType = 3
	arnrGovpxOpts := arnrOpts
	arnrGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, arnrOpts, arnrSources)
	arnrGovpxFrames := encodeFramesWithGovpx(t, arnrGovpxOpts, arnrSources)
	arnrLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-auto-alt-ref-arnr", arnrOpts, arnrOpts.TargetBitrateKbps, arnrSources)
	// ARNR/hidden-ARF byte parity is an open gap, but keeping this in the
	// two-pass stream matrix catches frame-count and packet-shape regressions
	// while asserting the common keyframe and logging the later divergence.
	assertSegmentByteParity(t, "twopass-e2e-auto-alt-ref-arnr", arnrGovpxFrames, arnrLibvpxFrames, 1)
}

func makePanningSources(w, h, count, offset int) []Image {
	sources := make([]Image, count)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(w, h, i+offset)
	}
	return sources
}

func encodeFramesWithGovpxTwoPassStatsSetter(t *testing.T, opts EncoderOptions, stats []FirstPassFrameStats, sources []Image, disable bool) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	if disable {
		stats = nil
	}
	if err := enc.SetTwoPassStats(stats); err != nil {
		t.Fatalf("SetTwoPassStats: %v", err)
	}
	out := encodeGovpxBurst(t, enc, opts, sources, 0, true)
	out = append(out, drainGovpxFlush(t, enc, opts, "SetTwoPassStats FlushInto")...)
	return out
}

func encodePostResetWithGovpx(t *testing.T, opts EncoderOptions, warm []Image, afterReset []Image) [][]byte {
	t.Helper()
	return encodePostResetWithGovpxMutations(t, opts, warm, afterReset, nil, nil)
}

func encodePostResetWithGovpxMutations(t *testing.T, opts EncoderOptions, warm []Image, afterReset []Image, beforeWarm func(*testing.T, *VP8Encoder), afterWarm func(*testing.T, *VP8Encoder)) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	if beforeWarm != nil {
		beforeWarm(t, enc)
	}
	for i, src := range warm {
		if _, err := enc.EncodeInto(buf, src, uint64(i), 1, 0); err != nil && !errors.Is(err, ErrFrameNotReady) {
			t.Fatalf("warm EncodeInto frame %d: %v", i, err)
		}
	}
	if afterWarm != nil {
		afterWarm(t, enc)
	}
	enc.Reset()
	out := encodeGovpxBurst(t, enc, opts, afterReset, 0, true)
	out = append(out, drainGovpxFlush(t, enc, opts, "post-reset FlushInto")...)
	return out
}

func encodePostResizeResetWithGovpx(t *testing.T, initOpts EncoderOptions, warm []Image, newOpts EncoderOptions, afterReset []Image) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(initOpts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	_ = encodeGovpxBurst(t, enc, initOpts, warm, 0, true)
	if err := enc.SetRealtimeTarget(RealtimeTarget{Width: newOpts.Width, Height: newOpts.Height}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	enc.Reset()
	return encodeGovpxBurst(t, enc, newOpts, afterReset, 0, true)
}

func encodeWithMidStreamFlush(t *testing.T, opts EncoderOptions, sources []Image, split int) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	out := encodeGovpxBurst(t, enc, opts, sources[:split], 0, true)
	out = append(out, drainGovpxFlush(t, enc, opts, "mid FlushInto")...)
	out = append(out, encodeGovpxBurst(t, enc, opts, sources[split:], uint64(split), true)...)
	out = append(out, drainGovpxFlush(t, enc, opts, "final FlushInto")...)
	return out
}

func encodeWithMidStreamFlushRuntimeControls(t *testing.T, opts EncoderOptions, sources []Image, split int, before func(*testing.T, *VP8Encoder), apply map[int]func(*testing.T, *VP8Encoder), flags []EncodeFlags) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	if before != nil {
		before(t, enc)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		if i == split {
			out = append(out, drainGovpxFlush(t, enc, opts, "mid FlushInto")...)
		}
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
	out = append(out, drainGovpxFlush(t, enc, opts, "final FlushInto")...)
	return out
}

func encodeGovpxBurst(t *testing.T, enc *VP8Encoder, opts EncoderOptions, sources []Image, ptsBase uint64, includeDrops bool) [][]byte {
	t.Helper()
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		result, err := enc.EncodeInto(buf, src, ptsBase+uint64(i), 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped && !includeDrops {
			t.Fatalf("frame %d dropped, want full stream", i)
		}
		if !result.Dropped {
			out = append(out, append([]byte(nil), result.Data...))
		}
	}
	return out
}

func drainGovpxFlush(t *testing.T, enc *VP8Encoder, opts EncoderOptions, label string) [][]byte {
	t.Helper()
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	var out [][]byte
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if !result.Dropped {
			out = append(out, append([]byte(nil), result.Data...))
		}
	}
	return out
}

func assertSegmentByteParityFrom(t *testing.T, label string, got [][]byte, want [][]byte, start int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s frame count mismatch: got=%d want=%d", label, len(got), len(want))
	}
	for i := range got {
		gFP, gKey := parseVP8FramePartitionSizes(got[i])
		wFP, wKey := parseVP8FramePartitionSizes(want[i])
		if bytes.Equal(got[i], want[i]) {
			t.Logf("%s frame %d byte MATCH: len=%d first_part=%d keyframe=%t", label, i, len(got[i]), gFP, gKey)
			continue
		}
		firstDiff := firstByteDiff(got[i], want[i])
		if i < start {
			t.Logf("%s frame %d byte mismatch (not asserted, start=%d): got_len=%d want_len=%d first_diff=%d got_first_part=%d want_first_part=%d got_keyframe=%t want_keyframe=%t",
				label, i, start, len(got[i]), len(want[i]), firstDiff, gFP, wFP, gKey, wKey)
			continue
		}
		t.Errorf("%s frame %d byte mismatch: got_len=%d want_len=%d first_diff=%d got_first_part=%d want_first_part=%d got_keyframe=%t want_keyframe=%t",
			label, i, len(got[i]), len(want[i]), firstDiff, gFP, wFP, gKey, wKey)
	}
}

func encodeFramesWithLibvpxTwoPassOracle(t *testing.T, vpxenc string, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image) [][]byte {
	t.Helper()
	return encodeFramesWithLibvpxTwoPassOracleArgs(t, vpxenc, vpxencOracle, name, opts, targetKbps, sources, nil)
}

func encodeFramesWithLibvpxTwoPassOracleArgs(t *testing.T, vpxenc string, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string) [][]byte {
	t.Helper()
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, name+".yuv")
	ivf1Path := filepath.Join(dir, name+"-pass1.ivf")
	ivf2Path := filepath.Join(dir, name+"-pass2.ivf")
	fpfPath := filepath.Join(dir, name+".fpf")
	writeEncoderValidationI420(t, yuvPath, sources)
	passExtraArgs := libvpxTwoPassControlArgs(opts)
	passExtraArgs = append(passExtraArgs, extraArgs...)
	runLibvpxPass1WithExtra(t, vpxenc, yuvPath, ivf1Path, fpfPath, opts, targetKbps, len(sources), passExtraArgs)

	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		libvpxDeadlineArg(opts.Deadline),
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--passes=2",
		"--pass=2",
		"--fpf=" + fpfPath,
		"--end-usage=vbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--kf-min-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--kf-max-dist=" + strconv.Itoa(opts.KeyFrameInterval),
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivf2Path,
	}
	if opts.TwoPassVBRBiasPct > 0 {
		args = append(args, "--bias-pct="+strconv.Itoa(opts.TwoPassVBRBiasPct))
	}
	if opts.TwoPassMinPct > 0 {
		args = append(args, "--minsection-pct="+strconv.Itoa(opts.TwoPassMinPct))
	}
	if opts.TwoPassMaxPct > 0 {
		args = append(args, "--maxsection-pct="+strconv.Itoa(opts.TwoPassMaxPct))
	}
	args = append(args, passExtraArgs...)
	args = append(args, yuvPath)
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vpxenc-oracle two-pass pass2 failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ivf2Path)
	if err != nil {
		t.Fatalf("read %s: %v", ivf2Path, err)
	}
	return parseIVFFramePayloads(t, data)
}

func libvpxTwoPassControlArgs(opts EncoderOptions) []string {
	var args []string
	if opts.Threads > 0 {
		args = append(args, "--threads="+strconv.Itoa(opts.Threads))
	}
	if opts.LookaheadFrames > 0 {
		args = append(args, "--lag-in-frames="+strconv.Itoa(opts.LookaheadFrames))
	}
	if opts.ErrorResilient {
		value := "1"
		if opts.ErrorResilientPartitions {
			value = "3"
		}
		args = append(args, "--error-resilient="+value)
	}
	if opts.AutoAltRef {
		args = append(args, "--auto-alt-ref=1")
	}
	if opts.TokenPartitions > 0 {
		args = append(args, "--token-parts="+strconv.Itoa(opts.TokenPartitions))
	}
	if opts.Tuning == TuneSSIM {
		args = append(args, "--tune=ssim")
	}
	if opts.Sharpness > 0 {
		args = append(args, "--sharpness="+strconv.Itoa(opts.Sharpness))
	}
	if opts.NoiseSensitivity > 0 {
		args = append(args, "--noise-sensitivity="+strconv.Itoa(opts.NoiseSensitivity))
	}
	if opts.ScreenContentMode > 0 {
		args = append(args, "--screen-content-mode="+strconv.Itoa(opts.ScreenContentMode))
	}
	if opts.StaticThreshold > 0 {
		args = append(args, "--static-thresh="+strconv.Itoa(opts.StaticThreshold))
	}
	if opts.DropFrameAllowed {
		watermark := opts.DropFrameWaterMark
		if watermark <= 0 {
			watermark = defaultDropFramesWaterMark
		}
		args = append(args, "--drop-frame="+strconv.Itoa(min(watermark, 100)))
	}
	if opts.MaxIntraBitratePct > 0 {
		args = append(args, "--max-intra-rate="+strconv.Itoa(opts.MaxIntraBitratePct))
	}
	if opts.GFCBRBoostPct > 0 {
		args = append(args, "--gf-cbr-boost="+strconv.Itoa(opts.GFCBRBoostPct))
	}
	if opts.ARNRMaxFrames > 0 {
		args = append(args, "--arnr-maxframes="+strconv.Itoa(opts.ARNRMaxFrames))
		args = append(args, "--arnr-strength="+strconv.Itoa(opts.ARNRStrength))
		args = append(args, "--arnr-type="+strconv.Itoa(opts.ARNRType))
	}
	return args
}
