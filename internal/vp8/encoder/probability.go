package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/bitstream.c coefficient probability
// update selection and vp8/common/treecoder.c branch-count probability fitting.

type CoefficientProbabilityUpdates struct {
	Probs       tables.CoefficientProbs
	Update      [tables.BlockTypes][tables.CoefBands][tables.PrevCoefContexts][tables.EntropyNodes]bool
	UpdateCount int
	SavingsBits int
}

type coefficientBranchCounts [tables.BlockTypes][tables.CoefBands][tables.PrevCoefContexts][tables.EntropyNodes][2]int

func WriteCoefficientProbabilityUpdates(w *BoolWriter, updates *CoefficientProbabilityUpdates) error {
	if w == nil {
		return ErrInvalidPacketConfig
	}
	for block := range tables.BlockTypes {
		for band := range tables.CoefBands {
			for ctx := range tables.PrevCoefContexts {
				for node := range tables.EntropyNodes {
					update := updates != nil && updates.Update[block][band][ctx][node]
					if update {
						prob := updates.Probs[block][band][ctx][node]
						if prob == 0 {
							return ErrInvalidPacketConfig
						}
						w.WriteBool(1, tables.CoefUpdateProbs[block][band][ctx][node])
						w.WriteLiteral(uint32(prob), 8)
					} else {
						w.WriteBool(0, tables.CoefUpdateProbs[block][band][ctx][node])
					}
				}
			}
		}
	}
	return w.Err()
}

// BuildKeyFrameCoefficientProbabilityUpdates ports the default
// (non-error-resilient) branch of libvpx vp8_update_coef_probs for key frames.
// The libvpx default-path loop emits an update only when
// prob_update_savings > 0 for that (i,j,k,t); the per-(k,t) "force when
// newp != *Pold on key frames" branch lives behind
// VPX_ERROR_RESILIENT_PARTITIONS (bitstream.c:924-928) and is handled by
// BuildKeyFrameCoefficientProbabilityUpdatesIndependent. The libvpx default
// path treats key frames identically to inter frames at the savings step, so
// no extra force-emit is applied here — matching the bitstream libvpx writes.
func BuildKeyFrameCoefficientProbabilityUpdates(rows int, cols int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs) (tables.CoefficientProbs, CoefficientProbabilityUpdates, error) {
	var counts coefficientBranchCounts
	if err := buildKeyFrameCoefficientBranchCounts(rows, cols, modes, coeffs, above, base, &counts); err != nil {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, err
	}
	return coefficientProbabilityUpdatesFromCounts(base, &counts)
}

// BuildKeyFrameCoefficientProbabilityUpdatesIndependent ports the key-frame
// branch of libvpx independent_coef_context_savings / vp8_update_coef_probs.
// Libvpx resets key-frame independent-context probability fitting to
// default_coef_counts, so the emitted updates are intentionally independent of
// the current frame's coefficient content.
func BuildKeyFrameCoefficientProbabilityUpdatesIndependent(rows int, cols int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs) (tables.CoefficientProbs, CoefficientProbabilityUpdates, error) {
	var counts coefficientBranchCounts
	if err := buildKeyFrameCoefficientBranchCounts(rows, cols, modes, coeffs, above, base, &counts); err != nil {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, err
	}
	counts = defaultKeyFrameIndependentCoefficientBranchCountsForUpdate()
	return coefficientProbabilityUpdatesFromCountsIndependent(base, &counts, true)
}

func buildKeyFrameCoefficientBranchCounts(rows int, cols int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs, counts *coefficientBranchCounts) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if base == nil || counts == nil || len(modes) < required || len(coeffs) < required || len(above) < cols {
		return ErrModeBufferTooSmall
	}

	for col := range cols {
		above[col] = TokenContextPlanes{}
	}
	for row := range rows {
		left := TokenContextPlanes{}
		for col := range cols {
			index := row*cols + col
			mode := &modes[index]
			if !validKeyFrameMacroblockMode(mode) {
				return ErrInvalidPacketConfig
			}
			if err := countCoefficientMacroblockBranches(mode.YMode == common.BPred, &above[col], &left, &coeffs[index], counts); err != nil {
				return err
			}
		}
	}
	return nil
}

func BuildInterCoefficientProbabilityUpdates(rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs) (tables.CoefficientProbs, CoefficientProbabilityUpdates, error) {
	var counts coefficientBranchCounts
	if err := buildInterCoefficientBranchCounts(rows, cols, modes, coeffs, above, base, &counts); err != nil {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, err
	}
	return coefficientProbabilityUpdatesFromCounts(base, &counts)
}

// KeyFrameCoefficientEntropySavings ports the default_coef_context_savings
// branch of libvpx's vp8_estimate_entropy_savings for key frames. The result
// is whole bits, matching libvpx's prob_update_savings units.
func KeyFrameCoefficientEntropySavings(rows int, cols int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs) (int, error) {
	var counts coefficientBranchCounts
	if err := buildKeyFrameCoefficientBranchCounts(rows, cols, modes, coeffs, above, base, &counts); err != nil {
		return 0, err
	}
	return coefficientEntropySavingsFromCounts(base, &counts), nil
}

// KeyFrameCoefficientEntropySavingsIndependent ports the
// VPX_ERROR_RESILIENT_PARTITIONS coefficient-savings branch used by libvpx for
// key frames. It uses the same default_coef_counts independent-context model
// as BuildKeyFrameCoefficientProbabilityUpdatesIndependent so recode
// accounting matches the probabilities this package writes.
func KeyFrameCoefficientEntropySavingsIndependent(rows int, cols int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs) (int, error) {
	var counts coefficientBranchCounts
	if err := buildKeyFrameCoefficientBranchCounts(rows, cols, modes, coeffs, above, base, &counts); err != nil {
		return 0, err
	}
	counts = defaultKeyFrameIndependentCoefficientBranchCountsForUpdate()
	return coefficientEntropySavingsFromCountsIndependent(base, &counts, true), nil
}

// InterCoefficientEntropySavings ports the default_coef_context_savings branch
// of libvpx's vp8_estimate_entropy_savings for inter frames. The result is
// whole bits, matching libvpx's prob_update_savings units.
func InterCoefficientEntropySavings(rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs) (int, error) {
	var counts coefficientBranchCounts
	if err := buildInterCoefficientBranchCounts(rows, cols, modes, coeffs, above, base, &counts); err != nil {
		return 0, err
	}
	return coefficientEntropySavingsFromCounts(base, &counts), nil
}

// InterCoefficientEntropySavingsIndependent ports the
// VPX_ERROR_RESILIENT_PARTITIONS coefficient-savings branch used by libvpx for
// inter frames.
func InterCoefficientEntropySavingsIndependent(rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs) (int, error) {
	var counts coefficientBranchCounts
	if err := buildInterCoefficientBranchCounts(rows, cols, modes, coeffs, above, base, &counts); err != nil {
		return 0, err
	}
	return coefficientEntropySavingsFromCountsIndependent(base, &counts, false), nil
}

func coefficientEntropySavingsFromCounts(base *tables.CoefficientProbs, counts *coefficientBranchCounts) int {
	if base == nil || counts == nil {
		return 0
	}
	savings := 0
	for block := range tables.BlockTypes {
		for band := range tables.CoefBands {
			for ctx := range tables.PrevCoefContexts {
				for node := range tables.EntropyNodes {
					ct := (*counts)[block][band][ctx][node]
					total := ct[0] + ct[1]
					if total == 0 {
						continue
					}
					newProb := coefficientProbabilityFromBranchCount(ct)
					oldProb := (*base)[block][band][ctx][node]
					if newProb == oldProb {
						continue
					}
					updateProb := tables.CoefUpdateProbs[block][band][ctx][node]
					if s := coefficientProbabilityUpdateSavings(ct, oldProb, newProb, updateProb); s > 0 {
						savings += s
					}
				}
			}
		}
	}
	return savings
}

func coefficientEntropySavingsFromCountsIndependent(base *tables.CoefficientProbs, counts *coefficientBranchCounts, keyFrame bool) int {
	if base == nil || counts == nil {
		return 0
	}
	savings := 0
	for block := range tables.BlockTypes {
		for band := range tables.CoefBands {
			var summed [tables.EntropyNodes][2]int
			for ctx := range tables.PrevCoefContexts {
				for node := range tables.EntropyNodes {
					summed[node][0] += (*counts)[block][band][ctx][node][0]
					summed[node][1] += (*counts)[block][band][ctx][node][1]
				}
			}
			for node := range tables.EntropyNodes {
				newProb := coefficientProbabilityFromBranchCount(summed[node])
				nodeSavings := 0
				for ctx := range tables.PrevCoefContexts {
					oldProb := (*base)[block][band][ctx][node]
					if keyFrame && newProb == oldProb {
						continue
					}
					updateProb := tables.CoefUpdateProbs[block][band][ctx][node]
					nodeSavings += coefficientProbabilityUpdateSavings(summed[node], oldProb, newProb, updateProb)
				}
				if nodeSavings > 0 || keyFrame {
					savings += nodeSavings
				}
			}
		}
	}
	return savings
}

// BuildInterCoefficientProbabilityUpdatesIndependent ports libvpx
// vp8/encoder/bitstream.c independent_coef_context_savings + the matching
// branch in vp8_update_coef_probs. Under VPX_ERROR_RESILIENT_PARTITIONS the
// per-prev-context branch counts for a given (block_type, band) are summed,
// a single new probability is computed from that summed distribution, and
// every context k in PREV_COEF_CONTEXTS is updated together when the
// aggregated savings across k are positive (or, on key frames, whenever the
// shared new probability differs from the current one). This keeps the
// emitted coef contexts decodable independent of the per-context cross-talk
// the default path relies on, so a lost partition does not corrupt the
// downstream coef-prob tables.
func BuildInterCoefficientProbabilityUpdatesIndependent(rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs, keyFrame bool) (tables.CoefficientProbs, CoefficientProbabilityUpdates, error) {
	var counts coefficientBranchCounts
	if err := buildInterCoefficientBranchCounts(rows, cols, modes, coeffs, above, base, &counts); err != nil {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, err
	}
	return coefficientProbabilityUpdatesFromCountsIndependent(base, &counts, keyFrame)
}

func buildInterCoefficientBranchCounts(rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs, counts *coefficientBranchCounts) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if base == nil || counts == nil || len(modes) < required || len(coeffs) < required || len(above) < cols {
		return ErrModeBufferTooSmall
	}

	for col := range cols {
		above[col] = TokenContextPlanes{}
	}
	for row := range rows {
		left := TokenContextPlanes{}
		for col := range cols {
			index := row*cols + col
			is4x4 := interModeUses4x4Tokens(modes[index].Mode)
			if modes[index].MBSkipCoeff {
				resetTokenContext(&above[col], &left, is4x4)
				continue
			}
			if !validInterCoefficientTokenMode(&modes[index]) {
				return ErrInvalidPacketConfig
			}
			if err := countCoefficientMacroblockBranches(is4x4, &above[col], &left, &coeffs[index], counts); err != nil {
				return err
			}
		}
	}
	return nil
}

// coefficientProbabilityUpdatesFromCounts ports libvpx's default coefficient
// probability update walk in vp8_update_coef_probs (bitstream.c:865-950) for
// the non-error-resilient case. The per-(i,j,k,t) update fires only when
// prob_update_savings>0; the libvpx "force on key frames when newp != *Pold"
// branch at bitstream.c:920-928 is gated on
// VPX_ERROR_RESILIENT_PARTITIONS && frame_type == KEY_FRAME, so it does not
// apply here — that case is handled by
// coefficientProbabilityUpdatesFromCountsIndependent.
func coefficientProbabilityUpdatesFromCounts(base *tables.CoefficientProbs, counts *coefficientBranchCounts) (tables.CoefficientProbs, CoefficientProbabilityUpdates, error) {
	if base == nil || counts == nil {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrInvalidPacketConfig
	}
	frameProbs := *base
	updates := CoefficientProbabilityUpdates{Probs: *base}
	for block := range tables.BlockTypes {
		for band := range tables.CoefBands {
			for ctx := range tables.PrevCoefContexts {
				for node := range tables.EntropyNodes {
					ct := (*counts)[block][band][ctx][node]
					total := ct[0] + ct[1]
					if total == 0 {
						continue
					}
					newProb := coefficientProbabilityFromBranchCount(ct)
					oldProb := frameProbs[block][band][ctx][node]
					if newProb == oldProb {
						continue
					}
					updateProb := tables.CoefUpdateProbs[block][band][ctx][node]
					savings := coefficientProbabilityUpdateSavings(ct, oldProb, newProb, updateProb)
					if savings <= 0 {
						continue
					}
					frameProbs[block][band][ctx][node] = newProb
					updates.Probs[block][band][ctx][node] = newProb
					updates.Update[block][band][ctx][node] = true
					updates.UpdateCount++
					updates.SavingsBits += savings
				}
			}
		}
	}
	return frameProbs, updates, nil
}

// coefficientProbabilityUpdatesFromCountsIndependent ports libvpx
// vp8/encoder/bitstream.c independent_coef_context_savings (lines 678-740 in
// v1.16.0) and the matching VPX_ERROR_RESILIENT_PARTITIONS branch in
// vp8_update_coef_probs (lines 879-928). For every (block_type, band):
//
//  1. Branch counts are summed across PREV_COEF_CONTEXTS (libvpx sums token
//     counts and re-runs vp8_tree_probs_from_distribution on the sum; that is
//     equivalent to summing branch counts because branch_counts is linear in
//     the per-token counts).
//  2. A single new probability per entropy node is computed from the summed
//     branch count using the same Pfactor=256, Round=1 fitting as
//     coefficientProbabilityFromBranchCount.
//  3. For each entropy node t the savings are aggregated across k as
//     sum_k prob_update_savings(summed_ct, oldp[i][j][k][t], shared_newp[t],
//     upd[i][j][k][t]). When that aggregate is positive (or, on key frames,
//     whenever shared_newp[t] != oldp[i][j][k][t]), every k context is
//     updated to shared_newp[t]. This forces the prev-coef-context tables to
//     stay equal so a single emitted update keeps every k decodable.
func coefficientProbabilityUpdatesFromCountsIndependent(base *tables.CoefficientProbs, counts *coefficientBranchCounts, keyFrame bool) (tables.CoefficientProbs, CoefficientProbabilityUpdates, error) {
	if base == nil || counts == nil {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrInvalidPacketConfig
	}
	frameProbs := *base
	updates := CoefficientProbabilityUpdates{Probs: *base}
	for block := range tables.BlockTypes {
		for band := range tables.CoefBands {
			// Step 1: sum branch counts across PrevCoefContexts. This mirrors
			// sum_probs_over_prev_coef_context (bitstream.c:655) followed by
			// vp8_tree_probs_from_distribution acting on the summed token
			// distribution. Branch counts are linear in token counts so
			// summing branch counts directly produces the same result.
			var summed [tables.EntropyNodes][2]int
			for ctx := range tables.PrevCoefContexts {
				for node := range tables.EntropyNodes {
					summed[node][0] += (*counts)[block][band][ctx][node][0]
					summed[node][1] += (*counts)[block][band][ctx][node][1]
				}
			}
			// Step 2: compute the shared new probability per entropy node
			// from the summed distribution.
			var sharedNew [tables.EntropyNodes]uint8
			for node := range tables.EntropyNodes {
				sharedNew[node] = coefficientProbabilityFromBranchCount(summed[node])
			}
			// Step 3: aggregate per-node savings across the k contexts. On
			// key frames libvpx skips the per-k contribution where
			// newp == oldp[k] (bitstream.c:720-723) so the savings only
			// reflect the contexts that would actually change.
			var nodeSavings [tables.EntropyNodes]int
			for ctx := range tables.PrevCoefContexts {
				for node := range tables.EntropyNodes {
					oldProb := frameProbs[block][band][ctx][node]
					newProb := sharedNew[node]
					if keyFrame && newProb == oldProb {
						continue
					}
					updateProb := tables.CoefUpdateProbs[block][band][ctx][node]
					nodeSavings[node] += coefficientProbabilityUpdateSavings(summed[node], oldProb, newProb, updateProb)
				}
			}
			// Step 4: decide u per-(k, node) following the libvpx
			// vp8_update_coef_probs error-resilient branch
			// (bitstream.c:909-928). The per-node `s` is the aggregate
			// savings shared across all k; on key frames, an additional
			// per-k force fires when newp != oldp[k] regardless of `s`.
			for ctx := range tables.PrevCoefContexts {
				for node := range tables.EntropyNodes {
					newProb := sharedNew[node]
					oldProb := frameProbs[block][band][ctx][node]
					update := nodeSavings[node] > 0
					if keyFrame && newProb != oldProb {
						update = true
					}
					if !update {
						continue
					}
					// libvpx writes `vp8_write(w, u, upd)` and
					// `vp8_write_literal(w, newp, 8)` whenever u=1 even
					// if newp == oldp; mirror that to keep the emitted
					// bitstream byte-identical with a libvpx encoder.
					frameProbs[block][band][ctx][node] = newProb
					updates.Probs[block][band][ctx][node] = newProb
					updates.Update[block][band][ctx][node] = true
					updates.UpdateCount++
				}
			}
			for node := range tables.EntropyNodes {
				if nodeSavings[node] > 0 || keyFrame {
					updates.SavingsBits += nodeSavings[node]
				}
			}
		}
	}
	return frameProbs, updates, nil
}

func defaultKeyFrameIndependentCoefficientBranchCountsForUpdate() coefficientBranchCounts {
	var counts coefficientBranchCounts
	for block := range tables.BlockTypes {
		for band := range tables.CoefBands {
			for node := range tables.EntropyNodes {
				counts[block][band][0][node] = defaultKeyFrameIndependentCoefficientBranchCounts[block][band][node]
			}
		}
	}
	return counts
}

func coefficientProbabilityFromBranchCount(ct [2]int) uint8 {
	total := ct[0] + ct[1]
	if total <= 0 {
		return 128
	}
	prob := (ct[0]*256 + (total >> 1)) / total
	if prob <= 0 {
		return 1
	}
	if prob > 255 {
		return 255
	}
	return uint8(prob)
}

func coefficientProbabilityUpdateSavings(ct [2]int, oldProb uint8, newProb uint8, updateProb uint8) int {
	oldBits := coefficientBranchCost(ct, oldProb)
	newBits := coefficientBranchCost(ct, newProb)
	updateBits := 8 + ((coefficientBitCost(updateProb, 1) - coefficientBitCost(updateProb, 0)) >> 8)
	return oldBits - newBits - updateBits
}

func coefficientBranchCost(ct [2]int, prob uint8) int {
	return (ct[0]*coefficientBitCost(prob, 0) + ct[1]*coefficientBitCost(prob, 1)) >> 8
}

func coefficientBitCost(prob uint8, bit int) int {
	if bit == 0 {
		return tables.ProbCost[prob]
	}
	return tables.ProbCost[255-int(prob)]
}

func countCoefficientMacroblockBranches(is4x4 bool, above *TokenContextPlanes, left *TokenContextPlanes, coeffs *MacroblockCoefficients, counts *coefficientBranchCounts) error {
	if above == nil || left == nil || coeffs == nil || counts == nil {
		return ErrInvalidPacketConfig
	}
	blockType := 0
	skipDC := 0
	if !is4x4 {
		eob := coeffs.BlockEOB(24, 0)
		ctx := int(above.Y2 + left.Y2)
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if err := countBlockCoefficientBranches(counts, 1, ctx, 0, &coeffs.QCoeff[24], eob); err != nil {
			return err
		}
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		above.Y2 = hasCoeffs
		left.Y2 = hasCoeffs

		blockType = 0
		skipDC = 1
	} else {
		blockType = 3
	}

	for block := range 16 {
		eob := coeffs.BlockEOB(block, skipDC)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(above.Y1[a] + left.Y1[l])
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if err := countBlockCoefficientBranches(counts, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob); err != nil {
			return err
		}
		hasCoeffs := uint8(0)
		if eob > skipDC {
			hasCoeffs = 1
		}
		above.Y1[a] = hasCoeffs
		left.Y1[l] = hasCoeffs
	}

	for block := 16; block < 24; block++ {
		eob := coeffs.BlockEOB(block, 0)
		a, l := tokenUVContextIndex(block)
		ctx := int(getTokenUVContext(above, a) + getTokenUVContext(left, l))
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if err := countBlockCoefficientBranches(counts, 2, ctx, 0, &coeffs.QCoeff[block], eob); err != nil {
			return err
		}
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		setTokenUVContext(above, a, hasCoeffs)
		setTokenUVContext(left, l, hasCoeffs)
	}
	return nil
}

func countBlockCoefficientBranches(counts *coefficientBranchCounts, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) error {
	if counts == nil || qcoeff == nil || blockType < 0 || blockType >= tables.BlockTypes || ctx < 0 || ctx >= tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return ErrInvalidPacketConfig
	}
	if eob <= skipDC {
		return countCoefficientTokenBranches(&(*counts)[blockType][skipDC][ctx], tables.DCTEOBToken)
	}

	band := skipDC
	tokenCtx := ctx
	for pos := skipDC; pos < 16; pos++ {
		rc := int(tables.DefaultZigZag1D[pos])
		coeff := int(qcoeff[rc])
		if coeff == 0 {
			if err := countCoefficientTokenBranches(&(*counts)[blockType][band][tokenCtx], tables.ZeroToken); err != nil {
				return err
			}
			if pos == 15 {
				return nil
			}
			band = int(tables.CoefBandsTable[pos+1])
			tokenCtx = 0
			continue
		}

		token, _, ok := coeffToken(coeff)
		if !ok {
			return ErrInvalidPacketConfig
		}
		if err := countCoefficientTokenBranches(&(*counts)[blockType][band][tokenCtx], token); err != nil {
			return err
		}
		if pos == 15 {
			return nil
		}
		band = int(tables.CoefBandsTable[pos+1])
		tokenCtx = int(tables.PrevTokenClass[token])
		if pos+1 == eob {
			return countCoefficientTokenBranches(&(*counts)[blockType][band][tokenCtx], tables.DCTEOBToken)
		}
	}
	return nil
}

type coefficientTokenBranchPath struct {
	len   uint8
	nodes [7]uint8
	bits  [7]uint8
}

var coefficientTokenBranchPaths = buildCoefficientTokenBranchPaths()

func buildCoefficientTokenBranchPaths() [tables.MaxEntropyTokens]coefficientTokenBranchPath {
	var paths [tables.MaxEntropyTokens]coefficientTokenBranchPath
	for token := range tables.MaxEntropyTokens {
		encoding := tables.CoefEncodings[token]
		node := int16(0)
		for bitIndex := int(encoding.Len) - 1; bitIndex >= 0; bitIndex-- {
			bit := int((encoding.Value >> uint(bitIndex)) & 1)
			probIndex := int(node >> 1)
			if probIndex < 0 || probIndex >= tables.EntropyNodes || int(node)+bit >= len(tables.CoefTree) {
				panic("govpx: invalid VP8 coefficient token tree")
			}
			path := &paths[token]
			if int(path.len) >= len(path.nodes) {
				panic("govpx: coefficient token path too long")
			}
			path.nodes[path.len] = uint8(probIndex)
			path.bits[path.len] = uint8(bit)
			path.len++
			next := tables.CoefTree[int(node)+bit]
			if next <= 0 {
				if bitIndex != 0 || int(-next) != token {
					panic("govpx: invalid VP8 coefficient token encoding")
				}
				break
			}
			node = next
		}
	}
	return paths
}

func countCoefficientTokenBranches(counts *[tables.EntropyNodes][2]int, token int) error {
	if counts == nil || token < 0 || token >= tables.MaxEntropyTokens {
		return ErrInvalidPacketConfig
	}
	switch token {
	case tables.DCTEOBToken:
		counts[0][0]++
		return nil
	case tables.ZeroToken:
		counts[0][1]++
		counts[1][0]++
		return nil
	}
	path := coefficientTokenBranchPaths[token]
	for i := uint8(0); i < path.len; i++ {
		counts[path.nodes[i]][path.bits[i]]++
	}
	return nil
}
