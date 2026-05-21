package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

// TestApplyEstMaxQRollingRatioGateRequiresPositiveTarget pins the
// outer-half of the libvpx gate at vp8/encoder/firstpass.c lines
// 923-924:
//
//	if ((cpi->rolling_target_bits > 0) &&
//	    (cpi->active_worst_quality < cpi->worst_quality)) { ... }
//
// When rolling_target_bits <= 0 (the initial state before any frame
// has been encoded, or when the rate controller drains the rolling
// averages to zero), the rolling-ratio update is a no-op and the
// est_max_qcorrection_factor is preserved.
func TestApplyEstMaxQRollingRatioGateRequiresPositiveTarget(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      40,
		pass2ActiveWorstQValid: true,
		rollingActualBits:      900,
		rollingTargetBits:      0,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("zero-target gate: factor mutated to %v, want unchanged 1.0", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioGateRequiresActiveBelowWorst pins the
// inner-half of the libvpx gate at firstpass.c line 924:
//
//	cpi->active_worst_quality < cpi->worst_quality
//
// When the regulator's active_worst_quality has been pushed all the
// way up to oxcf.worst_allowed_q (the configured ceiling), libvpx
// suppresses the rolling-ratio nudge so the factor cannot drag the
// already-pegged worst-Q regulator any further.
func TestApplyEstMaxQRollingRatioGateRequiresActiveBelowWorst(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      127,
		pass2ActiveWorstQValid: true,
		rollingActualBits:      800,
		rollingTargetBits:      1000,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("active==worst gate: factor mutated to %v, want unchanged 1.0", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioGateRequiresValidSeed pins govpx's
// equivalent of libvpx's `current_video_frame == 0` short-circuit:
// before the pass-2 seed has run, the regulator has not yet pushed
// active_worst_quality below worst_quality (libvpx initializes them
// equal at vp8_init_first_pass / vp8_new_framerate). Without a valid
// seed govpx treats the gate as closed so the very first
// estimate_max_q call keeps the factor at its libvpx-shaped 1.0
// initial value.
func TestApplyEstMaxQRollingRatioGateRequiresValidSeed(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      40,
		pass2ActiveWorstQValid: false,
		rollingActualBits:      800,
		rollingTargetBits:      1000,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("pre-seed gate: factor mutated to %v, want unchanged 1.0", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioStepDown pins the libvpx
// firstpass.c:930-931 `ratio < 0.95 -> factor -= 0.005` branch when
// the gate is open. Setup mirrors a mid-clip pass-2 frame where the
// rate controller has been underspending: rolling_actual_bits /
// rolling_target_bits = 0.9, so the factor steps down 0.005.
func TestApplyEstMaxQRollingRatioStepDown(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      40,
		pass2ActiveWorstQValid: true,
		rollingActualBits:      900,
		rollingTargetBits:      1000,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 0.995 {
		t.Fatalf("ratio<0.95 step: factor=%v, want 0.995", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioStepUp pins the libvpx
// firstpass.c:932-933 `ratio > 1.05 -> factor += 0.005` branch when
// the gate is open. Setup mirrors a mid-clip pass-2 frame where the
// rate controller has been overspending: rolling_actual_bits /
// rolling_target_bits = 1.1.
func TestApplyEstMaxQRollingRatioStepUp(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      40,
		pass2ActiveWorstQValid: true,
		rollingActualBits:      1100,
		rollingTargetBits:      1000,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 1.005 {
		t.Fatalf("ratio>1.05 step: factor=%v, want 1.005", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioStepDeadZone pins the libvpx
// firstpass.c:930-934 `else` branch: ratio in [0.95, 1.05] leaves
// the factor unchanged. The gate is open (positive target, active <
// worst) so we are exercising the dead-zone of the libvpx
// piecewise update, not the outer guards.
func TestApplyEstMaxQRollingRatioStepDeadZone(t *testing.T) {
	ts := &twoPassState{
		estMaxQCorrection:      1.0,
		worstQuality:           127,
		pass2ActiveWorstQ:      40,
		pass2ActiveWorstQValid: true,
		rollingActualBits:      1000,
		rollingTargetBits:      1000,
	}
	ts.applyEstMaxQRollingRatioAdjustment()
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("dead-zone step: factor=%v, want unchanged 1.0", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioRunsInSeedPass2 pins the integration
// of the rolling-ratio nudge into seedPass2ActiveWorstQ. The libvpx
// equivalent is firstpass.c line 2349 calling estimate_max_q, which
// then runs the rolling-ratio update at lines 920-941 BEFORE the
// Q-search. govpx mirrors this by invoking
// applyEstMaxQRollingRatioAdjustment as the first step of
// seedPass2ActiveWorstQ's q-search prep. We pre-seed
// pass2ActiveWorstQValid=true with active=40<worst=127 so the gate
// is open, supply rolling bits with ratio 0.9, and assert that
// est_max_qcorrection_factor stepped down by 0.005 after the seed
// runs.
func TestApplyEstMaxQRollingRatioRunsInSeedPass2(t *testing.T) {
	stats := make([]FirstPassFrameStats, 32)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError: 5000,
			CodedError: 1000,
			Count:      1,
			PcntInter:  0.8,
		}
	}
	ts := &twoPassState{}
	ts.configure(stats, 50_000, 50, 0, 400)
	ts.configureFrameDims(128, 128)
	ts.configureQuantizerBounds(0, vp8common.MaxQ)
	// Pre-seed a valid active_worst_quality below the configured
	// worst, so the libvpx gate is open even on this frame.
	ts.pass2ActiveWorstQ = 40
	ts.pass2ActiveWorstQValid = true
	ts.setRollingBits(900, 1000)
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("setup: estMaxQCorrection = %v, want 1.0", ts.estMaxQCorrection)
	}
	ts.seedPass2ActiveWorstQ(50_000)
	if ts.estMaxQCorrection != 0.995 {
		t.Fatalf("seed pass-2: estMaxQCorrection = %v, want 0.995 (rolling-ratio step down)", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioRunsInDampedUpdate pins the integration
// of the rolling-ratio nudge into dampedUpdatePass2ActiveWorstQ. The
// libvpx equivalent is firstpass.c line 2381 calling estimate_max_q,
// which then runs the rolling-ratio update at lines 920-941. With
// the damped-branch window gate open and rolling bits indicating
// overshoot (ratio 1.1), the factor must step up by 0.005.
func TestApplyEstMaxQRollingRatioRunsInDampedUpdate(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.setRollingBits(1100, 1000)
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("setup: estMaxQCorrection = %v, want 1.0", ts.estMaxQCorrection)
	}
	ts.frameIndex = 1
	ts.dampedUpdatePass2ActiveWorstQ(1)
	if ts.estMaxQCorrection != 1.005 {
		t.Fatalf("damped update: estMaxQCorrection = %v, want 1.005 (rolling-ratio step up)", ts.estMaxQCorrection)
	}
}

// TestApplyEstMaxQRollingRatioNoOpInTrailingClip pins that the
// rolling-ratio update fires only inside libvpx's estimate_max_q
// — i.e., the seed and damped branches of vp8_second_pass. In the
// trailing portion of the clip (past the (count*255)>>8 window) the
// damped branch returns before invoking estimate_max_q, so the
// factor is preserved across that frame even when rolling bits
// would otherwise nudge it.
func TestApplyEstMaxQRollingRatioNoOpInTrailingClip(t *testing.T) {
	ts := makeDampedActiveWorstQState(t, 256, 4, 40)
	ts.setRollingBits(1100, 1000)
	ts.frameIndex = 255
	ts.dampedUpdatePass2ActiveWorstQ(255)
	if ts.estMaxQCorrection != 1.0 {
		t.Fatalf("trailing-clip gate closed but factor moved: got %v, want unchanged 1.0", ts.estMaxQCorrection)
	}
}

// TestSetRollingBitsStoresValues pins the simple field-update
// semantics of setRollingBits. The encoder calls this from
// vp8_encoder_frame.go before frameTargetBitsWithAltRef so the next
// pass-2 frame's estimate_max_q-equivalents see the latest rolling
// stats.
func TestSetRollingBitsStoresValues(t *testing.T) {
	ts := &twoPassState{}
	ts.setRollingBits(1234, 5678)
	if ts.rollingActualBits != 1234 {
		t.Fatalf("rollingActualBits = %d, want 1234", ts.rollingActualBits)
	}
	if ts.rollingTargetBits != 5678 {
		t.Fatalf("rollingTargetBits = %d, want 5678", ts.rollingTargetBits)
	}
}
