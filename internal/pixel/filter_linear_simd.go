//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// Nonnegative two-tap filters with total weight <=128 fit signed int16,
// including the rounding bias: 255*128+64 =32704. Eight output pixels can
// therefore share one multiply-add pipeline with no intermediate clipping.
func filterLinear8(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) bool {
	if len(taps) != 2 || taps[0] < 0 || taps[1] < 0 || int(taps[0])+int(taps[1]) > 128 || w <= 0 || (w != 4 && w%8 != 0) || h <= 0 || ss < 0 || ds < 0 || step < 0 {
		return false
	}
	_ = src[(h-1)*ss+w-1+step]
	_ = dst[(h-1)*ds+w-1]
	c0, c1, bias := archsimd.BroadcastInt16x8(taps[0]), archsimd.BroadcastInt16x8(taps[1]), archsimd.BroadcastInt16x8(64)
	n := 8
	if w == 4 {
		n = 4
	}
	for y := range h {
		for x := 0; x < w; x += n {
			p := src[y*ss+x:]
			a, b := widen8(loadN(p, n)), widen8(loadN(p[step:], n))
			v := packBytes(filterShift7(mulAdd16(a.Mul(c0).Add(bias), b, c1)))
			if n == 4 {
				binary.LittleEndian.PutUint32(dst[y*ds+x:], v.ReshapeToUint32s().GetElem(0))
			} else {
				storeBytes8(dst[y*ds+x:], v)
			}
		}
	}
	return true
}
