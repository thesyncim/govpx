package govpx

import (
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// CYCLIC_REFRESH_AQ — verbatim port of libvpx v1.16.0
// vp9/encoder/vp9_aq_cyclicrefresh.{c,h}. The state machine maintains
// a refresh map of (mi_rows * mi_cols) signed-char entries indexed in
// 8x8 raster order, a rotating sb_index pointer cycling through the
// frame in superblock order, and per-segment qindex deltas computed
// from rate_ratio_qdelta.
//
// libvpx: vp9/encoder/vp9_aq_cyclicrefresh.h:23-30
const (
	// CR_SEGMENT_ID_BASE — segment id for no refresh.
	// libvpx: vp9_aq_cyclicrefresh.h:25
	vp9CyclicRefreshSegmentBase = 0
	// CR_SEGMENT_ID_BOOST1 — base refresh segment.
	// libvpx: vp9_aq_cyclicrefresh.h:26
	vp9CyclicRefreshSegmentBoost1 = 1
	// CR_SEGMENT_ID_BOOST2 — more aggressive refresh segment.
	// libvpx: vp9_aq_cyclicrefresh.h:27
	vp9CyclicRefreshSegmentBoost2 = 2
	// CR_MAX_RATE_TARGET_RATIO clamps segment BOOST2's rate target ratio.
	// libvpx: vp9_aq_cyclicrefresh.h:30
	vp9CyclicRefreshMaxRateTargetRatio = 4.0
	// MI_BLOCK_SIZE — superblock size in 8x8 mi units.
	// libvpx: vp9/common/vp9_onyxc_int.h MI_BLOCK_SIZE = 8
	vp9CyclicRefreshSuperblockMi = 8
)

// vp9CyclicRefreshState mirrors libvpx's struct CYCLIC_REFRESH from
// vp9_aq_cyclicrefresh.h:32-74 in field-for-field order so callers
// can compare against the C oracle one-to-one.
type vp9CyclicRefreshState struct {
	// enabled latches the AQ mode at configure time. Mirrors
	// cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ.
	enabled bool

	// percent_refresh — target fraction of (8x8) blocks per frame.
	// libvpx: vp9_aq_cyclicrefresh.h:35
	percentRefresh int
	// max_qdelta_perc — cap on q-delta as % of base q.
	// libvpx: vp9_aq_cyclicrefresh.h:37
	maxQdeltaPerc int
	// sb_index — rotating superblock pointer through the frame.
	// libvpx: vp9_aq_cyclicrefresh.h:39
	sbIndex int
	// time_for_refresh — extra cycle-wait for refreshed blocks.
	// libvpx: vp9_aq_cyclicrefresh.h:43
	timeForRefresh int
	// target_num_seg_blocks — blocks slated for delta-q this frame.
	// libvpx: vp9_aq_cyclicrefresh.h:45
	targetNumSegBlocks int
	// actual_num_seg{1,2}_blocks — refreshed-this-frame buckets.
	// libvpx: vp9_aq_cyclicrefresh.h:47-48
	actualNumSeg1Blocks int
	actualNumSeg2Blocks int
	// rdmult — RD multiplier for segment 1.
	// libvpx: vp9_aq_cyclicrefresh.h:50
	rdmult int
	// map — per-(8x8) refresh state (signed char in libvpx).
	// libvpx: vp9_aq_cyclicrefresh.h:52
	refreshMap []int8
	// last_coded_q_map — last q a block was coded at.
	// libvpx: vp9_aq_cyclicrefresh.h:54
	lastCodedQMap []uint8
	// thresh_rate_sb / thresh_dist_sb — per-SB projected rate/dist thresholds.
	// libvpx: vp9_aq_cyclicrefresh.h:57-58
	threshRateSb int64
	threshDistSb int64
	// motion_thresh — MV magnitude cap (1/8 pel units).
	// libvpx: vp9_aq_cyclicrefresh.h:61
	motionThresh int16
	// rate_ratio_qdelta — rate ratio target driving compute_deltaq().
	// libvpx: vp9_aq_cyclicrefresh.h:63
	rateRatioQdelta float64
	// rate_boost_fac — boost factor for segment BOOST2.
	// libvpx: vp9_aq_cyclicrefresh.h:65
	rateBoostFac int
	// low_content_avg — running average of low-motion frame fraction.
	// libvpx: vp9_aq_cyclicrefresh.h:66
	lowContentAvg float64
	// qindex_delta[3] — per-segment q deltas. Index 0 unused, 1/2 active.
	// libvpx: vp9_aq_cyclicrefresh.h:67
	qindexDelta [3]int
	// reduce_refresh — drop percent_refresh to 5 when set.
	// libvpx: vp9_aq_cyclicrefresh.h:68
	reduceRefresh bool
	// weight_segment — segment weight average for rc_bits_per_mb().
	// libvpx: vp9_aq_cyclicrefresh.h:69
	weightSegment float64
	// apply_cyclic_refresh — per-frame gate from update_parameters().
	// libvpx: vp9_aq_cyclicrefresh.h:70
	applyCyclicRefresh bool
	// counter_encode_maxq_scene_change — high-Q scene-change counter.
	// libvpx: vp9_aq_cyclicrefresh.h:71
	counterEncodeMaxqSceneChange int
	// skip_flat_static_blocks — screen-content flat-block skip.
	// libvpx: vp9_aq_cyclicrefresh.h:72
	skipFlatStaticBlocks bool
	// content_mode — content classification flag (default 1).
	// libvpx: vp9_aq_cyclicrefresh.h:73
	contentMode bool

	// segmentation_map — exposed segment id grid the encoder consults
	// per (mi_row, mi_col). Mirrors libvpx's cpi->segmentation_map for
	// the lifetime of the cyclic-refresh state.
	segMap []uint8

	// consecZeroMv — per-(mi_row, mi_col) saturating counter of
	// consecutive frames the LAST-frame MV at this 8x8 block was
	// near-zero (|mv.row| < 8 && |mv.col| < 8). Mirrors
	// cpi->consec_zero_mv from libvpx's VP9_COMP
	// (vp9/encoder/vp9_encoder.h:838) — co-located here because the
	// cyclic refresh setup path is the only consumer. Updated per
	// encoded SB by vp9CyclicRefreshUpdateZeroMVCnt, mirroring
	// libvpx's update_zeromv_cnt (vp9_encodeframe.c:5999-6022). The
	// counter feeds the eligibility filter in
	// cyclic_refresh_update_map (vp9_aq_cyclicrefresh.c:437-442).
	consecZeroMv []uint8

	// miRows / miCols pin the current frame's mi-grid dims.
	miRows int
	miCols int

	// apply tracks whether this frame's segmentation has been built and
	// should be honoured by the segment-id lookup path.
	apply bool
}

// vp9CyclicRefreshAlloc mirrors libvpx vp9_cyclic_refresh_alloc()
// from vp9_aq_cyclicrefresh.c:32-53. Resets last_coded_q_map to MAXQ
// (255) and seeds counter_encode_maxq_scene_change/content_mode.
func (cr *vp9CyclicRefreshState) vp9CyclicRefreshAlloc(miRows, miCols int) {
	n := miRows * miCols
	if n <= 0 {
		cr.refreshMap = nil
		cr.lastCodedQMap = nil
		cr.segMap = nil
		cr.consecZeroMv = nil
		cr.miRows = 0
		cr.miCols = 0
		return
	}
	if cap(cr.refreshMap) < n {
		cr.refreshMap = make([]int8, n)
	} else {
		cr.refreshMap = cr.refreshMap[:n]
		for i := range cr.refreshMap {
			cr.refreshMap[i] = 0
		}
	}
	if cap(cr.lastCodedQMap) < n {
		cr.lastCodedQMap = make([]uint8, n)
	} else {
		cr.lastCodedQMap = cr.lastCodedQMap[:n]
	}
	// libvpx: vp9_aq_cyclicrefresh.c:49 — memset to MAXQ.
	for i := range cr.lastCodedQMap {
		cr.lastCodedQMap[i] = vp9dec.MaxQ
	}
	if cap(cr.segMap) < n {
		cr.segMap = make([]uint8, n)
	} else {
		cr.segMap = cr.segMap[:n]
		for i := range cr.segMap {
			cr.segMap[i] = 0
		}
	}
	// libvpx: vp9_encoder.c:2180-2183 — vpx_calloc(consec_zero_mv).
	if cap(cr.consecZeroMv) < n {
		cr.consecZeroMv = make([]uint8, n)
	} else {
		cr.consecZeroMv = cr.consecZeroMv[:n]
		for i := range cr.consecZeroMv {
			cr.consecZeroMv[i] = 0
		}
	}
	cr.miRows = miRows
	cr.miCols = miCols
	// libvpx: vp9_aq_cyclicrefresh.c:50-51.
	cr.counterEncodeMaxqSceneChange = 0
	cr.contentMode = true
}

// configure latches the AQ mode and re-allocates internal maps. Called
// by the encoder when the AQ option or resolution changes.
func (cr *vp9CyclicRefreshState) configure(enabled bool, width, height int) {
	cr.enabled = enabled
	cr.apply = false
	if !enabled || width <= 0 || height <= 0 {
		cr.refreshMap = nil
		cr.lastCodedQMap = nil
		cr.segMap = nil
		cr.consecZeroMv = nil
		cr.miRows = 0
		cr.miCols = 0
		cr.sbIndex = 0
		return
	}
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	cr.vp9CyclicRefreshAlloc(miRows, miCols)
	sbCount := vp9CyclicRefreshSuperblockCount(miRows, miCols)
	if sbCount > 0 && cr.sbIndex >= sbCount {
		cr.sbIndex = 0
	}
}

// vp9CyclicRefreshResetResize mirrors vp9_cyclic_refresh_reset_resize()
// from vp9_aq_cyclicrefresh.c:686-696. Zeros the refresh map, resets
// last_coded_q_map to MAXQ, parks sb_index at 0, and zeros the
// scene-change counter.
func (cr *vp9CyclicRefreshState) vp9CyclicRefreshResetResize() {
	for i := range cr.refreshMap {
		cr.refreshMap[i] = 0
	}
	for i := range cr.lastCodedQMap {
		cr.lastCodedQMap[i] = vp9dec.MaxQ
	}
	// libvpx: vp9_encoder.c:4103-4106 — resize_pending zeroes consec_zero_mv.
	for i := range cr.consecZeroMv {
		cr.consecZeroMv[i] = 0
	}
	cr.sbIndex = 0
	cr.counterEncodeMaxqSceneChange = 0
}

// vp9CyclicRefreshComputeDeltaq mirrors compute_deltaq() from
// vp9_aq_cyclicrefresh.c:90-99. Translates a rate-ratio target into a
// qindex delta, clamped to -max_qdelta_perc * q / 100.
func (cr *vp9CyclicRefreshState) vp9CyclicRefreshComputeDeltaq(q int, rateFactor float64, intraFrame bool) int {
	// libvpx: vp9_aq_cyclicrefresh.c:93 — vp9_compute_qdelta_by_rate(rc, frame_type, q, rate_factor, bit_depth).
	deltaq := vp9CyclicRefreshComputeQDeltaByRate(q, rateFactor, intraFrame)
	// libvpx: vp9_aq_cyclicrefresh.c:95-97 — clamp -deltaq to max_qdelta_perc.
	if -deltaq > cr.maxQdeltaPerc*q/100 {
		deltaq = -cr.maxQdeltaPerc * q / 100
	}
	return deltaq
}

// vp9CyclicRefreshComputeQDeltaByRate matches libvpx's
// vp9_compute_qdelta_by_rate (vp9_ratectrl.c:2573-2595): for best=0,
// worst=MAXQ, finds the smallest qindex whose projected bits-per-mb is
// <= rate_target_ratio * base_bits_per_mb.
func vp9CyclicRefreshComputeQDeltaByRate(qindex int, rateTargetRatio float64, intraFrame bool) int {
	if qindex < 0 {
		qindex = 0
	} else if qindex > vp9dec.MaxQ {
		qindex = vp9dec.MaxQ
	}
	// libvpx: vp9_ratectrl.c:2580-2581 — base_bits_per_mb.
	baseBitsPerMB := vp9RCBitsPerMB(intraFrame, qindex, 1.0)
	// libvpx: vp9_ratectrl.c:2584 — target_bits_per_mb = ratio * base.
	targetBitsPerMB := int(rateTargetRatio * float64(baseBitsPerMB))
	targetIndex := vp9dec.MaxQ
	// libvpx: vp9_ratectrl.c:2587-2593.
	for i := range vp9dec.MaxQ {
		if vp9RCBitsPerMB(intraFrame, i, 1.0) <= targetBitsPerMB {
			targetIndex = i
			break
		}
	}
	return targetIndex - qindex
}

// vp9CyclicRefreshUpdateMap mirrors cyclic_refresh_update_map() from
// vp9_aq_cyclicrefresh.c:364-476. Walks superblocks starting at sb_index,
// flips eligible 8x8 blocks (refreshMap == 0) to BOOST1 until
// target_num_seg_blocks blocks are queued or the cycle completes.
//
// This is the deterministic baseline cycling pass; libvpx layers an
// extra last_coded_q_map / consec_zero_mv filter on top via
// qindex_thresh / consec_zero_mv_thresh, which govpx exposes via
// the optional eligibility arguments below.
func (cr *vp9CyclicRefreshState) vp9CyclicRefreshUpdateMap(consecZeroMv []uint8, qindexThresh int, consecZeroMvThresh int, screenContent bool) {
	miRows, miCols := cr.miRows, cr.miCols
	if miRows <= 0 || miCols <= 0 {
		return
	}
	// libvpx: vp9_aq_cyclicrefresh.c:374 — memset seg_map BASE.
	for i := range cr.segMap {
		cr.segMap[i] = vp9CyclicRefreshSegmentBase
	}
	const blockMi = vp9CyclicRefreshSuperblockMi
	sbCols := (miCols + blockMi - 1) / blockMi
	sbRows := (miRows + blockMi - 1) / blockMi
	sbsInFrame := sbCols * sbRows
	if sbsInFrame <= 0 {
		cr.targetNumSegBlocks = 0
		return
	}
	// libvpx: vp9_aq_cyclicrefresh.c:379 — block_count = percent_refresh * mi_rows * mi_cols / 100.
	blockCount := cr.percentRefresh * miRows * miCols / 100
	// libvpx: vp9_aq_cyclicrefresh.c:384.
	if cr.sbIndex >= sbsInFrame {
		cr.sbIndex = 0
	}
	i := cr.sbIndex
	cr.targetNumSegBlocks = 0
	countSel := 0
	countTot := 0
	for {
		sbRow := i / sbCols
		sbCol := i - sbRow*sbCols
		miRow := sbRow * blockMi
		miCol := sbCol * blockMi
		blIndex := miRow*miCols + miCol
		xmis := min(miCols-miCol, blockMi)
		ymis := min(miRows-miRow, blockMi)
		sumMap := 0
		// libvpx: vp9_aq_cyclicrefresh.c:429-447 — loop through 8x8 blocks.
		for y := range ymis {
			for x := range xmis {
				blIndex2 := blIndex + y*miCols + x
				if cr.refreshMap[blIndex2] == 0 {
					countTot++
					// libvpx: vp9_aq_cyclicrefresh.c:437-442 — eligibility filter.
					eligible := true
					if cr.contentMode && consecZeroMv != nil && len(consecZeroMv) > blIndex2 && len(cr.lastCodedQMap) > blIndex2 {
						if int(cr.lastCodedQMap[blIndex2]) > qindexThresh ||
							int(consecZeroMv[blIndex2]) < consecZeroMvThresh {
							// matches libvpx — also eligible.
							eligible = true
						} else {
							eligible = false
						}
					}
					if eligible {
						sumMap++
						countSel++
					}
				} else if cr.refreshMap[blIndex2] < 0 {
					// libvpx: vp9_aq_cyclicrefresh.c:443-445 — increment cooldown counters.
					cr.refreshMap[blIndex2]++
				}
			}
		}
		// libvpx: vp9_aq_cyclicrefresh.c:450 — half-superblock heuristic.
		if sumMap >= xmis*ymis/2 {
			for y := range ymis {
				for x := range xmis {
					cr.segMap[blIndex+y*miCols+x] = vp9CyclicRefreshSegmentBoost1
				}
			}
			cr.targetNumSegBlocks += xmis * ymis
		}
		i++
		if i == sbsInFrame {
			i = 0
		}
		if cr.targetNumSegBlocks >= blockCount || i == cr.sbIndex {
			break
		}
	}
	cr.sbIndex = i
	// libvpx: vp9_aq_cyclicrefresh.c:473-475 — reduce_refresh gate.
	cr.reduceRefresh = false
	if !screenContent && countSel < (3*countTot)>>2 {
		cr.reduceRefresh = true
	}
}

// vp9CyclicRefreshUpdateParameters mirrors
// vp9_cyclic_refresh_update_parameters() from vp9_aq_cyclicrefresh.c:479-593.
// Decides apply_cyclic_refresh + sets per-frame
// percent_refresh / rate_ratio_qdelta / motion_thresh.
func (cr *vp9CyclicRefreshState) vp9CyclicRefreshUpdateParameters(args vp9CyclicRefreshUpdateParametersArgs) {
	num8x8bl := args.Macroblocks << 2
	thresholdLowMotion := 20
	qpThresh := 20
	if args.ScreenContent {
		qpThresh = 35
	}
	if qpThresh > args.BestQuality<<1 {
		qpThresh = args.BestQuality << 1
	}
	qpMaxThresh := 117 * vp9dec.MaxQ >> 7
	cr.applyCyclicRefresh = true
	// libvpx: vp9_aq_cyclicrefresh.c:492-505 — disable gates.
	if args.FrameIsIntraOnly || args.TemporalLayerID > 0 ||
		args.Lossless ||
		args.AvgFrameQindexInter < qpThresh ||
		(!args.UseSVC && cr.contentMode &&
			args.AvgFrameLowMotion < thresholdLowMotion &&
			args.FramesSinceKey > 40) ||
		(!args.UseSVC && args.AvgFrameQindexInter > qpMaxThresh &&
			args.FramesSinceKey > 20) {
		cr.applyCyclicRefresh = false
		return
	}
	// libvpx: vp9_aq_cyclicrefresh.c:507-512.
	cr.percentRefresh = 10
	if cr.reduceRefresh {
		cr.percentRefresh = 5
	}
	cr.maxQdeltaPerc = 60
	cr.timeForRefresh = 0
	cr.motionThresh = 32
	cr.rateBoostFac = 15
	// libvpx: vp9_aq_cyclicrefresh.c:516-528 — boosted ratio after key.
	numTemporalLayers := args.NumberTemporalLayers
	if numTemporalLayers <= 0 {
		numTemporalLayers = 1
	}
	if cr.percentRefresh > 0 &&
		args.FramesSinceKey < (4*numTemporalLayers)*(100/cr.percentRefresh) {
		cr.rateRatioQdelta = 3.0
	} else {
		cr.rateRatioQdelta = 2.0
		if cr.contentMode && args.NoiseLevelMedium {
			cr.rateRatioQdelta = 1.7
			cr.rateBoostFac = 13
		}
	}
	// libvpx: vp9_aq_cyclicrefresh.c:532-544 — screen-content tweaks.
	if args.ScreenContent {
		if args.SpatialLayerID == args.NumberSpatialLayers-1 {
			cr.skipFlatStaticBlocks = true
		}
		if cr.skipFlatStaticBlocks {
			cr.percentRefresh = 5
		} else {
			cr.percentRefresh = 10
		}
		if cr.contentMode && cr.counterEncodeMaxqSceneChange < 30 {
			if cr.skipFlatStaticBlocks {
				cr.percentRefresh = 10
			} else {
				cr.percentRefresh = 15
			}
		}
		cr.rateRatioQdelta = 2.0
		cr.rateBoostFac = 10
	}
	// libvpx: vp9_aq_cyclicrefresh.c:546-554 — low-resolution tweaks.
	if args.Width*args.Height <= 352*288 {
		if args.AvgFrameBandwidth < 3000 {
			cr.motionThresh = 64
			cr.rateBoostFac = 13
		} else {
			cr.maxQdeltaPerc = 70
			if cr.rateRatioQdelta < 2.5 {
				cr.rateRatioQdelta = 2.5
			}
		}
	}
	// libvpx: vp9_aq_cyclicrefresh.c:555-566 — VBR tweaks.
	if args.RateControlIsVBR {
		cr.percentRefresh = 10
		cr.rateRatioQdelta = 1.5
		cr.rateBoostFac = 10
		if args.RefreshGoldenFrame && !args.UseSVC {
			cr.percentRefresh = 0
			cr.rateRatioQdelta = 1.0
		}
	}
	// libvpx: vp9_aq_cyclicrefresh.c:571-578 — segment-weight average.
	targetRefresh := cr.percentRefresh * cr.miRows * cr.miCols / 100
	weightSegmentTarget := float64(targetRefresh) / float64(num8x8bl)
	weightSegment := float64((targetRefresh+cr.actualNumSeg1Blocks+
		cr.actualNumSeg2Blocks)>>1) / float64(num8x8bl)
	if weightSegmentTarget < 7*weightSegment/8 {
		weightSegment = weightSegmentTarget
	}
	if args.ScreenContent {
		weightSegment = float64(cr.actualNumSeg1Blocks+cr.actualNumSeg2Blocks) /
			float64(num8x8bl)
	}
	cr.weightSegment = weightSegment
	// libvpx: vp9_aq_cyclicrefresh.c:587-592 — content_mode=0 fallback.
	if !cr.contentMode {
		cr.actualNumSeg1Blocks = cr.percentRefresh * cr.miRows * cr.miCols / 100
		cr.actualNumSeg2Blocks = 0
		cr.weightSegment = float64(cr.actualNumSeg1Blocks) / float64(num8x8bl)
	}
}

// vp9CyclicRefreshUpdateParametersArgs bundles the libvpx update_parameters()
// inputs the encoder threads down from VP9_COMP / RATE_CONTROL.
type vp9CyclicRefreshUpdateParametersArgs struct {
	Macroblocks          int
	FrameIsIntraOnly     bool
	TemporalLayerID      int
	NumberTemporalLayers int
	NumberSpatialLayers  int
	SpatialLayerID       int
	Lossless             bool
	UseSVC               bool
	ScreenContent        bool
	NoiseLevelMedium     bool
	RateControlIsVBR     bool
	RefreshGoldenFrame   bool
	AvgFrameQindexInter  int
	AvgFrameLowMotion    int
	FramesSinceKey       int
	BestQuality          int
	AvgFrameBandwidth    int
	Width                int
	Height               int
}

// vp9CyclicRefreshSetup mirrors vp9_cyclic_refresh_setup() from
// vp9_aq_cyclicrefresh.c:596-680. Decides whether to emit
// segmentation, computes qindex_delta[1]/[2], and rebuilds the
// segmentation map.
func (cr *vp9CyclicRefreshState) vp9CyclicRefreshSetup(args vp9CyclicRefreshSetupArgs) {
	if cr.miRows <= 0 || cr.miCols <= 0 {
		cr.apply = false
		return
	}
	// libvpx: vp9_aq_cyclicrefresh.c:604.
	if args.CurrentVideoFrame == 0 {
		cr.lowContentAvg = 0.0
	}
	// libvpx: vp9_aq_cyclicrefresh.c:606-607.
	if args.ResizePending && args.TemporalLayerID == 0 {
		cr.vp9CyclicRefreshResetResize()
	}
	sceneChange := args.HighSourceSad
	// libvpx: vp9_aq_cyclicrefresh.c:608-622 — disable-segmentation path.
	if !cr.applyCyclicRefresh || args.ForceUpdateSegmentation || sceneChange {
		for i := range cr.segMap {
			cr.segMap[i] = 0
		}
		if (args.FrameIsKey || sceneChange) && args.TemporalLayerID == 0 {
			for i := range cr.lastCodedQMap {
				cr.lastCodedQMap[i] = vp9dec.MaxQ
			}
			cr.sbIndex = 0
			cr.reduceRefresh = false
			cr.counterEncodeMaxqSceneChange = 0
		}
		cr.apply = false
		return
	}
	cr.counterEncodeMaxqSceneChange++
	// libvpx: vp9_aq_cyclicrefresh.c:631 — thresh_rate_sb.
	cr.threshRateSb = (int64(args.Sb64TargetRate) << 8) << 2
	// libvpx: vp9_aq_cyclicrefresh.c:635 — thresh_dist_sb.
	q := vp9ConvertQIndexToQ(args.BaseQindex)
	cr.threshDistSb = int64(q*q) << 2
	// libvpx: vp9_aq_cyclicrefresh.c:659 — compute_deltaq for BOOST1.
	cr.qindexDelta[0] = 0
	cr.qindexDelta[1] = cr.vp9CyclicRefreshComputeDeltaq(args.BaseQindex, cr.rateRatioQdelta, args.FrameIsIntraOnly)
	// libvpx: vp9_aq_cyclicrefresh.c:665 — rdmult.
	//   cr->rdmult = vp9_compute_rd_mult(cpi, qindex2);
	// The frame-type bucket follows the libvpx branching in
	// vp9_compute_rd_mult_based_on_qindex: KF wins, then ARF/GF when
	// refreshing, else inter.  CR runs after the encoder has resolved
	// refresh flags, so we recompute the same bucket here.
	qindex2 := clamp(args.BaseQindex+args.YDcDeltaQ+cr.qindexDelta[1], 0, vp9dec.MaxQ)
	frameType := encoder.RDFrameTypeFor(args.FrameIsKey, args.IsSrcFrameAltRef,
		args.RefreshGoldenFrame, args.RefreshAltRefFrame)
	cr.rdmult = encoder.ComputeRDMult(qindex2, frameType)
	// libvpx: vp9_aq_cyclicrefresh.c:669-674 — BOOST2 delta.
	ratio := 0.1 * float64(cr.rateBoostFac) * cr.rateRatioQdelta
	if ratio > vp9CyclicRefreshMaxRateTargetRatio {
		ratio = vp9CyclicRefreshMaxRateTargetRatio
	}
	cr.qindexDelta[2] = cr.vp9CyclicRefreshComputeDeltaq(args.BaseQindex, ratio, args.FrameIsIntraOnly)
	// libvpx: vp9_aq_cyclicrefresh.c:678.
	consecZeroMvThresh := 0
	if !args.ScreenContent {
		consecZeroMvThresh = 100
	}
	qindexThresh := args.BaseQindex + cr.qindexDelta[1]
	if args.ScreenContent {
		qindexThresh = args.BaseQindex + cr.qindexDelta[2]
	}
	if cr.contentMode && args.NoiseLevelMedium {
		consecZeroMvThresh = 60
		if qindexThresh < args.BaseQindex {
			qindexThresh = args.BaseQindex
		}
	}
	cr.vp9CyclicRefreshUpdateMap(args.ConsecZeroMv, qindexThresh, consecZeroMvThresh, args.ScreenContent)
	cr.apply = true
}

// vp9CyclicRefreshSetupArgs bundles the libvpx setup() inputs from
// VP9_COMP / RATE_CONTROL / VP9_COMMON.
type vp9CyclicRefreshSetupArgs struct {
	CurrentVideoFrame       int
	FrameIsKey              bool
	FrameIsIntraOnly        bool
	TemporalLayerID         int
	ResizePending           bool
	HighSourceSad           bool
	ForceUpdateSegmentation bool
	ScreenContent           bool
	NoiseLevelMedium        bool
	BaseQindex              int
	YDcDeltaQ               int
	Sb64TargetRate          int
	ConsecZeroMv            []uint8
	// IsSrcFrameAltRef / RefreshGoldenFrame / RefreshAltRefFrame mirror
	// cpi->rc.is_src_frame_alt_ref, cpi->refresh_golden_frame, and
	// cpi->refresh_alt_ref_frame from vp9_aq_cyclicrefresh.c:665 — the
	// inputs to libvpx's vp9_compute_rd_mult frame-type branching.
	IsSrcFrameAltRef   bool
	RefreshGoldenFrame bool
	RefreshAltRefFrame bool
}

// vp9CyclicRefreshPostencode mirrors vp9_cyclic_refresh_postencode()
// from vp9_aq_cyclicrefresh.c:261-317. Counts actual segment 1/2 blocks,
// accumulates low_content_avg, and gates golden-frame refresh.
func (cr *vp9CyclicRefreshState) vp9CyclicRefreshPostencode(args vp9CyclicRefreshPostencodeArgs) vp9CyclicRefreshPostencodeResult {
	cr.actualNumSeg1Blocks = 0
	cr.actualNumSeg2Blocks = 0
	miRows, miCols := cr.miRows, cr.miCols
	lowContentFrame := 0
	// libvpx: vp9_aq_cyclicrefresh.c:273-288.
	for mr := range miRows {
		for mc := range miCols {
			idx := mr*miCols + mc
			if idx < len(cr.segMap) {
				switch cr.segMap[idx] {
				case vp9CyclicRefreshSegmentBoost1:
					cr.actualNumSeg1Blocks++
				case vp9CyclicRefreshSegmentBoost2:
					cr.actualNumSeg2Blocks++
				}
			}
			if args.IsInterBlock != nil && idx < len(args.IsInterBlock) &&
				args.IsInterBlock[idx] != 0 && args.MvRow != nil &&
				args.MvCol != nil && idx < len(args.MvRow) &&
				idx < len(args.MvCol) &&
				absInt16(args.MvRow[idx]) < 16 && absInt16(args.MvCol[idx]) < 16 {
				lowContentFrame++
			}
		}
	}
	res := vp9CyclicRefreshPostencodeResult{}
	if args.UseSVC || args.ExtRefreshFrameFlagsPending || args.GfCBRBoostPct != 0 {
		return res
	}
	// libvpx: vp9_aq_cyclicrefresh.c:294-301 — force golden on resize.
	if args.ResizePending {
		res.SetGoldenUpdate = true
		res.ForceGoldenRefresh = true
	}
	denom := miRows * miCols
	if denom > 0 {
		fractionLow := float64(lowContentFrame) / float64(denom)
		cr.lowContentAvg = (fractionLow + 3*cr.lowContentAvg) / 4
		// libvpx: vp9_aq_cyclicrefresh.c:305-315 — reject golden if low-content too small.
		if !res.ForceGoldenRefresh && args.RefreshGoldenFrame &&
			args.FramesSinceKey > args.FramesSinceGolden+1 {
			if fractionLow < 0.65 || cr.lowContentAvg < 0.6 {
				res.ClearRefreshGolden = true
			}
			cr.lowContentAvg = fractionLow
		}
	}
	return res
}

type vp9CyclicRefreshPostencodeArgs struct {
	UseSVC                      bool
	ExtRefreshFrameFlagsPending bool
	GfCBRBoostPct               int
	ResizePending               bool
	RefreshGoldenFrame          bool
	FramesSinceKey              int
	FramesSinceGolden           int
	// Optional per-(mi_row, mi_col) post-coded state — when nil the
	// low-content accumulator is skipped.
	IsInterBlock []uint8
	MvRow        []int16
	MvCol        []int16
}

type vp9CyclicRefreshPostencodeResult struct {
	SetGoldenUpdate    bool
	ForceGoldenRefresh bool
	ClearRefreshGolden bool
}

// vp9CyclicRefreshSetGoldenUpdate mirrors
// vp9_cyclic_refresh_set_golden_update() from vp9_aq_cyclicrefresh.c:320-334.
// Returns the baseline_gf_interval value the rate controller should
// install.
func (cr *vp9CyclicRefreshState) vp9CyclicRefreshSetGoldenUpdate(args vp9CyclicRefreshSetGoldenUpdateArgs) int {
	var baseline int
	if cr.percentRefresh > 0 {
		baseline = min(4*(100/cr.percentRefresh), 40)
	} else {
		baseline = 40
	}
	if args.RateControlIsVBR {
		baseline = 20
	}
	// libvpx: vp9_aq_cyclicrefresh.c:331-333.
	if args.AvgFrameLowMotion < 50 && args.FramesSinceKey > 40 && cr.contentMode {
		baseline = 10
	}
	return baseline
}

type vp9CyclicRefreshSetGoldenUpdateArgs struct {
	RateControlIsVBR  bool
	AvgFrameLowMotion int
	FramesSinceKey    int
}

// vp9CyclicRefreshLimitQ mirrors vp9_cyclic_refresh_limit_q() from
// vp9_aq_cyclicrefresh.c:698-705. Applies a -8 frame-level q step
// limit when percent_refresh > 0.
func (cr *vp9CyclicRefreshState) vp9CyclicRefreshLimitQ(q1Frame int, q *int) {
	if q == nil {
		return
	}
	if cr.percentRefresh > 0 && q1Frame-*q > 8 {
		*q = q1Frame - 8
	}
}

// vp9CyclicRefreshUpdateSegmentPostencode mirrors
// vp9_cyclic_refresh_update_sb_postencode() from
// vp9_aq_cyclicrefresh.c:225-255. Updates last_coded_q_map for the
// encoded SB.
func (cr *vp9CyclicRefreshState) vp9CyclicRefreshUpdateSegmentPostencode(miRow, miCol, bw, bh int, baseQindex int, segID uint8, isInter, skip bool) {
	if cr.miRows <= 0 || cr.miCols <= 0 {
		return
	}
	xmis := min(cr.miCols-miCol, bw)
	ymis := min(cr.miRows-miRow, bh)
	blIndex := miRow*cr.miCols + miCol
	if segID > vp9CyclicRefreshSegmentBoost2 {
		return
	}
	q := clamp(baseQindex+cr.qindexDelta[segID], 0, vp9dec.MaxQ)
	for y := range ymis {
		for x := range xmis {
			off := blIndex + y*cr.miCols + x
			if off >= len(cr.lastCodedQMap) {
				continue
			}
			if !isInter || !skip {
				cr.lastCodedQMap[off] = uint8(q)
			} else {
				if uint8(q) < cr.lastCodedQMap[off] {
					cr.lastCodedQMap[off] = uint8(q)
				}
			}
		}
	}
}

// vp9CyclicRefreshUpdateZeroMVCnt mirrors update_zeromv_cnt() from
// vp9/encoder/vp9_encodeframe.c:5999-6022. For every encoded SB whose
// chosen leaf references LAST_FRAME and is inter (and the leaf is in
// a refresh-tracked segment), bumps consec_zero_mv up by one when the
// chosen MV magnitude is < 8 in both dimensions, and resets to zero
// otherwise. The counter is saturating at 255. Called from the per-SB
// encode hook so the next frame's update_map filter sees the correct
// per-block stationarity history.
func (cr *vp9CyclicRefreshState) vp9CyclicRefreshUpdateZeroMVCnt(
	miRow, miCol, bw, bh int, mvRow, mvCol int16, refFrame int8,
	isInter bool, segID uint8,
) {
	if cr.miRows <= 0 || cr.miCols <= 0 || len(cr.consecZeroMv) == 0 {
		return
	}
	// libvpx: vp9_encodeframe.c:6012 — gates on LAST_FRAME + inter +
	// segment_id <= CR_SEGMENT_ID_BOOST2.
	if !isInter || refFrame != vp9dec.LastFrame ||
		segID > vp9CyclicRefreshSegmentBoost2 {
		return
	}
	xmis := min(cr.miCols-miCol, bw)
	ymis := min(cr.miRows-miRow, bh)
	if xmis <= 0 || ymis <= 0 {
		return
	}
	blIndex := miRow*cr.miCols + miCol
	zero := absInt16(mvRow) < 8 && absInt16(mvCol) < 8
	for y := range ymis {
		for x := range xmis {
			off := blIndex + y*cr.miCols + x
			if off < 0 || off >= len(cr.consecZeroMv) {
				continue
			}
			if zero {
				// libvpx: vp9_encodeframe.c:6014-6016 — saturating bump.
				if cr.consecZeroMv[off] < 255 {
					cr.consecZeroMv[off]++
				}
			} else {
				// libvpx: vp9_encodeframe.c:6017-6019 — reset on large MV.
				cr.consecZeroMv[off] = 0
			}
		}
	}
}

// prepareFrame is the encoder-facing entry point. Equivalent to libvpx's
// vp9_cyclic_refresh_update_parameters + vp9_cyclic_refresh_setup pair,
// called once per frame just before vp9_encode_frame. Maintains the
// existing govpx call surface.
func (cr *vp9CyclicRefreshState) prepareFrame(apply bool, miRows, miCols int) {
	cr.apply = false
	if !cr.enabled || !apply || miRows <= 0 || miCols <= 0 {
		return
	}
	// Re-alloc on mi-grid change.
	if cr.miRows != miRows || cr.miCols != miCols || len(cr.segMap) < miRows*miCols {
		cr.vp9CyclicRefreshAlloc(miRows, miCols)
	}
	// Default per-frame params if update_parameters() was not threaded in.
	if cr.percentRefresh == 0 {
		cr.percentRefresh = 10
	}
	if cr.maxQdeltaPerc == 0 {
		cr.maxQdeltaPerc = 60
	}
	if cr.rateRatioQdelta == 0 {
		cr.rateRatioQdelta = 2.0
	}
	if cr.rateBoostFac == 0 {
		cr.rateBoostFac = 15
	}
	if cr.motionThresh == 0 {
		cr.motionThresh = 32
	}
	cr.applyCyclicRefresh = true
	cr.vp9CyclicRefreshUpdateMap(nil, 0, 0, false)
	cr.apply = cr.targetNumSegBlocks > 0
}

func vp9CyclicRefreshSuperblockCount(miRows, miCols int) int {
	if miRows <= 0 || miCols <= 0 {
		return 0
	}
	sbCols := (miCols + vp9CyclicRefreshSuperblockMi - 1) / vp9CyclicRefreshSuperblockMi
	sbRows := (miRows + vp9CyclicRefreshSuperblockMi - 1) / vp9CyclicRefreshSuperblockMi
	return sbRows * sbCols
}

func (cr *vp9CyclicRefreshState) segmentID(miRow, miCol int) uint8 {
	if !cr.enabled || !cr.apply || miRow < 0 || miCol < 0 ||
		miRow >= cr.miRows || miCol >= cr.miCols {
		return 0
	}
	idx := miRow*cr.miCols + miCol
	if idx < 0 || idx >= len(cr.segMap) {
		return 0
	}
	if cr.segMap[idx] >= vp9dec.MaxSegments {
		return 0
	}
	return cr.segMap[idx]
}

// segmentationParams builds the SegmentationParams libvpx emits for
// cyclic-refresh frames. Mirrors the BOOST1/BOOST2 enable + alt-q
// assignment in vp9_aq_cyclicrefresh.c:639-675.
func (cr *vp9CyclicRefreshState) segmentationParams(baseQIndex int) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   false, // libvpx: vp9_aq_cyclicrefresh.c:642 — SEGMENT_DELTADATA.
	}
	// libvpx's vp9_choose_segmap_coding_method (vp9_segmentation.c:242-316)
	// initialises seg->tree_probs / seg->pred_probs to MAX_PROB (255),
	// then OVERWRITES them with the realized-mi_grid counts at
	// encode_segmentation time (vp9_bitstream.c:773). libvpx's
	// cyclic-refresh setup never touches tree_probs itself
	// (vp9_aq_cyclicrefresh.c writes only feature_data / feature_mask).
	// Match that contract: seed with MAX_PROB here and let the
	// chooser populate the realized-grid probs via
	// vp9ChooseSegmentMapCodingMethod when it runs in
	// collectVP9EncodeFrameCounts.
	for i := range vp9dec.SegTreeProbs {
		seg.TreeProbs[i] = vp9dec.MaxProb
	}
	for i := range vp9dec.PredictionProbs {
		seg.PredProbs[i] = vp9dec.MaxProb
	}
	// Compute deltas if the libvpx setup path didn't already populate
	// them — keeps backwards compatibility with the prepareFrame-only
	// caller path used today.
	if cr.qindexDelta[1] == 0 && cr.qindexDelta[2] == 0 {
		ratio := cr.rateRatioQdelta
		if ratio <= 0 {
			ratio = 2.0
		}
		cr.qindexDelta[1] = cr.vp9CyclicRefreshComputeDeltaq(baseQIndex, ratio, false)
		ratio2 := 0.1 * float64(cr.rateBoostFac) * ratio
		if ratio2 > vp9CyclicRefreshMaxRateTargetRatio {
			ratio2 = vp9CyclicRefreshMaxRateTargetRatio
		}
		if ratio2 <= 0 {
			ratio2 = ratio * 1.5
		}
		cr.qindexDelta[2] = cr.vp9CyclicRefreshComputeDeltaq(baseQIndex, ratio2, false)
	}
	delta1 := clampInt(cr.qindexDelta[1], -255, 255)
	delta2 := clampInt(cr.qindexDelta[2], -255, 255)
	if delta1 != 0 {
		seg.FeatureMask[vp9CyclicRefreshSegmentBoost1] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[vp9CyclicRefreshSegmentBoost1][vp9dec.SegLvlAltQ] = int16(delta1)
	}
	if delta2 != 0 {
		seg.FeatureMask[vp9CyclicRefreshSegmentBoost2] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[vp9CyclicRefreshSegmentBoost2][vp9dec.SegLvlAltQ] = int16(delta2)
	}
	return seg
}

func absInt16(v int16) int16 {
	if v < 0 {
		return -v
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
