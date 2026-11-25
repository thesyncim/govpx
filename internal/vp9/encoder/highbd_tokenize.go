package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// High-bit-depth (10-bit / 12-bit) VP9 coefficient quantizer and boolean
// range-coder writer. Exact mirror of internal/vp9/decoder/detokenize_highbd.go.

var vp9Cat6ProbHigh12 = [18]uint8{
	255, 255, 255, 255, 254, 254,
	254, 252, 249, 243, 230, 196,
	177, 153, 140, 133, 130, 129,
}

func highbdCat6Probs(bd vp9dec.BitDepth) []uint8 {
	switch bd {
	case vp9dec.BitDepth12:
		return vp9Cat6ProbHigh12[:]
	case vp9dec.BitDepth10:
		return vp9Cat6ProbHigh12[2:]
	default:
		return tables.Cat6Prob[:]
	}
}

// HighbdQuantizeBlock quantizes int32 forward-transform coefficients into
// qcoeff and dequantized dqcoeff, returning EOB (one past the last non-zero
// scan position).
func HighbdQuantizeBlock(coeff, qcoeff, dqcoeff []int32, scan []int16,
	dequant [2]int16, txSize common.TxSize, lossless bool,
) int {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	clear(qcoeff[:maxEob])
	clear(dqcoeff[:maxEob])
	if lossless {
		eob := 0
		for i := range maxEob {
			rc := scan[i]
			c := coeff[rc]
			q := c / 4
			if q != 0 {
				qcoeff[rc] = q
				dqcoeff[rc] = q * 4
				eob = i + 1
			}
		}
		return eob
	}
	dqShift := uint(0)
	if txSize == common.Tx32x32 {
		dqShift = 1
	}
	eob := 0
	for i := range maxEob {
		rc := scan[i]
		c := coeff[rc]
		neg := c < 0
		absC := c
		if neg {
			absC = -absC
		}
		dq := int32(dequant[1])
		if i == 0 {
			dq = int32(dequant[0])
		}
		effDQ := dq >> dqShift
		if effDQ <= 0 {
			effDQ = 1
		}
		// Deadzone rounding: round((absC + effDQ/3) / effDQ)
		q := (absC + effDQ/3) / effDQ
		if q != 0 {
			dqVal := (q * dq) >> dqShift
			if neg {
				q = -q
				dqVal = -dqVal
			}
			qcoeff[rc] = q
			dqcoeff[rc] = dqVal
			eob = i + 1
		}
	}
	return eob
}

func writeProbU8(w *bitstream.Writer, bit int, prob uint8) {
	if w != nil {
		w.Write(uint32(bit), uint32(prob))
	}
}

// WriteCoefsHighBD encodes one transform block of high-bit-depth (10/12-bit)
// quantized coefficients to w (if w != nil) and/or accumulates counts.
func WriteCoefsHighBD(w *bitstream.Writer, probs *vp9dec.FrameCoefProbs,
	txSize common.TxSize, planeType, isInter int,
	scan, neighbors []int16, qcoeff []int32, eob int,
	initCtx int, bd vp9dec.BitDepth,
	tokenCache *[1024]uint8, counts *vp9dec.CoefCounts,
) {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	bandTrans := vp9dec.BandTranslateForTxSize(txSize)
	coefProbs := &probs[txSize][planeType][isInter]
	cat6Probs := highbdCat6Probs(bd)
	cat6Bits := len(cat6Probs)

	var coefCounts *[vp9dec.CoefBands][vp9dec.CoefContexts][vp9dec.UnconstrainedNodes + 1]uint32
	var eobCounts *[vp9dec.CoefBands][vp9dec.CoefContexts]uint32
	if counts != nil {
		coefCounts = &counts.Coef[txSize][planeType][isInter]
		eobCounts = &counts.EobBranch[txSize][planeType][isInter]
	}

	ctx := initCtx
	band := int(bandTrans[0])
	prob := &coefProbs[band][ctx]

	c := 0
	for c < maxEob {
		if eobCounts != nil {
			eobCounts[band][ctx]++
		}
		if c >= eob {
			writeProbU8(w, 0, prob[0])
			if coefCounts != nil {
				coefCounts[band][ctx][3]++
			}
			return
		}
		writeProbU8(w, 1, prob[0])

		// Zero run until next non-zero coefficient.
		for qcoeff[scan[c]] == 0 {
			writeProbU8(w, 0, prob[1])
			if coefCounts != nil {
				coefCounts[band][ctx][0]++
			}
			tokenCache[scan[c]] = 0
			c++
			if c >= maxEob {
				return
			}
			band = int(bandTrans[c])
			ctx = vp9dec.GetCoefContext(neighbors, tokenCache, c)
			prob = &coefProbs[band][ctx]
		}
		// Non-zero.
		writeProbU8(w, 1, prob[1])

		qc := qcoeff[scan[c]]
		sign := 0
		absQ := qc
		if qc < 0 {
			sign = 1
			absQ = -qc
		}
		if absQ == 1 {
			writeProbU8(w, 0, prob[2])
			if coefCounts != nil {
				coefCounts[band][ctx][1]++
			}
			tokenCache[scan[c]] = 1
		} else {
			writeProbU8(w, 1, prob[2])
			if coefCounts != nil {
				coefCounts[band][ctx][2]++
			}
			p := &tables.Pareto8Full[prob[2]-1]
			switch {
			case absQ == 2:
				tokenCache[scan[c]] = 2
				writeProbU8(w, 0, p[0])
				writeProbU8(w, 0, p[1])
			case absQ == 3:
				tokenCache[scan[c]] = 3
				writeProbU8(w, 0, p[0])
				writeProbU8(w, 1, p[1])
				writeProbU8(w, 0, p[2])
			case absQ == 4:
				tokenCache[scan[c]] = 3
				writeProbU8(w, 0, p[0])
				writeProbU8(w, 1, p[1])
				writeProbU8(w, 1, p[2])
			case absQ <= 6: // cat1: 5..6
				tokenCache[scan[c]] = 4
				writeProbU8(w, 1, p[0])
				writeProbU8(w, 0, p[3])
				writeProbU8(w, 0, p[4])
				writeProbU8(w, int(absQ-5)&1, tables.Cat1Prob[0])
			case absQ <= 10: // cat2: 7..10
				tokenCache[scan[c]] = 4
				writeProbU8(w, 1, p[0])
				writeProbU8(w, 0, p[3])
				writeProbU8(w, 1, p[4])
				v := int(absQ - 7)
				writeProbU8(w, (v>>1)&1, tables.Cat2Prob[0])
				writeProbU8(w, v&1, tables.Cat2Prob[1])
			case absQ <= 18: // cat3: 11..18
				tokenCache[scan[c]] = 5
				writeProbU8(w, 1, p[0])
				writeProbU8(w, 1, p[3])
				writeProbU8(w, 0, p[5])
				writeProbU8(w, 0, p[6])
				v := int(absQ - 11)
				writeProbU8(w, (v>>2)&1, tables.Cat3Prob[0])
				writeProbU8(w, (v>>1)&1, tables.Cat3Prob[1])
				writeProbU8(w, v&1, tables.Cat3Prob[2])
			case absQ <= 34: // cat4: 19..34
				tokenCache[scan[c]] = 5
				writeProbU8(w, 1, p[0])
				writeProbU8(w, 1, p[3])
				writeProbU8(w, 0, p[5])
				writeProbU8(w, 1, p[6])
				v := int(absQ - 19)
				writeProbU8(w, (v>>3)&1, tables.Cat4Prob[0])
				writeProbU8(w, (v>>2)&1, tables.Cat4Prob[1])
				writeProbU8(w, (v>>1)&1, tables.Cat4Prob[2])
				writeProbU8(w, v&1, tables.Cat4Prob[3])
			case absQ <= 66: // cat5: 35..66
				tokenCache[scan[c]] = 5
				writeProbU8(w, 1, p[0])
				writeProbU8(w, 1, p[3])
				writeProbU8(w, 1, p[5])
				writeProbU8(w, 0, p[7])
				v := int(absQ - 35)
				for i := range 5 {
					writeProbU8(w, (v>>uint(4-i))&1, tables.Cat5Prob[i])
				}
			default: // cat6: 67..
				tokenCache[scan[c]] = 5
				writeProbU8(w, 1, p[0])
				writeProbU8(w, 1, p[3])
				writeProbU8(w, 1, p[5])
				writeProbU8(w, 1, p[7])
				v := int(absQ - 67)
				for i := range cat6Bits {
					writeProbU8(w, (v>>uint(cat6Bits-1-i))&1, cat6Probs[i])
				}
			}
		}
		if w != nil {
			w.WriteBit(uint32(sign))
		}
		c++
		if c >= maxEob {
			return
		}
		band = int(bandTrans[c])
		ctx = vp9dec.GetCoefContext(neighbors, tokenCache, c)
		prob = &coefProbs[band][ctx]
	}
}
