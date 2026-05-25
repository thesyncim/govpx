package govpx

import "testing"

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

// TestVP8SpeedFeaturesRDPathStepParamMirrorsLibvpxAllDeadlines pins the
// libvpx rdopt.c NEWMV step_param/further_steps cascade
// (vp8/encoder/rdopt.c:2034/2086) across every RD-on deadline+cpu_used
// combination. The picker-vs-RD split must hold not just for Realtime
// cpu_used<=3 (covered by
// TestVP8SpeedFeaturesRDPathStepParamMirrorsLibvpx above) but for the full
// RD-on surface:
//
//   - BestQuality + any cpu_used (Speed = cpu_used pass-through;
//     libvpx onyx_if.c:1481-1484 sets compressor_speed=0 with no
//     cpu_used clamp, and case 0 leaves first_step=0,
//     max_step_search_steps=MAX_MVSEARCH_STEPS).
//   - GoodQuality + cpu_used<=3 (sf->first_step=1 only when Speed>0;
//     RD on per onyx_if.c:916 Speed>3 gate).
//   - Realtime + cpu_used<=3 (already covered above, included here
//     for the cross-deadline invariant check).
//
// The BestQuality+cpu_used>=8 cohort was the regression site. Speed stayed
// at cpu_used (e.g. 8) but sf.RD=1, so the RD path was selected with
// step_param=sf.first_step=0 and further_steps=max-1-step. The earlier
// adjustedForImprovedMVStart path routed cpi->Speed through
// libvpxInterFrameFurtherSteps (which applies pickinter.c:1005-1008's
// Speed>=8 short-circuit), silently capping further_steps to 0 on every MB
// with sr>0 from improved_mv_pred. This test pins the verbatim rdopt.c:2086
// formula for BestQuality+cpu>=8 plus every other RD-on cohort.
//
// libvpx rdopt.c:2034: step_param = cpi->sf.first_step  (no speed_adjust)
// libvpx rdopt.c:2086: further_steps = (sf->max_step_search_steps - 1) - step_param
//
// vs libvpx pickinter.c:932: step_param = sf->first_step + speed_adjust
// vs libvpx pickinter.c:1005-1008: further_steps = Speed>=8 ? 0 : (max-1-step)
//
// govpx's interAnalysisSearchConfig() collapses both forms via
// fullPixelFinalRefine (= e.interAnalysisUsesRDModeDecision()):
//   - On RD: fullPixelSearchParam = first_step alone,
//     fullPixelSpeedAdjust = 0,
//     fullPixelFurtherSteps = max-1-first_step (no Speed cap;
//     BestQuality forces furtherStepsSpeed=0 so even cpu>=8
//     skips the Speed>=8 short-circuit; Good/Realtime cpu<=3
//     naturally falls below the cap because speed<8).
//   - On non-RD picker: fullPixelSearchParam = first_step + speed_adjust,
//     fullPixelSpeedAdjust = speed_adjust,
//     fullPixelFurtherSteps = (Speed>=8 ? 0 : max-1-step).
func TestVP8SpeedFeaturesRDPathStepParamMirrorsLibvpxAllDeadlines(t *testing.T) {
	type rdCase struct {
		name        string
		deadline    Deadline
		cpuUsed     int
		wantStep    int // libvpx rdopt.c:2034 = sf->first_step
		wantFurther int // libvpx rdopt.c:2086 = max-1-step_param
	}
	// libvpx onyx_if.c case 0 (BestQuality, lines 891-894): first_step=0,
	// max_step_search_steps=MAX_MVSEARCH_STEPS (=8). No cpu_used clamp
	// (onyx_if.c:1481-1484), so Speed = cpu_used directly.
	//
	// libvpx onyx_if.c case 1/3 (GoodQuality, lines 895-..., not 916+
	// where RD is turned off): first_step=1 iff Speed>0 (line 903).
	// GoodQuality clamps cpu_used to [-5, 5] (libvpxEffectiveCPUUsed)
	// before vp8_set_speed_features.
	//
	// libvpx onyx_if.c case 2 (Realtime): first_step=1 iff Speed>0
	// (line 941). At Speed<=3, RD is still on (line 947 turns RD off at
	// Speed>3).
	cases := []rdCase{
		// BestQuality: first_step=0 for all cpu_used. further=8-1-0=7.
		// The cpu_used>=8 cohort is the regression site.
		{name: "best-cpu-0", deadline: DeadlineBestQuality, cpuUsed: 0, wantStep: 0, wantFurther: 7},
		{name: "best-cpu-4", deadline: DeadlineBestQuality, cpuUsed: 4, wantStep: 0, wantFurther: 7},
		{name: "best-cpu-8", deadline: DeadlineBestQuality, cpuUsed: 8, wantStep: 0, wantFurther: 7},
		{name: "best-cpu-12", deadline: DeadlineBestQuality, cpuUsed: 12, wantStep: 0, wantFurther: 7},
		{name: "best-cpu-16", deadline: DeadlineBestQuality, cpuUsed: 16, wantStep: 0, wantFurther: 7},
		// GoodQuality + cpu_used<=3 (RD on; libvpx clamps cpu_used to
		// [-5, 5] before Speed translation, so cpu_used=0 → Speed=0,
		// cpu_used=3 → Speed=3, cpu_used=-3 → Speed=-3<0 still uses
		// case 1/3 cascade with Speed=cpu_used [-3], for which Speed>0
		// is false → first_step=0).
		{name: "good-cpu-0", deadline: DeadlineGoodQuality, cpuUsed: 0, wantStep: 0, wantFurther: 7},
		{name: "good-cpu-1", deadline: DeadlineGoodQuality, cpuUsed: 1, wantStep: 1, wantFurther: 6},
		{name: "good-cpu-2", deadline: DeadlineGoodQuality, cpuUsed: 2, wantStep: 1, wantFurther: 6},
		{name: "good-cpu-3", deadline: DeadlineGoodQuality, cpuUsed: 3, wantStep: 1, wantFurther: 6},
		{name: "good-cpu-neg3", deadline: DeadlineGoodQuality, cpuUsed: -3, wantStep: 0, wantFurther: 7},
		// Realtime + cpu_used<=3 (already covered above; replicate the
		// negative-cpu_used cases here for the cross-deadline check).
		// At cpu_used=0, libvpxCPUUsed returns the cold-start sentinel
		// 4 on a fresh encoder (e.frameCount==0), which is > 3 → RD OFF.
		// Skip cpu_used=0 here and exercise the explicit-negative cohort
		// where libvpxCPUUsed returns -cpu_used directly.
		{name: "rt-cpu-neg1", deadline: DeadlineRealtime, cpuUsed: -1, wantStep: 1, wantFurther: 6},
		{name: "rt-cpu-neg2", deadline: DeadlineRealtime, cpuUsed: -2, wantStep: 1, wantFurther: 6},
		{name: "rt-cpu-neg3", deadline: DeadlineRealtime, cpuUsed: -3, wantStep: 1, wantFurther: 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tc.deadline, CpuUsed: tc.cpuUsed}}
			if !e.interAnalysisUsesRDModeDecision() {
				t.Fatalf("deadline=%v cpu_used=%d expected RD on but interAnalysisUsesRDModeDecision()=false", tc.deadline, tc.cpuUsed)
			}
			cfg := e.interAnalysisSearchConfig()
			if got := int(cfg.fullPixelSearchParam); got != tc.wantStep {
				t.Errorf("step_param = %d, want %d (libvpx rdopt.c:2034 = sf->first_step alone, no pickinter speed_adjust)", got, tc.wantStep)
			}
			if got := int(cfg.fullPixelFurtherSteps); got != tc.wantFurther {
				t.Errorf("further_steps = %d, want %d (libvpx rdopt.c:2086 = max-1-step; no Speed>=8 short-circuit on RD path)", got, tc.wantFurther)
			}
			if got := int(cfg.fullPixelSpeedAdjust); got != 0 {
				t.Errorf("speed_adjust = %d under RD, want 0 (libvpx rdopt.c does not apply pickinter speed_adjust)", got)
			}
			if !cfg.fullPixelFinalRefine {
				t.Errorf("fullPixelFinalRefine = false under RD, want true (selects rdopt.c:2086 verbatim formula in adjustedForImprovedMVStart)")
			}
		})
	}
}
