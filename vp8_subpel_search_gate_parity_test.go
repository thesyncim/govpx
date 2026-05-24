package govpx

import "testing"

// TestVP8SubPelSearchGatePreservesNegativeCPUUsedCascade pins that
// the new libvpxRealtimeCPISpeedForSubPelSearchGate helper preserves the
// negative-cpu_used (explicit cpi->Speed) fractional-search cascade
// verbatim. Every cpu_used in [-3, -16] must produce the same
// interAnalysisSearchConfig.fractionalSearch dispatch as before this gate
// was split, matching libvpx's `Speed > 4 → Step`, `Speed > 8 → Half`,
// `Speed >= 15 → Skip` cascade at vp8/encoder/onyx_if.c:954/1012/1023.
//
// The targeted-gate pattern mirrors
// libvpxRealtimeCPISpeedForImprovedMVPredGate and only kicks in when
// libvpxEffectiveCPUUsed > 0 (positive cpu_used RT, auto-select path)
// AND frameCount > 0. Negative cpu_used / explicit-Speed RT therefore
// returns the unchanged libvpxCPUUsed() and inherits the existing
// dispatch — guarding TestVP8ExtremeCPUUsedHalfPixelStaysEnabled and the broader
// TestVP8SpeedFeaturesPickInterMirrorsLibvpxRTSpeedCascade pins.
func TestVP8SubPelSearchGatePreservesNegativeCPUUsedCascade(t *testing.T) {
	cases := []struct {
		cpiSpeed int
		want     interAnalysisFractionalSearchMethod
	}{
		// Speed <= 4: iterative_sub_pixel=1 → Iterative.
		{cpiSpeed: 3, want: interAnalysisFractionalSearchIterative},
		{cpiSpeed: 4, want: interAnalysisFractionalSearchIterative},
		// Speed > 4 (line 954): iterative_sub_pixel=0; quarter/half on →
		// vp8_find_best_sub_pixel_step → Step.
		{cpiSpeed: 5, want: interAnalysisFractionalSearchStep},
		{cpiSpeed: 6, want: interAnalysisFractionalSearchStep},
		{cpiSpeed: 7, want: interAnalysisFractionalSearchStep},
		{cpiSpeed: 8, want: interAnalysisFractionalSearchStep},
		// Speed > 8 (line 1012): quarter_pixel_search=0; half on →
		// vp8_find_best_half_pixel_step → Half.
		{cpiSpeed: 9, want: interAnalysisFractionalSearchHalf},
		{cpiSpeed: 10, want: interAnalysisFractionalSearchHalf},
		{cpiSpeed: 12, want: interAnalysisFractionalSearchHalf},
		{cpiSpeed: 14, want: interAnalysisFractionalSearchHalf},
		// Speed >= 15 (line 1023): half_pixel_search=0 → vp8_skip_
		// fractional_mv_step → Skip.
		{cpiSpeed: 15, want: interAnalysisFractionalSearchSkip},
		{cpiSpeed: 16, want: interAnalysisFractionalSearchSkip},
	}
	for _, tc := range cases {
		cpuUsed := -tc.cpiSpeed
		t.Run(cpuUsedTag(cpuUsed), func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: cpuUsed}}
			if got := e.libvpxCPUUsed(); got != tc.cpiSpeed {
				t.Fatalf("libvpxCPUUsed() = %d, want %d", got, tc.cpiSpeed)
			}
			// The new gate must NOT alter behavior for negative cpu_used.
			if got := e.libvpxRealtimeCPISpeedForSubPelSearchGate(); got != tc.cpiSpeed {
				t.Errorf("libvpxRealtimeCPISpeedForSubPelSearchGate() = %d, want %d (negative cpu_used must return libvpxCPUUsed unchanged)", got, tc.cpiSpeed)
			}
			cfg := e.interAnalysisSearchConfig()
			if got := cfg.fractionalSearch; got != tc.want {
				t.Errorf("fractionalSearch = %d, want %d (libvpx onyx_if.c:1064-1071 after 954/1012/1023)", got, tc.want)
			}
		})
	}
}

// TestVP8SubPelSearchGateColdStartUnchanged pins that the new
// gate returns libvpxCPUUsed() unchanged at the cold-start frame
// (frameCount == 0). The auto-select Speed path is observed only after
// frame 0 commits in libvpx; the keyframe encode runs at the seeded
// cpi->Speed = oxcf.cpu_used (libvpx onyx_if.c:1706 seed at
// vp8_create_compressor + every vp8_change_config). Preserving the cold-
// start dispatch keeps the keyframe encode aligned with the inter-frame
// wall-clock pin.
func TestVP8SubPelSearchGateColdStartUnchanged(t *testing.T) {
	for cpuUsed := 1; cpuUsed <= 8; cpuUsed++ {
		t.Run(cpuUsedTag(cpuUsed), func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: cpuUsed}}
			// frameCount == 0 (zero value) at construction.
			if got := e.libvpxRealtimeCPISpeedForSubPelSearchGate(); got != e.libvpxCPUUsed() {
				t.Errorf("cold-start gate = %d, want libvpxCPUUsed()=%d (cpu_used=%d, frameCount=0)", got, e.libvpxCPUUsed(), cpuUsed)
			}
		})
	}
}

// TestVP8SubPelSearchGateRealisticSpeedAfterColdStart pins that
// at cpu_used > 0 RT after frameCount > 0, the gate returns the
// libvpx-realistic cpi->Speed convergence point (cpu_used+1 capped at
// 16). This is the audit-observed trajectory from the 720p RT cpu=8
// per-frame trace (cpi_speed=9 at frame 2). When the libvpx-
// realistic Speed exceeds 4 / 8 / 15 the fractional dispatch must
// promote to Step / Half / Skip respectively, mirroring the libvpx
// cascade gates at vp8/encoder/onyx_if.c:954/1012/1023.
//
// Note: govpx's e.autoSpeed seeds at cpu_used on frame 0 (see
// libvpxAutoSelectSpeed cold-start branch in vp8_encoder_config.go:715),
// then evolves under the inter-frame wall-clock pin. The gate's job is to
// override that pin-suppressed value with the libvpx-realistic cpu_used+1
// trajectory specifically for the fractional sub-pel dispatch, leaving every
// other speed-feature lookup on the actual e.autoSpeed.
func TestVP8SubPelSearchGateRealisticSpeedAfterColdStart(t *testing.T) {
	cases := []struct {
		cpuUsed       int
		wantSpeed     int
		wantFractnl   interAnalysisFractionalSearchMethod
		gateThreshold string
	}{
		// cpu_used=1 → realistic Speed=2 (still below Speed > 4) → Step
		// would NOT fire on realistic Speed alone, but govpx's autoSpeed
		// seed = cpu_used = 1, then the autoSpeed cold-start branch in
		// vp8_auto_select_speed sets autoSpeed = 4 on first frame.
		// max(autoSpeed=4, realistic=2) = 4. At Speed == 4, the gate is
		// `Speed > 4` (strict), so fractional stays Iterative. Note this
		// matches libvpx at cpi->Speed=4 (line 954 `Speed > 4` does NOT
		// fire).
		{cpuUsed: 1, wantSpeed: 4, wantFractnl: interAnalysisFractionalSearchIterative, gateThreshold: "Speed=4 → no gate fired"},
		// cpu_used=4 → realistic = min(4+1, 16) = 5. max(autoSpeed=4,
		// realistic=5) = 5. Speed > 4 fires → Step.
		{cpuUsed: 4, wantSpeed: 5, wantFractnl: interAnalysisFractionalSearchStep, gateThreshold: "Speed=5 > 4 → Step"},
		// cpu_used=7 → realistic = 8. Speed > 4 fires; Speed > 8 does
		// not (strict) → Step.
		{cpuUsed: 7, wantSpeed: 8, wantFractnl: interAnalysisFractionalSearchStep, gateThreshold: "Speed=8 > 4 → Step"},
		// cpu_used=8 → realistic = 9. Speed > 8 fires → Half. This is
		// the audit-observed cpi_speed=9 trajectory.
		{cpuUsed: 8, wantSpeed: 9, wantFractnl: interAnalysisFractionalSearchHalf, gateThreshold: "Speed=9 > 8 → Half"},
		// cpu_used=10 → realistic = 11. Speed > 8 fires → Half.
		{cpuUsed: 10, wantSpeed: 11, wantFractnl: interAnalysisFractionalSearchHalf, gateThreshold: "Speed=11 > 8 → Half"},
		// cpu_used=14 → realistic = 15. Speed >= 15 fires → Skip.
		{cpuUsed: 14, wantSpeed: 15, wantFractnl: interAnalysisFractionalSearchSkip, gateThreshold: "Speed=15 >= 15 → Skip"},
		// cpu_used=15 → realistic = 16 (capped). Speed >= 15 fires →
		// Skip.
		{cpuUsed: 15, wantSpeed: 16, wantFractnl: interAnalysisFractionalSearchSkip, gateThreshold: "Speed=16 >= 15 → Skip"},
	}
	for _, tc := range cases {
		t.Run(cpuUsedTag(tc.cpuUsed)+"/"+tc.gateThreshold, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: tc.cpuUsed}}
			// Simulate post-cold-start state: autoSpeed = 4 (libvpx
			// vp8_auto_select_speed cold-start branch), frameCount = 1.
			e.autoSpeed = 4
			e.frameCount = 1
			if got := e.libvpxRealtimeCPISpeedForSubPelSearchGate(); got != tc.wantSpeed {
				t.Errorf("libvpxRealtimeCPISpeedForSubPelSearchGate() = %d, want %d (cpu_used=%d, autoSpeed=4 post-frame-0)", got, tc.wantSpeed, tc.cpuUsed)
			}
			cfg := e.interAnalysisSearchConfig()
			if got := cfg.fractionalSearch; got != tc.wantFractnl {
				t.Errorf("fractionalSearch = %d, want %d (%s; libvpx onyx_if.c:1064-1071)", got, tc.wantFractnl, tc.gateThreshold)
			}
		})
	}
}

// TestVP8SubPelSearchGateCPU0RTUnchanged pins that cpu_used==0
// RT (the byte-parity-gated path) still returns the actual
// libvpxCPUUsed() so the threads=4 cpu=0 RT byte-parity sentinel
// (regression_w854h480_threads4_vbr_inter_diverge,
// regression_w1280h720_threads4_vbr_inter_diverge) keeps fractional
// search on the unchanged dispatch. The gate guard `cpuUsed <= 0`
// covers cpu_used=0 → no realistic cpu_used+1 promotion.
func TestVP8SubPelSearchGateCPU0RTUnchanged(t *testing.T) {
	e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 0}}
	// Even after frame 0 with autoSpeed evolved, the gate must NOT
	// promote past libvpxCPUUsed.
	e.autoSpeed = 6
	e.frameCount = 5
	if got := e.libvpxRealtimeCPISpeedForSubPelSearchGate(); got != e.libvpxCPUUsed() {
		t.Errorf("cpu_used=0 RT gate = %d, want libvpxCPUUsed()=%d (cpu_used=0 byte-parity-gated path)", got, e.libvpxCPUUsed())
	}
}

// TestVP8SubPelSearchGateNonRTUnchanged pins that non-realtime
// deadlines (good / best quality) bypass the gate entirely: the
// fractional dispatch consults the unchanged libvpxCPUUsed(). libvpx's
// vp8_auto_select_speed only fires on case 2 (MODE_REALTIME) — case
// 0/1/3 leave cpi->Speed alone — so there is no cpu_used+1 trajectory
// to mirror on the non-RT paths.
func TestVP8SubPelSearchGateNonRTUnchanged(t *testing.T) {
	for _, deadline := range []Deadline{DeadlineGoodQuality, DeadlineBestQuality} {
		for cpuUsed := 0; cpuUsed <= 8; cpuUsed++ {
			t.Run(string(rune('A'+int(deadline)))+"/"+cpuUsedTag(cpuUsed), func(t *testing.T) {
				e := &VP8Encoder{opts: EncoderOptions{Deadline: deadline, CpuUsed: cpuUsed}}
				e.autoSpeed = 4
				e.frameCount = 3
				if got := e.libvpxRealtimeCPISpeedForSubPelSearchGate(); got != e.libvpxCPUUsed() {
					t.Errorf("non-RT gate = %d, want libvpxCPUUsed()=%d (cpu_used=%d, deadline=%v)", got, e.libvpxCPUUsed(), cpuUsed, deadline)
				}
			})
		}
	}
}
