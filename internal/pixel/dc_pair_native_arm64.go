//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

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
		} else if slicesOverlap(pred, dst, len(pred), len(dst)) {
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
	} else if slicesOverlap(pred, dst, len(pred), len(dst)) {
		return false
	}
	DCPairInt32Native(int32(d0)<<3, int32(d1)<<3, pred, ps, dst, ds)
	return true
}
