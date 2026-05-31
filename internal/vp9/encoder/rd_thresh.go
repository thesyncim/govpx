package encoder

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// Rate-distortion mode-threshold state ported from libvpx v1.16.0
// (vp9/encoder/vp9_rd.{c,h} + vp9/encoder/vp9_pickmode.c). It implements
// the mode_rd_thresh / rd_less_than_thresh early-exit gate at libvpx
// vp9_pickmode.c:2240-2257 plus the supporting per-frame and per-tile state
// vp9_set_rd_speed_thresholds / set_block_thresholds / update_thresh_freq_fact.
//
// The state lives on VP9Encoder as a single non-MT tile (govpx single-tile
// realtime configuration) and is allocated lazily at first frame init. The
// allocation matches libvpx vp9_encodeframe.c:5415-5432 — fill
// thresh_freq_fact with RD_THRESH_INIT_FACT at tile birth — and the per-frame
// rebuild matches libvpx vp9_rd.c:355-385 set_block_thresholds.

// vp9MaxModes mirrors libvpx's MAX_MODES (vp9_rd.h:41).
const vp9MaxModes = 30

// vp9RDThreshInitFact / vp9RDThreshMaxFact / vp9RDThreshInc mirror libvpx's
// RD_THRESH_INIT_FACT / RD_THRESH_MAX_FACT / RD_THRESH_INC
// (vp9_rd.h:44-46).
const (
	vp9RDThreshInitFact = 32
	vp9RDThreshMaxFact  = 64
	vp9RDThreshInc      = 1
)

// ThrMode mirrors libvpx's THR_MODES enum (vp9_rd.h:53-93). The numeric
// ordering matters: it is used as an index into thresh_mult[] and
// thresh_freq_fact[][], and the mode_idx[ref][INTER_OFFSET(mode)] table at
// vp9_pickmode.c:1098-1103 expects exactly these slots.
type ThrMode uint8

const (
	vp9ThrNearestMV ThrMode = iota
	vp9ThrNearestA
	vp9ThrNearestG

	vp9ThrDC

	vp9ThrNewMV
	vp9ThrNewA
	vp9ThrNewG

	vp9ThrNearMV
	vp9ThrNearA
	vp9ThrNearG

	vp9ThrZeroMV
	vp9ThrZeroG
	vp9ThrZeroA

	vp9ThrCompNearestLA
	vp9ThrCompNearestGA

	vp9ThrTM

	vp9ThrCompNearLA
	vp9ThrCompNewLA
	vp9ThrCompNearGA
	vp9ThrCompNewGA

	vp9ThrCompZeroLA
	vp9ThrCompZeroGA

	vp9ThrHPred
	vp9ThrVPred
	vp9ThrD135Pred
	vp9ThrD207Pred
	vp9ThrD153Pred
	vp9ThrD63Pred
	vp9ThrD117Pred
	vp9ThrD45Pred
)

// vp9RDThreshBlockSizeFactor is the verbatim port of libvpx's
// rd_thresh_block_size_factor[BLOCK_SIZES]. The factors are << 2 (2 = x0.5,
// 32 = x8 etc.) and correct mode RD thresholds for block size.
//
// libvpx: vp9/encoder/vp9_rd.c:88-90
//
//	static const uint8_t rd_thresh_block_size_factor[BLOCK_SIZES] = {
//	  2, 3, 3, 4, 6, 6, 8, 12, 12, 16, 24, 24, 32
//	};
var vp9RDThreshBlockSizeFactor = [common.BlockSizes]uint8{
	2, 3, 3, 4, 6, 6, 8, 12, 12, 16, 24, 24, 32,
}

// ModeIdxTable mirrors libvpx's static mode_idx[MAX_REF_FRAMES][4] table
// at vp9_pickmode.c:1098-1103. Index 0 is INTRA_FRAME (DC/V/H/TM); 1..3 are
// LAST/GOLDEN/ALTREF for the inter modes NEAREST/NEAR/ZERO/NEW
// (INTER_OFFSET ordering).
//
//	{ THR_DC, THR_V_PRED, THR_H_PRED, THR_TM },
//	{ THR_NEARESTMV, THR_NEARMV, THR_ZEROMV, THR_NEWMV },
//	{ THR_NEARESTG, THR_NEARG, THR_ZEROG, THR_NEWG },
//	{ THR_NEARESTA, THR_NEARA, THR_ZEROA, THR_NEWA },
var ModeIdxTable = [vp9dec.MaxRefFrames][4]ThrMode{
	{vp9ThrDC, vp9ThrVPred, vp9ThrHPred, vp9ThrTM},
	{vp9ThrNearestMV, vp9ThrNearMV, vp9ThrZeroMV, vp9ThrNewMV},
	{vp9ThrNearestG, vp9ThrNearG, vp9ThrZeroG, vp9ThrNewG},
	{vp9ThrNearestA, vp9ThrNearA, vp9ThrZeroA, vp9ThrNewA},
}

// FullRDLastNewMVIndex mirrors LAST_NEW_MV_INDEX in vp9_rdopt.c. Full-RD mode
// thresholds are forced to zero through this entry so the nearest/new/intra-DC
// front of the schedule is always evaluated before adaptive pruning starts.
const FullRDLastNewMVIndex ThrMode = vp9ThrNewG

// FullRDModeDefinition mirrors libvpx's MODE_DEFINITION used by
// vp9_rd_pick_inter_mode_sb.
type FullRDModeDefinition struct {
	Mode     common.PredictionMode
	RefFrame [2]int8
}

// FullRDModeOrder mirrors vp9_rdopt.c::vp9_mode_order. The enum index is also
// the THR_MODES index consumed by rd->threshes and thresh_freq_fact.
var FullRDModeOrder = [vp9MaxModes]FullRDModeDefinition{
	{Mode: common.NearestMv, RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}},
	{Mode: common.NearestMv, RefFrame: [2]int8{vp9dec.AltrefFrame, vp9dec.NoRefFrame}},
	{Mode: common.NearestMv, RefFrame: [2]int8{vp9dec.GoldenFrame, vp9dec.NoRefFrame}},

	{Mode: common.DcPred, RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},

	{Mode: common.NewMv, RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}},
	{Mode: common.NewMv, RefFrame: [2]int8{vp9dec.AltrefFrame, vp9dec.NoRefFrame}},
	{Mode: common.NewMv, RefFrame: [2]int8{vp9dec.GoldenFrame, vp9dec.NoRefFrame}},

	{Mode: common.NearMv, RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}},
	{Mode: common.NearMv, RefFrame: [2]int8{vp9dec.AltrefFrame, vp9dec.NoRefFrame}},
	{Mode: common.NearMv, RefFrame: [2]int8{vp9dec.GoldenFrame, vp9dec.NoRefFrame}},

	{Mode: common.ZeroMv, RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}},
	{Mode: common.ZeroMv, RefFrame: [2]int8{vp9dec.GoldenFrame, vp9dec.NoRefFrame}},
	{Mode: common.ZeroMv, RefFrame: [2]int8{vp9dec.AltrefFrame, vp9dec.NoRefFrame}},

	{Mode: common.NearestMv, RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame}},
	{Mode: common.NearestMv, RefFrame: [2]int8{vp9dec.GoldenFrame, vp9dec.AltrefFrame}},

	{Mode: common.TmPred, RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},

	{Mode: common.NearMv, RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame}},
	{Mode: common.NewMv, RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame}},
	{Mode: common.NearMv, RefFrame: [2]int8{vp9dec.GoldenFrame, vp9dec.AltrefFrame}},
	{Mode: common.NewMv, RefFrame: [2]int8{vp9dec.GoldenFrame, vp9dec.AltrefFrame}},

	{Mode: common.ZeroMv, RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame}},
	{Mode: common.ZeroMv, RefFrame: [2]int8{vp9dec.GoldenFrame, vp9dec.AltrefFrame}},

	{Mode: common.HPred, RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},
	{Mode: common.VPred, RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},
	{Mode: common.D135Pred, RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},
	{Mode: common.D207Pred, RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},
	{Mode: common.D153Pred, RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},
	{Mode: common.D63Pred, RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},
	{Mode: common.D117Pred, RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},
	{Mode: common.D45Pred, RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},
}

// FullRDModeIndex maps a candidate mode/ref pair to libvpx's full-RD
// vp9_mode_order index. The returned value is a THR_MODES index.
func FullRDModeIndex(mode common.PredictionMode, refFrame, secondRefFrame int8) (ThrMode, bool) {
	for i, def := range FullRDModeOrder {
		if def.Mode == mode &&
			def.RefFrame[0] == refFrame &&
			def.RefFrame[1] == secondRefFrame {
			return ThrMode(i), true
		}
	}
	return 0, false
}

// FullRDSingleModeIndex maps an inter single-ref mode directly through
// libvpx's mode_idx table. It is the hot-path sibling of FullRDModeIndex for
// the common single-reference full-RD loop.
func FullRDSingleModeIndex(mode common.PredictionMode, refFrame int8) (ThrMode, bool) {
	if refFrame <= vp9dec.IntraFrame || refFrame >= vp9dec.MaxRefFrames {
		return 0, false
	}
	offset := ModeOffsetInter(mode)
	if offset < 0 || offset >= len(ModeIdxTable[refFrame]) {
		return 0, false
	}
	return ModeIdxTable[refFrame][offset], true
}

// FullRDCorrectNewMVMode mirrors the post-pick correction in
// vp9_rd_pick_inter_mode_sb that relabels a NEWMV winner when its selected MV
// exactly matches the cheaper nearest, near, or zero-motion mode.
func FullRDCorrectNewMVMode(mode common.PredictionMode, mv [2]vp9dec.MV,
	compound bool, nearest, near [2]vp9dec.MV,
	nearestValid, nearValid [2]bool,
) common.PredictionMode {
	if mode != common.NewMv {
		return mode
	}
	if fullRDMVsMatch(mv, nearest, nearestValid, compound) {
		return common.NearestMv
	}
	if fullRDMVsMatch(mv, near, nearValid, compound) {
		return common.NearMv
	}
	if mv[0] == (vp9dec.MV{}) && (!compound || mv[1] == (vp9dec.MV{})) {
		return common.ZeroMv
	}
	return mode
}

func fullRDMVsMatch(mv, ref [2]vp9dec.MV, valid [2]bool, compound bool) bool {
	if !valid[0] || mv[0] != ref[0] {
		return false
	}
	if compound {
		return valid[1] && mv[1] == ref[1]
	}
	return true
}

// ModeOffsetInter maps an inter prediction-mode value (NEARESTMV..NEWMV)
// to the [0,3] offset libvpx's mode_idx table expects. Mirrors
// INTER_OFFSET(mode) at vp9_pickmode.c:2240 (defined in vp9_blockd.h).
//
// NEARESTMV=10, NEARMV=11, ZEROMV=12, NEWMV=13 (common.enums) ⇒ offset 0..3.
func ModeOffsetInter(mode common.PredictionMode) int {
	return int(mode) - int(common.NearestMv)
}

// RDThreshState carries the per-frame and per-tile state for the
// mode_rd_thresh gate. govpx's current realtime encoder is single-tile and
// single-segment for this path, so libvpx's
// [MAX_SEGMENTS][BLOCK_SIZES][MAX_MODES] threshes collapse to a single segment
// plane and libvpx's per-tile thresh_freq_fact collapses to one tile plane.
//
// libvpx: vp9/encoder/vp9_rd.h:111-130 RD_OPT + vp9/encoder/vp9_block.h
// TileDataEnc::thresh_freq_fact[BLOCK_SIZES][MAX_MODES].
type RDThreshState struct {
	// threshMult mirrors RD_OPT::thresh_mult[MAX_MODES]
	// (vp9_rd.h:116).
	threshMult [vp9MaxModes]int

	// threshes mirrors RD_OPT::threshes[MAX_SEGMENTS][BLOCK_SIZES][MAX_MODES]
	// (vp9_rd.h:119) collapsed to the single-segment plane (segment_id=0).
	threshes [common.BlockSizes][vp9MaxModes]int

	// threshFreqFact mirrors TileDataEnc::thresh_freq_fact[BLOCK_SIZES][MAX_MODES]
	// (vp9_block.h). Single-tile govpx realtime collapses libvpx's per-tile
	// array to a single tile plane.
	threshFreqFact [common.BlockSizes][vp9MaxModes]int

	// initialised tracks whether threshFreqFact has been primed for the
	// current encode session (RD_THRESH_INIT_FACT init). Cleared on
	// session reset.
	initialised bool
}

// Initialized reports whether thresh_freq_fact has been primed for the current
// encode session.
func (s *RDThreshState) Initialized() bool {
	return s.initialised
}

// Threshold returns the RD threshold for bsize/mode.
func (s *RDThreshState) Threshold(bsize common.BlockSize, mode ThrMode) int {
	return s.threshes[bsize][mode]
}

// ThreshFreqFact returns the adaptive frequency factor for bsize/mode.
func (s *RDThreshState) ThreshFreqFact(bsize common.BlockSize, mode ThrMode) int {
	return s.threshFreqFact[bsize][mode]
}

// InitFreqFact primes thresh_freq_fact to RD_THRESH_INIT_FACT
// for every (bsize, mode) slot, mirroring libvpx's tile-data birth init
// at vp9_encodeframe.c:5421-5427.
//
//	for (i = 0; i < BLOCK_SIZES; ++i) {
//	  for (j = 0; j < MAX_MODES; ++j) {
//	    tile_data->thresh_freq_fact[i][j] = RD_THRESH_INIT_FACT;
//	    ...
//	  }
//	}
func (s *RDThreshState) InitFreqFact() {
	for i := range s.threshFreqFact {
		for j := range s.threshFreqFact[i] {
			s.threshFreqFact[i][j] = vp9RDThreshInitFact
		}
	}
	s.initialised = true
}

// SetRDSpeedThresholds is the verbatim port of libvpx's
// vp9_set_rd_speed_thresholds.
//
// libvpx: vp9/encoder/vp9_rd.c:693-745
//
//	void vp9_set_rd_speed_thresholds(VP9_COMP *cpi) {
//	  int i;
//	  RD_OPT *const rd = &cpi->rd;
//	  SPEED_FEATURES *const sf = &cpi->sf;
//
//	  for (i = 0; i < MAX_MODES; ++i)
//	    rd->thresh_mult[i] = cpi->oxcf.mode == BEST ? -500 : 0;
//
//	  if (sf->adaptive_rd_thresh) {
//	    rd->thresh_mult[THR_NEARESTMV] = 300;
//	    rd->thresh_mult[THR_NEARESTG]  = 300;
//	    rd->thresh_mult[THR_NEARESTA]  = 300;
//	  } else {
//	    rd->thresh_mult[THR_NEARESTMV] = 0;
//	    rd->thresh_mult[THR_NEARESTG]  = 0;
//	    rd->thresh_mult[THR_NEARESTA]  = 0;
//	  }
//
//	  rd->thresh_mult[THR_DC] += 1000;
//	  ...
//	}
//
// `mode == BEST` is a libvpx-only quality preset that govpx does not surface
// for this encoder path, so the BEST=-500 leg collapses to 0.
func (rd *RDThreshState) SetRDSpeedThresholds(adaptiveRdThresh int) {
	// Reset all entries to 0 (govpx never runs BEST).
	for i := range rd.threshMult {
		rd.threshMult[i] = 0
	}

	if adaptiveRdThresh != 0 {
		rd.threshMult[vp9ThrNearestMV] = 300
		rd.threshMult[vp9ThrNearestG] = 300
		rd.threshMult[vp9ThrNearestA] = 300
	} else {
		rd.threshMult[vp9ThrNearestMV] = 0
		rd.threshMult[vp9ThrNearestG] = 0
		rd.threshMult[vp9ThrNearestA] = 0
	}

	rd.threshMult[vp9ThrDC] += 1000

	rd.threshMult[vp9ThrNewMV] += 1000
	rd.threshMult[vp9ThrNewA] += 1000
	rd.threshMult[vp9ThrNewG] += 1000

	rd.threshMult[vp9ThrNearMV] += 1000
	rd.threshMult[vp9ThrNearA] += 1000
	rd.threshMult[vp9ThrCompNearestLA] += 1000
	rd.threshMult[vp9ThrCompNearestGA] += 1000

	rd.threshMult[vp9ThrTM] += 1000

	rd.threshMult[vp9ThrCompNearLA] += 1500
	rd.threshMult[vp9ThrCompNewLA] += 2000
	rd.threshMult[vp9ThrNearG] += 1000
	rd.threshMult[vp9ThrCompNearGA] += 1500
	rd.threshMult[vp9ThrCompNewGA] += 2000

	rd.threshMult[vp9ThrZeroMV] += 2000
	rd.threshMult[vp9ThrZeroG] += 2000
	rd.threshMult[vp9ThrZeroA] += 2000
	rd.threshMult[vp9ThrCompZeroLA] += 2500
	rd.threshMult[vp9ThrCompZeroGA] += 2500

	rd.threshMult[vp9ThrHPred] += 2000
	rd.threshMult[vp9ThrVPred] += 2000
	rd.threshMult[vp9ThrD45Pred] += 2500
	rd.threshMult[vp9ThrD135Pred] += 2500
	rd.threshMult[vp9ThrD117Pred] += 2500
	rd.threshMult[vp9ThrD153Pred] += 2500
	rd.threshMult[vp9ThrD207Pred] += 2500
	rd.threshMult[vp9ThrD63Pred] += 2500
}

// vp9ComputeRDThreshFactor is the verbatim port of libvpx's
// compute_rd_thresh_factor.
//
// libvpx: vp9/encoder/vp9_rd.c:312-329
//
//	static int compute_rd_thresh_factor(int qindex, vpx_bit_depth_t bit_depth) {
//	  double q;
//	  q = vp9_dc_quant(qindex, 0, VPX_BITS_8) / 4.0;
//	  // TODO(debargha): Adjust the function below.
//	  return VPXMAX((int)(pow(q, RD_THRESH_POW) * 5.12), 8);
//	}
//
// govpx is 8-bit only (highbitdepth not surfaced), so the BITS_10/BITS_12
// legs collapse to the BITS_8 q/4.0 expression. RD_THRESH_POW = 1.25
// (vp9_rd.c:43).
func vp9ComputeRDThreshFactor(qindex int) int {
	q := float64(vp9dec.VpxDcQuant(qindex, 0, vp9dec.BitDepth8)) / 4.0
	v := int(math.Pow(q, 1.25) * 5.12)
	if v < 8 {
		return 8
	}
	return v
}

// SetBlockThresholds is the verbatim port of libvpx's set_block_thresholds.
// Single-segment collapse: govpx does not enable segmentation for this path,
// so libvpx's seg_id loop reduces to segment_id=0.
//
// libvpx: vp9/encoder/vp9_rd.c:355-385
//
//	static void set_block_thresholds(const VP9_COMMON *cm, RD_OPT *rd) {
//	  int i, bsize, segment_id;
//	  for (segment_id = 0; segment_id < MAX_SEGMENTS; ++segment_id) {
//	    const int qindex =
//	        clamp(vp9_get_qindex(&cm->seg, segment_id, cm->base_qindex) +
//	                  cm->y_dc_delta_q,
//	              0, MAXQ);
//	    const int q = compute_rd_thresh_factor(qindex, cm->bit_depth);
//	    for (bsize = 0; bsize < BLOCK_SIZES; ++bsize) {
//	      const int t = q * rd_thresh_block_size_factor[bsize];
//	      const int thresh_max = INT_MAX / t;
//	      if (bsize >= BLOCK_8X8) {
//	        for (i = 0; i < MAX_MODES; ++i)
//	          rd->threshes[segment_id][bsize][i] =
//	              rd->thresh_mult[i] < thresh_max
//	                  ? rd->thresh_mult[i] * t / 4
//	                  : INT_MAX;
//	      } else {
//	        for (i = 0; i < MAX_REFS; ++i) ...   // sub8x8 path
//	      }
//	    }
//	  }
//	}
//
// govpx does not surface the sub-8x8 RD picker (vp9_pick_inter_mode_sub8x8),
// so the bsize<Block8x8 branch is omitted; only the bsize>=Block8x8 branch
// is populated for the realtime nonrd picker.
func (rd *RDThreshState) SetBlockThresholds(baseQindex, yDcDeltaQ int) {
	qindex := min(max(baseQindex+yDcDeltaQ, 0), vp9dec.MaxQ)
	q := vp9ComputeRDThreshFactor(qindex)
	for bsize := range common.BlockSizes {
		t := q * int(vp9RDThreshBlockSizeFactor[bsize])
		if t <= 0 {
			// Defensive: libvpx's INT_MAX/t would divide-by-zero only if
			// q==0, which compute_rd_thresh_factor's floor=8 prevents;
			// but if a future bit-depth introduces t=0 fall back to
			// INT_MAX threshold to disable the gate for that bsize.
			for i := range rd.threshes[bsize] {
				rd.threshes[bsize][i] = math.MaxInt32
			}
			continue
		}
		threshMax := math.MaxInt32 / t
		if bsize >= common.Block8x8 {
			for i := range vp9MaxModes {
				if rd.threshMult[i] < threshMax {
					rd.threshes[bsize][i] = rd.threshMult[i] * t / 4
				} else {
					rd.threshes[bsize][i] = math.MaxInt32
				}
			}
		}
	}
}

// RDLessThanThresh is the verbatim port of libvpx's rd_less_than_thresh
// (vp9_rd.h:193-196).
//
//	static INLINE int rd_less_than_thresh(int64_t best_rd, int thresh,
//	                                      const int *const thresh_fact) {
//	  return best_rd < ((int64_t)thresh * (*thresh_fact) >> 5) || thresh == INT_MAX;
//	}
//
// best_rd is signed int64 because libvpx's RDCOST can be negative under
// rate-bias scenarios; govpx's per-mode score is unsigned uint64 (RDCOST is
// monotonic on rate+dist, both non-negative) but the gate compares
// best_rd < thresh*fact>>5 so the comparison is valid when best_rd has not
// overflowed int64 — which it can't because score = (R*rdmult)>>9 + (D<<7)
// with R, rdmult, D bounded.
func RDLessThanThresh(bestRd uint64, thresh, threshFact int) bool {
	if thresh == math.MaxInt32 {
		return true
	}
	// libvpx computes (int64_t)thresh * fact >> 5 in signed int64 arithmetic.
	// thresh*fact stays well below int64 max for any realistic (q,
	// thresh_mult, fact) combination (max ~ 64K * 64K = 4G); evaluate in
	// int64 for libvpx-faithful overflow shape.
	lhs := int64(thresh) * int64(threshFact) >> 5
	if lhs < 0 {
		// Negative product means thresh*fact overflowed int64 — libvpx
		// implicitly relies on this never happening; mirror by treating
		// the threshold as exhausted (no skip).
		return false
	}
	return bestRd < uint64(lhs)
}

// FullRDModeRDThreshold returns the rd_less_than_thresh base threshold for a
// full-RD mode-order entry. libvpx clears mode_threshold[0..LAST_NEW_MV_INDEX]
// before it computes the adaptive thresholds, so those modes are never skipped.
func (rd *RDThreshState) FullRDModeRDThreshold(bsize common.BlockSize,
	modeIndex ThrMode, bestModeSkippable, scheduleModeSearch bool,
) int {
	modeRDThresh := 0
	if modeIndex > FullRDLastNewMVIndex {
		modeRDThresh = rd.Threshold(bsize, modeIndex)
	}
	if bestModeSkippable && scheduleModeSearch {
		modeRDThresh <<= 1
	}
	return modeRDThresh
}

// UpdateFullRDThreshFact mirrors libvpx vp9_update_rd_thresh_fact from
// vp9_rd.c. Unlike the realtime non-RD picker's per-ref update helper below,
// full-RD updates every mode slot for a small block-size neighborhood around
// the selected block.
func (rd *RDThreshState) UpdateFullRDThreshFact(bsize common.BlockSize,
	bestModeIndex ThrMode, adaptiveRdThresh int,
) {
	if adaptiveRdThresh <= 0 || bsize >= common.BlockSizes {
		return
	}
	topMode := vp9MaxModes
	if bsize < common.Block8x8 {
		topMode = 6
	}
	minSize := bsize - 1
	if minSize < common.Block4x4 {
		minSize = common.Block4x4
	}
	maxSize := bsize + 2
	if maxSize > common.Block64x64 {
		maxSize = common.Block64x64
	}
	cap := adaptiveRdThresh * vp9RDThreshMaxFact
	for mode := range topMode {
		for bs := minSize; bs <= maxSize; bs++ {
			fact := &rd.threshFreqFact[bs][mode]
			if ThrMode(mode) == bestModeIndex {
				*fact -= *fact >> 4
			} else {
				*fact = min(*fact+vp9RDThreshInc, cap)
			}
		}
	}
}

// UpdateThreshFreqFact is the verbatim port of libvpx's
// update_thresh_freq_fact (vp9_pickmode.c:1148-1163), the non-row-MT branch.
// row-MT is not surfaced in govpx for this path, so the row-MT sibling at
// vp9_pickmode.c:1130-1146 is omitted.
//
//	static INLINE void update_thresh_freq_fact(
//	    VP9_COMP *cpi, TileDataEnc *tile_data, unsigned int source_variance,
//	    BLOCK_SIZE bsize, MV_REFERENCE_FRAME ref_frame, THR_MODES best_mode_idx,
//	    PREDICTION_MODE mode) {
//	  THR_MODES thr_mode_idx = mode_idx[ref_frame][mode_offset(mode)];
//	  int *freq_fact = &tile_data->thresh_freq_fact[bsize][thr_mode_idx];
//	  if (thr_mode_idx == best_mode_idx)
//	    *freq_fact -= (*freq_fact >> 4);
//	  else if (cpi->sf.limit_newmv_early_exit && mode == NEWMV &&
//	           ref_frame == LAST_FRAME && source_variance < 5) {
//	    *freq_fact = VPXMIN(*freq_fact + RD_THRESH_INC, 32);
//	  } else {
//	    *freq_fact = VPXMIN(*freq_fact + RD_THRESH_INC,
//	                        cpi->sf.adaptive_rd_thresh * RD_THRESH_MAX_FACT);
//	  }
//	}
func (rd *RDThreshState) UpdateThreshFreqFact(sourceVariance uint, bsize common.BlockSize,
	refFrame int8, bestModeIdx ThrMode, mode common.PredictionMode,
	limitNewMvEarlyExit, adaptiveRdThresh int,
) {
	thrModeIdx := ModeIdxTable[refFrame][ModeOffsetInterOrIntra(mode)]
	fact := &rd.threshFreqFact[bsize][thrModeIdx]
	if thrModeIdx == bestModeIdx {
		*fact -= *fact >> 4
		return
	}
	if limitNewMvEarlyExit != 0 && mode == common.NewMv &&
		refFrame == vp9dec.LastFrame && sourceVariance < 5 {
		v := min(*fact+vp9RDThreshInc, 32)
		*fact = v
		return
	}
	cap := adaptiveRdThresh * vp9RDThreshMaxFact
	v := min(*fact+vp9RDThreshInc, cap)
	*fact = v
}

// ModeOffsetInterOrIntra mirrors libvpx's mode_offset (vp9_pickmode.c:1108-1120).
// For inter modes (>= NEARESTMV) it returns INTER_OFFSET(mode); for intra
// modes it maps {DC,V,H,TM}→{0,1,2,3}; anything else returns -1.
func ModeOffsetInterOrIntra(mode common.PredictionMode) int {
	if mode >= common.NearestMv {
		return int(mode) - int(common.NearestMv)
	}
	switch mode {
	case common.DcPred:
		return 0
	case common.VPred:
		return 1
	case common.HPred:
		return 2
	case common.TmPred:
		return 3
	default:
		return -1
	}
}
