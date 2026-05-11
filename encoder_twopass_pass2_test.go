package govpx

import (
	"testing"
)

// TestTwoPassFramesToKeyHonoursTestCandidateKF pins the
// libvpxTestCandidateKeyFrame predicate firing inside framesToKey.
// Build stats where frame 6 is a clear scene cut (low intra/coded
// ratio drop) so the predicate fires after the MIN_GF_INTERVAL=4
// gate.
func TestTwoPassFramesToKeyHonoursTestCandidateKF(t *testing.T) {
	stats := make([]FirstPassFrameStats, 50)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError: 10000,
			CodedError: 100,
			PcntInter:  0.99,
		}
	}
	// Frame 6: simulate a scene cut by inverting intra/coded.
	for i := 6; i <= 12; i++ {
		stats[i] = FirstPassFrameStats{
			IntraError:    100,
			CodedError:    9000,
			PcntInter:     0.05,
			PcntSecondRef: 0.0,
			PcntNeutral:   0.0,
		}
	}
	var ts twoPassState
	ts.configure(stats, 1000, 50, 50, 200)
	got := ts.framesToKey(0, 30)
	// Predicate-driven KF should fire well before the 30-frame floor.
	if got > 20 {
		t.Fatalf("framesToKey with scene cut at frame 6 = %d, want <= 20", got)
	}
	if got < libvpxMinGFInterval {
		t.Fatalf("framesToKey = %d, want >= MIN_GF_INTERVAL=%d", got, libvpxMinGFInterval)
	}
}

// firstPassRegression* values pin every libvpx-aligned FIRSTPASS_STATS field
// on the deterministic 32x32 ramp clip above. TestOracleFirstPassStatsCompare
// separately gates these values against empirical libvpx output with the small
// quality-equivalent tolerance used for predictor-residual rounding.
//
// Frame 0 has no LAST so MV stats are zero; coded_error == intra_error.
// LAST searches run against the reconstructed first-pass reference at libvpx's
// fixed pass-1 q=26, while encode_breakout raw checks use the separate prior
// source buffer. Frame 2 also sees the initial GOLDEN reference for the
// second-ref experiment.
//

// TestPass2VBRSectionLimitClampsTarget pins the libvpx
// vp8/encoder/firstpass.c Pass2Encode VBR section-limit application:
// per-frame target is clamped to [0, section_max_bits] where
// section_max_bits derives from
// `cpi->oxcf.two_pass_vbrmax_section` applied to the live VBR
// per-frame budget. libvpx does NOT clamp pass-2 frame targets at a
// section_min — instead, `min_frame_bandwidth` is added as an additive
// floor inside assign_std_frame_bits; pass2VBRSectionLimits therefore
// returns sectionMin=0. The test asserts the upward-clamp branch
// (modified_err >> avg) drops the target to sectionMax, and verifies
// the downward case lands somewhere reasonable above the additive
// min_frame_bandwidth floor that finishFrame credits per visible
// frame.
func TestPass2VBRSectionLimitClampsTarget(t *testing.T) {
	stats := makeTwoPassSpikyStats(10)
	const (
		perFrame = 1000
		biasPct  = 100
		minPct   = 50
		maxPct   = 150
	)
	var ts twoPassState
	ts.configure(stats, perFrame, biasPct, minPct, maxPct)
	// libvpx-parity: pass2VBRSectionLimits returns sectionMin=0 (the
	// per-frame min_frame_bandwidth is an additive floor inside
	// assign_std_frame_bits, not a clamp on the err-fraction target).
	highMin, highMax := ts.pass2VBRSectionLimits(0, perFrame)
	if highMin != 0 {
		t.Fatalf("section min = %d, want 0 (libvpx pass-2 has no err-fraction floor)", highMin)
	}
	wantMax := int64(libvpxFrameMaxBitsVBR(ts.bitsLeft, int64(len(stats)), maxPct))
	if highMax != wantMax {
		t.Fatalf("section max = %d, want live VBR max %d", highMax, wantMax)
	}
	// First frame is the synthetic KF (highest err); its err-fraction
	// target is bound by the libvpx KF cap (max_bits * frames_to_key)
	// rather than the per-frame VBR ceiling. Use a non-KF frame to
	// exercise the std-frame VBR cap branch.
	got := ts.frameTargetBits(1, false, perFrame)
	if int64(got) > wantMax {
		t.Fatalf("frame target = %d, exceeds section max %d", got, wantMax)
	}
	// The std-frame target should be at least the additive
	// min_frame_bandwidth floor (libvpx adds it after the err-fraction
	// target inside assign_std_frame_bits).
	if int64(got) < int64(ts.minFrameBandwidth) {
		t.Fatalf("frame target = %d, below additive min_frame_bandwidth floor %d",
			got, ts.minFrameBandwidth)
	}
}

// TestPass2ARFPendingTriggersFromHighMotionSection pins the libvpx
// vp8/encoder/firstpass.c `define_gf_group` / `select_arf_period`
// ARF-pending decision. A synthetic stats sequence with a stable
// high-prediction-quality (high intra/coded ratio, high pcnt_inter)
// section coming up should trigger sourceAltRefPending and arm
// framesTillAltRefFrame to a positive value via scheduleAltRefSource.
func TestPass2ARFPendingTriggersFromHighMotionSection(t *testing.T) {
	const sectionLen = 16
	stats := make([]FirstPassFrameStats, sectionLen)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError:    20000,
			CodedError:    200,
			PcntInter:     0.95,
			PcntMotion:    0.4,
			PcntSecondRef: 0.0,
			PcntNeutral:   0.0,
			MVrAbs:        5,
			MVcAbs:        5,
			Count:         1,
		}
	}
	var ts twoPassState
	ts.configure(stats, 1000, 100, 50, 200)
	interval, pending := ts.pass2DetectARFPending(0, sectionLen, true, libvpxMinGFInterval+8)
	if !pending {
		t.Fatalf("pass2DetectARFPending returned pending=false on high-motion section")
	}
	if interval < libvpxMinGFInterval {
		t.Fatalf("ARF interval = %d, want >= MIN_GF_INTERVAL=%d", interval, libvpxMinGFInterval)
	}

	// Wire the encoder side: pass2MaybeArmAltRefPending should call
	// scheduleAltRefSource so sourceAltRefPending and
	// framesTillAltRefFrame both transition to "armed" state.
	enc := &VP8Encoder{
		opts: EncoderOptions{
			AutoAltRef:       true,
			LookaheadFrames:  sectionLen + 1,
			KeyFrameInterval: 0,
		},
	}
	enc.twoPass = ts
	enc.pass2MaybeArmAltRefPending(0, 0, false)
	if !enc.sourceAltRefPending {
		t.Fatalf("sourceAltRefPending = false after high-motion section, want true")
	}
	if enc.framesTillAltRefFrame <= 0 {
		t.Fatalf("framesTillAltRefFrame = %d, want > 0", enc.framesTillAltRefFrame)
	}
	if !enc.altRefSourceValid {
		t.Fatalf("altRefSourceValid = false, scheduleAltRefSource must record the future PTS")
	}
}
