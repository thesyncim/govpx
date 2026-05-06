package decoder

import (
	"errors"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/decoder/detokenize.c GetCoeffs.

var ErrTokenGridBufferTooSmall = errors.New("govpx: VP8 token grid buffer too small")

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

	for i := 0; i < 16; i++ {
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
		a, l := uvContextIndex(i)
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

	for col := 0; col < cols; col++ {
		above[col] = EntropyContextPlanes{}
	}

	total := 0
	for row := 0; row < rows; row++ {
		rowPartition := row & (partitions - 1)
		rowStart := row * cols
		rowModes := modes[rowStart : rowStart+cols]
		rowTokens := tokens[rowStart : rowStart+cols]
		left := EntropyContextPlanes{}
		for col := 0; col < cols; col++ {
			mode := &rowModes[col]
			token := &rowTokens[col]
			if mode.MBSkipCoeff {
				clearMacroblockTokens(token)
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

func DecodeBlockCoeffs(br *boolcoder.Decoder, probs *tables.CoefficientProbs, blockType int, ctx int, n int, out *[16]int16) int {
	p := (*probs)[blockType][n][ctx]
	if br.ReadBool(p[0]) == 0 {
		return 0
	}

	for {
		n++
		if br.ReadBool(p[1]) == 0 {
			if n == 16 {
				return 16
			}
			p = (*probs)[blockType][tables.CoefBandsTable[n]][0]
		} else {
			v := 0
			tokenClass := uint8(2)
			if br.ReadBool(p[2]) == 0 {
				tokenClass = 1
				v = 1
			} else {
				if br.ReadBool(p[3]) == 0 {
					if br.ReadBool(p[4]) == 0 {
						v = 2
					} else {
						v = 3 + int(br.ReadBool(p[5]))
					}
				} else {
					if br.ReadBool(p[6]) == 0 {
						if br.ReadBool(p[7]) == 0 {
							v = 5 + int(br.ReadBool(159))
						} else {
							v = 7 + 2*int(br.ReadBool(165))
							v += int(br.ReadBool(145))
						}
					} else {
						bit1 := int(br.ReadBool(p[8]))
						bit0 := int(br.ReadBool(p[9+bit1]))
						cat := 2*bit1 + bit0
						v = readTokenCategory(br, cat)
					}
				}
			}

			j := tables.DefaultZigZag1D[n-1]
			out[j] = readSignedCoeff(br, v)
			if n == 16 {
				return 16
			}
			p = (*probs)[blockType][tables.CoefBandsTable[n]][tokenClass]
			if br.ReadBool(p[0]) == 0 {
				return n
			}
		}
		if n == 16 {
			return 16
		}
	}
}

func clearMacroblockTokens(out *MacroblockTokens) {
	for i, eob := range out.EOB {
		if eob != 0 {
			out.QCoeff[i] = [16]int16{}
		}
	}
	out.EOB = [25]uint8{}
}

func uvContextIndex(block int) (int, int) {
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

func readSignedCoeff(br *boolcoder.Decoder, value int) int16 {
	if br.ReadBit() != 0 {
		return int16(-value)
	}
	return int16(value)
}

func readTokenCategory(br *boolcoder.Decoder, cat int) int {
	v := 0
	switch cat {
	case 0:
		for _, prob := range tables.Cat3Prob {
			v += v + int(br.ReadBool(prob))
		}
	case 1:
		for _, prob := range tables.Cat4Prob {
			v += v + int(br.ReadBool(prob))
		}
	case 2:
		for _, prob := range tables.Cat5Prob {
			v += v + int(br.ReadBool(prob))
		}
	default:
		for _, prob := range tables.Cat6Prob {
			v += v + int(br.ReadBool(prob))
		}
	}
	return v + 3 + (8 << cat)
}
