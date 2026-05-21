//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestVP8RealtimePickerCPUFastpathParity pins task #244: the RT-mode
// vp8_pick_inter_mode fast-path gates for cpu_used = -12, -8, -4. These are
// the negative-cpu_used values that bypass vp8_auto_select_speed in libvpx
// encodeframe.c:686-687 and pin cpi->Speed = -cpu_used (positive 12, 8, 4
// respectively). At Speed > 3 the libvpx RT picker is the SOLE mode-
// decision path (sf->RD = 0 in onyx_if.c:946-949 Mode==2 branch), so any
// gate divergence in pickinter.c surfaces as immediate byte mismatch.
//
// LIBVPX REFERENCES (v1.16.0):
//
//	vp8/encoder/pickinter.c:753-757   cpi->Speed < 12 zeromv_rd_adjustment gate
//	vp8/encoder/pickinter.c:929       speed_adjust = (Speed > 5)
//	                                    ? ((Speed >= 8) ? 3 : 2)
//	                                    : 1
//	vp8/encoder/pickinter.c:1005-1008 further_steps = (Speed >= 8) ? 0
//	                                    : (sf->max_step_search_steps - 1
//	                                       - step_param)
//	vp8/encoder/onyx_if.c:768-1087    vp8_set_speed_features (Speed > 6 →
//	                                    improved_mv_pred = 0; Speed > 4 →
//	                                    search_method = HEX; Speed > 8 →
//	                                    quarter_pixel_search = 0;
//	                                    Speed >= 15 → half_pixel_search = 0)
//	vp8/encoder/encodeframe.c:685-690 cpi->Speed = -(cpi->oxcf.cpu_used)
//	                                    (negative-cpu RT explicit-Speed
//	                                    branch, skips vp8_auto_select_speed)
//
// GOVPX MIRRORS:
//
//	vp8_encoder_config.go libvpxCPUUsed: negative-cpu RT falls back to
//	  libvpxSpeedFeatureCPUUsed → -cpu_used (positive), matching libvpx's
//	  encodeframe.c:686-687 pin.
//	vp8_encoder_inter_speed.go interAnalysisSearchConfig: builds the fast-
//	  picker search config from libvpxCPUUsed(). All four pickinter.c
//	  gates above mirror in: libvpxInterFrameSpeedAdjust (line 929),
//	  libvpxInterFrameFurtherSteps (line 1006), the speed>4/>8/>=15
//	  hex/half/skip cascade (onyx_if.c Mode==2 lines 953/1012/1023), and
//	  libvpxInterFrameImprovedMVPredictionForFeatureSpeed (Speed>6 gate).
//	vp8_encoder_inter_rd.go fastZeroMVLastAdjustmentEligible: gates the
//	  pickinter.c:756 (cpi->Speed < 12) zeromv_rd_adjustment cutoff via
//	  libvpxCPUUsed() >= 12, matching the Speed=12 cutoff exactly for
//	  cpu_used = -12.
//
// AUDIT FINDING (this task):
//
// No divergence. govpx's gates match libvpx verbatim at all three cpu
// values. Byte-parity is asserted across cpu_used=-12, -8, -4 at 320x240
// CBR / RT / 8 panning frames against the libvpx oracle. Task #232's
// improved-MV further_steps fix (commit 8d313a30) had already pinned the
// only pickinter.c divergence in this cpu range; the remaining picker
// gates were already faithful from task #226's frame-6 audit and earlier.
//
// This test exists as a regression sentinel: a future change to the
// negative-cpu_used / Speed mapping in libvpxCPUUsed,
// libvpxSpeedFeatureCPUUsed, libvpxInterFrameSpeedAdjust,
// libvpxInterFrameFurtherSteps, libvpxInterFrameImprovedMVPrediction*, or
// fastZeroMVLastAdjustmentEligible would re-open this audit and surface
// here before it cascades into the option-grid fuzz cohort.
//
// TASK REFERENCES:
//
//   - task #232 (commit 8d313a30) RD-vs-picker further_steps split
//   - task #239 (commit c7efc12d) RD-vs-picker NEWMV step_param split
//   - task #226 (frame-6 picker audit)
func TestVP8RealtimePickerCPUFastpathParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run realtime picker fast-path parity")
	}
	vpxencOracle := coracletest.VpxencOracle(t)

	cases := []struct {
		name    string
		cpuUsed int
	}{
		{name: "rt-cpu_used-minus12-fastest", cpuUsed: -12},
		{name: "rt-cpu_used-minus8-very-fast", cpuUsed: -8},
		{name: "rt-cpu_used-minus4-moderate", cpuUsed: -4},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			opts := EncoderOptions{
				Width:             320,
				Height:            240,
				FPS:               30,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: 600,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineRealtime,
				CpuUsed:           c.cpuUsed,
			}
			extraArgs := libvpxEndUsageArgs([]string{"--end-usage=cbr"})

			sources := make([]Image, 8)
			for i := range sources {
				sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "realtime-picker-fastpath-"+c.name, opts, 600, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				t.Fatalf("frame count drift: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}
			if len(govpxFrames) == 0 {
				t.Fatalf("no frames produced")
			}

			for i := range govpxFrames {
				if !bytes.Equal(govpxFrames[i], libvpxFrames[i]) {
					diff := -1
					n := min(len(govpxFrames[i]), len(libvpxFrames[i]))
					for k := 0; k < n; k++ {
						if govpxFrames[i][k] != libvpxFrames[i][k] {
							diff = k
							break
						}
					}
					t.Fatalf("cpu_used=%d frame %d byte mismatch: first_diff=%d gov_len=%d lib_len=%d",
						c.cpuUsed, i, diff, len(govpxFrames[i]), len(libvpxFrames[i]))
				}
			}

			t.Logf("realtime picker fast path: cpu_used=%d %dx%d %d frames byte-MATCH (no pickinter.c gate divergence)",
				c.cpuUsed, opts.Width, opts.Height, len(govpxFrames))
		})
	}
}
