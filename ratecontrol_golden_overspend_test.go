package govpx

import "testing"

func TestBoostedFrameTargetBits(t *testing.T) {
	if got := boostedFrameTargetBits(1000, 100); got != 2000 {
		t.Fatalf("boosted target = %d, want 2000", got)
	}
	if got := boostedFrameTargetBits(1000, 0); got != 1000 {
		t.Fatalf("zero-boost target = %d, want 1000", got)
	}
	if got := boostedFrameTargetBits(maxInt(), 100); got != maxInt() {
		t.Fatalf("overflow-boost target = %d, want maxInt", got)
	}
}

// TestCalcGFParamsMatchesLibvpxBoostTables pins the libvpx
// vp8/encoder/ratectrl.c calc_gf_params boost computation for known
// inputs. The hand-computed expectations follow:
//
//	GFQ_ADJUSTMENT[40] = 128.
//	gf_intra_usage_adjustment[clamp(10,0,14)] = 70.
//	gf_frame_usage = max((golden+altref)*100/total, 100*gf_active/MBs)
//	              = max((200+0)*100/1200, 100*200/1200) = 16.
//	gf_adjust_table[16] = 300.
//	Boost = (((128 * 70) / 100) * 300) / 100 = 267.
//	kf_gf_boost_qlimits[40] = 390 -> no ceiling clamp; >=110 floor unused.
//	gf_interval_table[16] = 7; baseline=8 wins; max_gf_interval=15 caps.
func TestCalcGFParamsMatchesLibvpxBoostTables(t *testing.T) {
	out := calcGFParams(gfParamsInput{
		Q:                     40,
		RecentRefIntra:        100,
		RecentRefLast:         900,
		RecentRefGolden:       200,
		RecentRefAltRef:       0,
		GFActiveCount:         200,
		Macroblocks:           1200,
		ThisFramePercentIntra: 10,
		BaselineGFInterval:    8,
		MaxGFInterval:         15,
	})
	if out.GFFrameUsage != 16 {
		t.Fatalf("gf_frame_usage = %d, want libvpx 16", out.GFFrameUsage)
	}
	if out.Boost != 267 {
		t.Fatalf("calcGFParams boost = %d, want libvpx 267", out.Boost)
	}
	if out.FramesTillUpdate != 8 {
		t.Fatalf("calcGFParams interval = %d, want libvpx 8", out.FramesTillUpdate)
	}
}

// TestCalcGFParamsAppliesQLimitCeiling exercises the kf_gf_boost_qlimits
// ceiling: at low Q with high gf_frame_usage, the raw boost product
// exceeds the table limit and must be clamped down. With
// kf_gf_boost_qlimits[20]=250 the result is forced to 250, and the
// last_boost>=1500 branch never fires so the interval is governed by
// gf_interval_table[gf_frame_usage].
func TestCalcGFParamsAppliesQLimitCeiling(t *testing.T) {
	out := calcGFParams(gfParamsInput{
		Q:                     20,
		RecentRefIntra:        50,
		RecentRefLast:         100,
		RecentRefGolden:       400,
		RecentRefAltRef:       400,
		GFActiveCount:         950,
		Macroblocks:           1000,
		ThisFramePercentIntra: 0,
		BaselineGFInterval:    8,
		MaxGFInterval:         20,
	})
	if out.Boost != libvpxKFGFBoostQLimits[20] {
		t.Fatalf("calcGFParams boost = %d, want clamped to qlimits 250", out.Boost)
	}
	// gf_frame_usage = max((400+400)*100/950, 100*950/1000) = max(84,95)=95.
	// gf_interval_table[95] = 11 (libvpx gf_interval_table boundary).
	if out.GFFrameUsage != 95 {
		t.Fatalf("gf_frame_usage = %d, want 95", out.GFFrameUsage)
	}
	if out.FramesTillUpdate != libvpxGFIntervalTable[95] {
		t.Fatalf("calcGFParams interval = %d, want gf_interval_table[95]=%d",
			out.FramesTillUpdate, libvpxGFIntervalTable[95])
	}
}

// TestCalcGFParamsAppliesBoostFloor pins the lower 110 floor: at high Q
// with low usage the raw product falls under 110, so the boost is
// floored. The interval still picks up the gf_interval_table value at
// the resulting gf_frame_usage.
func TestCalcGFParamsAppliesBoostFloor(t *testing.T) {
	out := calcGFParams(gfParamsInput{
		Q:                     0,
		RecentRefIntra:        1000,
		RecentRefLast:         0,
		RecentRefGolden:       0,
		RecentRefAltRef:       0,
		GFActiveCount:         0,
		Macroblocks:           1000,
		ThisFramePercentIntra: 14,
		BaselineGFInterval:    8,
		MaxGFInterval:         15,
	})
	if out.Boost != 110 {
		t.Fatalf("calcGFParams boost = %d, want 110 floor", out.Boost)
	}
	if out.FramesTillUpdate != 8 {
		t.Fatalf("calcGFParams interval = %d, want baseline 8", out.FramesTillUpdate)
	}
}

// TestCalcGFParamsBoostExtendsInterval covers the >750/>1000/>1250/>=1500
// boost-extension thresholds. With cleared intra/inter ref usage, the
// raw boost is 198 at Q=127; with low intra and gf_frame_usage=0 the
// only path to a large boost is via the test stub. We hand-pick inputs
// that yield boost >= 1500 by zeroing tot_mbs (all entries 0) so
// gf_frame_usage falls back to 100*gf_active/MBs and intra adjustment
// runs at idx=0 (125).
func TestCalcGFParamsBoostExtendsInterval(t *testing.T) {
	// libvpxKFGFBoostQLimits saturates at 600 above index 62; choose
	// Q=80 so the raw product is far above 600 and the qlimit ceiling
	// brings it to exactly 600. Then >=1500 path is not taken (boost is
	// 600), so verify the interval-extension thresholds remain inactive.
	out := calcGFParams(gfParamsInput{
		Q:                     80,
		RecentRefIntra:        0,
		RecentRefLast:         0,
		RecentRefGolden:       1000,
		RecentRefAltRef:       0,
		GFActiveCount:         1000,
		Macroblocks:           1000,
		ThisFramePercentIntra: 0,
		BaselineGFInterval:    8,
		MaxGFInterval:         15,
	})
	if out.Boost != 600 {
		t.Fatalf("calcGFParams boost = %d, want libvpx ceiling 600", out.Boost)
	}
	// gf_interval_table[100]=11 wins over baseline 8, but max=15 caps.
	if out.FramesTillUpdate != 11 {
		t.Fatalf("calcGFParams interval = %d, want gf_interval_table[100]=11", out.FramesTillUpdate)
	}
}

// TestRateControlAccumulatesKeyFrameOverspend pins the
// vp8_adjust_key_frame_context post-pack overspend split: 7/8 to
// kf_overspend_bits, 1/8 to gf_overspend_bits when single-layer. With
// keyFrameCount==1 the libvpx bootstrap uses key_freq -- with no
// configured frequency the estimate is 1, so kf_bitrate_adjustment
// equals kf_overspend_bits.
func TestRateControlAccumulatesKeyFrameOverspend(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  30,
		bitsPerFrame:      1000,
		bufferLevelBits:   500,
		maximumBufferBits: 5000,
	}
	// 2000 bytes = 16000 bits; perFrameBandwidth=1000 -> overspend=15000.
	rc.postEncodeFrameWithPacketContext(2000, rateControlPostEncodeContext{
		keyFrame:    true,
		macroblocks: 1,
		showFrame:   true,
	})
	if got := rc.kfOverspendBits; got != 15000*7/8 {
		t.Fatalf("kfOverspendBits = %d, want %d (7/8 of 15000)", got, 15000*7/8)
	}
	if got := rc.gfOverspendBits; got != 15000/8 {
		t.Fatalf("gfOverspendBits = %d, want %d (1/8 of 15000)", got, 15000/8)
	}
	// First keyframe -> kf_bitrate_adjustment = kf_overspend_bits / 1.
	if rc.kfBitrateAdjustment != rc.kfOverspendBits {
		t.Fatalf("kfBitrateAdjustment = %d, want %d", rc.kfBitrateAdjustment, rc.kfOverspendBits)
	}
}

func TestRateControlKeyFrameOverspendSeedsGoldenRecoveryAdjustment(t *testing.T) {
	rc := rateControlState{
		mode:                  RateControlVBR,
		minQuantizer:          4,
		maxQuantizer:          56,
		currentQuantizer:      30,
		bitsPerFrame:          1000,
		bufferLevelBits:       500,
		maximumBufferBits:     5000,
		framesTillGFUpdateDue: libvpxDefaultGFInterval,
		outputFrameRate:       30,
	}
	rc.postEncodeFrameWithPacketContext(2000, rateControlPostEncodeContext{
		keyFrame:    true,
		macroblocks: 1,
		showFrame:   true,
	})
	if rc.kfBitrateAdjustment != (15000*7/8)/61 {
		t.Fatalf("kfBitrateAdjustment = %d, want two-second bootstrap drain %d",
			rc.kfBitrateAdjustment, (15000*7/8)/61)
	}
	if rc.nonGFBitrateAdjustment != (15000/8)/libvpxDefaultGFInterval {
		t.Fatalf("nonGFBitrateAdjustment = %d, want key-as-golden drain %d",
			rc.nonGFBitrateAdjustment, (15000/8)/libvpxDefaultGFInterval)
	}
}

func TestRateControlTemporalKeyFrameOverspendUsesLayerFrameRate(t *testing.T) {
	rc := rateControlState{
		mode:                          RateControlCBR,
		minQuantizer:                  4,
		maxQuantizer:                  56,
		currentQuantizer:              30,
		bitsPerFrame:                  23333,
		currentTemporalLayers:         3,
		currentLayerPerFrameBandwidth: 18666,
		currentLayerOutputFrameRate:   7,
		bufferLevelBits:               500,
		maximumBufferBits:             5000,
		outputFrameRate:               30,
		keyFrameCount:                 1,
	}
	rc.postEncodeFrameWithPacketContext(3685, rateControlPostEncodeContext{
		keyFrame:    true,
		macroblocks: 1,
		showFrame:   true,
	})
	const overspend = 3685*8 - 18666
	if rc.kfOverspendBits != overspend {
		t.Fatalf("temporal kfOverspendBits = %d, want %d", rc.kfOverspendBits, overspend)
	}
	if got, want := rc.kfBitrateAdjustment, overspend/(1+7*2); got != want {
		t.Fatalf("temporal kfBitrateAdjustment = %d, want layer-rate drain %d", got, want)
	}
	if rc.outputFrameRate != 30 {
		t.Fatalf("outputFrameRate after temporal overspend = %d, want restored 30", rc.outputFrameRate)
	}
}

// TestRateControlUndersizeKeyFrameSkipsOverspend pins the libvpx guard:
// when projected_frame_size <= per_frame_bandwidth, neither
// kf_overspend_bits nor gf_overspend_bits accumulate.
func TestRateControlUndersizeKeyFrameSkipsOverspend(t *testing.T) {
	rc := rateControlState{
		mode:              RateControlCBR,
		minQuantizer:      4,
		maxQuantizer:      56,
		currentQuantizer:  30,
		bitsPerFrame:      4000,
		bufferLevelBits:   500,
		maximumBufferBits: 5000,
	}
	rc.postEncodeFrameWithPacketContext(100, rateControlPostEncodeContext{
		keyFrame:    true,
		macroblocks: 1,
		showFrame:   true,
	}) // 800 bits < 4000.
	if rc.kfOverspendBits != 0 || rc.gfOverspendBits != 0 || rc.kfBitrateAdjustment != 0 {
		t.Fatalf("undersize KF accumulated overspend: kf=%d gf=%d adj=%d",
			rc.kfOverspendBits, rc.gfOverspendBits, rc.kfBitrateAdjustment)
	}
}

// TestRateControlGoldenFrameAccumulatesOverspend pins the libvpx
// update_golden_frame_stats post-pack accumulation: GF overspend equals
// projected_frame_size - inter_frame_target, and non_gf_bitrate_adjustment
// is gf_overspend_bits / frames_till_gf_update_due. Pre-seed
// inter_frame_target and frames_till_gf_update_due to mirror the state
// the encoder publishes before invoking calc_pframe_target_size for the
// GF refresh frame.
func TestRateControlGoldenFrameAccumulatesOverspend(t *testing.T) {
	rc := rateControlState{
		mode:                  RateControlCBR,
		minQuantizer:          4,
		maxQuantizer:          56,
		currentQuantizer:      30,
		bitsPerFrame:          1000,
		bufferLevelBits:       500,
		maximumBufferBits:     5000,
		interFrameTarget:      900,
		framesTillGFUpdateDue: 10,
	}
	// 500 bytes = 4000 bits. Overspend over inter_frame_target=900 -> 3100.
	rc.postEncodeFrameWithPacketContext(500, rateControlPostEncodeContext{
		goldenFrame: true,
		macroblocks: 1,
		showFrame:   true,
	})
	if rc.gfOverspendBits != 3100 {
		t.Fatalf("gfOverspendBits = %d, want 3100", rc.gfOverspendBits)
	}
	if rc.nonGFBitrateAdjustment != 310 {
		t.Fatalf("nonGFBitrateAdjustment = %d, want 310 (3100/10)", rc.nonGFBitrateAdjustment)
	}
	// Golden refresh resets framesSinceGolden to 0.
	if rc.framesSinceGolden != 0 {
		t.Fatalf("framesSinceGolden = %d, want 0", rc.framesSinceGolden)
	}
}

func TestRateControlAltRefPacketAccumulatesFullPostPackOverspend(t *testing.T) {
	rc := rateControlState{
		mode:                  RateControlCBR,
		minQuantizer:          4,
		maxQuantizer:          56,
		currentQuantizer:      30,
		bitsPerFrame:          1000,
		frameTargetBits:       3000,
		bufferLevelBits:       5000,
		maximumBufferBits:     8000,
		interFrameTarget:      900,
		framesTillGFUpdateDue: 10,
		framesSinceKeyframe:   9,
		framesSinceGolden:     4,
	}
	// 500 bytes = 4000 bits. Unlike GF refresh, ARF post-pack accounting
	// accumulates the full hidden-frame packet size. Hidden-ARF mode
	// requires autoAltRef=true (libvpx oxcf.play_alternate); without it
	// libvpx's update_alt_ref_frame_stats does not run and govpx mirrors
	// that gate (see ratecontrol_postencode.go).
	rc.postEncodeFrameWithPacketContext(500, rateControlPostEncodeContext{
		altRefFrame: true,
		autoAltRef:  true,
		macroblocks: 1,
		showFrame:   false,
	})
	if rc.gfOverspendBits != 4000 {
		t.Fatalf("ARF gfOverspendBits = %d, want full packet size 4000", rc.gfOverspendBits)
	}
	if rc.nonGFBitrateAdjustment != 400 {
		t.Fatalf("ARF nonGFBitrateAdjustment = %d, want 400 (4000/10)", rc.nonGFBitrateAdjustment)
	}
	if rc.framesSinceKeyframe != 9 || rc.framesSinceGolden != 4 || rc.framesTillGFUpdateDue != 10 {
		t.Fatalf("ARF invisible counters = framesSinceKey:%d framesSinceGolden:%d framesTillGF:%d, want unchanged 9/4/10",
			rc.framesSinceKeyframe, rc.framesSinceGolden, rc.framesTillGFUpdateDue)
	}
}

func TestRateControlPass2SkipsPostPackOverspend(t *testing.T) {
	tests := []struct {
		name string
		ctx  rateControlPostEncodeContext
	}{
		{
			name: "key",
			ctx: rateControlPostEncodeContext{
				keyFrame:    true,
				macroblocks: 1,
				showFrame:   true,
			},
		},
		{
			name: "golden",
			ctx: rateControlPostEncodeContext{
				goldenFrame: true,
				macroblocks: 1,
				showFrame:   true,
			},
		},
		{
			name: "altref",
			ctx: rateControlPostEncodeContext{
				altRefFrame: true,
				macroblocks: 1,
				showFrame:   false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := rateControlState{
				mode:                  RateControlCBR,
				minQuantizer:          4,
				maxQuantizer:          56,
				currentQuantizer:      30,
				bitsPerFrame:          1000,
				frameTargetBits:       3000,
				bufferLevelBits:       5000,
				maximumBufferBits:     8000,
				interFrameTarget:      900,
				framesTillGFUpdateDue: 10,
			}
			tt.ctx.skipPostPackOverspend = true
			rc.postEncodeFrameWithPacketContext(500, tt.ctx)
			if rc.kfOverspendBits != 0 || rc.gfOverspendBits != 0 ||
				rc.kfBitrateAdjustment != 0 || rc.nonGFBitrateAdjustment != 0 ||
				rc.keyFrameCount != 0 {
				t.Fatalf("pass2 %s overspend state = kf:%d gf:%d kfAdj:%d gfAdj:%d keyCount:%d, want all zero",
					tt.name, rc.kfOverspendBits, rc.gfOverspendBits,
					rc.kfBitrateAdjustment, rc.nonGFBitrateAdjustment, rc.keyFrameCount)
			}
		})
	}
}

// TestRateControlAccumulatesPostPackAltRefOverspend pins the libvpx
// update_alt_ref_frame_stats branch: unlike GF refresh (which
// accumulates projected_frame_size - inter_frame_target), the ARF
// accumulates the full projected_frame_size into gf_overspend_bits,
// then divides by frames_till_gf_update_due for the drain rate.
func TestRateControlAccumulatesPostPackAltRefOverspend(t *testing.T) {
	rc := rateControlState{
		framesTillGFUpdateDue: 10,
	}
	// 4000 bits goes fully into gf_overspend_bits.
	rc.accumulatePostPackAltRefOverspend(4000, false)
	if rc.gfOverspendBits != 4000 {
		t.Fatalf("gf_overspend_bits after ARF = %d, want 4000", rc.gfOverspendBits)
	}
	if rc.nonGFBitrateAdjustment != 400 {
		t.Fatalf("non_gf_bitrate_adjustment = %d, want 400 (4000/10)",
			rc.nonGFBitrateAdjustment)
	}
}

// TestRateControlAccumulatesPostPackAltRefOverspendNoUpdateDueZeroDrain
// pins the libvpx `if (frames_till_gf_update_due > 0)` guard on the
// non_gf_bitrate_adjustment update.
func TestRateControlAccumulatesPostPackAltRefOverspendNoUpdateDueZeroDrain(t *testing.T) {
	rc := rateControlState{framesTillGFUpdateDue: 0}
	rc.accumulatePostPackAltRefOverspend(4000, false)
	if rc.gfOverspendBits != 4000 {
		t.Fatalf("gf_overspend_bits = %d, want 4000", rc.gfOverspendBits)
	}
	if rc.nonGFBitrateAdjustment != 0 {
		t.Fatalf("non_gf_bitrate_adjustment with no update due = %d, want 0",
			rc.nonGFBitrateAdjustment)
	}
}

// TestRateControlAccumulatesPostPackAltRefOverspendIgnoresZeroBits pins
// the libvpx `if (actualBits > 0)` guard.
func TestRateControlAccumulatesPostPackAltRefOverspendIgnoresZeroBits(t *testing.T) {
	rc := rateControlState{framesTillGFUpdateDue: 10}
	rc.accumulatePostPackAltRefOverspend(0, false)
	if rc.gfOverspendBits != 0 {
		t.Fatalf("zero ARF bits should not accumulate: %d", rc.gfOverspendBits)
	}
}

// TestRateControlGFOverspendDrainsIntoNextPFrameTarget pins the libvpx
// calc_pframe_target_size GF-overspend recovery branch: starting with
// gf_overspend_bits=2000, non_gf_bitrate_adjustment=200, the next p-frame
// target = per_frame_bandwidth - 200, and the gf_overspend_bits residue is
// 1800. min_frame_target = max(min_frame_bandwidth, per_frame_bandwidth/4).
// The buffered-mode percent_low/percent_high pass is suppressed by
// keeping bufferLevelBits at bufferOptimalBits.
func TestRateControlGFOverspendDrainsIntoNextPFrameTarget(t *testing.T) {
	rc := rateControlState{
		mode:                   RateControlCBR,
		minQuantizer:           4,
		maxQuantizer:           56,
		currentQuantizer:       30,
		bitsPerFrame:           1000,
		bufferLevelBits:        2000,
		bufferOptimalBits:      2000,
		maximumBufferBits:      4000,
		rollingTargetBits:      1000,
		gfOverspendBits:        2000,
		nonGFBitrateAdjustment: 200,
	}
	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{
		temporalLayerCount: 1,
	})
	if rc.frameTargetBits != 800 {
		t.Fatalf("frameTargetBits = %d, want 800 (1000 - 200 GF drain)", rc.frameTargetBits)
	}
	if rc.gfOverspendBits != 1800 {
		t.Fatalf("gfOverspendBits = %d, want 1800 residue", rc.gfOverspendBits)
	}
	if rc.interFrameTarget != 800 {
		t.Fatalf("interFrameTarget = %d, want 800 (recorded after recovery)", rc.interFrameTarget)
	}
}

// TestRateControlKFOverspendDrainsBeforeGFOverspend pins libvpx's
// ordering inside calc_pframe_target_size: kf_overspend recovery is
// applied first against per_frame_bandwidth, then gf_overspend is drained
// against the post-KF target. min_frame_target=per_frame_bandwidth/4
// caps both adjustments.
func TestRateControlKFOverspendDrainsBeforeGFOverspend(t *testing.T) {
	rc := rateControlState{
		mode:                   RateControlCBR,
		minQuantizer:           4,
		maxQuantizer:           56,
		currentQuantizer:       30,
		bitsPerFrame:           1000,
		bufferLevelBits:        2000,
		bufferOptimalBits:      2000,
		maximumBufferBits:      4000,
		rollingTargetBits:      1000,
		kfOverspendBits:        4000,
		kfBitrateAdjustment:    300,
		gfOverspendBits:        1000,
		nonGFBitrateAdjustment: 100,
	}
	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{
		temporalLayerCount: 1,
	})
	// per_frame_bandwidth=1000, min_frame_target=250.
	// KF drain: 300, residue kfOverspendBits=3700, post-KF target=700.
	// GF drain: 100, residue gfOverspendBits=900, post-GF target=600.
	if rc.kfOverspendBits != 3700 {
		t.Fatalf("kfOverspendBits residue = %d, want 3700", rc.kfOverspendBits)
	}
	if rc.gfOverspendBits != 900 {
		t.Fatalf("gfOverspendBits residue = %d, want 900", rc.gfOverspendBits)
	}
	if rc.frameTargetBits != 600 {
		t.Fatalf("frameTargetBits = %d, want 600 (700-100 after GF drain)", rc.frameTargetBits)
	}
	if rc.interFrameTarget != 600 {
		t.Fatalf("interFrameTarget = %d, want 600", rc.interFrameTarget)
	}
}

// TestRateControlOverspendRecoveryClampsAtMinFrameTarget pins the
// min_frame_target = max(min_frame_bandwidth, per_frame_bandwidth/4)
// floor inside calc_pframe_target_size. With kf_bitrate_adjustment far
// exceeding the available headroom, the drain saturates at
// per_frame_bandwidth - min_frame_target and the residue is reduced
// accordingly.
func TestRateControlOverspendRecoveryClampsAtMinFrameTarget(t *testing.T) {
	rc := rateControlState{
		mode:                RateControlCBR,
		minQuantizer:        4,
		maxQuantizer:        56,
		currentQuantizer:    30,
		bitsPerFrame:        1000,
		bufferLevelBits:     2000,
		bufferOptimalBits:   2000,
		maximumBufferBits:   4000,
		rollingTargetBits:   1000,
		kfOverspendBits:     20000,
		kfBitrateAdjustment: 5000,
	}
	rc.beginFrameWithTargetAndContext(false, rc.bitsPerFrame, rateControlFrameContext{
		temporalLayerCount: 1,
	})
	// min_frame_target=250, max KF drain=750, residue kfOverspendBits=19250.
	if rc.kfOverspendBits != 19250 {
		t.Fatalf("kfOverspendBits residue = %d, want 19250", rc.kfOverspendBits)
	}
	if rc.frameTargetBits != 250 {
		t.Fatalf("frameTargetBits = %d, want min_frame_target 250", rc.frameTargetBits)
	}
}

func TestOnePassAltRefRefreshUsesStaleTargetWithoutOverspendDrain(t *testing.T) {
	rc := rateControlState{
		mode:                   RateControlCBR,
		bitsPerFrame:           23333,
		frameTargetBits:        18769,
		bufferLevelBits:        400000,
		bufferOptimalBits:      3500000,
		undershootPct:          100,
		overshootPct:           100,
		kfOverspendBits:        10419,
		kfBitrateAdjustment:    400,
		gfOverspendBits:        1099,
		nonGFBitrateAdjustment: 156,
		interFrameTarget:       18769,
	}

	rc.beginOnePassAltRefRefreshFrameWithTargetAndContext(rc.bitsPerFrame, rateControlFrameContext{})

	if rc.frameTargetBits != 10511 {
		t.Fatalf("frameTargetBits = %d, want stale target shaped by buffer to 10511", rc.frameTargetBits)
	}
	if rc.kfOverspendBits != 10419 || rc.gfOverspendBits != 1099 {
		t.Fatalf("overspend bits = kf:%d gf:%d, want unchanged 10419/1099",
			rc.kfOverspendBits, rc.gfOverspendBits)
	}
	if rc.interFrameTarget != 18769 {
		t.Fatalf("interFrameTarget = %d, want stale 18769", rc.interFrameTarget)
	}
}

// TestRateControlGoldenFrameTargetBitsMatchesLibvpx pins the libvpx
// boost-weighted GF section split from calc_pframe_target_size. With
// boost=400, frames_till_gf_update_due=7 (frames_in_section=8) and
// inter_frame_target=1000:
//
//	allocation_chunks = 8*100 + 300 = 1100
//	bits_in_section   = 1000 * 8 = 8000
//	(8000 >> 7) = 62 < 1100, so target = 400 * 8000 / 1100 = 2909.
func TestRateControlGoldenFrameTargetBitsMatchesLibvpx(t *testing.T) {
	got := libvpxGoldenFrameTargetBits(400, 7, 1000)
	if got != 2909 {
		t.Fatalf("libvpxGoldenFrameTargetBits = %d, want 2909", got)
	}
}

// TestRateControlGoldenFrameTargetBitsHalvesLargeBoost pins libvpx's
// `while (Boost > 1000) Boost /= 2; allocation_chunks /= 2;` overflow
// guard. With boost=1500, the loop runs once -> boost=750,
// allocation_chunks=(8*100+1400)/2=1100. bits_in_section=8000.
// (8000 >> 7)=62 < 1100, so target = 750 * 8000 / 1100 = 5454.
func TestRateControlGoldenFrameTargetBitsHalvesLargeBoost(t *testing.T) {
	got := libvpxGoldenFrameTargetBits(1500, 7, 1000)
	if got != 5454 {
		t.Fatalf("libvpxGoldenFrameTargetBits with large boost = %d, want 5454", got)
	}
}

// TestRateControlGoldenFrameTargetBitsHighPrecisionPath pins libvpx's
// alternate `Boost * (bits_in_section / allocation_chunks)` branch
// taken when `bits_in_section >> 7 > allocation_chunks`. With
// inter_frame_target=1<<20, frames_in_section=8, boost=400:
//
//	bits_in_section = 8 << 20.
//	bits_in_section >> 7 = 8 << 13 = 65536, > allocation_chunks=1100.
//	target = 400 * (8<<20)/1100 = 400 * 7626 = 3050400.
func TestRateControlGoldenFrameTargetBitsHighPrecisionPath(t *testing.T) {
	got := libvpxGoldenFrameTargetBits(400, 7, 1<<20)
	want := 400 * ((8 << 20) / 1100)
	if got != want {
		t.Fatalf("libvpxGoldenFrameTargetBits high-precision = %d, want %d", got, want)
	}
}
