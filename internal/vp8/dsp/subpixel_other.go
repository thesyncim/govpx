//go:build (!arm64 && !amd64) || purego

package dsp

import "github.com/thesyncim/govpx/internal/vp8/tables"

// Pure-Go fallback for SixTapPredict on architectures without a SIMD port.
// Mirrors the libvpx v1.16.0 scalar six-tap predictor; width-specialized
// scalar kernels keep the hot fallback paths out of the generic width/height
// loop in subpixel.go.

func sixTapPredict16x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset != 0 || yoffset != 0 {
		sixTapPredict16xNScalar(src, srcStride, xoffset, yoffset, dst, dstStride, 16)
		return true
	}
	copyCentralBlock(src, srcStride, dst, dstStride, 16, 16)
	return true
}

func sixTapPredict16x8Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset != 0 || yoffset != 0 {
		sixTapPredict16xNScalar(src, srcStride, xoffset, yoffset, dst, dstStride, 8)
		return true
	}
	copyCentralBlock(src, srcStride, dst, dstStride, 16, 8)
	return true
}

func sixTapPredict8x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset != 0 || yoffset != 0 {
		sixTapPredict8xNScalar(src, srcStride, xoffset, yoffset, dst, dstStride, 16)
		return true
	}
	copyCentralBlock(src, srcStride, dst, dstStride, 8, 16)
	return true
}

func sixTapPredict8x8Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset != 0 || yoffset != 0 {
		sixTapPredict8xNScalar(src, srcStride, xoffset, yoffset, dst, dstStride, 8)
		return true
	}
	copyCentralBlock(src, srcStride, dst, dstStride, 8, 8)
	return true
}

func sixTapPredict8x8PairMaybe(
	src0 []byte, src0Stride int,
	src1 []byte, src1Stride int,
	xoffset int, yoffset int,
	dst0 []byte, dst0Stride int,
	dst1 []byte, dst1Stride int,
) bool {
	if xoffset != 0 || yoffset != 0 {
		sixTapPredict8xNScalar(src0, src0Stride, xoffset, yoffset, dst0, dst0Stride, 8)
		sixTapPredict8xNScalar(src1, src1Stride, xoffset, yoffset, dst1, dst1Stride, 8)
		return true
	}
	copyCentralBlock(src0, src0Stride, dst0, dst0Stride, 8, 8)
	copyCentralBlock(src1, src1Stride, dst1, dst1Stride, 8, 8)
	return true
}

func sixTapPredict8x4Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset != 0 || yoffset != 0 {
		sixTapPredict8xNScalar(src, srcStride, xoffset, yoffset, dst, dstStride, 4)
		return true
	}
	copyCentralBlock(src, srcStride, dst, dstStride, 8, 4)
	return true
}

func sixTapPredict4x4Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset != 0 || yoffset != 0 {
		sixTapPredict4xNScalar(src, srcStride, xoffset, yoffset, dst, dstStride, 4)
		return true
	}
	copyCentralBlock(src, srcStride, dst, dstStride, 4, 4)
	return true
}

func sixTapPredict16xNScalar(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int, height int) {
	hFilter := tables.SubPelFilters[xoffset]
	vFilter := tables.SubPelFilters[yoffset]
	var tmp [21 * 16]byte

	for y := 0; y < height+5; y++ {
		srcRow := y * srcStride
		tmpRow := y * 16
		for x := 0; x < 16; x++ {
			v := int(src[srcRow+x+0])*int(hFilter[0]) +
				int(src[srcRow+x+1])*int(hFilter[1]) +
				int(src[srcRow+x+2])*int(hFilter[2]) +
				int(src[srcRow+x+3])*int(hFilter[3]) +
				int(src[srcRow+x+4])*int(hFilter[4]) +
				int(src[srcRow+x+5])*int(hFilter[5]) +
				tables.FilterWeight/2
			tmp[tmpRow+x] = ClipPixel(v >> tables.FilterShift)
		}
	}

	for y := 0; y < height; y++ {
		dstRow := y * dstStride
		tmpRow := y * 16
		for x := 0; x < 16; x++ {
			v := int(tmp[tmpRow+x])*int(vFilter[0]) +
				int(tmp[tmpRow+16+x])*int(vFilter[1]) +
				int(tmp[tmpRow+32+x])*int(vFilter[2]) +
				int(tmp[tmpRow+48+x])*int(vFilter[3]) +
				int(tmp[tmpRow+64+x])*int(vFilter[4]) +
				int(tmp[tmpRow+80+x])*int(vFilter[5]) +
				tables.FilterWeight/2
			dst[dstRow+x] = ClipPixel(v >> tables.FilterShift)
		}
	}
}

func sixTapPredict8xNScalar(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int, height int) {
	hFilter := tables.SubPelFilters[xoffset]
	vFilter := tables.SubPelFilters[yoffset]
	var tmp [21 * 8]byte

	for y := 0; y < height+5; y++ {
		srcRow := y * srcStride
		tmpRow := y * 8
		for x := 0; x < 8; x++ {
			v := int(src[srcRow+x+0])*int(hFilter[0]) +
				int(src[srcRow+x+1])*int(hFilter[1]) +
				int(src[srcRow+x+2])*int(hFilter[2]) +
				int(src[srcRow+x+3])*int(hFilter[3]) +
				int(src[srcRow+x+4])*int(hFilter[4]) +
				int(src[srcRow+x+5])*int(hFilter[5]) +
				tables.FilterWeight/2
			tmp[tmpRow+x] = ClipPixel(v >> tables.FilterShift)
		}
	}

	for y := 0; y < height; y++ {
		dstRow := y * dstStride
		tmpRow := y * 8
		for x := 0; x < 8; x++ {
			v := int(tmp[tmpRow+x])*int(vFilter[0]) +
				int(tmp[tmpRow+8+x])*int(vFilter[1]) +
				int(tmp[tmpRow+16+x])*int(vFilter[2]) +
				int(tmp[tmpRow+24+x])*int(vFilter[3]) +
				int(tmp[tmpRow+32+x])*int(vFilter[4]) +
				int(tmp[tmpRow+40+x])*int(vFilter[5]) +
				tables.FilterWeight/2
			dst[dstRow+x] = ClipPixel(v >> tables.FilterShift)
		}
	}
}

func sixTapPredict4xNScalar(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int, height int) {
	hFilter := tables.SubPelFilters[xoffset]
	vFilter := tables.SubPelFilters[yoffset]
	var tmp [21 * 4]byte

	for y := 0; y < height+5; y++ {
		srcRow := y * srcStride
		tmpRow := y * 4
		for x := 0; x < 4; x++ {
			v := int(src[srcRow+x+0])*int(hFilter[0]) +
				int(src[srcRow+x+1])*int(hFilter[1]) +
				int(src[srcRow+x+2])*int(hFilter[2]) +
				int(src[srcRow+x+3])*int(hFilter[3]) +
				int(src[srcRow+x+4])*int(hFilter[4]) +
				int(src[srcRow+x+5])*int(hFilter[5]) +
				tables.FilterWeight/2
			tmp[tmpRow+x] = ClipPixel(v >> tables.FilterShift)
		}
	}

	for y := 0; y < height; y++ {
		dstRow := y * dstStride
		tmpRow := y * 4
		for x := 0; x < 4; x++ {
			v := int(tmp[tmpRow+x])*int(vFilter[0]) +
				int(tmp[tmpRow+4+x])*int(vFilter[1]) +
				int(tmp[tmpRow+8+x])*int(vFilter[2]) +
				int(tmp[tmpRow+12+x])*int(vFilter[3]) +
				int(tmp[tmpRow+16+x])*int(vFilter[4]) +
				int(tmp[tmpRow+20+x])*int(vFilter[5]) +
				tables.FilterWeight/2
			dst[dstRow+x] = ClipPixel(v >> tables.FilterShift)
		}
	}
}
