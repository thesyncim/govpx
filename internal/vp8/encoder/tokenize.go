package encoder

import (
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/tokenize.c coefficient token
// selection and vp8/encoder/bitstream.c coefficient token packing.

type TokenContextPlanes struct {
	Y1 [4]uint8
	U  [2]uint8
	V  [2]uint8
	Y2 uint8
}

func WriteBlockTokens(w *BoolWriter, probs *tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16) error {
	if w == nil || probs == nil || qcoeff == nil || blockType < 0 || blockType >= tables.BlockTypes || ctx < 0 || ctx >= tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return ErrInvalidPacketConfig
	}

	return writeBlockTokensEOB(w, probs, blockType, ctx, skipDC, qcoeff, BlockCoeffEOB(qcoeff, skipDC))
}

func WriteCoefficientMacroblockTokens(w *BoolWriter, probs *tables.CoefficientProbs, is4x4 bool, above *TokenContextPlanes, left *TokenContextPlanes, coeffs *MacroblockCoefficients) error {
	if w == nil || probs == nil || above == nil || left == nil || coeffs == nil {
		return ErrInvalidPacketConfig
	}
	return writeCoefficientMacroblockTokensWithEOBs(w, probs, is4x4, above, left, coeffs)
}

func writeCoefficientMacroblockTokensWithEOBs(w *BoolWriter, probs *tables.CoefficientProbs, is4x4 bool, above *TokenContextPlanes, left *TokenContextPlanes, coeffs *MacroblockCoefficients) error {
	blockType := 0
	skipDC := 0
	if !is4x4 {
		eob := int(coeffs.EOB[24])
		ctx := int(above.Y2 + left.Y2)
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if eob == 0 {
			w.WriteBool(0, (*probs)[1][0][ctx][0])
		} else {
			if err := writeBlockTokensEOB(w, probs, 1, ctx, 0, &coeffs.QCoeff[24], eob); err != nil {
				return err
			}
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
		eob := max(int(coeffs.EOB[block]), skipDC)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(above.Y1[a] + left.Y1[l])
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if eob <= skipDC {
			w.WriteBool(0, (*probs)[blockType][skipDC][ctx][0])
		} else {
			if err := writeBlockTokensEOB(w, probs, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob); err != nil {
				return err
			}
		}
		hasCoeffs := uint8(0)
		if eob > skipDC {
			hasCoeffs = 1
		}
		above.Y1[a] = hasCoeffs
		left.Y1[l] = hasCoeffs
	}

	for block := 16; block < 24; block++ {
		eob := int(coeffs.EOB[block])
		a, l := tokenUVContextIndex(block)
		ctx := int(getTokenUVContext(above, a) + getTokenUVContext(left, l))
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if eob == 0 {
			w.WriteBool(0, (*probs)[2][0][ctx][0])
		} else {
			if err := writeBlockTokensEOB(w, probs, 2, ctx, 0, &coeffs.QCoeff[block], eob); err != nil {
				return err
			}
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

func WriteCoefficientTokenGrid(w *BoolWriter, rows int, cols int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, probs *tables.CoefficientProbs) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if w == nil || probs == nil || len(modes) < required || len(coeffs) < required || len(above) < cols {
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
			if err := writeCoefficientMacroblockTokensWithEOBs(w, probs, mode.YMode == common.BPred, &above[col], &left, &coeffs[index]); err != nil {
				return err
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func WriteCoefficientTokenGridPartitioned(writers *[8]BoolWriter, partitions int, rows int, cols int, modes []KeyFrameMacroblockMode, coeffs []MacroblockCoefficients, above []TokenContextPlanes, probs *tables.CoefficientProbs) error {
	if rows < 0 || cols < 0 {
		return ErrModeBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return ErrModeBufferTooSmall
	}
	required := rows * cols
	if writers == nil || probs == nil || len(modes) < required || len(coeffs) < required || len(above) < cols || partitions != 2 && partitions != 4 && partitions != 8 {
		return ErrModeBufferTooSmall
	}

	for col := range cols {
		above[col] = TokenContextPlanes{}
	}
	for row := range rows {
		w := &writers[row&(partitions-1)]
		left := TokenContextPlanes{}
		for col := range cols {
			index := row*cols + col
			mode := &modes[index]
			if !validKeyFrameMacroblockMode(mode) {
				return ErrInvalidPacketConfig
			}
			if err := writeCoefficientMacroblockTokensWithEOBs(w, probs, mode.YMode == common.BPred, &above[col], &left, &coeffs[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

func BlockCoeffEOB(qcoeff *[16]int16, skipDC int) int {
	for pos := 15; pos >= skipDC; pos-- {
		rc := int(tables.DefaultZigZag1D[pos])
		if qcoeff[rc] != 0 {
			return pos + 1
		}
	}
	return skipDC
}

func (coeffs *MacroblockCoefficients) SetBlockEOB(block int, eob int) {
	if coeffs == nil || block < 0 || block >= len(coeffs.EOB) {
		return
	}
	if eob < 0 {
		eob = 0
	}
	if eob > 16 {
		eob = 16
	}
	coeffs.EOB[block] = uint8(eob)
}

func (coeffs *MacroblockCoefficients) BlockEOB(block int, skipDC int) int {
	if coeffs == nil || block < 0 || block >= len(coeffs.EOB) {
		return skipDC
	}
	eob := int(coeffs.EOB[block])
	if eob < skipDC {
		return skipDC
	}
	return eob
}

// coeffAbsTokenLUT maps abs(coeff) in [0, DCTMaxValue] to the VP8 entropy
// token id. Index 0 carries tables.ZeroToken so the table can also be
// consulted by callers that prefer a single load over an explicit
// "coeff == 0" branch.
//
// Materializing the classifier as a 2049-byte lookup turns the hot
// per-coefficient classification (previously a function call with a
// six-way range-comparison switch -- gcflags -m=2 reports
// "cannot inline coeffToken: function too complex: cost 102 exceeds
// budget 80") into a single byte load that the compiler can fold into
// the surrounding loop body. Out-of-range magnitudes
// (abs(coeff) > DCTMaxValue) are filtered by the caller before
// indexing, so the LUT itself never needs a sentinel value.
var coeffAbsTokenLUT = buildCoeffAbsTokenLUT()

func buildCoeffAbsTokenLUT() [tables.DCTMaxValue + 1]uint8 {
	var lut [tables.DCTMaxValue + 1]uint8
	for i := 0; i <= tables.DCTMaxValue; i++ {
		switch {
		case i == 0:
			lut[i] = tables.ZeroToken
		case i == 1:
			lut[i] = tables.OneToken
		case i == 2:
			lut[i] = tables.TwoToken
		case i == 3:
			lut[i] = tables.ThreeToken
		case i == 4:
			lut[i] = tables.FourToken
		case i <= 6:
			lut[i] = tables.DCTValCategory1
		case i <= 10:
			lut[i] = tables.DCTValCategory2
		case i <= 18:
			lut[i] = tables.DCTValCategory3
		case i <= 34:
			lut[i] = tables.DCTValCategory4
		case i <= 66:
			lut[i] = tables.DCTValCategory5
		default:
			lut[i] = tables.DCTValCategory6
		}
	}
	return lut
}

func coeffToken(coeff int) (int, int, bool) {
	if coeff < 0 {
		coeff = -coeff
	}
	if coeff <= 0 || coeff > tables.DCTMaxValue {
		return 0, 0, false
	}
	return int(coeffAbsTokenLUT[coeff]), coeff, true
}

// ResetTokenContextPlanes applies the inter-frame mb_no_coeff_skip context
// reset. Whole-block modes clear all contexts; 4x4 modes preserve Y2 because
// no Y2 tokens are coded for the macroblock.
func ResetTokenContextPlanes(above *TokenContextPlanes, left *TokenContextPlanes, is4x4 bool) {
	if above == nil || left == nil {
		return
	}
	if !is4x4 {
		*above = TokenContextPlanes{}
		*left = TokenContextPlanes{}
		return
	}
	aboveY2, leftY2 := above.Y2, left.Y2
	*above = TokenContextPlanes{Y2: aboveY2}
	*left = TokenContextPlanes{Y2: leftY2}
}

// UpdateTokenContextPlanesFromCoefficients updates above/left token contexts
// after a macroblock has been built, matching the EOB-derived updates inside
// WriteCoefficientMacroblockTokens. Used by mode-decision to keep RD scoring
// in step with libvpx's above_context / left_context plumbing across MBs.
func UpdateTokenContextPlanesFromCoefficients(above *TokenContextPlanes, left *TokenContextPlanes, is4x4 bool, coeffs *MacroblockCoefficients) {
	if above == nil || left == nil || coeffs == nil {
		return
	}
	skipDC := 0
	if !is4x4 {
		eob := coeffs.BlockEOB(24, 0)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		above.Y2 = hasCoeffs
		left.Y2 = hasCoeffs
		skipDC = 1
	}
	for block := range 16 {
		eob := coeffs.BlockEOB(block, skipDC)
		hasCoeffs := uint8(0)
		if eob > skipDC {
			hasCoeffs = 1
		}
		above.Y1[block&3] = hasCoeffs
		left.Y1[(block&0x0c)>>2] = hasCoeffs
	}
	for block := 16; block < 24; block++ {
		eob := coeffs.BlockEOB(block, 0)
		a, l := tokenUVContextIndex(block)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		setTokenUVContext(above, a, hasCoeffs)
		setTokenUVContext(left, l, hasCoeffs)
	}
}

func tokenUVContextIndex(block int) (int, int) {
	base := 0
	if block > 19 {
		base = 2
	}
	a := base + (block & 1)
	l := base
	if (block & 3) > 1 {
		l++
	}
	return a, l
}

func getTokenUVContext(ctx *TokenContextPlanes, index int) uint8 {
	if index < 2 {
		return ctx.U[index]
	}
	return ctx.V[index-2]
}

func setTokenUVContext(ctx *TokenContextPlanes, index int, value uint8) {
	if index < 2 {
		ctx.U[index] = value
		return
	}
	ctx.V[index-2] = value
}

const maxCategory6Coeff = tables.DCTMaxValue
