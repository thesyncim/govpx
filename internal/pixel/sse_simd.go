//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"simd/archsimd"
)

func ssePacket(lo, hi archsimd.Int32x4, a, b archsimd.Uint8x16) (archsimd.Int32x4, archsimd.Int32x4) {
	dl := widen8(a).Sub(widen8(b))
	dh := widenHi8(a).BitsToInt16().Sub(widenHi8(b).BitsToInt16())
	return lo.Add(square8(dl)), hi.Add(square8(dh))
}
