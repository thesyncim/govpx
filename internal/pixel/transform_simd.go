//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// Load4 reads exactly four int16 samples, including minimally sized rows.
func Load4(s []int16) archsimd.Int32x4 {
	_ = s[3]
	return archsimd.Int32x4{}.SetElem(0, int32(s[0])).SetElem(1, int32(s[1])).SetElem(2, int32(s[2])).SetElem(3, int32(s[3]))
}

// Store4 preserves Go's wrapping int16 conversion, without saturating it.
// Transpose4 follows goav1's in-register 4x4 int32 transpose.
func Transpose4(a, b, c, d archsimd.Int32x4) (archsimd.Int32x4, archsimd.Int32x4, archsimd.Int32x4, archsimd.Int32x4) {
	ab0 := a.InterleaveLo(b).ToBits().ReshapeToUint64s()
	ab1 := a.InterleaveHi(b).ToBits().ReshapeToUint64s()
	cd0 := c.InterleaveLo(d).ToBits().ReshapeToUint64s()
	cd1 := c.InterleaveHi(d).ToBits().ReshapeToUint64s()
	return ab0.InterleaveLo(cd0).ReshapeToUint32s().BitsToInt32(), ab0.InterleaveHi(cd0).ReshapeToUint32s().BitsToInt32(), ab1.InterleaveLo(cd1).ReshapeToUint32s().BitsToInt32(), ab1.InterleaveHi(cd1).ReshapeToUint32s().BitsToInt32()
}
func LoadPixels4(s []byte) archsimd.Int32x4 {
	return archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(s)).ReshapeToUint8s().ExtendLo8ToUint16().BitsToInt16().ExtendLo4ToInt32()
}

func loadU16(s []uint16) archsimd.Int32x4 {
	_ = s[3]
	return archsimd.Int32x4{}.SetElem(0, int32(s[0])).SetElem(1, int32(s[1])).SetElem(2, int32(s[2])).SetElem(3, int32(s[3]))
}
func storeU16(s []uint16, v archsimd.Int32x4) {
	_ = s[3]
	s[0] = uint16(v.GetElem(0))
	s[1] = uint16(v.GetElem(1))
	s[2] = uint16(v.GetElem(2))
	s[3] = uint16(v.GetElem(3))
}
