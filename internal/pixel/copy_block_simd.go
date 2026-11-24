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
				archsimd.LoadUint8x16(src[y*ss:]).Store(dst[y*ds:])
			}
			return
		case 32:
			for y := range h {
				s := src[y*ss:]
				d := dst[y*ds:]
				v0 := archsimd.LoadUint8x16(s[0:16])
				v1 := archsimd.LoadUint8x16(s[16:32])
				v0.Store(d[0:16])
				v1.Store(d[16:32])
			}
			return
		case 64:
			for y := range h {
				s := src[y*ss:]
				d := dst[y*ds:]
				v0 := archsimd.LoadUint8x16(s[0:16])
				v1 := archsimd.LoadUint8x16(s[16:32])
				v2 := archsimd.LoadUint8x16(s[32:48])
				v3 := archsimd.LoadUint8x16(s[48:64])
				v0.Store(d[0:16])
				v1.Store(d[16:32])
				v2.Store(d[32:48])
				v3.Store(d[48:64])
			}
			return
		}
	}
	for y := range h {
		copy(dst[y*ds:y*ds+w], src[y*ss:y*ss+w])
	}
}
