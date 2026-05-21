package govpx

import (
	"math"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

// TestLibvpxEstimateMaxQReturnsMaxOnZeroBudget pins libvpx's
// `if (section_target_bandwidth <= 0) return maxq_max_limit` early
// exit.
func TestLibvpxEstimateMaxQReturnsMaxOnZeroBudget(t *testing.T) {
	got := libvpxEstimateMaxQ(1500, 0, 0, 100.0, 1.0, 1.0, 1.0, 0, 127)
	if got != 127 {
		t.Fatalf("estimate_max_q with zero budget = %d, want maxq_max_limit=127", got)
	}
}

// TestLibvpxEstimateMaxQFindsLowestQAcceptingBudget pins libvpx's
// loop semantics: walk Q from min upward, return the first Q whose
// bits_per_mb_at_q <= target_norm_bits_per_mb. Use a generous budget
// so a low Q satisfies it.
func TestLibvpxEstimateMaxQFindsLowestQAcceptingBudget(t *testing.T) {
	// num_mbs=1500, section_target_bandwidth=10_000_000 -> very large
	// per-MB budget; even Q=0 should pass.
	got := libvpxEstimateMaxQ(1500, 10_000_000, 0, 50.0, 1.0, 1.0, 1.0, 0, 127)
	if got != 0 {
		t.Fatalf("estimate_max_q with very large budget = %d, want 0", got)
	}
}

// TestLibvpxEstimateMaxQReturnsMaxWhenBudgetTooSmall pins the
// fall-through return when no Q satisfies the per-MB target.
func TestLibvpxEstimateMaxQReturnsMaxWhenBudgetTooSmall(t *testing.T) {
	// Tiny target bits relative to err_per_mb=10000 forces fallthrough.
	got := libvpxEstimateMaxQ(1500, 1500, 0, 10000.0, 1.0, 1.0, 1.0, 0, 127)
	if got != 127 {
		t.Fatalf("estimate_max_q with tight budget = %d, want maxq_max_limit=127", got)
	}
}

// TestLibvpxEstimateMaxQHonoursMinLimitAsFloor pins libvpx's
// `for (Q = maxq_min_limit; Q < maxq_max_limit; ...)` floor: the
// search never returns below maxq_min_limit.
func TestLibvpxEstimateMaxQHonoursMinLimitAsFloor(t *testing.T) {
	got := libvpxEstimateMaxQ(1500, 10_000_000, 0, 50.0, 1.0, 1.0, 1.0, 30, 127)
	if got != 30 {
		t.Fatalf("estimate_max_q with min_limit=30 = %d, want 30", got)
	}
}

func TestLibvpxEstimateModeMVCostMatchesLibvpxFormula(t *testing.T) {
	stats := FirstPassFrameStats{
		PcntInter:  1.6,
		PcntMotion: 0.4,
		NewMVCount: 3,
		Count:      2,
	}
	got := libvpxEstimateModeMVCost(stats, 10)
	if got != 10752 {
		t.Fatalf("estimate_modemvcost = %d, want 10752", got)
	}
}

func TestLibvpxBitCostUsesThirteenBitFloor(t *testing.T) {
	if got := libvpxBitCost(0.0001); got != 13.0 {
		t.Fatalf("bitcost floor = %v, want 13", got)
	}
}

// TestLibvpxGetPredictionDecayRateMatchesLibvpxFormula pins the libvpx
// vp8/encoder/firstpass.c get_prediction_decay_rate computation.
// With pcnt_inter=0.9, pcnt_motion=0.2, mvr_abs=10, mvc_abs=10:
//
//	rate = 0.9
//	motion_decay = 1.0 - 0.2/20 = 0.99 (no clamp, 0.9 < 0.99).
//	mv_rabs = |10 * 0.2| = 2; mv_cabs = 2.
//	distance_factor = sqrt(4+4)/250 = 2.828/250 = 0.01131.
//	distance_factor = 1.0 - 0.01131 = 0.9887.
//	rate stays at 0.9 (rate < distance_factor).
func TestLibvpxGetPredictionDecayRateMatchesLibvpxFormula(t *testing.T) {
	stats := FirstPassFrameStats{
		PcntInter:  0.9,
		PcntMotion: 0.2,
		MVrAbs:     10,
		MVcAbs:     10,
	}
	got := libvpxGetPredictionDecayRate(stats)
	if math.Abs(got-0.9) > 1e-9 {
		t.Fatalf("prediction_decay_rate = %v, want ~0.9", got)
	}
}

// TestLibvpxGetPredictionDecayRateLargeMVZerosOut pins the libvpx
// `(distance_factor > 1.0) ? 0.0 : (1.0 - distance_factor)` clamp.
// Large MVs produce distance_factor > 1, which becomes 0.0 and
// dominates the min.
func TestLibvpxGetPredictionDecayRateLargeMVZerosOut(t *testing.T) {
	stats := FirstPassFrameStats{
		PcntInter:  0.9,
		PcntMotion: 1.0,
		MVrAbs:     500,
		MVcAbs:     500,
	}
	got := libvpxGetPredictionDecayRate(stats)
	if got != 0.0 {
		t.Fatalf("large-MV decay rate = %v, want 0.0", got)
	}
}

// TestLibvpxGetPredictionDecayRateMotionDecayClamps pins the libvpx
// `motion_decay = 1.0 - pcnt_motion/20` floor when motion_decay
// becomes the dominant term. With pcnt_inter=0.99, pcnt_motion=10,
// motion_decay=0.5 -> rate clamps to 0.5.
func TestLibvpxGetPredictionDecayRateMotionDecayClamps(t *testing.T) {
	stats := FirstPassFrameStats{
		PcntInter:  0.99,
		PcntMotion: 10.0,
		MVrAbs:     1,
		MVcAbs:     1,
	}
	got := libvpxGetPredictionDecayRate(stats)
	// motion_decay = 1.0 - 10/20 = 0.5.
	// distance_factor = sqrt(100+100)/250 = 14.14/250 = 0.0566 -> 0.9434.
	// rate = min(0.99, 0.5, 0.9434) = 0.5.
	if math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("motion-decay clamp = %v, want 0.5", got)
	}
}

// TestLibvpxDetectTransitionToStillReturnsFalseOnShortInterval pins
// the libvpx `frame_interval > MIN_GF_INTERVAL` gate.
func TestLibvpxDetectTransitionToStillReturnsFalseOnShortInterval(t *testing.T) {
	rates := []float64{1.0, 1.0, 1.0}
	if libvpxDetectTransitionToStill(libvpxMinGFInterval, 3, 0.999, 0.5, rates) {
		t.Fatalf("frame_interval == MIN_GF_INTERVAL should not fire")
	}
}

// TestLibvpxDetectTransitionToStillReturnsFalseOnLowDecayRate pins
// the libvpx `loop_decay_rate >= 0.999` gate.
func TestLibvpxDetectTransitionToStillReturnsFalseOnLowDecayRate(t *testing.T) {
	rates := []float64{1.0, 1.0, 1.0}
	if libvpxDetectTransitionToStill(10, 3, 0.95, 0.5, rates) {
		t.Fatalf("loop_decay_rate < 0.999 should not fire")
	}
}

// TestLibvpxDetectTransitionToStillReturnsFalseOnHighDecayAccum pins
// the libvpx `decay_accumulator < 0.9` gate.
func TestLibvpxDetectTransitionToStillReturnsFalseOnHighDecayAccum(t *testing.T) {
	rates := []float64{1.0, 1.0, 1.0}
	if libvpxDetectTransitionToStill(10, 3, 0.999, 0.95, rates) {
		t.Fatalf("decay_accumulator >= 0.9 should not fire")
	}
}

// TestLibvpxDetectTransitionToStillReturnsTrueOnAllStill pins the
// happy-path: long interval, low accumulator, high decay rate, and
// all next-still rates >= 0.999 -> transition_to_still=true.
func TestLibvpxDetectTransitionToStillReturnsTrueOnAllStill(t *testing.T) {
	rates := []float64{0.999, 1.0, 1.0}
	if !libvpxDetectTransitionToStill(10, 3, 0.999, 0.5, rates) {
		t.Fatalf("all-still lookahead should fire")
	}
}

// TestLibvpxDetectTransitionToStillReturnsFalseOnLookaheadDip pins the
// libvpx loop break: any lookahead frame with decay_rate < 0.999
// breaks the transition-still detection.
func TestLibvpxDetectTransitionToStillReturnsFalseOnLookaheadDip(t *testing.T) {
	rates := []float64{1.0, 0.95, 1.0}
	if libvpxDetectTransitionToStill(10, 3, 0.999, 0.5, rates) {
		t.Fatalf("middle dip should break the lookahead loop")
	}
}

// TestLibvpxCalculateModifiedErrMatchesLibvpxFormula pins libvpx's
//
//	modified_err = av_err * pow(this_err/av_err, vbrbias/100)
//
// where av_err = total_ssim/count.
func TestLibvpxCalculateModifiedErrMatchesLibvpxFormula(t *testing.T) {
	got := libvpxCalculateModifiedErr(200.0, 1000.0, 10, 50)
	avErr := 1000.0 / 10
	want := avErr * math.Pow(200.0/avErr, 0.5)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("calculate_modified_err = %v, want ~%v", got, want)
	}
}

// TestLibvpxCalculateModifiedErrZeroCountReturnsZero pins libvpx's
// safe-fallback when count==0 (governed by govpx; libvpx does not
// guard, but govpx's helper protects against /0).
func TestLibvpxCalculateModifiedErrZeroCountReturnsZero(t *testing.T) {
	if got := libvpxCalculateModifiedErr(200.0, 1000.0, 0, 50); got != 0 {
		t.Fatalf("count=0 = %v, want 0", got)
	}
}

// TestLibvpxCalculateModifiedErrZeroAvErrUsesDoubleDivideCheck pins
// the libvpx DOUBLE_DIVIDE_CHECK fallback: when av_err is ~0, the
// helper substitutes 1.0 in the denominator (so modified_err =
// av_err * pow(this_err, vbrbias/100), but with av_err near 0 the
// product is also near 0).
func TestLibvpxCalculateModifiedErrZeroAvErrUsesDoubleDivideCheck(t *testing.T) {
	got := libvpxCalculateModifiedErr(50.0, 0.0, 10, 50)
	// av_err = 0 -> modified = 0 * pow(50/1, 0.5) = 0. Doesn't blow up.
	if got != 0 {
		t.Fatalf("av_err=0 = %v, want 0", got)
	}
}

// TestLibvpxEstimateQReturnsMaxOnZeroBudget pins libvpx's
// `if (target_norm_bits_per_mb <= 0) return MAXQ` early exit (govpx
// uses vp8common.MaxQ as the libvpx MAXQ analog).
func TestLibvpxEstimateQReturnsMaxOnZeroBudget(t *testing.T) {
	got := libvpxEstimateQ(1500, 0, 100.0, 1.0, 1.0)
	if got != vp8common.MaxQ {
		t.Fatalf("estimate_q with zero budget = %d, want %d", got, vp8common.MaxQ)
	}
}

// TestLibvpxEstimateQFindsLowestQAcceptingBudget pins the libvpx
// estimate_q loop returning the lowest Q whose bits_per_mb_at_q is
// at or below the per-MB target.
func TestLibvpxEstimateQFindsLowestQAcceptingBudget(t *testing.T) {
	got := libvpxEstimateQ(1500, 10_000_000, 50.0, 1.0, 1.0)
	if got != 0 {
		t.Fatalf("estimate_q with very large budget = %d, want 0", got)
	}
}

// TestLibvpxEstimateKFGroupQReturnsDoubleMaxOnEmptyBudget pins libvpx's
// `if (target_norm_bits_per_mb <= 0) return MAXQ * 2;` early exit.
func TestLibvpxEstimateKFGroupQReturnsDoubleMaxOnEmptyBudget(t *testing.T) {
	got := libvpxEstimateKFGroupQ(1500, 0, 100.0, 5.0, 50, 0, 0, 1.0)
	want := (vp8common.MaxQ + 1) * 2
	if got != want {
		t.Fatalf("estimate_kf_group_q with zero budget = %d, want %d", got, want)
	}
}

// TestLibvpxEstimateKFGroupQOvershootIncrementsBeyondMax pins the
// libvpx tail loop that bumps Q (and shrinks bits_per_mb_at_q by
// 0.96 each step) when no Q in [0, MAXQ) satisfies the budget.
func TestLibvpxEstimateKFGroupQOvershootIncrementsBeyondMax(t *testing.T) {
	// Use a tiny budget with high err_per_mb so even at Q=MAXQ the
	// bits are still above target. Q should overshoot MAXQ.
	got := libvpxEstimateKFGroupQ(1500, 1500, 100000.0, 5.0, 50, 1000, 1000, 1.0)
	if got <= vp8common.MaxQ {
		t.Fatalf("estimate_kf_group_q overshoot = %d, want > MAXQ=%d", got, vp8common.MaxQ)
	}
	if got >= (vp8common.MaxQ+1)*2 {
		t.Fatalf("estimate_kf_group_q overshoot = %d, want < MAXQ*2", got)
	}
}

// TestLibvpxEstimateKFGroupQSpendRatioFallback pins the libvpx
// `if (long_rolling_target_bits <= 0) current_spend_ratio = 10.0`
// fallback: caller passes 0 for long_rolling_target_bits and the
// helper still returns a sane Q.
func TestLibvpxEstimateKFGroupQSpendRatioFallback(t *testing.T) {
	got := libvpxEstimateKFGroupQ(1500, 100_000_000, 50.0, 5.0, 50, 0, 0, 1.0)
	if got < 0 || got > (vp8common.MaxQ+1)*2 {
		t.Fatalf("estimate_kf_group_q with long_rolling_target=0 returned out-of-range Q=%d", got)
	}
}

// TestLibvpxCalcCorrectionFactorMatchesLibvpxFormula pins the libvpx
// vp8/encoder/firstpass.c calc_correction_factor:
//
//	error_term = err_per_mb / err_devisor
//	power_term = min(pt_low + Q*0.01, pt_high)
//	cf = pow(error_term, power_term)
//	clamp(cf, 0.05, 5.0)
func TestLibvpxCalcCorrectionFactorMatchesLibvpxFormula(t *testing.T) {
	// err_per_mb=300, err_devisor=150 -> error_term=2.0.
	// Q=20 -> power_term = 0.40 + 20*0.01 = 0.60 (< 0.90 cap).
	// cf = pow(2.0, 0.60) ~ 1.5157.
	got := libvpxCalcCorrectionFactor(300.0, 150.0, 0.40, 0.90, 20)
	want := math.Pow(2.0, 0.60)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("calc_correction_factor = %v, want ~%v", got, want)
	}
}

// TestLibvpxCalcCorrectionFactorClampsBelow005 pins the lower clamp
// at 0.05.
func TestLibvpxCalcCorrectionFactorClampsBelow005(t *testing.T) {
	// err_per_mb=1, err_devisor=1e6 -> error_term=1e-6.
	// power_term=0.4. cf = pow(1e-6, 0.4) ~ 0.00398 -> clamped to 0.05.
	got := libvpxCalcCorrectionFactor(1.0, 1e6, 0.40, 0.90, 0)
	if got != 0.05 {
		t.Fatalf("calc_correction_factor lower clamp = %v, want 0.05", got)
	}
}

// TestLibvpxCalcCorrectionFactorClampsAbove50 pins the upper clamp at 5.0.
func TestLibvpxCalcCorrectionFactorClampsAbove50(t *testing.T) {
	// err_per_mb=1e6, err_devisor=1 -> error_term=1e6. cf will exceed 5.0.
	got := libvpxCalcCorrectionFactor(1e6, 1.0, 0.40, 0.90, 100)
	if got != 5.0 {
		t.Fatalf("calc_correction_factor upper clamp = %v, want 5.0", got)
	}
}

// TestLibvpxCalcCorrectionFactorClampsPowerTermAtPtHigh pins the
// `power_term = (power_term > pt_high) ? pt_high : power_term` cap.
// At Q=200, raw power_term = 0.4 + 2.0 = 2.4, clamped to pt_high=0.90.
func TestLibvpxCalcCorrectionFactorClampsPowerTermAtPtHigh(t *testing.T) {
	// err_per_mb=300, err_devisor=150 -> error_term=2.0.
	// raw power_term=0.4+2.0=2.4 -> clamped to 0.90.
	// cf = pow(2.0, 0.90) ~ 1.866.
	got := libvpxCalcCorrectionFactor(300.0, 150.0, 0.40, 0.90, 200)
	want := math.Pow(2.0, 0.90)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("calc_correction_factor with clamped power = %v, want ~%v", got, want)
	}
}

// TestLibvpxEstimateMaxQRollingRatioAdjustmentRatioBelowOneShrinks
// pins the libvpx
// `if (rolling_ratio < 0.95) est_max_qcorrection_factor -= 0.005`
// branch.
func TestLibvpxEstimateMaxQRollingRatioAdjustmentRatioBelowOneShrinks(t *testing.T) {
	got := libvpxEstimateMaxQRollingRatioAdjustment(1.0, 900, 1000)
	if got != 0.995 {
		t.Fatalf("ratio<0.95 update = %v, want 0.995", got)
	}
}

// TestLibvpxEstimateMaxQRollingRatioAdjustmentRatioAboveOneGrows pins
// the libvpx `if (rolling_ratio > 1.05) factor += 0.005` branch.
func TestLibvpxEstimateMaxQRollingRatioAdjustmentRatioAboveOneGrows(t *testing.T) {
	got := libvpxEstimateMaxQRollingRatioAdjustment(1.0, 1100, 1000)
	if got != 1.005 {
		t.Fatalf("ratio>1.05 update = %v, want 1.005", got)
	}
}

// TestLibvpxEstimateMaxQRollingRatioAdjustmentClamps01To10 pins the
// libvpx `clamp(factor, 0.1, 10.0)` clamp.
func TestLibvpxEstimateMaxQRollingRatioAdjustmentClamps01To10(t *testing.T) {
	got := libvpxEstimateMaxQRollingRatioAdjustment(0.05, 900, 1000)
	if got != 0.1 {
		t.Fatalf("lower clamp = %v, want 0.1", got)
	}
	got = libvpxEstimateMaxQRollingRatioAdjustment(20.0, 1100, 1000)
	if got != 10.0 {
		t.Fatalf("upper clamp = %v, want 10.0", got)
	}
}

// TestLibvpxEstimateMaxQRollingRatioAdjustmentInRangeNoChange pins
// the libvpx `else` branch: 0.95 <= ratio <= 1.05 leaves the factor
// unchanged.
func TestLibvpxEstimateMaxQRollingRatioAdjustmentInRangeNoChange(t *testing.T) {
	got := libvpxEstimateMaxQRollingRatioAdjustment(1.0, 1000, 1000)
	if got != 1.0 {
		t.Fatalf("in-range update = %v, want 1.0", got)
	}
}

// TestLibvpxEstimateMaxQRollingRatioAdjustmentZeroTargetIsNoOp pins
// the libvpx `if (rolling_target_bits > 0)` outer guard.
func TestLibvpxEstimateMaxQRollingRatioAdjustmentZeroTargetIsNoOp(t *testing.T) {
	got := libvpxEstimateMaxQRollingRatioAdjustment(2.5, 1000, 0)
	if got != 2.5 {
		t.Fatalf("zero-target update = %v, want 2.5 unchanged", got)
	}
}
