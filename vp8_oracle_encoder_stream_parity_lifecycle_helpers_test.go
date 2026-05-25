//go:build govpx_oracle_trace

package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"strconv"
	"testing"
)

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

func encodeFramesWithLibvpxTwoPassOracle(t *testing.T, vpxenc string, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image) [][]byte {
	t.Helper()
	return encodeFramesWithLibvpxTwoPassOracleArgs(t, vpxenc, vpxencOracle, name, opts, targetKbps, sources, nil)
}

func encodeFramesWithLibvpxTwoPassOracleArgs(t *testing.T, vpxenc string, vpxencOracle string, _ string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string) [][]byte {
	t.Helper()
	passExtraArgs := libvpxTwoPassControlArgs(opts)
	passExtraArgs = append(passExtraArgs, extraArgs...)
	sectionArgs := []string{}
	if opts.TwoPassVBRBiasPct > 0 {
		sectionArgs = append(sectionArgs, "--bias-pct="+strconv.Itoa(opts.TwoPassVBRBiasPct))
	}
	if opts.TwoPassMinPct > 0 {
		sectionArgs = append(sectionArgs, "--minsection-pct="+strconv.Itoa(opts.TwoPassMinPct))
	}
	if opts.TwoPassMaxPct > 0 {
		sectionArgs = append(sectionArgs, "--maxsection-pct="+strconv.Itoa(opts.TwoPassMaxPct))
	}

	_, data, diag, err := vp8test.VpxencVP8TwoPassEncodeI420(
		encoderValidationI420Bytes(t, sources),
		vp8test.VpxencVP8TwoPassConfig{
			FirstPassBinaryPath:  vpxenc,
			SecondPassBinaryPath: vpxencOracle,
			Common: vp8test.VpxencVP8Config{
				Width:             opts.Width,
				Height:            opts.Height,
				Frames:            len(sources),
				Deadline:          libvpxOracleDeadline(opts.Deadline),
				CPUUsed:           opts.CpuUsed,
				TargetBitrateKbps: targetKbps,
				MinQ:              opts.MinQuantizer,
				MaxQ:              opts.MaxQuantizer,
				Timebase:          "1/" + strconv.Itoa(opts.FPS),
				FPS:               strconv.Itoa(opts.FPS) + "/1",
				KeyFrameDistSet:   true,
				KeyFrameMinDist:   opts.KeyFrameInterval,
				KeyFrameMaxDist:   opts.KeyFrameInterval,
				ExtraArgs:         []string{"--end-usage=vbr"},
			},
			FirstPassExtraArgs:  passExtraArgs,
			SecondPassExtraArgs: append(sectionArgs, passExtraArgs...),
		},
	)
	if err != nil {
		t.Fatalf("vpxenc-oracle two-pass encode failed: %v\n%s", err, diag)
	}
	frames, err := testutil.IVFFramePayloads(data)
	if err != nil {
		t.Fatalf("IVFFramePayloads: %v", err)
	}
	return frames
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
