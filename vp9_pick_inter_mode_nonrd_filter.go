package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func vp9NonrdModeRDThresh(qindex int, bsize common.BlockSize,
	refFrame int8, mode common.PredictionMode, adaptiveRDThresh int,
	bestModeSkipTxfm bool, biasGolden bool, framesSinceGolden int,
) int64 {
	if bsize < 0 || bsize >= common.BlockSizes ||
		refFrame <= vp9dec.IntraFrame || refFrame >= vp9dec.MaxRefFrames ||
		mode < common.NearestMv || mode > common.NewMv {
		return 0
	}
	modeOffset := vp9ModeOffsetInter(mode)
	modeIndex := vp9ModeIdxTable[refFrame][modeOffset]
	threshMult := vp9NonrdThreshMult(modeIndex, adaptiveRDThresh)
	if threshMult <= 0 {
		return 0
	}
	threshFactor := vp9ComputeRDThreshFactor(qindex)
	t := int64(threshFactor) * int64(vp9RDThreshBlockSizeFactor[bsize])
	thresh := int64(threshMult) * t / 4
	if bestModeSkipTxfm {
		thresh <<= 1
	}
	if biasGolden && refFrame == vp9dec.GoldenFrame && framesSinceGolden > 4 {
		thresh <<= 3
	}
	return thresh
}

func vp9NonrdThreshMult(modeIndex vp9ThrModes, adaptiveRDThresh int) int {
	switch modeIndex {
	case vp9ThrNearestMV, vp9ThrNearestG, vp9ThrNearestA:
		if adaptiveRDThresh != 0 {
			return 300
		}
		return 0
	case vp9ThrNewMV, vp9ThrNewG, vp9ThrNewA,
		vp9ThrNearMV, vp9ThrNearG, vp9ThrNearA:
		return 1000
	case vp9ThrZeroMV, vp9ThrZeroG, vp9ThrZeroA:
		return 2000
	default:
		return 0
	}
}

// vp9SearchFilterRef is the verbatim port of libvpx's search_filter_ref
// (vp9_pickmode.c:1499-1584). It runs the inter predictor for each filter in
// [filter_start, filter_end] (typically {EIGHTTAP, EIGHTTAP_SMOOTH} in the
// realtime path), scores each via model_rd_for_sb_y + vp9_get_switchable_rate
// using the libvpx-faithful Lagrangian RDCOST, and returns the winning filter
// together with the (rate, dist, var, sse, tx_size) tuple at that filter.
//
// This is the per-block filter histogram path: libvpx's filter choice varies
// across blocks because model_rd_for_sb_y combines variance + sse with the
// quantizer-aware DC/AC rate model, producing per-filter cost orderings that
// can flip between neighbouring blocks even when the raw SSE delta is small.
// The previous govpx legacy proxy used raw SSE per filter, which collapsed the
// histogram to a single dominant filter (counts.SwitchableInterp c==1) on the
// {0x32} cpu=-8 RT speed=8 64x64 seed; libvpx emits c>=2 for the same seed
// because the model_rd-driven race wins different filters on different blocks
// (see vp9_oracle_encoder_runtime_controls_fuzz_test.go {0x32} entry).
//
// libvpx: vp9/encoder/vp9_pickmode.c:1499-1584 search_filter_ref.
//
//	for (filter = filter_start; filter <= filter_end; ++filter) {
//	    int64_t cost;
//	    mi->interp_filter = filter;
//	    vp9_build_inter_predictors_sby(xd, mi_row, mi_col, bsize);
//	    if (use_model_yrd_large)
//	      model_rd_for_sb_y_large(cpi, bsize, x, xd, &pf_rate[filter],
//	                              &pf_dist[filter], &pf_var[filter],
//	                              &pf_sse[filter], mi_row, mi_col,
//	                              this_early_term, flag_preduv_computed);
//	    else
//	      model_rd_for_sb_y(cpi, bsize, x, xd, &pf_rate[filter],
//	                        &pf_dist[filter], &pf_var[filter],
//	                        &pf_sse[filter], 0);
//	    curr_rate[filter] = pf_rate[filter];
//	    pf_rate[filter] += vp9_get_switchable_rate(cpi, xd);
//	    cost = RDCOST(x->rdmult, x->rddiv, pf_rate[filter], pf_dist[filter]);
//	    pf_tx_size[filter] = mi->tx_size;
//	    if (cost < best_cost) {
//	      best_filter = filter;
//	      best_cost = cost;
//	      ...
//	    }
//	}
//
// govpx differences:
//   - use_model_yrd_large is FALSE for the deferred RuntimeControls seed #8
//     (VBR, base_qindex non-zero but rc_mode != VPX_CBR — see vp9_pickmode.c:
//     2045-2048). The large-block model_rd kernel is gated to that path
//     specifically; this helper invokes the plain encoder.ModelRdForSbY mirror.
//     When the use_model_yrd_large port lands it will be wired here behind
//     the same large_block + CBR + base_qindex gate.
//
// The candidates slice supplies the filter sweep ([filter_start..filter_end]).
// Caller is responsible for the gate from vp9_pickmode.c:2318-2330 — this
// helper only runs the sweep, it does not check pred_filter_search /
// (mode == NEWMV || filter_ref == SWITCHABLE) / subpel-MV / LAST_FRAME etc.
func (e *VP9Encoder) vp9SearchFilterRef(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8, mv vp9dec.MV,
	candidates []vp9dec.InterpFilter, switchableCtx int,
	dequant [2]int16, qindex int,
) (bestFilter vp9dec.InterpFilter, bestVarY, bestSseY uint64,
	bestRate int, bestDist int64, bestTxSize common.TxSize, ok bool,
) {
	if len(candidates) == 0 {
		return 0, 0, 0, 0, 0, 0, false
	}
	// libvpx: vp9_pickmode.c:1517 int64_t best_cost = INT64_MAX;
	bestCost := uint64(1<<63 - 1)
	bestFilter = candidates[0]
	rdmult := e.activeRDMult(qindex)
	for _, filter := range candidates {
		// libvpx: vp9_pickmode.c:1527-1528 mi->interp_filter = filter;
		//                                  vp9_build_inter_predictors_sby(...).
		// govpx fuses both into vp9InterPredictionVarianceSSE which assigns
		// the filter to the synthetic NeighborMi, builds the predictor, then
		// returns (var, sse) via vp9BlockDiffVarianceSSE (libvpx's
		// fn_ptr[bsize].vf inside model_rd_for_sb_y at vp9_pickmode.c:661-666).
		varY, sseY, vok := e.vp9InterPredictionVarianceSSE(inter, miRows,
			miCols, miRow, miCol, bsize, mode, refFrame, mv, filter)
		if !vok {
			continue
		}
		// libvpx: vp9_pickmode.c:1530-1537 model_rd_for_sb_y(_large).
		rateY, distY, _, mrdTxSize := encoder.ModelRdForSbY(bsize, qindex, dequant,
			varY, sseY, 0)
		// libvpx: vp9_pickmode.c:1538 curr_rate[filter] = pf_rate[filter];
		// (curr_rate captures the pre-switchable rate so the caller can
		// commit the model_rd rate without double-counting the switchable
		// bit cost. govpx returns the curr_rate equivalent — rateY here —
		// so the caller can fold it into the picker's outer (rate, dist)
		// tuple without re-applying vp9_get_switchable_rate.)
		// libvpx: vp9_pickmode.c:1539 pf_rate[filter] +=
		//   vp9_get_switchable_rate(cpi, xd);
		filterRate := rateY + vp9SwitchableInterpRateCost(
			vp9InterModeCostFrameContext(inter),
			switchableCtx, filter)
		// libvpx: vp9_pickmode.c:1540 cost = RDCOST(x->rdmult, x->rddiv,
		//   pf_rate[filter], pf_dist[filter]);
		// govpx vp9RDCost is the verbatim port (vp9_rd.h:29-30 RDCOST
		// macro) — rdmult * rate + (dist << rddiv_bits).
		cost := vp9RDCost(rdmult, vp9RDDivBits, filterRate, uint64(distY))
		// libvpx: vp9_pickmode.c:1541 pf_tx_size[filter] = mi->tx_size;
		// libvpx: vp9_pickmode.c:1542 if (cost < best_cost) ...
		if !ok || cost < bestCost {
			bestFilter = filter
			bestCost = cost
			bestVarY = varY
			bestSseY = sseY
			bestRate = rateY
			bestDist = distY
			bestTxSize = mrdTxSize
			ok = true
		}
	}
	return bestFilter, bestVarY, bestSseY, bestRate, bestDist, bestTxSize, ok
}
