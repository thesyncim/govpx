//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

// Fixed row groups remove loop address setup while preserving packet order.
// Read-only rows may repeat or overlap. Output must not overlap either input.
// Limit stride arithmetic before forming exact row windows. Invalid pointer
// arguments decline so the original public path retains its panic behavior.
func Gather16PtrFast(src *byte, ss int, pred *byte, ps int, out *int16) bool {
	if !Enabled || src == nil || pred == nil || out == nil || (uint(ss)|uint(ps))>>48 != 0 {
		return false
	}
	sp, pp, op := unsafe.Pointer(src), unsafe.Pointer(pred), unsafe.Pointer(out)
	{
		const y = 0
		{
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+0)*ss)))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+0)*ps)))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+1)*ss)))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+1)*ps)))
			lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
			lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
			hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
			hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
			lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*64+0)*2)))
			lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*64+16)*2)))
			hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*64+32)*2)))
			hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*64+48)*2)))
		}
		{
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+2)*ss)))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+2)*ps)))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+3)*ss)))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+3)*ps)))
			lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
			lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
			hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
			hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
			lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*64+8)*2)))
			lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*64+24)*2)))
			hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*64+40)*2)))
			hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*64+56)*2)))
		}

	}
	{
		const y = 4
		{
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+0)*ss)))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+0)*ps)))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+1)*ss)))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+1)*ps)))
			lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
			lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
			hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
			hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
			lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*64+0)*2)))
			lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*64+16)*2)))
			hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*64+32)*2)))
			hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*64+48)*2)))
		}
		{
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+2)*ss)))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+2)*ps)))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+3)*ss)))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+3)*ps)))
			lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
			lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
			hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
			hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
			lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*64+8)*2)))
			lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*64+24)*2)))
			hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*64+40)*2)))
			hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*64+56)*2)))
		}

	}
	{
		const y = 8
		{
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+0)*ss)))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+0)*ps)))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+1)*ss)))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+1)*ps)))
			lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
			lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
			hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
			hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
			lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (2*64+0)*2)))
			lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (2*64+16)*2)))
			hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (2*64+32)*2)))
			hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (2*64+48)*2)))
		}
		{
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+2)*ss)))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+2)*ps)))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+3)*ss)))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+3)*ps)))
			lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
			lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
			hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
			hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
			lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (2*64+8)*2)))
			lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (2*64+24)*2)))
			hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (2*64+40)*2)))
			hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (2*64+56)*2)))
		}

	}
	{
		const y = 12
		{
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+0)*ss)))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+0)*ps)))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+1)*ss)))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+1)*ps)))
			lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
			lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
			hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
			hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
			lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (3*64+0)*2)))
			lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (3*64+16)*2)))
			hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (3*64+32)*2)))
			hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (3*64+48)*2)))
		}
		{
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+2)*ss)))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+2)*ps)))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, (y+3)*ss)))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, (y+3)*ps)))
			lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
			lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
			hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
			hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
			lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (3*64+8)*2)))
			lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (3*64+24)*2)))
			hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (3*64+40)*2)))
			hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (3*64+56)*2)))
		}

	}
	return true
}
func Gather8PtrFast(src *byte, ss int, pred *byte, ps int, out *int16) bool {
	if !Enabled || src == nil || pred == nil || out == nil || (uint(ss)|uint(ps))>>48 != 0 {
		return false
	}
	sp, pp, op := unsafe.Pointer(src), unsafe.Pointer(pred), unsafe.Pointer(out)
	{
		const y = 0
		{
			a := archsimd.Uint64x2{}.SetElem(0, *(*uint64)(unsafe.Add(sp, (y+0)*ss))).SetElem(1, *(*uint64)(unsafe.Add(sp, (y+1)*ss))).ReshapeToUint8s()
			b := archsimd.Uint64x2{}.SetElem(0, *(*uint64)(unsafe.Add(pp, (y+0)*ps))).SetElem(1, *(*uint64)(unsafe.Add(pp, (y+1)*ps))).ReshapeToUint8s()
			lo := widen8(a).Sub(widen8(b)).ToBits().ReshapeToUint64s()
			hi := widenHi8(a).Sub(widenHi8(b)).ReshapeToUint64s()
			lo.InterleaveLo(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*32+0)*2)))
			lo.InterleaveHi(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*32+16)*2)))
		}
		{
			a := archsimd.Uint64x2{}.SetElem(0, *(*uint64)(unsafe.Add(sp, (y+2)*ss))).SetElem(1, *(*uint64)(unsafe.Add(sp, (y+3)*ss))).ReshapeToUint8s()
			b := archsimd.Uint64x2{}.SetElem(0, *(*uint64)(unsafe.Add(pp, (y+2)*ps))).SetElem(1, *(*uint64)(unsafe.Add(pp, (y+3)*ps))).ReshapeToUint8s()
			lo := widen8(a).Sub(widen8(b)).ToBits().ReshapeToUint64s()
			hi := widenHi8(a).Sub(widenHi8(b)).ReshapeToUint64s()
			lo.InterleaveLo(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*32+8)*2)))
			lo.InterleaveHi(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (0*32+24)*2)))
		}

	}
	{
		const y = 4
		{
			a := archsimd.Uint64x2{}.SetElem(0, *(*uint64)(unsafe.Add(sp, (y+0)*ss))).SetElem(1, *(*uint64)(unsafe.Add(sp, (y+1)*ss))).ReshapeToUint8s()
			b := archsimd.Uint64x2{}.SetElem(0, *(*uint64)(unsafe.Add(pp, (y+0)*ps))).SetElem(1, *(*uint64)(unsafe.Add(pp, (y+1)*ps))).ReshapeToUint8s()
			lo := widen8(a).Sub(widen8(b)).ToBits().ReshapeToUint64s()
			hi := widenHi8(a).Sub(widenHi8(b)).ReshapeToUint64s()
			lo.InterleaveLo(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*32+0)*2)))
			lo.InterleaveHi(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*32+16)*2)))
		}
		{
			a := archsimd.Uint64x2{}.SetElem(0, *(*uint64)(unsafe.Add(sp, (y+2)*ss))).SetElem(1, *(*uint64)(unsafe.Add(sp, (y+3)*ss))).ReshapeToUint8s()
			b := archsimd.Uint64x2{}.SetElem(0, *(*uint64)(unsafe.Add(pp, (y+2)*ps))).SetElem(1, *(*uint64)(unsafe.Add(pp, (y+3)*ps))).ReshapeToUint8s()
			lo := widen8(a).Sub(widen8(b)).ToBits().ReshapeToUint64s()
			hi := widenHi8(a).Sub(widenHi8(b)).ReshapeToUint64s()
			lo.InterleaveLo(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*32+8)*2)))
			lo.InterleaveHi(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(unsafe.Add(op, (1*32+24)*2)))
		}

	}
	return true
}
