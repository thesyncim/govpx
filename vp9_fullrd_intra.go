package govpx

// vp9_fullrd_intra.go is a verbatim port of the intra-mode *rate cost* and
// intra-mode RD comparison used inside libvpx's full-RD intra pickers —
// rd_pick_intra_sby_mode / super_block_uvrd / rd_pick_intra_sbuv_mode and the
// intra branch of vp9_rd_pick_inter_mode_sb. It lives in its own file (rather
// than editing the shared full-RD pickers) so the rate-cost component can be
// validated against libvpx VALUES in isolation.
//
// The pieces ported here are the rate halves of the intra RD decision:
//
//   - the keyframe Y-mode cost table  y_mode_costs[A][L]
//        = vp9_cost_tokens(vp9_kf_y_mode_prob[A][L], vp9_intra_mode_tree)
//        (libvpx vp9/encoder/vp9_rd.c:97-100, consumed at
//         vp9/encoder/vp9_rdopt.c:1379 inside rd_pick_intra_sby_mode).
//   - the inter-frame Y intra cost  mbmode_cost
//        = vp9_cost_tokens(fc->y_mode_prob[1], vp9_intra_mode_tree)
//        (libvpx vp9_rd.c:103, consumed at vp9_rdopt.c:3864 inside the intra
//         branch of vp9_rd_pick_inter_mode_sb as cpi->mbmode_cost[mi->mode]).
//        NOTE the FIXED size-group index 1 — the encoder's intra-mode RD cost
//        does *not* index y_mode_prob by size_group_lookup[bsize]; that table
//        only drives the bitstream writer (write_intra_mode). The previous
//        govpx inter-intra picker keyed the cost on
//        YModeProb[SizeGroupLookup[bsize]], which diverged from libvpx for
//        every block whose size group != 1 (BLOCK_16X16 and larger).
//   - the UV intra cost  intra_uv_mode_cost[frame_type][y_mode][uv_mode]
//        = vp9_cost_tokens(KEY  ? vp9_kf_uv_mode_prob[y_mode]
//                               : fc->uv_mode_prob[y_mode], vp9_intra_mode_tree)
//        (libvpx vp9_rd.c:104-108, consumed at vp9_rdopt.c:1496 inside
//         rd_pick_intra_sbuv_mode and at vp9_rdopt.c:1527 inside rd_sbuv_dcpred).
//
// The token-cost primitive itself (encoder.VP9CostTokens over
// common.IntraModeTree against a 9-entry probability row) is already
// exhaustively validated against a libvpx oracle by
// internal/vp9/encoder/token_cost_oracle_test.go and
// internal/vp9/common/trees_test.go. This file pins the *table selection* and
// the *RD comparison* that wrap that primitive.

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// vp9FullRDIntraUVFrameType selects which UV-mode probability family feeds
// intra_uv_mode_cost. libvpx indexes cpi->intra_uv_mode_cost by
// cpi->common.frame_type (KEY_FRAME=0 / INTER_FRAME=1); the KEY_FRAME slot is
// built from vp9_kf_uv_mode_prob and the INTER_FRAME slot from fc->uv_mode_prob
// (vp9/encoder/vp9_rd.c:104-108).
type vp9FullRDIntraUVFrameType int

const (
	vp9FullRDKeyFrame   vp9FullRDIntraUVFrameType = 0
	vp9FullRDInterFrame vp9FullRDIntraUVFrameType = 1
)

// vp9FullRDKeyframeYModeCosts fills costs[mode] with the keyframe Y-mode bit
// cost y_mode_costs[above][left], i.e.
//
//	vp9_cost_tokens(costs, vp9_kf_y_mode_prob[above][left], vp9_intra_mode_tree)
//
// This is the bmode_costs row that rd_pick_intra_sby_mode adds to the
// transform rate: this_rate = this_rate_tokenonly + bmode_costs[mode]
// (libvpx vp9/encoder/vp9_rdopt.c:1379, 1398; table built in vp9_rd.c:97-100).
//
// `above` / `left` are the neighbour intra modes from vp9_above_block_mode /
// vp9_left_block_mode (clamped to DC_PRED for inter / off-frame neighbours by
// the caller). costs must have room for common.IntraModes entries.
func vp9FullRDKeyframeYModeCosts(costs []int, above, left common.PredictionMode) {
	a := vp9FullRDClampIntraMode(above)
	l := vp9FullRDClampIntraMode(left)
	probs := tables.KfYModeProb[a][l]
	encoder.VP9CostTokens(costs, probs[:], common.IntraModeTree[:])
}

// vp9FullRDInterIntraYModeCosts fills costs[mode] with the inter-frame
// mbmode_cost used by the intra branch of vp9_rd_pick_inter_mode_sb:
//
//	vp9_cost_tokens(costs, fc->y_mode_prob[1], vp9_intra_mode_tree)
//
// (libvpx vp9/encoder/vp9_rd.c:103; consumed at vp9_rdopt.c:3864 as
// cpi->mbmode_cost[mi->mode]). The size-group index is the literal constant 1
// — NOT size_group_lookup[bsize]. costs must have room for common.IntraModes
// entries; fc is the frame context whose YModeProb mirrors fc->y_mode_prob
// (indexed [BLOCK_SIZE_GROUPS][9]).
func vp9FullRDInterIntraYModeCosts(costs []int, fc *vp9dec.FrameContext) {
	const mbmodeSizeGroup = 1 // libvpx vp9_rd.c:103 fc->y_mode_prob[1]
	row := fc.YModeProb[mbmodeSizeGroup]
	encoder.VP9CostTokens(costs, row[:], common.IntraModeTree[:])
}

// vp9FullRDIntraUVModeCosts fills costs[mode] with the UV intra bit cost
// intra_uv_mode_cost[frame_type][yMode][mode]:
//
//	KEY_FRAME:   vp9_cost_tokens(costs, vp9_kf_uv_mode_prob[yMode], tree)
//	INTER_FRAME: vp9_cost_tokens(costs, fc->uv_mode_prob[yMode], tree)
//
// (libvpx vp9/encoder/vp9_rd.c:104-108; consumed at vp9_rdopt.c:1496 inside
// rd_pick_intra_sbuv_mode as
// cpi->intra_uv_mode_cost[frame_type][xd->mi[0]->mode][mode]). costs must have
// room for common.IntraModes entries. `fc` carries fc->uv_mode_prob via its
// UvModeProb field; it is only consulted for the INTER_FRAME family.
func vp9FullRDIntraUVModeCosts(costs []int, frameType vp9FullRDIntraUVFrameType,
	yMode common.PredictionMode, fc *vp9dec.FrameContext,
) {
	y := vp9FullRDClampIntraMode(yMode)
	if frameType == vp9FullRDKeyFrame {
		probs := tables.KfUvModeProb[y]
		encoder.VP9CostTokens(costs, probs[:], common.IntraModeTree[:])
		return
	}
	row := fc.UvModeProb[y]
	encoder.VP9CostTokens(costs, row[:], common.IntraModeTree[:])
}

// vp9FullRDIntraModeRD expands libvpx's RDCOST(x->rdmult, x->rddiv, this_rate,
// this_distortion) for the intra-mode decision, where this_rate is the sum of
// the transform token-only rate and the intra-mode bit cost:
//
//	this_rate = this_rate_tokenonly + bmode_costs[mode];      // Y picker
//	this_rd   = RDCOST(x->rdmult, x->rddiv, this_rate, this_distortion);
//
// (libvpx vp9/encoder/vp9_rdopt.c:1398-1399 for the keyframe Y picker;
// vp9_rdopt.c:1494-1495 for the UV picker; the inter-frame intra branch builds
// rate2 = rate_y + cpi->mbmode_cost[mi->mode] + rate_uv_intra[uv_tx] at
// vp9_rdopt.c:3864 then scores it through the same RDCOST). rddiv is the
// libvpx constant RD_DIV_BITS (encoder.RDDivBits = 7).
//
// modeCost is bmode_costs[mode] (or mbmode_cost[mode]); tokenRate is the
// transform-domain rate (rate_tokenonly); distortion is the transform-domain
// distortion. Returns (rate, rdcost) so the caller can both track the winning
// rate and compare RD values, exactly as the libvpx pickers do.
func vp9FullRDIntraModeRD(rdmult, modeCost, tokenRate int, distortion uint64) (rate int, rd uint64) {
	rate = tokenRate + modeCost
	rd = encoder.RDCost(rdmult, encoder.RDDivBits, rate, distortion)
	return rate, rd
}

// vp9FullRDClampIntraMode mirrors the implicit invariant that the intra
// rate-cost tables are indexed by an intra prediction mode in
// [DC_PRED, TM_PRED]. libvpx never calls these with an out-of-range mode (the
// neighbour-mode helpers clamp inter/off-frame neighbours to DC_PRED); this
// guard keeps the Go table indexing panic-free for defensive callers and tests.
func vp9FullRDClampIntraMode(mode common.PredictionMode) common.PredictionMode {
	if mode < common.DcPred || int(mode) >= common.IntraModes {
		return common.DcPred
	}
	return mode
}
