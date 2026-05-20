package govpx

import (
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
