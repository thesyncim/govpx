// SPDX-License-Identifier: BSD-2-Clause
// Extracted from goav1 at ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4.
// See UPSTREAM.md and ../../third_party/goav1/LICENSE.
//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"simd/archsimd"
)

func widenHi8(v archsimd.Uint8x16) archsimd.Uint16x8 {
	return v.InterleaveHi(archsimd.BroadcastUint8x16(0)).ReshapeToUint16s()
}

func hiInt16(v archsimd.Int16x8) archsimd.Int16x8 {
	return v.HiToLo()
}

func sqSumInt32(acc archsimd.Int64x2, v archsimd.Int32x4) archsimd.Int64x2 {
	hi := v.HiToLo()
	return acc.Add(v.MulWidenLo(v)).Add(hi.MulWidenLo(hi))
}
