package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// Full-RD transform-size search support, ported verbatim from libvpx
// v1.16.0 vp9/encoder/vp9_rdopt.c. This file factors out the
// choose_tx_size_from_rd (vp9_rdopt.c:907-1023) per-candidate
// rate+distortion accumulation and the TX_4X4..TX_32X32 RDCOST
// comparison so it can be exercised by libvpx-value unit tests without
// editing the shared full-RD encode files. The intra keyframe and inter
// pickers can adopt FullRDChooseTxSize once their per-tx-size
// (rate,dist,skip,sse) producers are wired through txfm_rd_in_plane.

// FullRDTxCandidate carries the per-tx-size output of libvpx
// txfm_rd_in_plane (vp9_rdopt.c:854-889): r[n][0] (the coefficient
// rate), d[n] (distortion), s[n] (skippable flag), sse[n]. A candidate
// is "exit_early"/invalid when txfm_rd_in_plane sets rate=INT_MAX /
// dist=INT64_MAX; mark it with Valid=false to reproduce the
// rd[n]=INT64_MAX path (vp9_rdopt.c:973-974).
type FullRDTxCandidate struct {
	// Valid is false when txfm_rd_in_plane reported exit_early (rate
	// == INT_MAX). libvpx vp9_rdopt.c:973-974 then pins rd to
	// INT64_MAX for that tx_size.
	Valid bool
	// Rate is r[n][0]: the coefficient-block rate from txfm_rd_in_plane
	// (cost_coeffs accumulation), excluding the tx_size signalling cost.
	Rate int
	// Dist is d[n]: the distortion (pixel_sse * 16, or transform-domain
	// SSE) from block_rd_txfm (vp9_rdopt.c:766-768 / 826).
	Dist uint64
	// Skip is s[n]: the per-tx-size skippable flag args.skippable.
	Skip bool
	// SSE is sse[n]: the residual SSE accumulated by block_rd_txfm.
	SSE uint64
}

// FullRDTxResult is the choose_tx_size_from_rd output (vp9_rdopt.c:
// 1004-1009): the selected tx_size plus the rate/distortion/skip/sse it
// reports back to super_block_yrd's caller.
type FullRDTxResult struct {
	TxSize     common.TxSize
	Rate       int
	Dist       uint64
	Skip       bool
	SSE        uint64
	BestRDCost uint64
}

const rdCostMax = ^uint64(0)

// txSizeRDCost expands libvpx RDCOST(x->rdmult, x->rddiv, rate, dist)
// (vp9_rdopt.c via vp9_rd.h:29-30) saturating at the INT64_MAX sentinel
// the C uses for invalid candidates.
func txSizeRDCost(rdmult, rddiv, rate int, dist uint64) uint64 {
	if dist == rdCostMax {
		return rdCostMax
	}
	return RDCost(rdmult, rddiv, rate, dist)
}

// FullRDChooseTxSize is a verbatim port of the candidate-selection body
// of libvpx choose_tx_size_from_rd (vp9/encoder/vp9_rdopt.c:946-1009).
// It takes the per-tx-size txfm_rd_in_plane outputs (one entry per
// TX_SIZE, index 0==TX_4X4 .. 3==TX_32X32), the tx_size signalling cost
// row cpi->tx_size_cost[max_tx_size-1][tx_size_ctx][n] (precomputed via
// FullRDTxSizeCostRow), and the frame RD state, and returns the
// best_tx / rate / distortion / skip / sse choose_tx_size_from_rd would
// write back. isInter selects the is_inter_block(mi) branches at
// vp9_rdopt.c:976-991. lossless mirrors xd->lossless at line 988.
//
//	for (n = start_tx; n >= end_tx; n--) {
//	  const int r_tx_size = cpi->tx_size_cost[max_tx_size-1][ctx][n];
//	  ... txfm_rd_in_plane(...) -> r[n][0], d[n], s[n], sse[n]
//	  r[n][1] = r[n][0];
//	  if (r[n][0] < INT_MAX) r[n][1] += r_tx_size;
//	  if (d[n]==INT64_MAX || r[n][0]==INT_MAX) rd[n][0]=rd[n][1]=INT64_MAX;
//	  else if (s[n]) { ... } else { rd[n][0]=RDCOST(r[n][0]+s0,d[n]); rd[n][1]=RDCOST(r[n][1]+s0,d[n]); }
//	  if (is_inter && !lossless && !s[n] && sse[n]!=INT64_MAX) {
//	    rd[n][0]=VPXMIN(rd[n][0],RDCOST(s1,sse[n]));
//	    rd[n][1]=VPXMIN(rd[n][1],RDCOST(s1,sse[n]));
//	  }
//	  if (breakout && (rd[n][1]==INT64_MAX ||
//	      (n<max_tx_size && rd[n][1]>rd[n+1][1]) || s[n]==1)) break;
//	  if (rd[n][1] < best_rd) { best_tx=n; best_rd=rd[n][1]; }
//	}
//
// s0/s1 are vp9_cost_bit(skip_prob, 0/1) (vp9_rdopt.c:943-944).
// refBestRD seeds best_rd (vp9_rdopt.c:924). txModeSelect selects the
// rate index r[n][cm->tx_mode==TX_MODE_SELECT] reported back (line
// 1007); pass txModeSelect=true for the TX_MODE_SELECT case.
func FullRDChooseTxSize(
	cand [common.TxSizes]FullRDTxCandidate,
	txSizeCostRow [common.TxSizes]int,
	maxTxSize common.TxSize,
	startTx, endTx int,
	rdmult, rddiv, s0, s1 int,
	isInter, lossless, breakout, txModeSelect bool,
	refBestRD uint64,
) FullRDTxResult {
	var r [common.TxSizes][2]int
	var d [common.TxSizes]uint64
	var s [common.TxSizes]bool
	var sse [common.TxSizes]uint64
	rd := [common.TxSizes][2]uint64{
		{rdCostMax, rdCostMax}, {rdCostMax, rdCostMax},
		{rdCostMax, rdCostMax}, {rdCostMax, rdCostMax},
	}

	bestRD := refBestRD
	bestTx := int(maxTxSize)

	for n := startTx; n >= endTx; n-- {
		if n < 0 || n >= int(common.TxSizes) {
			continue
		}
		rTxSize := txSizeCostRow[n]
		c := cand[n]
		// txfm_rd_in_plane outputs (vp9_rdopt.c:963-967). exit_early
		// maps to !Valid -> r[n][0]=INT_MAX, d[n]=INT64_MAX.
		if !c.Valid {
			// r[n][0] == INT_MAX: line 970 leaves r[n][1]=r[n][0];
			// line 973 sets rd[n][*]=INT64_MAX. We skip selection;
			// d/sse stay 0 but are never read for this n.
			s[n] = false
			rd[n][0] = rdCostMax
			rd[n][1] = rdCostMax
		} else {
			r[n][0] = c.Rate
			d[n] = c.Dist
			s[n] = c.Skip
			sse[n] = c.SSE

			r[n][1] = r[n][0]
			// vp9_rdopt.c:970-972 — r[n][0] < INT_MAX here (Valid).
			r[n][1] += rTxSize

			switch {
			case s[n]:
				// vp9_rdopt.c:975-986 — skippable branch.
				if isInter {
					rd[n][0] = txSizeRDCost(rdmult, rddiv, s1, sse[n])
					rd[n][1] = rd[n][0]
					r[n][1] -= rTxSize
				} else {
					rd[n][0] = txSizeRDCost(rdmult, rddiv, s1, sse[n])
					rd[n][1] = txSizeRDCost(rdmult, rddiv, s1+rTxSize, sse[n])
				}
			default:
				// vp9_rdopt.c:983-986 — non-skip branch.
				rd[n][0] = txSizeRDCost(rdmult, rddiv, r[n][0]+s0, d[n])
				rd[n][1] = txSizeRDCost(rdmult, rddiv, r[n][1]+s0, d[n])
			}
		}

		// vp9_rdopt.c:988-991 — inter non-skip sse floor.
		if isInter && !lossless && !s[n] && c.Valid && sse[n] != rdCostMax {
			floor := txSizeRDCost(rdmult, rddiv, s1, sse[n])
			if floor < rd[n][0] {
				rd[n][0] = floor
			}
			if floor < rd[n][1] {
				rd[n][1] = floor
			}
		}

		// vp9_rdopt.c:993-997 — tx_size_search_breakout.
		if breakout {
			if rd[n][1] == rdCostMax ||
				(n < int(maxTxSize) && rd[n][1] > rd[n+1][1]) ||
				s[n] {
				break
			}
		}

		// vp9_rdopt.c:999-1002.
		if rd[n][1] < bestRD {
			bestTx = n
			bestRD = rd[n][1]
		}
	}

	// vp9_rdopt.c:1004-1009.
	rateIdx := 0
	if txModeSelect {
		rateIdx = 1
	}
	return FullRDTxResult{
		TxSize:     common.TxSize(bestTx),
		Rate:       r[bestTx][rateIdx],
		Dist:       d[bestTx],
		Skip:       s[bestTx],
		SSE:        sse[bestTx],
		BestRDCost: bestRD,
	}
}

// FullRDTxSizeCostRow precomputes cpi->tx_size_cost[max_tx_size-1][ctx]
// — the per-tx_size signalling cost row choose_tx_size_from_rd reads at
// vp9_rdopt.c:958. It is the verbatim libvpx fill loop from
// vp9/encoder/vp9_rd.c:116-132:
//
//	for (i = TX_8X8; i < TX_SIZES; ++i)
//	  for (j = 0; j < TX_SIZE_CONTEXTS; ++j) {
//	    const vpx_prob *tx_probs = get_tx_probs(i, j, &fc->tx_probs);
//	    for (k = 0; k <= i; ++k) {
//	      int cost = 0;
//	      for (m = 0; m <= k - (k == i); ++m) {
//	        if (m == k) cost += vp9_cost_zero(tx_probs[m]);
//	        else        cost += vp9_cost_one(tx_probs[m]);
//	      }
//	      cpi->tx_size_cost[i-1][j][k] = cost;
//	    }
//	  }
//
// txProbsRow is get_tx_probs(maxTxSize, ctx): the prob row for the
// frame's max_tx_size. The returned array is indexed by tx_size
// (0==TX_4X4); entries for tx_size > maxTxSize are 0 (never signalled /
// never read).
func FullRDTxSizeCostRow(txProbsRow []uint8, maxTxSize common.TxSize) [common.TxSizes]int {
	var row [common.TxSizes]int
	i := int(maxTxSize)
	for k := 0; k <= i && k < int(common.TxSizes); k++ {
		// m goes 0 .. k - (k==i).
		upper := k
		if k == i {
			upper = k - 1
		}
		cost := 0
		for m := 0; m <= upper; m++ {
			if m >= len(txProbsRow) {
				break
			}
			if m == k {
				cost += VP9CostZero(txProbsRow[m])
			} else {
				cost += VP9CostOne(txProbsRow[m])
			}
		}
		row[k] = cost
	}
	return row
}
