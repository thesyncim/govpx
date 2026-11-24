//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// The caller bounds dimensions and strides. Decline incomplete windows so
// the checked fallback preserves its original partial-write/panic behavior.
func subtractRowsBounded(src []byte, ss int, pred []byte, ps int, out []int16, os, w, h int) (bool, bool) {
	sn, pn, on := (h-1)*ss+w, (h-1)*ps+w, (h-1)*os+w
	if sn > len(src) || pn > len(pred) || on > len(out) {
		return false, false
	}
	if w == 4 {
		return subtractSmallRows4(src, ss, pred, ps, out, os, h), true
	}
	if w == 8 {
		return subtractSmallRows8(src, ss, pred, ps, out, os, h), true
	}
	return subtract16Rows(src, ss, pred, ps, out, os, w, h), true
}

func subtractSmallRows4(src []byte, ss int, pred []byte, ps int, out []int16, os, h int) bool {
	var bits archsimd.Int16x8
	for y := range h {
		a := (archsimd.Uint32x4{}).SetElem(0, binary.LittleEndian.Uint32(src[y*ss:])).ReshapeToUint8s()
		b := (archsimd.Uint32x4{}).SetElem(0, binary.LittleEndian.Uint32(pred[y*ps:])).ReshapeToUint8s()
		d := widen8(a).Sub(widen8(b))
		put4(out[y*os:], d.ToBits().ReshapeToUint64s().GetElem(0))
		bits = bits.Or(d)
	}
	q := bits.ToBits().ReshapeToUint64s()
	return q.GetElem(0)|q.GetElem(1) != 0
}

func subtractSmallRows8(src []byte, ss int, pred []byte, ps int, out []int16, os, h int) bool {
	var bits archsimd.Int16x8
	for y := range h {
		a := (archsimd.Uint64x2{}).SetElem(0, binary.LittleEndian.Uint64(src[y*ss:])).ReshapeToUint8s()
		b := (archsimd.Uint64x2{}).SetElem(0, binary.LittleEndian.Uint64(pred[y*ps:])).ReshapeToUint8s()
		d := widen8(a).Sub(widen8(b))
		d.Store(out[y*os:])
		bits = bits.Or(d)
	}
	q := bits.ToBits().ReshapeToUint64s()
	return q.GetElem(0)|q.GetElem(1) != 0
}
