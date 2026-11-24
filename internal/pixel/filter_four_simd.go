//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// Odd VP8 interpolation phases have zero outer taps. The six-tap caller
// proves the coefficient signs/bounds before omitting those unused inputs.
func filterFourNarrow8(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) {
	if w != 4 && h > 1 && step == ss && filterVerticalFour8(src, ss, dst, ds, w, h, taps) {
		return
	}
	if step == 1 && w >= 16 && w%16 == 0 && filterHorizFour16(src, ss, dst, ds, w, h, taps) {
		return
	}
	c0, c1 := archsimd.BroadcastInt16x8(taps[0]), archsimd.BroadcastInt16x8(taps[1])
	c2, c3 := archsimd.BroadcastInt16x8(taps[2]), archsimd.BroadcastInt16x8(taps[3])
	bias := archsimd.BroadcastInt16x8(64)
	n := 8
	if w == 4 {
		n = 4
	}
	for y := range h {
		for x := 0; x < w; x += n {
			at := y*ss + x
			var p0, p1, p2, p3 archsimd.Int16x8
			if step == 1 && len(src)-at >= 16 {
				a := archsimd.LoadUint8x16(src[at:])
				z := archsimd.Uint8x16{}
				p0 = widen8(a)
				p1 = widen8(z.ConcatShiftBytesRight(a, 1))
				p2 = widen8(z.ConcatShiftBytesRight(a, 2))
				p3 = widen8(z.ConcatShiftBytesRight(a, 3))
			} else {
				p0, p1 = widen8(loadN(src[at:], n)), widen8(loadN(src[at+step:], n))
				p2, p3 = widen8(loadN(src[at+2*step:], n)), widen8(loadN(src[at+3*step:], n))
			}
			v := mulAdd16(p0.Mul(c0).Add(bias), p3, c3)
			v = v.AddSaturated(p1.Mul(c1)).AddSaturated(p2.Mul(c2))
			packed := packBytes(filterShift7(v))
			if n == 4 {
				binary.LittleEndian.PutUint32(dst[y*ds+x:], packed.ReshapeToUint32s().GetElem(0))
			} else {
				storeBytes8(dst[y*ds+x:], packed)
			}
		}
	}
}
