//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// DCPairInPlaceInt32Native retains the one-window ABI and exact fallback
// while computing rounded DC coefficients and byte clipping with SIMD.
func DCPairInPlaceInt32Native(c0, c1 int32, b []byte, stride int) {
	if !Enabled {
		dcPairNativeScalar(c0, c1, b, stride, b, stride)
		return
	}
	if stride >= 8 && len(b) >= 8 && stride <= (len(b)-8)/3 {
		plus, minus := dcPairCoeffBytes(c0, c1)
		dcPairByteRows2(b, stride, b, stride, plus, minus)
		b2 := b[2*stride:]
		dcPairByteRows2(b2, stride, b2, stride, plus, minus)
		return
	}
	d0, d1 := dcPairNativeDelta(c0), dcPairNativeDelta(c1)
	// Exact original enabled fallback: complete block0 before slicing block1,
	// and evaluate destination before capturing each four-byte prediction.
	for block := range 2 {
		p, d, delta := b, b, d0
		if block != 0 {
			p = b[4:]
			d = b[4:]
			delta = d1
		}
		v := archsimd.BroadcastInt32x4(int32(delta))
		for y := range 4 {
			StorePixels4(d[y*stride:], LoadPixels4(p[y*stride:]).Add(v))
		}
	}
}

// Rounding uses wrapping int32 vector addition before the arithmetic shift.
// Each lane has only a positive or negative clipped correction. The byte
// shuffle repeats four copies of DC0 and DC1 in each eight-byte row packet.
var dcPairCoeffByteIndices = [16]int8{0, 0, 0, 0, 4, 4, 4, 4, 0, 0, 0, 0, 4, 4, 4, 4}

func dcPairCoeffBytes(c0, c1 int32) (archsimd.Uint8x16, archsimd.Uint8x16) {
	d := archsimd.Int32x4{}.SetElem(0, c0).SetElem(1, c1).Add(archsimd.BroadcastInt32x4(4)).ShiftAllRight(3)
	zero := archsimd.LoadInt16x8Array(&packZero16).ToBits().ReshapeToUint32s().BitsToInt32()
	maxByte := archsimd.BroadcastInt32x4(255)
	indices := archsimd.LoadInt8x16Array(&dcPairCoeffByteIndices)
	plus := d.Max(zero).Min(maxByte).ToBits().ReshapeToUint8s().PermuteOrZero(indices)
	minus := zero.Sub(d).Max(zero).Min(maxByte).ToBits().ReshapeToUint8s().PermuteOrZero(indices)
	return plus, minus
}

// Both exact eight-byte rows are captured before the two ordered stores.
// Callers prove complete windows and exact aliasing or mutually disjoint views.
func dcPairByteRows2(src []byte, ps int, out []byte, ds int, plus, minus archsimd.Uint8x16) {
	v := (archsimd.Uint64x2{}).SetElem(0, binary.LittleEndian.Uint64(src)).SetElem(1, binary.LittleEndian.Uint64(src[ps:])).ReshapeToUint8s()
	v = v.AddSaturated(plus).SubSaturated(minus)
	words := v.ReshapeToUint64s()
	binary.LittleEndian.PutUint64(out, words.GetElem(0))
	binary.LittleEndian.PutUint64(out[ds:], words.GetElem(1))
}
