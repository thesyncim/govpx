//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

// Rolling rows require disjoint input/output and non-overlapping output rows.
func filterVerticalRowsAllowed(src, dst []byte, ds, w, h int) bool {
	if h < 2 || w%8 != 0 || ds < w {
		return false
	}
	sp, dp := uintptr(unsafe.Pointer(unsafe.SliceData(src))), uintptr(unsafe.Pointer(unsafe.SliceData(dst)))
	return !((sp <= dp && dp-sp < uintptr(len(src))) || (dp < sp && sp-dp < uintptr(len(dst))))
}
func filterVerticalSix8(src []byte, ss int, dst []byte, ds, w, h int, taps []int16) bool {
	if !filterVerticalRowsAllowed(src, dst, ds, w, h) {
		return false
	}
	c0 := archsimd.BroadcastInt16x8(taps[0])
	c1 := archsimd.BroadcastInt16x8(taps[1])
	c2 := archsimd.BroadcastInt16x8(taps[2])
	c3 := archsimd.BroadcastInt16x8(taps[3])
	c4 := archsimd.BroadcastInt16x8(taps[4])
	c5 := archsimd.BroadcastInt16x8(taps[5])
	bias := archsimd.BroadcastInt16x8(64)
	for x := 0; x < w; x += 8 {
		p0 := widen8(load8(src[0*ss+x:]))
		p1 := widen8(load8(src[1*ss+x:]))
		p2 := widen8(load8(src[2*ss+x:]))
		p3 := widen8(load8(src[3*ss+x:]))
		p4 := widen8(load8(src[4*ss+x:]))
		for y := range h {
			p5 := widen8(load8(src[(y+5)*ss+x:]))
			v := mulAdd16(p1.Mul(c1).Add(bias), p4, c4)
			v = v.AddSaturated(p0.Mul(c0)).AddSaturated(p2.Mul(c2)).AddSaturated(p3.Mul(c3)).AddSaturated(p5.Mul(c5))
			storeBytes8(dst[y*ds+x:], packBytes(filterShift7(v)))
			p0, p1, p2, p3, p4 = p1, p2, p3, p4, p5
		}
	}
	return true
}
func filterVerticalFour8(src []byte, ss int, dst []byte, ds, w, h int, taps []int16) bool {
	if !filterVerticalRowsAllowed(src, dst, ds, w, h) {
		return false
	}
	c0 := archsimd.BroadcastInt16x8(taps[0])
	c1 := archsimd.BroadcastInt16x8(taps[1])
	c2 := archsimd.BroadcastInt16x8(taps[2])
	c3 := archsimd.BroadcastInt16x8(taps[3])
	bias := archsimd.BroadcastInt16x8(64)
	for x := 0; x < w; x += 8 {
		p0 := widen8(load8(src[0*ss+x:]))
		p1 := widen8(load8(src[1*ss+x:]))
		p2 := widen8(load8(src[2*ss+x:]))
		for y := range h {
			p3 := widen8(load8(src[(y+3)*ss+x:]))
			v := mulAdd16(p0.Mul(c0).Add(bias), p3, c3)
			v = v.AddSaturated(p1.Mul(c1)).AddSaturated(p2.Mul(c2))
			storeBytes8(dst[y*ds+x:], packBytes(filterShift7(v)))
			p0, p1, p2 = p1, p2, p3
		}
	}
	return true
}
