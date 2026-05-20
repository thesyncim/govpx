package govpx

var libvpxGFBoostQAdjustment = [128]int{
	80, 82, 84, 86, 88, 90, 92, 94, 96, 97, 98, 99, 100, 101, 102,
	103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117,
	118, 119, 120, 121, 122, 123, 124, 125, 126, 127, 128, 129, 130, 131, 132,
	133, 134, 135, 136, 137, 138, 139, 140, 141, 142, 143, 144, 145, 146, 147,
	148, 149, 150, 151, 152, 153, 154, 155, 156, 157, 158, 159, 160, 161, 162,
	163, 164, 165, 166, 167, 168, 169, 170, 171, 172, 173, 174, 175, 176, 177,
	178, 179, 180, 181, 182, 183, 184, 184, 185, 185, 186, 186, 187, 187, 188,
	188, 189, 189, 190, 190, 191, 191, 192, 192, 193, 193, 194, 194, 194, 194,
	195, 195, 196, 196, 197, 197, 198, 198,
}

// libvpxKFGFBoostQLimits ports kf_gf_boost_qlimits from
// vp8/encoder/ratectrl.c (one-pass upper limit on GF boost by Q).
var libvpxKFGFBoostQLimits = [128]int{
	150, 155, 160, 165, 170, 175, 180, 185, 190, 195, 200, 205, 210, 215, 220,
	225, 230, 235, 240, 245, 250, 255, 260, 265, 270, 275, 280, 285, 290, 295,
	300, 305, 310, 320, 330, 340, 350, 360, 370, 380, 390, 400, 410, 420, 430,
	440, 450, 460, 470, 480, 490, 500, 510, 520, 530, 540, 550, 560, 570, 580,
	590, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600,
	600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600,
	600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600,
	600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600,
	600, 600, 600, 600, 600, 600, 600, 600,
}

// libvpxGFAdjustTable ports gf_adjust_table from vp8/encoder/ratectrl.c.
// Indexed by gf_frame_usage (0..100) it scales the GF boost by recent
// golden-frame usage.
var libvpxGFAdjustTable = [101]int{
	100, 115, 130, 145, 160, 175, 190, 200, 210, 220, 230, 240, 260, 270, 280,
	290, 300, 310, 320, 330, 340, 350, 360, 370, 380, 390, 400, 400, 400, 400,
	400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400,
	400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400,
	400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400,
	400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400,
	400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400,
}

// libvpxGFIntraUsageAdjustment ports gf_intra_usage_adjustment from
// vp8/encoder/ratectrl.c. Indexed by clamp(this_frame_percent_intra, 0, 14)
// (the libvpx switch caps at 14 when percent_intra < 15).
var libvpxGFIntraUsageAdjustment = [20]int{
	125, 120, 115, 110, 105, 100, 95, 85, 80, 75,
	70, 65, 60, 55, 50, 50, 50, 50, 50, 50,
}

// libvpxGFIntervalTable ports gf_interval_table from vp8/encoder/ratectrl.c.
var libvpxGFIntervalTable = [101]int{
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9,
	9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10,
	10, 10, 10, 10, 10, 10, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11,
}

// gfParamsInput collects the libvpx calc_gf_params inputs that govpx must
// supply explicitly: the inter-frame Q used for the GFQ_ADJUSTMENT lookup,
// the per-MB ref-frame usage counts (intra/last/golden/altref), the count
// of macroblocks still pointing at the active golden, the number of MBs in
// the frame, percent_intra for this frame, and the maximum permitted GF
// interval (libvpx clamps to max_gf_interval).
type gfParamsInput struct {
	Q                     int
	RecentRefIntra        int
	RecentRefLast         int
	RecentRefGolden       int
	RecentRefAltRef       int
	GFActiveCount         int
	Macroblocks           int
	ThisFramePercentIntra int
	BaselineGFInterval    int
	MaxGFInterval         int
	RealtimeNoRecode      bool
}

// gfParamsOutput is the calc_gf_params result govpx consumes: the GF boost
// (last_boost) and the next-GF interval (frames_till_gf_update_due).
type gfParamsOutput struct {
	Boost            int
	FramesTillUpdate int
	GFFrameUsage     int
}

// calcGFParams ports the one-pass branch of vp8/encoder/ratectrl.c
// calc_gf_params: it computes the GF boost from GFQ_ADJUSTMENT scaled by
// gf_intra_usage_adjustment and gf_adjust_table[gf_frame_usage], applies
// the kf_gf_boost_qlimits ceiling and a 110 floor, and computes the
// frames_till_gf_update_due interval from baseline_gf_interval, last_boost
// thresholds (>750/>1000/>1250/>=1500), gf_interval_table, and the
// max_gf_interval cap. RealtimeNoRecode mirrors libvpx's one-pass safeguard:
// when compressor_speed==2 and sf.recode_loop==0, the boost is halved before
// the Q limit and interval logic run.
func calcGFParams(in gfParamsInput) gfParamsOutput {
	q := clampQuantizerValue(in.Q, 0, vp8MaxQIndex)
	totMBs := in.RecentRefIntra + in.RecentRefLast + in.RecentRefGolden + in.RecentRefAltRef
	gfFrameUsage := 0
	if totMBs > 0 {
		gfFrameUsage = (in.RecentRefGolden + in.RecentRefAltRef) * 100 / totMBs
	}
	pctGFActive := 0
	if in.Macroblocks > 0 {
		pctGFActive = (100 * in.GFActiveCount) / in.Macroblocks
	}
	gfFrameUsage = min(max(max(gfFrameUsage, pctGFActive), 0), 100)

	intraIdx := min(max(in.ThisFramePercentIntra, 0), 14)

	boost := libvpxGFBoostQAdjustment[q]
	boost = boost * libvpxGFIntraUsageAdjustment[intraIdx] / 100
	boost = boost * libvpxGFAdjustTable[gfFrameUsage] / 100

	if in.RealtimeNoRecode {
		boost /= 2
	}

	boost = min(max(boost, 110), libvpxKFGFBoostQLimits[q])

	framesTillUpdate := in.BaselineGFInterval
	if boost > 750 {
		framesTillUpdate++
	}
	if boost > 1000 {
		framesTillUpdate++
	}
	if boost > 1250 {
		framesTillUpdate++
	}
	if boost >= 1500 {
		framesTillUpdate++
	}
	framesTillUpdate = max(framesTillUpdate, libvpxGFIntervalTable[gfFrameUsage])
	if in.MaxGFInterval > 0 {
		framesTillUpdate = min(framesTillUpdate, in.MaxGFInterval)
	}
	return gfParamsOutput{
		Boost:            boost,
		FramesTillUpdate: framesTillUpdate,
		GFFrameUsage:     gfFrameUsage,
	}
}

// libvpxGoldenFrameTargetBits ports the libvpx GF target-sizing formula
// from vp8/encoder/ratectrl.c calc_pframe_target_size (the
// non-onepass-CBR auto_gold branch). It splits the upcoming GF-section
// bandwidth across the GF and the following p-frames so that the GF
// receives a `boost`-weighted share. The math is:
//
//	frames_in_section = framesTillGFUpdateDue + 1
//	allocation_chunks = frames_in_section*100 + (boost - 100)
//	bits_in_section   = inter_frame_target * frames_in_section
//	target            = boost * bits_in_section / allocation_chunks
//
// libvpx halves boost and allocation_chunks while boost > 1000 to avoid
// overflow in `boost * bits_in_section`, and switches the divide order
// when `bits_in_section >> 7 > allocation_chunks` to retain precision
// without overflow. Both branches are mirrored here.
func libvpxGoldenFrameTargetBits(boost int, framesTillGFUpdateDue int, interFrameTarget int) int {
	if min(boost, interFrameTarget) <= 0 || framesTillGFUpdateDue < 0 {
		return 0
	}
	framesInSection := framesTillGFUpdateDue + 1
	allocationChunks := framesInSection*100 + (boost - 100)
	if allocationChunks <= 0 {
		return 0
	}
	bitsInSection := interFrameTarget * framesInSection
	if bitsInSection <= 0 {
		return 0
	}
	for boost > 1000 {
		boost /= 2
		allocationChunks /= 2
		if allocationChunks <= 0 {
			return 0
		}
	}
	if (bitsInSection >> 7) > allocationChunks {
		return boost * (bitsInSection / allocationChunks)
	}
	return (boost * bitsInSection) / allocationChunks
}

// updateRecentRefFrameUsage mirrors the libvpx
// vp8/encoder/onyx_if.c update_golden_frame_stats branch:
//
//	if (cpi->frames_since_golden > 1) {
//	    cpi->recent_ref_frame_usage[INTRA_FRAME] +=
//	        cpi->mb.count_mb_ref_frame_usage[INTRA_FRAME];
//	    ...
//	}
//
// Counts from the just-encoded frame are accumulated into the rolling
// `recent_ref_frame_usage` totals (skipping the first frame after a GF
// to suppress noise). On GF refresh, libvpx resets these counters to 1
// each (handled separately, in resetGoldenFrameStats below).
func (rc *rateControlState) updateRecentRefFrameUsage(intra, last, golden, alt int) {
	if rc.framesSinceGolden <= 1 {
		return
	}
	rc.recentRefFrameUsageIntra = saturatingAdd(rc.recentRefFrameUsageIntra, intra)
	rc.recentRefFrameUsageLast = saturatingAdd(rc.recentRefFrameUsageLast, last)
	rc.recentRefFrameUsageGolden = saturatingAdd(rc.recentRefFrameUsageGolden, golden)
	rc.recentRefFrameUsageAltRef = saturatingAdd(rc.recentRefFrameUsageAltRef, alt)
}

// resetRecentRefFrameUsage mirrors libvpx's GF refresh reset:
//
//	cpi->recent_ref_frame_usage[INTRA_FRAME] = 1;
//	cpi->recent_ref_frame_usage[LAST_FRAME]  = 1;
//	cpi->recent_ref_frame_usage[GOLDEN_FRAME]= 1;
//	cpi->recent_ref_frame_usage[ALTREF_FRAME]= 1;
//
// (vp8/encoder/onyx_if.c update_golden_frame_stats refresh branch).
// Also resets gfActiveCount to the full MB count via the active_flags
// memset in libvpx; the caller passes that count.
func (rc *rateControlState) resetRecentRefFrameUsage(macroblocks int) {
	rc.recentRefFrameUsageIntra = 1
	rc.recentRefFrameUsageLast = 1
	rc.recentRefFrameUsageGolden = 1
	rc.recentRefFrameUsageAltRef = 1
	rc.gfActiveCount = macroblocks
}

// vbrMinFrameBandwidthBits ports the libvpx
// vp8/encoder/onyx_if.c min_frame_bandwidth derivation:
//
//	cpi->min_frame_bandwidth = (int)VPXMIN(
//	    (int64_t)cpi->av_per_frame_bandwidth * cpi->oxcf.two_pass_vbrmin_section / 100,
//	    INT_MAX);
//
// pct == 0 disables the minimum (returns 0).
func vbrMinFrameBandwidthBits(perFrameBandwidth int, pct int) int {
	if min(perFrameBandwidth, pct) <= 0 {
		return 0
	}
	v := int64(perFrameBandwidth) * int64(pct) / 100
	if v > int64(libvpxIntMax) {
		return libvpxIntMax
	}
	return int(v)
}

// libvpxAutoGoldOnePassRefreshDecision ports the libvpx one-pass auto_gold
// GF refresh decision from vp8/encoder/ratectrl.c calc_pframe_target_size.
// Excerpt:
//
//	if ((cpi->pass == 0) &&
//	    (cpi->this_frame_percent_intra < 15 || gf_frame_usage >= 5)) {
//	    cpi->common.refresh_golden_frame = 1;
//	}
//
// gf_frame_usage is computed exactly the same way as inside calcGFParams
// (max of (golden+altref)*100/total_recent_ref_usage and
// 100*gf_active_count/MBs). Returns true when libvpx would force a GF
// refresh on this frame.
func libvpxAutoGoldOnePassRefreshDecision(thisFramePercentIntra int, recentRefIntra, recentRefLast, recentRefGolden, recentRefAltRef, gfActiveCount, macroblocks int) bool {
	totMBs := recentRefIntra + recentRefLast + recentRefGolden + recentRefAltRef
	gfFrameUsage := 0
	if totMBs > 0 {
		gfFrameUsage = (recentRefGolden + recentRefAltRef) * 100 / totMBs
	}
	pctGFActive := 0
	if macroblocks > 0 {
		pctGFActive = (100 * gfActiveCount) / macroblocks
	}
	if pctGFActive > gfFrameUsage {
		gfFrameUsage = pctGFActive
	}
	return thisFramePercentIntra < 15 || gfFrameUsage >= 5
}

// pickFrameSize ports vp8/encoder/ratectrl.c vp8_pick_frame_size: the
// unified KF/p-frame target dispatcher. It returns true when the frame
// should be encoded and false when libvpx would set cpi->drop_frame and
// return 0 from vp8_pick_frame_size.
//
// govpx's existing entry point is beginFrameWithTargetAndContext, which
// computes the per-frame target. pickFrameSize wraps it so callers can
// follow libvpx's contract: invoke calc_iframe_target_size for KFs,
// calc_pframe_target_size for inter frames, and consume the drop signal
// before encode. After computing the target, this method also reflects
// libvpx's tail-of-calc_pframe_target_size buffer-underrun drop check
// (drop_frames_allowed && buffer_level < 0 && !KEY_FRAME) by calling
// shouldDropInterFrame and refunding av_per_frame_bandwidth via
// postDropFrame.
func (rc *rateControlState) pickFrameSize(keyFrame bool, baseTargetBits int, ctx rateControlFrameContext) bool {
	rc.beginFrameWithTargetAndContext(keyFrame, baseTargetBits, ctx)
	if keyFrame {
		return true
	}
	if rc.shouldDropInterFrame() {
		rc.postDropFrame()
		return false
	}
	return true
}
