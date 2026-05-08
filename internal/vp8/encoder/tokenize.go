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

type MacroblockCoefficients struct {
	QCoeff [25][16]int16
	EOB    [25]uint8

	eobMask uint32

	// OracleY1DCEOB1 tracks, per Y block 0..15, whether libvpx's
	// vp8_quantize_mb would have produced *d->eob = 1 against the original
	// (un-zeroed) DC coefficient using the Y1 DC quantizer. govpx zeros
	// dct[0] before quantize because that DC is hoisted into the Y2
	// second-order block (this matches libvpx's bitstream tokenize, which
	// starts at c=1 for Y_NO_DC), so coeffs.EOB[block] never reflects the
	// libvpx-side DC bump. The libvpx-side oracle captures eobs *after*
	// vp8_dequant_idct_add_y_block has memset qcoeff[0..1] back to zero,
	// at which point eob=1 with all-zero qcoeff is the visible state for
	// any Y block whose original dct[0] satisfied Y1DC's zbin/round/quant.
	//
	// This field is populated by buildPredictedMacroblockCoefficientsRD
	// when it has access to the original dct[0] and the segment's Y1DC
	// quantizer. It is used by emitOracleMBTrace to bump per-block eob
	// from 0 to 1 in trace rows so the eob match-rate scoreboard matches
	// libvpx. It does not influence bitstream emission, reconstruction,
	// or any other encoding decision.
	OracleY1DCEOB1 [16]uint8

	// OracleStaleY2EOB and OracleStaleY2QCoeff carry a "would-be" Y2
	// second-order block snapshot for SPLITMV/B_PRED macroblocks. libvpx's
	// vp8_quantize_mb skips block 24 when has_2nd_order is false, so
	// xd->block[24].qcoeff and xd->eobs[24] retain stale data from
	// whichever earlier RD-pick mode last quantized Y2. The libvpx oracle
	// trace captures that stale state, which makes the per-MB eob_sum
	// scoreboard diverge from govpx (which keeps block 24 zero for
	// SPLITMV/B_PRED). This field lets the trace emitter mirror libvpx's
	// stale Y2 contribution by running govpx's chosen-mode predictor
	// through the Y2 walsh + quantize even when the actual encoder path
	// skips it. Only consulted by emitOracleMBTrace; never feeds
	// bitstream emission, reconstruction, or any RD decision.
	OracleStaleY2EOB    uint8
	OracleStaleY2QCoeff [16]int16
	OracleStaleY2Set    bool
}

const (
	cached4x4EOBMask        = (uint32(1) << 24) - 1
	cachedWholeBlockEOBMask = (uint32(1) << 25) - 1
)

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
	if coeffs.eobCacheComplete(is4x4) {
		return writeCoefficientMacroblockTokensCached(w, probs, is4x4, above, left, coeffs)
	}

	blockType := 0
	skipDC := 0
	if !is4x4 {
		eob := coeffs.BlockEOB(24, 0)
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
		eob := coeffs.BlockEOB(block, skipDC)
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
		eob := coeffs.BlockEOB(block, 0)
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

func writeCoefficientMacroblockTokensCached(w *BoolWriter, probs *tables.CoefficientProbs, is4x4 bool, above *TokenContextPlanes, left *TokenContextPlanes, coeffs *MacroblockCoefficients) error {
	blockType := 0
	skipDC := 0
	if !is4x4 {
		eob := int(coeffs.EOB[24])
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
		eob := int(coeffs.EOB[block])
		if eob < skipDC {
			eob = skipDC
		}
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
		eob := int(coeffs.EOB[block])
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

	for col := 0; col < cols; col++ {
		above[col] = TokenContextPlanes{}
	}
	for row := 0; row < rows; row++ {
		w := &writers[row&(partitions-1)]
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
	coeffs.eobMask |= 1 << uint(block)
}

func (coeffs *MacroblockCoefficients) BlockEOB(block int, skipDC int) int {
	if coeffs == nil || block < 0 || block >= len(coeffs.QCoeff) {
		return skipDC
	}
	if coeffs.eobMask&(1<<uint(block)) != 0 {
		eob := int(coeffs.EOB[block])
		if eob < skipDC {
			return skipDC
		}
		return eob
	}
	return BlockCoeffEOB(&coeffs.QCoeff[block], skipDC)
}

func (coeffs *MacroblockCoefficients) eobCacheComplete(is4x4 bool) bool {
	mask := cachedWholeBlockEOBMask
	if is4x4 {
		mask = cached4x4EOBMask
	}
	return coeffs.eobMask&mask == mask
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
	for block := 0; block < 16; block++ {
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
