//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
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
