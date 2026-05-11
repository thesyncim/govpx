package decoder

import (
	"unsafe"

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

// coefUpdateProbsFlat is a flat view of tables.CoefUpdateProbs used by the
// hot coefficient probability update header reader. The underlying storage
// lives in tables.CoefUpdateProbs; this slice is read-only.
var coefUpdateProbsFlat = unsafe.Slice(
	(*uint8)(unsafe.Pointer(&tables.CoefUpdateProbs[0][0][0][0])),
	coefProbsFlatLen,
)

func parseCoefficientProbabilityHeader(br *boolcoder.Decoder) CoefficientProbabilityHeader {
	return parseCoefficientProbabilityHeaderInto(br, nil)
}

func parseCoefficientProbabilityHeaderInto(br *boolcoder.Decoder, probs *tables.CoefficientProbs) CoefficientProbabilityHeader {
	var dst []uint8
	if probs != nil {
		dst = unsafe.Slice((*uint8)(unsafe.Pointer(&(*probs)[0][0][0][0])), coefProbsFlatLen)
	}
	updateCount, nonDefault := br.ReadCoefUpdateProbsInto(
		coefUpdateProbsFlat,
		dst,
		tables.EntropyNodes,
		tables.PrevCoefContexts,
	)
	return CoefficientProbabilityHeader{
		UpdateCount:           updateCount,
		IndependentPartitions: !nonDefault,
	}
}
