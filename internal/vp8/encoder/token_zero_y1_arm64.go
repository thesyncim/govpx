//go:build goexperiment.simd && arm64 && !purego && !govpx_oracle_trace

// Empty Y1 token semantics follow libvpx v1.16.0 vp8/encoder/tokenize.c.
package encoder

import (
	"encoding/binary"
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"simd/archsimd"
)

// zeroY1TokenRecords handles a complete empty luma walk with zero contexts.
// The macroblock caller has already checked pointers and consumed Y2.
func zeroY1TokenRecords(above, left *TokenContextPlanes, coeffs *MacroblockCoefficients, counts *coefficientTokenCounts, records *InterCoefficientTokenRecords, blockType, skipDC int) bool {
	if !pixel.Enabled || binary.LittleEndian.Uint32(above.Y1[:])|binary.LittleEndian.Uint32(left.Y1[:]) != 0 {
		return false
	}
	if cap(records.Records)-len(records.Records) < 16 {
		return false
	}
	eobs := archsimd.LoadUint8x16Array((*[16]byte)(coeffs.EOB[:16]))
	if eobs.ReduceMax() > byte(skipDC) {
		return false
	}
	index := len(records.Records)
	rec := CoefficientTokenRecord(tables.DCTEOBToken | (((blockType*tables.CoefBands+skipDC)*tables.PrevCoefContexts)*coefficientProbRowSize)<<coefficientTokenRecordProbOffsetShift)
	records.Records = records.Records[:index+16]
	dst := records.Records[index : index+16]
	for j := range dst {
		dst[j] = rec
	}
	(*counts)[blockType][skipDC][0][tables.DCTEOBToken] += 16
	return true
}
