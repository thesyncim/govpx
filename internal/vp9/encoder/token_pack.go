package encoder

import (
	"errors"
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// ErrTokenBufferFull is returned when staged coefficient token storage is too
// small for the block or frame being tokenized.
var ErrTokenBufferFull = errors.New("encoder: VP9 token buffer full")

// coefProbsFlatLen is the flattened byte length of vp9dec.FrameCoefProbs
// ([TxSizes][CoefPlaneTypes][CoefRefTypes][CoefBands][CoefContexts]
// [UnconstrainedNodes]uint8).
const coefProbsFlatLen = int(common.TxSizes) * vp9dec.CoefPlaneTypes *
	vp9dec.CoefRefTypes * vp9dec.CoefBands * vp9dec.CoefContexts *
	UnconstrainedNodes

// coefProbsBaseOff returns the flat byte offset of fc[tx][planeType][isInter]
// inside FrameCoefProbs. Per-token rows add (band*CoefContexts+ctx)*
// UnconstrainedNodes.
func coefProbsBaseOff(tx common.TxSize, planeType, isInter int) int {
	return ((int(tx)*vp9dec.CoefPlaneTypes+planeType)*vp9dec.CoefRefTypes +
		isInter) * vp9dec.CoefBands * vp9dec.CoefContexts * UnconstrainedNodes
}

// CoefEOBToken returns the sole token for an all-zero transform block.
func CoefEOBToken(tx common.TxSize, planeType, isInter, initCtx int) (TokenExtra, bool) {
	if tx >= common.TxSizes || planeType < 0 || planeType >= vp9dec.CoefPlaneTypes ||
		isInter < 0 || isInter >= vp9dec.CoefRefTypes ||
		initCtx < 0 || initCtx >= vp9dec.CoefContexts {
		return TokenExtra{}, false
	}
	return TokenExtra{
		Token:   EobToken,
		ProbOff: uint16(coefProbsBaseOff(tx, planeType, isInter) + initCtx*UnconstrainedNodes),
	}, true
}

// CountCoefEOBTokens commits branch counts for validated all-zero blocks.
func CountCoefEOBTokens(tokens []TokenExtra, stats *FrameCoefBranchStats) bool {
	if stats == nil {
		return false
	}
	for _, tok := range tokens {
		probOff := int(tok.ProbOff)
		if tok.Token != EobToken || probOff%UnconstrainedNodes != 0 ||
			probOff < 0 || probOff >= coefProbsFlatLen {
			return false
		}
		row := probOff / UnconstrainedNodes
		ctx := row % vp9dec.CoefContexts
		row /= vp9dec.CoefContexts
		band := row % vp9dec.CoefBands
		row /= vp9dec.CoefBands
		ref := row % vp9dec.CoefRefTypes
		row /= vp9dec.CoefRefTypes
		plane := row % vp9dec.CoefPlaneTypes
		row /= vp9dec.CoefPlaneTypes
		if row < 0 || row >= int(common.TxSizes) {
			return false
		}
		recordCoefBranch00(&stats[row][plane][ref][band][ctx])
	}
	return true
}

// StageCoefBlock mirrors libvpx tokenize_b. It records coefficient tokens and
// branch counts without writing them, so a later pack pass can replay the same
// coefficient syntax after compressed-header probability updates.
func StageCoefBlock(dst []TokenExtra, a WriteCoefBlockArgs) (n int, eob int, ok bool) {
	maxEob := vp9dec.MaxEobForTxSize(a.TxSize)
	if len(dst) < maxEob {
		return stageCoefBlockChecked(dst, a, maxEob)
	}
	return stageCoefBlockFullWindow(dst[:maxEob], a, maxEob)
}

func stageCoefBlockFullWindow(tokens []TokenExtra, a WriteCoefBlockArgs, maxEob int) (n int, eob int, ok bool) {
	bandTrans := vp9dec.BandTranslateForTxSize(a.TxSize)
	scan := a.Scan
	_ = tokens[maxEob-1]
	_ = scan[maxEob-1]
	_ = bandTrans[maxEob-1]
	_ = a.Neighbors[(maxEob<<1)-1]
	dq := [2]int16{a.DequantDC, a.DequantAC}
	qcoeffs := a.QCoeffs
	if len(qcoeffs) >= maxEob {
		if len(a.TokenClasses) >= maxEob {
			return stageCoefBlockQCoeffClasses(tokens, a, maxEob, scan, bandTrans,
				qcoeffs, a.TokenClasses)
		}
		return stageCoefBlockQCoeff(tokens, a, maxEob, scan, bandTrans, qcoeffs)
	}
	qcoeffs = nil
	if a.KnownEOBValid && a.KnownEOB >= 0 && a.KnownEOB <= maxEob {
		eob = a.KnownEOB
	} else {
		eob = coeffBlockEOBEncode(scan, maxEob, a.Coeffs, qcoeffs)
	}
	if a.EOB != nil {
		*a.EOB = eob
	}

	// libvpx tokenize_b keeps token_cache as an uninitialized stack array:
	// every context read targets a position the current walk has already
	// written (the neighbor tables only reference earlier scan positions),
	// so a caller-provided dirty scratch is byte-equivalent and avoids
	// zeroing 1KB per transform block.
	tokenCache := a.TokenCache
	if tokenCache == nil {
		var local [1024]uint8
		tokenCache = &local
	}
	baseOff := coefProbsBaseOff(a.TxSize, a.PlaneType, a.IsInter)
	branchStatsRows := coefBranchStatsRowsFor(a.CoefBranchStats, a.TxSize,
		a.PlaneType, a.IsInter)
	ctx := a.InitCtx
	c := 0
	for c < maxEob {
		band := int(bandTrans[c])
		branchStats := coefBranchStatsSlot(branchStatsRows, band, ctx)
		probOff := uint16(baseOff + (band*vp9dec.CoefContexts+ctx)*UnconstrainedNodes)
		if c == eob {
			recordCoefBranch00(branchStats)
			tokens[c] = TokenExtra{Token: EobToken, ProbOff: probOff}
			return c + 1, eob, true
		}
		recordCoefBranch01(branchStats)

		raster := int(scan[c])
		for !coeffBlockHasCoeffAtRaster(raster, a.Coeffs, qcoeffs) {
			recordCoefBranch10(branchStats)
			tokens[c] = TokenExtra{Token: ZeroToken, ProbOff: probOff}
			tokenCacheSet(tokenCache, raster, 0)
			c++
			if c >= maxEob {
				return maxEob, eob, true
			}
			ctx = tokenCacheContext(a.Neighbors, tokenCache, c)
			band = int(bandTrans[c])
			branchStats = coefBranchStatsSlot(branchStatsRows, band, ctx)
			probOff = uint16(baseOff + (band*vp9dec.CoefContexts+ctx)*UnconstrainedNodes)
			raster = int(scan[c])
		}

		recordCoefBranch11(branchStats)
		dqv := dq[1]
		if c == 0 {
			dqv = dq[0]
		}
		var absVal, sign int
		if qcoeffs != nil {
			absVal, sign = coeffMagnitudeAndSignQ(qcoeffs[raster])
		} else {
			absVal, sign = coeffMagnitudeAndSignDQ(a.Coeffs[raster],
				dqv, a.TxSize == common.Tx32x32)
		}
		token, extra := TokenForAbsCoeff(absVal)
		tokens[c] = TokenExtra{
			Token:   int16(token),
			Extra:   int16((extra << 1) | sign),
			ProbOff: probOff,
		}
		recordCoefTokenBranches(token, branchStats)

		tokenCacheSet(tokenCache, raster, PtEnergyClass[token])
		c++
		if c < maxEob {
			ctx = tokenCacheContext(a.Neighbors, tokenCache, c)
		}
	}
	return maxEob, eob, true
}

func stageCoefBlockQCoeff(
	tokens []TokenExtra, a WriteCoefBlockArgs, maxEob int, scan []int16,
	bandTrans []uint8, qcoeffs []int16,
) (n int, eob int, ok bool) {
	_ = tokens[maxEob-1]
	_ = scan[maxEob-1]
	_ = bandTrans[maxEob-1]
	_ = qcoeffs[maxEob-1]
	_ = a.Neighbors[(maxEob<<1)-1]
	if a.KnownEOBValid && a.KnownEOB >= 0 && a.KnownEOB <= maxEob {
		eob = a.KnownEOB
	} else {
		eob = coeffBlockEOBCompleteQCoeffWindow(scan, maxEob, qcoeffs)
	}
	if a.EOB != nil {
		*a.EOB = eob
	}

	tokenCache := a.TokenCache
	if tokenCache == nil {
		var local [1024]uint8
		tokenCache = &local
	}
	baseOff := coefProbsBaseOff(a.TxSize, a.PlaneType, a.IsInter)
	branchStatsRows := coefBranchStatsRowsFor(a.CoefBranchStats, a.TxSize,
		a.PlaneType, a.IsInter)
	ctx := a.InitCtx
	c := 0
	for c < maxEob {
		band := int(bandTrans[c])
		branchStats := coefBranchStatsSlot(branchStatsRows, band, ctx)
		probOff := uint16(baseOff + (band*vp9dec.CoefContexts+ctx)*UnconstrainedNodes)
		if c == eob {
			recordCoefBranch00(branchStats)
			tokens[c] = TokenExtra{Token: EobToken, ProbOff: probOff}
			return c + 1, eob, true
		}
		recordCoefBranch01(branchStats)

		raster := int(scan[c])
		for qcoeffAt(qcoeffs, raster) == 0 {
			recordCoefBranch10(branchStats)
			tokens[c] = TokenExtra{Token: ZeroToken, ProbOff: probOff}
			tokenCacheSet(tokenCache, raster, 0)
			c++
			if c >= maxEob {
				return maxEob, eob, true
			}
			ctx = tokenCacheContext(a.Neighbors, tokenCache, c)
			band = int(bandTrans[c])
			branchStats = coefBranchStatsSlot(branchStatsRows, band, ctx)
			probOff = uint16(baseOff + (band*vp9dec.CoefContexts+ctx)*UnconstrainedNodes)
			raster = int(scan[c])
		}

		recordCoefBranch11(branchStats)
		token, extra, energy := coeffTokenExtraQCoeff(qcoeffAt(qcoeffs, raster))
		tokens[c] = TokenExtra{
			Token:   int16(token),
			Extra:   extra,
			ProbOff: probOff,
		}
		recordCoefTokenBranches(token, branchStats)

		tokenCacheSet(tokenCache, raster, energy)
		c++
		if c < maxEob {
			ctx = tokenCacheContext(a.Neighbors, tokenCache, c)
		}
	}
	return maxEob, eob, true
}

// stageCoefBlockQCoeffClasses is the fused-quantizer sibling of
// stageCoefBlockQCoeff: the quantizer scan already produced the
// per-raster-position token energy classes for these qcoeffs, so the walk
// reads zero-run state and neighbor contexts from that span instead of
// deriving and re-writing the incremental token cache. Every neighbor read
// targets a position earlier in scan order, where the precomputed class is
// exactly the value the incremental walk would have written, so the staged
// tokens and branch counts are byte-identical.
func stageCoefBlockQCoeffClasses(
	tokens []TokenExtra, a WriteCoefBlockArgs, maxEob int, scan []int16,
	bandTrans []uint8, qcoeffs []int16, classes []uint8,
) (n int, eob int, ok bool) {
	_ = tokens[maxEob-1]
	_ = scan[maxEob-1]
	_ = bandTrans[maxEob-1]
	_ = qcoeffs[maxEob-1]
	_ = classes[maxEob-1]
	_ = a.Neighbors[(maxEob<<1)-1]
	if a.KnownEOBValid && a.KnownEOB >= 0 && a.KnownEOB <= maxEob {
		eob = a.KnownEOB
	} else {
		eob = coeffBlockEOBCompleteQCoeffWindow(scan, maxEob, qcoeffs)
	}
	if a.EOB != nil {
		*a.EOB = eob
	}

	baseOff := coefProbsBaseOff(a.TxSize, a.PlaneType, a.IsInter)
	branchStatsRows := coefBranchStatsRowsFor(a.CoefBranchStats, a.TxSize,
		a.PlaneType, a.IsInter)
	ctx := a.InitCtx
	c := 0
	for c < maxEob {
		band := int(bandTrans[c])
		branchStats := coefBranchStatsSlot(branchStatsRows, band, ctx)
		probOff := uint16(baseOff + (band*vp9dec.CoefContexts+ctx)*UnconstrainedNodes)
		if c == eob {
			recordCoefBranch00(branchStats)
			tokens[c] = TokenExtra{Token: EobToken, ProbOff: probOff}
			return c + 1, eob, true
		}
		recordCoefBranch01(branchStats)

		raster := int(scan[c])
		for tokenClassAt(classes, raster) == 0 {
			recordCoefBranch10(branchStats)
			tokens[c] = TokenExtra{Token: ZeroToken, ProbOff: probOff}
			c++
			if c >= maxEob {
				return maxEob, eob, true
			}
			ctx = tokenClassContext(a.Neighbors, classes, c)
			band = int(bandTrans[c])
			branchStats = coefBranchStatsSlot(branchStatsRows, band, ctx)
			probOff = uint16(baseOff + (band*vp9dec.CoefContexts+ctx)*UnconstrainedNodes)
			raster = int(scan[c])
		}

		recordCoefBranch11(branchStats)
		token, extra, _ := coeffTokenExtraQCoeff(qcoeffAt(qcoeffs, raster))
		tokens[c] = TokenExtra{
			Token:   int16(token),
			Extra:   extra,
			ProbOff: probOff,
		}
		recordCoefTokenBranches(token, branchStats)

		c++
		if c < maxEob {
			ctx = tokenClassContext(a.Neighbors, classes, c)
		}
	}
	return maxEob, eob, true
}

// stageCoefBlockChecked preserves the small-buffer contract for defensive
// callers; the normal production path above gets a full transform-sized token
// window and avoids the per-token capacity helper.
func stageCoefBlockChecked(dst []TokenExtra, a WriteCoefBlockArgs, maxEob int) (n int, eob int, ok bool) {
	scan := a.Scan[:maxEob]
	bandTrans := vp9dec.BandTranslateForTxSize(a.TxSize)[:maxEob]
	dq := [2]int16{a.DequantDC, a.DequantAC}
	qcoeffs := a.QCoeffs
	if len(qcoeffs) < maxEob {
		qcoeffs = nil
	}
	if a.KnownEOBValid && a.KnownEOB >= 0 && a.KnownEOB <= maxEob {
		eob = a.KnownEOB
	} else {
		eob = coeffBlockEOBEncode(scan, maxEob, a.Coeffs, qcoeffs)
	}
	if a.EOB != nil {
		*a.EOB = eob
	}

	tokenCache := a.TokenCache
	if tokenCache == nil {
		var local [1024]uint8
		tokenCache = &local
	}
	baseOff := coefProbsBaseOff(a.TxSize, a.PlaneType, a.IsInter)
	branchStatsRows := coefBranchStatsRowsFor(a.CoefBranchStats, a.TxSize,
		a.PlaneType, a.IsInter)
	ctx := a.InitCtx
	c := 0
	for c < maxEob {
		band := int(bandTrans[c])
		branchStats := coefBranchStatsSlot(branchStatsRows, band, ctx)
		probOff := uint16(baseOff + (band*vp9dec.CoefContexts+ctx)*UnconstrainedNodes)
		if c == eob {
			recordCoefBranch00(branchStats)
			if !stageToken(dst, &n, probOff, EobToken, 0) {
				return n, eob, false
			}
			return n, eob, true
		}
		recordCoefBranch01(branchStats)

		raster := int(scan[c])
		for !coeffBlockHasCoeffAtRaster(raster, a.Coeffs, qcoeffs) {
			recordCoefBranch10(branchStats)
			if !stageToken(dst, &n, probOff, ZeroToken, 0) {
				return n, eob, false
			}
			tokenCache[raster] = 0
			c++
			if c >= maxEob {
				return n, eob, true
			}
			ctx = vp9dec.GetCoefContext(a.Neighbors, tokenCache, c)
			band = int(bandTrans[c])
			branchStats = coefBranchStatsSlot(branchStatsRows, band, ctx)
			probOff = uint16(baseOff + (band*vp9dec.CoefContexts+ctx)*UnconstrainedNodes)
			raster = int(scan[c])
		}

		recordCoefBranch11(branchStats)
		dqv := dq[1]
		if c == 0 {
			dqv = dq[0]
		}
		var absVal, sign int
		if qcoeffs != nil {
			absVal, sign = coeffMagnitudeAndSignQ(qcoeffAt(qcoeffs, raster))
		} else {
			absVal, sign = coeffMagnitudeAndSignDQ(a.Coeffs[raster],
				dqv, a.TxSize == common.Tx32x32)
		}
		token, extra := TokenForAbsCoeff(absVal)
		if !stageToken(dst, &n, probOff, token, (extra<<1)|sign) {
			return n, eob, false
		}
		recordCoefTokenBranches(token, branchStats)

		tokenCache[raster] = PtEnergyClass[token]
		c++
		if c < maxEob {
			ctx = vp9dec.GetCoefContext(a.Neighbors, tokenCache, c)
		}
	}
	return n, eob, true
}

func stageToken(dst []TokenExtra, n *int, probOff uint16, token, extra int) bool {
	if *n >= len(dst) {
		return false
	}
	dst[*n] = TokenExtra{
		Token:   int16(token),
		Extra:   int16(extra),
		ProbOff: probOff,
	}
	*n = *n + 1
	return true
}

func recordCoefTokenBranches(token int, branchStats *[EntropyNodes][2]uint32) {
	if token == OneToken {
		if branchStats != nil {
			branchStats[PivotNode][0]++
		}
		return
	}
	if token < TwoToken || token > Category6Tok {
		panic("encoder: invalid VP9 coefficient token")
	}
	if branchStats == nil {
		return
	}
	branchStats[PivotNode][1]++
	recordCoefTokenTailBranches(token, branchStats)
}

const (
	coefTailLowValNode = UnconstrainedNodes + iota
	coefTailTwoNode
	coefTailThreeFourNode
	coefTailHighLowNode
	coefTailCatOneNode
	coefTailCatThreeFourNode
	coefTailCatThreeNode
	coefTailCatFiveNode
)

func recordCoefTokenTailBranches(token int, branchStats *[EntropyNodes][2]uint32) {
	if token < TwoToken || token > Category6Tok {
		panic("encoder: invalid VP9 coefficient token")
	}
	if branchStats == nil {
		return
	}
	switch token {
	case TwoToken:
		branchStats[coefTailLowValNode][0]++
		branchStats[coefTailTwoNode][0]++
	case ThreeToken:
		branchStats[coefTailLowValNode][0]++
		branchStats[coefTailTwoNode][1]++
		branchStats[coefTailThreeFourNode][0]++
	case FourToken:
		branchStats[coefTailLowValNode][0]++
		branchStats[coefTailTwoNode][1]++
		branchStats[coefTailThreeFourNode][1]++
	case Category1Tok:
		branchStats[coefTailLowValNode][1]++
		branchStats[coefTailHighLowNode][0]++
		branchStats[coefTailCatOneNode][0]++
	case Category2Tok:
		branchStats[coefTailLowValNode][1]++
		branchStats[coefTailHighLowNode][0]++
		branchStats[coefTailCatOneNode][1]++
	case Category3Tok:
		branchStats[coefTailLowValNode][1]++
		branchStats[coefTailHighLowNode][1]++
		branchStats[coefTailCatThreeFourNode][0]++
		branchStats[coefTailCatThreeNode][0]++
	case Category4Tok:
		branchStats[coefTailLowValNode][1]++
		branchStats[coefTailHighLowNode][1]++
		branchStats[coefTailCatThreeFourNode][0]++
		branchStats[coefTailCatThreeNode][1]++
	case Category5Tok:
		branchStats[coefTailLowValNode][1]++
		branchStats[coefTailHighLowNode][1]++
		branchStats[coefTailCatThreeFourNode][1]++
		branchStats[coefTailCatFiveNode][0]++
	case Category6Tok:
		branchStats[coefTailLowValNode][1]++
		branchStats[coefTailHighLowNode][1]++
		branchStats[coefTailCatThreeFourNode][1]++
		branchStats[coefTailCatFiveNode][1]++
	}
}

// PackTokens mirrors libvpx pack_mb_tokens for staged coefficient tokens.
func PackTokens(bw *bitstream.Writer, tokens []TokenExtra, fc *vp9dec.FrameCoefProbs) int {
	i := 0
	for i < len(tokens) {
		tok := tokens[i]
		if tok.Token == EOSBToken {
			return i + 1
		}
		probs := stagedTokenProbs(fc, tok)
		if tok.Token == EobToken {
			bw.Write(0, uint32(probs[0]))
			i++
			continue
		}
		bw.Write(1, uint32(probs[0]))
		for tok.Token == ZeroToken {
			bw.Write(0, uint32(probs[1]))
			i++
			if i >= len(tokens) {
				return i
			}
			tok = tokens[i]
			if tok.Token == EOSBToken {
				return i + 1
			}
			probs = stagedTokenProbs(fc, tok)
		}

		token := int(tok.Token)
		if token < OneToken || token > Category6Tok {
			panic("encoder: invalid staged VP9 coefficient token")
		}
		extra := int(tok.Extra)
		writePackedCoefTokenBodyAfterNotZero(bw, token, extra>>1, extra&1,
			probs[1], probs[2])
		i++
	}
	return i
}

func packTokenBlockAndHasResidue(
	bw *bitstream.Writer, tokens []TokenExtra, start, maxEob int, fc *vp9dec.FrameCoefProbs,
) (bool, int, bool) {
	if maxEob <= 0 || start < 0 || start > len(tokens) {
		return false, 0, false
	}
	if len(tokens)-start >= maxEob {
		return packTokenBlockAndHasResidueWindow(bw, tokens[start:start+maxEob], fc)
	}
	hasResidue := false
	end := start + maxEob
	c := start
	for c < end {
		if c >= len(tokens) {
			return false, 0, false
		}
		tok := tokens[c]
		if tok.Token == EOSBToken {
			return false, 0, false
		}
		probs := stagedTokenProbs(fc, tok)
		if tok.Token == EobToken {
			bw.Write(0, uint32(probs[0]))
			return hasResidue, c - start + 1, true
		}
		bw.Write(1, uint32(probs[0]))
		for tok.Token == ZeroToken {
			bw.Write(0, uint32(probs[1]))
			c++
			if c >= end {
				return hasResidue, c - start, true
			}
			if c >= len(tokens) {
				return false, 0, false
			}
			tok = tokens[c]
			if tok.Token == EOSBToken || tok.Token == EobToken {
				return false, 0, false
			}
			probs = stagedTokenProbs(fc, tok)
		}

		hasResidue = true
		token := int(tok.Token)
		if token < OneToken || token > Category6Tok {
			panic("encoder: invalid staged VP9 coefficient token")
		}
		extra := int(tok.Extra)
		writePackedCoefTokenBodyAfterNotZero(bw, token, extra>>1, extra&1,
			probs[1], probs[2])
		c++
	}
	return hasResidue, maxEob, true
}

func packTokenBlockAndHasResidueWindow(
	bw *bitstream.Writer, tokens []TokenExtra, fc *vp9dec.FrameCoefProbs,
) (bool, int, bool) {
	hasResidue := false
	consumed := 0
	for len(tokens) > 0 {
		tok := tokens[0]
		tokens = tokens[1:]
		consumed++
		if tok.Token == EOSBToken {
			return false, 0, false
		}
		probs := stagedTokenProbs(fc, tok)
		if tok.Token == EobToken {
			bw.Write(0, uint32(probs[0]))
			return hasResidue, consumed, true
		}
		bw.Write(1, uint32(probs[0]))
		for tok.Token == ZeroToken {
			bw.Write(0, uint32(probs[1]))
			if len(tokens) == 0 {
				return hasResidue, consumed, true
			}
			tok = tokens[0]
			tokens = tokens[1:]
			consumed++
			if tok.Token == EOSBToken || tok.Token == EobToken {
				return false, 0, false
			}
			probs = stagedTokenProbs(fc, tok)
		}

		hasResidue = true
		token := int(tok.Token)
		if token < OneToken || token > Category6Tok {
			panic("encoder: invalid staged VP9 coefficient token")
		}
		extra := int(tok.Extra)
		writePackedCoefTokenBodyAfterNotZero(bw, token, extra>>1, extra&1,
			probs[1], probs[2])
	}
	return hasResidue, consumed, true
}

func writePackedCoefTokenTail(bw *bitstream.Writer, token int, probs *[8]uint8) {
	switch token {
	case TwoToken:
		bw.WritePacked(0b00, uint32(probs[0])<<8|uint32(probs[1]), 2)
	case ThreeToken:
		bw.WritePacked(0b010, uint32(probs[0])<<16|uint32(probs[1])<<8|
			uint32(probs[2]), 3)
	case FourToken:
		bw.WritePacked(0b011, uint32(probs[0])<<16|uint32(probs[1])<<8|
			uint32(probs[2]), 3)
	case Category1Tok:
		bw.WritePacked(0b100, uint32(probs[0])<<16|uint32(probs[3])<<8|
			uint32(probs[4]), 3)
	case Category2Tok:
		bw.WritePacked(0b101, uint32(probs[0])<<16|uint32(probs[3])<<8|
			uint32(probs[4]), 3)
	case Category3Tok:
		bw.WritePacked(0b1100, uint32(probs[0])<<24|uint32(probs[3])<<16|
			uint32(probs[5])<<8|uint32(probs[6]), 4)
	case Category4Tok:
		bw.WritePacked(0b1101, uint32(probs[0])<<24|uint32(probs[3])<<16|
			uint32(probs[5])<<8|uint32(probs[6]), 4)
	case Category5Tok:
		bw.WritePacked(0b1110, uint32(probs[0])<<24|uint32(probs[3])<<16|
			uint32(probs[5])<<8|uint32(probs[7]), 4)
	case Category6Tok:
		bw.WritePacked(0b1111, uint32(probs[0])<<24|uint32(probs[3])<<16|
			uint32(probs[5])<<8|uint32(probs[7]), 4)
	default:
		panic("encoder: invalid staged VP9 coefficient token")
	}
}

func stagedTokenProbs(fc *vp9dec.FrameCoefProbs, tok TokenExtra) *[UnconstrainedNodes]uint8 {
	return (*[UnconstrainedNodes]uint8)(unsafe.Add(unsafe.Pointer(fc), uintptr(tok.ProbOff)))
}
