package govpx

import (
	"testing"
)

// TestRecodeRestoresFullCodingContext mirrors libvpx
// vp8/encoder/onyx_if.c's vp8_save_coding_context / vp8_restore_coding_context
// contract: every field listed in CODING_CONTEXT must be restored to its
// pre-loop snapshot when the recode loop rejects an attempt.
func TestRecodeRestoresFullCodingContext(t *testing.T) {
	e := newTestEncoder(t)

	// Seed the encoder with non-default values across every libvpx-listed
	// CODING_CONTEXT field plus the ref/skip-prob siblings govpx restores.
	e.rc.framesSinceKeyframe = 4
	e.loopFilterLevel = 11
	e.rc.framesTillGFUpdateDue = 7
	e.framesSinceGolden = 3
	e.rc.thisFramePercentIntra = 42
	e.modeProbs.YMode[0] = 17
	e.modeProbs.UVMode[0] = 19
	e.modeProbs.BMode[0] = 23
	e.modeProbs.MV[0][0] = 31
	e.modeProbs.MV[1][0] = 37
	e.coefProbs[0][0][0][0] = 41
	e.refProbIntra = 71
	e.refProbLast = 73
	e.refProbGolden = 79
	e.probSkipFalse = 83
	e.lastSkipFalseProbs = [3]uint8{89, 97, 101}

	baseline := struct {
		framesSinceKey        int
		filterLevel           uint8
		framesTillGFUpdateDue int
		framesSinceGolden     int
		thisFramePercentIntra int
		yMode0                uint8
		uvMode0               uint8
		bMode0                uint8
		mv00                  uint8
		mv10                  uint8
		coef0000              uint8
		refIntra              uint8
		refLast               uint8
		refGolden             uint8
		probSkipFalse         uint8
		lastSkipFalseProbs    [3]uint8
	}{
		framesSinceKey:        e.rc.framesSinceKeyframe,
		filterLevel:           e.loopFilterLevel,
		framesTillGFUpdateDue: e.rc.framesTillGFUpdateDue,
		framesSinceGolden:     e.framesSinceGolden,
		thisFramePercentIntra: e.rc.thisFramePercentIntra,
		yMode0:                e.modeProbs.YMode[0],
		uvMode0:               e.modeProbs.UVMode[0],
		bMode0:                e.modeProbs.BMode[0],
		mv00:                  e.modeProbs.MV[0][0],
		mv10:                  e.modeProbs.MV[1][0],
		coef0000:              e.coefProbs[0][0][0][0],
		refIntra:              e.refProbIntra,
		refLast:               e.refProbLast,
		refGolden:             e.refProbGolden,
		probSkipFalse:         e.probSkipFalse,
		lastSkipFalseProbs:    e.lastSkipFalseProbs,
	}

	e.saveCodingContext()

	// Aggressively mutate every snapshotted field, simulating an attempt
	// that mutated the coding context before being rejected.
	e.rc.framesSinceKeyframe = 99
	e.loopFilterLevel = 200
	e.rc.framesTillGFUpdateDue = 0
	e.framesSinceGolden = 100
	e.rc.thisFramePercentIntra = 0
	e.modeProbs.YMode[0] = 0
	e.modeProbs.UVMode[0] = 0
	e.modeProbs.BMode[0] = 0
	e.modeProbs.MV[0][0] = 0
	e.modeProbs.MV[1][0] = 0
	e.coefProbs[0][0][0][0] = 0
	e.refProbIntra = 0
	e.refProbLast = 0
	e.refProbGolden = 0
	e.probSkipFalse = 0
	e.lastSkipFalseProbs = [3]uint8{0, 0, 0}

	e.restoreCodingContext()

	if e.rc.framesSinceKeyframe != baseline.framesSinceKey {
		t.Fatalf("framesSinceKeyframe = %d, want %d", e.rc.framesSinceKeyframe, baseline.framesSinceKey)
	}
	if e.loopFilterLevel != baseline.filterLevel {
		t.Fatalf("loopFilterLevel = %d, want %d", e.loopFilterLevel, baseline.filterLevel)
	}
	if e.rc.framesTillGFUpdateDue != baseline.framesTillGFUpdateDue {
		t.Fatalf("framesTillGFUpdateDue = %d, want %d", e.rc.framesTillGFUpdateDue, baseline.framesTillGFUpdateDue)
	}
	if e.framesSinceGolden != baseline.framesSinceGolden {
		t.Fatalf("framesSinceGolden = %d, want %d", e.framesSinceGolden, baseline.framesSinceGolden)
	}
	if e.rc.thisFramePercentIntra != baseline.thisFramePercentIntra {
		t.Fatalf("thisFramePercentIntra = %d, want %d", e.rc.thisFramePercentIntra, baseline.thisFramePercentIntra)
	}
	if e.modeProbs.YMode[0] != baseline.yMode0 {
		t.Fatalf("modeProbs.YMode[0] = %d, want %d", e.modeProbs.YMode[0], baseline.yMode0)
	}
	if e.modeProbs.UVMode[0] != baseline.uvMode0 {
		t.Fatalf("modeProbs.UVMode[0] = %d, want %d", e.modeProbs.UVMode[0], baseline.uvMode0)
	}
	if e.modeProbs.BMode[0] != baseline.bMode0 {
		t.Fatalf("modeProbs.BMode[0] = %d, want %d", e.modeProbs.BMode[0], baseline.bMode0)
	}
	if e.modeProbs.MV[0][0] != baseline.mv00 {
		t.Fatalf("modeProbs.MV[0][0] = %d, want %d", e.modeProbs.MV[0][0], baseline.mv00)
	}
	if e.modeProbs.MV[1][0] != baseline.mv10 {
		t.Fatalf("modeProbs.MV[1][0] = %d, want %d", e.modeProbs.MV[1][0], baseline.mv10)
	}
	if e.coefProbs[0][0][0][0] != baseline.coef0000 {
		t.Fatalf("coefProbs[0][0][0][0] = %d, want %d", e.coefProbs[0][0][0][0], baseline.coef0000)
	}
	if e.refProbIntra != baseline.refIntra || e.refProbLast != baseline.refLast || e.refProbGolden != baseline.refGolden {
		t.Fatalf("ref probs = intra:%d last:%d golden:%d, want %d/%d/%d",
			e.refProbIntra, e.refProbLast, e.refProbGolden,
			baseline.refIntra, baseline.refLast, baseline.refGolden)
	}
	if e.probSkipFalse != baseline.probSkipFalse {
		t.Fatalf("probSkipFalse = %d, want %d", e.probSkipFalse, baseline.probSkipFalse)
	}
	if e.lastSkipFalseProbs != baseline.lastSkipFalseProbs {
		t.Fatalf("lastSkipFalseProbs = %v, want %v", e.lastSkipFalseProbs, baseline.lastSkipFalseProbs)
	}
}

// TestRecodeForcedKeyFrameRetriesAtAdjustedQ exercises libvpx
// vp8/encoder/onyx_if.c's "Special case handling for forced key frames" branch
// in encode_frame_to_data_rate around line 4065. When the SS error of the
// forced-KF reconstruction is much larger than ambient_err, q_high is lowered
// and Q is set to (q_high + q_low) >> 1; the inverse holds when the KF SS
// error is much smaller. The unit test drives forcedKeyFrameRecodeQuantizer
// directly so the libvpx Q-adjustment formula is locked in.
func TestRecodeForcedKeyFrameRetriesAtAdjustedQ(t *testing.T) {
	e := newTestEncoder(t)
	rc := &e.rc

	// Case 1: kf_err > ambient_err * 7/8 -> lower q_high to (Q-1), Q := mid.
	rc.currentQuantizer = 60
	recode := frameSizeRecodeState{qLow: 10, qHigh: 80}
	q, recoded := rc.forcedKeyFrameRecodeQuantizer(8800, 1000, &recode)
	if !recoded {
		t.Fatalf("kf_err > ambient*7/8 should trigger recode (returned recoded=false)")
	}
	if recode.qHigh != 59 {
		t.Fatalf("qHigh after lossy KF branch = %d, want 59 (Q-1)", recode.qHigh)
	}
	if want := (recode.qHigh + recode.qLow) >> 1; q != want {
		t.Fatalf("q after lossy KF branch = %d, want (qHigh+qLow)>>1 = %d", q, want)
	}

	// Case 2: kf_err < ambient_err / 2 -> raise q_low to (Q+1), Q := mid+1.
	rc.currentQuantizer = 30
	recode = frameSizeRecodeState{qLow: 10, qHigh: 80}
	q, recoded = rc.forcedKeyFrameRecodeQuantizer(100, 1000, &recode)
	if !recoded {
		t.Fatalf("kf_err < ambient/2 should trigger recode (returned recoded=false)")
	}
	if recode.qLow != 31 {
		t.Fatalf("qLow after much-better KF branch = %d, want 31 (Q+1)", recode.qLow)
	}
	if want := (recode.qHigh + recode.qLow + 1) >> 1; q != want {
		t.Fatalf("q after much-better KF branch = %d, want (qHigh+qLow+1)>>1 = %d", q, want)
	}

	// Case 3: kf_err in the libvpx "neither too lossy nor much better"
	// window [ambient/2, ambient*7/8] -> no recode, Q unchanged.
	rc.currentQuantizer = 40
	recode = frameSizeRecodeState{qLow: 10, qHigh: 80}
	q, recoded = rc.forcedKeyFrameRecodeQuantizer(800, 1000, &recode)
	if recoded {
		t.Fatalf("kf_err in libvpx neutral band should not trigger recode")
	}
	if q != 40 {
		t.Fatalf("q with no recode = %d, want unchanged 40", q)
	}

	// Case 4: ambient_err <= 0 -> branch is disabled, no recode.
	rc.currentQuantizer = 20
	recode = frameSizeRecodeState{qLow: 0, qHigh: 80}
	q, recoded = rc.forcedKeyFrameRecodeQuantizer(123, 0, &recode)
	if recoded {
		t.Fatalf("ambient_err <= 0 should disable forced-KF branch")
	}
	if q != 20 {
		t.Fatalf("q with disabled branch = %d, want unchanged 20", q)
	}

	// Case 5: end-to-end through encodeKeyFrameWithQuantizerFeedback. Seed
	// ambient_err and engage thisKeyFrameForced; confirm that the recode
	// loop walks the q-bound state. We exercise the integration path by
	// running a real key-frame encode loop with a tiny image; the precise Q
	// outcome depends on the encoded SS error, but the loop must terminate
	// and the loop counter must be at least 1.
	enc := newTestEncoder(t)
	enableOracleTraceForTest(enc)
	enc.thisKeyFrameForced = true
	enc.ambientErr = 1
	enc.frameCount = 1
	dst := make([]byte, 16384)
	src := rateControlTestFrame(16, 16, 11)
	if _, err := enc.EncodeInto(dst, src, 1, 1, EncodeForceKeyFrame); err != nil {
		t.Fatalf("forced-KF EncodeInto returned error: %v", err)
	}
	if oracleTraceBuild && enc.oracleTraceRecodeLoopCountForTest() < 1 {
		t.Fatalf("oracle trace recode loop count = %d, want >=1 after forced-KF encode", enc.oracleTraceRecodeLoopCountForTest())
	}
	if enc.thisKeyFrameForced {
		t.Fatalf("thisKeyFrameForced still set after forced-KF commit, want cleared")
	}
}

func TestResetRestoresRateControlQuantizerAverages(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 4096)
	for i := range 4 {
		if _, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, i), uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
	}
	if e.rc.normalInterFrames == 0 {
		t.Fatalf("normalInterFrames = 0, want precondition inter history before reset")
	}
	e.rc.rateCorrectionFactor = 2.0
	e.rc.keyFrameCorrectionFactor = 3.0
	e.rc.goldenCorrectionFactor = 4.0
	e.rc.currentQuantizer = 40
	e.rc.lastQuantizer = 39
	e.rc.lastInterQuantizer = 38
	e.rc.frameTargetBits = 123

	e.Reset()

	if e.rc.avgFrameQuantizer != e.rc.maxQuantizer || e.rc.normalInterFrames != 0 || e.rc.normalInterQuantizerTotal != 0 || e.rc.normalInterAvgQuantizer != e.rc.maxQuantizer {
		t.Fatalf("quantizer averages after reset = avg:%d frames:%d total:%d normal:%d, want max/0/0/max", e.rc.avgFrameQuantizer, e.rc.normalInterFrames, e.rc.normalInterQuantizerTotal, e.rc.normalInterAvgQuantizer)
	}
	if e.rc.rollingActualBits != e.rc.bitsPerFrame || e.rc.rollingTargetBits != e.rc.bitsPerFrame ||
		e.rc.longRollingActualBits != e.rc.bitsPerFrame || e.rc.longRollingTargetBits != e.rc.bitsPerFrame {
		t.Fatalf("rolling bits after reset = short:%d/%d long:%d/%d, want libvpx per-frame bandwidth %d",
			e.rc.rollingActualBits, e.rc.rollingTargetBits, e.rc.longRollingActualBits, e.rc.longRollingTargetBits, e.rc.bitsPerFrame)
	}
	if e.rc.rateCorrectionFactor != 1.0 || e.rc.keyFrameCorrectionFactor != 1.0 || e.rc.goldenCorrectionFactor != 1.0 {
		t.Fatalf("correction factors after reset = %g/%g/%g, want 1/1/1", e.rc.rateCorrectionFactor, e.rc.keyFrameCorrectionFactor, e.rc.goldenCorrectionFactor)
	}
	if e.rc.currentQuantizer != e.rc.minQuantizer || e.rc.lastQuantizer != e.rc.minQuantizer || e.rc.lastInterQuantizer != e.rc.minQuantizer {
		t.Fatalf("quantizers after reset = current:%d last:%d lastInter:%d, want min %d", e.rc.currentQuantizer, e.rc.lastQuantizer, e.rc.lastInterQuantizer, e.rc.minQuantizer)
	}
	if e.rc.frameTargetBits != e.rc.bitsPerFrame {
		t.Fatalf("frame target after reset = %d, want bitsPerFrame %d", e.rc.frameTargetBits, e.rc.bitsPerFrame)
	}
}
