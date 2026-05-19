package govpx

import (
	"testing"
)

// TestVP8Task247SpeedFeaturesCPUExtremePinsLibvpxCascade pins the verbatim
// libvpx vp8_set_speed_features cascade (onyx_if.c lines 768-1087) for the
// extreme realtime cpu_used tier {-12, -11, -10, -9}, used by libvpx's
// screen-capture / ultra-low-latency RT profile.
//
// libvpx encodeframe.c:686-687 maps cpu_used < 0 to cpi->Speed = -cpu_used,
// so cpu_used=-9 → cpi->Speed=9, cpu_used=-12 → cpi->Speed=12, etc. The
// govpx funnel libvpxCPUUsed() mirrors that abs flip for the negative-
// explicit path; libvpxInterFrameContinuousSpeedForFeatureSpeed then adds
// the +7 RT offset (the libvpx RT(x) = (x)+7 macro at onyx_if.c:689), so
// the continuous Speed values fed to the speed_map lookups are 16/17/18/19
// for cpi->Speed 9/10/11/12.
//
// At cpi->Speed >= 8 every realtime branch is at its terminal state in
// case 2 (line 928):
//
//	sf->optimize_coefficients = 0  (line 929 — unconditional in case 2)
//	sf->recode_loop           = 0  (line 930 — unconditional in case 2)
//	sf->improved_quant        = 0  (line 936 — Speed > 0)
//	sf->improved_dct          = 0  (line 937 — Speed > 0)
//	sf->use_fastquant_for_pick= 1  (line 939 — Speed > 0)
//	sf->no_skip_block4x4_search=0  (line 940 — Speed > 0)
//	sf->first_step            = 1  (line 941 — Speed > 0)
//	sf->auto_filter           = 0  (line 952 — Speed > 4)
//	sf->search_method         = HEX(line 953 — Speed > 4)
//	sf->iterative_sub_pixel   = 0  (line 954 — Speed > 4)
//	sf->RD                    = 0  (line 947 — Speed > 3 → RD off)
//	sf->improved_mv_pred      = 0  (line 1009 — Speed > 6)
//	sf->quarter_pixel_search  = 0  (line 1012 — Speed > 8)
//
// And version 0 keeps NORMAL_LOOPFILTER until cpi->Speed >= 14 (line 1017),
// and sf->half_pixel_search stays 1 until cpi->Speed >= 15 (line 1023).
// Both of those last gates therefore stay OFF (NORMAL filter, half-pixel
// search ON) for the entire cpi->Speed 9..12 tier — the extreme RT cascade
// here is one tier shy of triggering the loop-filter and half-pel
// downgrades. The find_fractional_mv_step dispatch (lines 1064-1071)
// resolves to vp8_find_best_half_pixel_step (Half) for every speed in
// the 9..12 tier because the `Speed > 8` strict-greater gate at line
// 1012 disables quarter_pixel_search starting at Speed=9, and the
// `Speed >= 15` gate at line 1023 has not yet fired so half_pixel_search
// is still 1.
//
// Per-tier expected fractional pick:
//
//	cpi->Speed=9  → quarter=0 (Speed>8) → Half (vp8_find_best_half_pixel_step)
//	cpi->Speed=10 → quarter=0           → Half
//	cpi->Speed=11 → quarter=0           → Half
//	cpi->Speed=12 → quarter=0           → Half
//
// Pickinter step_param / further_steps (libvpx pickinter.c:929/1005-1008)
// is at its terminal value for the whole tier: speed_adjust = 3
// (Speed >= 8), step_param = first_step + 3 = 4, further_steps = 0
// (Speed >= 8 short-circuit).
//
// thresh_mult speed-map lookups at continuous Speed 16..19 sit in the
// tail entries of every map (vp8_set_speed_features lines 700-735),
// driving the extreme RT cascade into its most-aggressive RD-threshold
// state. The picker thresholds are exercised across the two production
// resolutions to ensure the per-resolution wiring (cpi->common.MBs feeds
// the Speed > 6 thresh adaptive override at lines 957-1007) does not
// move the speed-feature state.
//
// The two resolutions exercised are 1280x720 (HD screen capture, the
// libvpx primary RT profile target) and 854x480 (480p mobile, the second
// most common ultra-low-latency profile). Both resolutions must produce
// the same speed-feature state because vp8_set_speed_features is keyed on
// cpi->Speed only — the thresh_mult overrides for Speed > 6 depend on
// totalMBs but the per-mode boolean state (RD, search_method,
// iterative_sub_pixel, etc.) is resolution-independent.
func TestVP8Task247SpeedFeaturesCPUExtremePinsLibvpxCascade(t *testing.T) {
	type want struct {
		optimizeCoefficients bool
		useFastQuant         bool
		useFastQuantForPick  bool
		noSkipBlock4x4       bool
		firstStep            int
		recodeLoop           int
		rdModeDecision       bool
		searchMethodHex      bool
		fractional           interAnalysisFractionalSearchMethod
		autoFilterFastSearch bool // !auto_filter → loopFilterUsesFastSearch
		improvedMVPred       bool
		simpleLoopFilter     bool
		stepParam            int
		furtherSteps         int
		speedAdjust          int
	}
	cases := []struct {
		cpuUsed  int
		cpiSpeed int
		want     want
	}{
		{
			// cpu_used=-9 → cpi->Speed=9. Quarter pixel is DISABLED at the
			// strict `Speed > 8` gate (libvpx onyx_if.c:1012), so even
			// Speed=9 falls through to the half-pixel branch. Half-pixel
			// stays ON (gate is `Speed >= 15`, line 1023). NORMAL_LOOPFILTER
			// (gate is `Speed >= 14`, line 1017). find_fractional_mv_step
			// dispatch (lines 1064-1071): iterative=0, quarter=0,
			// half=1 → vp8_find_best_half_pixel_step → Half.
			cpuUsed: -9, cpiSpeed: 9,
			want: want{
				optimizeCoefficients: false,
				useFastQuant:         true,
				useFastQuantForPick:  true,
				noSkipBlock4x4:       false,
				firstStep:            1,
				recodeLoop:           0,
				rdModeDecision:       false,
				searchMethodHex:      true,
				fractional:           interAnalysisFractionalSearchHalf,
				autoFilterFastSearch: true,
				improvedMVPred:       false,
				simpleLoopFilter:     false,
				stepParam:            4,
				furtherSteps:         0,
				speedAdjust:          3,
			},
		},
		{
			// cpu_used=-10 → cpi->Speed=10. quarter_pixel_search=0 fires
			// (Speed > 8); half-pixel still ON; NORMAL_LOOPFILTER.
			cpuUsed: -10, cpiSpeed: 10,
			want: want{
				optimizeCoefficients: false,
				useFastQuant:         true,
				useFastQuantForPick:  true,
				noSkipBlock4x4:       false,
				firstStep:            1,
				recodeLoop:           0,
				rdModeDecision:       false,
				searchMethodHex:      true,
				fractional:           interAnalysisFractionalSearchHalf,
				autoFilterFastSearch: true,
				improvedMVPred:       false,
				simpleLoopFilter:     false,
				stepParam:            4,
				furtherSteps:         0,
				speedAdjust:          3,
			},
		},
		{
			// cpu_used=-11 → cpi->Speed=11. Same terminal SF state as
			// Speed=10 (the cpi->Speed=10 RT-mode mode_check_freq[NEW1]
			// override at libvpx onyx_if.c:877-879 is the only Speed=10
			// specialty — it does not change the boolean SF state).
			cpuUsed: -11, cpiSpeed: 11,
			want: want{
				optimizeCoefficients: false,
				useFastQuant:         true,
				useFastQuantForPick:  true,
				noSkipBlock4x4:       false,
				firstStep:            1,
				recodeLoop:           0,
				rdModeDecision:       false,
				searchMethodHex:      true,
				fractional:           interAnalysisFractionalSearchHalf,
				autoFilterFastSearch: true,
				improvedMVPred:       false,
				simpleLoopFilter:     false,
				stepParam:            4,
				furtherSteps:         0,
				speedAdjust:          3,
			},
		},
		{
			// cpu_used=-12 → cpi->Speed=12. Same terminal SF state as
			// Speed=11. cm->filter_type stays NORMAL until Speed >= 14.
			cpuUsed: -12, cpiSpeed: 12,
			want: want{
				optimizeCoefficients: false,
				useFastQuant:         true,
				useFastQuantForPick:  true,
				noSkipBlock4x4:       false,
				firstStep:            1,
				recodeLoop:           0,
				rdModeDecision:       false,
				searchMethodHex:      true,
				fractional:           interAnalysisFractionalSearchHalf,
				autoFilterFastSearch: true,
				improvedMVPred:       false,
				simpleLoopFilter:     false,
				stepParam:            4,
				furtherSteps:         0,
				speedAdjust:          3,
			},
		},
	}

	// Production resolutions: 1280x720 (HD screen capture, primary RT
	// profile) and 854x480 (480p mobile, secondary low-latency profile).
	// Both should produce identical boolean SF state because
	// vp8_set_speed_features is keyed on cpi->Speed only.
	resolutions := []struct {
		name   string
		width  int
		height int
	}{
		{"1280x720", 1280, 720},
		{"854x480", 854, 480},
	}

	for _, tc := range cases {
		for _, res := range resolutions {
			subname := cpuUsedTag(tc.cpuUsed) + "/" + res.name
			t.Run(subname, func(t *testing.T) {
				e := &VP8Encoder{opts: EncoderOptions{
					Deadline: DeadlineRealtime,
					CpuUsed:  tc.cpuUsed,
					Width:    res.width,
					Height:   res.height,
				}}

				if got := e.libvpxCPUUsed(); got != tc.cpiSpeed {
					t.Fatalf("libvpxCPUUsed() = %d, want %d (libvpx encodeframe.c:687: cpi->Speed = -cpu_used)", got, tc.cpiSpeed)
				}

				w := tc.want
				if got := e.libvpxOptimizeCoefficients(); got != w.optimizeCoefficients {
					t.Errorf("optimize_coefficients = %t, want %t (libvpx onyx_if.c:929)", got, w.optimizeCoefficients)
				}
				if got := e.libvpxUseFastQuant(); got != w.useFastQuant {
					t.Errorf("use_fastquant (improved_quant==0) = %t, want %t (libvpx onyx_if.c:936)", got, w.useFastQuant)
				}
				if got := e.libvpxUseFastQuantForPick(); got != w.useFastQuantForPick {
					t.Errorf("use_fastquant_for_pick = %t, want %t (libvpx onyx_if.c:939)", got, w.useFastQuantForPick)
				}
				if got := e.interAnalysisNoSkipBlock4x4Search(); got != w.noSkipBlock4x4 {
					t.Errorf("no_skip_block4x4_search = %t, want %t (libvpx onyx_if.c:940)", got, w.noSkipBlock4x4)
				}
				if got := libvpxInterFrameFirstStepForFeatureSpeed(DeadlineRealtime, tc.cpiSpeed); got != w.firstStep {
					t.Errorf("first_step = %d, want %d (libvpx onyx_if.c:941)", got, w.firstStep)
				}
				if got := libvpxSpeedFeatureRecodeLoop(DeadlineRealtime, tc.cpuUsed); got != w.recodeLoop {
					t.Errorf("recode_loop = %d, want %d (libvpx onyx_if.c:930)", got, w.recodeLoop)
				}
				if got := e.interAnalysisUsesRDModeDecision(); got != w.rdModeDecision {
					t.Errorf("RD = %t, want %t (libvpx onyx_if.c:947)", got, w.rdModeDecision)
				}

				cfg := e.interAnalysisSearchConfig()
				if got := cfg.fullPixelSearch == interAnalysisFullPixelSearchHex; got != w.searchMethodHex {
					t.Errorf("search_method = HEX %t, want %t (libvpx onyx_if.c:953)", got, w.searchMethodHex)
				}
				if got := cfg.fractionalSearch; got != w.fractional {
					t.Errorf("find_fractional_mv_step = %d, want %d (libvpx onyx_if.c:1064-1071 after 954/1012/1023)", got, w.fractional)
				}
				if got := e.loopFilterUsesFastSearch(); got != w.autoFilterFastSearch {
					t.Errorf("loop-filter fast-search (auto_filter==0) = %t, want %t (libvpx onyx_if.c:952)", got, w.autoFilterFastSearch)
				}
				if got := libvpxInterFrameImprovedMVPredictionForFeatureSpeed(DeadlineRealtime, tc.cpiSpeed); got != w.improvedMVPred {
					t.Errorf("improved_mv_pred = %t, want %t (libvpx onyx_if.c:1009)", got, w.improvedMVPred)
				}
				if got := e.encoderUsesSimpleLoopFilter(); got != w.simpleLoopFilter {
					t.Errorf("filter_type = SimpleLoopFilter %t, want %t (libvpx onyx_if.c:1017; Speed >= 14 gate)", got, w.simpleLoopFilter)
				}
				if got := int(cfg.fullPixelSearchParam); got != w.stepParam {
					t.Errorf("step_param = %d, want %d (libvpx pickinter.c:932)", got, w.stepParam)
				}
				if got := int(cfg.fullPixelFurtherSteps); got != w.furtherSteps {
					t.Errorf("further_steps = %d, want %d (libvpx pickinter.c:1005-1008; Speed >= 8 → 0)", got, w.furtherSteps)
				}
				if got := libvpxInterFrameSpeedAdjust(tc.cpiSpeed); got != w.speedAdjust {
					t.Errorf("speed_adjust = %d, want %d (libvpx pickinter.c:929; Speed >= 8 → 3)", got, w.speedAdjust)
				}
			})
		}
	}
}

// TestVP8Task247ExtremeCPUUsedHalfPixelStaysEnabled pins the libvpx
// `Speed >= 15` gate at onyx_if.c:1023 explicitly: in the extreme
// negative cpu_used tier {-9..-12} the cpi->Speed value never reaches 15,
// so sf->half_pixel_search stays 1. The find_fractional_mv_step dispatch
// at onyx_if.c:1064-1071 therefore picks vp8_find_best_half_pixel_step
// (govpx interAnalysisFractionalSearchHalf) for Speed 10..12 and
// vp8_find_best_sub_pixel_step (govpx Step) for Speed 9 — never
// vp8_skip_fractional_mv_step. The libvpx Skip path requires Speed >= 15.
//
// This is the inverse pin of the Speed >= 15 audit: any future patch
// that lowers the half-pixel threshold below 15 for the extreme tier
// would regress visual quality vs libvpx, and this test would catch it.
func TestVP8Task247ExtremeCPUUsedHalfPixelStaysEnabled(t *testing.T) {
	for cpuUsed := -12; cpuUsed <= -9; cpuUsed++ {
		t.Run(cpuUsedTag(cpuUsed), func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{
				Deadline: DeadlineRealtime,
				CpuUsed:  cpuUsed,
				Width:    1280,
				Height:   720,
			}}
			cfg := e.interAnalysisSearchConfig()
			if cfg.fractionalSearch == interAnalysisFractionalSearchSkip {
				t.Fatalf("fractional = Skip at cpu_used=%d; libvpx onyx_if.c:1023 requires Speed >= 15 for half_pixel_search = 0", cpuUsed)
			}
		})
	}
}

// TestVP8Task247ExtremeCPUUsedThreshMapsTerminalEntries pins the
// continuous-Speed (= cpi->Speed + 7) lookup against the libvpx speed_map
// tail entries for the extreme negative cpu_used tier. At cpi->Speed=12
// the continuous Speed is 19, which traverses every speed_map past its
// last finite limit and returns the final value (INT_MAX-bounded entry in
// libvpx, libvpxSpeedMapMax sentinel in govpx).
//
// Specifically (libvpx onyx_if.c:700-735, govpx vp8_encoder_inter_speed.go
// :843-873). All gates expressed in terms of continuous Speed
// (= cpi->Speed + 7 in realtime):
//
//	thresh_mult_map_znn    tail = 2000          (continuous >= RT(2)=9)
//	thresh_mult_map_vhpred tail = INT_MAX/Disab (continuous >= RT(7)=14)
//	thresh_mult_map_bpred  tail = INT_MAX/Disab (continuous >= RT(6)=13)
//	thresh_mult_map_tm     tail = INT_MAX/Disab (continuous >= RT(7)=14)
//	thresh_mult_map_new1   tail = 2000          (continuous past final
//	                                              finite at RT(0)=7)
//	thresh_mult_map_new2   tail = 4000          (continuous >= RT(5)=12)
//	thresh_mult_map_split1 tail = INT_MAX/Disab (continuous >= RT(3)=10)
//	thresh_mult_map_split2 tail = INT_MAX/Disab (continuous >= RT(3)=10)
//
// At cpi->Speed=9 (continuous=16) all of {vhpred, bpred, tm, split1,
// split2} are already in their Disabled tail (the strict gates fired at
// continuous 13/14/10). new1/new2 are in their numeric tail (2000/4000).
// For cpi->Speed=10..12 (continuous 17..19) the table outputs do not
// change — the extreme RT tier hits the terminal entries uniformly.
func TestVP8Task247ExtremeCPUUsedThreshMapsTerminalEntries(t *testing.T) {
	for _, tc := range []struct {
		cpiSpeed       int
		wantZNN        int
		wantVHPred     int
		wantBPred      int
		wantTM         int
		wantNew1       int
		wantNew2       int
		wantSplit1     int
		wantSplit2     int
		wantSplit1Note string
	}{
		{
			// cpi->Speed=9: continuous=16. Past every RT(N) gate up through
			// RT(7)=14 (vhpred), RT(6)=13 (bpred), RT(7)=14 (tm). All three
			// land on their INT_MAX tail (Disabled). new1 tail=2000;
			// new2 tail=4000 (RT(5)=12 gate exhausted). split1/split2
			// disabled at RT(3)=10.
			cpiSpeed: 9, wantZNN: 2000,
			wantVHPred: libvpxInterModeThresholdDisabled,
			wantBPred:  libvpxInterModeThresholdDisabled,
			wantTM:     libvpxInterModeThresholdDisabled,
			wantNew1:   2000, wantNew2: 4000,
			wantSplit1: libvpxInterModeThresholdDisabled, wantSplit2: libvpxInterModeThresholdDisabled,
		},
		{
			// cpi->Speed=10: continuous=17. Same tail values as Speed=9 (no
			// new gates between 16 and 17 in any thresh map). The Speed=10
			// specialty is NEW1 mode-check-freq only.
			cpiSpeed: 10, wantZNN: 2000,
			wantVHPred: libvpxInterModeThresholdDisabled,
			wantBPred:  libvpxInterModeThresholdDisabled,
			wantTM:     libvpxInterModeThresholdDisabled,
			wantNew1:   2000, wantNew2: 4000,
			wantSplit1: libvpxInterModeThresholdDisabled, wantSplit2: libvpxInterModeThresholdDisabled,
		},
		{
			// cpi->Speed=12: continuous=19. Identical terminal entries — the
			// extreme RT tier 9..12 hits the same speed-map tail for every
			// thresh table.
			cpiSpeed: 12, wantZNN: 2000,
			wantVHPred: libvpxInterModeThresholdDisabled,
			wantBPred:  libvpxInterModeThresholdDisabled,
			wantTM:     libvpxInterModeThresholdDisabled,
			wantNew1:   2000, wantNew2: 4000,
			wantSplit1: libvpxInterModeThresholdDisabled, wantSplit2: libvpxInterModeThresholdDisabled,
		},
	} {
		t.Run(cpuUsedTag(-tc.cpiSpeed), func(t *testing.T) {
			mult := libvpxInterModeThresholdMultipliersForCPISpeed(DeadlineRealtime, tc.cpiSpeed, libvpxInterModeThresholdContext{})
			if got, want := mult[libvpxThrZero2], tc.wantZNN; got != want {
				t.Errorf("thresh_mult_map_znn (THR_ZERO2) at cpi->Speed=%d = %d, want %d", tc.cpiSpeed, got, want)
			}
			if got, want := mult[libvpxThrVPred], tc.wantVHPred; got != want {
				t.Errorf("thresh_mult_map_vhpred (THR_V_PRED) at cpi->Speed=%d = %d, want %d (libvpx onyx_if.c:705)", tc.cpiSpeed, got, want)
			}
			if got, want := mult[libvpxThrBPred], tc.wantBPred; got != want {
				t.Errorf("thresh_mult_map_bpred (THR_B_PRED) at cpi->Speed=%d = %d, want %d (libvpx onyx_if.c:709)", tc.cpiSpeed, got, want)
			}
			if got, want := mult[libvpxThrTMPred], tc.wantTM; got != want {
				t.Errorf("thresh_mult_map_tm (THR_TM) at cpi->Speed=%d = %d, want %d (libvpx onyx_if.c:714)", tc.cpiSpeed, got, want)
			}
			if got, want := mult[libvpxThrNew1], tc.wantNew1; got != want {
				t.Errorf("thresh_mult_map_new1 (THR_NEW1) at cpi->Speed=%d = %d, want %d (libvpx onyx_if.c:719)", tc.cpiSpeed, got, want)
			}
			if got, want := mult[libvpxThrNew2], tc.wantNew2; got != want {
				t.Errorf("thresh_mult_map_new2 (THR_NEW2) at cpi->Speed=%d = %d, want %d (libvpx onyx_if.c:722)", tc.cpiSpeed, got, want)
			}
			if got, want := mult[libvpxThrSplit1], tc.wantSplit1; got != want {
				t.Errorf("thresh_mult_map_split1 (THR_SPLIT1) at cpi->Speed=%d = %d, want %d (libvpx onyx_if.c:727)", tc.cpiSpeed, got, want)
			}
			if got, want := mult[libvpxThrSplit2], tc.wantSplit2; got != want {
				t.Errorf("thresh_mult_map_split2 (THR_SPLIT2) at cpi->Speed=%d = %d, want %d (libvpx onyx_if.c:732)", tc.cpiSpeed, got, want)
			}
		})
	}
}

// TestVP8Task247ExtremeCPUUsedSimpleLoopFilterStaysOff pins the libvpx
// `Speed >= 14` gate at onyx_if.c:1017 explicitly: for the extreme
// negative cpu_used tier {-9..-12} the cpi->Speed value stays below 14,
// so cm->filter_type stays NORMAL_LOOPFILTER and govpx's
// encoderUsesSimpleLoopFilter must return false. The gate is strict
// `>=`, so even cpi->Speed=13 would keep NORMAL_LOOPFILTER; -14 → 14
// would be the first to trigger SIMPLE_LOOPFILTER. The test exercises
// cpu_used=-13 (just below the gate; still NORMAL) and -14 (gate
// triggers; SimpleLoopFilter) as sentinel anchors next to the extreme
// tier to lock the threshold.
func TestVP8Task247ExtremeCPUUsedSimpleLoopFilterStaysOff(t *testing.T) {
	for _, tc := range []struct {
		cpuUsed          int
		wantSimpleFilter bool
	}{
		{cpuUsed: -9, wantSimpleFilter: false},
		{cpuUsed: -10, wantSimpleFilter: false},
		{cpuUsed: -11, wantSimpleFilter: false},
		{cpuUsed: -12, wantSimpleFilter: false},
		{cpuUsed: -13, wantSimpleFilter: false}, // sentinel: just below the gate
		{cpuUsed: -14, wantSimpleFilter: true},  // sentinel: gate triggers
	} {
		t.Run(cpuUsedTag(tc.cpuUsed), func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{
				Deadline: DeadlineRealtime,
				CpuUsed:  tc.cpuUsed,
				Width:    1280,
				Height:   720,
			}}
			if got := e.encoderUsesSimpleLoopFilter(); got != tc.wantSimpleFilter {
				t.Fatalf("encoderUsesSimpleLoopFilter() at cpu_used=%d = %t, want %t (libvpx onyx_if.c:1017; Speed >= 14)", tc.cpuUsed, got, tc.wantSimpleFilter)
			}
		})
	}
}

// TestVP8Task247ExtremeCPUUsedEncodesAcrossResolutions exercises the
// EncodeInto pipeline end-to-end at the extreme realtime cpu_used tier
// across the two production resolutions (1280x720 and 854x480), with two
// frames each (one keyframe + one inter). The test is a smoke gate: the
// speed-feature state computed by the other Task #247 tests must produce
// a valid bitstream without runtime errors at every cpu_used in the
// extreme tier. Any panic or error in the encode loop would surface a
// missing-port or a mis-wired Speed-cascade branch.
//
// Frames are colour-filled to keep the test fast (no per-MB synthesis)
// while still driving every macroblock through the inter-pick and
// loop-filter selection at HD scale. The encoder uses CBR + RT to keep
// the rate-controller in its non-recode path (sf->recode_loop = 0,
// libvpx onyx_if.c:930), matching the libvpx screen-capture profile.
func TestVP8Task247ExtremeCPUUsedEncodesAcrossResolutions(t *testing.T) {
	resolutions := []struct {
		name   string
		width  int
		height int
	}{
		{"1280x720", 1280, 720},
		{"854x480", 854, 480},
	}
	for _, cpuUsed := range []int{-9, -10, -11, -12} {
		for _, res := range resolutions {
			subname := cpuUsedTag(cpuUsed) + "/" + res.name
			t.Run(subname, func(t *testing.T) {
				e, err := NewVP8Encoder(EncoderOptions{
					Width:               res.width,
					Height:              res.height,
					FPS:                 30,
					RateControlMode:     RateControlCBR,
					TargetBitrateKbps:   1200,
					MinQuantizer:        4,
					MaxQuantizer:        56,
					DropFrameAllowed:    true,
					DropFrameWaterMark:  defaultDropFramesWaterMark,
					Deadline:            DeadlineRealtime,
					CpuUsed:             cpuUsed,
					KeyFrameInterval:    120,
					BufferSizeMs:        600,
					BufferInitialSizeMs: 400,
					BufferOptimalSizeMs: 500,
				})
				if err != nil {
					t.Fatalf("NewVP8Encoder(cpu_used=%d, %dx%d) = %v", cpuUsed, res.width, res.height, err)
				}

				src := testImage(res.width, res.height)
				fillImage(src, 180, 90, 170)
				// Estimate dst capacity: 1 byte/pixel + 1KB header slack is
				// enough for the colour-filled frames at the realtime
				// quantizer floor.
				dst := make([]byte, res.width*res.height+1024)

				for frame := range 2 {
					if frame == 1 {
						// Perturb a few pixels so frame 1 is a true inter
						// frame with some residual rather than an exact
						// repeat that the picker can collapse to a near-
						// zero token frame.
						for row := 0; row < res.height; row += 32 {
							for col := 0; col < res.width; col += 32 {
								src.Y[row*src.YStride+col] ^= 0x40
							}
						}
					}
					result, err := e.EncodeInto(dst, src, uint64(frame), 1, 0)
					if err != nil {
						t.Fatalf("EncodeInto frame=%d cpu_used=%d %dx%d returned error: %v", frame, cpuUsed, res.width, res.height, err)
					}
					if result.Dropped {
						// Drop is legal here (CBR can drop the first inter
						// frame on a tight buffer), but the keyframe must
						// be emitted.
						if frame == 0 {
							t.Fatalf("EncodeInto frame=0 cpu_used=%d %dx%d: keyframe was dropped", cpuUsed, res.width, res.height)
						}
						continue
					}
					if frame == 0 && !result.KeyFrame {
						t.Fatalf("EncodeInto frame=0 cpu_used=%d %dx%d: KeyFrame=false; want first frame as keyframe", cpuUsed, res.width, res.height)
					}
					if len(result.Data) == 0 {
						t.Fatalf("EncodeInto frame=%d cpu_used=%d %dx%d: empty payload", frame, cpuUsed, res.width, res.height)
					}
				}
			})
		}
	}
}
