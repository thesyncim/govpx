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
			y := 0
			for ; y+1 < h; y += 2 {
				binary.LittleEndian.PutUint64(dst[do:], binary.LittleEndian.Uint64(src[so:]))
				binary.LittleEndian.PutUint64(dst[do+ds:], binary.LittleEndian.Uint64(src[so+ss:]))
				so += 2 * ss
				do += 2 * ds
			}
			if y < h {
				binary.LittleEndian.PutUint64(dst[do:], binary.LittleEndian.Uint64(src[so:]))
			}
			return
		case 16:
			y := 0
			for ; y+1 < h; y += 2 {
				archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16])).StoreArray((*[16]byte)(dst[do : do+16]))
				archsimd.LoadUint8x16Array((*[16]byte)(src[so+ss : so+ss+16])).StoreArray((*[16]byte)(dst[do+ds : do+ds+16]))
				so += 2 * ss
				do += 2 * ds
			}
			if y < h {
				archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16])).StoreArray((*[16]byte)(dst[do : do+16]))
			}
			return
		case 32:
			y := 0
			for ; y+1 < h; y += 2 {
				s0, d0 := src[so:so+32], dst[do:do+32]
				s1, d1 := src[so+ss:so+ss+32], dst[do+ds:do+ds+32]
				v0 := archsimd.LoadUint8x16Array((*[16]byte)(s0[0:16]))
				v1 := archsimd.LoadUint8x16Array((*[16]byte)(s0[16:32]))
				v2 := archsimd.LoadUint8x16Array((*[16]byte)(s1[0:16]))
				v3 := archsimd.LoadUint8x16Array((*[16]byte)(s1[16:32]))
				v0.StoreArray((*[16]byte)(d0[0:16]))
				v1.StoreArray((*[16]byte)(d0[16:32]))
				v2.StoreArray((*[16]byte)(d1[0:16]))
				v3.StoreArray((*[16]byte)(d1[16:32]))
				so += 2 * ss
				do += 2 * ds
			}
			if y < h {
				s := src[so : so+32]
				d := dst[do : do+32]
				archsimd.LoadUint8x16Array((*[16]byte)(s[0:16])).StoreArray((*[16]byte)(d[0:16]))
				archsimd.LoadUint8x16Array((*[16]byte)(s[16:32])).StoreArray((*[16]byte)(d[16:32]))
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

// ExtendPlaneBorder replicates the four visible edges of a plane into its
// surrounding border using SIMD 16-byte stores for left/right margins (16 or 32
// bytes) and row copies for top/bottom margins.
func ExtendPlaneBorder(full []byte, origin, stride, width, height, borderX, borderY int) bool {
	if len(full) == 0 || stride <= 0 || width <= 0 || height <= 0 || borderX <= 0 || borderY <= 0 {
		return false
	}
	if borderX > stride/2 || width > stride-2*borderX {
		return false
	}
	if origin < borderX || borderY > (origin-borderX)/stride {
		return false
	}
	tail := len(full) - origin
	if width+borderX > tail {
		return false
	}
	maxRows := (tail - width - borderX) / stride
	if height-1 > maxRows || borderY > maxRows-(height-1) {
		return false
	}
	switch borderX {
	case 32:
		rowStart := origin
		for range height {
			lf := archsimd.BroadcastUint8x16(full[rowStart])
			lf.StoreArray((*[16]byte)(full[rowStart-32 : rowStart-16]))
			lf.StoreArray((*[16]byte)(full[rowStart-16 : rowStart]))
			rf := archsimd.BroadcastUint8x16(full[rowStart+width-1])
			rf.StoreArray((*[16]byte)(full[rowStart+width : rowStart+width+16]))
			rf.StoreArray((*[16]byte)(full[rowStart+width+16 : rowStart+width+32]))
			rowStart += stride
		}
	case 16:
		rowStart := origin
		for range height {
			archsimd.BroadcastUint8x16(full[rowStart]).StoreArray((*[16]byte)(full[rowStart-16 : rowStart]))
			archsimd.BroadcastUint8x16(full[rowStart+width-1]).StoreArray((*[16]byte)(full[rowStart+width : rowStart+width+16]))
			rowStart += stride
		}
	default:
		rowStart := origin
		for range height {
			leftFill := full[rowStart]
			leftDst := full[rowStart-borderX : rowStart]
			for i := range leftDst {
				leftDst[i] = leftFill
			}
			rightFill := full[rowStart+width-1]
			rightDst := full[rowStart+width : rowStart+width+borderX]
			for i := range rightDst {
				rightDst[i] = rightFill
			}
			rowStart += stride
		}
	}
	span := width + 2*borderX
	topSrc := full[origin-borderX : origin-borderX+span]
	for y := 1; y <= borderY; y++ {
		dstStart := origin - y*stride - borderX
		copy(full[dstStart:dstStart+span], topSrc)
	}
	botOrigin := origin + (height-1)*stride
	botSrc := full[botOrigin-borderX : botOrigin-borderX+span]
	for y := 1; y <= borderY; y++ {
		dstStart := botOrigin + y*stride - borderX
		copy(full[dstStart:dstStart+span], botSrc)
	}
	return true
}
