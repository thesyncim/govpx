package decoder

import (
	"errors"

	"github.com/thesyncim/libgopx/internal/vp8/common"
)

// Ported from libvpx v1.16.0:
// - vp8/common/postproc.c
// - vpx_dsp/deblock.c

var ErrPostProcessBufferTooSmall = errors.New("libgopx: VP8 postprocess buffer too small")

var postProcessRV = [...]int16{
	8, 5, 2, 2, 8, 12, 4, 9, 8, 3, 0, 3, 9, 0, 0, 0, 8, 3, 14,
	4, 10, 1, 11, 14, 1, 14, 9, 6, 12, 11, 8, 6, 10, 0, 0, 8, 9, 0,
	3, 14, 8, 11, 13, 4, 2, 9, 0, 3, 9, 6, 1, 2, 3, 14, 13, 1, 8,
	2, 9, 7, 3, 3, 1, 13, 13, 6, 6, 5, 2, 7, 11, 9, 11, 8, 7, 3,
	2, 0, 13, 13, 14, 4, 12, 5, 12, 10, 8, 10, 13, 10, 4, 14, 4, 10, 0,
	8, 11, 1, 13, 7, 7, 14, 6, 14, 13, 2, 13, 5, 4, 4, 0, 10, 0, 5,
	13, 2, 12, 7, 11, 13, 8, 0, 4, 10, 7, 2, 7, 2, 2, 5, 3, 4, 7,
	3, 3, 14, 14, 5, 9, 13, 3, 14, 3, 6, 3, 0, 11, 8, 13, 1, 13, 1,
	12, 0, 10, 9, 7, 6, 2, 8, 5, 2, 13, 7, 1, 13, 14, 7, 6, 7, 9,
	6, 10, 11, 7, 8, 7, 5, 14, 8, 4, 4, 0, 8, 7, 10, 0, 8, 14, 11,
	3, 12, 5, 7, 14, 3, 14, 5, 2, 6, 11, 12, 12, 8, 0, 11, 13, 1, 2,
	0, 5, 10, 14, 7, 8, 0, 4, 11, 0, 8, 0, 3, 10, 5, 8, 0, 11, 6,
	7, 8, 10, 7, 13, 9, 2, 5, 1, 5, 10, 2, 4, 3, 5, 6, 10, 8, 9,
	4, 11, 14, 0, 10, 0, 5, 13, 2, 12, 7, 11, 13, 8, 0, 4, 10, 7, 2,
	7, 2, 2, 5, 3, 4, 7, 3, 3, 14, 14, 5, 9, 13, 3, 14, 3, 6, 3,
	0, 11, 8, 13, 1, 13, 1, 12, 0, 10, 9, 7, 6, 2, 8, 5, 2, 13, 7,
	1, 13, 14, 7, 6, 7, 9, 6, 10, 11, 7, 8, 7, 5, 14, 8, 4, 4, 0,
	8, 7, 10, 0, 8, 14, 11, 3, 12, 5, 7, 14, 3, 14, 5, 2, 6, 11, 12,
	12, 8, 0, 11, 13, 1, 2, 0, 5, 10, 14, 7, 8, 0, 4, 11, 0, 8, 0,
	3, 10, 5, 8, 0, 11, 6, 7, 8, 10, 7, 13, 9, 2, 5, 1, 5, 10, 2,
	4, 3, 5, 6, 10, 8, 9, 4, 11, 14, 3, 8, 3, 7, 8, 5, 11, 4, 12,
	3, 11, 9, 14, 8, 14, 13, 4, 3, 1, 2, 14, 6, 5, 4, 4, 11, 4, 6,
	2, 1, 5, 8, 8, 12, 13, 5, 14, 10, 12, 13, 0, 9, 5, 5, 11, 10, 13,
	9, 10, 13,
}

func ApplyPostProcess(src *common.Image, dst *common.FrameBuffer, rows int, cols int, modes []MacroblockMode, filterLevel uint8, scratch []byte) error {
	if src == nil || dst == nil || rows <= 0 || cols <= 0 || len(modes) < rows*cols || len(scratch) < cols*24 {
		return ErrPostProcessBufferTooSmall
	}
	if !validPostProcessImage(src) || !validPostProcessImage(&dst.Img) {
		return ErrPostProcessBufferTooSmall
	}
	copyPostProcessImage(&dst.Img, src)
	dst.ExtendBorders()

	q := int(filterLevel) * 10 / 6
	if q > 63 {
		q = 63
	}
	q += (4 - 5) * 10

	yLimits := scratch[:cols*16]
	uvLimits := scratch[cols*16 : cols*24]
	deblockPostProcess(src, &dst.Img, rows, cols, modes, q, yLimits, uvLimits)
	demacroblockPostProcess(&dst.Img, q)
	return nil
}

func validPostProcessImage(img *common.Image) bool {
	if img.Width <= 0 || img.Height <= 0 || img.CodedWidth <= 0 || img.CodedHeight <= 0 {
		return false
	}
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	return img.YBorder >= 17 && img.UVBorder >= 9 &&
		len(img.YFull) != 0 && len(img.UFull) != 0 && len(img.VFull) != 0 &&
		img.YOrigin >= 0 && img.UOrigin >= 0 && img.VOrigin >= 0 &&
		img.YStride >= img.CodedWidth+2*img.YBorder &&
		img.UStride >= ((img.CodedWidth+1)>>1)+2*img.UVBorder &&
		img.VStride >= ((img.CodedWidth+1)>>1)+2*img.UVBorder &&
		len(img.Y) >= planeLen(img.YStride, img.CodedHeight, img.CodedWidth) &&
		len(img.U) >= planeLen(img.UStride, uvHeight, uvWidth) &&
		len(img.V) >= planeLen(img.VStride, uvHeight, uvWidth)
}

func planeLen(stride int, height int, width int) int {
	if height <= 0 {
		return 0
	}
	return (height-1)*stride + width
}

func copyPostProcessImage(dst *common.Image, src *common.Image) {
	copy(dst.Y, src.Y)
	copy(dst.U, src.U)
	copy(dst.V, src.V)
}

func q2mbl(x int) int {
	if x < 20 {
		x = 20
	}
	x = 50 + (x-50)*10/8
	return x * x / 3
}

func postProcessDeblockLevel(q int) int {
	level := 0.00006*float64(q*q*q) - 0.0067*float64(q*q) + 0.306*float64(q) + 0.0065
	return int(level + 0.5)
}

func deblockPostProcess(src *common.Image, dst *common.Image, rows int, cols int, modes []MacroblockMode, q int, yLimits []byte, uvLimits []byte) {
	ppl := postProcessDeblockLevel(q)
	if ppl <= 0 || dst.Width < 8 || dst.Height < 8 {
		copyPostProcessImage(dst, src)
		return
	}

	uvWidth := (dst.Width + 1) >> 1
	for mbRow := 0; mbRow < rows; mbRow++ {
		for mbCol := 0; mbCol < cols; mbCol++ {
			limit := byte(ppl)
			if modes[mbRow*cols+mbCol].MBSkipCoeff {
				limit = byte(ppl >> 1)
			}
			fillBytes(yLimits[mbCol*16:mbCol*16+16], limit)
			fillBytes(uvLimits[mbCol*8:mbCol*8+8], limit)
		}

		yStart := src.YOrigin + 16*mbRow*src.YStride
		yDstStart := dst.YOrigin + 16*mbRow*dst.YStride
		postProcDownAndAcrossMBRow(src.YFull, yStart, dst.YFull, yDstStart, src.YStride, dst.YStride, dst.Width, yLimits, 16)

		if uvWidth >= 8 {
			uStart := src.UOrigin + 8*mbRow*src.UStride
			uDstStart := dst.UOrigin + 8*mbRow*dst.UStride
			vStart := src.VOrigin + 8*mbRow*src.VStride
			vDstStart := dst.VOrigin + 8*mbRow*dst.VStride
			postProcDownAndAcrossMBRow(src.UFull, uStart, dst.UFull, uDstStart, src.UStride, dst.UStride, uvWidth, uvLimits, 8)
			postProcDownAndAcrossMBRow(src.VFull, vStart, dst.VFull, vDstStart, src.VStride, dst.VStride, uvWidth, uvLimits, 8)
		}
	}
}

func demacroblockPostProcess(img *common.Image, q int) {
	level := q2mbl(q)
	mbPostProcAcrossIP(img.YFull, img.YOrigin, img.YStride, img.Height, img.Width, level)
	mbPostProcDown(img.YFull, img.YOrigin, img.YStride, img.Height, img.Width, level)
}

func fillBytes(dst []byte, value byte) {
	for i := range dst {
		dst[i] = value
	}
}

func postProcDownAndAcrossMBRow(src []byte, srcStart int, dst []byte, dstStart int, srcPitch int, dstPitch int, cols int, flimits []byte, size int) {
	for row := 0; row < size; row++ {
		srcRow := srcStart + row*srcPitch
		dstRow := dstStart + row*dstPitch

		for col := 0; col < cols; col++ {
			v := src[srcRow+col]
			limit := int(flimits[col])
			if byteDiff(v, src[srcRow+col-2*srcPitch]) < limit &&
				byteDiff(v, src[srcRow+col-srcPitch]) < limit &&
				byteDiff(v, src[srcRow+col+srcPitch]) < limit &&
				byteDiff(v, src[srcRow+col+2*srcPitch]) < limit {
				k1 := (int(src[srcRow+col-2*srcPitch]) + int(src[srcRow+col-srcPitch]) + 1) >> 1
				k2 := (int(src[srcRow+col+2*srcPitch]) + int(src[srcRow+col+srcPitch]) + 1) >> 1
				k3 := (k1 + k2 + 1) >> 1
				v = byte((k3 + int(v) + 1) >> 1)
			}
			dst[dstRow+col] = v
		}

		dst[dstRow-2] = dst[dstRow]
		dst[dstRow-1] = dst[dstRow]
		dst[dstRow+cols] = dst[dstRow+cols-1]
		dst[dstRow+cols+1] = dst[dstRow+cols-1]

		var delayed [4]byte
		for col := 0; col < cols; col++ {
			v := dst[dstRow+col]
			limit := int(flimits[col])
			if byteDiff(v, dst[dstRow+col-2]) < limit &&
				byteDiff(v, dst[dstRow+col-1]) < limit &&
				byteDiff(v, dst[dstRow+col+1]) < limit &&
				byteDiff(v, dst[dstRow+col+2]) < limit {
				k1 := (int(dst[dstRow+col-2]) + int(dst[dstRow+col-1]) + 1) >> 1
				k2 := (int(dst[dstRow+col+2]) + int(dst[dstRow+col+1]) + 1) >> 1
				k3 := (k1 + k2 + 1) >> 1
				v = byte((k3 + int(v) + 1) >> 1)
			}
			delayed[col&3] = v
			if col >= 2 {
				dst[dstRow+col-2] = delayed[(col-2)&3]
			}
		}
		dst[dstRow+cols-2] = delayed[(cols-2)&3]
		dst[dstRow+cols-1] = delayed[(cols-1)&3]
	}
}

func mbPostProcAcrossIP(plane []byte, start int, pitch int, rows int, cols int, flimit int) {
	for row := 0; row < rows; row++ {
		rowStart := start + row*pitch
		sumsq := 16
		sum := 0
		var delayed [16]byte

		for i := -8; i < 0; i++ {
			plane[rowStart+i] = plane[rowStart]
		}
		for i := 0; i < 17; i++ {
			plane[rowStart+i+cols] = plane[rowStart+cols-1]
		}
		for i := -8; i <= 6; i++ {
			v := int(plane[rowStart+i])
			sumsq += v * v
			sum += v
			delayed[i+8] = 0
		}
		for col := 0; col < cols+8; col++ {
			x := int(plane[rowStart+col+7]) - int(plane[rowStart+col-8])
			y := int(plane[rowStart+col+7]) + int(plane[rowStart+col-8])
			sum += x
			sumsq += x * y
			delayed[col&15] = plane[rowStart+col]
			if sumsq*15-sum*sum < flimit {
				delayed[col&15] = byte((8 + sum + int(plane[rowStart+col])) >> 4)
			}
			plane[rowStart+col-8] = delayed[(col-8)&15]
		}
	}
}

func mbPostProcDown(plane []byte, start int, pitch int, rows int, cols int, flimit int) {
	for col := 0; col < cols; col++ {
		s := start + col
		sumsq := 0
		sum := 0
		var delayed [16]byte

		for i := -8; i < 0; i++ {
			plane[s+i*pitch] = plane[s]
		}
		for i := 0; i < 17; i++ {
			plane[s+(i+rows)*pitch] = plane[s+(rows-1)*pitch]
		}
		for i := -8; i <= 6; i++ {
			v := int(plane[s+i*pitch])
			sumsq += v * v
			sum += v
		}
		for row := 0; row < rows+8; row++ {
			next := int(plane[s+7*pitch])
			prev := int(plane[s-8*pitch])
			sumsq += next*next - prev*prev
			sum += next - prev
			delayed[row&15] = plane[s]
			if sumsq*15-sum*sum < flimit {
				delayed[row&15] = byte((int(postProcessRV[(row&127)+(col&7)]) + sum + int(plane[s])) >> 4)
			}
			if row >= 8 {
				plane[s-8*pitch] = delayed[(row-8)&15]
			}
			s += pitch
		}
	}
}

func byteDiff(a byte, b byte) int {
	diff := int(a) - int(b)
	if diff < 0 {
		return -diff
	}
	return diff
}
