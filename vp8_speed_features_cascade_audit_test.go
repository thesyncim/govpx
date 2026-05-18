package govpx

import "testing"

// TestVP8SpeedFeaturesCascadeMirrorsLibvpx pins the verbatim mirror of
// libvpx `vp8_set_speed_features` (vp8/encoder/onyx_if.c lines 768-1087)
// across the realtime cpi->Speed cascade for cpu_used in {-3, -4, -5,
// -7, -8, -9, -10, -12, -14, -15}. Task #223 closure: the audit walks
// each Speed-gated assignment that libvpx case 2 (Mode==MODE_REALTIME)
// performs and asserts the govpx mirror returns the identical SF state
// for every Speed value in 3..15.
//
// libvpx encodeframe.c lines 685-691 flips negative cpu_used to its
// absolute value before vp8_set_speed_features runs, so cpu_used=-3
// produces cpi->Speed=3, cpu_used=-8 produces cpi->Speed=8, and so on.
// The govpx mirror funnel `e.libvpxCPUUsed()` mirrors that abs flip
// exactly for the negative-explicit path (positive cpu_used uses the
// autoSpeed path, exercised separately by
// TestLibvpxSpeedFeatureCPUUsedMirrorsRealtimeAutoSelect).
//
// libvpx field → govpx mirror (libvpx v1.16.0, onyx_if.c line):
//
//	sf->optimize_coefficients (line 929 set to 0 in case 2)
//	  → e.libvpxOptimizeCoefficients()       (encoder_inter_speed.go:122)
//
//	sf->recode_loop           (line 930 set to 0 in case 2)
//	  → libvpxSpeedFeatureRecodeLoop()       (encoder_config.go:644)
//
//	sf->auto_filter           (lines 931/944/948/952)
//	  → !e.loopFilterUsesFastSearch()        (encoder_loopfilter.go:204)
//
//	sf->iterative_sub_pixel,
//	sf->quarter_pixel_search,
//	sf->half_pixel_search,
//	cpi->find_fractional_mv_step
//	                          (lines 932/954/1012/1023 + dispatch
//	                           lines 1064-1071)
//	  → e.interAnalysisSearchConfig().fractionalSearch
//	                                           (encoder_inter_speed.go:67)
//
//	sf->search_method         (lines 933/953)
//	  → e.interAnalysisSearchConfig().fullPixelSearch (NSTEP vs HEX)
//
//	sf->improved_quant        (line 936)
//	  → !e.libvpxUseFastQuant()              (encoder_inter_speed.go:133,
//	                                          fast quant fires when
//	                                          improved_quant==0)
//
//	sf->use_fastquant_for_pick(line 939)
//	  → e.libvpxUseFastQuantForPick()        (encoder_inter_speed.go:144)
//
//	sf->no_skip_block4x4_search(line 940)
//	  → e.interAnalysisNoSkipBlock4x4Search()(encoder_inter_speed.go:217)
//
//	sf->first_step            (line 941)
//	  → libvpxInterFrameFirstStepForFeatureSpeed (encoder_inter_speed.go:233)
//
//	sf->RD                    (line 947)
//	  → e.interAnalysisUsesRDModeDecision()  (encoder_inter_speed.go:111)
//
//	sf->improved_mv_pred      (line 1009 in case 2 Speed > 6 block)
//	  → libvpxInterFrameImprovedMVPredictionForFeatureSpeed
//	                                           (encoder_inter_speed.go:266)
//
// The test is exhaustive across cpi->Speed in {3, 4, 5, 6, 7, 8, 9, 10,
// 11, 12, 14, 15} and asserts every govpx mirror returns the
// libvpx-expected value. The expected SF state per Speed is derived by
// applying the libvpx case 2 cascade in order:
//
//	initial: optimize_coefficients=0, recode_loop=0, auto_filter=1,
//	         iterative_sub_pixel=1, search_method=NSTEP,
//	         improved_quant=1, improved_dct=1, use_fastquant_for_pick=0,
//	         no_skip_block4x4_search=1, first_step=0, RD=1,
//	         improved_mv_pred=1, quarter_pixel_search=1,
//	         half_pixel_search=1
//
//	if Speed > 0:   improved_quant=0, improved_dct=0,
//	                use_fastquant_for_pick=1, no_skip_block4x4_search=0,
//	                first_step=1
//	if Speed > 2:   auto_filter=0
//	if Speed > 3:   RD=0, auto_filter=1
//	if Speed > 4:   auto_filter=0, search_method=HEX,
//	                iterative_sub_pixel=0
//	if Speed > 6:   improved_mv_pred=0   (plus adaptive RD thresh,
//	                                       exercised separately)
//	if Speed > 8:   quarter_pixel_search=0
//	if Speed >= 15: half_pixel_search=0
//
// find_fractional_mv_step dispatch (lines 1064-1071):
//
//	iterative_sub_pixel==1: Iterative
//	else if quarter_pixel_search: Step
//	else if half_pixel_search:    Half
//	else:                         Skip
func TestVP8SpeedFeaturesCascadeMirrorsLibvpx(t *testing.T) {
	type want struct {
		optimizeCoefficients bool
		recodeLoop           int
		autoFilter           bool
		searchMethodHex      bool
		fractional           interAnalysisFractionalSearchMethod
		improvedQuant        bool
		useFastQuantForPick  bool
		noSkipBlock4x4       bool
		firstStep            int
		rdModeDecision       bool
		improvedMVPred       bool
	}
	// applyLibvpxCascade computes the post-set_speed_features SF state for
	// case 2 (Mode==MODE_REALTIME) at the given cpi->Speed, applying every
	// gated branch in libvpx order.
	applyLibvpxCascade := func(cpiSpeed int) want {
		w := want{
			optimizeCoefficients: false, // case 2 unconditional: line 929
			recodeLoop:           0,     // case 2 unconditional: line 930
			autoFilter:           true,  // case 2 unconditional: line 931
			searchMethodHex:      false, // case 2 unconditional: line 933 (NSTEP)
			improvedQuant:        true,  // default
			useFastQuantForPick:  false, // default
			noSkipBlock4x4:       true,  // default
			firstStep:            0,     // default
			rdModeDecision:       true,  // default
			improvedMVPred:       true,  // default
		}
		// iterative=1, quarter=1, half=1 in defaults; tracked separately
		// for the find_fractional_mv_step dispatch below.
		iterative := true
		quarter := true
		half := true

		if cpiSpeed > 0 {
			w.improvedQuant = false
			w.useFastQuantForPick = true
			w.noSkipBlock4x4 = false
			w.firstStep = 1
		}
		if cpiSpeed > 2 {
			w.autoFilter = false
		}
		if cpiSpeed > 3 {
			w.rdModeDecision = false
			w.autoFilter = true
		}
		if cpiSpeed > 4 {
			w.autoFilter = false
			w.searchMethodHex = true
			iterative = false
		}
		if cpiSpeed > 6 {
			w.improvedMVPred = false
		}
		if cpiSpeed > 8 {
			quarter = false
		}
		if cpiSpeed >= 15 {
			half = false
		}

		// find_fractional_mv_step dispatch (libvpx onyx_if.c:1064-1071):
		switch {
		case iterative:
			w.fractional = interAnalysisFractionalSearchIterative
		case quarter:
			w.fractional = interAnalysisFractionalSearchStep
		case half:
			w.fractional = interAnalysisFractionalSearchHalf
		default:
			w.fractional = interAnalysisFractionalSearchSkip
		}
		return w
	}

	cpiSpeeds := []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 15}
	for _, cpiSpeed := range cpiSpeeds {
		// In libvpx encodeframe.c:687, cpu_used < 0 produces
		// cpi->Speed = -cpu_used, so cpu_used = -cpiSpeed yields the
		// matching cpi->Speed.
		cpuUsed := -cpiSpeed
		w := applyLibvpxCascade(cpiSpeed)

		t.Run(cpuUsedTag(cpuUsed), func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: cpuUsed}}

			if got := e.libvpxOptimizeCoefficients(); got != w.optimizeCoefficients {
				t.Errorf("optimize_coefficients = %t, want %t (libvpx onyx_if.c:929)", got, w.optimizeCoefficients)
			}
			if got := libvpxSpeedFeatureRecodeLoop(DeadlineRealtime, cpuUsed); got != w.recodeLoop {
				t.Errorf("recode_loop = %d, want %d (libvpx onyx_if.c:930)", got, w.recodeLoop)
			}
			if got := !e.loopFilterUsesFastSearch(); got != w.autoFilter {
				t.Errorf("auto_filter = %t, want %t (libvpx onyx_if.c:931/944/948/952)", got, w.autoFilter)
			}
			cfg := e.interAnalysisSearchConfig()
			if got := cfg.fullPixelSearch == interAnalysisFullPixelSearchHex; got != w.searchMethodHex {
				t.Errorf("search_method=HEX = %t, want %t (libvpx onyx_if.c:933/953)", got, w.searchMethodHex)
			}
			if got := cfg.fractionalSearch; got != w.fractional {
				t.Errorf("find_fractional_mv_step dispatch = %d, want %d (libvpx onyx_if.c:1064-1071 after lines 954/1012/1023)", got, w.fractional)
			}
			if got := !e.libvpxUseFastQuant(); got != w.improvedQuant {
				t.Errorf("improved_quant = %t, want %t (libvpx onyx_if.c:936; quantize_b=fast iff improved_quant==0)", got, w.improvedQuant)
			}
			if got := e.libvpxUseFastQuantForPick(); got != w.useFastQuantForPick {
				t.Errorf("use_fastquant_for_pick = %t, want %t (libvpx onyx_if.c:939)", got, w.useFastQuantForPick)
			}
			if got := e.interAnalysisNoSkipBlock4x4Search(); got != w.noSkipBlock4x4 {
				t.Errorf("no_skip_block4x4_search = %t, want %t (libvpx onyx_if.c:798/940)", got, w.noSkipBlock4x4)
			}
			if got := libvpxInterFrameFirstStepForFeatureSpeed(DeadlineRealtime, cpiSpeed); got != w.firstStep {
				t.Errorf("first_step = %d, want %d (libvpx onyx_if.c:941)", got, w.firstStep)
			}
			if got := e.interAnalysisUsesRDModeDecision(); got != w.rdModeDecision {
				t.Errorf("RD = %t, want %t (libvpx onyx_if.c:947)", got, w.rdModeDecision)
			}
			if got := libvpxInterFrameImprovedMVPredictionForFeatureSpeed(DeadlineRealtime, cpiSpeed); got != w.improvedMVPred {
				t.Errorf("improved_mv_pred = %t, want %t (libvpx onyx_if.c:1009)", got, w.improvedMVPred)
			}
		})
	}
}

// TestVP8SpeedFeaturesPickInterStepParamMirrorsLibvpx pins the verbatim
// mirror of the libvpx pickinter.c full-pel motion-search step_param and
// further_steps cascade (lines 929/1005-1008) for the fast-RT path
// (Speed >= 4, sf->RD == 0). govpx threads these via interAnalysisSearchConfig
// fullPixelSearchParam / fullPixelFurtherSteps.
//
// libvpx pickinter.c:929: speed_adjust = (cpi->Speed > 5)
//
//	? ((cpi->Speed >= 8) ? 3 : 2)
//	: 1
//
// pickinter.c:932: step_param = sf->first_step + speed_adjust
// pickinter.c:1005-1008: further_steps = (cpi->Speed >= 8) ? 0
//
//	: (max_step - 1 - step_param)
//
// At Speed >= 4 sf->RD = 0 → govpx interAnalysisUsesRDModeDecision()
// returns false and the search-config builder uses
// libvpxInterFrameSearchParamForFeatureSpeed (= first_step + speed_adjust)
// + libvpxInterFrameFurtherSteps (= Speed>=8 ? 0 : max-1-step) directly,
// matching libvpx's pickinter path verbatim.
func TestVP8SpeedFeaturesPickInterStepParamMirrorsLibvpx(t *testing.T) {
	cases := []struct {
		cpiSpeed     int
		wantStep     int
		wantFurther  int
		wantAdjust   int
		wantFirstStp int
	}{
		// Speed 4 / 5: fast-RT (RD off at Speed > 3), first_step=1,
		// speed_adjust=1, step_param=2, further=8-1-2=5.
		{cpiSpeed: 4, wantStep: 2, wantFurther: 5, wantAdjust: 1, wantFirstStp: 1},
		{cpiSpeed: 5, wantStep: 2, wantFurther: 5, wantAdjust: 1, wantFirstStp: 1},
		// Speed 6 / 7: speed_adjust=2 (Speed > 5), step_param=3,
		// further=8-1-3=4.
		{cpiSpeed: 6, wantStep: 3, wantFurther: 4, wantAdjust: 2, wantFirstStp: 1},
		{cpiSpeed: 7, wantStep: 3, wantFurther: 4, wantAdjust: 2, wantFirstStp: 1},
		// Speed 8+: speed_adjust=3 (Speed >= 8), step_param=4,
		// further=0 (Speed >= 8 short-circuits).
		{cpiSpeed: 8, wantStep: 4, wantFurther: 0, wantAdjust: 3, wantFirstStp: 1},
		{cpiSpeed: 9, wantStep: 4, wantFurther: 0, wantAdjust: 3, wantFirstStp: 1},
		{cpiSpeed: 10, wantStep: 4, wantFurther: 0, wantAdjust: 3, wantFirstStp: 1},
		{cpiSpeed: 12, wantStep: 4, wantFurther: 0, wantAdjust: 3, wantFirstStp: 1},
		{cpiSpeed: 15, wantStep: 4, wantFurther: 0, wantAdjust: 3, wantFirstStp: 1},
	}
	for _, tc := range cases {
		cpuUsed := -tc.cpiSpeed
		t.Run(cpuUsedTag(cpuUsed), func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: cpuUsed}}
			cfg := e.interAnalysisSearchConfig()
			if got := int(cfg.fullPixelSearchParam); got != tc.wantStep {
				t.Errorf("step_param = %d, want %d (libvpx pickinter.c:932 = first_step + speed_adjust)", got, tc.wantStep)
			}
			if got := int(cfg.fullPixelFurtherSteps); got != tc.wantFurther {
				t.Errorf("further_steps = %d, want %d (libvpx pickinter.c:1005-1008)", got, tc.wantFurther)
			}
			if got := libvpxInterFrameSpeedAdjust(tc.cpiSpeed); got != tc.wantAdjust {
				t.Errorf("speed_adjust = %d, want %d (libvpx pickinter.c:929)", got, tc.wantAdjust)
			}
			if got := libvpxInterFrameFirstStepForFeatureSpeed(DeadlineRealtime, tc.cpiSpeed); got != tc.wantFirstStp {
				t.Errorf("sf->first_step = %d, want %d (libvpx onyx_if.c:941; Speed > 0 path)", got, tc.wantFirstStp)
			}
		})
	}
}

// TestVP8SpeedFeaturesRDPathStepParamMirrorsLibvpx pins the verbatim
// mirror of the libvpx rdopt.c NEWMV step_param cascade (lines
// 2034/2086) for the RD path (Speed <= 3 in case 2). govpx mirrors via
// interAnalysisSearchConfig with fullPixelFinalRefine=true (= RD on),
// fullPixelSpeedAdjust=0 (no pickinter adjust in RD), and step_param=
// libvpxInterFrameFirstStepForFeatureSpeed (= sf->first_step).
//
// libvpx rdopt.c:2034: step_param = cpi->sf.first_step
// libvpx rdopt.c:2086: further_steps = (sf->max_step_search_steps - 1)
//
//   - step_param
//
// At Speed <= 3 sf->RD = 1 → govpx interAnalysisUsesRDModeDecision()
// returns true and the search-config builder uses first_step alone
// (no speed_adjust), matching libvpx's rdopt path verbatim.
func TestVP8SpeedFeaturesRDPathStepParamMirrorsLibvpx(t *testing.T) {
	cases := []struct {
		cpiSpeed     int
		wantStep     int
		wantFurther  int
		wantFirstStp int
	}{
		// Speed 1-3 RT: first_step=1 (sf->first_step set inside
		// Speed > 0), so step_param=1, further=8-1-1=6. RD path,
		// no speed_adjust.
		{cpiSpeed: 1, wantStep: 1, wantFurther: 6, wantFirstStp: 1},
		{cpiSpeed: 2, wantStep: 1, wantFurther: 6, wantFirstStp: 1},
		{cpiSpeed: 3, wantStep: 1, wantFurther: 6, wantFirstStp: 1},
	}
	for _, tc := range cases {
		cpuUsed := -tc.cpiSpeed
		t.Run(cpuUsedTag(cpuUsed), func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: cpuUsed}}
			cfg := e.interAnalysisSearchConfig()
			if !e.interAnalysisUsesRDModeDecision() {
				t.Fatalf("Speed=%d expected RD on but interAnalysisUsesRDModeDecision returned false", tc.cpiSpeed)
			}
			if got := int(cfg.fullPixelSearchParam); got != tc.wantStep {
				t.Errorf("step_param = %d, want %d (libvpx rdopt.c:2034 = sf->first_step alone)", got, tc.wantStep)
			}
			if got := int(cfg.fullPixelFurtherSteps); got != tc.wantFurther {
				t.Errorf("further_steps = %d, want %d (libvpx rdopt.c:2086 = max-1-step)", got, tc.wantFurther)
			}
			if got := libvpxInterFrameFirstStepForFeatureSpeed(DeadlineRealtime, tc.cpiSpeed); got != tc.wantFirstStp {
				t.Errorf("sf->first_step = %d, want %d (libvpx onyx_if.c:941; Speed > 0 path)", got, tc.wantFirstStp)
			}
			if got := int(cfg.fullPixelSpeedAdjust); got != 0 {
				t.Errorf("speed_adjust = %d under RD, want 0 (libvpx rdopt.c does not apply pickinter speed_adjust)", got)
			}
		})
	}
}

// TestVP8SpeedFeaturesNew1ModeCheckFreqMirrorsLibvpxSpeed10 pins the
// libvpx onyx_if.c:877-879 special-case: at cpi->Speed == 10 (cpu_used
// = -10) and Mode == 2 (realtime), the mode_check_freq[THR_NEW1]
// speed_map lookup uses Speed2 = RT(9) = 16 instead of the natural
// continuous-Speed lookup (which would be RT(10) = 17). This caps the
// NEW1 throttle one step shy of the Speed=10 rate so libvpx keeps
// testing NEW1 even after raising other thresholds.
//
// govpx mirror: libvpxInterModeCheckFrequenciesForCPISpeed
// (encoder_inter_speed.go:780) substitutes new1Speed=16 when
// deadline==DeadlineRealtime && speed==10.
func TestVP8SpeedFeaturesNew1ModeCheckFreqMirrorsLibvpxSpeed10(t *testing.T) {
	// At Speed=10 (RT cpu_used=-10), new1Speed lookup uses 16 (= RT(9)).
	// libvpxModeCheckFreqMapNew1 = {0, 17, 2, 18, 4, 19, 8, SpeedMapMax}.
	// At speed=16: 16<17 → return 0. (Without the override speed=17
	// would yield 2.)
	freq := libvpxInterModeCheckFrequenciesForCPISpeed(DeadlineRealtime, 10)
	if got, want := freq[libvpxThrNew1], 0; got != want {
		t.Fatalf("mode_check_freq[THR_NEW1] at Speed=10 = %d, want %d (libvpx onyx_if.c:877-879; speed2=RT(9)=16 → map yields 0)", got, want)
	}

	// At Speed=11 (no override): continuous=18 → map walk yields 4
	// ({0, RT(10)=17, 2, RT(11)=18, 4, RT(12)=19, ...}; at speed=18
	// the do-while continues past 17 and 18 and stops on 19 with
	// res=4).
	freq11 := libvpxInterModeCheckFrequenciesForCPISpeed(DeadlineRealtime, 11)
	if got, want := freq11[libvpxThrNew1], 4; got != want {
		t.Fatalf("mode_check_freq[THR_NEW1] at Speed=11 = %d, want %d (libvpx onyx_if.c:879; no override, continuous=18 → map yields 4)", got, want)
	}

	// At Speed=9 (no override): continuous=16 → map yields 0.
	freq9 := libvpxInterModeCheckFrequenciesForCPISpeed(DeadlineRealtime, 9)
	if got, want := freq9[libvpxThrNew1], 0; got != want {
		t.Fatalf("mode_check_freq[THR_NEW1] at Speed=9 = %d, want %d (no override, continuous=16 → map yields 0)", got, want)
	}
}

// cpuUsedTag formats a subtest name from a signed cpu_used. Negative
// values produce "cpu-neg-N" so the test output is greppable.
func cpuUsedTag(cpuUsed int) string {
	if cpuUsed < 0 {
		return "cpu-neg-" + itoaPositive(-cpuUsed)
	}
	return "cpu-" + itoaPositive(cpuUsed)
}

func itoaPositive(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
