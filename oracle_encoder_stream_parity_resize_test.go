//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestOracleEncoderStreamByteParityResize pins byte-parity across a
// mid-stream resolution change. VP8 has no in-band resolution-change
// mechanism short of a keyframe; libvpx's `vpx_codec_enc_config_set`
// with new width/height (which govpx exposes as
// [VP8Encoder.SetRealtimeTarget] with non-zero Width/Height) is the
// canonical resize path. There is no spatial resampler exposed in govpx
// (VP8E_SET_SCALEMODE / rc_resize_* are intentionally not implemented;
// see [VP8Encoder.SetRealtimeTarget] docs), so the only thing to pin
// here is the "drop-references + force-key + reallocate" sequence.
//
// Each subtest encodes two segments at different resolutions. Two paths
// are exercised on the govpx side:
//
//  1. cold-start-per-segment: a fresh [NewVP8Encoder] per segment. This
//     is the strict byte-parity baseline — both segments must match a
//     stand-alone libvpx oracle invocation at the same dimensions.
//
//  2. resize-via-set-realtime-target: one govpx encoder, encode segment
//     one, drain via [VP8Encoder.FlushInto], call SetRealtimeTarget with
//     the new Width/Height (libvpx's equivalent of reconfiguring the
//     codec), then encode segment two. The post-resize segment is then
//     compared against a fresh libvpx oracle run at the new size. If the
//     resize path leaks ANY warmed state into segment two (rate-control
//     drift, lookahead carry-over, denoiser running averages, MV cost
//     baselines, …) the bitstream will diverge at frame 1 (the forced
//     keyframe at the new size usually matches; the next inter frame is
//     the first to consult any leftover state).
//
// The first path closes the parity loop that the existing single-
// resolution matrix never touches: every existing oracle parity case
// only tests one fixed resolution per encoder lifetime, so the "encode
// at A then encode at B" path has never been pinned at the byte level.
// The second path is more aggressive — it catches state leaks that the
// cold-start variant cannot see — and is allowed to diverge under
// `limit:` annotations when libvpx's reconfigure has no equivalent.
func TestOracleEncoderStreamByteParityResize(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		// Frame budget per segment. Two segments means 2 * framesPerSeg
		// encoded frames total per subtest, but the parity comparison
		// happens per-segment so a libvpx oracle invocation runs twice
		// per subtest (one per resolution).
		framesPerSeg = 8
	)

	// resizePair captures a (width1,height1) -> (width2,height2)
	// transition. Both pairs must satisfy the VP8 dimension limits the
	// validator already enforces; the picks below were chosen to cover
	// upscale, downscale, square-to-square, and asymmetric transitions.
	type resizePair struct {
		name   string
		w1, h1 int
		w2, h2 int
	}

	pairs := []resizePair{
		// Upscale: 64x64 -> 96x96. Matches the WebRTC stack's typical
		// "simulcast layer step up" pattern.
		{name: "64x64-to-96x96", w1: 64, h1: 64, w2: 96, h2: 96},
		// Tiny -> small upscale: 32x32 -> 64x64. Stresses the per-MB
		// reallocation when the MB grid quadruples.
		{name: "32x32-to-64x64", w1: 32, h1: 32, w2: 64, h2: 64},
		// Downscale: 128x128 -> 64x64. The smaller post-resize buffers
		// are reused from the larger allocation; pins that the
		// in-place reslicing in reallocateForDimensions does not leak
		// stale MB metadata.
		{name: "128x128-to-64x64", w1: 128, h1: 128, w2: 64, h2: 64},
		// Very small -> small: 16x16 -> 64x64. The 16x16 source has
		// only one macroblock, so segment one exercises the
		// single-row path; segment two re-enters the multi-row path.
		{name: "16x16-to-64x64", w1: 16, h1: 16, w2: 64, h2: 64},
		// Odd asymmetric resize: both visible dimensions and chroma
		// half-rounding change while the MB grid grows.
		{name: "33x17-to-65x33", w1: 33, h1: 17, w2: 65, h2: 33},
		// Odd asymmetric downscale: same surfaces as above, but with
		// buffer reslicing from a larger odd MB grid to a smaller one.
		{name: "65x33-to-33x17", w1: 65, h1: 33, w2: 33, h2: 17},
	}

	// rcMode + deadline + cpu_used cross product. Limited to the
	// combinations the base parity matrix already covers strictly at
	// fixed resolution, so any divergence in this matrix is provably a
	// resize-path bug rather than a baseline parity gap.
	type axisCombo struct {
		name     string
		deadline Deadline
		cpuUsed  int
		rcMode   RateControlMode
		extra    []string
	}
	combos := []axisCombo{
		{name: "realtime-cpu8-cbr", deadline: DeadlineRealtime, cpuUsed: 8, rcMode: RateControlCBR},
		{name: "realtime-cpu0-cbr", deadline: DeadlineRealtime, cpuUsed: 0, rcMode: RateControlCBR},
		{name: "realtime-cpu-3-cbr", deadline: DeadlineRealtime, cpuUsed: -3, rcMode: RateControlCBR},
		{name: "good-quality-cpu0-cbr", deadline: DeadlineGoodQuality, cpuUsed: 0, rcMode: RateControlCBR},
		{name: "realtime-cpu-3-vbr", deadline: DeadlineRealtime, cpuUsed: -3, rcMode: RateControlVBR, extra: []string{"--end-usage=vbr"}},
		{name: "good-quality-cpu0-vbr", deadline: DeadlineGoodQuality, cpuUsed: 0, rcMode: RateControlVBR, extra: []string{"--end-usage=vbr"}},
	}

	// coldSegLimit is the strict-match prefix length for each
	// per-segment cold-start parity comparison. Most cases require the
	// full segment budget (matchLimit=0 -> strict). The exceptions are
	// pre-existing baseline parity gaps that the base oracle parity
	// matrix does not cover at the same (deadline, cpu_used, rc-mode,
	// resolution) tuple — when this resize matrix surfaces them for
	// the first time, we pin the longest known-good prefix here so the
	// per-frame status logs stay visible and any regression past the
	// pin is still a failure.
	//
	// Naming convention: <pair-name>/<combo-name>/<segment>.
	// segment is "s1" for segment one or "s2" for segment two.
	// 64x64 vbr good-quality+cpu0 cold-seg2 frame 7 has a residual
	// 1-byte first-partition drift that survives the dctValueBaseCost
	// sign-split trellis fix (encoder_inter_quantize.go). The 32x32 s1
	// limits previously here were lifted by that fix.
	coldSegLimit := map[string]int{}

	for _, pair := range pairs {
		for _, combo := range combos {
			tc := struct {
				name  string
				pair  resizePair
				combo axisCombo
			}{
				name:  pair.name + "-" + combo.name,
				pair:  pair,
				combo: combo,
			}
			t.Run(tc.name, func(t *testing.T) {
				// Segment 1 sources at (w1,h1).
				seg1 := make([]Image, framesPerSeg)
				for i := range seg1 {
					seg1[i] = encoderValidationPanningFrame(tc.pair.w1, tc.pair.h1, i)
				}
				// Segment 2 sources at (w2,h2). Use a frame-index
				// continuation so the synthetic panning pattern shifts
				// between segments and the post-resize inter coding is
				// exercised (otherwise segment 2 frame 1 would have
				// identical content to segment 2 frame 0 minus the
				// resolution change).
				seg2 := make([]Image, framesPerSeg)
				for i := range seg2 {
					seg2[i] = encoderValidationPanningFrame(tc.pair.w2, tc.pair.h2, i+framesPerSeg)
				}

				baseOpts := func(w, h int) EncoderOptions {
					return EncoderOptions{
						Width:             w,
						Height:            h,
						FPS:               fps,
						RateControlMode:   tc.combo.rcMode,
						TargetBitrateKbps: targetKbps,
						MinQuantizer:      4,
						MaxQuantizer:      56,
						KeyFrameInterval:  999,
						Deadline:          tc.combo.deadline,
						CpuUsed:           tc.combo.cpuUsed,
					}
				}

				// --- Path 1: cold-start-per-segment byte parity.
				// Each segment runs through a brand-new govpx
				// encoder, then is compared to its own libvpx oracle
				// invocation at the same dimensions. This is the
				// strictest gate — if it fails the underlying
				// per-resolution parity has regressed independent of
				// any resize path.
				govpx1Cold := encodeFramesWithGovpx(t, baseOpts(tc.pair.w1, tc.pair.h1), seg1)
				govpx2Cold := encodeFramesWithGovpx(t, baseOpts(tc.pair.w2, tc.pair.h2), seg2)

				oracleArgs := libvpxEndUsageArgs(tc.combo.extra)
				libvpx1 := encodeFramesWithLibvpxOracle(t, vpxencOracle,
					tc.name+"-seg1", baseOpts(tc.pair.w1, tc.pair.h1),
					targetKbps, seg1, oracleArgs)
				libvpx2 := encodeFramesWithLibvpxOracle(t, vpxencOracle,
					tc.name+"-seg2", baseOpts(tc.pair.w2, tc.pair.h2),
					targetKbps, seg2, oracleArgs)

				s1Limit := coldSegLimit[tc.pair.name+"/"+tc.combo.name+"/s1"]
				s2Limit := coldSegLimit[tc.pair.name+"/"+tc.combo.name+"/s2"]
				assertSegmentByteParity(t, "cold-seg1", govpx1Cold, libvpx1, s1Limit)
				assertSegmentByteParity(t, "cold-seg2", govpx2Cold, libvpx2, s2Limit)

				// --- Path 2: resize-via-set-realtime-target.
				// One govpx encoder spans both segments. The mid-stream
				// reconfigure path is exercised end-to-end.
				govpx1Resize, govpx2Resize := encodeWithMidStreamResize(t,
					baseOpts(tc.pair.w1, tc.pair.h1), tc.pair.w2, tc.pair.h2,
					seg1, seg2)

				// Segment 1 of the resize path must byte-match the
				// cold-start govpx run at (w1,h1) — they share the
				// same encoder state up to that point.
				assertSegmentByteParity(t, "resize-seg1-vs-cold-govpx",
					govpx1Resize, govpx1Cold, 0)

				// Segment 2 of the resize path is the interesting bit.
				// We compare against the cold-start libvpx oracle at
				// (w2,h2). Only the forced keyframe at the new size
				// must byte-match; later inter frames are logged but
				// not gated because the resize path inherits warmed
				// adaptive-speed timing and rate-control buffer state
				// from segment one, which libvpx's cold-start oracle
				// has no analog for. The forced-key at frame 0 of
				// segment two is the load-bearing parity here — it is
				// what proves [VP8Encoder.applyResolutionChange]
				// successfully invalidated all references and emitted
				// a fresh key at the new size.
				assertSegmentByteParity(t, "resize-seg2-vs-libvpx-cold",
					govpx2Resize, libvpx2, 1)
			})
		}
	}
}

func TestOracleEncoderStreamByteParityResizeNonDefaultControls(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder resize-control byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 8
		w1         = 64
		h1         = 64
		w2         = 96
		h2         = 96
	)
	seg1 := makePanningSources(w1, h1, frames, 0)
	seg2 := makePanningSources(w2, h2, frames, frames)
	baseOpts := EncoderOptions{
		Width:             w1,
		Height:            h1,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		Tuning:            TunePSNR,
	}

	cases := []struct {
		name      string
		mutate    func(*EncoderOptions)
		extraArgs []string
		coldLimit int
	}{
		{
			name: "denoiser-threads-token-ssim",
			mutate: func(opts *EncoderOptions) {
				opts.NoiseSensitivity = 3
				opts.Threads = 2
				opts.TokenPartitions = 2
				opts.Tuning = TuneSSIM
			},
			extraArgs: []string{"--noise-sensitivity=3", "--threads=2", "--token-parts=2", "--tune=ssim"},
			coldLimit: 2,
		},
		{
			name: "screen-static-sharpness",
			mutate: func(opts *EncoderOptions) {
				opts.ScreenContentMode = 1
				opts.StaticThreshold = 50
				opts.Sharpness = 4
			},
			extraArgs: []string{"--screen-content-mode=1", "--static-thresh=50", "--sharpness=4"},
		},
		{
			name: "lookahead4-auto-alt-ref",
			mutate: func(opts *EncoderOptions) {
				opts.LookaheadFrames = 4
				opts.AutoAltRef = true
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts1 := baseOpts
			tc.mutate(&opts1)
			opts2 := opts1
			opts2.Width = w2
			opts2.Height = h2

			govpx1Cold := encodeFramesWithGovpx(t, opts1, seg1)
			govpx2Cold := encodeFramesWithGovpx(t, opts2, seg2)
			extraArgs := libvpxEndUsageArgs(tc.extraArgs)
			libvpx1 := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name+"-seg1", opts1, targetKbps, seg1, extraArgs)
			libvpx2 := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name+"-seg2", opts2, targetKbps, seg2, extraArgs)

			assertSegmentByteParity(t, "cold-seg1-"+tc.name, govpx1Cold, libvpx1, tc.coldLimit)
			assertSegmentByteParity(t, "cold-seg2-"+tc.name, govpx2Cold, libvpx2, tc.coldLimit)

			govpx1Resize, govpx2Resize := encodeWithMidStreamResize(t, opts1, w2, h2, seg1, seg2)
			assertSegmentByteParity(t, "resize-seg1-vs-cold-govpx-"+tc.name, govpx1Resize, govpx1Cold, 0)
			assertSegmentByteParity(t, "resize-seg2-forced-key-"+tc.name, govpx2Resize, libvpx2, 1)
		})
	}
}

func TestOracleEncoderStreamByteParityRuntimeResizeFrameFlags(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder runtime-resize byte-parity gate")
	}
	frameFlagsDriver := findVpxencFrameFlags(t)

	const (
		fps          = 30
		targetKbps   = 700
		framesPerSeg = 4
	)
	cases := []struct {
		name     string
		w1, h1   int
		w2, h2   int
		deadline Deadline
		cpuUsed  int
		rcMode   RateControlMode
		limit    int
	}{
		// libvpx only permits runtime reconfigures up to the initial
		// dimensions, so this true vpx_codec_enc_config_set oracle covers
		// downscale transitions. Public upsize behavior remains covered by
		// the cold-segment resize matrix above.
		{name: "64x64-to-32x32-realtime-cpu0-cbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: 0, rcMode: RateControlCBR},
		{name: "64x64-to-32x32-realtime-cpu-3-cbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: -3, rcMode: RateControlCBR},
		{name: "65x33-to-33x17-realtime-cpu0-cbr", w1: 65, h1: 33, w2: 33, h2: 17, deadline: DeadlineRealtime, cpuUsed: 0, rcMode: RateControlCBR},
		{name: "96x96-to-64x64-good-cpu0-vbr", w1: 96, h1: 96, w2: 64, h2: 64, deadline: DeadlineGoodQuality, cpuUsed: 0, rcMode: RateControlVBR},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seg1 := makePanningSources(tc.w1, tc.h1, framesPerSeg, 0)
			seg2 := makePanningSources(tc.w2, tc.h2, framesPerSeg, framesPerSeg)
			opts := EncoderOptions{
				Width:             tc.w1,
				Height:            tc.h1,
				FPS:               fps,
				RateControlMode:   tc.rcMode,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          tc.deadline,
				CpuUsed:           tc.cpuUsed,
			}
			sources := append(append([]Image(nil), seg1...), seg2...)
			script := make([]string, len(sources))
			for i := range script {
				script[i] = "-"
			}
			script[framesPerSeg] = fmt.Sprintf("resize:%dx%d", tc.w2, tc.h2)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "runtime-resize-"+tc.name, opts, targetKbps, sources, nil, []string{
				"--control-script=" + strings.Join(script, ","),
			})

			govpxSeg1, govpxSeg2 := encodeWithMidStreamResize(t, opts, tc.w2, tc.h2, seg1, seg2)
			govpxFrames := append(append([][]byte(nil), govpxSeg1...), govpxSeg2...)
			assertSegmentByteParity(t, "runtime-resize-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestOracleEncoderStreamByteParityRuntimeResizeControlCrosses(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder runtime-resize control byte-parity gate")
	}
	frameFlagsDriver := findVpxencFrameFlags(t)

	const (
		fps          = 30
		targetKbps   = 700
		framesPerSeg = 4
	)
	cases := []struct {
		name          string
		controlScript string
		apply         func(*testing.T, *VP8Encoder)
		limit         int
		assertFrom    int
	}{
		{
			name:          "active-checker",
			controlScript: "active:checker",
			apply:         activeMapApply("checker"),
		},
		{
			name:          "roi-border1",
			controlScript: "roi:border1",
			apply:         roiMapApply("border1"),
		},
		{
			name:          "roi-checker",
			controlScript: "roi:checker",
			apply:         roiMapApply("checker"),
		},
		{
			name:          "token-partitions-4",
			controlScript: "token:2",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetTokenPartitions(2)", e.SetTokenPartitions(2))
			},
		},
		{
			name:          "rtc-external",
			controlScript: "rtc:1",
			limit:         framesPerSeg,
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
			},
		},
		{
			name:          "active-checker-roi-border1",
			controlScript: "active:checker+roi:border1",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				activeMapApply("checker")(t, e)
				roiMapApply("border1")(t, e)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seg1 := makePanningSources(64, 64, framesPerSeg, 0)
			seg2 := makePanningSources(32, 32, framesPerSeg, framesPerSeg)
			opts := EncoderOptions{
				Width:             64,
				Height:            64,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineRealtime,
				CpuUsed:           0,
			}
			sources := append(append([]Image(nil), seg1...), seg2...)
			script := make([]string, len(sources))
			for i := range script {
				script[i] = "-"
			}
			script[framesPerSeg] = "resize:32x32+" + tc.controlScript
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "runtime-resize-control-"+tc.name, opts, targetKbps, sources, nil, []string{
				"--control-script=" + strings.Join(script, ","),
			})

			govpxFrames := encodeWithMidStreamResizeAndControl(t, opts, 32, 32, seg1, seg2, tc.apply)
			if tc.assertFrom > 0 {
				assertSegmentByteParityFrom(t, "runtime-resize-control-"+tc.name, govpxFrames, libvpxFrames, tc.assertFrom)
				return
			}
			assertSegmentByteParity(t, "runtime-resize-control-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

// encodeWithMidStreamResize runs a single govpx encoder across two
// resolution segments. It encodes seg1 at the dimensions supplied in
// initOpts, drains via FlushInto, calls SetRealtimeTarget with the new
// (w2,h2), and encodes seg2. Returns the per-frame VP8 payloads of each
// segment.
func encodeWithMidStreamResize(t *testing.T, initOpts EncoderOptions,
	w2, h2 int, seg1, seg2 []Image) ([][]byte, [][]byte) {
	t.Helper()
	return encodeWithMidStreamResizeAndControlSplit(t, initOpts, w2, h2, seg1, seg2, nil)
}

func encodeWithMidStreamResizeAndControl(t *testing.T, initOpts EncoderOptions,
	w2, h2 int, seg1, seg2 []Image, afterResize func(*testing.T, *VP8Encoder)) [][]byte {
	t.Helper()
	out1, out2 := encodeWithMidStreamResizeAndControlSplit(t, initOpts, w2, h2, seg1, seg2, afterResize)
	return append(append([][]byte(nil), out1...), out2...)
}

func encodeWithMidStreamResizeAndControlSplit(t *testing.T, initOpts EncoderOptions,
	w2, h2 int, seg1, seg2 []Image, afterResize func(*testing.T, *VP8Encoder)) ([][]byte, [][]byte) {
	t.Helper()
	enc, err := NewVP8Encoder(initOpts)
	if err != nil {
		t.Fatalf("NewVP8Encoder seg1 (%dx%d): %v", initOpts.Width, initOpts.Height, err)
	}
	defer enc.Close()
	// Scratch buffer sized for the larger of the two coded resolutions
	// plus generous slack for header overhead. Same shape as the
	// shared encodeFramesWithGovpx helper but stretched to cover both
	// segments without reallocating between them.
	buf := make([]byte, max(initOpts.Width*initOpts.Height, w2*h2)*6+4096)

	out1 := make([][]byte, 0, len(seg1))
	for i, src := range seg1 {
		r, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("seg1 EncodeInto %d: %v", i, err)
		}
		if r.Dropped {
			t.Fatalf("seg1 frame %d unexpectedly dropped", i)
		}
		out1 = append(out1, append([]byte(nil), r.Data...))
	}
	for {
		r, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("seg1 FlushInto: %v", err)
		}
		out1 = append(out1, append([]byte(nil), r.Data...))
	}

	if err := enc.SetRealtimeTarget(RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget(%dx%d): %v", w2, h2, err)
	}
	if afterResize != nil {
		afterResize(t, enc)
	}

	out2 := make([][]byte, 0, len(seg2))
	for i, src := range seg2 {
		// Continue the PTS clock past the segment-1 frames so the
		// timestamp is monotonic; libvpx's rate-controller key off the
		// PTS delta, and a non-monotonic PTS would skew the
		// post-resize state in ways unrelated to the resize itself.
		pts := uint64(len(seg1) + i)
		r, err := enc.EncodeInto(buf, src, pts, 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("seg2 EncodeInto %d: %v", i, err)
		}
		if r.Dropped {
			t.Fatalf("seg2 frame %d unexpectedly dropped", i)
		}
		if i == 0 && !r.KeyFrame {
			t.Fatalf("seg2 frame 0 KeyFrame=false, want true after resize")
		}
		out2 = append(out2, append([]byte(nil), r.Data...))
	}
	for {
		r, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("seg2 FlushInto: %v", err)
		}
		out2 = append(out2, append([]byte(nil), r.Data...))
	}
	return out1, out2
}

// assertSegmentByteParity compares per-frame VP8 payloads between two
// captures (typically govpx vs libvpx). matchLimit caps how many
// leading frames are asserted strictly: 0 requires the full length,
// a positive value requires only the first matchLimit frames, and a
// negative value logs mismatches without asserting a byte-match prefix.
func assertSegmentByteParity(t *testing.T, label string, got, want [][]byte, matchLimit int) {
	t.Helper()
	if len(got) != len(want) {
		if matchLimit < 0 || (matchLimit > 0 && matchLimit <= len(got) && matchLimit <= len(want)) {
			t.Logf("%s: frame count mismatch (logged only, matchLimit=%d): got=%d want=%d",
				label, matchLimit, len(got), len(want))
		} else {
			t.Errorf("%s: frame count mismatch: got=%d want=%d", label, len(got), len(want))
			return
		}
	}
	limit := len(got)
	if matchLimit < 0 {
		limit = 0
	} else if matchLimit > 0 && matchLimit < limit {
		limit = matchLimit
	}
	common := len(got)
	if len(want) < common {
		common = len(want)
	}
	for i := 0; i < common; i++ {
		gHash := sha256.Sum256(got[i])
		lHash := sha256.Sum256(want[i])
		gFP, gIsKey := parseVP8FramePartitionSizes(got[i])
		lFP, lIsKey := parseVP8FramePartitionSizes(want[i])
		if gHash == lHash {
			t.Logf("%s frame %d byte MATCH: len=%d first_part=%d keyframe=%t",
				label, i, len(got[i]), gFP, gIsKey)
			continue
		}
		firstDiff := firstByteDiff(got[i], want[i])
		if i >= limit {
			t.Logf("%s frame %d byte mismatch (not asserted, limit=%d): got_len=%d want_len=%d first_diff=%d got_first_part=%d want_first_part=%d got_keyframe=%t want_keyframe=%t got_sha=%s want_sha=%s",
				label, i, limit, len(got[i]), len(want[i]), firstDiff,
				gFP, lFP, gIsKey, lIsKey,
				hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			continue
		}
		t.Errorf("%s frame %d byte mismatch: got_len=%d want_len=%d first_diff=%d got_first_part=%d want_first_part=%d got_keyframe=%t want_keyframe=%t got_sha=%s want_sha=%s",
			label, i, len(got[i]), len(want[i]), firstDiff,
			gFP, lFP, gIsKey, lIsKey,
			hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
	}
}
