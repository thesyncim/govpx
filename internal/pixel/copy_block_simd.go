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
		switch w {
		case 4:
			for y := range h {
				binary.LittleEndian.PutUint32(dst[y*ds:], binary.LittleEndian.Uint32(src[y*ss:]))
			}
			return
		case 8:
			for y := range h {
				binary.LittleEndian.PutUint64(dst[y*ds:], binary.LittleEndian.Uint64(src[y*ss:]))
			}
			return
		case 16:
			for y := range h {
				archsimd.LoadUint8x16Array((*[16]byte)(src[0:16])).StoreArray((*[16]byte)(dst[0:16]))
				if y+1 < h {
					src = src[ss:]
					dst = dst[ds:]
				}
			}
			return
		case 32:
			for y := range h {
				v0 := archsimd.LoadUint8x16Array((*[16]byte)(src[0:16]))
				v1 := archsimd.LoadUint8x16Array((*[16]byte)(src[16:32]))
				v0.StoreArray((*[16]byte)(dst[0:16]))
				v1.StoreArray((*[16]byte)(dst[16:32]))
				if y+1 < h {
					src = src[ss:]
					dst = dst[ds:]
				}
			}
			return
		case 64:
			for y := range h {
				v0 := archsimd.LoadUint8x16Array((*[16]byte)(src[0:16]))
				v1 := archsimd.LoadUint8x16Array((*[16]byte)(src[16:32]))
				v2 := archsimd.LoadUint8x16Array((*[16]byte)(src[32:48]))
				v3 := archsimd.LoadUint8x16Array((*[16]byte)(src[48:64]))
				v0.StoreArray((*[16]byte)(dst[0:16]))
				v1.StoreArray((*[16]byte)(dst[16:32]))
				v2.StoreArray((*[16]byte)(dst[32:48]))
				v3.StoreArray((*[16]byte)(dst[48:64]))
				if y+1 < h {
					src = src[ss:]
					dst = dst[ds:]
				}
			}
			return
		}
	}
	for y := range h {
		copy(dst[y*ds:y*ds+w], src[y*ss:y*ss+w])
	}
}
