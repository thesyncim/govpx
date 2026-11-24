package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_inter_sub8x8_intra.go ports the INTRA evaluation that libvpx's
// vp9_rd_pick_inter_mode_sub8x8 runs for the INTRA_FRAME reference (ref_index 5
// in vp9_ref_order, i.e. evaluated AFTER every inter ref). The two pieces are:
//
//   - rd_pick_intra_sub_8x8_y_mode (vp9/encoder/vp9_rdopt.c:1299-1360): the
//     per-4x4 sub-block intra Y mode search across the 8x8 (driving
//     rd_pick_intra4x4block per sub-block of size 4x4 / 8x4 / 4x8). On an inter
//     frame the per-sub-block bmode_costs row is the FIXED cpi->mbmode_cost
//     (= cost_tokens(fc->y_mode_prob[1]), vp9_rd.c:103) — NOT the keyframe
//     context-keyed cpi->y_mode_costs[A][L] (that override only runs when
//     cpi->common.frame_type == KEY_FRAME, vp9_rdopt.c:1325-1330).
//   - choose_intra_uv_mode (vp9_rdopt.c:1531-1549) → rd_pick_intra_sbuv_mode
//     (vp9_rdopt.c:1468-1512) over max(bsize, BLOCK_8X8) at TX_4X4, with the
//     inter-frame UV cost intra_uv_mode_cost[INTER_FRAME][y_mode][uv_mode]
//     (= cost_tokens(fc->uv_mode_prob[y_mode]), vp9_rd.c:107-108).
//
// The wrapper (rdPickInterModeSub8x8) calls rdPickInterSub8x8IntraYMode after
// the inter ref loop, then sums Y + UV rate/dist + intra_cost_penalty + the
// INTRA ref-frame signalling cost + the no-skip flag, applies RDCOST, and
// commits the intra mode when it beats the running best (mi->mode = the chosen
// intra mode, ref_frame[0] = INTRA_FRAME, mi->interp_filter = SWITCHABLE_FILTERS,
// mi->mv[0] = 0). GATED behind vp9InterUseDeepRDSub8x8 with the rest of the
// wrapper; production keeps the model stand-in.

// vp9Sub8x8IntraYResult is rd_pick_intra_sub_8x8_y_mode's output: the committed
// per-sub-block bmi quartet + the running rate/dist + the post-coding plane[0]
// entropy context (t_above[2]/t_left[2]) for the next-sibling stamp.
type vp9Sub8x8IntraYResult struct {
	bmi        [4]vp9dec.Bmi
	mode       common.PredictionMode // mic->mode = bmi[3].as_mode
	rate       int                   // *rate  = cost (incl. mbmode_cost per sub-block)
	rateY      int                   // *rate_y = tot_rate_y (token-only)
	distortion uint64                // *distortion = total_distortion
	segEntropy vp9Sub8x8SegmentEntropy
	rd         uint64 // RDCOST(rdmult, rddiv, cost, total_distortion)
	valid      bool
}

// rdPickInterSub8x8IntraYMode ports rd_pick_intra_sub_8x8_y_mode for the
// inter-frame case (vp9_rdopt.c:1299-1360). bsize is the sub-8x8 shape
// (BLOCK_4X4 / 8X4 / 4X8); the footprint is the 8x8 at (miRow, miCol).
// bestRD is the running best_rd (the inter best so far) used as the per-sub-block
// early-exit budget. Returns valid=false when total_rd reaches best_rd (the
// libvpx INT64_MAX early-exit at vp9_rdopt.c:1337,1350).
func (e *VP9Encoder) rdPickInterSub8x8IntraYMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, rdmult int, bestRD uint64,
) (vp9Sub8x8IntraYResult, bool) {
	if inter == nil || mi == nil || bsize >= common.Block8x8 {
		return vp9Sub8x8IntraYResult{}, false
	}
	keyLike := e.vp9InterIntraKeyframeState(inter)
	if keyLike.dq == nil || keyLike.img == nil {
		return vp9Sub8x8IntraYResult{}, false
	}

	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])

	// libvpx vp9_rdopt.c:1318 + 1316-1330 — bmode_costs = cpi->mbmode_cost for
	// inter frames (the frame_type==KEY_FRAME override at :1325-1330 does NOT
	// fire on an inter frame). mbmode_cost = cost_tokens(fc->y_mode_prob[1]).
	var bmodeCosts [common.IntraModes]int
	vp9FullRDInterIntraYModeCosts(bmodeCosts[:], &inter.selectFc)

	// rd_pick_intra4x4block pins mi->tx_size = TX_4X4 (vp9_rdopt.c:1088).
	mi.TxSize = common.Tx4x4

	// Seed t_above[2]/t_left[2] from the live plane[0] entropy context (the
	// sibling-stamped seed), then thread it across the per-sub-block grid exactly
	// as libvpx passes xd->plane[0].above_context + idx / left_context + idy into
	// rd_pick_intra4x4block (vp9_rdopt.c:1334) and that function writes the winning
	// tempa/templ back through those pointers (vp9_rdopt.c:1280-1282).
	var seed vp9Sub8x8SegmentEntropy
	e.vp9Sub8x8SeedEntropy(&seed, miRow, miCol)
	aboveCtx := seed.above
	leftCtx := seed.left

	// e.cbRdmult / pickVP9Sub4x4IntraBlockMode path read the dequant from
	// key.dq.Y[segID]; rdmult feeds the per-sub-block RDCOST. Mirror the keyframe
	// driver's cbRdmult prime so any downstream cbRdmult read is the inter rdmult.
	prevCbRdmult := e.cbRdmult
	e.cbRdmult = rdmult
	defer func() { e.cbRdmult = prevCbRdmult }()

	var total vp9Sub8x8IntraYResult
	totalRD := uint64(0)
	// libvpx vp9_rdopt.c:1319-1320 — for (idy=0; idy<2; idy+=num_4x4_high)
	// for (idx=0; idx<2; idx+=num_4x4_wide).
	for idy := 0; idy < 2; idy += num4x4H {
		for idx := 0; idx < 2; idx += num4x4W {
			i := idy*2 + idx
			// best_rd - total_rd is the per-sub-block budget (vp9_rdopt.c:1335).
			remainingRD := ^uint64(0)
			if bestRD != ^uint64(0) {
				if totalRD >= bestRD {
					return vp9Sub8x8IntraYResult{}, false
				}
				remainingRD = bestRD - totalRD
			}
			bestMode, rd, ok := e.pickVP9Sub4x4IntraBlockMode(&keyLike, tile,
				miRows, miCols, miRow, miCol, bsize, mi, idy, idx, bmodeCosts[:],
				rdmult, aboveCtx[idx:idx+num4x4W], leftCtx[idy:idy+num4x4H],
				remainingRD)
			if !ok {
				return vp9Sub8x8IntraYResult{}, false
			}
			thisRD := encoder.RDCost(rdmult, encoder.RDDivBits, rd.rate, rd.distortion)
			// libvpx vp9_rdopt.c:1337 — if (this_rd >= best_rd - total_rd) return
			// INT64_MAX.
			if remainingRD != ^uint64(0) && thisRD >= remainingRD {
				return vp9Sub8x8IntraYResult{}, false
			}
			totalRD += thisRD
			total.rate += rd.rate
			total.rateY += rd.rateTokenOnly
			total.distortion += rd.distortion
			// libvpx vp9_rdopt.c:1344-1348 — replicate best_mode into the bmi cells
			// the sub-block covers.
			mi.Bmi[i].AsMode = bestMode
			for j := 1; j < num4x4H; j++ {
				mi.Bmi[i+j*2].AsMode = bestMode
			}
			for j := 1; j < num4x4W; j++ {
				mi.Bmi[i+j].AsMode = bestMode
			}
			// libvpx vp9_rdopt.c:1350 — if (total_rd >= best_rd) return INT64_MAX.
			if bestRD != ^uint64(0) && totalRD >= bestRD {
				return vp9Sub8x8IntraYResult{}, false
			}
		}
	}
	// libvpx vp9_rdopt.c:1357 — mic->mode = mic->bmi[3].as_mode.
	mi.Mode = mi.Bmi[3].AsMode
	total.mode = mi.Mode
	total.bmi = mi.Bmi
	total.segEntropy = vp9Sub8x8SegmentEntropy{above: aboveCtx, left: leftCtx}
	total.rd = encoder.RDCost(rdmult, encoder.RDDivBits, total.rate, total.distortion)
	total.valid = true
	return total, true
}

// vp9Sub8x8IntraUVResult is choose_intra_uv_mode's output: the chosen chroma
// mode + its rate/dist for the 8x8 (BLOCK_8X8) chroma extent.
type vp9Sub8x8IntraUVResult struct {
	mode          common.PredictionMode
	rate          int    // rate_uv_intra (this_rate = tokenonly + uv mode cost)
	rateTokenOnly int    // rate_uv (rate_uv_tokenonly)
	distortion    uint64 // dist_uv
	skippable     bool
}

// vp9Sub8x8IntraUVRD ports choose_intra_uv_mode (vp9_rdopt.c:1531-1549) →
// rd_pick_intra_sbuv_mode (vp9_rdopt.c:1468-1512) for the inter-frame sub-8x8
// intra path: a full super_block_uvrd search over BLOCK_8X8 at TX_4X4 with the
// inter-frame UV cost intra_uv_mode_cost[INTER_FRAME][yMode][mode]. yMode is the
// chosen Y mode (mi->mode after rd_pick_intra_sub_8x8_y_mode) which the UV cost
// table is keyed on (vp9_rdopt.c:1496 cpi->intra_uv_mode_cost[...][xd->mi[0]->mode]).
func (e *VP9Encoder) vp9Sub8x8IntraUVRD(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	yMode common.PredictionMode, rdmult int,
) (vp9Sub8x8IntraUVResult, bool) {
	if inter == nil {
		return vp9Sub8x8IntraUVResult{}, false
	}
	keyLike := e.vp9InterIntraKeyframeState(inter)
	if keyLike.dq == nil || keyLike.img == nil {
		return vp9Sub8x8IntraUVResult{}, false
	}
	if int(yMode) < 0 || int(yMode) >= common.IntraModes {
		yMode = common.DcPred
	}
	// libvpx vp9_rdopt.c:1540/1545 — rd_pick_intra_sbuv_mode receives
	// max(bsize, BLOCK_8X8); for a sub-8x8 partition that is BLOCK_8X8.
	const uvBsize = common.Block8x8
	maxTxSize := common.MaxTxsizeLookup[uvBsize]
	uvMask := e.sf.IntraUvModeMask[maxTxSize]
	if uvMask == 0 {
		uvMask = sfIntraAll
	}
	// libvpx vp9_rd.c:107-108 — intra_uv_mode_cost[INTER_FRAME][y_mode] =
	// cost_tokens(fc->uv_mode_prob[y_mode]).
	var uvModeCosts [common.IntraModes]int
	vp9FullRDIntraUVModeCosts(uvModeCosts[:], vp9FullRDInterFrame, yMode,
		&inter.selectFc)

	// The UV transform predictor needs an mi whose Mode (Y), TxSize and SbType
	// drive get_uv_tx_size / the chroma neighbour context. mi->sb_type stays the
	// sub-8x8 shape for the predictor; uv_tx_size from BLOCK_8X8 + TX_4X4 = TX_4X4.
	mi := vp9dec.NeighborMi{
		SbType:   uvBsize,
		TxSize:   common.Tx4x4,
		Mode:     yMode,
		RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame},
	}

	prevCbRdmult := e.cbRdmult
	e.cbRdmult = rdmult
	defer func() { e.cbRdmult = prevCbRdmult }()

	useTxDomainDistortion := e.vp9KeyframeUseTransformDomainDistortion(&keyLike,
		miRows, miCols, miRow, miCol, uvBsize)

	best := vp9Sub8x8IntraUVResult{mode: common.DcPred}
	bestRD := ^uint64(0)
	bestValid := false
	for mode := common.DcPred; mode <= common.TmPred; mode++ {
		if uvMask&(1<<uint(mode)) == 0 {
			continue
		}
		bestRDForMode := bestRD
		bestRDValid := bestValid
		coeffRate, distortion, skippable, ok := e.scoreVP9KeyframeUvModeTransformRD(
			&keyLike, mode, uvBsize, tile, miRows, miCols, miRow, miCol, &mi,
			useTxDomainDistortion, rdmult, uvModeCosts[mode], bestRDForMode,
			bestRDValid)
		if !ok {
			continue
		}
		thisRate := coeffRate + uvModeCosts[mode]
		thisRD := encoder.RDCost(rdmult, encoder.RDDivBits, thisRate, distortion)
		if !bestValid || thisRD < bestRD {
			bestRD = thisRD
			best = vp9Sub8x8IntraUVResult{
				mode:          mode,
				rate:          thisRate,
				rateTokenOnly: coeffRate,
				distortion:    distortion,
				skippable:     skippable,
			}
			bestValid = true
		}
	}
	if !bestValid {
		return vp9Sub8x8IntraUVResult{}, false
	}
	return best, true
}
