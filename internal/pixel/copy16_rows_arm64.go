//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// Copy16Rows follows libvpx v1.16.0 vp8/common/reconinter.c row order.
// A whole row is loaded before its store, preserving per-row memmove aliases.
func Copy16Rows(src []byte, ss int, dst []byte, ds int) bool {
	if !Enabled || (uint(ss)|uint(ds))>>30 != 0 || 15*ss+16 > len(src) || 15*ds+16 > len(dst) {
		return false
	}
	for row := range 16 {
		v := archsimd.LoadUint8x16(src[row*ss:])
		v.Store(dst[row*ds:])
	}
	return true
}
