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
			ss2, ds2 := 2*ss, 2*ds
			ss3, ds3 := 3*ss, 3*ds
			ss4, ds4 := 4*ss, 4*ds
			y := 0
			for ; y+3 < h; y += 4 {
				v0 := *(*[8]byte)(src[so : so+8])
				v1 := *(*[8]byte)(src[so+ss : so+ss+8])
				v2 := *(*[8]byte)(src[so+ss2 : so+ss2+8])
				v3 := *(*[8]byte)(src[so+ss3 : so+ss3+8])
				*(*[8]byte)(dst[do : do+8]) = v0
				*(*[8]byte)(dst[do+ds : do+ds+8]) = v1
				*(*[8]byte)(dst[do+ds2 : do+ds2+8]) = v2
				*(*[8]byte)(dst[do+ds3 : do+ds3+8]) = v3
				so += ss4
				do += ds4
			}
			for ; y < h; y++ {
				*(*[8]byte)(dst[do : do+8]) = *(*[8]byte)(src[so : so+8])
				so += ss
				do += ds
			}
			return
		case 16:
			ss2, ds2 := 2*ss, 2*ds
			ss3, ds3 := 3*ss, 3*ds
			ss4, ds4 := 4*ss, 4*ds
			y := 0
			for ; y+3 < h; y += 4 {
				v0 := *(*[16]byte)(src[so : so+16])
				v1 := *(*[16]byte)(src[so+ss : so+ss+16])
				v2 := *(*[16]byte)(src[so+ss2 : so+ss2+16])
				v3 := *(*[16]byte)(src[so+ss3 : so+ss3+16])
				*(*[16]byte)(dst[do : do+16]) = v0
				*(*[16]byte)(dst[do+ds : do+ds+16]) = v1
				*(*[16]byte)(dst[do+ds2 : do+ds2+16]) = v2
				*(*[16]byte)(dst[do+ds3 : do+ds3+16]) = v3
				so += ss4
				do += ds4
			}
			for ; y < h; y++ {
				*(*[16]byte)(dst[do : do+16]) = *(*[16]byte)(src[so : so+16])
				so += ss
				do += ds
			}
			return
		case 32:
			ss2, ds2 := 2*ss, 2*ds
			ss3, ds3 := 3*ss, 3*ds
			ss4, ds4 := 4*ss, 4*ds
			y := 0
			for ; y+3 < h; y += 4 {
				*(*[32]byte)(dst[do : do+32]) = *(*[32]byte)(src[so : so+32])
				*(*[32]byte)(dst[do+ds : do+ds+32]) = *(*[32]byte)(src[so+ss : so+ss+32])
				*(*[32]byte)(dst[do+ds2 : do+ds2+32]) = *(*[32]byte)(src[so+ss2 : so+ss2+32])
				*(*[32]byte)(dst[do+ds3 : do+ds3+32]) = *(*[32]byte)(src[so+ss3 : so+ss3+32])
				so += ss4
				do += ds4
			}
			for ; y < h; y++ {
				*(*[32]byte)(dst[do : do+32]) = *(*[32]byte)(src[so : so+32])
				so += ss
				do += ds
			}
			return
		case 64:
			ss2, ds2 := 2*ss, 2*ds
			y := 0
			for ; y+1 < h; y += 2 {
				*(*[64]byte)(dst[do : do+64]) = *(*[64]byte)(src[so : so+64])
				*(*[64]byte)(dst[do+ds : do+ds+64]) = *(*[64]byte)(src[so+ss : so+ss+64])
				so += ss2
				do += ds2
			}
			for ; y < h; y++ {
				*(*[64]byte)(dst[do : do+64]) = *(*[64]byte)(src[so : so+64])
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
