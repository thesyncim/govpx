//go:build goexperiment.simd && arm64 && !purego && !govpx_oracle_trace

// Empty UV token semantics follow libvpx v1.16.0 vp8/encoder/tokenize.c.
package encoder

import (
	"encoding/binary"
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// zeroUVTokenRecords handles a complete empty chroma walk with zero contexts.
// The macroblock caller has already checked pointers and consumed Y2 and Y1.
func zeroUVTokenRecords(above, left *TokenContextPlanes, coeffs *MacroblockCoefficients, counts *coefficientTokenCounts, records *InterCoefficientTokenRecords) bool {
	if !pixel.Enabled || above.U != [2]byte{} || above.V != [2]byte{} || left.U != [2]byte{} || left.V != [2]byte{} {
		return false
	}
	if cap(records.Records)-len(records.Records) < 8 || binary.LittleEndian.Uint64(coeffs.EOB[16:24]) != 0 {
		return false
	}
	const blockType, skipDC = 2, 0
	index := len(records.Records)
	rec := CoefficientTokenRecord(tables.DCTEOBToken | (((blockType*tables.CoefBands+skipDC)*tables.PrevCoefContexts)*coefficientProbRowSize)<<coefficientTokenRecordProbOffsetShift)
	records.Records = records.Records[:index+8]
	dst := records.Records[index : index+8]
	for j := range dst {
		dst[j] = rec
	}
	(*counts)[blockType][skipDC][0][tables.DCTEOBToken] += 8
	return true
}
