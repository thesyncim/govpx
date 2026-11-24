//go:build goexperiment.simd && arm64 && !purego

package dsp

import "simd/archsimd"

func intraSumBytes(v archsimd.Uint8x16) int {
	return int(v.ExtendLo8ToUint16().Add(v.HiToLo().ExtendLo8ToUint16()).ReduceSum())
}
