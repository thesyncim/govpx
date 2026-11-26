//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// Copy16Rows follows libvpx v1.16.0 vp8/common/reconinter.c row order.
// A whole row is loaded before its store, preserving per-row memmove aliases.
func Copy16Rows(src []byte, ss int, dst []byte, ds int) bool {
	if !Enabled || (uint(ss)|uint(ds))>>30 != 0 || 15*ss+16 > len(src) || 15*ds+16 > len(dst) {
		return false
	}
	_ = src[15*ss+15]
	_ = dst[15*ds+15]
	so, do := 0, 0
	ss2, ss3, ss4 := 2*ss, 3*ss, 4*ss
	ds2, ds3, ds4 := 2*ds, 3*ds, 4*ds
	for range 4 {
		archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16])).StoreArray((*[16]byte)(dst[do : do+16]))
		archsimd.LoadUint8x16Array((*[16]byte)(src[so+ss : so+ss+16])).StoreArray((*[16]byte)(dst[do+ds : do+ds+16]))
		archsimd.LoadUint8x16Array((*[16]byte)(src[so+ss2 : so+ss2+16])).StoreArray((*[16]byte)(dst[do+ds2 : do+ds2+16]))
		archsimd.LoadUint8x16Array((*[16]byte)(src[so+ss3 : so+ss3+16])).StoreArray((*[16]byte)(dst[do+ds3 : do+ds3+16]))
		so += ss4
		do += ds4
	}
	return true
}
