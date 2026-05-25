package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

// TestVP8SpeedFeaturesAdjustedImprovedMVStartMirrorsLibvpxBothPaths pins
// the picker-vs-RD split applied by adjustedForImprovedMVStart after
// vp8_mv_pred returns a non-zero sr. This is the fix surface:
//
//   - RD path (rdopt.c:2076/2086): if sr > step_param then
//     step_param = sr; further_steps = max-1-step_param  (no Speed>=8 cap).
//   - Picker path (pickinter.c:971/973/1005-1008): sr += speed_adjust;
//     if sr > step_param then step_param = sr; further_steps =
//     (Speed>=8 ? 0 : max-1-step_param).
//
// govpx's adjustedForImprovedMVStart switches on fullPixelFinalRefine
// (= interAnalysisUsesRDModeDecision()) to pick the libvpx-correct
// formula. This test exercises both branches at concrete sr values that
// would bump step_param past the initial first_step, including the
// BestQuality+cpu_used=8 cohort where the picker formula would
// incorrectly cap further_steps to 0.
func TestVP8SpeedFeaturesAdjustedImprovedMVStartMirrorsLibvpxBothPaths(t *testing.T) {
	type adjCase struct {
		name        string
		deadline    Deadline
		cpuUsed     int
		sr          int // value returned by improved-MV predictor
		wantStep    int // post-adjustedForImprovedMVStart step_param
		wantFurther int // post-adjustedForImprovedMVStart further_steps
		wantRD      bool
	}
	cases := []adjCase{
		// BestQuality+cpu_used=8 regression cohort. RD path:
		// step_param = sr (since fullPixelSpeedAdjust=0 under RD, and
		// initial fullPixelSearchParam=first_step=0; any sr>0 bumps).
		// further_steps = max(7-sr, 0). NO Speed>=8 short-circuit.
		{name: "best-cpu-8-sr-1", deadline: DeadlineBestQuality, cpuUsed: 8, sr: 1, wantStep: 1, wantFurther: 6, wantRD: true},
		{name: "best-cpu-8-sr-3", deadline: DeadlineBestQuality, cpuUsed: 8, sr: 3, wantStep: 3, wantFurther: 4, wantRD: true},
		{name: "best-cpu-8-sr-5", deadline: DeadlineBestQuality, cpuUsed: 8, sr: 5, wantStep: 5, wantFurther: 2, wantRD: true},
		{name: "best-cpu-8-sr-7", deadline: DeadlineBestQuality, cpuUsed: 8, sr: 7, wantStep: 7, wantFurther: 0, wantRD: true},
		// BestQuality+cpu_used=0. RD path same as above.
		{name: "best-cpu-0-sr-2", deadline: DeadlineBestQuality, cpuUsed: 0, sr: 2, wantStep: 2, wantFurther: 5, wantRD: true},
		// GoodQuality+cpu_used=3 (RD on). step_param=first_step=1
		// initially; sr+speed_adjust (=sr+0 under RD) bumps only if
		// sr > 1.
		{name: "good-cpu-3-sr-2", deadline: DeadlineGoodQuality, cpuUsed: 3, sr: 2, wantStep: 2, wantFurther: 5, wantRD: true},
		{name: "good-cpu-3-sr-1", deadline: DeadlineGoodQuality, cpuUsed: 3, sr: 1, wantStep: 1, wantFurther: 6, wantRD: true},
		// GoodQuality+cpu_used=0 (RD on, first_step=0). sr=2 bumps to 2.
		{name: "good-cpu-0-sr-2", deadline: DeadlineGoodQuality, cpuUsed: 0, sr: 2, wantStep: 2, wantFurther: 5, wantRD: true},
		// Realtime+cpu_used=-2 (RD on, Speed=2, first_step=1). sr=3
		// bumps to 3.
		{name: "rt-cpu-neg2-sr-3", deadline: DeadlineRealtime, cpuUsed: -2, sr: 3, wantStep: 3, wantFurther: 4, wantRD: true},
		// Picker path: Realtime+cpu_used=-8 (Speed=8, RD off). Initial
		// step_param = first_step+speed_adjust = 1+3 = 4. sr=2 →
		// sr+speed_adjust=5 > 4 → step_param=5. further_steps =
		// (Speed>=8 ? 0 : max-1-step) = 0.
		{name: "rt-cpu-neg8-sr-2", deadline: DeadlineRealtime, cpuUsed: -8, sr: 2, wantStep: 5, wantFurther: 0, wantRD: false},
		// Picker path: Realtime+cpu_used=-5 (Speed=5, RD off). Initial
		// step_param = 1+1 = 2. sr=3 → sr+speed_adjust=4 > 2 →
		// step_param=4. further_steps = max-1-4 = 3 (Speed<8 so no
		// short-circuit).
		{name: "rt-cpu-neg5-sr-3", deadline: DeadlineRealtime, cpuUsed: -5, sr: 3, wantStep: 4, wantFurther: 3, wantRD: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: tc.deadline, CpuUsed: tc.cpuUsed}}
			if got := e.interAnalysisUsesRDModeDecision(); got != tc.wantRD {
				t.Fatalf("interAnalysisUsesRDModeDecision() = %v, want %v (deadline=%v cpu_used=%d)", got, tc.wantRD, tc.deadline, tc.cpuUsed)
			}
			cfg := e.interAnalysisSearchConfig()
			start := newInterFrameSearchStart(vp8enc.MotionVector{}, tc.sr, 0)
			adjusted := cfg.adjustedForImprovedMVStart(start)
			if got := int(adjusted.fullPixelSearchParam); got != tc.wantStep {
				t.Errorf("step_param = %d, want %d (libvpx %s)", got, tc.wantStep, libvpxFormulaForRD(tc.wantRD))
			}
			if got := int(adjusted.fullPixelFurtherSteps); got != tc.wantFurther {
				t.Errorf("further_steps = %d, want %d (libvpx %s)", got, tc.wantFurther, libvpxFurtherFormulaForRD(tc.wantRD))
			}
		})
	}
}
