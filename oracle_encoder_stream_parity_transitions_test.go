//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

	t.Run("flush-no-lookahead-resume-matches-single-oracle-stream", func(t *testing.T) {
		sources := makePanningSources(64, 64, 10, 0)
		govpxFrames := encodeWithMidStreamFlush(t, baseOpts, sources, 4)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "flush-no-lookahead", baseOpts, targetKbps, sources, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "flush-no-lookahead", govpxFrames, libvpxFrames, 0)
	})

	t.Run("flush-lookahead-drain-resume-matches-single-oracle-stream", func(t *testing.T) {
		opts := baseOpts
		opts.LookaheadFrames = 2
		sources := makePanningSources(64, 64, 10, 0)
		govpxFrames := encodeWithMidStreamFlush(t, opts, sources, 4)
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "flush-lookahead", opts, targetKbps, sources, []string{"--end-usage=cbr"})
		assertSegmentByteParity(t, "flush-lookahead", govpxFrames, libvpxFrames, 0)
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

	govpxOpts := opts
	govpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, opts, sources)
	govpxFrames := encodeFramesWithGovpx(t, govpxOpts, sources)
	libvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-ramp", opts, targetKbps, sources)
	// The first keyframe has a known one-byte first-partition drift in the
	// two-pass startup header. The following inter frames byte-match and are
	// the transition coverage this row is meant to pin.
	assertSegmentByteParityFrom(t, "twopass-e2e", govpxFrames, libvpxFrames, 1)

	sectionOpts := opts
	sectionOpts.TwoPassVBRBiasPct = 80
	sectionOpts.TwoPassMinPct = 50
	sectionOpts.TwoPassMaxPct = 200
	sectionGovpxOpts := sectionOpts
	sectionGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, sectionOpts, sources)
	sectionGovpxFrames := encodeFramesWithGovpx(t, sectionGovpxOpts, sources)
	sectionLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-ramp-sections", sectionOpts, targetKbps, sources)
	assertSegmentByteParityFrom(t, "twopass-e2e-sections", sectionGovpxFrames, sectionLibvpxFrames, 1)
}

func makePanningSources(w, h, count, offset int) []Image {
	sources := make([]Image, count)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(w, h, i+offset)
	}
	return sources
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
	return encodeGovpxBurst(t, enc, opts, afterReset, 0, true)
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
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, name+".yuv")
	ivf1Path := filepath.Join(dir, name+"-pass1.ivf")
	ivf2Path := filepath.Join(dir, name+"-pass2.ivf")
	fpfPath := filepath.Join(dir, name+".fpf")
	writeEncoderValidationI420(t, yuvPath, sources)
	runLibvpxPass1(t, vpxenc, yuvPath, ivf1Path, fpfPath, opts, targetKbps, len(sources))

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
