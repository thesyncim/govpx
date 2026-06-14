package encoder

import (
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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

// RDDivBits mirrors libvpx's RDDIV_BITS.
const RDDivBits = 7

// rdBoostFactor / rdFrameTypeFactor mirror libvpx's private modulation
// tables used by modulate_rdmult.
//
// libvpx: vp9/encoder/vp9_rd.c:189-199.
var rdBoostFactor = [16]int{64, 32, 32, 32, 24, 16, 12, 12, 8, 8, 4, 4, 2, 2, 1, 0}

var rdFrameTypeFactor = [7]int{
	128, // KF_UPDATE
	144, // LF_UPDATE
	128, // GF_UPDATE
	128, // ARF_UPDATE
	144, // OVERLAY_UPDATE
	144, // MID_OVERLAY_UPDATE
	0,   // USE_BUF_FRAME: no real frame is encoded on this path.
}

// defRDQMultInter is libvpx's def_inter_rd_multiplier(qindex): the
// per-qindex floating coefficient applied to q*q when computing the inter
// frame's base rdmult.
//
// libvpx: vp9/encoder/vp9_rd.c:223-225
//
//	static double def_inter_rd_multiplier(int qindex) {
//	  return 4.15 + (0.001 * (double)qindex);
//	}
func defRDQMultInter(qindex int) float64 {
	return 4.15 + 0.001*float64(qindex)
}

// defRDQMultArf is libvpx's def_arf_rd_multiplier(qindex).
//
// libvpx: vp9/encoder/vp9_rd.c:230-232
//
//	static double def_arf_rd_multiplier(int qindex) {
//	  return 4.25 + (0.001 * (double)qindex);
//	}
func defRDQMultArf(qindex int) float64 {
	return 4.25 + 0.001*float64(qindex)
}

// defRDQMultKey is libvpx's def_kf_rd_multiplier(qindex).
//
// libvpx: vp9/encoder/vp9_rd.c:237-239
//
//	static double def_kf_rd_multiplier(int qindex) {
//	  return 4.35 + (0.001 * (double)qindex);
//	}
func defRDQMultKey(qindex int) float64 {
	return 4.35 + 0.001*float64(qindex)
}

// RDFrameType selects which of the three libvpx per-qindex multiplier
// coefficient curves applies.  Matches the branching in
// vp9_compute_rd_mult_based_on_qindex (vp9/encoder/vp9_rd.c:241-275):
//
//   - KEY_FRAME              => key  (4.35 + 0.001*q)
//   - GF/ARF refresh         => arf  (4.25 + 0.001*q)
//   - otherwise              => inter (4.15 + 0.001*q)
type RDFrameType uint8

const (
	RDFrameKey RDFrameType = iota
	RDFrameArfGolden
	RDFrameInter
)

// ComputeRDMultBasedOnQindex is the verbatim port of libvpx's
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
func ComputeRDMultBasedOnQindex(qindex int, frameType RDFrameType) int {
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
	case RDFrameKey:
		mult = defRDQMultKey(qindex)
	case RDFrameArfGolden:
		mult = defRDQMultArf(qindex)
	default:
		mult = defRDQMultInter(qindex)
	}
	rdmult = int(float64(rdmult) * mult)
	if rdmult <= 0 {
		return 1
	}
	return rdmult
}

// RDMultModulation carries the two-pass GF-group state consumed by libvpx's
// modulate_rdmult.
type RDMultModulation struct {
	TwoPass    bool
	IsKeyFrame bool
	UpdateType uint8
	GFUBoost   int
}

// ModulateRDMult ports libvpx's modulate_rdmult.
//
// libvpx: vp9/encoder/vp9_rd.c:278-292
//
//	static int modulate_rdmult(const VP9_COMP *cpi, int rdmult) {
//	  int64_t rdmult_64 = rdmult;
//	  if (cpi->oxcf.pass == 2 && (cpi->common.frame_type != KEY_FRAME)) {
//	    const GF_GROUP *const gf_group = &cpi->twopass.gf_group;
//	    const FRAME_UPDATE_TYPE frame_type = gf_group->update_type[gf_group->index];
//	    const int gfu_boost = cpi->multi_layer_arf
//	                              ? gf_group->gfu_boost[gf_group->index]
//	                              : cpi->rc.gfu_boost;
//	    const int boost_index = VPXMIN(15, (gfu_boost / 100));
//	    rdmult_64 = (rdmult_64 * rd_frame_type_factor[frame_type]) >> 7;
//	    rdmult_64 += ((rdmult_64 * rd_boost_factor[boost_index]) >> 7);
//	  }
//	  return (int)rdmult_64;
//	}
func ModulateRDMult(rdmult int, mod RDMultModulation) int {
	if !mod.TwoPass || mod.IsKeyFrame {
		return rdmult
	}
	frameType := int(mod.UpdateType)
	if frameType < 0 || frameType >= len(rdFrameTypeFactor) {
		frameType = int(LFUpdate)
	}
	boostIndex := mod.GFUBoost / 100
	if boostIndex < 0 {
		boostIndex = 0
	}
	if boostIndex > 15 {
		boostIndex = 15
	}
	rdmult64 := int64(rdmult)
	rdmult64 = (rdmult64 * int64(rdFrameTypeFactor[frameType])) >> 7
	rdmult64 += (rdmult64 * int64(rdBoostFactor[boostIndex])) >> 7
	return int(rdmult64)
}

// ComputeRDMult is the one-pass/no-modulation wrapper around libvpx's
// vp9_compute_rd_mult formula.
//
// libvpx: vp9/encoder/vp9_rd.c:294-302
//
//	int vp9_compute_rd_mult(const VP9_COMP *cpi, int qindex) {
//	  int rdmult = vp9_compute_rd_mult_based_on_qindex(cpi, qindex);
//	  ...
//	  return modulate_rdmult(cpi, rdmult);
//	}
func ComputeRDMult(qindex int, frameType RDFrameType) int {
	return ComputeRDMultBasedOnQindex(qindex, frameType)
}

// ComputeRDMultWithModulation is ComputeRDMult with the two-pass
// modulate_rdmult leg enabled when the caller supplies active GF-group state.
func ComputeRDMultWithModulation(qindex int, frameType RDFrameType,
	mod RDMultModulation,
) int {
	return ModulateRDMult(ComputeRDMultBasedOnQindex(qindex, frameType), mod)
}

// RDFrameTypeFor selects the libvpx frame-type bucket for the rdmult
// lookup.  Mirrors the branching in vp9_compute_rd_mult_based_on_qindex
// (vp9/encoder/vp9_rd.c:256-266): KEY_FRAME wins outright; otherwise an
// ARF/GF refresh that is not a srcframe_altref wins; otherwise inter.
func RDFrameTypeFor(isKey, isSrcFrameAltRef, refreshGolden, refreshAlt bool) RDFrameType {
	if isKey {
		return RDFrameKey
	}
	if !isSrcFrameAltRef && (refreshGolden || refreshAlt) {
		return RDFrameArfGolden
	}
	return RDFrameInter
}

// RDCostFromRate folds a libvpx-shaped rate (already scaled by
// VP9_PROB_COST_SHIFT == 9) into the rdmult-weighted rate cost the
// RDCOST macro expands to.  Pulled out as a helper so the inter mode
// picker can reuse it for the rate-only component when distortion is
// already known to be zero.
//
// libvpx: vp9/encoder/vp9_rd.h:29-30 (rate side of RDCOST)
func RDCostFromRate(rdmult, rate int) uint64 {
	if rate < 0 {
		rate = 0
	}
	return uint64((int64(rate)*int64(rdmult) +
		(1 << (VP9ProbCostShift - 1))) >> VP9ProbCostShift)
}

// RDCostFromDistortion expands the distortion side of the RDCOST macro
// (D << RDDIV_BITS).  Kept as a helper for symmetry with
// RDCostFromRate so call sites can read the two pieces independently.
//
// libvpx: vp9/encoder/vp9_rd.h:29-30 (distortion side of RDCOST)
func RDCostFromDistortion(rddiv int, distortion uint64) uint64 {
	if rddiv < 0 {
		rddiv = 0
	}
	return distortion << uint(rddiv)
}

// RDCost expands libvpx's RDCOST macro.
func RDCost(rdmult, rddiv, rate int, distortion uint64) uint64 {
	return RDCostFromRate(rdmult, rate) + RDCostFromDistortion(rddiv, distortion)
}

// KeyframeRDMul is the integer keyframe rdmult formula used by the keyframe
// mode picker. It is kept beside the floating-point RD helpers because both
// implement libvpx's qindex-to-rdmult family.
func KeyframeRDMul(qindex int) int {
	if qindex < 0 {
		qindex = 0
	}
	if qindex > vp9dec.MaxQ {
		qindex = vp9dec.MaxQ
	}
	q := int(vp9dec.VpxDcQuant(qindex, 0, vp9dec.BitDepth8))
	rdmult := q * q * (4350 + qindex) / 1000
	if rdmult < 1 {
		return 1
	}
	return rdmult
}
