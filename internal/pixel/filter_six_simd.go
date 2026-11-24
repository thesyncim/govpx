//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// VP8's two negative taps total at most 128. Accumulating those first keeps
// the signed int16 sum exact; subsequent positive saturation already clips
// to byte 255 after the final rounding shift.
func filterSixNarrow8(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) bool {
	if len(taps) != 6 || w <= 0 || (w != 4 && w%8 != 0) || h <= 0 || ss < 0 || ds < 0 || step < 0 {
		return false
	}
	if min(taps[0], taps[2], taps[3], taps[5]) < 0 || max(taps[1], taps[4]) > 0 || min(taps[1], taps[4]) < -128 || max(taps[0], taps[2], taps[3], taps[5]) > 128 || -int(taps[1])-int(taps[4]) > 128 {
		return false
	}
	_ = src[(h-1)*ss+w-1+5*step]
	_ = dst[(h-1)*ds+w-1]
	if taps[0] == 0 && taps[5] == 0 {
		filterFourNarrow8(src[step:], ss, dst, ds, w, h, step, taps[1:5])
		return true
	}
	if w != 4 && h > 1 && step == ss && filterVerticalSix8(src, ss, dst, ds, w, h, taps) {
		return true
	}
	if step == 1 && w >= 16 && w%16 == 0 && filterHorizSix16(src, ss, dst, ds, w, h, taps) {
		return true
	}
	c0, c1, c2 := archsimd.BroadcastInt16x8(taps[0]), archsimd.BroadcastInt16x8(taps[1]), archsimd.BroadcastInt16x8(taps[2])
	c3, c4, c5 := archsimd.BroadcastInt16x8(taps[3]), archsimd.BroadcastInt16x8(taps[4]), archsimd.BroadcastInt16x8(taps[5])
	bias := archsimd.BroadcastInt16x8(64)
	n := 8
	if w == 4 {
		n = 4
	}
	for y := range h {
		for x := 0; x < w; x += n {
			at := y*ss + x
			var p0, p1, p2, p3, p4, p5 archsimd.Int16x8
			if step == 1 && len(src)-at >= 16 {
				a := archsimd.LoadUint8x16(src[at:])
				z := archsimd.Uint8x16{}
				p0 = widen8(a)
				p1 = widen8(z.ConcatShiftBytesRight(a, 1))
				p2 = widen8(z.ConcatShiftBytesRight(a, 2))
				p3 = widen8(z.ConcatShiftBytesRight(a, 3))
				p4 = widen8(z.ConcatShiftBytesRight(a, 4))
				p5 = widen8(z.ConcatShiftBytesRight(a, 5))
			} else {
				p0, p1, p2 = widen8(loadN(src[at:], n)), widen8(loadN(src[at+step:], n)), widen8(loadN(src[at+2*step:], n))
				p3, p4, p5 = widen8(loadN(src[at+3*step:], n)), widen8(loadN(src[at+4*step:], n)), widen8(loadN(src[at+5*step:], n))
			}
			v := mulAdd16(p1.Mul(c1).Add(bias), p4, c4)
			v = v.AddSaturated(p0.Mul(c0)).AddSaturated(p2.Mul(c2)).AddSaturated(p3.Mul(c3)).AddSaturated(p5.Mul(c5))
			b := packBytes(filterShift7(v))
			if n == 4 {
				binary.LittleEndian.PutUint32(dst[y*ds+x:], b.ReshapeToUint32s().GetElem(0))
			} else {
				storeBytes8(dst[y*ds+x:], b)
			}
		}
	}
	return true
}
