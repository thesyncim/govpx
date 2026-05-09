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

func parseCoefficientProbabilityHeader(br *boolcoder.Decoder) CoefficientProbabilityHeader {
	return parseCoefficientProbabilityHeaderInto(br, nil)
}

func parseCoefficientProbabilityHeaderInto(br *boolcoder.Decoder, probs *tables.CoefficientProbs) CoefficientProbabilityHeader {
	h := CoefficientProbabilityHeader{IndependentPartitions: true}
	for block := range tables.BlockTypes {
		for band := range tables.CoefBands {
			for ctx := range tables.PrevCoefContexts {
				for node := range tables.EntropyNodes {
					if br.ReadBool(tables.CoefUpdateProbs[block][band][ctx][node]) != 0 {
						value := uint8(br.ReadLiteral(8))
						if probs != nil {
							(*probs)[block][band][ctx][node] = value
						}
						h.UpdateCount++
						if ctx > 0 {
							h.IndependentPartitions = false
						}
					}
				}
			}
		}
	}
	return h
}
