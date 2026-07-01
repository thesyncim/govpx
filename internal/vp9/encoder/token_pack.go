package encoder

import (
	"errors"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// ErrTokenBufferFull is returned when staged coefficient token storage is too
// small for the block or frame being tokenized.
var ErrTokenBufferFull = errors.New("encoder: VP9 token buffer full")

// StageCoefBlock mirrors libvpx tokenize_b. It records coefficient tokens and
// branch counts without writing them, so a later pack pass can replay the same
// coefficient syntax after compressed-header probability updates.
func StageCoefBlock(dst []TokenExtra, a WriteCoefBlockArgs) (n int, eob int, ok bool) {
	maxEob := vp9dec.MaxEobForTxSize(a.TxSize)
	bandTrans := vp9dec.BandTranslateForTxSize(a.TxSize)
	dq := [2]int16{a.DequantDC, a.DequantAC}
	qcoeffs := a.QCoeffs
	if len(qcoeffs) < maxEob {
		qcoeffs = nil
	}
	if a.KnownEOBValid && a.KnownEOB >= 0 && a.KnownEOB <= maxEob {
		eob = a.KnownEOB
	} else {
		eob = coeffBlockEOBEncode(a.Scan, maxEob, a.Coeffs, qcoeffs)
	}
	if a.EOB != nil {
		*a.EOB = eob
	}

	var tokenCache [1024]uint8
	ctx := a.InitCtx
	bandIdx := 0
	c := 0
	for c < maxEob {
		band := int(bandTrans[bandIdx])
		bandIdx++
		branchStats := coefBranchStatsSlot(a.CoefBranchStats, a.TxSize,
			a.PlaneType, a.IsInter, band, ctx)
		if c == eob {
			recordCoefBranch(branchStats, 0, 0)
			if !stageToken(dst, &n, a, band, ctx, EobToken, 0) {
				return n, eob, false
			}
			return n, eob, true
		}
		recordCoefBranch(branchStats, 0, 1)

		for !CoeffBlockHasCoeff(a.Scan, c, a.Coeffs, qcoeffs) {
			recordCoefBranch(branchStats, 1, 0)
			if !stageToken(dst, &n, a, band, ctx, ZeroToken, 0) {
				return n, eob, false
			}
			tokenCache[a.Scan[c]] = 0
			c++
			if c >= maxEob {
				return n, eob, true
			}
			ctx = vp9dec.GetCoefContext(a.Neighbors, &tokenCache, c)
			band = int(bandTrans[bandIdx])
			bandIdx++
			branchStats = coefBranchStatsSlot(a.CoefBranchStats, a.TxSize,
				a.PlaneType, a.IsInter, band, ctx)
		}

		recordCoefBranch(branchStats, 1, 1)
		raster := a.Scan[c]
		dqv := dq[1]
		if c == 0 {
			dqv = dq[0]
		}
		absVal, sign := CoeffMagnitudeAndSign(qcoeffs, int(raster),
			a.Coeffs[raster], dqv, a.TxSize == common.Tx32x32)
		token, extra := TokenForAbsCoeff(absVal)
		if !stageToken(dst, &n, a, band, ctx, token, (extra<<1)|sign) {
			return n, eob, false
		}
		recordCoefTokenBranches(token, branchStats)

		tokenCache[raster] = PtEnergyClass[token]
		c++
		if c < maxEob {
			ctx = vp9dec.GetCoefContext(a.Neighbors, &tokenCache, c)
		}
	}
	return n, eob, true
}

func stageToken(dst []TokenExtra, n *int, a WriteCoefBlockArgs,
	band, ctx, token, extra int,
) bool {
	if *n >= len(dst) {
		return false
	}
	dst[*n] = TokenExtra{
		Token:     int16(token),
		Extra:     int16(extra),
		TxSize:    a.TxSize,
		PlaneType: uint8(a.PlaneType),
		RefType:   uint8(a.IsInter),
		Band:      uint8(band),
		Context:   uint8(ctx),
	}
	*n = *n + 1
	return true
}

func recordCoefTokenBranches(token int, branchStats *[EntropyNodes][2]uint32) {
	if token == OneToken {
		recordCoefBranch(branchStats, PivotNode, 0)
		return
	}
	recordCoefBranch(branchStats, PivotNode, 1)
	enc := CoefEncodings[token]
	bits := int(enc.Value)
	length := int(enc.Len) - UnconstrainedNodes
	i := int8(0)
	for length > 0 {
		length--
		bit := (bits >> uint(length)) & 1
		recordCoefBranch(branchStats, UnconstrainedNodes+int(i>>1), bit)
		i = CoefConTree[int(i)+bit]
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

		bw.Write(1, uint32(probs[1]))
		token := int(tok.Token)
		extra := int(tok.Extra)
		if token == OneToken {
			bw.Write(0, uint32(probs[2]))
			bw.WriteBit(uint32(extra & 1))
		} else {
			bw.Write(1, uint32(probs[2]))
			enc := CoefEncodings[token]
			pareto := tables.Pareto8Full[probs[2]-1]
			writeTreeBits(bw, CoefConTree[:], pareto[:], int(enc.Value),
				int(enc.Len)-UnconstrainedNodes)
			if token >= Category1Tok {
				eb := VP9ExtraBits[token]
				value := extra >> 1
				for bit := eb.Len - 1; bit >= 0; bit-- {
					bw.Write(uint32((value>>uint(bit))&1),
						uint32(eb.Prob[eb.Len-1-bit]))
				}
			}
			bw.WriteBit(uint32(extra & 1))
		}
		i++
	}
	return i
}

func stagedTokenProbs(fc *vp9dec.FrameCoefProbs, tok TokenExtra) *[UnconstrainedNodes]uint8 {
	return &(*fc)[tok.TxSize][tok.PlaneType][tok.RefType][tok.Band][tok.Context]
}
