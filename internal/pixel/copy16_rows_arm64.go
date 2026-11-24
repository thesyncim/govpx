//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// Copy16Rows follows libvpx v1.16.0 vp8/common/reconinter.c row order.
// A whole row is loaded before its store, preserving per-row memmove aliases.
func Copy16Rows(src []byte, ss int, dst []byte, ds int) bool {
	if !Enabled || (uint(ss)|uint(ds))>>30 != 0 || 15*ss+16 > len(src) || 15*ds+16 > len(dst) {
		return false
	}
	if len(src) >= 16*ss && len(dst) >= 16*ds {
		for range 16 {
			archsimd.LoadUint8x16Array((*[16]byte)(src[:16])).StoreArray((*[16]byte)(dst[:16]))
			src = src[ss:]
			dst = dst[ds:]
		}
		return true
	}
	for row := range 16 {
		v := archsimd.LoadUint8x16Array((*[16]byte)(src[row*ss : row*ss+16]))
		v.StoreArray((*[16]byte)(dst[row*ds : row*ds+16]))
	}
	return true
}
