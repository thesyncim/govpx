package decoder

import (
	"errors"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/decoder/detokenize.c GetCoeffs.

var ErrTokenGridBufferTooSmall = errors.New("govpx: VP8 token grid buffer too small")

var zeroMacroblockEOB [25]uint8

type EntropyContextPlanes struct {
	Y1 [4]uint8
	U  [2]uint8
	V  [2]uint8
	Y2 uint8
}

type MacroblockTokens struct {
	QCoeff [25][16]int16
	EOB    [25]uint8
}

func ResetMacroblockTokenContext(above *EntropyContextPlanes, left *EntropyContextPlanes, is4x4 bool) {
	if !is4x4 {
		*above = EntropyContextPlanes{}
		*left = EntropyContextPlanes{}
		return
	}

	aboveY2, leftY2 := above.Y2, left.Y2
	*above = EntropyContextPlanes{Y2: aboveY2}
	*left = EntropyContextPlanes{Y2: leftY2}
}

func DecodeMacroblockTokens(br *boolcoder.Decoder, probs *tables.CoefficientProbs, is4x4 bool, above *EntropyContextPlanes, left *EntropyContextPlanes, out *MacroblockTokens) int {
	clearMacroblockTokens(out)

	blockType := 0
	skipDC := 0
	eobTotal := 0

	if !is4x4 {
		ctx := int(above.Y2 + left.Y2)
		nonzeros := DecodeBlockCoeffs(br, probs, 1, ctx, 0, &out.QCoeff[24])
		hasCoeffs := uint8(0)
		if nonzeros > 0 {
			hasCoeffs = 1
		}
		above.Y2 = hasCoeffs
		left.Y2 = hasCoeffs
		out.EOB[24] = uint8(nonzeros)
		eobTotal += nonzeros - 16

		blockType = 0
		skipDC = 1
	} else {
		blockType = 3
	}

	for i := range 16 {
		a := i & 3
		l := (i & 0x0c) >> 2
		ctx := int(above.Y1[a] + left.Y1[l])
		nonzeros := DecodeBlockCoeffs(br, probs, blockType, ctx, skipDC, &out.QCoeff[i])
		hasCoeffs := uint8(0)
		if nonzeros > 0 {
			hasCoeffs = 1
		}
		above.Y1[a] = hasCoeffs
		left.Y1[l] = hasCoeffs

		nonzeros += skipDC
		out.EOB[i] = uint8(nonzeros)
		eobTotal += nonzeros
	}

	for i := 16; i < 24; i++ {
		a, l := common.UVTokenContextIndex(i)
		ctx := int(getUVContext(above, a) + getUVContext(left, l))
		nonzeros := DecodeBlockCoeffs(br, probs, 2, ctx, 0, &out.QCoeff[i])
		hasCoeffs := uint8(0)
		if nonzeros > 0 {
			hasCoeffs = 1
		}
		setUVContext(above, a, hasCoeffs)
		setUVContext(left, l, hasCoeffs)

		out.EOB[i] = uint8(nonzeros)
		eobTotal += nonzeros
	}

	return eobTotal
}

func DecodeTokenGrid(readers []boolcoder.Decoder, rows int, cols int, probs *tables.CoefficientProbs, modes []MacroblockMode, above []EntropyContextPlanes, tokens []MacroblockTokens) (int, error) {
	if rows < 0 || cols < 0 {
		return 0, ErrTokenGridBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return 0, ErrTokenGridBufferTooSmall
	}
	required := rows * cols
	if len(modes) < required || len(tokens) < required || len(above) < cols {
		return 0, ErrTokenGridBufferTooSmall
	}
	partitions := len(readers)
	if partitions != 1 && partitions != 2 && partitions != 4 && partitions != 8 {
		return 0, ErrTokenGridBufferTooSmall
	}

	for col := range cols {
		above[col] = EntropyContextPlanes{}
	}

	total := 0
	for row := range rows {
		rowPartition := row & (partitions - 1)
		rowStart := row * cols
		rowModes := modes[rowStart : rowStart+cols]
		rowTokens := tokens[rowStart : rowStart+cols]
		left := EntropyContextPlanes{}
		for col := range cols {
			mode := &rowModes[col]
			token := &rowTokens[col]
			if mode.MBSkipCoeff {
				clearMacroblockTokensIfNeeded(token)
				ResetMacroblockTokenContext(&above[col], &left, mode.Is4x4)
				continue
			}
			eobTotal := DecodeMacroblockTokens(&readers[rowPartition], probs, mode.Is4x4, &above[col], &left, token)
			if eobTotal == 0 {
				mode.MBSkipCoeff = true
			}
			total += eobTotal
		}
	}
	return total, nil
}

func DecodeTokenGridWithErrorConcealment(readers []boolcoder.Decoder, rows int, cols int, probs *tables.CoefficientProbs, modes []MacroblockMode, above []EntropyContextPlanes, tokens []MacroblockTokens) (int, int, error) {
	if rows < 0 || cols < 0 {
		return 0, 0, ErrTokenGridBufferTooSmall
	}
	if rows != 0 && cols > int(^uint(0)>>1)/rows {
		return 0, 0, ErrTokenGridBufferTooSmall
	}
	required := rows * cols
	if len(modes) < required || len(tokens) < required || len(above) < cols {
		return 0, 0, ErrTokenGridBufferTooSmall
	}
	partitions := len(readers)
	if partitions != 1 && partitions != 2 && partitions != 4 && partitions != 8 {
		return 0, 0, ErrTokenGridBufferTooSmall
	}

	for col := range cols {
		above[col] = EntropyContextPlanes{}
	}

	total := 0
	firstCorrupt := required
	frameCorruptResidual := false
	for row := range rows {
		rowPartition := row & (partitions - 1)
		rowStart := row * cols
		rowModes := modes[rowStart : rowStart+cols]
		rowTokens := tokens[rowStart : rowStart+cols]
		left := EntropyContextPlanes{}
		for col := range cols {
			index := rowStart + col
			mode := &rowModes[col]
			token := &rowTokens[col]
			reader := &readers[rowPartition]
			if mode.MBSkipCoeff {
				clearMacroblockTokensIfNeeded(token)
				ResetMacroblockTokenContext(&above[col], &left, mode.Is4x4)
			} else if !frameCorruptResidual && reader.Err() == nil {
				eobTotal := DecodeMacroblockTokens(reader, probs, mode.Is4x4, &above[col], &left, token)
				if eobTotal == 0 {
					mode.MBSkipCoeff = true
				}
				total += eobTotal
			}
			if frameCorruptResidual || reader.Err() != nil {
				frameCorruptResidual = true
				if index < firstCorrupt {
					firstCorrupt = index
				}
				clearMacroblockTokensIfNeeded(token)
				mode.MBSkipCoeff = true
			}
		}
	}
	return total, firstCorrupt, nil
}

func DecodeBlockCoeffs(br *boolcoder.Decoder, probs *tables.CoefficientProbs, blockType int, ctx int, n int, out *[16]int16) int {
	return br.ReadVP8BlockCoeffs(probs, blockType, ctx, n, out)
}

func clearMacroblockTokens(out *MacroblockTokens) {
	for i, eob := range out.EOB {
		if eob != 0 {
			out.QCoeff[i] = [16]int16{}
		}
	}
	out.EOB = [25]uint8{}
}

func clearMacroblockTokensIfNeeded(out *MacroblockTokens) {
	if out.EOB == zeroMacroblockEOB {
		return
	}
	clearMacroblockTokens(out)
}

func getUVContext(ctx *EntropyContextPlanes, index int) uint8 {
	if index < 2 {
		return ctx.U[index]
	}
	return ctx.V[index-2]
}

func setUVContext(ctx *EntropyContextPlanes, index int, value uint8) {
	if index < 2 {
		ctx.U[index] = value
		return
	}
	ctx.V[index-2] = value
}
