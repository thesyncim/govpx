package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// copySourceToFrameBufferActive performs the active-map-aware partial frame
// copy from libvpx vp8_lookahead_push. activeMap is a row-major mb_rows*mb_cols
// array; non-zero cells mark active macroblocks. For each active run within a
// row, a 16-pixel-tall band of luma plus the colocated chroma is copied from
// src into dst; inactive macroblocks retain whatever the destination buffer
// already held. Border extension follows the full copy path.
func copySourceToFrameBufferActive(dst *vp8common.FrameBuffer, src vp8enc.SourceImage, activeMap []uint8, mbRows int, mbCols int) {
	if len(activeMap) < mbRows*mbCols {
		copySourceToFrameBuffer(dst, src)
		return
	}
	for row := range mbRows {
		col := 0
		for col < mbCols {
			// Skip leading inactive cells.
			for col < mbCols && activeMap[row*mbCols+col] == 0 {
				col++
			}
			if col >= mbCols {
				break
			}
			runStart := col
			for col < mbCols && activeMap[row*mbCols+col] != 0 {
				col++
			}
			runEnd := col
			copyActiveLumaRect(dst.Img.Y, dst.Img.YStride, src.Y, src.YStride, src.Width, src.Height, row<<4, runStart<<4, 16, (runEnd-runStart)<<4)
			copyActiveChromaRect(dst.Img.U, dst.Img.UStride, src.U, src.UStride, (src.Width+1)>>1, (src.Height+1)>>1, row<<3, runStart<<3, 8, (runEnd-runStart)<<3)
			copyActiveChromaRect(dst.Img.V, dst.Img.VStride, src.V, src.VStride, (src.Width+1)>>1, (src.Height+1)>>1, row<<3, runStart<<3, 8, (runEnd-runStart)<<3)
		}
	}
	padFrameVisibleToCoded(&dst.Img)
	dst.ExtendBorders()
}

func copyActiveLumaRect(dst []byte, dstStride int, src []byte, srcStride int, width int, height int, y0 int, x0 int, h int, w int) {
	copyActivePlaneRect(dst, dstStride, src, srcStride, width, height, y0, x0, h, w)
}

func copyActiveChromaRect(dst []byte, dstStride int, src []byte, srcStride int, width int, height int, y0 int, x0 int, h int, w int) {
	copyActivePlaneRect(dst, dstStride, src, srcStride, width, height, y0, x0, h, w)
}

func copyActivePlaneRect(dst []byte, dstStride int, src []byte, srcStride int, width int, height int, y0 int, x0 int, h int, w int) {
	yEnd := min(y0+h, height)
	xEnd := min(x0+w, width)
	for y := y0; y < yEnd; y++ {
		copy(dst[y*dstStride+x0:y*dstStride+xEnd], src[y*srcStride+x0:y*srcStride+xEnd])
	}
}
func copySourceToFrameBuffer(dst *vp8common.FrameBuffer, src vp8enc.SourceImage) {
	copyPlane(dst.Img.Y, dst.Img.YStride, src.Y, src.YStride, src.Width, src.Height)
	copyPlane(dst.Img.U, dst.Img.UStride, src.U, src.UStride, (src.Width+1)>>1, (src.Height+1)>>1)
	copyPlane(dst.Img.V, dst.Img.VStride, src.V, src.VStride, (src.Width+1)>>1, (src.Height+1)>>1)
	padFrameVisibleToCoded(&dst.Img)
	dst.ExtendBorders()
}

func padFrameVisibleToCoded(img *vp8common.Image) {
	padPlaneVisibleToCoded(img.Y, img.YStride, img.Width, img.Height, img.CodedWidth, img.CodedHeight)
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	codedUVWidth := (img.CodedWidth + 1) >> 1
	codedUVHeight := (img.CodedHeight + 1) >> 1
	padPlaneVisibleToCoded(img.U, img.UStride, uvWidth, uvHeight, codedUVWidth, codedUVHeight)
	padPlaneVisibleToCoded(img.V, img.VStride, uvWidth, uvHeight, codedUVWidth, codedUVHeight)
}

func padPlaneVisibleToCoded(plane []byte, stride int, width int, height int, codedWidth int, codedHeight int) {
	if width <= 0 || height <= 0 {
		return
	}
	for y := range height {
		row := plane[y*stride:]
		last := row[width-1]
		for x := width; x < codedWidth; x++ {
			row[x] = last
		}
	}
	lastRow := plane[(height-1)*stride:]
	for y := height; y < codedHeight; y++ {
		copy(plane[y*stride:y*stride+codedWidth], lastRow[:codedWidth])
	}
}

func sourceImageFromVP8(src *vp8common.Image) vp8enc.SourceImage {
	return vp8enc.SourceImage{
		Width:    src.Width,
		Height:   src.Height,
		UVWidth:  (src.Width + 1) >> 1,
		UVHeight: (src.Height + 1) >> 1,
		Y:        src.Y,
		U:        src.U,
		V:        src.V,
		YStride:  src.YStride,
		UStride:  src.UStride,
		VStride:  src.VStride,
	}
}

func codedSourceImageFromVP8(src *vp8common.Image) vp8enc.SourceImage {
	return vp8enc.SourceImage{
		Width:    src.CodedWidth,
		Height:   src.CodedHeight,
		UVWidth:  (src.Width + 1) >> 1,
		UVHeight: (src.Height + 1) >> 1,
		Y:        src.Y,
		U:        src.U,
		V:        src.V,
		YStride:  src.YStride,
		UStride:  src.UStride,
		VStride:  src.VStride,
	}
}

func sourceImageUVDimensions(src vp8enc.SourceImage) (int, int) {
	uvWidth := src.UVWidth
	uvHeight := src.UVHeight
	if uvWidth <= 0 {
		uvWidth = (src.Width + 1) >> 1
	}
	if uvHeight <= 0 {
		uvHeight = (src.Height + 1) >> 1
	}
	return uvWidth, uvHeight
}
