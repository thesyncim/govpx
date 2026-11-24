//go:build goexperiment.simd && amd64 && !purego

package encoder

import "simd/archsimd"

func blockYrdMulHigh16(a, b archsimd.Int16x8) archsimd.Int16x8 { return a.MulHigh(b) }

func blockYrdSumPairs16(v archsimd.Int16x8) archsimd.Uint32x4 {
	return v.DotProductPairs(archsimd.BroadcastInt16x8(1)).ToBits()
}

func blockYrdSquarePairs16(v archsimd.Int16x8) archsimd.Uint32x4 {
	return v.DotProductPairs(v).ToBits()
}

func blockYrdSum32(v archsimd.Uint32x4) uint32 {
	v = v.Add(v.PermuteScalars(2, 3, 0, 1))
	return v.Add(v.PermuteScalars(1, 0, 3, 2)).GetElem(0)
}

func blockYrdSumUnsigned16(v archsimd.Uint16x8) uint32 {
	hi := v.ReshapeToUint32s().PermuteScalars(2, 3, 0, 1).ReshapeToUint16s()
	return blockYrdSum32(v.ExtendLo4ToUint32().Add(hi.ExtendLo4ToUint32()))
}
