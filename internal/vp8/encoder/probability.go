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
}

type coefficientBranchCounts [tables.BlockTypes][tables.CoefBands][tables.PrevCoefContexts][tables.EntropyNodes][2]int

func WriteCoefficientProbabilityUpdates(w *BoolWriter, updates *CoefficientProbabilityUpdates) error {
	if w == nil {
		return ErrInvalidPacketConfig
	}
	for block := 0; block < tables.BlockTypes; block++ {
		for band := 0; band < tables.CoefBands; band++ {
			for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
				for node := 0; node < tables.EntropyNodes; node++ {
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

func BuildKeyFrameCoefficientProbabilityUpdates(rows int, cols int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs) (tables.CoefficientProbs, CoefficientProbabilityUpdates, error) {
	if rows < 0 || cols < 0 {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrModeBufferTooSmall
	}
	required := rows * cols
	if base == nil || len(modes) < required || len(coeffs) < required || len(above) < cols {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrModeBufferTooSmall
	}

	var counts coefficientBranchCounts
	for col := 0; col < cols; col++ {
		above[col] = TokenContextPlanes{}
	}
	for row := 0; row < rows; row++ {
		left := TokenContextPlanes{}
		for col := 0; col < cols; col++ {
			index := row*cols + col
			mode := &modes[index]
			if !validKeyFrameMacroblockMode(mode) {
				return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrInvalidPacketConfig
			}
			if err := countCoefficientMacroblockBranches(mode.YMode == common.BPred, &above[col], &left, &coeffs[index], &counts); err != nil {
				return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, err
			}
		}
	}
	return coefficientProbabilityUpdatesFromCounts(base, &counts)
}

func BuildInterCoefficientProbabilityUpdates(rows int, cols int, modes []InterFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, base *tables.CoefficientProbs) (tables.CoefficientProbs, CoefficientProbabilityUpdates, error) {
	if rows < 0 || cols < 0 {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrModeBufferTooSmall
	}
	required := rows * cols
	if base == nil || len(modes) < required || len(coeffs) < required || len(above) < cols {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrModeBufferTooSmall
	}

	var counts coefficientBranchCounts
	for col := 0; col < cols; col++ {
		above[col] = TokenContextPlanes{}
	}
	for row := 0; row < rows; row++ {
		left := TokenContextPlanes{}
		for col := 0; col < cols; col++ {
			index := row*cols + col
			is4x4 := interModeUses4x4Tokens(modes[index].Mode)
			if modes[index].MBSkipCoeff {
				resetTokenContext(&above[col], &left, is4x4)
				continue
			}
			if !validInterCoefficientTokenMode(&modes[index]) {
				return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrInvalidPacketConfig
			}
			if err := countCoefficientMacroblockBranches(is4x4, &above[col], &left, &coeffs[index], &counts); err != nil {
				return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, err
			}
		}
	}
	return coefficientProbabilityUpdatesFromCounts(base, &counts)
}

func coefficientProbabilityUpdatesFromCounts(base *tables.CoefficientProbs, counts *coefficientBranchCounts) (tables.CoefficientProbs, CoefficientProbabilityUpdates, error) {
	if base == nil || counts == nil {
		return tables.CoefficientProbs{}, CoefficientProbabilityUpdates{}, ErrInvalidPacketConfig
	}
	frameProbs := *base
	updates := CoefficientProbabilityUpdates{Probs: *base}
	for block := 0; block < tables.BlockTypes; block++ {
		for band := 0; band < tables.CoefBands; band++ {
			for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
				for node := 0; node < tables.EntropyNodes; node++ {
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
					if coefficientProbabilityUpdateSavings(ct, oldProb, newProb, updateProb) <= 0 {
						continue
					}
					frameProbs[block][band][ctx][node] = newProb
					updates.Probs[block][band][ctx][node] = newProb
					updates.Update[block][band][ctx][node] = true
					updates.UpdateCount++
				}
			}
		}
	}
	return frameProbs, updates, nil
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

	for block := 0; block < 16; block++ {
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

func countCoefficientTokenBranches(counts *[tables.EntropyNodes][2]int, token int) error {
	if counts == nil || token < 0 || token >= tables.MaxEntropyTokens {
		return ErrInvalidPacketConfig
	}
	encoding := tables.CoefEncodings[token]
	node := int16(0)
	for bitIndex := int(encoding.Len) - 1; bitIndex >= 0; bitIndex-- {
		bit := int((encoding.Value >> uint(bitIndex)) & 1)
		probIndex := int(node >> 1)
		if probIndex < 0 || probIndex >= tables.EntropyNodes || int(node)+bit >= len(tables.CoefTree) {
			return ErrInvalidPacketConfig
		}
		counts[probIndex][bit]++
		next := tables.CoefTree[int(node)+bit]
		if next <= 0 {
			if bitIndex != 0 || int(-next) != token {
				return ErrInvalidPacketConfig
			}
			return nil
		}
		node = next
	}
	return ErrInvalidPacketConfig
}
