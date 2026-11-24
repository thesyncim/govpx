//go:build goexperiment.simd && amd64 && !purego

package dsp

import "simd/archsimd"

func intraSumBytes(v archsimd.Uint8x16) int {
	s := v.SumOf8AbsDiff(archsimd.Uint8x16{})
	return int(s.GetElem(0) + s.GetElem(1))
}
