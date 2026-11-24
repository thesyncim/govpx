// SPDX-License-Identifier: BSD-2-Clause
// Extracted from goav1 at ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4.
// See UPSTREAM.md and ../../third_party/goav1/LICENSE.
//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"simd/archsimd"
)

var (
	hi16WordsIdx = [16]int8{8, 9, 10, 11, 12, 13, 14, 15, -1, -1, -1, -1, -1, -1, -1, -1}
	oddDwordsIdx = [16]int8{4, 5, 6, 7, -1, -1, -1, -1, 12, 13, 14, 15, -1, -1, -1, -1}
)

func widenHi8(v archsimd.Uint8x16) archsimd.Uint16x8 {
	return highBytes(v).ExtendLo8ToUint16()
}

func hiInt16(v archsimd.Int16x8) archsimd.Int16x8 {
	return v.ToBits().ReshapeToUint8s().PermuteOrZero(archsimd.LoadInt8x16Array(&hi16WordsIdx)).
		ReshapeToUint16s().BitsToInt16()
}

func sqSumInt32(acc archsimd.Int64x2, v archsimd.Int32x4) archsimd.Int64x2 {
	odd := v.ToBits().ReshapeToUint8s().PermuteOrZero(archsimd.LoadInt8x16Array(&oddDwordsIdx)).
		ReshapeToUint32s().BitsToInt32()
	return acc.Add(v.MulWidenEven(v)).Add(odd.MulWidenEven(odd))
}
