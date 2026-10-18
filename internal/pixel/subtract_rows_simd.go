//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

// The caller bounds dimensions and strides. Decline incomplete windows so
// the checked fallback preserves its original partial-write/panic behavior.
func subtractRowsBounded(src []byte, ss int, pred []byte, ps int, out []int16, os, w, h int) (bool, bool) {
	sn, pn, on := (h-1)*ss+w, (h-1)*ps+w, (h-1)*os+w
	if sn > len(src) || pn > len(pred) || on > len(out) {
		return false, false
	}
	sp, pp, op := unsafe.Pointer(unsafe.SliceData(src)), unsafe.Pointer(unsafe.SliceData(pred)), unsafe.Pointer(unsafe.SliceData(out))
	// Wider reads must not observe output stores earlier than the eight-lane
	// fallback. Read-only input aliases are fine; output/input overlap declines.
	su, pu, ou := uintptr(sp), uintptr(pp), uintptr(op)
	if ou-su < uintptr(sn) || su-ou < uintptr(2*on) || ou-pu < uintptr(pn) || pu-ou < uintptr(2*on) {
		return false, false
	}
	if w == 4 {
		return subtractSmallRows4(sp, ss, pp, ps, op, os, h), true
	}
	if w == 8 {
		return subtractSmallRows8(sp, ss, pp, ps, op, os, h), true
	}
	return subtract16Rows(sp, ss, pp, ps, op, os, w, h), true
}

func subtractSmallRows4(sp unsafe.Pointer, ss int, pp unsafe.Pointer, ps int, op unsafe.Pointer, os, h int) bool {
	var bits archsimd.Int16x8
	for {
		a := (archsimd.Uint32x4{}).SetElem(0, *(*uint32)(sp)).ReshapeToUint8s()
		b := (archsimd.Uint32x4{}).SetElem(0, *(*uint32)(pp)).ReshapeToUint8s()
		d := widen8(a).Sub(widen8(b))
		*(*uint64)(op) = d.ToBits().ReshapeToUint64s().GetElem(0)
		bits = bits.Or(d)
		h--
		if h == 0 {
			break
		}
		sp = unsafe.Add(sp, ss)
		pp = unsafe.Add(pp, ps)
		op = unsafe.Add(op, 2*os)
	}
	q := bits.ToBits().ReshapeToUint64s()
	return q.GetElem(0)|q.GetElem(1) != 0
}

func subtractSmallRows8(sp unsafe.Pointer, ss int, pp unsafe.Pointer, ps int, op unsafe.Pointer, os, h int) bool {
	var bits archsimd.Int16x8
	for {
		a := (archsimd.Uint64x2{}).SetElem(0, *(*uint64)(sp)).ReshapeToUint8s()
		b := (archsimd.Uint64x2{}).SetElem(0, *(*uint64)(pp)).ReshapeToUint8s()
		d := widen8(a).Sub(widen8(b))
		d.StoreArray((*[8]int16)(op))
		bits = bits.Or(d)
		h--
		if h == 0 {
			break
		}
		sp = unsafe.Add(sp, ss)
		pp = unsafe.Add(pp, ps)
		op = unsafe.Add(op, 2*os)
	}
	q := bits.ToBits().ReshapeToUint64s()
	return q.GetElem(0)|q.GetElem(1) != 0
}
