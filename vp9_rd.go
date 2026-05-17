package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// Lagrangian rate–distortion infrastructure ported verbatim from libvpx
// v1.16.0. See vp9/encoder/vp9_rd.{c,h} for the C reference.
//
// libvpx: vp9/encoder/vp9_rd.h:26-34
//
//	#define RDDIV_BITS 7
//	#define RDCOST(RM, DM, R, D) \
//	  ROUND_POWER_OF_TWO(((int64_t)(R)) * (RM), VP9_PROB_COST_SHIFT) + ((D) << (DM))
//
// VP9_PROB_COST_SHIFT == 9 (libvpx: vp9/encoder/vp9_cost.h:24); the rate is
// already in 1/512-bit units, so the rate-cost term is R*RM rounded by 256
// then divided by 512.  D is in plain SSE units; (D << 7) folds it into the
// rate domain so the final cost is monotonic in the encoded byte count.

// vp9DefRDQMultInter is libvpx's def_inter_rd_multiplier(qindex): the
// per-qindex floating coefficient applied to q*q when computing the inter
// frame's base rdmult.
//
// libvpx: vp9/encoder/vp9_rd.c:223-225
//
//	static double def_inter_rd_multiplier(int qindex) {
//	  return 4.15 + (0.001 * (double)qindex);
//	}
func vp9DefRDQMultInter(qindex int) float64 {
	return 4.15 + 0.001*float64(qindex)
}

// vp9DefRDQMultArf is libvpx's def_arf_rd_multiplier(qindex).
//
// libvpx: vp9/encoder/vp9_rd.c:230-232
//
//	static double def_arf_rd_multiplier(int qindex) {
//	  return 4.25 + (0.001 * (double)qindex);
//	}
func vp9DefRDQMultArf(qindex int) float64 {
	return 4.25 + 0.001*float64(qindex)
}

// vp9DefRDQMultKey is libvpx's def_kf_rd_multiplier(qindex).
//
// libvpx: vp9/encoder/vp9_rd.c:237-239
//
//	static double def_kf_rd_multiplier(int qindex) {
//	  return 4.35 + (0.001 * (double)qindex);
//	}
func vp9DefRDQMultKey(qindex int) float64 {
	return 4.35 + 0.001*float64(qindex)
}

// vp9RDFrameType selects which of the three libvpx per-qindex multiplier
// coefficient curves applies.  Matches the branching in
// vp9_compute_rd_mult_based_on_qindex (vp9/encoder/vp9_rd.c:241-275):
//
//   - KEY_FRAME              => key  (4.35 + 0.001*q)
//   - GF/ARF refresh         => arf  (4.25 + 0.001*q)
//   - otherwise              => inter (4.15 + 0.001*q)
type vp9RDFrameType uint8

const (
	vp9RDFrameKey vp9RDFrameType = iota
	vp9RDFrameArfGolden
	vp9RDFrameInter
)

// vp9ComputeRDMultBasedOnQindex is the verbatim port of libvpx's
// vp9_compute_rd_mult_based_on_qindex.  It returns the base Lagrange
// multiplier consumed by RDCOST.  govpx is bit-depth=8 only (libvpx's
// HIGHBITDEPTH branches are no-ops here).
//
// libvpx: vp9/encoder/vp9_rd.c:241-276
//
//	int vp9_compute_rd_mult_based_on_qindex(const VP9_COMP *cpi, int qindex) {
//	  const RD_CONTROL *rdc = &cpi->rd_ctrl;
//	  const int q = vp9_dc_quant(qindex, 0, cpi->common.bit_depth);
//	  int rdmult = q * q;
//	  ...
//	  if (cpi->common.frame_type == KEY_FRAME) {
//	    double def_rd_q_mult = def_kf_rd_multiplier(qindex);
//	    rdmult = (int)((double)rdmult * def_rd_q_mult * rdc->rd_mult_key_qp_fac);
//	  } else if (!cpi->rc.is_src_frame_alt_ref &&
//	             (cpi->refresh_golden_frame || cpi->refresh_alt_ref_frame)) {
//	    double def_rd_q_mult = def_arf_rd_multiplier(qindex);
//	    rdmult = (int)((double)rdmult * def_rd_q_mult * rdc->rd_mult_arf_qp_fac);
//	  } else {
//	    double def_rd_q_mult = def_inter_rd_multiplier(qindex);
//	    rdmult = (int)((double)rdmult * def_rd_q_mult * rdc->rd_mult_inter_qp_fac);
//	  }
//	  return rdmult > 0 ? rdmult : 1;
//	}
//
// govpx leaves rd_ctrl.rd_mult_{key,arf,inter}_qp_fac at libvpx's default
// 1.0 because the Vizier RC plumbing (use_vizier_rc_params) is not enabled;
// vp9_init_rd_parameters' early-return path applies (vp9_rd.c:210).
func vp9ComputeRDMultBasedOnQindex(qindex int, frameType vp9RDFrameType) int {
	if qindex < 0 {
		qindex = 0
	}
	if qindex > vp9dec.MaxQ {
		qindex = vp9dec.MaxQ
	}
	q := int(vp9dec.VpxDcQuant(qindex, 0, vp9dec.BitDepth8))
	rdmult := q * q
	var mult float64
	switch frameType {
	case vp9RDFrameKey:
		mult = vp9DefRDQMultKey(qindex)
	case vp9RDFrameArfGolden:
		mult = vp9DefRDQMultArf(qindex)
	default:
		mult = vp9DefRDQMultInter(qindex)
	}
	rdmult = int(float64(rdmult) * mult)
	if rdmult <= 0 {
		return 1
	}
	return rdmult
}

// vp9ComputeRDMult is the verbatim port of libvpx's vp9_compute_rd_mult.
// govpx does not run the libvpx two-pass GF-group modulation here yet, so
// the second leg of modulate_rdmult (vp9/encoder/vp9_rd.c:278-292) collapses
// to the identity, matching the libvpx single-pass path.
//
// libvpx: vp9/encoder/vp9_rd.c:294-302
//
//	int vp9_compute_rd_mult(const VP9_COMP *cpi, int qindex) {
//	  int rdmult = vp9_compute_rd_mult_based_on_qindex(cpi, qindex);
//	  ...
//	  return modulate_rdmult(cpi, rdmult);
//	}
func vp9ComputeRDMult(qindex int, frameType vp9RDFrameType) int {
	return vp9ComputeRDMultBasedOnQindex(qindex, frameType)
}

// vp9GetAdaptiveRDMult is the verbatim port of libvpx's
// vp9_get_adaptive_rdmult.  It scales the base rdmult by beta (the TPL
// per-block intra/mc-dep ratio) and re-applies modulate_rdmult.  govpx's
// TPL caller (getVP9TPLRDMultDelta) inlines the scaling step today; this
// helper exists so the cyclic-refresh and other AQ paths can call the
// libvpx-named entry point.
//
// libvpx: vp9/encoder/vp9_rd.c:304-310
//
//	int vp9_get_adaptive_rdmult(const VP9_COMP *cpi, double beta) {
//	  int rdmult =
//	      vp9_compute_rd_mult_based_on_qindex(cpi, cpi->common.base_qindex);
//	  rdmult = (int)((double)rdmult / beta);
//	  rdmult = rdmult > 0 ? rdmult : 1;
//	  return modulate_rdmult(cpi, rdmult);
//	}
func vp9GetAdaptiveRDMult(qindex int, frameType vp9RDFrameType, beta float64) int {
	base := vp9ComputeRDMultBasedOnQindex(qindex, frameType)
	if beta <= 0 {
		return base
	}
	rdmult := int(float64(base) / beta)
	if rdmult <= 0 {
		return 1
	}
	return rdmult
}

// vp9RDFrameTypeFor selects the libvpx frame-type bucket for the rdmult
// lookup.  Mirrors the branching in vp9_compute_rd_mult_based_on_qindex
// (vp9/encoder/vp9_rd.c:256-266): KEY_FRAME wins outright; otherwise an
// ARF/GF refresh that is not a srcframe_altref wins; otherwise inter.
func vp9RDFrameTypeFor(isKey, isSrcFrameAltRef, refreshGolden, refreshAlt bool) vp9RDFrameType {
	if isKey {
		return vp9RDFrameKey
	}
	if !isSrcFrameAltRef && (refreshGolden || refreshAlt) {
		return vp9RDFrameArfGolden
	}
	return vp9RDFrameInter
}

// vp9RDCostFromRate folds a libvpx-shaped rate (already scaled by
// VP9_PROB_COST_SHIFT == 9) into the rdmult-weighted rate cost the
// RDCOST macro expands to.  Pulled out as a helper so the inter mode
// picker can reuse it for the rate-only component when distortion is
// already known to be zero.
//
// libvpx: vp9/encoder/vp9_rd.h:29-30 (rate side of RDCOST)
func vp9RDCostFromRate(rdmult, rate int) uint64 {
	if rate < 0 {
		rate = 0
	}
	return uint64((int64(rate)*int64(rdmult) +
		(1 << (encoder.VP9ProbCostShift - 1))) >> encoder.VP9ProbCostShift)
}

// vp9RDCostFromDistortion expands the distortion side of the RDCOST macro
// (D << RDDIV_BITS).  Kept as a helper for symmetry with
// vp9RDCostFromRate so call sites can read the two pieces independently.
//
// libvpx: vp9/encoder/vp9_rd.h:29-30 (distortion side of RDCOST)
func vp9RDCostFromDistortion(rddiv int, distortion uint64) uint64 {
	if rddiv < 0 {
		rddiv = 0
	}
	return distortion << uint(rddiv)
}

// rd_variance_adjustment infrastructure — verbatim port of libvpx's
// rd_variance_adjustment (vp9/encoder/vp9_rdopt.c:3273-3363, v1.16.0).
//
// libvpx applies this bias inside vp9_rd_pick_inter_mode_sb after the
// per-mode this_rd has been finalised but before the candidate is compared
// to best_rd (vp9_rdopt.c:3960-3964).  The adjustment penalises modes whose
// reconstruction is significantly smoother than the source — protecting
// fine texture (film grain in particular) on textured SB partitions.  It is
// gated at the caller by `recon != NULL`, which libvpx only allocates when
// `cpi->oxcf.content == VP9E_CONTENT_FILM` (vp9_rdopt.c:3503-3515), so the
// adjustment is FILM-content-only in practice.
//
// LOW_VAR_THRESH and VAR_MULT match the libvpx #defines verbatim.

// vp9LowVarThresh is libvpx's LOW_VAR_THRESH (vp9_rdopt.c:3276).
const vp9LowVarThresh = 250

// vp9VarMult is libvpx's VAR_MULT (vp9_rdopt.c:3277).
const vp9VarMult = 250

// vp9MaxVarAdjust mirrors libvpx's max_var_adjust[VP9E_CONTENT_INVALID]
// table (vp9_rdopt.c:3278).  Indexed by VP9E_CONTENT enum:
// DEFAULT=0, SCREEN=1, FILM=2.
var vp9MaxVarAdjust = [3]uint{16, 16, 250}

// vp9SectionNoiseDef mirrors libvpx's SECTION_NOISE_DEF
// (vp9_firstpass.h:28).  Used by rd_variance_adjustment to scale
// LOW_VAR_THRESH by the GF group's estimated noise energy in pass 2.
const vp9SectionNoiseDef = 250.0

// vp9RDVarianceAdjustmentInputs captures every input rd_variance_adjustment
// consumes.  Pulled into a struct so the call site can synthesize each
// field from its local state and pass them into a pure function that
// matches libvpx's branching one-to-one.
//
// libvpx: vp9/encoder/vp9_rdopt.c:3280-3285 (parameter list).
type vp9RDVarianceAdjustmentInputs struct {
	// SrcVariance is libvpx's `src_variance` BEFORE the (bw*bh) scaling.
	// The caller is expected to compute it via vp9_get_sby_variance (which
	// in govpx is vp9BlockSourceVariance128 on the luma source buffer).
	SrcVariance uint
	// RecVariance is libvpx's `rec_variance` BEFORE the (bw*bh) scaling.
	// The caller is expected to compute it via vp9_get_sby_variance on
	// libvpx's `recon` buffer (8-bit reconstruction at stride 64).
	RecVariance uint
	// BSize is the BLOCK_SIZE the RD is being evaluated at.
	BSize common.BlockSize
	// ContentType mirrors `cpi->oxcf.content`.  Index into vp9MaxVarAdjust.
	ContentType vp9SpeedDispatchContent
	// Pass2 mirrors `cpi->oxcf.pass == 2`.  Gates the FILM noise-factor
	// low-var-threshold scaling at vp9_rdopt.c:3318-3331.
	Pass2 bool
	// GroupNoiseEnergy mirrors `cpi->twopass.gf_group.group_noise_energy`
	// used to compute noise_factor in pass-2 FILM mode.  Caller should
	// pass 0 when the GF-group state is not yet populated; the resulting
	// noise_factor (== 0/250) collapses the FILM branch's threshold to 0
	// which then bails on the early-return `src_rec_min > low_var_thresh`
	// path (mirrors libvpx when group_noise_energy is unset).
	GroupNoiseEnergy int
	// RefFrame mirrors `ref_frame`.  Compared to INTRA_FRAME (=0) by the
	// FILM pass-2 low_var_thresh inflation at vp9_rdopt.c:3325-3327.
	RefFrame int8
	// SecondRefFrame mirrors `second_ref_frame`.  Used to detect compound
	// (`second_ref_frame > INTRA_FRAME`) at vp9_rdopt.c:3328-3330 and
	// :3354-3357.
	SecondRefFrame int8
	// ThisMode mirrors `this_mode`.  Only used to detect DC_PRED inside
	// the FILM intra-frame branch at vp9_rdopt.c:3327.
	ThisMode common.PredictionMode
}

// vp9RDVarianceAdjustment is the verbatim Go port of libvpx's
// rd_variance_adjustment (vp9/encoder/vp9_rdopt.c:3280-3362).  Returns the
// adjusted this_rd; if the caller's this_rd is the int64 sentinel for
// INT64_MAX it is returned unmodified (matching libvpx's early return at
// :3298).
//
// libvpx body, reproduced inline for the reviewer:
//
//	static void rd_variance_adjustment(VP9_COMP *cpi, MACROBLOCK *x,
//	                                   BLOCK_SIZE bsize, int64_t *this_rd,
//	                                   struct buf_2d *recon,
//	                                   MV_REFERENCE_FRAME ref_frame,
//	                                   MV_REFERENCE_FRAME second_ref_frame,
//	                                   PREDICTION_MODE this_mode) {
//	  ...
//	  if (*this_rd == INT64_MAX) return;
//	  rec_variance = vp9_get_sby_variance(cpi, recon, bsize);
//	  src_variance = vp9_get_sby_variance(cpi, &x->plane[0].src, bsize);
//	  // Scale based on area in 8x8 blocks
//	  rec_variance /= (bw * bh);
//	  src_variance /= (bw * bh);
//
//	  if (content_type == VP9E_CONTENT_FILM) {
//	    if (cpi->oxcf.pass == 2) {
//	      double noise_factor =
//	          (double)cpi->twopass.gf_group.group_noise_energy /
//	          SECTION_NOISE_DEF;
//	      low_var_thresh = (unsigned int)(low_var_thresh * noise_factor);
//	      if (ref_frame == INTRA_FRAME) {
//	        low_var_thresh *= 2;
//	        if (this_mode == DC_PRED) low_var_thresh *= 5;
//	      } else if (second_ref_frame > INTRA_FRAME) {
//	        low_var_thresh *= 2;
//	      }
//	    }
//	  } else {
//	    low_var_thresh = LOW_VAR_THRESH / 2;
//	  }
//
//	  src_rec_min = VPXMIN(src_variance, rec_variance);
//	  if (src_rec_min > low_var_thresh) return;
//
//	  var_diff = (src_variance > rec_variance) ?
//	      (src_variance - rec_variance) * 2 :
//	      (rec_variance - src_variance) / 2;
//	  adj_max = max_var_adjust[content_type];
//	  var_factor =
//	      (unsigned int)((int64_t)VAR_MULT * var_diff) /
//	      VPXMAX(1, src_variance);
//	  var_factor = VPXMIN(adj_max, var_factor);
//	  if ((content_type == VP9E_CONTENT_FILM) &&
//	      ((ref_frame == INTRA_FRAME) || (second_ref_frame > INTRA_FRAME))) {
//	    var_factor *= 2;
//	  }
//	  *this_rd += (*this_rd * var_factor) / 100;
//	}
func vp9RDVarianceAdjustment(thisRD int64, in vp9RDVarianceAdjustmentInputs) int64 {
	// libvpx: vp9_rdopt.c:3298 — `if (*this_rd == INT64_MAX) return;`.
	// govpx callers represent INT64_MAX with math.MaxInt64.  Anything that
	// already overflowed past that sentinel is opaque to the picker.
	if thisRD == vp9RDVarianceAdjustmentInfinity {
		return thisRD
	}
	if in.BSize >= common.BlockSizes {
		// Defensive: libvpx indexes num_8x8_blocks_*_lookup at BLOCK_*; an
		// out-of-range bsize would slice-panic on the lookup table.
		return thisRD
	}
	// libvpx: vp9_rdopt.c:3294-3295 — `const int bw =
	// num_8x8_blocks_wide_lookup[bsize]; const int bh =
	// num_8x8_blocks_high_lookup[bsize];`.
	bw := uint(common.Num8x8BlocksWideLookup[in.BSize])
	bh := uint(common.Num8x8BlocksHighLookup[in.BSize])
	if bw == 0 || bh == 0 {
		return thisRD
	}
	// libvpx: vp9_rdopt.c:3296 — `vp9e_tune_content content_type =
	// cpi->oxcf.content;`.
	contentType := in.ContentType
	srcVariance := in.SrcVariance
	recVariance := in.RecVariance
	// libvpx: vp9_rdopt.c:3314-3316 — "Scale based on area in 8x8 blocks".
	scale := bw * bh
	recVariance /= scale
	srcVariance /= scale
	// libvpx: vp9_rdopt.c:3293 — `unsigned int low_var_thresh =
	// LOW_VAR_THRESH;`.
	lowVarThresh := uint(vp9LowVarThresh)
	// libvpx: vp9_rdopt.c:3318-3334 — FILM branch with optional pass-2
	// noise-factor scaling, then the non-FILM `low_var_thresh / 2` else.
	if contentType == vp9ContentFilm {
		if in.Pass2 {
			noiseFactor :=
				float64(in.GroupNoiseEnergy) / vp9SectionNoiseDef
			lowVarThresh = uint(float64(lowVarThresh) * noiseFactor)
			if in.RefFrame == vp9dec.IntraFrame {
				lowVarThresh *= 2
				if in.ThisMode == common.DcPred {
					lowVarThresh *= 5
				}
			} else if in.SecondRefFrame > vp9dec.IntraFrame {
				lowVarThresh *= 2
			}
		}
	} else {
		lowVarThresh = uint(vp9LowVarThresh) / 2
	}
	// libvpx: vp9_rdopt.c:3339 — `src_rec_min = VPXMIN(src_variance,
	// rec_variance);`.
	srcRecMin := min(recVariance, srcVariance)
	// libvpx: vp9_rdopt.c:3341 — `if (src_rec_min > low_var_thresh) return;`.
	if srcRecMin > lowVarThresh {
		return thisRD
	}
	// libvpx: vp9_rdopt.c:3343-3346 — asymmetric var_diff.  When the
	// reconstruction is smoother than the source the penalty doubles; when
	// the reconstruction is rougher the penalty is halved.
	var varDiff uint
	if srcVariance > recVariance {
		varDiff = (srcVariance - recVariance) * 2
	} else {
		varDiff = (recVariance - srcVariance) / 2
	}
	// libvpx: vp9_rdopt.c:3348 — `adj_max = max_var_adjust[content_type];`.
	// Bounds-clamp content_type as libvpx does (the array is sized
	// VP9E_CONTENT_INVALID == 3 so DEFAULT/SCREEN/FILM all land in-range).
	adjIdx := int(contentType)
	if adjIdx < 0 || adjIdx >= len(vp9MaxVarAdjust) {
		adjIdx = 0
	}
	adjMax := vp9MaxVarAdjust[adjIdx]
	// libvpx: vp9_rdopt.c:3350-3352 — `var_factor = (unsigned int)((int64_t)
	// VAR_MULT * var_diff) / VPXMAX(1, src_variance); var_factor =
	// VPXMIN(adj_max, var_factor);`.  We perform the multiplication in
	// int64 to match libvpx's explicit cast and avoid wrap-around at large
	// var_diff values.
	srcDenom := max(srcVariance, 1)
	varFactor := min(uint(uint64(vp9VarMult)*uint64(varDiff)/uint64(srcDenom)), adjMax)
	// libvpx: vp9_rdopt.c:3354-3357 — FILM doubling for intra and compound.
	if contentType == vp9ContentFilm &&
		(in.RefFrame == vp9dec.IntraFrame ||
			in.SecondRefFrame > vp9dec.IntraFrame) {
		varFactor *= 2
	}
	// libvpx: vp9_rdopt.c:3359 — `*this_rd += (*this_rd * var_factor) / 100;`.
	// Performed in int64 to match libvpx's int64 *this_rd.
	if thisRD < 0 {
		return thisRD
	}
	adjustment := thisRD * int64(varFactor) / 100
	// Saturate to the sentinel rather than overflow the int64 cost domain.
	if adjustment < 0 || thisRD > vp9RDVarianceAdjustmentInfinity-adjustment {
		return vp9RDVarianceAdjustmentInfinity
	}
	return thisRD + adjustment
}

// vp9RDVarianceAdjustmentInfinity is the int64 sentinel matching libvpx's
// INT64_MAX (which represents "RD overflowed / mode rejected").  Kept as a
// named constant so call sites read the same word libvpx does.
const vp9RDVarianceAdjustmentInfinity = int64(^uint64(0) >> 1)
