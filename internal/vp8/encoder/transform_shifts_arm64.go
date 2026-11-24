//go:build goexperiment.simd && arm64 && !purego

// Fixed immediate shifts preserve the libvpx v1.16.0 VP8 transform arithmetic
// without loading 16-byte shift-count vectors per operation.
package encoder

import "simd/archsimd"

func vp8Left2(v archsimd.Int32x4) archsimd.Int32x4 { return v.ShiftAllLeft(2) }

func vp8Left3(v archsimd.Int32x4) archsimd.Int32x4 { return v.ShiftAllLeft(3) }

func vp8Right3(v archsimd.Int32x4) archsimd.Int32x4 { return v.ShiftAllRight(3) }

func vp8Right4(v archsimd.Int32x4) archsimd.Int32x4 { return v.ShiftAllRight(4) }

func vp8Right12(v archsimd.Int32x4) archsimd.Int32x4 { return v.ShiftAllRight(12) }

func vp8Right16(v archsimd.Int32x4) archsimd.Int32x4 { return v.ShiftAllRight(16) }

func vp8Sign(v archsimd.Int32x4) archsimd.Int32x4 { return v.ShiftAllRight(31) }

func vp8Zero4(v archsimd.Int32x4) bool { return v.ToBits().ReduceMax() == 0 }
