package dsp

import "github.com/thesyncim/libgopx/internal/vp8/tables"

// Ported from libvpx v1.16.0 vp8/common/filter.c bilinear prediction paths.

func BilinearPredict4x4(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 4, 4)
}

func BilinearPredict8x4(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 4)
}

func BilinearPredict8x8(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 8)
}

func BilinearPredict16x16(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 16, 16)
}

func bilinearPredict(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int, width int, height int) {
	hFilter := tables.BilinearFilters[xoffset]
	vFilter := tables.BilinearFilters[yoffset]
	var tmp [17 * 16]uint16

	for y := 0; y < height+1; y++ {
		srcRow := y * srcStride
		tmpRow := y * width
		for x := 0; x < width; x++ {
			tmp[tmpRow+x] = uint16((int(src[srcRow+x])*int(hFilter[0]) + int(src[srcRow+x+1])*int(hFilter[1]) + tables.FilterWeight/2) >> tables.FilterShift)
		}
	}

	for y := 0; y < height; y++ {
		tmpRow := y * width
		dstRow := y * dstStride
		for x := 0; x < width; x++ {
			v := (int(tmp[tmpRow+x])*int(vFilter[0]) + int(tmp[tmpRow+width+x])*int(vFilter[1]) + tables.FilterWeight/2) >> tables.FilterShift
			dst[dstRow+x] = uint8(v)
		}
	}
}
