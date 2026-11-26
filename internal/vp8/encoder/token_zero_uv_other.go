//go:build (!goexperiment.simd || !arm64 || purego) && !govpx_oracle_trace

// Empty UV/Y1 token semantics follow libvpx v1.16.0 vp8/encoder/tokenize.c.
package encoder

import (
	"encoding/binary"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// zeroUVTokenRecords handles a complete empty chroma walk with zero contexts.
// The macroblock caller has already checked pointers and consumed Y2 and Y1.
func zeroUVTokenRecords(above, left *TokenContextPlanes, coeffs *MacroblockCoefficients, counts *coefficientTokenCounts, records *InterCoefficientTokenRecords) bool {
	if above.U != [2]byte{} || above.V != [2]byte{} || left.U != [2]byte{} || left.V != [2]byte{} {
		return false
	}
	if cap(records.Records)-len(records.Records) < 8 || binary.LittleEndian.Uint64(coeffs.EOB[16:24]) != 0 {
		return false
	}
	const blockType, skipDC = 2, 0
	index := len(records.Records)
	rec := CoefficientTokenRecord(tables.DCTEOBToken | (((blockType*tables.CoefBands+skipDC)*tables.PrevCoefContexts)*coefficientProbRowSize)<<coefficientTokenRecordProbOffsetShift)
	records.Records = records.Records[:index+8]
	dst := (*[8]CoefficientTokenRecord)(records.Records[index : index+8])
	dst[0], dst[1], dst[2], dst[3] = rec, rec, rec, rec
	dst[4], dst[5], dst[6], dst[7] = rec, rec, rec, rec
	(*counts)[blockType][skipDC][0][tables.DCTEOBToken] += 8
	return true
}

// zeroY1TokenRecords handles a complete empty luma walk with zero contexts.
// The macroblock caller has already checked pointers and consumed Y2.
func zeroY1TokenRecords(above, left *TokenContextPlanes, coeffs *MacroblockCoefficients, counts *coefficientTokenCounts, records *InterCoefficientTokenRecords, blockType, skipDC int) bool {
	if binary.LittleEndian.Uint32(above.Y1[:])|binary.LittleEndian.Uint32(left.Y1[:]) != 0 {
		return false
	}
	if cap(records.Records)-len(records.Records) < 16 {
		return false
	}
	u0 := binary.LittleEndian.Uint64(coeffs.EOB[:8])
	u1 := binary.LittleEndian.Uint64(coeffs.EOB[8:16])
	switch skipDC {
	case 0:
		if u0|u1 != 0 {
			return false
		}
	case 1:
		if (u0|u1)&0xfefefefefefefefe != 0 {
			return false
		}
	default:
		for _, b := range coeffs.EOB[:16] {
			if b > byte(skipDC) {
				return false
			}
		}
	}
	index := len(records.Records)
	rec := CoefficientTokenRecord(tables.DCTEOBToken | (((blockType*tables.CoefBands+skipDC)*tables.PrevCoefContexts)*coefficientProbRowSize)<<coefficientTokenRecordProbOffsetShift)
	records.Records = records.Records[:index+16]
	dst := (*[16]CoefficientTokenRecord)(records.Records[index : index+16])
	dst[0], dst[1], dst[2], dst[3] = rec, rec, rec, rec
	dst[4], dst[5], dst[6], dst[7] = rec, rec, rec, rec
	dst[8], dst[9], dst[10], dst[11] = rec, rec, rec, rec
	dst[12], dst[13], dst[14], dst[15] = rec, rec, rec, rec
	(*counts)[blockType][skipDC][0][tables.DCTEOBToken] += 16
	return true
}
