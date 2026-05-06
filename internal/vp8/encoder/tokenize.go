package encoder

import (
	"github.com/thesyncim/libgopx/internal/vp8/common"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/encoder/tokenize.c coefficient token
// selection and vp8/encoder/bitstream.c coefficient token packing.

type TokenContextPlanes struct {
	Y1 [4]uint8
	U  [2]uint8
	V  [2]uint8
	Y2 uint8
}

type MacroblockCoefficients struct {
	QCoeff [25][16]int16
}

func WriteBlockTokens(w *BoolWriter, probs *tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16) error {
	if w == nil || probs == nil || qcoeff == nil || blockType < 0 || blockType >= tables.BlockTypes || ctx < 0 || ctx >= tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return ErrInvalidPacketConfig
	}

	return writeBlockTokensEOB(w, probs, blockType, ctx, skipDC, qcoeff, BlockCoeffEOB(qcoeff, skipDC))
}

func writeBlockTokensEOB(w *BoolWriter, probs *tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) error {
	p := (*probs)[blockType][skipDC][ctx]
	if eob <= skipDC {
		w.WriteBool(0, p[0])
		return w.Err()
	}

	w.WriteBool(1, p[0])
	for pos := skipDC; pos < 16; pos++ {
		rc := int(tables.DefaultZigZag1D[pos])
		coeff := int(qcoeff[rc])
		if coeff == 0 {
			w.WriteBool(0, p[1])
			if pos == 15 {
				return w.Err()
			}
			p = (*probs)[blockType][tables.CoefBandsTable[pos+1]][0]
			continue
		}

		token, mag, ok := coeffToken(coeff)
		if !ok {
			return ErrInvalidPacketConfig
		}
		writeNonZeroCoeffToken(w, p, token, mag)
		if coeff < 0 {
			w.WriteBit(1)
		} else {
			w.WriteBit(0)
		}
		if w.Err() != nil {
			return w.Err()
		}

		if pos == 15 {
			return nil
		}
		p = (*probs)[blockType][tables.CoefBandsTable[pos+1]][tables.PrevTokenClass[token]]
		if pos+1 == eob {
			w.WriteBool(0, p[0])
			return w.Err()
		}
		w.WriteBool(1, p[0])
	}
	return w.Err()
}

func WriteCoefficientMacroblockTokens(w *BoolWriter, probs *tables.CoefficientProbs, is4x4 bool, above *TokenContextPlanes, left *TokenContextPlanes, coeffs *MacroblockCoefficients) error {
	if w == nil || probs == nil || above == nil || left == nil || coeffs == nil {
		return ErrInvalidPacketConfig
	}

	blockType := 0
	skipDC := 0
	if !is4x4 {
		eob := BlockCoeffEOB(&coeffs.QCoeff[24], 0)
		ctx := int(above.Y2 + left.Y2)
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if err := writeBlockTokensEOB(w, probs, 1, ctx, 0, &coeffs.QCoeff[24], eob); err != nil {
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
		eob := BlockCoeffEOB(&coeffs.QCoeff[block], skipDC)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(above.Y1[a] + left.Y1[l])
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if err := writeBlockTokensEOB(w, probs, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob); err != nil {
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
		eob := BlockCoeffEOB(&coeffs.QCoeff[block], 0)
		a, l := tokenUVContextIndex(block)
		ctx := int(getTokenUVContext(above, a) + getTokenUVContext(left, l))
		if ctx >= tables.PrevCoefContexts {
			return ErrInvalidPacketConfig
		}
		if err := writeBlockTokensEOB(w, probs, 2, ctx, 0, &coeffs.QCoeff[block], eob); err != nil {
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

	for col := 0; col < cols; col++ {
		above[col] = TokenContextPlanes{}
	}
	for row := 0; row < rows; row++ {
		left := TokenContextPlanes{}
		for col := 0; col < cols; col++ {
			index := row*cols + col
			mode := &modes[index]
			if !validKeyFrameMacroblockMode(mode) {
				return ErrInvalidPacketConfig
			}
			if err := WriteCoefficientMacroblockTokens(w, probs, mode.YMode == common.BPred, &above[col], &left, &coeffs[index]); err != nil {
				return err
			}
		}
	}
	if w.Err() != nil {
		return w.Err()
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

func coeffToken(coeff int) (int, int, bool) {
	if coeff < 0 {
		coeff = -coeff
	}
	switch {
	case coeff <= 0:
		return 0, 0, false
	case coeff == 1:
		return tables.OneToken, coeff, true
	case coeff == 2:
		return tables.TwoToken, coeff, true
	case coeff == 3:
		return tables.ThreeToken, coeff, true
	case coeff == 4:
		return tables.FourToken, coeff, true
	case coeff <= 6:
		return tables.DCTValCategory1, coeff, true
	case coeff <= 10:
		return tables.DCTValCategory2, coeff, true
	case coeff <= 18:
		return tables.DCTValCategory3, coeff, true
	case coeff <= 34:
		return tables.DCTValCategory4, coeff, true
	case coeff <= 66:
		return tables.DCTValCategory5, coeff, true
	case coeff <= maxCategory6Coeff:
		return tables.DCTValCategory6, coeff, true
	default:
		return 0, 0, false
	}
}

func writeNonZeroCoeffToken(w *BoolWriter, p [tables.EntropyNodes]uint8, token int, mag int) {
	w.WriteBool(1, p[1])
	switch token {
	case tables.OneToken:
		w.WriteBool(0, p[2])
	case tables.TwoToken:
		w.WriteBool(1, p[2])
		w.WriteBool(0, p[3])
		w.WriteBool(0, p[4])
	case tables.ThreeToken:
		w.WriteBool(1, p[2])
		w.WriteBool(0, p[3])
		w.WriteBool(1, p[4])
		w.WriteBool(0, p[5])
	case tables.FourToken:
		w.WriteBool(1, p[2])
		w.WriteBool(0, p[3])
		w.WriteBool(1, p[4])
		w.WriteBool(1, p[5])
	case tables.DCTValCategory1:
		w.WriteBool(1, p[2])
		w.WriteBool(1, p[3])
		w.WriteBool(0, p[6])
		w.WriteBool(0, p[7])
		writeCoeffExtraBits(w, token, mag)
	case tables.DCTValCategory2:
		w.WriteBool(1, p[2])
		w.WriteBool(1, p[3])
		w.WriteBool(0, p[6])
		w.WriteBool(1, p[7])
		writeCoeffExtraBits(w, token, mag)
	default:
		w.WriteBool(1, p[2])
		w.WriteBool(1, p[3])
		w.WriteBool(1, p[6])
		cat := token - tables.DCTValCategory3
		bit1 := uint8((cat >> 1) & 1)
		bit0 := uint8(cat & 1)
		w.WriteBool(bit1, p[8])
		w.WriteBool(bit0, p[9+int(bit1)])
		writeCoeffExtraBits(w, token, mag)
	}
}

func writeCoeffExtraBits(w *BoolWriter, token int, mag int) {
	extra := tables.ExtraBitsTable[token]
	offset := mag - int(extra.BaseVal)
	for i := 0; i < int(extra.Len); i++ {
		shift := int(extra.Len) - 1 - i
		w.WriteBool(uint8((offset>>uint(shift))&1), extra.Prob[i])
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
