//go:build govpx_oracle_trace

package codec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"strconv"
	"strings"
	"testing"
)

// TestVP8OracleEncoderStreamByteParityDimensions widens the strict byte-parity
// matrix along the *resolution axis* that the base
// [TestVP8OracleEncoderStreamByteParity] and
// [TestVP8OracleEncoderStreamByteParityExtended] matrices do not exhaustively
// cover. It targets:
//
//   - sub-MB-aligned / very small frames at multiple cpu_used presets
//     (16x16, 16x32, 32x16);
//   - asymmetric small frames (24x16, 40x16, 16x40, 16x48, 48x16) — these
//     stress unequal MB row/column counts and the chroma half-rounding
//     boundary on the short dimension;
//   - off-axis (odd-width / odd-height) small frames (17x17, 31x17,
//     17x31, 49x17, 17x49) — pad rows/columns differ on every chroma
//     boundary and the encoded MB grid is the same as the next even
//     multiple of 16 above;
//   - mid-range 16:9 (426x240/240p, 854x480/FWVGA, 1024x576, 1280x720),
//     mid-range 4:3 (320x240/QVGA, 640x480/VGA, 800x600), and square
//     (240x240, 320x320, 400x400);
//   - very wide / very tall narrow strips (320x16, 16x320, 640x32,
//     32x640) — these have a 1-MB-tall or 1-MB-wide stripe of MBs and
//     stress edge-MB neighbour-availability decisions.
//
// Each case encodes 16 frames of the smooth panning fixture through
// govpx and the patched libvpx vpxenc oracle under matching options
// and asserts byte-identical VP8 packets.
//
// Runtime budget: BestQuality is limited to fixtures <= 64x64;
// large mid-range / wide-strip fixtures only run at cpu_used in
// {4, 8} so the test stays fast in oracle-test.
func TestVP8OracleEncoderStreamByteParityDimensions(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 16
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	mk := func(w, h int) fixture {
		return fixture{
			name:   panningName(w, h),
			w:      w,
			h:      h,
			source: encoderValidationPanningFrame,
		}
	}

	cases := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		fx       fixture
	}{
		// (1) Very small / sub-MB-aligned across cpu_used. The base
		// matrix pins 16x16 at most cpu_used values, but 16x32 and
		// 32x16 are only pinned at a couple of presets — fill in the
		// {-8,-3,0,4,8} cpu_used sweep here so any cpu-preset drift on
		// the asymmetric small grids is exposed.
		{name: "small-rt-cpu-8-16x32", deadline: DeadlineRealtime, cpuUsed: -8, fx: mk(16, 32)},
		{name: "small-rt-cpu-3-16x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(16, 32)},
		{name: "small-rt-cpu0-16x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(16, 32)},
		{name: "small-rt-cpu4-16x32", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(16, 32)},
		{name: "small-rt-cpu8-16x32", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(16, 32)},
		{name: "small-rt-cpu-8-32x16", deadline: DeadlineRealtime, cpuUsed: -8, fx: mk(32, 16)},
		{name: "small-rt-cpu-3-32x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(32, 16)},
		{name: "small-rt-cpu0-32x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(32, 16)},
		{name: "small-rt-cpu4-32x16", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(32, 16)},
		{name: "small-rt-cpu8-32x16", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(32, 16)},
		// 16x16 BestQuality + GoodQuality — the base matrix touches
		// Realtime here, this rounds out the deadline coverage on the
		// smallest fixture.
		{name: "small-best-cpu0-16x16", deadline: DeadlineBestQuality, cpuUsed: 0, fx: mk(16, 16)},
		{name: "small-good-cpu0-16x16", deadline: DeadlineGoodQuality, cpuUsed: 0, fx: mk(16, 16)},
		{name: "small-good-cpu4-16x32", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: mk(16, 32)},
		{name: "small-good-cpu4-32x16", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: mk(32, 16)},
		{name: "small-best-cpu0-16x32", deadline: DeadlineBestQuality, cpuUsed: 0, fx: mk(16, 32)},
		{name: "small-best-cpu0-32x16", deadline: DeadlineBestQuality, cpuUsed: 0, fx: mk(32, 16)},

		// (2) Asymmetric small. Each picks a representative cpu_used to
		// keep the matrix bounded — the asymmetry, not the picker
		// preset, is the axis under test here.
		{name: "asym-rt-cpu0-24x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(24, 16)},
		{name: "asym-rt-cpu-3-24x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(24, 16)},
		{name: "asym-rt-cpu8-24x16", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(24, 16)},
		{name: "asym-rt-cpu0-40x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(40, 16)},
		{name: "asym-rt-cpu-3-40x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(40, 16)},
		{name: "asym-rt-cpu4-40x16", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(40, 16)},
		{name: "asym-rt-cpu0-16x40", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(16, 40)},
		{name: "asym-rt-cpu-3-16x40", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(16, 40)},
		{name: "asym-rt-cpu4-16x40", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(16, 40)},
		{name: "asym-rt-cpu0-16x48", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(16, 48)},
		{name: "asym-rt-cpu-3-16x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(16, 48)},
		{name: "asym-rt-cpu0-48x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(48, 16)},
		{name: "asym-rt-cpu-3-48x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(48, 16)},
		{name: "asym-good-cpu4-40x16", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: mk(40, 16)},
		{name: "asym-good-cpu4-16x40", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: mk(16, 40)},

		// (3) Off-axis (odd) small. Encoded MB grid rounds up to the
		// next 16-multiple so a 17-wide frame uses the same 2-MB-wide
		// grid as a 32-wide one, but the right/bottom MB column writes
		// padded edge pixels.
		{name: "odd-rt-cpu-8-17x17", deadline: DeadlineRealtime, cpuUsed: -8, fx: mk(17, 17)},
		{name: "odd-rt-cpu-3-17x17", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(17, 17)},
		{name: "odd-rt-cpu0-17x17", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(17, 17)},
		{name: "odd-rt-cpu4-17x17", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(17, 17)},
		{name: "odd-rt-cpu8-17x17", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(17, 17)},
		{name: "odd-rt-cpu0-31x17", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(31, 17)},
		{name: "odd-rt-cpu-3-31x17", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(31, 17)},
		{name: "odd-rt-cpu4-31x17", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(31, 17)},
		{name: "odd-rt-cpu0-17x31", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(17, 31)},
		{name: "odd-rt-cpu-3-17x31", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(17, 31)},
		{name: "odd-rt-cpu4-17x31", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(17, 31)},
		{name: "odd-rt-cpu0-49x17", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(49, 17)},
		{name: "odd-rt-cpu-3-49x17", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(49, 17)},
		{name: "odd-rt-cpu4-49x17", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(49, 17)},
		{name: "odd-rt-cpu0-17x49", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(17, 49)},
		{name: "odd-rt-cpu-3-17x49", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(17, 49)},
		{name: "odd-rt-cpu4-17x49", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(17, 49)},
		// BestQuality fits the <=64x64 runtime budget on these and now
		// byte-matches the full 16-frame sequence across odd coded edges.
		{name: "odd-best-cpu0-17x17", deadline: DeadlineBestQuality, cpuUsed: 0, fx: mk(17, 17)},
		{name: "odd-best-cpu0-31x17", deadline: DeadlineBestQuality, cpuUsed: 0, fx: mk(31, 17)},
		{name: "odd-best-cpu0-17x31", deadline: DeadlineBestQuality, cpuUsed: 0, fx: mk(17, 31)},
		{name: "odd-good-cpu4-49x17", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: mk(49, 17)},
		{name: "odd-good-cpu4-17x49", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: mk(17, 49)},

		// (4) Mid-range 16:9. Keep cpu_used in the fast band (4, 8) for
		// the larger fixtures so the test fits the oracle-test wall
		// budget. cpu_used=-3 here would be 30-60s/frame at 720p.
		{name: "mid169-rt-cpu4-426x240", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(426, 240)},
		{name: "mid169-rt-cpu8-426x240", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(426, 240)},
		{name: "mid169-rt-cpu4-854x480", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(854, 480)},
		{name: "mid169-rt-cpu8-854x480", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(854, 480)},
		{name: "mid169-rt-cpu4-1024x576", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(1024, 576)},
		// 1024x576 and both 1280x720 realtime cases are byte-pinned
		// across the full sequence.
		{name: "mid169-rt-cpu8-1024x576", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(1024, 576)},
		{name: "mid169-rt-cpu4-1280x720", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(1280, 720)},
		{name: "mid169-rt-cpu8-1280x720", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(1280, 720)},

		// (5) Mid-range 4:3. Up to VGA we can afford cpu_used=0; SVGA
		// stays at the fast band.
		{name: "mid43-rt-cpu0-320x240", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(320, 240)},
		{name: "mid43-rt-cpu4-320x240", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(320, 240)},
		{name: "mid43-rt-cpu8-320x240", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(320, 240)},
		{name: "mid43-rt-cpu0-640x480", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(640, 480)},
		{name: "mid43-rt-cpu4-640x480", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(640, 480)},
		{name: "mid43-rt-cpu8-640x480", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(640, 480)},
		{name: "mid43-rt-cpu4-800x600", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(800, 600)},
		{name: "mid43-rt-cpu8-800x600", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(800, 600)},

		// (6) Square. Up to 400x400 we can run cpu_used=0; the picker
		// path matters more than the sheer pixel count here.
		{name: "sq-rt-cpu0-240x240", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(240, 240)},
		{name: "sq-rt-cpu4-240x240", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(240, 240)},
		{name: "sq-rt-cpu8-240x240", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(240, 240)},
		{name: "sq-rt-cpu0-320x320", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(320, 320)},
		{name: "sq-rt-cpu4-320x320", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(320, 320)},
		{name: "sq-rt-cpu4-400x400", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(400, 400)},
		{name: "sq-rt-cpu8-400x400", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(400, 400)},
		{name: "sq-good-cpu4-240x240", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: mk(240, 240)},
		{name: "sq-good-cpu4-320x320", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: mk(320, 320)},

		// (7) Very wide / very tall narrow strips. The MB grid is one
		// row (or column) of MBs, so every MB is a top/bottom edge MB.
		{name: "strip-rt-cpu0-320x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(320, 16)},
		{name: "strip-rt-cpu-3-320x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(320, 16)},
		{name: "strip-rt-cpu4-320x16", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(320, 16)},
		{name: "strip-rt-cpu8-320x16", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(320, 16)},
		{name: "strip-rt-cpu0-16x320", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(16, 320)},
		{name: "strip-rt-cpu-3-16x320", deadline: DeadlineRealtime, cpuUsed: -3, fx: mk(16, 320)},
		{name: "strip-rt-cpu4-16x320", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(16, 320)},
		{name: "strip-rt-cpu8-16x320", deadline: DeadlineRealtime, cpuUsed: 8, fx: mk(16, 320)},
		{name: "strip-rt-cpu0-640x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(640, 32)},
		{name: "strip-rt-cpu4-640x32", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(640, 32)},
		{name: "strip-rt-cpu0-32x640", deadline: DeadlineRealtime, cpuUsed: 0, fx: mk(32, 640)},
		{name: "strip-rt-cpu4-32x640", deadline: DeadlineRealtime, cpuUsed: 4, fx: mk(32, 640)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			opts := EncoderOptions{
				Width:             tc.fx.w,
				Height:            tc.fx.h,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          tc.deadline,
				CpuUsed:           strictByteParityCPUUsed(tc.deadline, tc.cpuUsed),
				Tuning:            TunePSNR,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := libvpxEndUsageArgs(nil)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, targetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := testutil.FirstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := testutil.FirstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}

func panningName(w, h int) string {
	return "panning-" + strconv.Itoa(w) + "x" + strconv.Itoa(h)
}

// TestVP8OracleEncoderStreamByteParityFrameFlags exercises per-frame
// VP8 flag scheduling — the EncodeForceKeyFrame /
// EncodeNoUpdateLast / EncodeNoUpdateGolden / EncodeNoUpdateAltRef /
// EncodeNoReferenceLast / EncodeNoReferenceGolden /
// EncodeNoReferenceAltRef / EncodeForceGoldenFrame /
// EncodeForceAltRefFrame / EncodeNoUpdateEntropy axes that the
// stock vpxenc binary cannot drive. The libvpx-side reference is the
// companion vpxenc-frameflags driver, which translates the same
// per-frame schedule into vpx_codec_encode flags.
//
// Cases that diverge are pinned with `limit:` so the gap is visible
// in the per-frame "byte mismatch (not asserted, ...)" log lines
// without regressing the strict gate.
func TestVP8OracleEncoderStreamByteParityFrameFlags(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 8
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning16 := fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}
	panning48 := fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	panningOdd := fixture{name: "panning-65x33", w: 65, h: 33, source: encoderValidationPanningFrame}

	cases := []struct {
		name                     string
		deadline                 Deadline
		cpuUsed                  int
		fx                       fixture
		limit                    int
		flags                    []EncodeFlags // per-frame; missing indices default to 0.
		rcMode                   RateControlMode
		rcModeSet                bool
		cqLevel                  int
		disableKf                bool
		tokenParts               int
		errorResilient           bool
		errorResilientPartitions bool
		extraArgs                []string // appended to libvpx driver argv.
	}{
		// Force-keyframe at frame 3. Both implementations must emit
		// a keyframe at index 3; the surrounding frames must
		// byte-match too.
		{name: "force-kf-frame3-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-cpu0-48x48", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning48, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		// Force-keyframe at frame 1 (immediately after the initial key).
		{name: "force-kf-frame1-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeForceKeyFrame}},
		{name: "force-kf-frame1-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: []EncodeFlags{0, EncodeForceKeyFrame}},
		// Force-keyframe on every frame (the "all keyframes by flag" axis
		// — orthogonal to the existing `kfInterval=1` axis which uses
		// kf-min/max-dist instead of the per-frame flag).
		{name: "force-kf-every-frame-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame, EncodeForceKeyFrame}},
		// Explicit force-key requests still win when automatic
		// keyframes are disabled from construction.
		{name: "disable-kf-force-kf-frame3-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, disableKf: true, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}, extraArgs: []string{"--kf-disabled"}},
		// Hidden packets clear VP8 show_frame while keeping the encoded
		// reference update byte-identical to libvpx.
		{name: "invisible-keyframe-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{EncodeInvisibleFrame}},
		{name: "invisible-inter-frame1-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeInvisibleFrame}},
		{name: "invisible-inter-run-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeInvisibleFrame, EncodeInvisibleFrame, 0, EncodeInvisibleFrame}},
		{name: "invisible-no-upd-entropy-no-ref-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeInvisibleFrame | EncodeNoUpdateEntropy | EncodeNoUpdateLast | EncodeNoReferenceGolden | EncodeNoReferenceAltRef}},
		{name: "invisible-force-gf-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, EncodeInvisibleFrame | EncodeForceGoldenFrame, 0, EncodeInvisibleFrame | EncodeForceAltRefFrame}},
		{name: "invisible-force-kf-frame3-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, EncodeInvisibleFrame | EncodeForceKeyFrame}},
		{name: "invisible-no-upd-all-token8-er3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 3, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeInvisibleFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "invisible-no-upd-entropy-no-upd-all-token4-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeInvisibleFrame|EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "invisible-force-gf-arf-no-upd-entropy-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: []EncodeFlags{0, 0, EncodeInvisibleFrame | EncodeForceGoldenFrame | EncodeNoUpdateEntropy, 0, EncodeInvisibleFrame | EncodeForceAltRefFrame | EncodeNoUpdateEntropy}},

		// EncodeNoUpdateLast on every inter frame — exercises the
		// "freeze LAST" pattern used by WebRTC scalability layers.
		// libvpx vp8_cx_iface vp8e_set_frame_flags routes any of
		// {NO_UPD_LAST, NO_UPD_GF, NO_UPD_ARF, FORCE_GF, FORCE_ARF}
		// through vp8_update_reference with an inverted "upd" mask
		// (start at all-three, XOR off each NO_UPD_*); govpx mirrors
		// the same mask via libvpxExternalRefreshMask so the
		// downstream refresh / sourceAltRefActive bookkeeping lines
		// up on the next frame.
		{name: "no-upd-last-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast}},
		{name: "no-upd-last-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: []EncodeFlags{0, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast, EncodeNoUpdateLast}},
		// EncodeNoUpdateGolden / EncodeNoUpdateAltRef on every inter
		// frame. Together with the existing temporal SVC parity report
		// tests these pin the per-flag refresh accounting through
		// the libvpx upd-mask semantics described above.
		{name: "no-upd-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateGolden)},
		{name: "no-upd-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateAltRef)},
		{name: "no-upd-gf-arf-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-all-every-inter-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},

		// EncodeNoReferenceGolden / EncodeNoReferenceAltRef — drop
		// the GF/ARF reference from the inter prediction pool. The
		// picker must fall back to LAST-only, which is what WebRTC
		// scalability uses to prevent cross-layer dependencies.
		{name: "no-ref-last-every-inter-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast)},
		{name: "no-ref-last-every3-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: everyNFlag(frames, 3, EncodeNoReferenceLast)},
		{name: "no-ref-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceGolden)},
		{name: "no-ref-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceAltRef)},
		{name: "no-ref-gf-arf-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-ref-all-every-inter-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},

		// Combined no-update-Last+no-reference-Golden, the canonical
		// "base temporal layer" pattern from libvpx's
		// vpx_temporal_svc_encoder mode 1.
		{name: "no-upd-last-no-ref-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-ref-last-no-upd-gf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast|EncodeNoUpdateGolden)},
		{name: "no-ref-last-no-upd-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoReferenceLast|EncodeNoUpdateAltRef)},
		{name: "no-ref-gf-no-upd-last-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoReferenceGolden|EncodeNoUpdateLast)},
		{name: "no-ref-arf-no-upd-gf-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoReferenceAltRef|EncodeNoUpdateGolden)},

		// EncodeForceGoldenFrame / EncodeForceAltRefFrame at a
		// specific frame, mirroring the "manual GF/ARF refresh"
		// pattern. libvpx's upd-mask interpretation here is the
		// surprising part: with no NO_UPD_* bits set the mask stays
		// at 7 so refresh_last_frame, refresh_golden_frame and
		// refresh_alt_ref_frame ALL flip to 1 on the forced frame
		// (independent of which of FORCE_GF / FORCE_ARF the user
		// requested). libvpxExternalRefreshMask reproduces that.
		{name: "force-gf-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame}},
		{name: "force-gf-frame4-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame}},
		{name: "force-gf-no-upd-last-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame | EncodeNoUpdateLast}},
		{name: "force-gf-every3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: everyNFlag(frames, 3, EncodeForceGoldenFrame)},
		{name: "force-arf-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceAltRefFrame}},
		{name: "force-arf-no-upd-last-gf-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceAltRefFrame | EncodeNoUpdateLast | EncodeNoUpdateGolden}},
		{name: "force-arf-every3-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: everyNFlag(frames, 3, EncodeForceAltRefFrame)},
		{name: "force-gf-arf-same-frame-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame | EncodeForceAltRefFrame}},
		{name: "force-gf-no-upd-entropy-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceGoldenFrame | EncodeNoUpdateEntropy}},
		{name: "force-arf-no-upd-entropy-frame4-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceAltRefFrame | EncodeNoUpdateEntropy}},

		// EncodeNoUpdateEntropy on every inter frame — keeps the
		// reference entropy adaptation state frozen.
		{name: "no-upd-entropy-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy)},
		{name: "no-upd-entropy-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy)},
		{name: "no-upd-entropy-realtime-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy)},
		{name: "force-kf-no-upd-last-entropy-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame | EncodeNoUpdateLast | EncodeNoUpdateEntropy}},
		{name: "no-upd-entropy-no-upd-all-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-all-er2-token8-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 3, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-entropy-no-upd-all-er3-token8-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 3, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-entropy-no-upd-all-er3-token8-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, tokenParts: 3, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}, flags: repeatFlag(frames-1, EncodeNoUpdateEntropy|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "invisible-no-ref-all-odd-dims-realtime-cpu-3-65x33", deadline: DeadlineRealtime, cpuUsed: -3, fx: panningOdd, flags: repeatFlag(frames-1, EncodeInvisibleFrame|EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-upd-last-no-ref-gf-odd-dims-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, flags: repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},
		{name: "no-ref-gf-no-upd-last-token4-odd-dims-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeNoReferenceGolden|EncodeNoUpdateLast)},
		{name: "force-gf-arf-token4-odd-dims-realtime-cpu0-65x33", deadline: DeadlineRealtime, cpuUsed: 0, fx: panningOdd, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: []EncodeFlags{0, 0, EncodeForceGoldenFrame, 0, EncodeForceAltRefFrame}},

		// Force-KF + no-update-GF/ARF (the layer-0 "I-frame anchor"
		// pattern used by 3-layer SVC mode 4 in libvpx's example).
		{name: "force-kf-no-upd-gf-arf-realtime-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{EncodeForceKeyFrame | EncodeNoUpdateGolden | EncodeNoUpdateAltRef}},
		{name: "force-kf-frame3-realtime-vbr-cpu-3-16x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame3-realtime-q-cpu-3-16x16-q20", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},

		// Token-partitions=2 / 4 crossed with per-frame flags to
		// confirm the partitioned writer also honors per-frame refs.
		{name: "force-kf-frame3-realtime-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "no-upd-last-realtime-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, tokenParts: 2, extraArgs: []string{"--token-parts=2"}, flags: repeatFlag(frames-1, EncodeNoUpdateLast)},

		// Good-quality deadline + per-frame flags to widen the
		// picker's mode-decision coverage.
		{name: "force-kf-frame3-good-quality-cpu4-32x32", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning32, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}},
		{name: "no-upd-last-good-quality-cpu4-16x16", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning16, flags: repeatFlag(frames-1, EncodeNoUpdateLast)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			rcMode := tc.rcMode
			if !tc.rcModeSet {
				rcMode = RateControlCBR
			}
			opts := EncoderOptions{
				Width:                    tc.fx.w,
				Height:                   tc.fx.h,
				FPS:                      fps,
				RateControlMode:          rcMode,
				TargetBitrateKbps:        targetKbps,
				MinQuantizer:             4,
				MaxQuantizer:             56,
				CQLevel:                  tc.cqLevel,
				KeyFrameInterval:         999,
				Deadline:                 tc.deadline,
				CpuUsed:                  tc.cpuUsed,
				Tuning:                   TunePSNR,
				TokenPartitions:          tc.tokenParts,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientPartitions,
			}
			if tc.disableKf {
				opts.KeyFrameInterval = 0
			}

			govpxFrames := encodeFramesWithGovpxFrameFlags(t, opts, sources, tc.flags)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, tc.name, opts, targetKbps, sources, tc.flags, tc.extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					assertStrictGateKnownGapMatchedPrefix(t, tc.name, govpxFrames, libvpxFrames, 1)
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}
			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := testutil.FirstByteDiff(govpxFrames[i], libvpxFrames[i])
				firstNonTagDiff := testutil.FirstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				if i >= limit {
					t.Logf("frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
						i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
					continue
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}

func TestVP8OracleEncoderStreamByteParityForceKeyFrameAPI(t *testing.T) {
	vp8test.RequireOracle(t, "force-key API byte-parity gate")
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		width      = 32
		height     = 16
	)

	cases := []struct {
		name            string
		frames          int
		lookaheadFrames int
		forceFrames     map[int]bool
		mutate          func(*EncoderOptions)
		buildFlags      func(int, map[int]bool) []EncodeFlags
		apiFlags        func([]EncodeFlags) []EncodeFlags
		setup           func(*testing.T, *VP8Encoder)
		runtimeApply    map[int]func(*testing.T, *VP8Encoder)
		controlScript   []string
		extraArgs       []string
		matchLimit      int
	}{
		{
			name:        "no-lookahead-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
		},
		{
			name:            "lookahead2-frame1-and4",
			frames:          8,
			lookaheadFrames: 2,
			forceFrames:     map[int]bool{1: true, 4: true},
			extraArgs:       []string{"--lag-in-frames=2"},
		},
		{
			name:            "lookahead4-frame4-and-flush",
			frames:          10,
			lookaheadFrames: 4,
			forceFrames:     map[int]bool{4: true, 9: true},
			extraArgs:       []string{"--lag-in-frames=4"},
		},
		{
			name:            "lookahead4-auto-alt-ref-frame4-and-flush",
			frames:          10,
			lookaheadFrames: 4,
			forceFrames:     map[int]bool{4: true, 9: true},
			mutate: func(opts *EncoderOptions) {
				opts.AutoAltRef = true
			},
			extraArgs: []string{"--lag-in-frames=4", "--auto-alt-ref=1"},
		},
		{
			name:        "active-map-checker-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			setup:       activeMapApply("checker"),
			extraArgs:   []string{"--active-map=checker"},
		},
		{
			name:        "roi-map-border-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			setup:       roiMapApply("border1"),
			extraArgs:   []string{"--roi-map=border1"},
		},
		{
			name:        "temporal-two-layer-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.TemporalScalability = TemporalScalabilityConfig{
					Enabled:                true,
					Mode:                   TemporalLayeringTwoLayers,
					LayerTargetBitrateKbps: [MaxTemporalLayers]int{420, targetKbps},
				}
			},
			buildFlags: forceKeyTemporalTwoLayerFlags,
			extraArgs: []string{
				"--temporal-layers=2",
				"--temporal-bitrates=420,700",
				"--temporal-decimators=2,1",
				"--temporal-periodicity=2",
				"--temporal-layer-ids=0,1",
			},
		},
		{
			name:        "drop-frame-enabled-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.DropFrameAllowed = true
				opts.DropFrameWaterMark = 60
			},
			extraArgs: []string{"--drop-frame=60"},
		},
		{
			name:        "keyframes-disabled-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.KeyFrameInterval = 0
			},
			extraArgs: []string{"--kf-disabled"},
		},
		{
			name:        "no-update-entropy-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			buildFlags: func(frames int, forceFrames map[int]bool) []EncodeFlags {
				flags := repeatFlag(frames-1, EncodeNoUpdateEntropy)
				for frame := range forceFrames {
					if frame >= 0 && frame < len(flags) {
						flags[frame] |= EncodeForceKeyFrame
					}
				}
				return flags
			},
			apiFlags: forceKeyAPIEncodeFlags,
		},
		{
			name:        "no-update-last-no-ref-gf-arf-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			buildFlags: func(frames int, forceFrames map[int]bool) []EncodeFlags {
				flags := repeatFlag(frames-1, EncodeNoUpdateLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
				for frame := range forceFrames {
					if frame >= 0 && frame < len(flags) {
						flags[frame] |= EncodeForceKeyFrame
					}
				}
				return flags
			},
			apiFlags: forceKeyAPIEncodeFlags,
		},
		{
			name:        "force-golden-altref-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			buildFlags: func(frames int, forceFrames map[int]bool) []EncodeFlags {
				flags := make([]EncodeFlags, frames)
				for frame := range forceFrames {
					if frame >= 0 && frame < len(flags) {
						flags[frame] = EncodeForceKeyFrame | EncodeForceGoldenFrame | EncodeForceAltRefFrame
					}
				}
				return flags
			},
			apiFlags: forceKeyAPIEncodeFlags,
		},
		{
			name:        "rtc-external-frame1-and4",
			frames:      8,
			forceFrames: map[int]bool{1: true, 4: true},
			mutate: func(opts *EncoderOptions) {
				opts.RTCExternalRateControl = true
			},
			extraArgs: []string{"--rtc-external=1"},
		},
		{
			name:        "set-reference-before-force-keyframe",
			frames:      8,
			forceFrames: map[int]bool{3: true, 6: true},
			runtimeApply: map[int]func(*testing.T, *VP8Encoder){
				2: setReferencePanningApply(ReferenceLast, 8, "last"),
				5: setReferencePanningApply(ReferenceGolden, 9, "golden"),
			},
			controlScript: runtimeControlScript(8, map[int]string{
				2: "setref:last:panning:8",
				5: "setref:golden:panning:9",
			}),
		},
		{
			name:        "rtc-external-set-reference-before-force-keyframe",
			frames:      8,
			forceFrames: map[int]bool{3: true, 6: true},
			runtimeApply: map[int]func(*testing.T, *VP8Encoder){
				1: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetRTCExternalRateControl(true)", e.SetRTCExternalRateControl(true))
				},
				2: setReferencePanningApply(ReferenceLast, 8, "last"),
				5: setReferencePanningApply(ReferenceGolden, 9, "golden"),
			},
			controlScript: runtimeControlScript(8, map[int]string{
				1: "rtc:1",
				2: "setref:last:panning:8",
				5: "setref:golden:panning:9",
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, tc.frames)
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
				LookaheadFrames:   tc.lookaheadFrames,
			}
			if tc.mutate != nil {
				tc.mutate(&opts)
			}
			flags := make([]EncodeFlags, tc.frames)
			if tc.buildFlags != nil {
				flags = tc.buildFlags(tc.frames, tc.forceFrames)
			} else {
				for frame := range tc.forceFrames {
					flags[frame] = EncodeForceKeyFrame
				}
			}

			apiFlags := make([]EncodeFlags, len(flags))
			if tc.apiFlags != nil {
				apiFlags = tc.apiFlags(flags)
			}
			govpxFrames := encodeFramesWithGovpxForceKeyScheduleFlagsSetupAndApply(t, opts, sources, tc.forceFrames, apiFlags, tc.setup, tc.runtimeApply)
			extraArgs := append([]string(nil), tc.extraArgs...)
			if tc.controlScript != nil {
				extraArgs = append(extraArgs, "--control-script="+strings.Join(tc.controlScript, ","))
			}
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "force-key-api-"+tc.name, opts, opts.TargetBitrateKbps, sources, flags, extraArgs)
			assertSegmentByteParity(t, "force-key-api", govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}

func strictByteParityCPUUsed(deadline Deadline, cpuUsed int) int {
	if deadline == DeadlineRealtime && cpuUsed > 0 {
		// Positive realtime cpu-used is libvpx's wall-clock adaptive
		// auto-speed mode. Strict byte-parity cases pin the requested
		// speed explicitly so govpx and libvpx make matching encoder
		// decisions on every machine.
		return -cpuUsed
	}
	return cpuUsed
}

// extraArgsContainsKFDist reports whether the caller already supplied a
// `--kf-min-dist` or `--kf-max-dist` (with either an `=` form or the
// space-separated `--name value` form) in extraArgs, so the default
// `--kf-min-dist=999 --kf-max-dist=999` "disable auto-KF" pair shouldn't
// be appended on top.
func extraArgsContainsKFDist(extraArgs []string) bool {
	for _, arg := range extraArgs {
		switch {
		case arg == "--kf-min-dist", arg == "--kf-max-dist":
			return true
		case len(arg) >= len("--kf-min-dist=") && arg[:len("--kf-min-dist=")] == "--kf-min-dist=":
			return true
		case len(arg) >= len("--kf-max-dist=") && arg[:len("--kf-max-dist=")] == "--kf-max-dist=":
			return true
		}
	}
	return false
}

func libvpxEndUsageArgs(extraArgs []string) []string {
	for _, arg := range extraArgs {
		if arg == "--end-usage" || len(arg) >= len("--end-usage=") && arg[:len("--end-usage=")] == "--end-usage=" {
			return extraArgs
		}
	}
	args := make([]string, 0, len(extraArgs)+1)
	args = append(args, "--end-usage=cbr")
	args = append(args, extraArgs...)
	return args
}

// encodeFramesWithGovpx returns the raw per-frame VP8 packet payloads
// produced by govpx for the supplied sources. Dropped frames (CBR
// decimation drops, buffer-underrun drops, vp8_drop_encodedframe_overshoot
// drops) leave no payload in the returned slice, mirroring libvpx's
// observable output where a drop produces no IVF packet for that source
// frame. Callers that need to validate a specific drop pattern compare
// the returned slice against the libvpx oracle's IVF packet list.
func encodeFramesWithGovpx(t *testing.T, opts EncoderOptions, sources []Image) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	appendResult := func(_ string, result EncodeResult) {
		if result.Dropped {
			return
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	for i, src := range sources {
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		appendResult("EncodeInto frame "+strconv.Itoa(i), result)
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		appendResult("FlushInto", result)
	}
	return out
}

// encodeFramesWithLibvpxOracle runs vpxenc-oracle on the supplied I420
// fixture and returns the per-frame VP8 packet payloads extracted from
// the resulting IVF file.
func encodeFramesWithLibvpxOracle(t *testing.T, vpxencOracle string, _ string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string) [][]byte {
	t.Helper()
	cfg := vp8test.VpxencVP8Config{
		BinaryPath:           vpxencOracle,
		Width:                opts.Width,
		Height:               opts.Height,
		Frames:               len(sources),
		Deadline:             libvpxOracleDeadline(opts.Deadline),
		DisableWarningPrompt: true,
		CPUUsed:              opts.CpuUsed,
		LagInFrames:          opts.LookaheadFrames,
		AutoAltRef:           opts.AutoAltRef,
		TargetBitrateKbps:    targetKbps,
		MinQ:                 opts.MinQuantizer,
		MaxQ:                 opts.MaxQuantizer,
		Timebase:             libvpxOracleTimebaseArg(opts),
		FPS:                  libvpxOracleFPSArg(opts),
		KeyFrameMinDist:      999,
		KeyFrameMaxDist:      999,
		ExtraArgs:            extraArgs,
	}
	// Only inject the default `--kf-min-dist=999 --kf-max-dist=999`
	// "no auto-KF" pair when the caller hasn't supplied its own kf-*
	// arguments via extraArgs. Several callers (long-fixture fuzz,
	// production parity, transitions, twopass fuzz, runtime-controls
	// parity) configure a finite KeyFrameInterval on the govpx side
	// and need libvpx's `cpi->key_frame_frequency` to match; passing
	// the default 999/999 silently in those cases would force govpx
	// to insert a keyframe at frame `KeyFrameInterval` while libvpx
	// keeps producing inter frames.
	if !extraArgsContainsKFDist(extraArgs) {
		cfg.KeyFrameDistSet = true
	}
	frames, diag, err := vp8test.VpxencVP8OracleFramePayloadsI420(
		encoderValidationI420Bytes(t, sources), cfg)
	if err != nil {
		t.Fatalf("vpxenc-oracle failed: %v\n%s", err, diag)
	}
	return frames
}

func libvpxOracleDeadline(deadline Deadline) string {
	switch deadline {
	case DeadlineBestQuality:
		return "best"
	case DeadlineRealtime:
		return "rt"
	default:
		return "good"
	}
}

func libvpxOracleTimebaseArg(opts EncoderOptions) string {
	if opts.TimebaseNum > 0 && opts.TimebaseDen > 0 {
		return strconv.Itoa(opts.TimebaseNum) + "/" + strconv.Itoa(opts.TimebaseDen)
	}
	return "1/" + strconv.Itoa(opts.FPS)
}

func libvpxOracleFPSArg(opts EncoderOptions) string {
	if opts.TimebaseNum > 0 && opts.TimebaseDen > 0 {
		return strconv.Itoa(opts.TimebaseDen) + "/" + strconv.Itoa(opts.TimebaseNum)
	}
	return strconv.Itoa(opts.FPS) + "/1"
}

// parseVP8FramePartitionSizes returns the first-partition byte length
// declared in the VP8 frame header plus whether the frame is marked as
// a keyframe. Returns (0, false) when the payload is too short.
func parseVP8FramePartitionSizes(p []byte) (firstPart int, isKeyframe bool) {
	if len(p) < 3 {
		return 0, false
	}
	tag := uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16
	isKeyframe = (tag & 1) == 0
	firstPart = int((tag >> 5) & 0x7FFFF)
	return firstPart, isKeyframe
}
