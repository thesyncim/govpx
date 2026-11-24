//go:build goexperiment.simd && amd64 && !purego

// Fixed shifts preserve the libvpx v1.16.0 VP8 transform arithmetic.
package encoder

import "simd/archsimd"

func vp8Left2(v archsimd.Int32x4) archsimd.Int32x4   { return v.ShiftAllLeft(2) }
func vp8Left3(v archsimd.Int32x4) archsimd.Int32x4   { return v.ShiftAllLeft(3) }
func vp8Right3(v archsimd.Int32x4) archsimd.Int32x4  { return v.ShiftAllRight(3) }
func vp8Right4(v archsimd.Int32x4) archsimd.Int32x4  { return v.ShiftAllRight(4) }
func vp8Right12(v archsimd.Int32x4) archsimd.Int32x4 { return v.ShiftAllRight(12) }
func vp8Right16(v archsimd.Int32x4) archsimd.Int32x4 { return v.ShiftAllRight(16) }
func vp8Sign(v archsimd.Int32x4) archsimd.Int32x4    { return v.ShiftAllRight(31) }

func vp8Zero4(v archsimd.Int32x4) bool {
	u := v.ToBits().ReshapeToUint64s()
	return u.GetElem(0)|u.GetElem(1) == 0
}
