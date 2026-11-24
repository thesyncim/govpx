//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// Gather16Fast reads a 16x16 block from src and pred and writes 256 int16
// residuals in 4x4 block-scan order to out.
func Gather16Fast(src []byte, ss int, pred []byte, ps int, out []int16) bool {
	if !Enabled || (uint(ss)|uint(ps))>>30 != 0 || 15*ss+16 > len(src) || 15*ps+16 > len(pred) || len(out) < 256 {
		return false
	}
	_ = src[15*ss+15]
	_ = pred[15*ps+15]
	_ = out[255]
	for g := range 4 {
		y := g * 4
		o := g * 64
		{
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(src[(y+0)*ss : (y+0)*ss+16]))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(pred[(y+0)*ps : (y+0)*ps+16]))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(src[(y+1)*ss : (y+1)*ss+16]))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(pred[(y+1)*ps : (y+1)*ps+16]))
			lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
			lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
			hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
			hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
			lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+0 : o+8]))
			lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+16 : o+24]))
			hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+32 : o+40]))
			hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+48 : o+56]))
		}
		{
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(src[(y+2)*ss : (y+2)*ss+16]))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(pred[(y+2)*ps : (y+2)*ps+16]))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(src[(y+3)*ss : (y+3)*ss+16]))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(pred[(y+3)*ps : (y+3)*ps+16]))
			lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
			lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
			hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
			hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
			lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+8 : o+16]))
			lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+24 : o+32]))
			hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+40 : o+48]))
			hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+56 : o+64]))
		}
	}
	return true
}

// Gather8Fast reads an 8x8 block from src and pred and writes 64 int16
// residuals in 4x4 block-scan order to out.
func Gather8Fast(src []byte, ss int, pred []byte, ps int, out []int16) bool {
	if !Enabled || (uint(ss)|uint(ps))>>30 != 0 || 7*ss+8 > len(src) || 7*ps+8 > len(pred) || len(out) < 64 {
		return false
	}
	_ = src[7*ss+7]
	_ = pred[7*ps+7]
	_ = out[63]
	for g := range 2 {
		y := g * 4
		o := g * 32
		{
			a := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(src[(y+0)*ss:])).SetElem(1, binary.LittleEndian.Uint64(src[(y+1)*ss:])).ReshapeToUint8s()
			b := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(pred[(y+0)*ps:])).SetElem(1, binary.LittleEndian.Uint64(pred[(y+1)*ps:])).ReshapeToUint8s()
			lo := widen8(a).Sub(widen8(b)).ToBits().ReshapeToUint64s()
			hi := widenHi8(a).Sub(widenHi8(b)).ReshapeToUint64s()
			lo.InterleaveLo(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+0 : o+8]))
			lo.InterleaveHi(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+16 : o+24]))
		}
		{
			a := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(src[(y+2)*ss:])).SetElem(1, binary.LittleEndian.Uint64(src[(y+3)*ss:])).ReshapeToUint8s()
			b := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(pred[(y+2)*ps:])).SetElem(1, binary.LittleEndian.Uint64(pred[(y+3)*ps:])).ReshapeToUint8s()
			lo := widen8(a).Sub(widen8(b)).ToBits().ReshapeToUint64s()
			hi := widenHi8(a).Sub(widenHi8(b)).ReshapeToUint64s()
			lo.InterleaveLo(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+8 : o+16]))
			lo.InterleaveHi(hi).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[o+24 : o+32]))
		}
	}
	return true
}
