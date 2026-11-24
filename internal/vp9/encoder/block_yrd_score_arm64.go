//go:build goexperiment.simd && arm64 && !purego

package encoder

import "simd/archsimd"

func blockYrdMulHigh16(a, b archsimd.Int16x8) archsimd.Int16x8 {
	lo := a.MulWidenLo(b).ToBits().ReshapeToUint16s()
	hi := a.HiToLo().MulWidenLo(b.HiToLo()).ToBits().ReshapeToUint16s()
	return lo.ConcatOdd(hi).BitsToInt16()
}

func blockYrdSumPairs16(v archsimd.Int16x8) archsimd.Uint32x4 {
	return v.ExtendLo4ToInt32().Add(v.HiToLo().ExtendLo4ToInt32()).ToBits()
}

func blockYrdSquarePairs16(v archsimd.Int16x8) archsimd.Uint32x4 {
	hi := v.HiToLo()
	return v.MulWidenLo(v).Add(hi.MulWidenLo(hi)).ToBits()
}

func blockYrdSum32(v archsimd.Uint32x4) uint32 { return v.ReduceSum() }

func blockYrdSumUnsigned16(v archsimd.Uint16x8) uint32 {
	return v.ExtendLo4ToUint32().Add(v.HiToLo().ExtendLo4ToUint32()).ReduceSum()
}
