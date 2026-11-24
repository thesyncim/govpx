package dsp

import "github.com/thesyncim/govpx/internal/pixel"

func varianceBlockSized(src []byte, srcStride int, ref []byte, refStride int, width, height int) (int, int) {
	if pixel.Enabled {
		sum, sse := pixel.Stats(src, srcStride, ref, refStride, width, height)
		return sum, int(sse)
	}

	switch width {
	case 16:
		return varianceBlock16xNScalar(src, srcStride, ref, refStride, height)
	case 8:
		return varianceBlock8xNScalar(src, srcStride, ref, refStride, height)
	case 4:
		return varianceBlock4xNScalar(src, srcStride, ref, refStride, height)
	default:
		return varianceBlockGeneric(src, srcStride, ref, refStride, width, height)
	}
}

func SSE16xNFast(src []byte, srcStride int, ref []byte, refStride int, height int) int {
	return int(pixel.SSE16(src, srcStride, ref, refStride, height))
}

func varianceBlock16xNScalar(src []byte, srcStride int, ref []byte, refStride int, height int) (int, int) {
	_ = src[(height-1)*srcStride+15]
	_ = ref[(height-1)*refStride+15]
	sum := 0
	sse := 0
	for y := 0; y < height; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		d0 := int(srcRow[0]) - int(refRow[0])
		d1 := int(srcRow[1]) - int(refRow[1])
		d2 := int(srcRow[2]) - int(refRow[2])
		d3 := int(srcRow[3]) - int(refRow[3])
		d4 := int(srcRow[4]) - int(refRow[4])
		d5 := int(srcRow[5]) - int(refRow[5])
		d6 := int(srcRow[6]) - int(refRow[6])
		d7 := int(srcRow[7]) - int(refRow[7])
		d8 := int(srcRow[8]) - int(refRow[8])
		d9 := int(srcRow[9]) - int(refRow[9])
		d10 := int(srcRow[10]) - int(refRow[10])
		d11 := int(srcRow[11]) - int(refRow[11])
		d12 := int(srcRow[12]) - int(refRow[12])
		d13 := int(srcRow[13]) - int(refRow[13])
		d14 := int(srcRow[14]) - int(refRow[14])
		d15 := int(srcRow[15]) - int(refRow[15])
		sum += d0 + d1 + d2 + d3 + d4 + d5 + d6 + d7 + d8 + d9 + d10 + d11 + d12 + d13 + d14 + d15
		sse += d0*d0 + d1*d1 + d2*d2 + d3*d3 + d4*d4 + d5*d5 + d6*d6 + d7*d7 +
			d8*d8 + d9*d9 + d10*d10 + d11*d11 + d12*d12 + d13*d13 + d14*d14 + d15*d15
	}
	return sum, sse
}

func varianceBlock8xNScalar(src []byte, srcStride int, ref []byte, refStride int, height int) (int, int) {
	_ = src[(height-1)*srcStride+7]
	_ = ref[(height-1)*refStride+7]
	sum := 0
	sse := 0
	for y := 0; y < height; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		d0 := int(srcRow[0]) - int(refRow[0])
		d1 := int(srcRow[1]) - int(refRow[1])
		d2 := int(srcRow[2]) - int(refRow[2])
		d3 := int(srcRow[3]) - int(refRow[3])
		d4 := int(srcRow[4]) - int(refRow[4])
		d5 := int(srcRow[5]) - int(refRow[5])
		d6 := int(srcRow[6]) - int(refRow[6])
		d7 := int(srcRow[7]) - int(refRow[7])
		sum += d0 + d1 + d2 + d3 + d4 + d5 + d6 + d7
		sse += d0*d0 + d1*d1 + d2*d2 + d3*d3 + d4*d4 + d5*d5 + d6*d6 + d7*d7
	}
	return sum, sse
}

func varianceBlock4xNScalar(src []byte, srcStride int, ref []byte, refStride int, height int) (int, int) {
	_ = src[(height-1)*srcStride+3]
	_ = ref[(height-1)*refStride+3]
	sum := 0
	sse := 0
	for y := 0; y < height; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		d0 := int(srcRow[0]) - int(refRow[0])
		d1 := int(srcRow[1]) - int(refRow[1])
		d2 := int(srcRow[2]) - int(refRow[2])
		d3 := int(srcRow[3]) - int(refRow[3])
		sum += d0 + d1 + d2 + d3
		sse += d0*d0 + d1*d1 + d2*d2 + d3*d3
	}
	return sum, sse
}
