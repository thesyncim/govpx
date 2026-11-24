package encoder

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vpx/arith"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
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
	CyclicRefreshSegmentBase = 0
	// CR_SEGMENT_ID_BOOST1 — base refresh segment.
	// libvpx: vp9_aq_cyclicrefresh.h:26
	CyclicRefreshSegmentBoost1 = 1
	// CR_SEGMENT_ID_BOOST2 — more aggressive refresh segment.
	// libvpx: vp9_aq_cyclicrefresh.h:27
	CyclicRefreshSegmentBoost2 = 2
	// CR_MAX_RATE_TARGET_RATIO clamps segment BOOST2's rate target ratio.
	// libvpx: vp9_aq_cyclicrefresh.h:30
	CyclicRefreshMaxRateTargetRatio = 4.0
	// MI_BLOCK_SIZE — superblock size in 8x8 mi units.
	// libvpx: vp9/common/vp9_onyxc_int.h MI_BLOCK_SIZE = 8
	CyclicRefreshSuperblockMI = 8
	cyclicRefreshMaxInt64     = int64(1<<63 - 1)
)

// CyclicRefreshSegmentIDBoosted reports whether segmentID is one of libvpx's
// cyclic-refresh boosted segments.
func CyclicRefreshSegmentIDBoosted(segmentID uint8) bool {
	return segmentID == CyclicRefreshSegmentBoost1 ||
		segmentID == CyclicRefreshSegmentBoost2
}

func cyclicRefreshCandidateSegment(cr *CyclicRefreshState,
	args CyclicRefreshUpdateSegmentArgs,
) uint8 {
	largeMotion := args.MvRow > cr.MotionThresh || args.MvRow < -cr.MotionThresh ||
		args.MvCol > cr.MotionThresh || args.MvCol < -cr.MotionThresh
	distTooHigh := false
	if args.Dist > uint64(cyclicRefreshMaxInt64) {
		distTooHigh = true
	} else {
		distTooHigh = int64(args.Dist) > cr.ThreshDistSB
	}
	if distTooHigh && (largeMotion || !args.IsInter) {
		return CyclicRefreshSegmentBase
	}
	if args.BSize >= common.Block16x16 && int64(args.Rate) < cr.ThreshRateSB &&
		args.IsInter && args.MvRow == 0 && args.MvCol == 0 && cr.RateBoostFac > 10 {
		return CyclicRefreshSegmentBoost2
	}
	return CyclicRefreshSegmentBoost1
}

// CyclicRefreshState mirrors libvpx's struct CYCLIC_REFRESH from
// vp9_aq_cyclicrefresh.h:32-74 in field-for-field order so callers
// can compare against the C oracle one-to-one.
type CyclicRefreshState struct {
	// Enabled latches the AQ mode at Configure time. Mirrors
	// cpi->oxcf.aq_mode == CYCLIC_REFRESH_AQ.
	Enabled bool

	// percent_refresh — target fraction of (8x8) blocks per frame.
	// libvpx: vp9_aq_cyclicrefresh.h:35
	PercentRefresh int
	// max_qdelta_perc — cap on q-delta as % of base q.
	// libvpx: vp9_aq_cyclicrefresh.h:37
	MaxQDeltaPerc int
	// sb_index — rotating superblock pointer through the frame.
	// libvpx: vp9_aq_cyclicrefresh.h:39
	SBIndex int
	// time_for_refresh — extra cycle-wait for refreshed blocks.
	// libvpx: vp9_aq_cyclicrefresh.h:43
	TimeForRefresh int
	// target_num_seg_blocks — blocks slated for delta-q this frame.
	// libvpx: vp9_aq_cyclicrefresh.h:45
	TargetNumSegBlocks int
	// actual_num_seg{1,2}_blocks — refreshed-this-frame buckets.
	// libvpx: vp9_aq_cyclicrefresh.h:47-48
	ActualNumSeg1Blocks int
	ActualNumSeg2Blocks int
	// RDMult — RD multiplier for segment 1.
	// libvpx: vp9_aq_cyclicrefresh.h:50
	RDMult int
	// map — per-(8x8) refresh state (signed char in libvpx).
	// libvpx: vp9_aq_cyclicrefresh.h:52
	RefreshMap []int8
	// last_coded_q_map — last q a block was coded at.
	// libvpx: vp9_aq_cyclicrefresh.h:54
	LastCodedQMap []uint8
	// thresh_rate_sb / thresh_dist_sb — per-SB projected rate/dist thresholds.
	// libvpx: vp9_aq_cyclicrefresh.h:57-58
	ThreshRateSB int64
	ThreshDistSB int64
	// motion_thresh — MV magnitude cap (1/8 pel units).
	// libvpx: vp9_aq_cyclicrefresh.h:61
	MotionThresh int16
	// rate_ratio_qdelta — rate ratio target driving compute_deltaq().
	// libvpx: vp9_aq_cyclicrefresh.h:63
	RateRatioQDelta float64
	// rate_boost_fac — boost factor for segment BOOST2.
	// libvpx: vp9_aq_cyclicrefresh.h:65
	RateBoostFac int
	// low_content_avg — running average of low-motion frame fraction.
	// libvpx: vp9_aq_cyclicrefresh.h:66
	LowContentAvg float64
	// qindex_delta[3] — per-segment q deltas. Index 0 unused, 1/2 active.
	// libvpx: vp9_aq_cyclicrefresh.h:67
	QIndexDelta [3]int
	// reduce_refresh — drop percent_refresh to 5 when set.
	// libvpx: vp9_aq_cyclicrefresh.h:68
	ReduceRefresh bool
	// weight_segment — segment weight average for rc_bits_per_mb().
	// libvpx: vp9_aq_cyclicrefresh.h:69
	WeightSegment float64
	// apply_cyclic_refresh — per-frame gate from update_parameters().
	// libvpx: vp9_aq_cyclicrefresh.h:70
	ApplyCyclicRefresh bool
	// counter_encode_maxq_scene_change — high-Q scene-change counter.
	// libvpx: vp9_aq_cyclicrefresh.h:71
	CounterEncodeMaxqSceneChange int
	// skip_flat_static_blocks — screen-content flat-block skip.
	// libvpx: vp9_aq_cyclicrefresh.h:72
	SkipFlatStaticBlocks bool
	// content_mode — content classification flag (default 1).
	// libvpx: vp9_aq_cyclicrefresh.h:73
	ContentMode bool

	// segmentation_map — exposed segment id grid the encoder consults
	// per (mi_row, mi_col). Mirrors libvpx's cpi->segmentation_map for
	// the lifetime of the cyclic-refresh state.
	SegMap []uint8

	// ConsecZeroMV — per-(mi_row, mi_col) saturating counter of
	// consecutive frames the LAST-frame MV at this 8x8 block was
	// near-zero (|mv.row| < 8 && |mv.col| < 8). Mirrors
	// cpi->consec_zero_mv from libvpx's VP9_COMP
	// (vp9/encoder/vp9_h:838) — co-located here because the
	// cyclic refresh setup path is the only consumer. Updated per
	// encoded SB by UpdateZeroMVCnt, mirroring
	// libvpx's update_zeromv_cnt (vp9_encodeframe.c:5999-6022). The
	// counter feeds the eligibility filter in
	// cyclic_refresh_update_map (vp9_aq_cyclicrefresh.c:437-442).
	ConsecZeroMV []uint8

	// MIRows / MICols pin the current frame's mi-grid dims.
	MIRows int
	MICols int

	// Apply tracks whether this frame's segmentation has been built and
	// should be honoured by the segment-id lookup path.
	Apply bool
}

// Alloc mirrors libvpx vp9_cyclic_refresh_alloc()
// from vp9_aq_cyclicrefresh.c:32-53. Resets last_coded_q_map to MAXQ
// (255) and seeds counter_encode_maxq_scene_change/content_mode.
func (cr *CyclicRefreshState) Alloc(miRows, miCols int) {
	n := miRows * miCols
	if n <= 0 {
		cr.RefreshMap = nil
		cr.LastCodedQMap = nil
		cr.SegMap = nil
		cr.ConsecZeroMV = nil
		cr.MIRows = 0
		cr.MICols = 0
		return
	}
	cr.RefreshMap = buffers.EnsureLenZeroed(cr.RefreshMap, n)
	cr.LastCodedQMap = buffers.EnsureLen(cr.LastCodedQMap, n)
	// libvpx: vp9_aq_cyclicrefresh.c:49 — memset to MAXQ.
	for i := range cr.LastCodedQMap {
		cr.LastCodedQMap[i] = vp9dec.MaxQ
	}
	cr.SegMap = buffers.EnsureLenZeroed(cr.SegMap, n)
	// libvpx: vp9_c:2180-2183 — vpx_calloc(consec_zero_mv).
	cr.ConsecZeroMV = buffers.EnsureLenZeroed(cr.ConsecZeroMV, n)
	cr.MIRows = miRows
	cr.MICols = miCols
	// libvpx: vp9_aq_cyclicrefresh.c:50-51.
	cr.CounterEncodeMaxqSceneChange = 0
	cr.ContentMode = true
}

// Configure latches the AQ mode and re-allocates internal maps. Called
// by the encoder when the AQ option or resolution changes.
func (cr *CyclicRefreshState) Configure(enabled bool, width, height int) {
	cr.Enabled = enabled
	cr.Apply = false
	if !enabled || width <= 0 || height <= 0 {
		cr.RefreshMap = nil
		cr.LastCodedQMap = nil
		cr.SegMap = nil
		cr.ConsecZeroMV = nil
		cr.MIRows = 0
		cr.MICols = 0
		cr.SBIndex = 0
		return
	}
	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	cr.Alloc(miRows, miCols)
	sbCount := CyclicRefreshSuperblockCount(miRows, miCols)
	if sbCount > 0 && cr.SBIndex >= sbCount {
		cr.SBIndex = 0
	}
}

// ResetResize mirrors vp9_cyclic_refresh_reset_resize()
// from vp9_aq_cyclicrefresh.c:686-696. Zeros the refresh map, resets
// last_coded_q_map to MAXQ, parks sb_index at 0, and zeros the
// scene-change counter.
func (cr *CyclicRefreshState) ResetResize() {
	for i := range cr.RefreshMap {
		cr.RefreshMap[i] = 0
	}
	for i := range cr.LastCodedQMap {
		cr.LastCodedQMap[i] = vp9dec.MaxQ
	}
	// libvpx: vp9_c:4103-4106 — resize_pending zeroes consec_zero_mv.
	for i := range cr.ConsecZeroMV {
		cr.ConsecZeroMV[i] = 0
	}
	cr.SBIndex = 0
	cr.CounterEncodeMaxqSceneChange = 0
}

// ComputeDeltaQ mirrors compute_deltaq() from
// vp9_aq_cyclicrefresh.c:90-99. Translates a rate-ratio target into a
// qindex delta, clamped to -max_qdelta_perc * q / 100.
// RCBitsPerMB mirrors vp9_cyclic_refresh_rc_bits_per_mb
// (vp9_aq_cyclicrefresh.c:137-156). It blends base and boosted segment
// bits-per-macroblock using weight_segment for vp9_rc_regulate_q.
func (cr *CyclicRefreshState) RCBitsPerMB(qindex int, intraOnly bool, encodeSpeed int, correctionFactor float64) int {
	if cr == nil {
		return BitsPerMB(intraOnly, qindex, correctionFactor)
	}
	if qindex < 0 {
		qindex = 0
	} else if qindex > vp9dec.MaxQ {
		qindex = vp9dec.MaxQ
	}
	var deltaq int
	if encodeSpeed < 8 {
		deltaq = cr.ComputeDeltaQ(qindex, cr.RateRatioQDelta, intraOnly)
	} else {
		deltaq = -(cr.MaxQDeltaPerc * qindex) / 200
	}
	boostedQ := qindex + deltaq
	if boostedQ < 0 {
		boostedQ = 0
	} else if boostedQ > vp9dec.MaxQ {
		boostedQ = vp9dec.MaxQ
	}
	base := float64(BitsPerMB(intraOnly, qindex, correctionFactor))
	boosted := float64(BitsPerMB(intraOnly, boostedQ, correctionFactor))
	w := cr.WeightSegment
	if w < 0 {
		w = 0
	} else if w > 1 {
		w = 1
	}
	return int(math.Round((1-w)*base + w*boosted))
}

func (cr *CyclicRefreshState) ComputeDeltaQ(q int, rateFactor float64, intraFrame bool) int {
	// libvpx: vp9_aq_cyclicrefresh.c:93 — vp9_compute_qdelta_by_rate(rc, frame_type, q, rate_factor, bit_depth).
	deltaq := CyclicRefreshComputeQDeltaByRate(q, rateFactor, intraFrame)
	// libvpx: vp9_aq_cyclicrefresh.c:95-97 — clamp -deltaq to max_qdelta_perc.
	if -deltaq > cr.MaxQDeltaPerc*q/100 {
		deltaq = -cr.MaxQDeltaPerc * q / 100
	}
	return deltaq
}

// CyclicRefreshComputeQDeltaByRate matches libvpx's
// vp9_compute_qdelta_by_rate (vp9_ratectrl.c:2573-2595): for best=0,
// worst=MAXQ, finds the smallest qindex whose projected bits-per-mb is
// <= rate_target_ratio * base_bits_per_mb.
func CyclicRefreshComputeQDeltaByRate(qindex int, rateTargetRatio float64, intraFrame bool) int {
	if qindex < 0 {
		qindex = 0
	} else if qindex > vp9dec.MaxQ {
		qindex = vp9dec.MaxQ
	}
	// libvpx: vp9_ratectrl.c:2580-2581 — base_bits_per_mb.
	baseBitsPerMB := BitsPerMB(intraFrame, qindex, 1.0)
	// libvpx: vp9_ratectrl.c:2584 — target_bits_per_mb = ratio * base.
	targetBitsPerMB := int(rateTargetRatio * float64(baseBitsPerMB))
	targetIndex := vp9dec.MaxQ
	// libvpx: vp9_ratectrl.c:2587-2593.
	for i := range vp9dec.MaxQ {
		if BitsPerMB(intraFrame, i, 1.0) <= targetBitsPerMB {
			targetIndex = i
			break
		}
	}
	return targetIndex - qindex
}

// UpdateMap mirrors cyclic_refresh_update_map() from
// vp9_aq_cyclicrefresh.c:364-476. Walks superblocks starting at sb_index,
// flips eligible 8x8 blocks (RefreshMap == 0) to BOOST1 until
// target_num_seg_blocks blocks are queued or the cycle completes.
//
// This is the deterministic baseline cycling pass; libvpx layers an
// extra last_coded_q_map / consec_zero_mv filter on top via
// qindex_thresh / consec_zero_mv_thresh, which govpx exposes via
// the optional eligibility arguments below.
func (cr *CyclicRefreshState) UpdateMap(consecZeroMV []uint8, qindexThresh int, consecZeroMvThresh int, screenContent bool) {
	miRows, miCols := cr.MIRows, cr.MICols
	if miRows <= 0 || miCols <= 0 {
		return
	}
	// libvpx: vp9_aq_cyclicrefresh.c:374 — memset seg_map BASE.
	for i := range cr.SegMap {
		cr.SegMap[i] = CyclicRefreshSegmentBase
	}
	const blockMi = CyclicRefreshSuperblockMI
	sbCols := (miCols + blockMi - 1) / blockMi
	sbRows := (miRows + blockMi - 1) / blockMi
	sbsInFrame := sbCols * sbRows
	if sbsInFrame <= 0 {
		cr.TargetNumSegBlocks = 0
		return
	}
	// libvpx: vp9_aq_cyclicrefresh.c:379 — block_count = percent_refresh * mi_rows * mi_cols / 100.
	blockCount := cr.PercentRefresh * miRows * miCols / 100
	// libvpx: vp9_aq_cyclicrefresh.c:384.
	if cr.SBIndex >= sbsInFrame {
		cr.SBIndex = 0
	}
	i := cr.SBIndex
	cr.TargetNumSegBlocks = 0
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
				if cr.RefreshMap[blIndex2] == 0 {
					countTot++
					// libvpx: vp9_aq_cyclicrefresh.c:437-442 — eligibility filter.
					eligible := true
					if cr.ContentMode && consecZeroMV != nil && len(consecZeroMV) > blIndex2 && len(cr.LastCodedQMap) > blIndex2 {
						if int(cr.LastCodedQMap[blIndex2]) > qindexThresh ||
							int(consecZeroMV[blIndex2]) < consecZeroMvThresh {
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
				} else if cr.RefreshMap[blIndex2] < 0 {
					// libvpx: vp9_aq_cyclicrefresh.c:443-445 — increment cooldown counters.
					cr.RefreshMap[blIndex2]++
				}
			}
		}
		// libvpx: vp9_aq_cyclicrefresh.c:450 — half-superblock heuristic.
		if sumMap >= xmis*ymis/2 {
			for y := range ymis {
				for x := range xmis {
					cr.SegMap[blIndex+y*miCols+x] = CyclicRefreshSegmentBoost1
				}
			}
			cr.TargetNumSegBlocks += xmis * ymis
		}
		i++
		if i == sbsInFrame {
			i = 0
		}
		if cr.TargetNumSegBlocks >= blockCount || i == cr.SBIndex {
			break
		}
	}
	cr.SBIndex = i
	// libvpx: vp9_aq_cyclicrefresh.c:473-475 — reduce_refresh gate.
	cr.ReduceRefresh = false
	if !screenContent && countSel < (3*countTot)>>2 {
		cr.ReduceRefresh = true
	}
}

// UpdateParameters mirrors
// vp9_cyclic_refresh_update_parameters() from vp9_aq_cyclicrefresh.c:479-593.
// Decides apply_cyclic_refresh + sets per-frame
// percent_refresh / rate_ratio_qdelta / motion_thresh.
func (cr *CyclicRefreshState) UpdateParameters(args CyclicRefreshUpdateParametersArgs) {
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
	cr.ApplyCyclicRefresh = true
	// libvpx: vp9_aq_cyclicrefresh.c:492-505 — disable gates.
	if args.FrameIsIntraOnly || args.TemporalLayerID > 0 ||
		args.Lossless ||
		args.AvgFrameQindexInter < qpThresh ||
		(!args.UseSVC && cr.ContentMode &&
			args.AvgFrameLowMotion < thresholdLowMotion &&
			args.FramesSinceKey > 40) ||
		(!args.UseSVC && args.AvgFrameQindexInter > qpMaxThresh &&
			args.FramesSinceKey > 20) {
		cr.ApplyCyclicRefresh = false
		return
	}
	// libvpx: vp9_aq_cyclicrefresh.c:507-512.
	cr.PercentRefresh = 10
	if cr.ReduceRefresh {
		cr.PercentRefresh = 5
	}
	cr.MaxQDeltaPerc = 60
	cr.TimeForRefresh = 0
	cr.MotionThresh = 32
	cr.RateBoostFac = 15
	// libvpx: vp9_aq_cyclicrefresh.c:516-528 — boosted ratio after key.
	numTemporalLayers := args.NumberTemporalLayers
	if numTemporalLayers <= 0 {
		numTemporalLayers = 1
	}
	if cr.PercentRefresh > 0 &&
		args.FramesSinceKey < (4*numTemporalLayers)*(100/cr.PercentRefresh) {
		cr.RateRatioQDelta = 3.0
	} else {
		cr.RateRatioQDelta = 2.0
		if cr.ContentMode && args.NoiseLevelMedium {
			cr.RateRatioQDelta = 1.7
			cr.RateBoostFac = 13
		}
	}
	// libvpx: vp9_aq_cyclicrefresh.c:532-544 — screen-content tweaks.
	if args.ScreenContent {
		if args.SpatialLayerID == args.NumberSpatialLayers-1 {
			cr.SkipFlatStaticBlocks = true
		}
		if cr.SkipFlatStaticBlocks {
			cr.PercentRefresh = 5
		} else {
			cr.PercentRefresh = 10
		}
		if cr.ContentMode && cr.CounterEncodeMaxqSceneChange < 30 {
			if cr.SkipFlatStaticBlocks {
				cr.PercentRefresh = 10
			} else {
				cr.PercentRefresh = 15
			}
		}
		cr.RateRatioQDelta = 2.0
		cr.RateBoostFac = 10
	}
	// libvpx: vp9_aq_cyclicrefresh.c:546-554 — low-resolution tweaks.
	if args.Width*args.Height <= 352*288 {
		if args.AvgFrameBandwidth < 3000 {
			cr.MotionThresh = 64
			cr.RateBoostFac = 13
		} else {
			cr.MaxQDeltaPerc = 70
			if cr.RateRatioQDelta < 2.5 {
				cr.RateRatioQDelta = 2.5
			}
		}
	}
	// libvpx: vp9_aq_cyclicrefresh.c:555-566 — VBR tweaks.
	if args.RateControlIsVBR {
		cr.PercentRefresh = 10
		cr.RateRatioQDelta = 1.5
		cr.RateBoostFac = 10
		if args.RefreshGoldenFrame && !args.UseSVC {
			cr.PercentRefresh = 0
			cr.RateRatioQDelta = 1.0
		}
	}
	// libvpx: vp9_aq_cyclicrefresh.c:571-578 — segment-weight average.
	targetRefresh := cr.PercentRefresh * cr.MIRows * cr.MICols / 100
	weightSegmentTarget := float64(targetRefresh) / float64(num8x8bl)
	weightSegment := float64((targetRefresh+cr.ActualNumSeg1Blocks+
		cr.ActualNumSeg2Blocks)>>1) / float64(num8x8bl)
	if weightSegmentTarget < 7*weightSegment/8 {
		weightSegment = weightSegmentTarget
	}
	if args.ScreenContent {
		weightSegment = float64(cr.ActualNumSeg1Blocks+cr.ActualNumSeg2Blocks) /
			float64(num8x8bl)
	}
	cr.WeightSegment = weightSegment
	// libvpx: vp9_aq_cyclicrefresh.c:587-592 — content_mode=0 fallback.
	if !cr.ContentMode {
		cr.ActualNumSeg1Blocks = cr.PercentRefresh * cr.MIRows * cr.MICols / 100
		cr.ActualNumSeg2Blocks = 0
		cr.WeightSegment = float64(cr.ActualNumSeg1Blocks) / float64(num8x8bl)
	}
}

// CyclicRefreshUpdateParametersArgs bundles the libvpx update_parameters()
// inputs the encoder threads down from VP9_COMP / RATE_CONTROL.
type CyclicRefreshUpdateParametersArgs struct {
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

// Setup mirrors vp9_cyclic_refresh_setup() from
// vp9_aq_cyclicrefresh.c:596-680. Decides whether to emit
// segmentation, computes qindex_delta[1]/[2], and rebuilds the
// segmentation map.
func (cr *CyclicRefreshState) Setup(args CyclicRefreshSetupArgs) {
	if cr.MIRows <= 0 || cr.MICols <= 0 {
		cr.Apply = false
		return
	}
	// libvpx: vp9_aq_cyclicrefresh.c:604.
	if args.CurrentVideoFrame == 0 {
		cr.LowContentAvg = 0.0
	}
	// libvpx: vp9_aq_cyclicrefresh.c:606-607.
	if args.ResizePending && args.TemporalLayerID == 0 {
		cr.ResetResize()
	}
	sceneChange := args.HighSourceSad
	// libvpx: vp9_aq_cyclicrefresh.c:608-622 — disable-segmentation path.
	if !cr.ApplyCyclicRefresh || args.ForceUpdateSegmentation || sceneChange {
		for i := range cr.SegMap {
			cr.SegMap[i] = 0
		}
		if (args.FrameIsKey || sceneChange) && args.TemporalLayerID == 0 {
			for i := range cr.LastCodedQMap {
				cr.LastCodedQMap[i] = vp9dec.MaxQ
			}
			cr.SBIndex = 0
			cr.ReduceRefresh = false
			cr.CounterEncodeMaxqSceneChange = 0
		}
		cr.Apply = false
		return
	}
	cr.CounterEncodeMaxqSceneChange++
	// libvpx: vp9_aq_cyclicrefresh.c:631 — thresh_rate_sb.
	cr.ThreshRateSB = (int64(args.Sb64TargetRate) << 8) << 2
	// libvpx: vp9_aq_cyclicrefresh.c:635 — thresh_dist_sb.
	q := ConvertQIndexToQ(args.BaseQindex)
	cr.ThreshDistSB = int64(q*q) << 2
	// libvpx: vp9_aq_cyclicrefresh.c:659 — compute_deltaq for BOOST1.
	cr.QIndexDelta[0] = 0
	cr.QIndexDelta[1] = cr.ComputeDeltaQ(args.BaseQindex, cr.RateRatioQDelta, args.FrameIsIntraOnly)
	// libvpx: vp9_aq_cyclicrefresh.c:665 — RDMult.
	//   cr->RDMult = vp9_compute_rd_mult(cpi, qindex2);
	// The frame-type bucket follows the libvpx branching in
	// vp9_compute_rd_mult_based_on_qindex: KF wins, then ARF/GF when
	// refreshing, else inter.  CR runs after the encoder has resolved
	// refresh flags, so we recompute the same bucket here.
	qindex2 := arith.ClampInt(args.BaseQindex+args.YDcDeltaQ+cr.QIndexDelta[1], 0, vp9dec.MaxQ)
	frameType := RDFrameTypeFor(args.FrameIsKey, args.IsSrcFrameAltRef,
		args.RefreshGoldenFrame, args.RefreshAltRefFrame)
	cr.RDMult = ComputeRDMult(qindex2, frameType)
	// libvpx: vp9_aq_cyclicrefresh.c:669-674 — BOOST2 delta.
	ratio := 0.1 * float64(cr.RateBoostFac) * cr.RateRatioQDelta
	if ratio > CyclicRefreshMaxRateTargetRatio {
		ratio = CyclicRefreshMaxRateTargetRatio
	}
	cr.QIndexDelta[2] = cr.ComputeDeltaQ(args.BaseQindex, ratio, args.FrameIsIntraOnly)
	// libvpx: vp9_aq_cyclicrefresh.c:678.
	consecZeroMvThresh := 0
	if !args.ScreenContent {
		consecZeroMvThresh = 100
	}
	qindexThresh := args.BaseQindex + cr.QIndexDelta[1]
	if args.ScreenContent {
		qindexThresh = args.BaseQindex + cr.QIndexDelta[2]
	}
	if cr.ContentMode && args.NoiseLevelMedium {
		consecZeroMvThresh = 60
		if qindexThresh < args.BaseQindex {
			qindexThresh = args.BaseQindex
		}
	}
	cr.UpdateMap(args.ConsecZeroMv, qindexThresh, consecZeroMvThresh, args.ScreenContent)
	cr.Apply = true
}

// CyclicRefreshSetupArgs bundles the libvpx setup() inputs from
// VP9_COMP / RATE_CONTROL / VP9_COMMON.
type CyclicRefreshSetupArgs struct {
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

// Postencode mirrors vp9_cyclic_refresh_postencode()
// from vp9_aq_cyclicrefresh.c:261-317. Counts actual segment 1/2 blocks,
// accumulates low_content_avg, and gates golden-frame refresh.
func (cr *CyclicRefreshState) Postencode(args CyclicRefreshPostencodeArgs) CyclicRefreshPostencodeResult {
	cr.ActualNumSeg1Blocks = 0
	cr.ActualNumSeg2Blocks = 0
	miRows, miCols := cr.MIRows, cr.MICols
	lowContentFrame := 0
	// libvpx: vp9_aq_cyclicrefresh.c:273-288.
	for mr := range miRows {
		for mc := range miCols {
			idx := mr*miCols + mc
			if idx < len(cr.SegMap) {
				switch cr.SegMap[idx] {
				case CyclicRefreshSegmentBoost1:
					cr.ActualNumSeg1Blocks++
				case CyclicRefreshSegmentBoost2:
					cr.ActualNumSeg2Blocks++
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
	res := CyclicRefreshPostencodeResult{}
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
		cr.LowContentAvg = (fractionLow + 3*cr.LowContentAvg) / 4
		// libvpx: vp9_aq_cyclicrefresh.c:305-315 — reject golden if low-content too small.
		if !res.ForceGoldenRefresh && args.RefreshGoldenFrame &&
			args.FramesSinceKey > args.FramesSinceGolden+1 {
			if fractionLow < 0.65 || cr.LowContentAvg < 0.6 {
				res.ClearRefreshGolden = true
			}
			cr.LowContentAvg = fractionLow
		}
	}
	return res
}

type CyclicRefreshPostencodeArgs struct {
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

type CyclicRefreshPostencodeResult struct {
	SetGoldenUpdate    bool
	ForceGoldenRefresh bool
	ClearRefreshGolden bool
}

type CyclicRefreshResolvedSegment struct {
	SegmentID       uint8
	RefreshMapValue int8
	BlockIndex      int
	XMis            int
	YMis            int
}

// ResolveSegment runs the segment-id decision from
// vp9_cyclic_refresh_update_segment without mutating the refresh or
// segmentation maps. Encoders use this during count-only walks so the
// measured segment-map coding costs see the same segment IDs the real
// encode pass will commit.
func (cr *CyclicRefreshState) ResolveSegment(
	args CyclicRefreshUpdateSegmentArgs,
) (CyclicRefreshResolvedSegment, bool) {
	if cr.MIRows <= 0 || cr.MICols <= 0 || args.MIRow < 0 || args.MICol < 0 ||
		args.MIRow >= cr.MIRows || args.MICol >= cr.MICols ||
		args.BSize < common.Block4x4 || args.BSize >= common.BlockSizes {
		return CyclicRefreshResolvedSegment{}, false
	}
	xmis := min(cr.MICols-args.MICol, int(common.Num8x8BlocksWideLookup[args.BSize]))
	ymis := min(cr.MIRows-args.MIRow, int(common.Num8x8BlocksHighLookup[args.BSize]))
	if xmis <= 0 || ymis <= 0 {
		return CyclicRefreshResolvedSegment{}, false
	}

	blockIndex := args.MIRow*cr.MICols + args.MICol
	if blockIndex < 0 || blockIndex >= len(cr.RefreshMap) ||
		blockIndex >= len(cr.SegMap) {
		return CyclicRefreshResolvedSegment{}, false
	}
	refreshThisBlock := cyclicRefreshCandidateSegment(cr, args)
	if args.RateControlIsVBR && args.RefFrame == vp9dec.GoldenFrame {
		refreshThisBlock = CyclicRefreshSegmentBase
	}

	segmentID := args.SegmentID
	if args.UseNonrdPickMode && CyclicRefreshSegmentIDBoosted(segmentID) {
		segmentID = refreshThisBlock
		if args.Skip {
			segmentID = CyclicRefreshSegmentBase
		}
	}

	newMapValue := cr.RefreshMap[blockIndex]
	if CyclicRefreshSegmentIDBoosted(segmentID) {
		newMapValue = int8(-cr.TimeForRefresh)
	} else if refreshThisBlock != CyclicRefreshSegmentBase {
		if cr.RefreshMap[blockIndex] == 1 {
			newMapValue = 0
		}
	} else {
		newMapValue = 1
	}

	return CyclicRefreshResolvedSegment{
		SegmentID:       segmentID,
		RefreshMapValue: newMapValue,
		BlockIndex:      blockIndex,
		XMis:            xmis,
		YMis:            ymis,
	}, true
}

// UpdateSegment mirrors vp9_cyclic_refresh_update_segment() from
// vp9_aq_cyclicrefresh.c:161-223. The mode picker initially labels a block
// from the prepared cyclic map; after the mode decision is known, libvpx may
// reset that provisional boosted segment to BASE, promote a cheap zero-motion
// inter block to BOOST2, or leave it as BOOST1. The realized segment is written
// back to both the current segmentation map and the cyclic refresh map.
func (cr *CyclicRefreshState) UpdateSegment(args CyclicRefreshUpdateSegmentArgs) uint8 {
	resolved, ok := cr.ResolveSegment(args)
	if !ok {
		return args.SegmentID
	}

	for y := range resolved.YMis {
		row := resolved.BlockIndex + y*cr.MICols
		for x := range resolved.XMis {
			off := row + x
			if off >= 0 && off < len(cr.RefreshMap) {
				cr.RefreshMap[off] = resolved.RefreshMapValue
			}
			if off >= 0 && off < len(cr.SegMap) {
				cr.SegMap[off] = resolved.SegmentID
			}
		}
	}
	return resolved.SegmentID
}

type CyclicRefreshUpdateSegmentArgs struct {
	MIRow            int
	MICol            int
	BSize            common.BlockSize
	SegmentID        uint8
	RefFrame         int8
	MvRow            int16
	MvCol            int16
	Rate             int
	Dist             uint64
	IsInter          bool
	Skip             bool
	UseNonrdPickMode bool
	RateControlIsVBR bool
}

// SetGoldenUpdate mirrors
// vp9_cyclic_refresh_set_golden_update() from vp9_aq_cyclicrefresh.c:320-334.
// Returns the baseline_gf_interval value the rate controller should
// install.
func (cr *CyclicRefreshState) SetGoldenUpdate(args CyclicRefreshSetGoldenUpdateArgs) int {
	var baseline int
	if cr.PercentRefresh > 0 {
		baseline = min(4*(100/cr.PercentRefresh), 40)
	} else {
		baseline = 40
	}
	if args.RateControlIsVBR {
		baseline = 20
	}
	// libvpx: vp9_aq_cyclicrefresh.c:331-333.
	if args.AvgFrameLowMotion < 50 && args.FramesSinceKey > 40 && cr.ContentMode {
		baseline = 10
	}
	return baseline
}

type CyclicRefreshSetGoldenUpdateArgs struct {
	RateControlIsVBR  bool
	AvgFrameLowMotion int
	FramesSinceKey    int
}

// LimitQ mirrors vp9_cyclic_refresh_limit_q() from
// vp9_aq_cyclicrefresh.c:698-705. Applies a -8 frame-level q step
// limit when percent_refresh > 0.
func (cr *CyclicRefreshState) LimitQ(q1Frame int, q *int) {
	if q == nil {
		return
	}
	if cr.PercentRefresh > 0 && q1Frame-*q > 8 {
		*q = q1Frame - 8
	}
}

// UpdateSegmentPostencode mirrors
// vp9_cyclic_refresh_update_sb_postencode() from
// vp9_aq_cyclicrefresh.c:225-255. Updates last_coded_q_map for the
// encoded SB.
func (cr *CyclicRefreshState) UpdateSegmentPostencode(miRow, miCol, bw, bh int, baseQindex int, segID uint8, isInter, skip bool) {
	if cr.MIRows <= 0 || cr.MICols <= 0 {
		return
	}
	xmis := min(cr.MICols-miCol, bw)
	ymis := min(cr.MIRows-miRow, bh)
	blIndex := miRow*cr.MICols + miCol
	if segID > CyclicRefreshSegmentBoost2 {
		return
	}
	q := arith.ClampInt(baseQindex+cr.QIndexDelta[segID], 0, vp9dec.MaxQ)
	for y := range ymis {
		for x := range xmis {
			off := blIndex + y*cr.MICols + x
			if off >= len(cr.LastCodedQMap) {
				continue
			}
			if !isInter || !skip {
				cr.LastCodedQMap[off] = uint8(q)
			} else {
				if uint8(q) < cr.LastCodedQMap[off] {
					cr.LastCodedQMap[off] = uint8(q)
				}
			}
		}
	}
}

// UpdateZeroMVCnt mirrors update_zeromv_cnt() from
// vp9/encoder/vp9_encodeframe.c:5999-6022. For every encoded SB whose
// chosen leaf references LAST_FRAME and is inter (and the leaf is in
// a refresh-tracked segment), bumps consec_zero_mv up by one when the
// chosen MV magnitude is < 8 in both dimensions, and resets to zero
// otherwise. The counter is saturating at 255. Called from the per-SB
// encode hook so the next frame's update_map filter sees the correct
// per-block stationarity history.
func (cr *CyclicRefreshState) UpdateZeroMVCnt(
	miRow, miCol, bw, bh int, mvRow, mvCol int16, refFrame int8,
	isInter bool, segID uint8,
) {
	if cr.MIRows <= 0 || cr.MICols <= 0 || len(cr.ConsecZeroMV) == 0 {
		return
	}
	// libvpx: vp9_encodeframe.c:6012 — gates on LAST_FRAME + inter +
	// segment_id <= CR_SEGMENT_ID_BOOST2.
	if !isInter || refFrame != vp9dec.LastFrame ||
		segID > CyclicRefreshSegmentBoost2 {
		return
	}
	xmis := min(cr.MICols-miCol, bw)
	ymis := min(cr.MIRows-miRow, bh)
	if xmis <= 0 || ymis <= 0 {
		return
	}
	blIndex := miRow*cr.MICols + miCol
	zero := absInt16(mvRow) < 8 && absInt16(mvCol) < 8
	for y := range ymis {
		for x := range xmis {
			off := blIndex + y*cr.MICols + x
			if off < 0 || off >= len(cr.ConsecZeroMV) {
				continue
			}
			if zero {
				// libvpx: vp9_encodeframe.c:6014-6016 — saturating bump.
				if cr.ConsecZeroMV[off] < 255 {
					cr.ConsecZeroMV[off]++
				}
			} else {
				// libvpx: vp9_encodeframe.c:6017-6019 — reset on large MV.
				cr.ConsecZeroMV[off] = 0
			}
		}
	}
}

// PrepareFrame is the encoder-facing entry point. Equivalent to libvpx's
// vp9_cyclic_refresh_update_parameters + vp9_cyclic_refresh_setup pair,
// called once per frame just before vp9_encode_frame. Maintains the
// existing govpx call surface.
func (cr *CyclicRefreshState) PrepareFrame(apply bool, miRows, miCols int) {
	cr.Apply = false
	if !cr.Enabled || !apply || miRows <= 0 || miCols <= 0 {
		return
	}
	// Re-alloc on mi-grid change.
	if cr.MIRows != miRows || cr.MICols != miCols || len(cr.SegMap) < miRows*miCols {
		cr.Alloc(miRows, miCols)
	}
	// Default per-frame params if update_parameters() was not threaded in.
	if cr.PercentRefresh == 0 {
		cr.PercentRefresh = 10
	}
	if cr.MaxQDeltaPerc == 0 {
		cr.MaxQDeltaPerc = 60
	}
	if cr.RateRatioQDelta == 0 {
		cr.RateRatioQDelta = 2.0
	}
	if cr.RateBoostFac == 0 {
		cr.RateBoostFac = 15
	}
	if cr.MotionThresh == 0 {
		cr.MotionThresh = 32
	}
	cr.ApplyCyclicRefresh = true
	cr.UpdateMap(nil, 0, 0, false)
	cr.Apply = cr.TargetNumSegBlocks > 0
}

func CyclicRefreshSuperblockCount(miRows, miCols int) int {
	if miRows <= 0 || miCols <= 0 {
		return 0
	}
	sbCols := (miCols + CyclicRefreshSuperblockMI - 1) / CyclicRefreshSuperblockMI
	sbRows := (miRows + CyclicRefreshSuperblockMI - 1) / CyclicRefreshSuperblockMI
	return sbRows * sbCols
}

func (cr *CyclicRefreshState) SegmentID(miRow, miCol int) uint8 {
	if !cr.Enabled || !cr.Apply || miRow < 0 || miCol < 0 ||
		miRow >= cr.MIRows || miCol >= cr.MICols {
		return 0
	}
	idx := miRow*cr.MICols + miCol
	if idx < 0 || idx >= len(cr.SegMap) {
		return 0
	}
	if cr.SegMap[idx] >= vp9dec.MaxSegments {
		return 0
	}
	return cr.SegMap[idx]
}

// SegmentationParams builds the SegmentationParams libvpx emits for
// cyclic-refresh frames. Mirrors the BOOST1/BOOST2 enable + alt-q
// assignment in vp9_aq_cyclicrefresh.c:639-675.
func (cr *CyclicRefreshState) SegmentationParams(baseQIndex int) vp9dec.SegmentationParams {
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
	// Compute deltas if the libvpx setup path did not already populate
	// them. The direct PrepareFrame-only path still needs segment deltas
	// before the root encoder asks for segmentation parameters.
	if cr.QIndexDelta[1] == 0 && cr.QIndexDelta[2] == 0 {
		ratio := cr.RateRatioQDelta
		if ratio <= 0 {
			ratio = 2.0
		}
		cr.QIndexDelta[1] = cr.ComputeDeltaQ(baseQIndex, ratio, false)
		ratio2 := 0.1 * float64(cr.RateBoostFac) * ratio
		if ratio2 > CyclicRefreshMaxRateTargetRatio {
			ratio2 = CyclicRefreshMaxRateTargetRatio
		}
		if ratio2 <= 0 {
			ratio2 = ratio * 1.5
		}
		cr.QIndexDelta[2] = cr.ComputeDeltaQ(baseQIndex, ratio2, false)
	}
	delta1 := arith.ClampInt(cr.QIndexDelta[1], -255, 255)
	delta2 := arith.ClampInt(cr.QIndexDelta[2], -255, 255)
	if delta1 != 0 {
		seg.FeatureMask[CyclicRefreshSegmentBoost1] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[CyclicRefreshSegmentBoost1][vp9dec.SegLvlAltQ] = int16(delta1)
	}
	if delta2 != 0 {
		seg.FeatureMask[CyclicRefreshSegmentBoost2] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[CyclicRefreshSegmentBoost2][vp9dec.SegLvlAltQ] = int16(delta2)
	}
	return seg
}

func absInt16(v int16) int16 {
	if v < 0 {
		return -v
	}
	return v
}
