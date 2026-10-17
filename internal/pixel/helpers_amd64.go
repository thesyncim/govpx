//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
	"unsafe"
)

func load8(b []byte) archsimd.Uint8x16 {
	return archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(b)).ReshapeToUint8s()
}
func storeBytes8(b []byte, v archsimd.Uint8x16) {
	binary.LittleEndian.PutUint64(b, v.ReshapeToUint64s().GetElem(0))
}

func widen8(v archsimd.Uint8x16) archsimd.Int16x8 { return v.ExtendLo8ToUint16().BitsToInt16() }
func high4(v archsimd.Int16x8) archsimd.Int32x4 {
	return v.ToBits().ReshapeToUint32s().BitsToInt32().PermuteScalars(2, 3, 0, 1).ToBits().ReshapeToUint16s().BitsToInt16().ExtendLo4ToInt32()
}
func mac(lo, hi archsimd.Int32x4, v, c archsimd.Int16x8) (archsimd.Int32x4, archsimd.Int32x4) {
	c32 := c.ExtendLo4ToInt32()
	return lo.Add(v.ExtendLo4ToInt32().Mul(c32)), hi.Add(high4(v).Mul(c32))
}
func packBytes(v archsimd.Int16x8) archsimd.Uint8x16 {
	return v.Max(archsimd.LoadInt16x8Array(&packZero16)).Min(archsimd.BroadcastInt16x8(255)).ToBits().ReshapeToUint8s().PermuteOrZero(archsimd.LoadInt8x16Array(&packByteIndices))
}
func store8(b []byte, lo, hi archsimd.Int32x4) {
	storeBytes8(b, packBytes(lo.SaturateToInt16Concat(hi)))
}
func sadAdd(acc archsimd.Uint32x4, a, b archsimd.Uint8x16) archsimd.Uint32x4 {
	return acc.Add(a.SumOf8AbsDiff(b).ReshapeToUint32s())
}
func sumSquares(v archsimd.Int32x4) uint64 {
	s := sqSumInt32(archsimd.Int64x2{}, v)
	return uint64(s.GetElem(0)) + uint64(s.GetElem(1))
}

// A VEX load avoids Go 1.27's legacy MOVUPS from its zero XMM register
// while a caller still has live AVX2 vectors.
var packZero16 [8]int16

var packByteIndices = [16]int8{0, 2, 4, 6, 8, 10, 12, 14, -128, -128, -128, -128, -128, -128, -128, -128}
var packInt16Indices = [16]int8{0, 1, 4, 5, 8, 9, 12, 13, -128, -128, -128, -128, -128, -128, -128, -128}

func Store4(s []int16, v archsimd.Int32x4) {
	put4(s, v.ToBits().ReshapeToUint8s().PermuteOrZero(archsimd.LoadInt8x16Array(&packInt16Indices)).ReshapeToUint64s().GetElem(0))
}
func StorePixels4(s []byte, v archsimd.Int32x4) {
	binary.LittleEndian.PutUint32(s, packBytes(v.SaturateToInt16Concat(v)).ReshapeToUint32s().GetElem(0))
}
func sum32(v archsimd.Int32x4) int32 {
	v = v.Add(v.PermuteScalars(2, 3, 0, 1))
	return v.Add(v.PermuteScalars(1, 0, 3, 2)).GetElem(0)
}
func sumU32(v archsimd.Uint32x4) uint32 { return uint32(sum32(v.BitsToInt32())) }
func Max4(v archsimd.Int32x4) int32 {
	v = v.Max(v.PermuteScalars(2, 3, 0, 1))
	return v.Max(v.PermuteScalars(1, 0, 3, 2)).GetElem(0)
}

// Four int16 outputs occupy precisely the eight bytes written here. This
// store is unaligned-safe on amd64 and arm64 and stays within the Go slice.
func put4(out []int16, v uint64) { _ = out[3]; *(*uint64)(unsafe.Pointer(&out[0])) = v }

func highBytes(v archsimd.Uint8x16) archsimd.Uint8x16 {
	return v.ReshapeToUint32s().PermuteScalars(2, 3, 0, 1).ReshapeToUint8s()
}

func permute16(v, i archsimd.Uint8x16) archsimd.Uint8x16 { return v.PermuteOrZero(i.BitsToInt8()) }

func highU16(v archsimd.Uint16x8) archsimd.Uint16x8 {
	return v.ReshapeToUint32s().PermuteScalars(2, 3, 0, 1).ReshapeToUint16s()
}

func filterVertical8(src []byte, ss int, dst []byte, ds, w, h int, taps []int16) bool { return false }

func RoundTransform4(v archsimd.Int32x4) archsimd.Int32x4 {
	return v.Add(archsimd.BroadcastInt32x4(8192)).ShiftAllRight(14)
}
func WrapTransform4(v archsimd.Int32x4) archsimd.Int32x4 { return v.ShiftAllLeft(16).ShiftAllRight(16) }

func mulAdd16(sum, a, b archsimd.Int16x8) archsimd.Int16x8 { return a.Mul(b).Add(sum) }
func filterShift7(v archsimd.Int16x8) archsimd.Int16x8     { return v.ShiftAllRight(7) }

func filterVerticalNarrow8(src []byte, ss int, dst []byte, ds, w, h int, taps []int16, mode int) bool {
	return false
}

func MulAdd4(a, b, c archsimd.Int32x4) archsimd.Int32x4 { return a.Mul(b).Add(c) }
func MulAdd8(a, b, c archsimd.Int32x8) archsimd.Int32x8 { return a.Mul(b).Add(c) }

func fpMulHigh16(a, b archsimd.Int16x8) archsimd.Int16x8 { return a.MulHigh(b) }
func fpReduceMax16(v archsimd.Int16x8) int               { return int(Max4(v.ExtendLo4ToInt32().Max(high4(v)))) }

func zipBytesLo(a, b archsimd.Uint8x16) archsimd.Uint16x8 {
	return a.ExtendLo8ToUint16().Or(b.ExtendLo8ToUint16().ShiftAllLeft(8))
}

func square8(v archsimd.Int16x8) archsimd.Int32x4 { return v.DotProductPairs(v) }
func sumDiff16(v archsimd.Int16x8) archsimd.Int32x4 {
	return v.DotProductPairs(archsimd.BroadcastInt16x8(1))
}

func lfShift3(v archsimd.Int16x8) archsimd.Int16x8 { return v.ShiftAllRight(3) }
func lfShift1(v archsimd.Int16x8) archsimd.Int16x8 { return v.ShiftAllRight(1) }

func vp8MulHighU16(a, b archsimd.Uint16x8) archsimd.Uint16x8 { return a.MulHigh(b) }

// fpClassIndices receives magnitudes already clamped to [0,11]. Their low
// bytes are exact LUT indices; the general pixel clamp is unnecessary here.
func fpClassIndices(v archsimd.Int16x8) archsimd.Uint8x16 {
	return v.ToBits().ReshapeToUint8s().PermuteOrZero(archsimd.LoadInt8x16Array(&packByteIndices))
}

// These compile-time-disabled ARM specializations retain the ordinary
// implementation here, without changing amd64's selected instruction bodies.
func loadLFColumns16(p []byte, off, pitch int) (archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16) {
	return loadLFColumns[[8]byte](p, off, pitch, 16)
}
