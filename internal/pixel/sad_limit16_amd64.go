//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

// SAD16Limit16Ptr follows libvpx v1.16.0 vp8/encoder/mcomp.c SAD row limits.
// Both inputs address readable 16x16 windows. Every vector load touches exactly
// 16 bytes; read-only overlaps and unaligned inputs are valid.
func SAD16Limit16Ptr(src *byte, ss int, ref *byte, rs, limit int) int {
	if !Enabled {
		return sad16Limit16PointerScalar(src, ss, ref, rs, limit)
	}
	// Accepted strides prove 15*stride+16 < 2^52 before offset arithmetic.
	if src == nil || ref == nil || (uint(ss)|uint(rs))>>48 != 0 {
		return SAD16Limit(unsafe.Slice(src, 15*ss+16), ss, unsafe.Slice(ref, 15*rs+16), rs, 16, limit)
	}
	sp, rp := unsafe.Pointer(src), unsafe.Pointer(ref)
	if limit >= 16*16*255 {
		var even, odd archsimd.Uint64x2
		for y := 0; y < 16; y += 2 {
			a, b := unsafe.Add(sp, y*ss), unsafe.Add(rp, y*rs)
			even = even.Add(load16(a).SumOf8AbsDiff(load16(b)))
			odd = odd.Add(load16(unsafe.Add(a, ss)).SumOf8AbsDiff(load16(unsafe.Add(b, rs))))
		}
		return sumU64(even.Add(odd))
	}
	var sum archsimd.Uint64x2
	value := 0
	for y := range 16 {
		sum = sum.Add(load16(unsafe.Add(sp, y*ss)).SumOf8AbsDiff(load16(unsafe.Add(rp, y*rs))))
		value = sumU64(sum)
		if value > limit {
			return value
		}
	}
	return value
}

// The disabled backend retains the original DSP raw-pointer loop, including
// backwards-readable windows. No slice span or vector instruction is needed.
func sad16Limit16PointerScalar(src *byte, ss int, ref *byte, rs, limit int) int {
	sp, rp := unsafe.Pointer(src), unsafe.Pointer(ref)
	sum := 0
	for y := range 16 {
		a, b := unsafe.Add(sp, y*ss), unsafe.Add(rp, y*rs)
		for x := range 16 {
			d := int(*(*byte)(unsafe.Add(a, x))) - int(*(*byte)(unsafe.Add(b, x)))
			if d < 0 {
				d = -d
			}
			sum += d
		}
		if sum > limit {
			return sum
		}
	}
	return sum
}
