package decoder

import (
	"errors"
	"math/bits"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/decoder/detokenize.c GetCoeffs.

var ErrTokenGridBufferTooSmall = errors.New("govpx: VP8 token grid buffer too small")

var zeroMacroblockEOB [25]uint8

const macroblockTokensTrackedMask uint32 = 1 << 31

type EntropyContextPlanes struct {
	Y1 [4]uint8
	U  [2]uint8
	V  [2]uint8
	Y2 uint8
}

type MacroblockTokens struct {
	QCoeff    [25][16]int16
	EOB       [25]uint8
	dirtyMask uint32
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

	eobTotal, dirtyMask := br.ReadVP8MacroblockCoeffs(probs, is4x4,
		&above.Y1, &left.Y1, &above.U, &left.U, &above.V, &left.V,
		&above.Y2, &left.Y2, &out.QCoeff, &out.EOB)

	if dirtyMask != 0 || !is4x4 {
		dirtyMask |= macroblockTokensTrackedMask
	}
	out.dirtyMask = dirtyMask
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
	mask := out.dirtyMask
	if mask != 0 {
		for mask &^= macroblockTokensTrackedMask; mask != 0; mask &= mask - 1 {
			i := bits.TrailingZeros32(mask)
			out.QCoeff[i] = [16]int16{}
		}
		out.EOB = [25]uint8{}
		out.dirtyMask = 0
		return
	}
	if out.EOB == zeroMacroblockEOB {
		return
	}
	for i, eob := range out.EOB {
		if eob != 0 {
			out.QCoeff[i] = [16]int16{}
		}
	}
	out.EOB = [25]uint8{}
}

func clearMacroblockTokensIfNeeded(out *MacroblockTokens) {
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
