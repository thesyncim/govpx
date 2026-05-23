//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"os"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8ThreadsValidation validates that the breakoutSkip gate matches libvpx
// and yields byte-exact frame 1 parity on the BestARNR / GoodARNR ARNR cohorts
// at threads=1, threads=2, and threads=4. The incorrect gate was:
//
//	breakoutSkip = !intra && (picker.MBSkipCoeff || staticBreakout)
//
// conflated libvpx's two real x->skip=1 sources (active_map_enabled+
// inactive and static encode_breakout) with the picker's downstream
// mbmi.mb_skip_coeff signal from tteob==0 (which only adjusts rate
// accounting at calculate_final_rd_costs and does NOT gate
// vp8_encode_inter16x16). The post-fix gate
//
//	breakoutSkip = !intra && (interMacroblockInactive || staticBreakout)
//
// matches libvpx vp8/encoder/encodeframe.c vp8cx_encode_inter_macroblock
// (line 1275-1281) and rdopt.c evaluate_inter_mode_rd (rdopt.c:1607-1608
// for inactive, 1620-1628 for encode_breakout).
func TestVP8ThreadsValidation(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP8 threaded parity validation")
	}
	vpxencOracle := vp8test.VpxencOracle(t)
	for _, threads := range []int{1, 2, 4} {
		t.Run("threads="+strconv.Itoa(threads), func(t *testing.T) {
			opts := EncoderOptions{
				Width:             1280,
				Height:            720,
				FPS:               30,
				RateControlMode:   RateControlVBR,
				TargetBitrateKbps: 700,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineBestQuality,
				CpuUsed:           0,
				Tuning:            TuneSSIM,
				ScreenContentMode: 1,
				TokenPartitions:   1,
				Threads:           threads,
				ARNRMaxFrames:     1,
				ARNRStrength:      1,
				ARNRType:          2,
			}
			extraArgs := libvpxEndUsageArgs([]string{
				"--end-usage=vbr",
				"--screen-content-mode=1",
				"--token-parts=1",
				"--threads=" + strconv.Itoa(threads),
				"--tune=ssim",
				"--arnr-maxframes=1",
				"--arnr-strength=1",
				"--arnr-type=2",
			})
			sources := make([]Image, 2)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
			}
			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			// At threads=2 and threads=4 the libvpx oracle is exposed to
			// MT-LF non-determinism. The reproducibility wrapper makes any
			// oracle-side flake visible as a SHA mismatch instead of letting
			// it taint this cross-thread parity check.
			libvpxFrames := encodeVP8FramesWithLibvpxOracleReproducible(t, vpxencOracle, "threaded-parity-validation", opts, 700, sources, extraArgs, VP8OracleReproducibleRuns)
			for i := range govpxFrames {
				gs := sha256.Sum256(govpxFrames[i])
				ls := sha256.Sum256(libvpxFrames[i])
				if gs != ls {
					t.Errorf("threads=%d frame %d: SHA mismatch govpx_len=%d libvpx_len=%d", threads, i, len(govpxFrames[i]), len(libvpxFrames[i]))
				}
				t.Logf("threads=%d frame %d: govpx_len=%d libvpx_len=%d match=%v", threads, i, len(govpxFrames[i]), len(libvpxFrames[i]), gs == ls)
			}
		})
	}
}
