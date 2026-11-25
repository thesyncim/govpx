//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"strconv"
	"strings"
	"testing"
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

func TestVP8OracleEncoderStreamByteParityActiveMapPatterns(t *testing.T) {
	vp8test.RequireOracle(t, "active-map byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
		width      = 64
		height     = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
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

	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	cases := []struct {
		name                string
		pattern             string
		limit               int
		cpuUsed             int
		noiseSensitivity    int
		screenContentMode   int
		tokenPartitions     int
		sharpness           int
		tuning              Tuning
		tuningSet           bool
		errorResilient      bool
		errorResilientParts bool
		threads             int
		extraArgs           []string
	}{
		{name: "all", pattern: "all", limit: 0},
		{name: "checker", pattern: "checker", limit: 0},
		{name: "left-off", pattern: "left-off", limit: 0},
		{name: "right-off", pattern: "right-off", limit: 0},
		{name: "border-off", pattern: "border-off", limit: 0},
		{name: "off", pattern: "off", limit: 0},
		{name: "left-off-cpu-3", pattern: "left-off", cpuUsed: -3, limit: 0},
		{name: "right-off-cpu-3", pattern: "right-off", cpuUsed: -3, limit: 0},
		{name: "border-off-cpu-3", pattern: "border-off", cpuUsed: -3, limit: 0},
		{name: "checker-noise1", pattern: "checker", noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "checker-noise2", pattern: "checker", noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "checker-noise3", pattern: "checker", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "checker-noise4", pattern: "checker", noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "checker-noise5", pattern: "checker", noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "checker-noise6", pattern: "checker", noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "left-off-noise1", pattern: "left-off", noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "left-off-noise2", pattern: "left-off", noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "left-off-noise3", pattern: "left-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "left-off-noise4", pattern: "left-off", noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "left-off-noise5", pattern: "left-off", noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "left-off-noise6", pattern: "left-off", noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "right-off-noise1", pattern: "right-off", noiseSensitivity: 1, limit: 0, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "right-off-noise2", pattern: "right-off", noiseSensitivity: 2, limit: 0, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "right-off-noise3", pattern: "right-off", noiseSensitivity: 3, limit: 0, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "right-off-noise4", pattern: "right-off", noiseSensitivity: 4, limit: 0, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "right-off-noise5", pattern: "right-off", noiseSensitivity: 5, limit: 0, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "right-off-noise6", pattern: "right-off", noiseSensitivity: 6, limit: 0, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "right-off-noise3-cpu-3", pattern: "right-off", cpuUsed: -3, noiseSensitivity: 3, limit: 0, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "checker-noise3-screen-content2", pattern: "checker", noiseSensitivity: 3, screenContentMode: 2, extraArgs: []string{"--noise-sensitivity=3", "--screen-content-mode=2"}},
		{name: "right-off-noise3-screen-content2", pattern: "right-off", noiseSensitivity: 3, screenContentMode: 2, limit: 0, extraArgs: []string{"--noise-sensitivity=3", "--screen-content-mode=2"}},
		{name: "checker-token-parts4", pattern: "checker", tokenPartitions: 2, limit: 0, extraArgs: []string{"--token-parts=2"}},
		{name: "right-off-sharpness4", pattern: "right-off", sharpness: 4, limit: 0, extraArgs: []string{"--sharpness=4"}},
		{name: "left-off-tune-ssim", pattern: "left-off", tuning: TuneSSIM, tuningSet: true, limit: 0, extraArgs: []string{"--tune=ssim"}},
		{name: "border-off-er3-token-parts4", pattern: "border-off", tokenPartitions: 2, errorResilient: true, errorResilientParts: true, limit: 0, extraArgs: []string{"--error-resilient=3", "--token-parts=2"}},
		{name: "border-off-noise1", pattern: "border-off", noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "border-off-noise2", pattern: "border-off", noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "border-off-noise3", pattern: "border-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "border-off-noise4", pattern: "border-off", noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "border-off-noise5", pattern: "border-off", noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "border-off-noise6", pattern: "border-off", noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "checker-noise3-threads2", pattern: "checker", noiseSensitivity: 3, threads: 2, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
		{name: "left-off-noise3-threads2", pattern: "left-off", noiseSensitivity: 3, threads: 2, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
		{name: "right-off-noise3-threads2", pattern: "right-off", noiseSensitivity: 3, threads: 2, limit: 0, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
		{name: "border-off-noise3-threads2", pattern: "border-off", noiseSensitivity: 3, threads: 2, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseOpts := opts
			if tc.cpuUsed != 0 {
				caseOpts.CpuUsed = tc.cpuUsed
			}
			caseOpts.NoiseSensitivity = tc.noiseSensitivity
			caseOpts.ScreenContentMode = tc.screenContentMode
			caseOpts.TokenPartitions = tc.tokenPartitions
			caseOpts.Sharpness = tc.sharpness
			if tc.tuningSet {
				caseOpts.Tuning = tc.tuning
			}
			caseOpts.ErrorResilient = tc.errorResilient
			caseOpts.ErrorResilientPartitions = tc.errorResilientParts
			caseOpts.Threads = tc.threads
			apply := map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					if tc.pattern == "off" {
						mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
						return
					}
					mustRuntime(t, "SetActiveMap("+tc.pattern+")", e.SetActiveMap(activeMapPattern(tc.pattern, rows, cols), rows, cols))
				},
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, caseOpts, sources, nil, apply)
			extraArgs := []string{
				"--active-map=" + tc.pattern,
			}
			extraArgs = append(extraArgs, tc.extraArgs...)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "active-map-"+tc.name+"-64x64", caseOpts, targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "active-map-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestVP8OracleEncoderStreamByteParityActiveMapOddDimensions(t *testing.T) {
	vp8test.RequireOracle(t, "active-map byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 10
		width      = 65
		height     = 33
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
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
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	cases := []struct {
		name             string
		pattern          string
		limit            int
		noiseSensitivity int
		extraArgs        []string
	}{
		{name: "checker", pattern: "checker"},
		{name: "left-off", pattern: "left-off"},
		{name: "right-off", pattern: "right-off"},
		{name: "border-off", pattern: "border-off"},
		{name: "checker-noise3", pattern: "checker", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "left-off-noise3", pattern: "left-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "right-off-noise3", pattern: "right-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "border-off-noise3", pattern: "border-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseOpts := opts
			caseOpts.NoiseSensitivity = tc.noiseSensitivity
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, caseOpts, sources, nil, map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap("+tc.pattern+")", e.SetActiveMap(activeMapPattern(tc.pattern, rows, cols), rows, cols))
				},
			})
			extraArgs := []string{
				"--active-map=" + tc.pattern,
			}
			extraArgs = append(extraArgs, tc.extraArgs...)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "active-map-odd-"+tc.name, caseOpts, targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "active-map-odd-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

// FuzzEncoderRandomStrides feeds govpx Image values with varying Y/U/V stride
// padding, and asserts that the encoded VP8 keyframe is byte-identical to the
// libvpx oracle keyframe encoded from the equivalent tight I420 content. A
// stride-walk bug that reads padding bytes will surface as a keyframe SHA-256
// mismatch.
//
// The libvpx oracle side ingests I420 from a YUV file, which is
// always tight (visible content, no stride padding). The govpx
// side receives the SAME visible content but laid out with
// fuzz-generated YStride / UStride / VStride padding values. If
// the encoder correctly walks strides, the two outputs match.
func FuzzEncoderRandomStrides(f *testing.F) {
	vp8test.RequireOracleF(f, "random-strides fuzz")
	// Each seed is (dimBucket, yPadBucket, uPadBucket, vPadBucket,
	// uAlignBucket, vAlignBucket).
	seeds := [][]byte{
		{0, 0, 0, 0, 0, 0}, // tight 16x16
		{0, 4, 0, 0, 0, 0}, // 16x16 with Y stride +4
		{1, 0, 2, 2, 0, 0}, // 32x16 with U/V stride +2
		{2, 8, 4, 4, 0, 0}, // 48x48 with mixed padding
		{3, 16, 8, 8, 0, 0},
		{4, 0, 0, 0, 1, 1}, // 64x64 odd alignment
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		vpxencOracle := vp8test.VpxencOracle(t)

		dim, padded, tight := newStridesFuzzImage(data)
		if dim.w == 0 {
			return
		}
		opts := EncoderOptions{
			Width:             dim.w,
			Height:            dim.h,
			FPS:               30,
			RateControlMode:   RateControlCBR,
			TargetBitrateKbps: 700,
			MinQuantizer:      4,
			MaxQuantizer:      56,
			KeyFrameInterval:  999,
			Deadline:          DeadlineRealtime,
			CpuUsed:           strictByteParityCPUUsed(DeadlineRealtime, -3),
			Tuning:            TunePSNR,
		}
		sum := sha256.Sum256(data)
		label := "fuzz-strides-" + hex.EncodeToString(sum[:4])
		t.Logf("%s w=%d h=%d yStride=%d uStride=%d vStride=%d",
			label, dim.w, dim.h, padded.YStride, padded.UStride, padded.VStride)

		// Govpx encodes the padded image; libvpx oracle encodes the
		// tight one. Both must produce the same VP8 packet bytes.
		govpxFrames := encodeFramesWithGovpx(t, opts, []Image{padded})
		libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, label, opts, opts.TargetBitrateKbps, []Image{tight}, libvpxEndUsageArgs(nil))
		assertSegmentByteParity(t, label, govpxFrames, libvpxFrames, 0)
	})
}

type stridesFuzzDim struct {
	w, h int
}

// newStridesFuzzImage constructs (padded, tight) image pairs where
// the visible content matches but `padded` may include arbitrary
// per-plane stride padding. Both images use deterministic
// fuzz-bytes-driven content via encoderValidationPanningFrame for
// determinism so the libvpx oracle's identical-content premise
// holds.
func newStridesFuzzImage(data []byte) (stridesFuzzDim, Image, Image) {
	r := testutil.NewByteCursor(data)
	if r.Remaining() == 0 {
		return stridesFuzzDim{}, Image{}, Image{}
	}
	dimPool := [...]stridesFuzzDim{
		{16, 16},
		{32, 16},
		{48, 48},
		{64, 64},
		{96, 96},
	}
	dim := dimPool[int(r.Next())%len(dimPool)]
	yPad := int(r.Next() & 0x1f)   // 0..31
	uPad := int(r.Next() & 0x0f)   // 0..15
	vPad := int(r.Next() & 0x0f)   // 0..15
	uExtra := int(r.Next() & 0x01) // odd alignment
	vExtra := int(r.Next() & 0x01)

	uvW := (dim.w + 1) >> 1
	uvH := (dim.h + 1) >> 1
	yStride := dim.w + yPad
	uStride := uvW + uPad + uExtra
	vStride := uvW + vPad + vExtra

	// Base image is the deterministic panning frame at the target
	// dimension. The libvpx oracle side uses this verbatim (tight
	// strides); the govpx side reuses the visible content but lays
	// it out into a wider stride.
	tight := encoderValidationPanningFrame(dim.w, dim.h, 0)

	padded := Image{
		Width:   dim.w,
		Height:  dim.h,
		Y:       make([]byte, yStride*dim.h),
		U:       make([]byte, uStride*uvH),
		V:       make([]byte, vStride*uvH),
		YStride: yStride,
		UStride: uStride,
		VStride: vStride,
	}
	// Fill padding bytes with a distinctive pattern so a stride-walk
	// bug that reads them shows up as a divergence rather than
	// silently zeroing out.
	for i := range padded.Y {
		padded.Y[i] = 0xa5
	}
	for i := range padded.U {
		padded.U[i] = 0x5a
	}
	for i := range padded.V {
		padded.V[i] = 0xa5
	}
	for y := 0; y < dim.h; y++ {
		copy(padded.Y[y*yStride:y*yStride+dim.w], tight.Y[y*tight.YStride:y*tight.YStride+dim.w])
	}
	for y := 0; y < uvH; y++ {
		copy(padded.U[y*uStride:y*uStride+uvW], tight.U[y*tight.UStride:y*tight.UStride+uvW])
		copy(padded.V[y*vStride:y*vStride+uvW], tight.V[y*tight.VStride:y*tight.VStride+uvW])
	}
	return dim, padded, tight
}

func encodeLibvpxValidationCorpus(t *testing.T, vpxenc string, tc encoderValidationCase, sources []Image) []byte {
	t.Helper()
	extraArgs := []string{
		"--end-usage=cbr",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
	}
	extraArgs = append(extraArgs, tc.libvpxArgs...)
	cfg := vp8test.VpxencVP8Config{
		BinaryPath:        vpxenc,
		Width:             tc.width,
		Height:            tc.height,
		Frames:            len(sources),
		Deadline:          libvpxValidationDeadline(tc.opts.Deadline),
		CPUUsed:           tc.opts.CpuUsed,
		LagInFrames:       0,
		AutoAltRef:        false,
		TargetBitrateKbps: tc.targetKbps,
		MinQ:              4,
		MaxQ:              56,
		FPS:               strconv.Itoa(tc.fps) + "/1",
		KeyFrameDistSet:   true,
		KeyFrameMinDist:   999,
		KeyFrameMaxDist:   999,
		ExtraArgs:         extraArgs,
	}
	ivf, diag, err := vp8test.VpxencVP8EncodeI420(encoderValidationI420Bytes(t, sources), cfg)
	if err != nil {
		t.Fatalf("vpxenc failed: %v\n%s", err, diag)
	}
	return ivf
}

func libvpxValidationDeadline(deadline Deadline) string {
	switch deadline {
	case DeadlineBestQuality:
		return "best"
	case DeadlineRealtime:
		return "rt"
	default:
		return "good"
	}
}

func TestVP8OracleEncoderCorpusValidation(t *testing.T) {
	vp8test.RequireOracle(t, "encoder oracle validation")
	oracle := vp8test.NewChecksumOracle(t)
	vpxenc := vp8test.Vpxenc(t)

	cases := []encoderValidationCase{
		{
			name:       "motion-eight-token-partitions",
			width:      64,
			height:     128,
			frames:     18,
			fps:        30,
			targetKbps: 700,
			pattern:    encoderValidationMotion,
			opts: encoderValidationOptions(64, 128, 30, 700, func(opts *EncoderOptions) {
				opts.TokenPartitions = int(vp8common.EightPartition)
			}),
			libvpxArgs: []string{"--token-parts=3"},
			// Realtime mode follows the cheaper libvpx pick_inter path; the
			// remaining gap is mostly mode-loop and rate-control parity.
			minPSNR:                       48.0,
			minSSIM:                       0.999,
			minFramePSNR:                  48.25,
			minFrameSSIM:                  0.999,
			maxPSNRGap:                    0.8,
			maxSSIMGap:                    0.001,
			maxFramePSNRGap:               1.5,
			maxFrameSSIMGap:               0.002,
			maxRateHigh:                   250.0,
			maxRateLow:                    95.0,
			maxRateGapPct:                 35.0,
			wantTokenPartition:            vp8common.EightPartition,
			checkTokenPartition:           true,
			checkAllTokenPartitionsActive: true,
			checkBPredModes:               true,
			checkInterFrames:              true,
		},
		{
			name:       "static-segmentation",
			width:      64,
			height:     64,
			frames:     18,
			fps:        30,
			targetKbps: 500,
			pattern:    encoderValidationSegmented,
			opts: encoderValidationOptions(64, 64, 30, 500, func(opts *EncoderOptions) {
				opts.StaticThreshold = 1
				opts.MaxQuantizer = 56
			}),
			libvpxArgs: []string{"--static-thresh=1"},
			// Static-threshold encode-breakout is bounded by the libvpx oracle,
			// while full mode RD and rate parity remain open.
			minPSNR:                 49.0,
			minSSIM:                 0.999,
			minFramePSNR:            48.75,
			minFrameSSIM:            0.999,
			maxPSNRGap:              0.5,
			maxSSIMGap:              0.001,
			maxFramePSNRGap:         0.55,
			maxFrameSSIMGap:         0.002,
			maxRateHigh:             250.0,
			maxRateLow:              95.0,
			maxRateGapPct:           5.0,
			checkSegmentationHeader: true,
			checkInterFrames:        true,
		},
		qualityValidationCase("best-quality-panning", DeadlineBestQuality, 0, 47.4, 47.1, 0.6, 0.7, 8.0),
		qualityValidationCase("good-quality-rd-panning", DeadlineGoodQuality, 3, 47.9, 47.6, 1.0, 1.2, 5.0),
		qualityValidationCase("good-quality-fast-panning", DeadlineGoodQuality, 4, 47.9, 47.6, 1.0, 1.2, 5.0),
		realtimeSpeedValidationCase(0, 47.9, 47.6, 0.8, 0.8, 12.0),
		realtimeSpeedValidationCase(3, 47.9, 47.6, 0.8, 0.8, 8.0),
		realtimeSpeedValidationCase(4, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(5, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(8, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(9, 47.9, 47.6, 0.4, 0.4, 5.0),
		realtimeSpeedValidationCase(15, 47.9, 47.6, 0.4, 0.4, 5.0),
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := encoderValidationFrames(tc)
			got := encodeGopvxValidationCorpus(t, tc, sources)
			wantChecksums := oracle.Frames(t, got.ivf)
			gotChecksums := decodeIVFChecksums(t, got.ivf)
			assertFrameChecksumsEqual(t, "govpx encode decoded by libvpx", gotChecksums, wantChecksums)
			assertGopvxEncoderValidationFeatures(t, got.ivf, tc)

			libvpxIVF := encodeLibvpxValidationCorpus(t, vpxenc, tc, sources)
			libvpxWantChecksums := oracle.Frames(t, libvpxIVF)
			libvpxGotChecksums := decodeIVFChecksums(t, libvpxIVF)
			assertFrameChecksumsEqual(t, "libvpx encode decoded by govpx", libvpxGotChecksums, libvpxWantChecksums)
			libvpxQuality := qualityMetricsForIVF(t, libvpxIVF, sources)
			libvpxOutputKbps := encoderValidationOutputKbps(len(libvpxIVF)-testutil.IVFFileHeaderSize-len(sources)*testutil.IVFFrameHeaderSize, tc.fps, len(sources))
			logEncoderValidationQuality(t, got.quality, got.outputKbps, libvpxQuality, libvpxOutputKbps)

			assertEncoderValidationQuality(t, "govpx", got.quality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "govpx", got.outputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)
			assertEncoderValidationQuality(t, "libvpx", libvpxQuality, tc.minPSNR, tc.minSSIM, tc.minFramePSNR, tc.minFrameSSIM)
			assertEncoderValidationRate(t, "libvpx", libvpxOutputKbps, tc.targetKbps, tc.maxRateLow, tc.maxRateHigh)
			assertEncoderValidationQualityGap(t, got.quality, libvpxQuality, tc)
			assertEncoderValidationRateGap(t, got.outputKbps, libvpxOutputKbps, tc)
		})
	}
}
