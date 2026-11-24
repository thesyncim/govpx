//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// DCPairInPlaceInt32Native specializes libvpx v1.16.0's idct_dequant_0_2x_neon
// for one prediction/destination window, retaining every fallback access order.
func DCPairInPlaceInt32Native(c0, c1 int32, b []byte, stride int) {
	if !Enabled {
		dcPairNativeScalar(c0, c1, b, stride, b, stride)
		return
	}
	if stride >= 8 && len(b) >= 8 && stride <= (len(b)-8)/3 {
		delta := dcPairSignedCoeff8(c0, c1)
		dcPairSignedRows2(b, stride, b, stride, delta)
		b2 := b[2*stride:]
		dcPairSignedRows2(b2, stride, b2, stride, delta)
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

// Saturating the rounded coefficients to int16 is sufficient when the pixel
// addition also saturates: any larger-magnitude delta already clips every
// possible byte result. Scalar rounding deliberately wraps at int32 first.
func dcPairSignedCoeff8(c0, c1 int32) archsimd.Int16x8 {
	d := archsimd.Int32x4{}.SetElem(0, (c0+4)>>3).SetElem(1, (c1+4)>>3).SaturateToInt16()
	d = d.InterleaveLo(d)
	return d.InterleaveLo(d)
}

// Callers prove both exact eight-byte windows and preserve the original
// two-row read packet before its two ordered stores. Saturating signed sums
// preserve the final byte clipping for every rounded int32 coefficient.
func dcPairSignedRows2(src []byte, ps int, out []byte, ds int, delta archsimd.Int16x8) {
	lo := binary.LittleEndian.Uint64(src)
	hi := binary.LittleEndian.Uint64(src[ps:])
	l := (archsimd.Uint64x2{}).SetElem(0, lo).ReshapeToUint8s().ExtendLo8ToUint16().BitsToInt16().AddSaturated(delta).SaturateToUint8().ReshapeToUint64s()
	h := (archsimd.Uint64x2{}).SetElem(0, hi).ReshapeToUint8s().ExtendLo8ToUint16().BitsToInt16().AddSaturated(delta).SaturateToUint8().ReshapeToUint64s()
	binary.LittleEndian.PutUint64(out, l.GetElem(0))
	binary.LittleEndian.PutUint64(out[ds:], h.GetElem(0))
}
