package decoder

import (
	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0:
// - vp8/decoder/decodeframe.c
// - vp8/common/coefupdateprobs.h

type CoefficientProbabilityHeader struct {
	UpdateCount           int
	IndependentPartitions bool
}

func parseCoefficientProbabilityHeader(br *boolcoder.Decoder) CoefficientProbabilityHeader {
	h := CoefficientProbabilityHeader{IndependentPartitions: true}
	for block := 0; block < tables.BlockTypes; block++ {
		for band := 0; band < tables.CoefBands; band++ {
			for ctx := 0; ctx < tables.PrevCoefContexts; ctx++ {
				for node := 0; node < tables.EntropyNodes; node++ {
					if br.ReadBool(tables.CoefUpdateProbs[block][band][ctx][node]) != 0 {
						_ = br.ReadLiteral(8)
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
