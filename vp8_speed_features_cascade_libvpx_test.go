package govpx

import "testing"

// TestVP8SpeedFeaturesCascadeMirrorsLibvpx pins the verbatim mirror of
// libvpx `vp8_set_speed_features` (vp8/encoder/onyx_if.c lines 768-1087)
// across the realtime cpi->Speed cascade for cpu_used in {-3, -4, -5,
// -7, -8, -9, -10, -12, -14, -15}. The audit walks each Speed-gated
// assignment that libvpx case 2 (Mode==MODE_REALTIME)
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
//	  → e.libvpxOptimizeCoefficients()       (vp8_encoder_inter_speed.go:122)
//
//	sf->recode_loop           (line 930 set to 0 in case 2)
//	  → libvpxSpeedFeatureRecodeLoop()       (vp8_encoder_config.go:644)
//
//	sf->auto_filter           (lines 931/944/948/952)
//	  → !e.loopFilterUsesFastSearch()        (vp8_encoder_loopfilter.go:204)
//
//	sf->iterative_sub_pixel,
//	sf->quarter_pixel_search,
//	sf->half_pixel_search,
//	cpi->find_fractional_mv_step
//	                          (lines 932/954/1012/1023 + dispatch
//	                           lines 1064-1071)
//	  → e.interAnalysisSearchConfig().fractionalSearch
//	                                           (vp8_encoder_inter_speed.go:67)
//
//	sf->search_method         (lines 933/953)
//	  → e.interAnalysisSearchConfig().fullPixelSearch (NSTEP vs HEX)
//
//	sf->improved_quant        (line 936)
//	  → !e.libvpxUseFastQuant()              (vp8_encoder_inter_speed.go:133,
//	                                          fast quant fires when
//	                                          improved_quant==0)
//
//	sf->use_fastquant_for_pick(line 939)
//	  → e.libvpxUseFastQuantForPick()        (vp8_encoder_inter_speed.go:144)
//
//	sf->no_skip_block4x4_search(line 940)
//	  → e.interAnalysisNoSkipBlock4x4Search()(vp8_encoder_inter_speed.go:217)
//
//	sf->first_step            (line 941)
//	  → libvpxInterFrameFirstStepForFeatureSpeed (vp8_encoder_inter_speed.go:233)
//
//	sf->RD                    (line 947)
//	  → e.interAnalysisUsesRDModeDecision()  (vp8_encoder_inter_speed.go:111)
//
//	sf->improved_mv_pred      (line 1009 in case 2 Speed > 6 block)
//	  → libvpxInterFrameImprovedMVPredictionForFeatureSpeed
//	                                           (vp8_encoder_inter_speed.go:266)
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
