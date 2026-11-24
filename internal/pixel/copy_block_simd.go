//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// CopyBlock copies a w x h byte block from src (stride ss) to dst (stride ds)
// using fixed-width register/SIMD loads and stores for standard VP8/VP9 block
// widths (4, 8, 16, 32, 64), avoiding per-row runtime.memmove call overhead.
func CopyBlock(src []byte, ss int, dst []byte, ds, w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	if ss >= 0 && ds >= 0 && (h-1)*ss+w <= len(src) && (h-1)*ds+w <= len(dst) {
		_ = src[(h-1)*ss+w-1]
		_ = dst[(h-1)*ds+w-1]
		so, do := 0, 0
		switch w {
		case 4:
			for range h {
				binary.LittleEndian.PutUint32(dst[do:], binary.LittleEndian.Uint32(src[so:]))
				so += ss
				do += ds
			}
			return
		case 8:
			for range h {
				binary.LittleEndian.PutUint64(dst[do:], binary.LittleEndian.Uint64(src[so:]))
				so += ss
				do += ds
			}
			return
		case 16:
			for range h {
				archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16])).StoreArray((*[16]byte)(dst[do : do+16]))
				so += ss
				do += ds
			}
			return
		case 32:
			for range h {
				s := src[so : so+32]
				d := dst[do : do+32]
				v0 := archsimd.LoadUint8x16Array((*[16]byte)(s[0:16]))
				v1 := archsimd.LoadUint8x16Array((*[16]byte)(s[16:32]))
				v0.StoreArray((*[16]byte)(d[0:16]))
				v1.StoreArray((*[16]byte)(d[16:32]))
				so += ss
				do += ds
			}
			return
		case 64:
			for range h {
				s := src[so : so+64]
				d := dst[do : do+64]
				v0 := archsimd.LoadUint8x16Array((*[16]byte)(s[0:16]))
				v1 := archsimd.LoadUint8x16Array((*[16]byte)(s[16:32]))
				v2 := archsimd.LoadUint8x16Array((*[16]byte)(s[32:48]))
				v3 := archsimd.LoadUint8x16Array((*[16]byte)(s[48:64]))
				v0.StoreArray((*[16]byte)(d[0:16]))
				v1.StoreArray((*[16]byte)(d[16:32]))
				v2.StoreArray((*[16]byte)(d[32:48]))
				v3.StoreArray((*[16]byte)(d[48:64]))
				so += ss
				do += ds
			}
			return
		}
	}
	for y := range h {
		copy(dst[y*ds:y*ds+w], src[y*ss:y*ss+w])
	}
}
