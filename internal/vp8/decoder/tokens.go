package decoder

import (
	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/decoder/detokenize.c GetCoeffs.

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
			if br.ReadBool(p[2]) == 0 {
				p = (*probs)[blockType][tables.CoefBandsTable[n]][1]
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
				p = (*probs)[blockType][tables.CoefBandsTable[n]][2]
			}

			j := tables.DefaultZigZag1D[n-1]
			out[j] = readSignedCoeff(br, v)
			if n == 16 || br.ReadBool(p[0]) == 0 {
				return n
			}
		}
		if n == 16 {
			return 16
		}
	}
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
