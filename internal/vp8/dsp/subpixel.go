package dsp

import "github.com/thesyncim/govpx/internal/vp8/tables"

// Ported from libvpx v1.16.0 vp8/common/filter.c prediction paths.

func BilinearPredict4x4(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 4, 4)
}

func BilinearPredict8x4(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 4)
}

func BilinearPredict8x8(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if bilinearPredict8x8Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 8)
}

func BilinearPredict16x16(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if bilinearPredict16x16Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 16, 16)
}

// SixTapPredict4x4 expects src to start two rows and two columns before the
// prediction origin so the six filter taps are addressable with positive
// indexes.
func SixTapPredict4x4(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if sixTapPredict4x4Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 4, 4)
}

func SixTapPredict16x8(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 16, 8)
}

func SixTapPredict8x16(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 16)
}

func SixTapPredict8x4(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if sixTapPredict8x4Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 4)
}

func SixTapPredict8x8(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if sixTapPredict8x8Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 8)
}

func SixTapPredict16x16(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if sixTapPredict16x16Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 16, 16)
}

func bilinearPredict(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int, width int, height int) {
	hFilter := tables.BilinearFilters[xoffset]
	vFilter := tables.BilinearFilters[yoffset]
	var tmp [17 * 16]uint16

	for y := 0; y < height+1; y++ {
		srcRow := y * srcStride
		tmpRow := y * width
		for x := range width {
			tmp[tmpRow+x] = uint16((int(src[srcRow+x])*int(hFilter[0]) + int(src[srcRow+x+1])*int(hFilter[1]) + tables.FilterWeight/2) >> tables.FilterShift)
		}
	}

	for y := range height {
		tmpRow := y * width
		dstRow := y * dstStride
		for x := range width {
			v := (int(tmp[tmpRow+x])*int(vFilter[0]) + int(tmp[tmpRow+width+x])*int(vFilter[1]) + tables.FilterWeight/2) >> tables.FilterShift
			dst[dstRow+x] = uint8(v)
		}
	}
}

func sixTapPredict(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int, width int, height int) {
	hFilter := tables.SubPelFilters[xoffset]
	vFilter := tables.SubPelFilters[yoffset]
	var tmp [21 * 16]int

	for y := 0; y < height+5; y++ {
		srcRow := y * srcStride
		tmpRow := y * width
		for x := range width {
			v := int(src[srcRow+x+0])*int(hFilter[0]) +
				int(src[srcRow+x+1])*int(hFilter[1]) +
				int(src[srcRow+x+2])*int(hFilter[2]) +
				int(src[srcRow+x+3])*int(hFilter[3]) +
				int(src[srcRow+x+4])*int(hFilter[4]) +
				int(src[srcRow+x+5])*int(hFilter[5]) +
				tables.FilterWeight/2
			tmp[tmpRow+x] = int(ClipPixel(v >> tables.FilterShift))
		}
	}

	for y := range height {
		dstRow := y * dstStride
		tmpRow := y * width
		for x := range width {
			v := tmp[tmpRow+x]*int(vFilter[0]) +
				tmp[tmpRow+width+x]*int(vFilter[1]) +
				tmp[tmpRow+2*width+x]*int(vFilter[2]) +
				tmp[tmpRow+3*width+x]*int(vFilter[3]) +
				tmp[tmpRow+4*width+x]*int(vFilter[4]) +
				tmp[tmpRow+5*width+x]*int(vFilter[5]) +
				tables.FilterWeight/2
			dst[dstRow+x] = ClipPixel(v >> tables.FilterShift)
		}
	}
}
