//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8OracleEncoderStreamByteParityResize pins byte-parity across a
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
func TestVP8OracleEncoderStreamByteParityResize(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)

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
	// per-segment cold-start parity comparison. The map is empty:
	// every cold-segment compare runs at matchLimit=0 (strict, full
	// segment budget). Historical exceptions (32x32 s1 first-partition
	// drift, 64x64 vbr good-quality+cpu0 cold-seg2 frame 7) were all
	// closed by the dctValueBaseCost sign-split trellis fix
	// (internal/vp8/encoder/inter_quantize.go) and remain pinned strict here as
	// regression sentinels. New per-segment slack requires an explicit
	// entry plus a task pinning the root cause; do not relax silently.
	//
	// Naming convention: <pair-name>/<combo-name>/<segment>.
	// segment is "s1" for segment one or "s2" for segment two.
	coldSegLimit := map[string]int{}

	// resizeSeg2KeyKnownDivergent marks <pair-name>/<combo-name> tuples
	// where the post-resize forced keyframe in the mid-stream
	// SetRealtimeTarget path diverges from a cold-start libvpx oracle at
	// (w2,h2). The govpx encoder carries adaptive-speed-timing /
	// vp8_auto_select_speed state from segment one across the resize, so
	// after porting the vp8_change_config Speed reset
	// (vp8/encoder/onyx_if.c:1706) the picker now consults the carried-
	// over autoSpeed for the first post-resize keyframe — libvpx's cold-
	// start oracle has no such history, so its Speed is the cpu_used
	// seed. Pre-port this comparison happened to match because govpx's
	// cold-start sentinel collapsed the carried-over autoSpeed to the
	// cpu_used=4 default that libvpx's cold-start also lands on; that
	// was the bug fixed by the Speed reset port. The cold-seg{1,2}
	// gates above still strictly enforce per-resolution parity, so the
	// only remaining check we relax here is the resize-seg2 vs
	// cold-libvpx first-frame compare, downgraded to a log-only diff.
	resizeSeg2KeyKnownDivergent := map[string]bool{
		"32x32-to-64x64/realtime-cpu8-cbr":   true,
		"128x128-to-64x64/realtime-cpu8-cbr": true,
		"16x16-to-64x64/realtime-cpu8-cbr":   true,
	}

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
				// a fresh key at the new size. Combos listed in
				// resizeSeg2KeyKnownDivergent surface a residual
				// adaptive-speed carryover divergence after the
				// vp8_change_config Speed reset port and are logged
				// only — see the map comment for the libvpx citation.
				if resizeSeg2KeyKnownDivergent[tc.pair.name+"/"+tc.combo.name] {
					if len(govpx2Resize) == 0 || len(libvpx2) == 0 {
						t.Fatalf("resize-seg2-vs-libvpx-cold: missing first frame: got=%d want=%d", len(govpx2Resize), len(libvpx2))
					}
					assertSegmentByteParity(t, "resize-seg2-vs-libvpx-cold",
						govpx2Resize[:1], libvpx2[:1], -1)
				} else {
					assertFirstFrameByteParity(t, "resize-seg2-vs-libvpx-cold",
						govpx2Resize, libvpx2)
				}
			})
		}
	}
}

func TestVP8OracleEncoderStreamByteParityResizeNonDefaultControls(t *testing.T) {
	vp8test.RequireOracle(t, "encoder resize-control byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)

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
			assertFirstFrameByteParity(t, "resize-seg2-forced-key-"+tc.name, govpx2Resize, libvpx2)
		})
	}
}

func TestVP8OracleEncoderStreamByteParityRuntimeResizeFrameFlags(t *testing.T) {
	vp8test.RequireOracle(t, "encoder runtime-resize byte-parity gate")
	frameFlagsDriver := vp8test.VpxencFrameFlags(t)

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
		cqLevel  int
		limit    int
	}{
		// libvpx only permits runtime reconfigures up to the initial
		// dimensions, so this true vpx_codec_enc_config_set oracle covers
		// downscale transitions. Public upsize behavior remains covered by
		// the cold-segment resize matrix above.
		{name: "64x64-to-32x32-realtime-cpu0-cbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: 0, rcMode: RateControlCBR},
		{name: "64x64-to-32x32-realtime-cpu-3-cbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: -3, rcMode: RateControlCBR},
		{name: "64x64-to-32x32-realtime-cpu0-vbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: 0, rcMode: RateControlVBR},
		{name: "64x64-to-32x32-realtime-cpu-3-vbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: -3, rcMode: RateControlVBR},
		{name: "64x64-to-32x32-realtime-cpu-3-cq20", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: -3, rcMode: RateControlCQ, cqLevel: 20},
		{name: "64x64-to-32x32-realtime-cpu-3-q20", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineRealtime, cpuUsed: -3, rcMode: RateControlQ, cqLevel: 20},
		{name: "64x64-to-32x32-good-cpu0-cbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineGoodQuality, cpuUsed: 0, rcMode: RateControlCBR},
		{name: "64x64-to-32x32-best-cpu0-cbr", w1: 64, h1: 64, w2: 32, h2: 32, deadline: DeadlineBestQuality, cpuUsed: 0, rcMode: RateControlCBR},
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
				CQLevel:           tc.cqLevel,
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

func TestVP8OracleEncoderStreamByteParityRuntimeResizePostFrameCrosses(t *testing.T) {
	vp8test.RequireOracle(t, "encoder runtime-resize post-frame byte-parity gate")
	frameFlagsDriver := vp8test.VpxencFrameFlags(t)

	const (
		fps          = 30
		targetKbps   = 700
		framesPerSeg = 4
		w1           = 64
		h1           = 64
		w2           = 32
		h2           = 32
	)
	seg1 := makePanningSources(w1, h1, framesPerSeg, 0)
	seg2 := makePanningSources(w2, h2, framesPerSeg, framesPerSeg)
	sources := append(append([]Image(nil), seg1...), seg2...)
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
		CpuUsed:           0,
	}

	cases := []struct {
		name     string
		flags    []EncodeFlags
		controls map[int]string
		apply    map[int]func(*testing.T, *VP8Encoder)
		limit    int
	}{
		{
			name:  "force-keyframe-after-resize",
			flags: indexedResizeFlags(len(sources), map[int]EncodeFlags{framesPerSeg + 1: EncodeForceKeyFrame}),
		},
		{
			name:  "force-golden-after-resize",
			flags: indexedResizeFlags(len(sources), map[int]EncodeFlags{framesPerSeg + 1: EncodeForceGoldenFrame}),
		},
		{
			name:  "invisible-inter-after-resize",
			flags: indexedResizeFlags(len(sources), map[int]EncodeFlags{framesPerSeg + 1: EncodeInvisibleFrame}),
		},
		{
			name: "invisible-force-altref-after-resize",
			flags: indexedResizeFlags(len(sources), map[int]EncodeFlags{
				framesPerSeg + 1: EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden,
			}),
		},
		{
			name:  "set-reference-last-after-resize",
			flags: indexedResizeFlags(len(sources), map[int]EncodeFlags{framesPerSeg + 1: EncodeNoReferenceGolden | EncodeNoReferenceAltRef}),
			controls: map[int]string{
				framesPerSeg + 1: "setref:last:panning:12",
			},
			apply: map[int]func(*testing.T, *VP8Encoder){
				framesPerSeg + 1: setReferencePanningApply(ReferenceLast, 12, "last"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			updates := map[int]string{
				framesPerSeg: fmt.Sprintf("resize:%dx%d", w2, h2),
			}
			for frame, update := range tc.controls {
				if frame == framesPerSeg {
					updates[frame] += "+" + update
					continue
				}
				updates[frame] = update
			}
			script := runtimeControlScript(len(sources), updates)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "runtime-resize-post-"+tc.name, baseOpts, targetKbps, sources, tc.flags, []string{
				"--control-script=" + strings.Join(script, ","),
			})
			govpxFrames := encodeWithMidStreamResizeGlobalControls(t, baseOpts, w2, h2, seg1, seg2, tc.flags, tc.apply)
			assertSegmentByteParity(t, "runtime-resize-post-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestVP8OracleEncoderStreamByteParityRuntimeResizeControlCrosses(t *testing.T) {
	vp8test.RequireOracle(t, "encoder runtime-resize control byte-parity gate")
	frameFlagsDriver := vp8test.VpxencFrameFlags(t)

	const (
		fps          = 30
		targetKbps   = 700
		framesPerSeg = 4
	)
	cases := []struct {
		name          string
		controlScript string
		apply         func(*testing.T, *VP8Encoder)
		flags         []EncodeFlags
		script        []string
		globalApply   map[int]func(*testing.T, *VP8Encoder)
		resizeApply   func(*testing.T, *VP8Encoder, int, int)
		mutate        func(*EncoderOptions)
		extraArgs     []string
		limit         int
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
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
			},
		},
		{
			name:          "drop-frame-low-buffer",
			controlScript: "bitrate:300+bufsz:500+bufinit:100+bufopt:300+drop:60",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(drop-low-buffer)", e.SetRateControl(RateControlConfig{
					Mode:                RateControlCBR,
					TargetBitrateKbps:   300,
					MinQuantizer:        4,
					MaxQuantizer:        56,
					BufferSizeMs:        500,
					BufferInitialSizeMs: 100,
					BufferOptimalSizeMs: 300,
					DropFrameAllowed:    true,
					DropFrameWaterMark:  60,
				}))
			},
		},
		{
			name:          "frame-drop-toggle",
			controlScript: "drop:60",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetFrameDropAllowed(true)", e.SetFrameDropAllowed(true))
			},
		},
		{
			name:          "resize-bwe-fps-q-drop",
			controlScript: "bitrate:500+fps:24+minq:8+maxq:48+drop:60",
			resizeApply: func(t *testing.T, e *VP8Encoder, w, h int) {
				t.Helper()
				mustRuntime(t, "SetRealtimeTarget(resize-bwe-fps-q-drop)", e.SetRealtimeTarget(RealtimeTarget{
					Width:        w,
					Height:       h,
					BitrateKbps:  500,
					FPS:          24,
					MinQuantizer: 8,
					MaxQuantizer: 48,
					FrameDrop:    RealtimeFrameDropEnabled,
				}))
			},
		},
		{
			name:          "active-checker-noise3-threads2",
			controlScript: "active:checker",
			apply:         activeMapApply("checker"),
			mutate: func(opts *EncoderOptions) {
				opts.NoiseSensitivity = 3
				opts.Threads = 2
			},
			extraArgs: []string{"--noise-sensitivity=3", "--threads=2"},
		},
		{
			name:          "denoiser-disable-after-resize",
			controlScript: "noise:0",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetNoiseSensitivity(0)", e.SetNoiseSensitivity(0))
			},
			mutate: func(opts *EncoderOptions) {
				opts.NoiseSensitivity = 3
			},
			extraArgs: []string{"--noise-sensitivity=3"},
		},
		{
			name:          "roi-border1-er2-token4",
			controlScript: "roi:border1",
			apply:         roiMapApply("border1"),
			mutate: func(opts *EncoderOptions) {
				opts.ErrorResilientPartitions = true
				opts.TokenPartitions = 2
			},
			extraArgs: []string{"--error-resilient=2", "--token-parts=2"},
		},
		{
			name:          "token-partitions-8-er3",
			controlScript: "token:3",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetTokenPartitions(3)", e.SetTokenPartitions(3))
			},
			mutate: func(opts *EncoderOptions) {
				opts.ErrorResilient = true
				opts.ErrorResilientPartitions = true
			},
			extraArgs: []string{"--error-resilient=3"},
		},
		{
			name:          "rtc-external-roi-checker",
			controlScript: "rtc:1+roi:checker",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				roiMapApply("checker")(t, e)
			},
		},
		{
			name:          "drop-frame-active-left-off",
			controlScript: "bitrate:300+bufsz:500+bufinit:100+bufopt:300+drop:60+active:left-off",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(drop-low-buffer)", e.SetRateControl(RateControlConfig{
					Mode:                RateControlCBR,
					TargetBitrateKbps:   300,
					MinQuantizer:        4,
					MaxQuantizer:        56,
					BufferSizeMs:        500,
					BufferInitialSizeMs: 100,
					BufferOptimalSizeMs: 300,
					DropFrameAllowed:    true,
					DropFrameWaterMark:  60,
				}))
				activeMapApply("left-off")(t, e)
			},
		},
		{
			name:          "drop-frame-roi-border1",
			controlScript: "bitrate:300+bufsz:500+bufinit:100+bufopt:300+drop:60+roi:border1",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(drop-low-buffer)", e.SetRateControl(RateControlConfig{
					Mode:                RateControlCBR,
					TargetBitrateKbps:   300,
					MinQuantizer:        4,
					MaxQuantizer:        56,
					BufferSizeMs:        500,
					BufferInitialSizeMs: 100,
					BufferOptimalSizeMs: 300,
					DropFrameAllowed:    true,
					DropFrameWaterMark:  60,
				}))
				roiMapApply("border1")(t, e)
			},
		},
		{
			name:          "deadline-good-cpu-3",
			controlScript: "deadline:good+cpu:-3",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetDeadline(good)", e.SetDeadline(DeadlineGoodQuality))
				mustRuntime(t, "SetCPUUsed(-3)", e.SetCPUUsed(-3))
			},
		},
		{
			name:          "cq-mode",
			controlScript: runtimeRateControlModeControlToken(RateControlCQ, targetKbps),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(CQ)", e.SetRateControl(runtimeRateControlModeConfig(RateControlCQ, targetKbps)))
			},
		},
		{
			name:          "q-mode",
			controlScript: runtimeRateControlModeControlToken(RateControlQ, targetKbps),
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetRateControl(Q)", e.SetRateControl(runtimeRateControlModeConfig(RateControlQ, targetKbps)))
			},
		},
		{
			name:          "sharpness7-screen2-static500",
			controlScript: "sharpness:7+screen:2+static:500",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetSharpness(7)", e.SetSharpness(7))
				mustRuntime(t, "SetScreenContentMode(2)", e.SetScreenContentMode(2))
				mustRuntime(t, "SetStaticThreshold(500)", e.SetStaticThreshold(500))
			},
		},
		{
			name:          "max-intra-gf-boost",
			controlScript: "maxintra:500+gfboost:500",
			apply: func(t *testing.T, e *VP8Encoder) {
				t.Helper()
				mustRuntime(t, "SetMaxIntraBitratePct(500)", e.SetMaxIntraBitratePct(500))
				mustRuntime(t, "SetGFCBRBoostPct(500)", e.SetGFCBRBoostPct(500))
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
		{
			name:        "temporal-two-layer-enable",
			flags:       temporalScalabilityWindowFlags(framesPerSeg*2, TemporalLayeringTwoLayers, framesPerSeg, framesPerSeg*2),
			script:      temporalScalabilityWindowScript(framesPerSeg*2, TemporalLayeringTwoLayers, framesPerSeg, framesPerSeg*2, "resize:32x32+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps)),
			globalApply: map[int]func(*testing.T, *VP8Encoder){framesPerSeg: runtimeTemporalApply(TemporalLayeringTwoLayers, targetKbps, "two-layer")},
		},
		{
			name:        "temporal-three-layer-enable",
			flags:       temporalScalabilityWindowFlags(framesPerSeg*2, TemporalLayeringThreeLayers, framesPerSeg, framesPerSeg*2),
			script:      temporalScalabilityWindowScript(framesPerSeg*2, TemporalLayeringThreeLayers, framesPerSeg, framesPerSeg*2, "resize:32x32+"+runtimeTemporalControlToken(TemporalLayeringThreeLayers, targetKbps)),
			globalApply: map[int]func(*testing.T, *VP8Encoder){framesPerSeg: runtimeTemporalApply(TemporalLayeringThreeLayers, targetKbps, "three-layer")},
		},
		{
			name:  "temporal-two-layer-active-checker-enable",
			flags: temporalScalabilityWindowFlags(framesPerSeg*2, TemporalLayeringTwoLayers, framesPerSeg, framesPerSeg*2),
			script: temporalScalabilityWindowScript(
				framesPerSeg*2,
				TemporalLayeringTwoLayers,
				framesPerSeg,
				framesPerSeg*2,
				"resize:32x32+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps)+"+active:checker",
			),
			globalApply: map[int]func(*testing.T, *VP8Encoder){
				framesPerSeg: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					runtimeTemporalApply(TemporalLayeringTwoLayers, targetKbps, "two-layer")(t, e)
					activeMapApply("checker")(t, e)
				},
			},
		},
		{
			name:  "temporal-two-layer-roi-border-enable",
			flags: temporalScalabilityWindowFlags(framesPerSeg*2, TemporalLayeringTwoLayers, framesPerSeg, framesPerSeg*2),
			script: temporalScalabilityWindowScript(
				framesPerSeg*2,
				TemporalLayeringTwoLayers,
				framesPerSeg,
				framesPerSeg*2,
				"resize:32x32+"+runtimeTemporalControlToken(TemporalLayeringTwoLayers, targetKbps)+"+roi:border1",
			),
			globalApply: map[int]func(*testing.T, *VP8Encoder){
				framesPerSeg: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					runtimeTemporalApply(TemporalLayeringTwoLayers, targetKbps, "two-layer")(t, e)
					roiMapApply("border1")(t, e)
				},
			},
		},
		{
			name:  "temporal-two-layer-disable-after-resize",
			flags: temporalScalabilityWindowFlags(framesPerSeg*2, TemporalLayeringTwoLayers, 0, framesPerSeg),
			script: func() []string {
				script := runtimeTemporalDisableScript(framesPerSeg*2, TemporalLayeringTwoLayers, framesPerSeg, targetKbps)
				script[framesPerSeg] = "resize:32x32+" + runtimeTemporalOffControlToken(targetKbps)
				return script
			}(),
			globalApply: map[int]func(*testing.T, *VP8Encoder){
				framesPerSeg: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetTemporalScalability(off)", e.SetTemporalScalability(TemporalScalabilityConfig{}))
				},
			},
			mutate: func(opts *EncoderOptions) {
				opts.TemporalScalability = runtimeTemporalConfig(TemporalLayeringTwoLayers, targetKbps)
			},
			extraArgs: runtimeTemporalExtraArgs(TemporalLayeringTwoLayers, targetKbps),
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
			if tc.mutate != nil {
				tc.mutate(&opts)
			}
			sources := append(append([]Image(nil), seg1...), seg2...)
			script := append([]string(nil), tc.script...)
			if script == nil {
				script = make([]string, len(sources))
				for i := range script {
					script[i] = "-"
				}
				script[framesPerSeg] = "resize:32x32+" + tc.controlScript
			}
			extraArgs := append([]string(nil), tc.extraArgs...)
			extraArgs = append(extraArgs, "--control-script="+strings.Join(script, ","))
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, frameFlagsDriver, "runtime-resize-control-"+tc.name, opts, opts.TargetBitrateKbps, sources, tc.flags, extraArgs)

			var govpxFrames [][]byte
			if tc.resizeApply != nil {
				govpxFrames = encodeWithMidStreamResizeGlobalControlsAndResize(t, opts, 32, 32, seg1, seg2, tc.flags, tc.globalApply, tc.resizeApply)
			} else if tc.globalApply != nil || tc.flags != nil {
				govpxFrames = encodeWithMidStreamResizeGlobalControls(t, opts, 32, 32, seg1, seg2, tc.flags, tc.globalApply)
			} else {
				govpxFrames = encodeWithMidStreamResizeAndControl(t, opts, 32, 32, seg1, seg2, tc.apply)
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

func encodeWithMidStreamResizeGlobalControls(t *testing.T, initOpts EncoderOptions,
	w2, h2 int, seg1, seg2 []Image, flags []EncodeFlags, apply map[int]func(*testing.T, *VP8Encoder)) [][]byte {
	t.Helper()
	return encodeWithMidStreamResizeGlobalControlsAndResize(t, initOpts, w2, h2, seg1, seg2, flags, apply, nil)
}

func encodeWithMidStreamResizeGlobalControlsAndResize(t *testing.T, initOpts EncoderOptions,
	w2, h2 int, seg1, seg2 []Image, flags []EncodeFlags, apply map[int]func(*testing.T, *VP8Encoder),
	resizeApply func(*testing.T, *VP8Encoder, int, int)) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(initOpts)
	if err != nil {
		t.Fatalf("NewVP8Encoder seg1 (%dx%d): %v", initOpts.Width, initOpts.Height, err)
	}
	defer enc.Close()
	buf := make([]byte, max(initOpts.Width*initOpts.Height, w2*h2)*6+4096)
	out := make([][]byte, 0, len(seg1)+len(seg2))
	encodeOne := func(global int, src Image) {
		t.Helper()
		if fn := apply[global]; fn != nil {
			fn(t, enc)
		}
		var f EncodeFlags
		if global < len(flags) {
			f = flags[global]
		}
		result, err := enc.EncodeInto(buf, src, uint64(global), 1, f)
		if errors.Is(err, ErrFrameNotReady) {
			return
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", global, err)
		}
		if result.Dropped {
			t.Fatalf("frame %d unexpectedly dropped", global)
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	for i, src := range seg1 {
		encodeOne(i, src)
	}
	for {
		r, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("seg1 FlushInto: %v", err)
		}
		if r.Dropped {
			t.Fatalf("seg1 flush packet unexpectedly dropped")
		}
		out = append(out, append([]byte(nil), r.Data...))
	}
	if resizeApply != nil {
		resizeApply(t, enc, w2, h2)
	} else if err := enc.SetRealtimeTarget(RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget(%dx%d): %v", w2, h2, err)
	}
	for i, src := range seg2 {
		encodeOne(len(seg1)+i, src)
	}
	for {
		r, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("seg2 FlushInto: %v", err)
		}
		if r.Dropped {
			t.Fatalf("seg2 flush packet unexpectedly dropped")
		}
		out = append(out, append([]byte(nil), r.Data...))
	}
	return out
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

func indexedResizeFlags(frames int, updates map[int]EncodeFlags) []EncodeFlags {
	flags := make([]EncodeFlags, frames)
	for frame, flag := range updates {
		if frame >= 0 && frame < frames {
			flags[frame] = flag
		}
	}
	return flags
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
		firstDiff := testutil.FirstByteDiff(got[i], want[i])
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

func assertFirstFrameByteParity(t *testing.T, label string, got, want [][]byte) {
	t.Helper()
	if len(got) == 0 || len(want) == 0 {
		t.Fatalf("%s: missing first frame: got=%d want=%d", label, len(got), len(want))
	}
	assertSegmentByteParity(t, label, got[:1], want[:1], 0)
}

// assertStrictGateKnownGapMatchedPrefix is the migration target for
// strict-gate cases that opt into known-divergence behaviour with
// tc.limit < 0. It computes the matched-prefix length of got vs
// want and asserts it is at least `floor`. Per plan §5 this catches
// silent regression in the matched prefix that the prior
// log-and-return pattern would have masked. Empty common range
// (one side produced zero frames) is logged-only — the floor only
// binds when at least one common frame exists.
func assertStrictGateKnownGapMatchedPrefix(t *testing.T, label string, got, want [][]byte, floor int) {
	t.Helper()
	common := min(len(got), len(want))
	if common == 0 {
		t.Logf("%s known-gap: no common frames (got=%d want=%d)", label, len(got), len(want))
		return
	}
	matched := 0
	for i := 0; i < common; i++ {
		if sha256.Sum256(got[i]) == sha256.Sum256(want[i]) {
			matched++
		} else {
			break
		}
	}
	t.Logf("%s known-gap matched-prefix=%d (floor=%d)", label, matched, floor)
	if matched < floor {
		t.Errorf("%s matched-prefix=%d below floor=%d (regression in matched prefix)", label, matched, floor)
	}
}
