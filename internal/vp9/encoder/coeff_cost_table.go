package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// coeff_cost_table.go ports libvpx v1.16.0 fill_token_costs
// (vp9/encoder/vp9_rd.c:135-152). The recursive token-tree walk that
// CoeffTreeTokenCost performs per coefficient is stable across every
// coefficient block in a frame (it depends only on the per-frame
// coefficient probability model), so libvpx precomputes it once into
//
//	x->token_costs[tx_size][type][ref][band][skipEOB?1:0][ctx][token]
//
// and the cost_coeffs / vp9_optimize_b hot loops do O(1) lookups. This
// file provides the same precomputed table; the cost values are produced
// by the identical vp9_cost_tokens / vp9_cost_tokens_skip expansion that
// CoeffTreeTokenCost runs, so the lookup is numerically byte-identical to
// the old per-coefficient recursion.

// CoeffModel is the per-(plane_type, ref) coefficient-probs leaf the cost
// machinery reads: cm->fc->coef_probs[tx_size][plane_type][ref], laid out
// as [band][ctx][UNCONSTRAINED_NODES]. Same shape as VP9TrellisCoefModel.
type CoeffModel = [vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes]uint8

// CoeffTokenCostTable holds the precomputed token-tree costs for one
// CoeffModel, indexed [band][ctx][skipEOB?1:0][token]. It mirrors the
// inner [band][2][COEFF_CONTEXTS][ENTROPY_TOKENS] block of libvpx's
// vp9_coeff_cost (note govpx orders ctx before skipEOB; the lookup helper
// hides the index order).
type CoeffTokenCostTable struct {
	cost [vp9dec.CoefBands][vp9dec.CoefContexts][2][EntropyTokens]int
}

// fillModelFullProbs expands a 3-node CoeffModel slot into the full
// ENTROPY_NODES probability row, mirroring vp9_model_to_full_probs (the
// pareto8 tail fill that CoeffTreeTokenCost performs inline).
func fillModelFullProbs(model []uint8) (full [EntropyNodes]uint8, ok bool) {
	if len(model) < UnconstrainedNodes || model[2] == 0 {
		return full, false
	}
	full[0] = model[0]
	full[1] = model[1]
	full[2] = model[2]
	tail := tables.Pareto8Full[model[2]-1]
	for i := range tail {
		full[3+i] = tail[i]
	}
	return full, true
}

// BuildCoeffTokenCostTable precomputes the token-tree cost table for one
// CoeffModel via the same vp9_cost_tokens / vp9_cost_tokens_skip expansion
// CoeffTreeTokenCost runs. The [..][token] entries are byte-identical to
// CoeffTreeTokenCost(model[band][ctx][:], skipEOB, token); slots with a
// zero pivot prob (model[2]==0) stay all-zero, matching the
// CoeffTreeTokenCost early-out.
func BuildCoeffTokenCostTable(model *CoeffModel) *CoeffTokenCostTable {
	t := &CoeffTokenCostTable{}
	if model == nil {
		return t
	}
	for band := 0; band < vp9dec.CoefBands; band++ {
		ctxCount := vp9dec.BandCoefContexts(band)
		for ctx := 0; ctx < ctxCount; ctx++ {
			full, ok := fillModelFullProbs(model[band][ctx][:])
			if !ok {
				continue
			}
			VP9CostTokens(t.cost[band][ctx][0][:], full[:], CoefTree[:])
			VP9CostTokensSkip(t.cost[band][ctx][1][:], full[:], CoefTree[:])
		}
	}
	return t
}

// Lookup returns the precomputed token-tree cost for (band, ctx, token),
// selecting the skip-EOB variant when skipEOB is true. It is the O(1)
// equivalent of trellisTokenCost / CoeffTreeTokenCost(model[band][ctx][:],
// skipEOB, token).
func (t *CoeffTokenCostTable) Lookup(band, ctx, token int, skipEOB bool) int {
	if t == nil || band < 0 || band >= vp9dec.CoefBands ||
		ctx < 0 || ctx >= vp9dec.CoefContexts ||
		token < 0 || token >= EntropyTokens {
		return 0
	}
	sel := 0
	if skipEOB {
		sel = 1
	}
	return t.cost[band][ctx][sel][token]
}

// FrameCoeffTokenCosts precomputes the token-cost tables for every
// (tx_size, plane_type, ref) the cost path indexes, mirroring the full
// extent of libvpx's fill_token_costs over x->token_costs. It is built
// once per frame from the frame's coefficient-probs model and shared by
// cost_coeffs (CoeffBlockRateCost) and the trellis (VP9OptimizeB).
type FrameCoeffTokenCosts struct {
	tables [common.TxSizes][vp9dec.CoefPlaneTypes][vp9dec.CoefRefTypes]*CoeffTokenCostTable
}

// BuildFrameCoeffTokenCosts precomputes the token-cost tables for all
// (tx_size, plane_type, ref) slots from the supplied per-tx-size coef-probs
// model, exactly as fill_token_costs iterates t/i/j.
func BuildFrameCoeffTokenCosts(fc *vp9dec.FrameCoefProbs) *FrameCoeffTokenCosts {
	f := &FrameCoeffTokenCosts{}
	if fc == nil {
		return f
	}
	for tx := common.TxSize(0); tx < common.TxSizes; tx++ {
		for pt := 0; pt < vp9dec.CoefPlaneTypes; pt++ {
			for ref := 0; ref < vp9dec.CoefRefTypes; ref++ {
				f.tables[tx][pt][ref] =
					BuildCoeffTokenCostTable(&fc[tx][pt][ref])
			}
		}
	}
	return f
}

// Table returns the precomputed CoeffTokenCostTable for (tx_size,
// plane_type, ref), or nil when out of range.
func (f *FrameCoeffTokenCosts) Table(tx common.TxSize, planeType, ref int,
) *CoeffTokenCostTable {
	if f == nil || tx < 0 || tx >= common.TxSizes ||
		planeType < 0 || planeType >= vp9dec.CoefPlaneTypes ||
		ref < 0 || ref >= vp9dec.CoefRefTypes {
		return nil
	}
	return f.tables[tx][planeType][ref]
}
