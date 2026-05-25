package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestRateControlActiveQuantizerBoundsUseLibvpxWarmupTables(t *testing.T) {
	rc := rateControlState{
		mode:                     RateControlCBR,
		minQuantizer:             4,
		maxQuantizer:             106,
		currentQuantizer:         4,
		bitsPerFrame:             1_000_000,
		frameTargetBits:          1_000_000,
		bufferOptimalBits:        60_000,
		bufferLevelBits:          0,
		maximumBufferBits:        72_000,
		normalInterFrames:        151,
		normalInterAvgQuantizer:  106,
		rateCorrectionFactor:     1.0,
		keyFrameCorrectionFactor: 1.0,
		goldenCorrectionFactor:   1.0,
	}

	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
	if activeBest != 80 || activeWorst != 106 {
		t.Fatalf("active bounds = %d/%d, want libvpx inter_minq[106]/worst 80/106", activeBest, activeWorst)
	}

	rc.selectQuantizerForFrameKind(false, false, 60)
	if rc.currentQuantizer != 80 {
		t.Fatalf("selected warmed-up quantizer = %d, want active-best floor q80", rc.currentQuantizer)
	}
}

func TestRateControlActiveQuantizerBoundsUseLibvpxCBRFullBufferClamp(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlCBR,
		minQuantizer:            4,
		maxQuantizer:            106,
		bufferOptimalBits:       1000,
		maximumBufferBits:       2000,
		normalInterFrames:       151,
		normalInterAvgQuantizer: 80,
	}

	rc.bufferLevelBits = 1000
	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
	if activeBest != 57 || activeWorst != 80 {
		t.Fatalf("optimal-buffer active bounds = %d/%d, want inter_minq[80]/ni_av_qi 57/80", activeBest, activeWorst)
	}

	rc.bufferLevelBits = 1500
	activeBest, activeWorst = rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
	if activeBest != 27 || activeWorst != 70 {
		t.Fatalf("mid-full-buffer active bounds = %d/%d, want libvpx scaled CBR bounds 27/70", activeBest, activeWorst)
	}

	rc.bufferLevelBits = 2000
	activeBest, activeWorst = rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
	if activeBest != 4 || activeWorst != 60 {
		t.Fatalf("full-buffer active bounds = %d/%d, want best-quality floor and active-worst q60", activeBest, activeWorst)
	}
}

func TestRateControlCQActiveQuantizerBoundsRespectCQLevel(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlCQ,
		minQuantizer:            4,
		maxQuantizer:            51,
		cqLevel:                 43,
		normalInterFrames:       151,
		normalInterAvgQuantizer: 51,
	}

	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
	if activeBest != 43 || activeWorst != 51 {
		t.Fatalf("CQ active bounds = %d/%d, want cq-level floor 43/51", activeBest, activeWorst)
	}
}

func TestRateControlQActiveQuantizerBoundsDoNotUseCQFloor(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlQ,
		minQuantizer:            4,
		maxQuantizer:            51,
		cqLevel:                 43,
		normalInterFrames:       151,
		normalInterAvgQuantizer: 51,
	}

	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
	wantBest := vp8enc.LibvpxInterMinQ[51]
	if wantBest >= rc.cqLevel {
		t.Fatalf("test fixture invalid: inter_minq[51] = %d, want below CQ level %d", wantBest, rc.cqLevel)
	}
	if activeBest != wantBest || activeWorst != 51 {
		t.Fatalf("Q active bounds = %d/%d, want no CQ floor %d/51", activeBest, activeWorst, wantBest)
	}
}

func TestRateControlPreservesCQActiveBestAcrossRuntimeCBRForcedKey(t *testing.T) {
	timing := timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1}
	var rc rateControlState
	if err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             30,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}, timing); err != nil {
		t.Fatalf("apply CQ config: %v", err)
	}

	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{timing: timing})
	rc.selectQuantizerForFrameKind(false, false, 4)
	cqQ := vp8common.PublicQuantizerToQIndex(30)
	if rc.activeBestQuantizer != cqQ {
		t.Fatalf("CQ active best = %d, want cq target q%d", rc.activeBestQuantizer, cqQ)
	}

	if err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}, timing); err != nil {
		t.Fatalf("apply CBR config: %v", err)
	}

	rc.beginFrameWithTargetAndContext(true, rc.bitsPerFrame, rateControlFrameContext{
		forcedKeyFrame: true,
		timing:         timing,
	})
	rc.selectQuantizerForFrameKind(true, false, 4)
	if rc.activeBestQuantizer != cqQ || rc.currentQuantizer != cqQ {
		t.Fatalf("forced-key active/current Q = %d/%d, want preserved CQ q%d", rc.activeBestQuantizer, rc.currentQuantizer, cqQ)
	}

	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{timing: timing})
	if rc.activeBestQuantizer != rc.minQuantizer {
		t.Fatalf("post-key inter active best = %d, want reset to best q%d", rc.activeBestQuantizer, rc.minQuantizer)
	}
}

func TestRateControlCQKeyFrameResetsStickyActiveBest(t *testing.T) {
	timing := timingState{timebaseNum: 1, timebaseDen: 30, frameDuration: 1}
	var rc rateControlState
	if err := rc.applyConfig(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   700,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             20,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}, timing); err != nil {
		t.Fatalf("apply CQ config: %v", err)
	}

	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{timing: timing})
	rc.selectQuantizerForFrameKind(false, false, 16)
	if rc.activeBestQuantizer <= rc.minQuantizer {
		t.Fatalf("CQ inter active best = %d, want raised above best q%d", rc.activeBestQuantizer, rc.minQuantizer)
	}

	rc.beginFrameWithTargetAndContext(true, rc.bitsPerFrame, rateControlFrameContext{timing: timing})
	rc.selectQuantizerForFrameKind(true, false, 16)
	if rc.activeBestQuantizer != rc.minQuantizer {
		t.Fatalf("CQ key active best = %d, want reset to best q%d", rc.activeBestQuantizer, rc.minQuantizer)
	}
}

// TestActiveBestQuantizerForcedKeyFramePass2Clamp verifies the libvpx
// onyx_if.c:3636-3642 forced-key sub-clamp. For a pass-2 KF emitted because
// the maximum key-frame interval was hit (this_key_frame_forced=true),
// active_best_quality must land in
// [avg_frame_qindex >> 2, avg_frame_qindex * 7 / 8].
func TestActiveBestQuantizerForcedKeyFramePass2Clamp(t *testing.T) {
	// Case 1: kf_high_motion_minq[active_worst] would normally be below
	// avg_frame_qindex >> 2; clamp lifts active_best to the lower bound.
	rc := rateControlState{
		mode:                      RateControlVBR,
		minQuantizer:              0,
		maxQuantizer:              127,
		normalInterFrames:         151,
		normalInterAvgQuantizer:   60,
		pass2ActiveWorstQValid:    true,
		pass2ActiveWorstQOverride: 60,
		avgFrameQuantizer:         80,
		thisKeyFrameForced:        true,
	}
	activeBest, _ := rc.libvpxActiveQuantizerBoundsForFrame(true, false, false)
	// kf_high_motion_minq[60] == 6 from vp8_ratecontrol_tables.go, well below
	// avg_frame_qindex>>2 == 80>>2 == 20. Expect lift to 20.
	if activeBest != 20 {
		t.Fatalf("forced-key pass2 KF active_best = %d, want lift to avg>>2 = 20", activeBest)
	}

	// Case 2: clamp upper bound. Synthesize a high active_worst that
	// would push kf_high_motion_minq lookup above avg*7/8.
	rc2 := rateControlState{
		mode:                      RateControlVBR,
		minQuantizer:              0,
		maxQuantizer:              127,
		normalInterFrames:         151,
		normalInterAvgQuantizer:   100,
		pass2ActiveWorstQValid:    true,
		pass2ActiveWorstQOverride: 127,
		avgFrameQuantizer:         24,
		thisKeyFrameForced:        true,
	}
	activeBest2, _ := rc2.libvpxActiveQuantizerBoundsForFrame(true, false, false)
	// kf_high_motion_minq[127] == 30 from vp8_ratecontrol_tables.go, above
	// avg_frame_qindex*7/8 == 24*7/8 == 21. Expect clamp down to 21.
	if activeBest2 != 21 {
		t.Fatalf("forced-key pass2 KF active_best = %d, want clamp to avg*7/8 = 21", activeBest2)
	}

	// Case 3: forced-key flag NOT set; clamp must not fire. Confirms the
	// clamp is gated correctly.
	rc3 := rc
	rc3.thisKeyFrameForced = false
	activeBest3, _ := rc3.libvpxActiveQuantizerBoundsForFrame(true, false, false)
	if activeBest3 != vp8enc.LibvpxKeyFrameHighMotionMinQ[60] {
		t.Fatalf("non-forced pass2 KF active_best = %d, want raw kf_high_motion_minq[60] = %d", activeBest3, vp8enc.LibvpxKeyFrameHighMotionMinQ[60])
	}

	// Case 4: forced-key flag set but pass-2 surface inactive (one-pass);
	// libvpx 3636-3642 is inside the `cpi->pass == 2` arm so the clamp
	// must NOT fire in one-pass mode.
	rc4 := rc
	rc4.pass2ActiveWorstQValid = false
	activeBest4, _ := rc4.libvpxActiveQuantizerBoundsForFrame(true, false, false)
	// One-pass KF: libvpxActiveWorstQuantizerForFrame returns maxQuantizer
	// (127), so kf_high_motion_minq[127] == 30 (unclamped).
	if activeBest4 != vp8enc.LibvpxKeyFrameHighMotionMinQ[127] {
		t.Fatalf("forced-key one-pass KF active_best = %d, want raw kf_high_motion_minq[127] = %d (no clamp)", activeBest4, vp8enc.LibvpxKeyFrameHighMotionMinQ[127])
	}
}

// TestActiveBestQuantizerPass2CQGoldenFrame15Over16Lowering verifies the
// libvpx onyx_if.c:3677-3679 "Constrained quality use slightly lower active
// best" lowering. For pass-2 CQ GF/ARF frames, active_best is multiplied by
// 15/16 after the gf_*_motion_minq lookup.
func TestActiveBestQuantizerPass2CQGoldenFrame15Over16Lowering(t *testing.T) {
	rc := rateControlState{
		mode:                      RateControlCQ,
		minQuantizer:              0,
		maxQuantizer:              127,
		cqLevel:                   30,
		normalInterFrames:         151,
		normalInterAvgQuantizer:   80,
		pass2ActiveWorstQValid:    true,
		pass2ActiveWorstQOverride: 80,
		avgFrameQuantizer:         60,
		framesSinceKeyframe:       10,
	}
	activeBest, _ := rc.libvpxActiveQuantizerBoundsForFrame(false, true, false)
	// q is min(active_worst=80, avg_frame_qindex=60) = 60. cqFloor:
	// q=60 already above cqLevel=30, so no lift. Then
	// gf_high_motion_minq[60] = 23 from vp8_ratecontrol_tables.go (row
	// 48-63: 17,17,18,18,19,19,20,20,21,21,22,22,23,23,24,24).
	// 15/16 lowering: 23 * 15 / 16 = 21.
	wantActiveBest := vp8enc.LibvpxGoldenFrameHighMotionMinQ[60] * 15 / 16
	if activeBest != wantActiveBest {
		t.Fatalf("pass2 CQ GF active_best = %d, want gf_high_motion_minq[60]*15/16 = %d", activeBest, wantActiveBest)
	}

	// Without the CQ mode the 15/16 must not fire (VBR pass-2 GF).
	rc2 := rc
	rc2.mode = RateControlVBR
	activeBest2, _ := rc2.libvpxActiveQuantizerBoundsForFrame(false, true, false)
	if activeBest2 != vp8enc.LibvpxGoldenFrameHighMotionMinQ[60] {
		t.Fatalf("pass2 VBR GF active_best = %d, want raw gf_high_motion_minq[60] = %d", activeBest2, vp8enc.LibvpxGoldenFrameHighMotionMinQ[60])
	}

	// One-pass CQ GF: libvpx 3677-3679 is inside the pass==2 arm; the
	// 15/16 must not fire for one-pass.
	rc3 := rc
	rc3.pass2ActiveWorstQValid = false
	rc3.pass2ActiveWorstQOverride = 0
	activeBest3, _ := rc3.libvpxActiveQuantizerBoundsForFrame(false, true, false)
	// One-pass inter: active_worst = maxQuantizer (127, since
	// bufferOptimalBits is zero → unbuffered fallthrough). avg=60 <
	// active_worst=127, so q=60. cqFloor lifts q to cqLevel only when
	// q<cqLevel; q=60 > cqLevel=30 so no lift. gf_high_motion_minq[60] =
	// 40, no 15/16 multiplier in one-pass.
	if activeBest3 != vp8enc.LibvpxGoldenFrameHighMotionMinQ[60] {
		t.Fatalf("one-pass CQ GF active_best = %d, want raw gf_high_motion_minq[60] = %d (no 15/16)", activeBest3, vp8enc.LibvpxGoldenFrameHighMotionMinQ[60])
	}
}
