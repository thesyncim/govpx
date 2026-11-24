//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import "simd/archsimd"

// Frozen exact ac259fce SIMD body; fallback is the unchanged scalar API.
func tmBytesPrior(dst []byte, ds int, above, left []byte, top byte, w int) {
	if !Enabled {
		TMPredictScalar(dst, ds, above, left, top, w)
		return
	}

	t := archsimd.BroadcastInt32x4(int32(top))
	for y := range w {
		v := archsimd.BroadcastInt32x4(int32(left[y])).Sub(t)
		for x := 0; x < w; x += 4 {
			StorePixels4(dst[y*ds+x:], LoadPixels4(above[x:]).Add(v))
		}
	}
}
