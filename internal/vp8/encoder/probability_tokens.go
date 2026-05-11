package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/tokenize.c coefficient token
// accumulation and vp8/encoder/bitstream.c coefficient probability updates.

type coefficientTokenCounts [tables.BlockTypes][tables.CoefBands][tables.PrevCoefContexts][tables.MaxEntropyTokens]int

// InterCoefficientTokenCounts is the public alias for the per-frame token
// count accumulator used by inter-frame probability updates. The encoder
// pre-builds this during accepted-MB reconstruction so InterFramePacket.Write
// can skip its own count walk (Lane D consolidation).
type InterCoefficientTokenCounts = coefficientTokenCounts

// ResetInterCoefficientTokenCounts zeroes a token count accumulator before a
// new accepted-MB reconstruction pass.
func ResetInterCoefficientTokenCounts(counts *InterCoefficientTokenCounts) {
	if counts == nil {
		return
	}
	*counts = InterCoefficientTokenCounts{}
}

// AccumulateInterMacroblockTokenCountsAndRecords adds the supplied MB's
// coefficient tokens to both the probability-update counts and the prepared
// token-record stream. The above/left context mutations mirror
// buildInterCoefficientTokenCounts.
func AccumulateInterMacroblockTokenCountsAndRecords(counts *InterCoefficientTokenCounts, records *InterCoefficientTokenRecords, is4x4 bool, above *TokenContextPlanes, left *TokenContextPlanes, coeffs *MacroblockCoefficients) error {
	if counts == nil {
		return ErrInvalidPacketConfig
	}
	return countCoefficientMacroblockTokensAndRecords(is4x4, above, left, coeffs, counts, records)
}

func buildKeyFrameCoefficientTokenCounts(rows int, cols int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs, counts *coefficientTokenCounts) error {
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
			if err := countCoefficientMacroblockTokens(mode.YMode == common.BPred, &above[col], &left, &coeffs[index], counts); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildInterCoefficientTokenCounts(rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs, counts *coefficientTokenCounts) error {
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
			if err := countCoefficientMacroblockTokens(is4x4, &above[col], &left, &coeffs[index], counts); err != nil {
				return err
			}
		}
	}
	return nil
}

func coefficientEntropySavingsFromTokenCounts(base *tables.CoefficientProbs, counts *coefficientTokenCounts) int {
	if base == nil || counts == nil {
		return 0
	}
	savings := 0
	for block := range tables.BlockTypes {
		for band := range tables.CoefBands {
			for ctx := range tables.PrevCoefContexts {
				branchCounts := coefficientBranchCountsFromTokenCounts(&(*counts)[block][band][ctx])
				for node := range tables.EntropyNodes {
					ct := branchCounts[node]
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

func coefficientEntropySavingsFromTokenCountsIndependent(base *tables.CoefficientProbs, counts *coefficientTokenCounts, keyFrame bool) int {
	if base == nil || counts == nil {
		return 0
	}
	savings := 0
	for block := range tables.BlockTypes {
		for band := range tables.CoefBands {
			var tokenSum [tables.MaxEntropyTokens]int
			for ctx := range tables.PrevCoefContexts {
				for token := range tables.MaxEntropyTokens {
					tokenSum[token] += (*counts)[block][band][ctx][token]
				}
			}
			summed := coefficientBranchCountsFromTokenCounts(&tokenSum)
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

func coefficientProbabilityUpdatesFromTokenCounts(base *tables.CoefficientProbs, counts *coefficientTokenCounts) (tables.CoefficientProbs, CoefficientProbabilityUpdates, error) {
	if base == nil || counts == nil {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrInvalidPacketConfig
	}
	frameProbs := *base
	updates := CoefficientProbabilityUpdates{Probs: *base}
	for block := range tables.BlockTypes {
		for band := range tables.CoefBands {
			for ctx := range tables.PrevCoefContexts {
				branchCounts := coefficientBranchCountsFromTokenCounts(&(*counts)[block][band][ctx])
				for node := range tables.EntropyNodes {
					ct := branchCounts[node]
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

func coefficientProbabilityUpdatesFromTokenCountsIndependent(base *tables.CoefficientProbs, counts *coefficientTokenCounts, keyFrame bool) (tables.CoefficientProbs, CoefficientProbabilityUpdates, error) {
	if base == nil || counts == nil {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrInvalidPacketConfig
	}
	frameProbs := *base
	updates := CoefficientProbabilityUpdates{Probs: *base}
	for block := range tables.BlockTypes {
		for band := range tables.CoefBands {
			var tokenSum [tables.MaxEntropyTokens]int
			for ctx := range tables.PrevCoefContexts {
				for token := range tables.MaxEntropyTokens {
					tokenSum[token] += (*counts)[block][band][ctx][token]
				}
			}
			summed := coefficientBranchCountsFromTokenCounts(&tokenSum)
			var sharedNew [tables.EntropyNodes]uint8
			for node := range tables.EntropyNodes {
				sharedNew[node] = coefficientProbabilityFromBranchCount(summed[node])
			}
			var nodeSavings [tables.EntropyNodes]int
			for ctx := range tables.PrevCoefContexts {
				for node := range tables.EntropyNodes {
					oldProb := frameProbs[block][band][ctx][node]
					newProb := sharedNew[node]
					updateProb := tables.CoefUpdateProbs[block][band][ctx][node]
					nodeSavings[node] += coefficientProbabilityUpdateSavings(summed[node], oldProb, newProb, updateProb)
				}
			}
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

func countCoefficientMacroblockTokens(is4x4 bool, above *TokenContextPlanes, left *TokenContextPlanes, coeffs *MacroblockCoefficients, counts *coefficientTokenCounts) error {
	return countCoefficientMacroblockTokensAndRecords(is4x4, above, left, coeffs, counts, nil)
}

func countCoefficientMacroblockTokensAndRecords(is4x4 bool, above *TokenContextPlanes, left *TokenContextPlanes, coeffs *MacroblockCoefficients, counts *coefficientTokenCounts, records *InterCoefficientTokenRecords) error {
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
		if err := countBlockCoefficientTokensAndRecords(counts, records, 1, ctx, 0, &coeffs.QCoeff[24], eob); err != nil {
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
		if err := countBlockCoefficientTokensAndRecords(counts, records, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob); err != nil {
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
		if err := countBlockCoefficientTokensAndRecords(counts, records, 2, ctx, 0, &coeffs.QCoeff[block], eob); err != nil {
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

func countBlockCoefficientTokens(counts *coefficientTokenCounts, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) error {
	return countBlockCoefficientTokensAndRecords(counts, nil, blockType, ctx, skipDC, qcoeff, eob)
}

func countBlockCoefficientTokensAndRecords(counts *coefficientTokenCounts, records *InterCoefficientTokenRecords, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) error {
	if counts == nil || qcoeff == nil || blockType < 0 || blockType >= tables.BlockTypes || ctx < 0 || ctx >= tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return ErrInvalidPacketConfig
	}
	if eob <= skipDC {
		(*counts)[blockType][skipDC][ctx][tables.DCTEOBToken]++
		records.appendTokenUnchecked(blockType, skipDC, ctx, tables.DCTEOBToken, 0, 0, false)
		return nil
	}
	if eob > 16 {
		return ErrInvalidPacketConfig
	}

	band := skipDC
	tokenCtx := ctx
	skipEOBNode := false
	for pos := skipDC; pos < eob; pos++ {
		rc := int(tables.DefaultZigZag1D[pos])
		coeff := int(qcoeff[rc])
		// Branchless |coeff| split into magnitude and sign nibble: signMask
		// is -1 when coeff is negative, 0 otherwise.
		signMask := coeff >> intSignShift
		mag := (coeff ^ signMask) - signMask
		sign := uint8(signMask & 1)
		if mag > tables.DCTMaxValue {
			return ErrInvalidPacketConfig
		}
		token := int(coeffAbsTokenLUT[mag])
		(*counts)[blockType][band][tokenCtx][token]++
		// Validation hoisted to the function entry, so the per-coeff
		// pack-and-append skips the redundant range checks.
		records.appendTokenUnchecked(blockType, band, tokenCtx, token, mag, sign, skipEOBNode)
		if pos+1 < 16 {
			band = int(tables.CoefBandsTable[pos+1])
			tokenCtx = int(tables.PrevTokenClass[token])
			skipEOBNode = tokenCtx == 0
		}
	}
	if eob < 16 {
		(*counts)[blockType][band][tokenCtx][tables.DCTEOBToken]++
		records.appendTokenUnchecked(blockType, band, tokenCtx, tables.DCTEOBToken, 0, 0, false)
	}
	return nil
}

func coefficientBranchCountsFromTokenCounts(tokens *[tables.MaxEntropyTokens]int) [tables.EntropyNodes][2]int {
	var counts [tables.EntropyNodes][2]int
	if tokens == nil {
		return counts
	}
	for token, total := range tokens {
		if total == 0 {
			continue
		}
		switch token {
		case tables.DCTEOBToken:
			counts[0][0] += total
			continue
		case tables.ZeroToken:
			counts[0][1] += total
			counts[1][0] += total
			continue
		}
		path := coefficientTokenBranchPaths[token]
		for i := uint8(0); i < path.len; i++ {
			counts[path.nodes[i]][path.bits[i]] += total
		}
	}
	return counts
}
