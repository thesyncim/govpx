package decoder

import (
	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0:
// - vp8/decoder/decodeframe.c
// - vp8/common/coefupdateprobs.h

type CoefficientProbabilityHeader struct {
	UpdateCount           int
	IndependentPartitions bool
}

const coefProbsFlatLen = tables.BlockTypes * tables.CoefBands * tables.PrevCoefContexts * tables.EntropyNodes

// coefUpdateProbsFlat is a flat copy of tables.CoefUpdateProbs used by the
// hot coefficient probability update header reader.
var coefUpdateProbsFlat = func() [coefProbsFlatLen]uint8 {
	var out [coefProbsFlatLen]uint8
	idx := 0
	for t := range tables.BlockTypes {
		for b := range tables.CoefBands {
			for c := range tables.PrevCoefContexts {
				for n := range tables.EntropyNodes {
					out[idx] = tables.CoefUpdateProbs[t][b][c][n]
					idx++
				}
			}
		}
	}
	return out
}()

func parseCoefficientProbabilityHeaderInto(br *boolcoder.Decoder, probs *tables.CoefficientProbs) CoefficientProbabilityHeader {
	var dstBuf [coefProbsFlatLen]uint8
	var dst []uint8
	if probs != nil {
		idx := 0
		for t := range tables.BlockTypes {
			for b := range tables.CoefBands {
				for c := range tables.PrevCoefContexts {
					copy(dstBuf[idx:idx+tables.EntropyNodes], probs[t][b][c][:])
					idx += tables.EntropyNodes
				}
			}
		}
		dst = dstBuf[:]
	}
	updateCount, nonDefault := br.ReadCoefUpdateProbsInto(
		coefUpdateProbsFlat[:],
		dst,
		tables.EntropyNodes,
		tables.PrevCoefContexts,
	)
	if probs != nil && updateCount > 0 {
		idx := 0
		for t := range tables.BlockTypes {
			for b := range tables.CoefBands {
				for c := range tables.PrevCoefContexts {
					copy(probs[t][b][c][:], dstBuf[idx:idx+tables.EntropyNodes])
					idx += tables.EntropyNodes
				}
			}
		}
	}
	return CoefficientProbabilityHeader{
		UpdateCount:           updateCount,
		IndependentPartitions: !nonDefault,
	}
}
