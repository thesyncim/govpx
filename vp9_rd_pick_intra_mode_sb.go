package govpx

import "github.com/thesyncim/govpx/internal/vp9/encoder"

// vp9RDPickIntraModeSb ports libvpx v1.16.0
// vp9/encoder/vp9_rdopt.c:3221-3271 (vp9_rd_pick_intra_mode_sb).  Once
// rd_pick_intra_sby_mode and rd_pick_intra_sbuv_mode have produced their
// per-plane (rate, rate_tokenonly, distortion, skippable) tuples, the
// outer routine composes the final RD_COST structure consumed by the
// partition picker.  The composition is the libvpx-verbatim lines
// 3258-3266 with the surrounding RDCOST evaluation at 3270:
//
//	if (y_skip && uv_skip) {
//	  rd_cost->rate = rate_y + rate_uv - rate_y_tokenonly - rate_uv_tokenonly +
//	                  vp9_cost_bit(vp9_get_skip_prob(cm, xd), 1);
//	  rd_cost->dist = dist_y + dist_uv;
//	} else {
//	  rd_cost->rate =
//	      rate_y + rate_uv + vp9_cost_bit(vp9_get_skip_prob(cm, xd), 0);
//	  rd_cost->dist = dist_y + dist_uv;
//	}
//	...
//	rd_cost->rdcost = RDCOST(x->rdmult, x->rddiv, rd_cost->rate, rd_cost->dist);
//
// When y_skip && uv_skip the encoder will set mi->skip = 1 in the
// downstream write path, so the coefficient (token) rates must be
// stripped and replaced with the single skip-flag bit at probability
// vp9_get_skip_prob(cm, xd).  In every other case the skip flag is
// written as 0 and the per-plane token rates remain.
//
// The helper is intentionally a pure function over the libvpx-faithful
// inputs so it can be unit-tested independently and called from any
// caller that has already produced the per-plane RD picks.  govpx's
// keyframe encode path (vp9_encoder.go pickVP9KeyframeMode +
// pickVP9KeyframeUvMode) currently produces only the Y-mode RD pick
// in-band; the UV side is fixed to DC_PRED without an RD search.  The
// composition still applies once the UV RD picker lands (paired tasks
// #130 / #134) — wiring it into the keyframe-source branch is a
// follow-up that gates on the UV picker's (rate_uv, rate_uv_tokenonly,
// dist_uv, uv_skip) being populated.

// vp9RDPickIntraModeSbInputs mirrors the locally-scoped state that
// libvpx's vp9_rd_pick_intra_mode_sb composes after its two children
// return.  Names match libvpx 1:1 so reviewers can diff against
// vp9_rdopt.c:3227-3265.
type vp9RDPickIntraModeSbInputs struct {
	// Y-plane picker outputs (libvpx rd_pick_intra_sby_mode /
	// rd_pick_intra_sub_8x8_y_mode, vp9_rdopt.c:1363-1416 / 1299-1361).
	//
	// rate_y         = rate_y_tokenonly + bmode_costs[mode]      (libvpx
	//                  vp9_rdopt.c:1398; for sub-8x8 the per-subblock
	//                  rate adds bmode_costs[mode] per 4x4 step and the
	//                  *rate field carries the sum, vp9_rdopt.c:1196).
	// rate_y_token   = the cost_coeffs token-rate output of super_block_yrd
	//                  (vp9_rdopt.c:1393).
	// dist_y         = block_rd_txfm pixel SSE accumulator, scaled by 16
	//                  (vp9_rdopt.c:689).
	// y_skip         = the all-zero-EOB flag set by super_block_yrd
	//                  (vp9_rdopt.c:887; xor'd through txfm_rd_in_plane).
	rateY      int
	rateYToken int
	distY      uint64
	ySkip      bool

	// UV-plane picker outputs (libvpx rd_pick_intra_sbuv_mode,
	// vp9_rdopt.c:1468-1512). Same naming convention as Y.
	rateUV      int
	rateUVToken int
	distUV      uint64
	uvSkip      bool

	// Encoder-frame state pulled from libvpx vp9_rdopt.c:3260/3264 — the
	// skip-flag probability at the current (above, left) neighbour
	// context and the Lagrange knobs that drive RDCOST.
	//
	// libvpx vp9_entropymode.c:vp9_get_skip_prob: fc->skip_probs[ctx]
	// indexed by GetSkipContext(above, left).  govpx mirrors this via
	// e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)].
	skipProb uint8
	rdmult   int
	rddiv    int
}

// vp9RDPickIntraModeSbResult matches the libvpx RD_COST struct that
// rd_cost in vp9_rd_pick_intra_mode_sb is filled out by lines 3259-3270.
type vp9RDPickIntraModeSbResult struct {
	// libvpx vp9_rdopt.c:3259-3265 (rate) and 3261/3265 (dist).
	Rate int
	Dist uint64

	// libvpx vp9_rdopt.c:3270 — RDCOST(x->rdmult, x->rddiv, rate, dist).
	RDCost uint64
}

// vp9RDPickIntraModeSbCompose is the verbatim port of the rd_cost
// composition at libvpx vp9_rdopt.c:3258-3266 + 3270.  Inputs come
// straight from the per-plane RD pickers; the helper performs the
// y_skip && uv_skip token-strip path or the default token-keep path and
// emits the final RD_COST struct.
//
// libvpx (vp9_rdopt.c:3258-3266, 3270):
//
//	if (y_skip && uv_skip) {
//	  rd_cost->rate = rate_y + rate_uv - rate_y_tokenonly - rate_uv_tokenonly +
//	                  vp9_cost_bit(vp9_get_skip_prob(cm, xd), 1);
//	  rd_cost->dist = dist_y + dist_uv;
//	} else {
//	  rd_cost->rate =
//	      rate_y + rate_uv + vp9_cost_bit(vp9_get_skip_prob(cm, xd), 0);
//	  rd_cost->dist = dist_y + dist_uv;
//	}
//	...
//	rd_cost->rdcost = RDCOST(x->rdmult, x->rddiv, rd_cost->rate, rd_cost->dist);
func vp9RDPickIntraModeSbCompose(in vp9RDPickIntraModeSbInputs) vp9RDPickIntraModeSbResult {
	var rate int
	if in.ySkip && in.uvSkip {
		// libvpx vp9_rdopt.c:3259-3260 — strip the per-plane coefficient
		// (token) rates, retain the mode bits, and pay the skip-flag
		// bit at vp9_cost_bit(skip_prob, 1).
		rate = in.rateY + in.rateUV - in.rateYToken - in.rateUVToken +
			encoder.VP9CostBit(in.skipProb, 1)
	} else {
		// libvpx vp9_rdopt.c:3263-3264 — keep the per-plane token rates
		// and pay the skip-flag bit at vp9_cost_bit(skip_prob, 0).
		rate = in.rateY + in.rateUV + encoder.VP9CostBit(in.skipProb, 0)
	}
	// libvpx vp9_rdopt.c:3261/3265 — dist = dist_y + dist_uv.
	dist := in.distY + in.distUV
	return vp9RDPickIntraModeSbResult{
		Rate: rate,
		Dist: dist,
		// libvpx vp9_rdopt.c:3270 — RDCOST(rdmult, rddiv, rate, dist).
		RDCost: vp9RDCost(in.rdmult, in.rddiv, rate, dist),
	}
}
