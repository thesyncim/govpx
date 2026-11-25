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
	d0, d1 := dcPairNativeDelta(c0), dcPairNativeDelta(c1)
	if stride >= 8 && len(b) >= 8 && stride <= (len(b)-8)/3 {
		if d0 == 0 && d1 == 0 {
			return
		}
		_ = b[3*stride+7]
		b2 := b[2*stride:]
		if d0 >= 0 && d1 >= 0 {
			addV := dcPairByteMask(uint8(d0), uint8(d1))
			v0 := archsimd.LoadUint64x2Array(&[2]uint64{binary.LittleEndian.Uint64(b), binary.LittleEndian.Uint64(b[stride:])}).ReshapeToUint8s().AddSaturated(addV).ReshapeToUint64s()
			v1 := archsimd.LoadUint64x2Array(&[2]uint64{binary.LittleEndian.Uint64(b2), binary.LittleEndian.Uint64(b2[stride:])}).ReshapeToUint8s().AddSaturated(addV).ReshapeToUint64s()
			binary.LittleEndian.PutUint64(b, v0.GetElem(0))
			binary.LittleEndian.PutUint64(b[stride:], v0.GetElem(1))
			binary.LittleEndian.PutUint64(b2, v1.GetElem(0))
			binary.LittleEndian.PutUint64(b2[stride:], v1.GetElem(1))
			return
		}
		if d0 <= 0 && d1 <= 0 {
			subV := dcPairByteMask(uint8(-d0), uint8(-d1))
			v0 := archsimd.LoadUint64x2Array(&[2]uint64{binary.LittleEndian.Uint64(b), binary.LittleEndian.Uint64(b[stride:])}).ReshapeToUint8s().SubSaturated(subV).ReshapeToUint64s()
			v1 := archsimd.LoadUint64x2Array(&[2]uint64{binary.LittleEndian.Uint64(b2), binary.LittleEndian.Uint64(b2[stride:])}).ReshapeToUint8s().SubSaturated(subV).ReshapeToUint64s()
			binary.LittleEndian.PutUint64(b, v0.GetElem(0))
			binary.LittleEndian.PutUint64(b[stride:], v0.GetElem(1))
			binary.LittleEndian.PutUint64(b2, v1.GetElem(0))
			binary.LittleEndian.PutUint64(b2[stride:], v1.GetElem(1))
			return
		}
		var a0, a1, s0, s1 uint8
		if d0 > 0 {
			a0 = uint8(d0)
			s1 = uint8(-d1)
		} else {
			s0 = uint8(-d0)
			a1 = uint8(d1)
		}
		addV, subV := dcPairByteMask(a0, a1), dcPairByteMask(s0, s1)
		v0 := archsimd.LoadUint64x2Array(&[2]uint64{binary.LittleEndian.Uint64(b), binary.LittleEndian.Uint64(b[stride:])}).ReshapeToUint8s().AddSaturated(addV).SubSaturated(subV).ReshapeToUint64s()
		v1 := archsimd.LoadUint64x2Array(&[2]uint64{binary.LittleEndian.Uint64(b2), binary.LittleEndian.Uint64(b2[stride:])}).ReshapeToUint8s().AddSaturated(addV).SubSaturated(subV).ReshapeToUint64s()
		binary.LittleEndian.PutUint64(b, v0.GetElem(0))
		binary.LittleEndian.PutUint64(b[stride:], v0.GetElem(1))
		binary.LittleEndian.PutUint64(b2, v1.GetElem(0))
		binary.LittleEndian.PutUint64(b2[stride:], v1.GetElem(1))
		return
	}
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

func dcPairByteMask(b0, b1 uint8) archsimd.Uint8x16 {
	w0 := uint32(b0) * 0x01010101
	w1 := uint32(b1) * 0x01010101
	q := uint64(w0) | (uint64(w1) << 32)
	return archsimd.BroadcastUint64x2(q).ReshapeToUint8s()
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

// DCPairNative selects the single-owner ARM implementation of libvpx
// v1.16.0 idct_dequant_0_2x_neon, retaining the original signed product math.
const DCPairNative = true

// DCPairInt32Native owns rounding, byte arithmetic and fallback, so a public
// caller need not keep ten arguments live across a boolean helper call.
func DCPairInt32Native(c0, c1 int32, pred []byte, ps int, dst []byte, ds int) {
	if !Enabled {
		dcPairNativeScalar(c0, c1, pred, ps, dst, ds)
		return
	}
fast:
	for {
		if ps < 8 || ds < 8 || len(pred) < 8 || len(dst) < 8 || ps > (len(pred)-8)/3 || ds > (len(dst)-8)/3 {
			break fast
		}
		if &pred[0] == &dst[0] {
			if ps != ds {
				break fast
			}
		} else if slicesOverlapExact(pred, dst, len(pred), len(dst)) {
			break fast
		}
		delta := dcPairSignedCoeff8(c0, c1)
		dcPairSignedRows2(pred, ps, dst, ds, delta)
		dcPairSignedRows2(pred[2*ps:], ps, dst[2*ds:], ds, delta)
		return
	}
	d0, d1 := dcPairNativeDelta(c0), dcPairNativeDelta(c1)
	// Exact enabled fallback: block0 is complete before slicing block1.
	// Capture each four-byte prediction before its store as AddConstant does.
	for block := range 2 {
		p, d, delta := pred, dst, d0
		if block != 0 {
			p = pred[4:]
			d = dst[4:]
			delta = d1
		}
		v := archsimd.BroadcastInt32x4(int32(delta))
		for y := range 4 {
			StorePixels4(d[y*ds:], LoadPixels4(p[y*ps:]).Add(v))
		}
	}
}

// AddConstantPair4 retains its direct boolean no-read/no-write decline API.
// Valid calls delegate to the same native arithmetic owner; d<<3 rounds
// exactly back to d because accepted signed deltas lie in [-255,255].
func AddConstantPair4(pred []byte, ps int, dst []byte, ds int, d0, d1 int16) bool {
	if !Enabled || ps < 8 || ds < 8 || len(pred) < 8 || len(dst) < 8 || ps > (len(pred)-8)/3 || ds > (len(dst)-8)/3 || d0 < -255 || d0 > 255 || d1 < -255 || d1 > 255 {
		return false
	}
	if &pred[0] == &dst[0] {
		if ps != ds {
			return false
		}
	} else if slicesOverlapExact(pred, dst, len(pred), len(dst)) {
		return false
	}
	DCPairInt32Native(int32(d0)<<3, int32(d1)<<3, pred, ps, dst, ds)
	return true
}
