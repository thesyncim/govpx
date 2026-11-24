package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestSelectQuantizerARFRefreshUsesARFTable(t *testing.T) {
	rc := rateControlState{
		mode:                     RateControlVBR,
		minQuantizer:             4,
		maxQuantizer:             106,
		bitsPerFrame:             1_000_000,
		frameTargetBits:          1_000_000,
		normalInterFrames:        151,
		normalInterAvgQuantizer:  106,
		avgFrameQuantizer:        106,
		framesSinceKeyframe:      30,
		rateCorrectionFactor:     1.0,
		keyFrameCorrectionFactor: 1.0,
		goldenCorrectionFactor:   1.0,
	}

	// vp8enc.LibvpxGoldenFrameHighMotionMinQ[106] = 49 (row 6 col 10 in the
	// 16-per-row Go layout, matching libvpx onyx_if.c gf_high_motion_minq).
	const wantARFFloor = 49

	// ARF refresh (altRefFrame=true, goldenFrame=false) routes through the
	// GF table just like a real GF refresh.
	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(false, false, true)
	if activeBest != wantARFFloor || activeWorst != 106 {
		t.Fatalf("ARF active bounds = %d/%d, want gf_high_motion_minq[106]=%d/106", activeBest, activeWorst, wantARFFloor)
	}

	// And the regulated quantizer must clamp to that floor.
	rc.selectQuantizerForFrameKindWithAltRef(false, false, true, 60, 0)
	if rc.currentQuantizer != wantARFFloor {
		t.Fatalf("ARF selected quantizer = %d, want active-best floor q%d", rc.currentQuantizer, wantARFFloor)
	}

	// A regular GF refresh (goldenFrame=true, altRefFrame=false) must yield
	// the same floor: libvpx merges the two via
	// `(refresh_golden_frame || refresh_alt_ref_frame)`.
	gfBest, gfWorst := rc.libvpxActiveQuantizerBoundsForFrame(false, true, false)
	if gfBest != activeBest || gfWorst != activeWorst {
		t.Fatalf("GF active bounds = %d/%d, want match ARF %d/%d", gfBest, gfWorst, activeBest, activeWorst)
	}

	// A plain inter frame (no GF, no ARF) must NOT take the GF branch and
	// instead use inter_minq[Q].
	interBest, _ := rc.libvpxActiveQuantizerBoundsForFrame(false, false, false)
	if interBest == activeBest {
		t.Fatalf("inter active best = %d unexpectedly equal to ARF/GF floor %d", interBest, activeBest)
	}
	if interBest != vp8enc.LibvpxInterMinQ[106] {
		t.Fatalf("inter active best = %d, want inter_minq[106]=%d", interBest, vp8enc.LibvpxInterMinQ[106])
	}

	// The zbin_oq_high cap must mirror libvpx's
	// `(refresh_alt_ref_frame || (refresh_golden_frame && !source_alt_ref_active))`
	// branch: ARF and GF both cap at 16, plain inter frames at ZBIN_OQ_MAX.
	if cap := vp8enc.LibvpxZbinOverQuantHighAltRef(false, false, true); cap != 16 {
		t.Fatalf("ARF zbin_oq_high = %d, want 16", cap)
	}
	if cap := vp8enc.LibvpxZbinOverQuantHighAltRef(false, true, false); cap != 16 {
		t.Fatalf("GF zbin_oq_high = %d, want 16", cap)
	}
	if cap := vp8enc.LibvpxZbinOverQuantHighAltRef(false, false, false); cap != libvpxZbinOverQuantMax {
		t.Fatalf("inter zbin_oq_high = %d, want %d", cap, libvpxZbinOverQuantMax)
	}
	if cap := vp8enc.LibvpxZbinOverQuantHighAltRef(true, false, true); cap != 0 {
		t.Fatalf("key zbin_oq_high = %d, want 0", cap)
	}
}

// TestSelectQuantizerGFLowMotionVsHighMotion pins the libvpx
// gf_low_motion_minq vs gf_high_motion_minq tables (and the matching
// gf_mid_motion_minq) at representative QINDEX_RANGE indices. libvpx's
// two-pass `vp8_regulate_q` flips between the three tables based on
// `cpi->gfu_boost` thresholds (>1000 -> low, <400 -> high, otherwise -> mid).
// govpx's one-pass path always selects the high-motion floor (matching
// libvpx pass != 2); the low/mid tables are pinned here so a future
// two-pass rate-control port can drop in the threshold gate without
// re-reading the libvpx C source for the table values.
func TestSelectQuantizerGFLowMotionVsHighMotion(t *testing.T) {
	type tableCase struct {
		name string
		got  [128]int
		want map[int]int
	}
	cases := []tableCase{
		{
			name: "gf_low_motion_minq",
			got:  vp8enc.LibvpxGoldenFrameLowMotionMinQ,
			want: map[int]int{
				0:   0,
				4:   1,
				18:  3,
				36:  8,
				56:  15,
				80:  27,
				100: 37,
				113: 44,
				127: 58,
			},
		},
		{
			name: "gf_mid_motion_minq",
			got:  vp8enc.LibvpxGoldenFrameMidMotionMinQ,
			want: map[int]int{
				0:   0,
				4:   1,
				18:  5,
				36:  10,
				56:  18,
				80:  30,
				100: 40,
				113: 50,
				127: 64,
			},
		},
		{
			name: "gf_high_motion_minq",
			got:  vp8enc.LibvpxGoldenFrameHighMotionMinQ,
			want: map[int]int{
				0:   0,
				4:   1,
				18:  5,
				36:  11,
				56:  21,
				80:  33,
				100: 43,
				113: 56,
				127: 80,
			},
		},
		{
			name: "kf_low_motion_minq",
			got:  vp8enc.LibvpxKeyFrameLowMotionMinQ,
			want: map[int]int{
				0:   0,
				36:  0,
				54:  1,
				80:  6,
				100: 13,
				127: 23,
			},
		},
		{
			name: "kf_high_motion_minq",
			got:  vp8enc.LibvpxKeyFrameHighMotionMinQ,
			want: map[int]int{
				0:   0,
				36:  1,
				54:  4,
				80:  11,
				100: 18,
				127: 30,
			},
		},
		{
			name: "inter_minq",
			got:  vp8enc.LibvpxInterMinQ,
			want: map[int]int{
				0:   0,
				4:   2,
				36:  23,
				80:  57,
				106: 80,
				127: 100,
			},
		},
	}
	for _, tc := range cases {
		for q, want := range tc.want {
			if got := tc.got[q]; got != want {
				t.Fatalf("%s[%d] = %d, want %d", tc.name, q, got, want)
			}
		}
	}

	// The libvpx tables follow a strict ordering at every Q: the
	// low-motion floor must never exceed the mid-motion floor and the
	// mid-motion floor must never exceed the high-motion floor (a
	// stronger boost -> a tighter active-best floor).
	for q := range 128 {
		lo := vp8enc.LibvpxGoldenFrameLowMotionMinQ[q]
		mid := vp8enc.LibvpxGoldenFrameMidMotionMinQ[q]
		hi := vp8enc.LibvpxGoldenFrameHighMotionMinQ[q]
		if lo > mid {
			t.Fatalf("gf_low[%d]=%d exceeds gf_mid[%d]=%d", q, lo, q, mid)
		}
		if mid > hi {
			t.Fatalf("gf_mid[%d]=%d exceeds gf_high[%d]=%d", q, mid, q, hi)
		}
	}
	for q := range 128 {
		if vp8enc.LibvpxKeyFrameLowMotionMinQ[q] > vp8enc.LibvpxKeyFrameHighMotionMinQ[q] {
			t.Fatalf("kf_low[%d]=%d exceeds kf_high[%d]=%d", q, vp8enc.LibvpxKeyFrameLowMotionMinQ[q], q, vp8enc.LibvpxKeyFrameHighMotionMinQ[q])
		}
	}
}

// TestSelectQuantizerARFRecodeInteraction drives the libvpx active-worst
// recode relaxation through an ARF refresh. The recode loop must seed
// q_low/q_high from libvpxActiveQuantizerBoundsForFrame with
// altRefFrame=true (sharing the GF active-best floor), the
// `libvpxZbinOverQuantHigh` cap must follow the ARF/GF branch (16, not
// ZBIN_OQ_MAX), and once the relax-active-worst-on-overshoot path raises
// q_high, the chosen recode Q must remain bounded by the new q_high
// rather than the ARF-table floor.
func TestSelectQuantizerARFRecodeInteraction(t *testing.T) {
	rc := rateControlState{
		mode:                    RateControlCBR,
		minQuantizer:            4,
		maxQuantizer:            127,
		currentQuantizer:        80,
		bitsPerFrame:            1_000_000,
		frameTargetBits:         1_000_000,
		bufferOptimalBits:       60_000,
		bufferLevelBits:         60_000,
		maximumBufferBits:       72_000,
		normalInterFrames:       151,
		normalInterAvgQuantizer: 106,
		avgFrameQuantizer:       106,
		framesSinceKeyframe:     30,
		rateCorrectionFactor:    1.0,
		goldenCorrectionFactor:  1.0,
	}

	recode := rc.newFrameSizeRecodeStateWithAltRef(false, false, true)

	// CBR full-buffer active-worst clamp pulls active_worst down to
	// normalInterAvgQuantizer (106); GF/ARF active-best floor is then
	// vp8enc.LibvpxGoldenFrameHighMotionMinQ[106] = 49.
	if recode.qLow != 49 || recode.qHigh != 106 {
		t.Fatalf("ARF recode seed = %+v, want q_low=49 q_high=106", recode)
	}
	// ARF refresh shares the GF zbin_oq_high cap of 16.
	if recode.zbinOQHigh != 16 {
		t.Fatalf("ARF recode zbin_oq_high = %d, want 16", recode.zbinOQHigh)
	}

	// Drive a heavy overshoot with q at the seeded q_high. The
	// libvpx-style relax-active-worst path raises active_worst_quality
	// (regulateHigh here) above 106, but does not reopen the local q_high
	// binary-search bound for the in-flight frame.
	rc.currentQuantizer = recode.qHigh
	rc.frameTargetBits = 1_000_000
	rc.bitsPerFrame = 1_000_000
	got, ok := rc.frameSizeRecodeQuantizerWithContext(2_000_000/8, false, false, 60, &recode)
	if !ok {
		t.Fatalf("ARF recode = q:%d ok:false, want recode triggered by overshoot", got)
	}
	if !recode.activeWorstQChanged {
		t.Fatalf("ARF recode active-worst flag = false, want libvpx 4%%/Qstep relaxation to fire")
	}
	if recode.qHigh != 106 {
		t.Fatalf("ARF recode q_high = %d, want local bound pinned at ARF seed 106", recode.qHigh)
	}
	if recode.regulateHigh <= 106 {
		t.Fatalf("ARF recode active-worst = %d, want raised above ARF active-worst seed 106", recode.regulateHigh)
	}
	if recode.regulateHigh > rc.maxQuantizer {
		t.Fatalf("ARF recode active-worst = %d, want clamped at maxQuantizer %d", recode.regulateHigh, rc.maxQuantizer)
	}
	if got > recode.qHigh {
		t.Fatalf("ARF recoded q = %d, want bounded by local q_high %d", got, recode.qHigh)
	}
	if got < recode.qLow {
		t.Fatalf("ARF recoded q = %d, want at or above ARF-table floor %d", got, recode.qLow)
	}

	// Wire the rc-side activeWorstQChanged flag so the post-encode hook
	// suppresses the rate-correction-factor update (libvpx parity).
	if !rc.activeWorstQChanged {
		t.Fatalf("rc.activeWorstQChanged = false, want set after libvpx active-worst relaxation")
	}
}

// TestPass2CBRBufferAdjustmentRaisesTargetUnderfilledBuffer pins the
// libvpx vp8/encoder/firstpass.c Pass2Encode CBR buffer-state adjustment
// (USAGE_STREAM_FROM_SERVER): when the encoder buffer is below optimal,
// the per-frame target is reduced so the buffer can refill. govpx's
// `applyPass2CBRBufferAdjustment` re-asserts the libvpx
// `bufferAdjustedFrameTargetBits` shaping after the second-pass
// error-fraction allocation overrides the one-pass target.
func TestPass2CBRBufferAdjustmentRaisesTargetUnderfilledBuffer(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		undershootPct:     defaultRateControlUndershootPct,
		overshootPct:      defaultRateControlOvershootPct,
		bitsPerFrame:      1000,
		bufferOptimalBits: 2000,
		bufferLevelBits:   900,
	}

	// Pre-condition: with bufferOptimalBits=2000, bufferLevelBits=900,
	// undershoot=100 (libvpx default), the libvpx adjustment computes
	//   percentLow = (2000-900)/21 = 52, uncapped at default,
	//   target  -= target * 52 / 200 = 1000 - 260.
	// So a 1000-bit two-pass target shrinks to 740.
	got := rc.applyPass2CBRBufferAdjustment(1000, false /*keyFrame*/)
	if got != 740 {
		t.Fatalf("applyPass2CBRBufferAdjustment = %d, want libvpx low-buffer 740", got)
	}

	// Key frames are deferred to libvpx's separate kf_bits / buffer cap
	// path, so the helper must leave key-frame targets untouched.
	if kf := rc.applyPass2CBRBufferAdjustment(1000, true /*keyFrame*/); kf != 1000 {
		t.Fatalf("applyPass2CBRBufferAdjustment(keyFrame) = %d, want unchanged 1000", kf)
	}

	// Non-CBR modes leave the second-pass error-fraction target alone.
	rc.mode = RateControlVBR
	if vbr := rc.applyPass2CBRBufferAdjustment(1000, false); vbr != 1000 {
		t.Fatalf("applyPass2CBRBufferAdjustment(VBR) = %d, want unchanged 1000", vbr)
	}
}

// TestPass2CBRBufferAdjustmentLowersTargetOverfilledBuffer pins the
// opposite Pass2Encode CBR branch: when the buffer is above optimal,
// the per-frame target is raised (relative to the post-error-fraction
// two-pass target) so the buffer can drain back toward optimal.
func TestPass2CBRBufferAdjustmentLowersTargetOverfilledBuffer(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		undershootPct:     defaultRateControlUndershootPct,
		overshootPct:      defaultRateControlOvershootPct,
		bitsPerFrame:      1000,
		bufferOptimalBits: 2000,
		bufferLevelBits:   3200,
	}

	// Pre-condition: with bufferOptimalBits=2000, bufferLevelBits=3200,
	// overshoot=100, the libvpx adjustment computes
	//   percentHigh = (3200-2000)/21 = 57,
	//   target += target * 57 / 200.
	// So a 1000-bit two-pass target grows to 1285 (matches
	// TestRateControlBeginFrameAdjustsTargetForHighBuffer).
	got := rc.applyPass2CBRBufferAdjustment(1000, false)
	if got != 1285 {
		t.Fatalf("applyPass2CBRBufferAdjustment = %d, want libvpx high-buffer 1285", got)
	}
}

// TestSelectQuantizerCQFloorApplied pins the libvpx
// vp8/encoder/firstpass.c estimate_max_q CQ floor
// (`USAGE_CONSTRAINED_QUALITY -> Q = max(Q, cq_target_quality)`). With
// a generous frame target the second-pass Q regulation would naturally
// pick a quantizer below cqLevel; the post-regulation CQ floor must
// raise the final Q back to the configured target.
func TestSelectQuantizerCQFloorApplied(t *testing.T) {
	cqLevel := vp8common.PublicQuantizerToQIndex(40)
	rc := rateControlState{
		mode:                     RateControlCQ,
		minQuantizer:             4,
		maxQuantizer:             127,
		cqLevel:                  cqLevel,
		currentQuantizer:         cqLevel,
		bitsPerFrame:             1 << 20,
		frameTargetBits:          1 << 20,
		rateCorrectionFactor:     1.0,
		keyFrameCorrectionFactor: 1.0,
		goldenCorrectionFactor:   1.0,
	}

	// Drive Q regulation with a huge per-frame budget so the regulator
	// would pick the active-best floor (well below cqLevel). Pre-seed
	// the current quantizer to a sub-floor value to verify the post-
	// regulation clamp lifts it to cqLevel.
	rc.currentQuantizer = 30
	rc.selectQuantizerForFrameKindWithScreenContent(false, false, 60, 0)
	if rc.currentQuantizer < cqLevel {
		t.Fatalf("CQ post-regulation quantizer = %d, want CQ floor %d (libvpx max(Q, cq_target_quality))",
			rc.currentQuantizer, cqLevel)
	}

	// applyCQFloor must also re-clamp callers that drop currentQuantizer
	// below cqLevel after regulation (e.g. recode-style adjustments).
	rc.currentQuantizer = 30
	rc.applyCQFloor()
	if rc.currentQuantizer != cqLevel {
		t.Fatalf("applyCQFloor() quantizer = %d, want CQ floor %d", rc.currentQuantizer, cqLevel)
	}

	// Non-CQ modes must leave the regulated quantizer alone.
	rc.mode = RateControlVBR
	rc.currentQuantizer = 30
	rc.applyCQFloor()
	if rc.currentQuantizer != 30 {
		t.Fatalf("applyCQFloor(VBR) quantizer = %d, want unchanged 30", rc.currentQuantizer)
	}
}
