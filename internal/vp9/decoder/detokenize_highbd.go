package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// High-bit-depth (10-bit / 12-bit) VP9 coefficient detokenizer.
// Ported verbatim from libvpx v1.16.0 vp9/decoder/vp9_detokenize.c
// (decode_coefs with CONFIG_VP9_HIGHBITDEPTH).

// vp9Cat6ProbHigh12 mirrors libvpx's vp9_cat6_prob_high12[18].
// 10-bit uses [2:] (16 entries); 12-bit uses [0:] (18 entries).
var vp9Cat6ProbHigh12 = [18]uint8{
	255, 255, 255, 255, 254, 254,
	254, 252, 249, 243, 230, 196,
	177, 153, 140, 133, 130, 129,
}

func cat6ProbsForBitDepth(bd BitDepth) []uint8 {
	switch bd {
	case BitDepth12:
		return vp9Cat6ProbHigh12[:]
	case BitDepth10:
		return vp9Cat6ProbHigh12[2:]
	default:
		return tables.Cat6Prob[:]
	}
}

// DecodeCoefsHighBD decodes one transform block of high-bit-depth (10/12-bit)
// coefficients into int32 dqcoeff, returning EOB.
func DecodeCoefsHighBD(r *bitstream.Reader, probs *FrameCoefProbs,
	txSize common.TxSize, planeType, isInter int,
	scan, neighbors []int16, dequant [2]int16,
	dqShift uint, initCtx int, bd BitDepth,
	dqcoeff []int32, tokenCache *[1024]uint8,
	counts *CoefCounts,
) int {
	maxEob := maxEobForTxSize(txSize)
	bandTrans := bandTranslatePadded(txSize)
	coefProbs := &probs[txSize][planeType][isInter]
	cat6Probs := cat6ProbsForBitDepth(bd)
	cat6Bits := len(cat6Probs)

	var coefCounts *[CoefBands][CoefContexts][UnconstrainedNodes + 1]uint32
	var eobCounts *[CoefBands][CoefContexts]uint32
	if counts != nil {
		coefCounts = &counts.Coef[txSize][planeType][isInter]
		eobCounts = &counts.EobBranch[txSize][planeType][isInter]
	}

	c := 0
	ctx := initCtx
	band := int(bandTrans[0])
	prob := &coefProbs[band][ctx]

	for c < maxEob {
		if eobCounts != nil {
			eobCounts[band][ctx]++
		}
		if r.Read(uint32(prob[eobContextNode])) == 0 {
			if coefCounts != nil {
				coefCounts[band][ctx][eobModelToken]++
			}
			break
		}

		// Zero run loop.
		for r.Read(uint32(prob[zeroContextNode])) == 0 {
			if coefCounts != nil {
				coefCounts[band][ctx][zeroToken]++
			}
			tokenCache[scan[c]] = 0
			c++
			if c >= maxEob {
				return c
			}
			band = int(bandTrans[c])
			ctx = getCoefContext(neighbors, tokenCache[:], c)
			prob = &coefProbs[band][ctx]
		}

		// Non-zero coefficient.
		var val int32
		if r.Read(uint32(prob[oneContextNode])) == 0 {
			if coefCounts != nil {
				coefCounts[band][ctx][oneToken]++
			}
			val = 1
			tokenCache[scan[c]] = 1
		} else {
			if coefCounts != nil {
				coefCounts[band][ctx][twoToken]++
			}
			pareto := &tables.Pareto8Full[prob[pivotNode]-1]
			if r.Read(uint32(pareto[0])) == 0 {
				if r.Read(uint32(pareto[1])) == 0 {
					val = 2
					tokenCache[scan[c]] = 2
				} else {
					if r.Read(uint32(pareto[2])) == 0 {
						val = 3
					} else {
						val = 4
					}
					tokenCache[scan[c]] = 3
				}
			} else {
				if r.Read(uint32(pareto[3])) == 0 {
					tokenCache[scan[c]] = 4
					if r.Read(uint32(pareto[4])) == 0 {
						val = int32(cat1MinVal + int(r.Read(uint32(tables.Cat1Prob[0]))))
					} else {
						v := int(r.Read(uint32(tables.Cat2Prob[0])))<<1 | int(r.Read(uint32(tables.Cat2Prob[1])))
						val = int32(cat2MinVal + v)
					}
				} else {
					tokenCache[scan[c]] = 5
					if r.Read(uint32(pareto[5])) == 0 {
						if r.Read(uint32(pareto[6])) == 0 {
							v := int(r.Read(uint32(tables.Cat3Prob[0])))<<2 |
								int(r.Read(uint32(tables.Cat3Prob[1])))<<1 |
								int(r.Read(uint32(tables.Cat3Prob[2])))
							val = int32(cat3MinVal + v)
						} else {
							v := int(r.Read(uint32(tables.Cat4Prob[0])))<<3 |
								int(r.Read(uint32(tables.Cat4Prob[1])))<<2 |
								int(r.Read(uint32(tables.Cat4Prob[2])))<<1 |
								int(r.Read(uint32(tables.Cat4Prob[3])))
							val = int32(cat4MinVal + v)
						}
					} else {
						if r.Read(uint32(pareto[7])) == 0 {
							v := int(r.Read(uint32(tables.Cat5Prob[0])))<<4 |
								int(r.Read(uint32(tables.Cat5Prob[1])))<<3 |
								int(r.Read(uint32(tables.Cat5Prob[2])))<<2 |
								int(r.Read(uint32(tables.Cat5Prob[3])))<<1 |
								int(r.Read(uint32(tables.Cat5Prob[4])))
							val = int32(cat5MinVal + v)
						} else {
							v := 0
							for i := range cat6Bits {
								v = (v << 1) | int(r.Read(uint32(cat6Probs[i])))
							}
							val = int32(cat6MinVal + v)
						}
					}
				}
			}
		}

		dq := int32(dequant[1])
		if c == 0 {
			dq = int32(dequant[0])
		}
		v := (val * dq) >> dqShift
		if r.ReadBit() != 0 {
			v = -v
		}
		dqcoeff[scan[c]] = v
		c++
		if c >= maxEob {
			break
		}
		band = int(bandTrans[c])
		ctx = getCoefContext(neighbors, tokenCache[:], c)
		prob = &coefProbs[band][ctx]
	}
	return c
}
