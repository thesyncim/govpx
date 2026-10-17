//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

// SAD16Limit16Ptr requires readable sixteen-row, sixteen-column windows, as
// does the DSP pointer API. Unusual strides retain the slice API fallback.
// Read-only input overlap and unaligned addresses are valid. Every row load
// touches exactly sixteen bytes within the caller-proved window.
func SAD16Limit16Ptr(src *byte, ss int, ref *byte, rs, limit int) int {
	// This conservative bound makes every 15*stride+16 span fit in an int.
	// Larger or negative strides use the existing fallback.
	if src == nil || ref == nil || (uint(ss)|uint(rs))>>48 != 0 {
		return SAD16Limit(unsafe.Slice(src, 15*ss+16), ss, unsafe.Slice(ref, 15*rs+16), rs, 16, limit)
	}
	sp, rp := unsafe.Pointer(src), unsafe.Pointer(ref)
	if limit >= 16*16*255 {
		return sad16ColsDualPtr(sp, ss, rp, rs, 16)
	}
	var sum archsimd.Uint16x8
	for y := range 16 {
		sum = sum.Add(absPairs(load16(step(sp, y*ss)), load16(step(rp, y*rs))))
		// Sixteen rows total at most 65280, so this unsigned reduction is exact.
		if value := int(sum.ReduceSum()); value > limit {
			return value
		}
	}
	return int(sum.ReduceSum())
}
