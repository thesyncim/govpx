//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

// TestOracleEncoderStreamByteParityDimensions widens the strict byte-parity
// matrix along the *resolution axis* that the base
// [TestOracleEncoderStreamByteParity] and
// [TestOracleEncoderStreamByteParityExtended] matrices do not exhaustively
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
func TestOracleEncoderStreamByteParityDimensions(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := coracletest.VpxencOracle(t)

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
