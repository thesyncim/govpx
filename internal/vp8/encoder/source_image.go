package encoder

import (
	"bytes"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// Ported from libvpx v1.16.0 vp8/encoder/lookahead.c
// vp8_lookahead_push and vp8/common/extend.c
// vp8_copy_and_extend_frame / vp8_copy_and_extend_frame_with_rect.

// CopySourceToFrameBufferActive performs the active-map-aware partial frame
// copy from libvpx vp8_lookahead_push. activeMap is a row-major mb_rows*mb_cols
// array; non-zero cells mark active macroblocks. For each active run within a
// row, a 16-pixel-tall band of luma plus the colocated chroma is copied from
// src into dst; inactive macroblocks retain whatever the destination buffer
// already held. Border extension follows the full copy path.
func CopySourceToFrameBufferActive(dst *vp8common.FrameBuffer, src SourceImage, activeMap []uint8, mbRows int, mbCols int) {
	if len(activeMap) < mbRows*mbCols {
		CopySourceToFrameBuffer(dst, src)
		return
	}
	for row := range mbRows {
		col := 0
		for col < mbCols {
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
	PadFrameVisibleToCoded(&dst.Img)
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

// CopySourceToFrameBuffer copies the visible source image into dst, pads the
// 16-aligned coded area from the visible edge, then extends VP8 reference
// borders.
func CopySourceToFrameBuffer(dst *vp8common.FrameBuffer, src SourceImage) {
	buffers.CopyPlane(dst.Img.Y, dst.Img.YStride, src.Y, src.YStride, src.Width, src.Height)
	buffers.CopyPlane(dst.Img.U, dst.Img.UStride, src.U, src.UStride, (src.Width+1)>>1, (src.Height+1)>>1)
	buffers.CopyPlane(dst.Img.V, dst.Img.VStride, src.V, src.VStride, (src.Width+1)>>1, (src.Height+1)>>1)
	PadFrameVisibleToCoded(&dst.Img)
	dst.ExtendBorders()
}

// PadFrameVisibleToCoded replicates visible right and bottom edges into the
// coded-but-invisible area before reference-border extension.
func PadFrameVisibleToCoded(img *vp8common.Image) {
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

// Source image views follow libvpx v1.16.0 VP8 YV12_BUFFER_CONFIG /
// lookahead semantics: luma carries visible and coded geometry, while
// chroma dimensions are derived from the visible 4:2:0 source.

// SourceImageFromImage exposes the visible region of a VP8 frame as an encoder
// source view.
func SourceImageFromImage(src *vp8common.Image) SourceImage {
	return SourceImage{
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

// CodedSourceImageFromImage exposes the coded region of a VP8 frame as an
// encoder source view while keeping chroma dimensions tied to the visible
// source size.
func CodedSourceImageFromImage(src *vp8common.Image) SourceImage {
	return SourceImage{
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

// SourceImageUVDimensions returns explicit chroma dimensions when present, or
// the VP8 4:2:0 visible dimensions derived from the luma size.
func SourceImageUVDimensions(src SourceImage) (int, int) {
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

// GatherClampedLumaBlock copies a visible-source luma block into dst, clamping
// reads at the source edges. This mirrors the VP8 encoder's libvpx v1.16.0
// visible-edge behavior for partial macroblocks before SAD/variance probes.
func GatherClampedLumaBlock(src SourceImage, baseY int, baseX int, width int, height int, dst []byte, dstStride int) {
	if min(width, height) <= 0 || src.Width <= 0 || src.Height <= 0 {
		return
	}
	srcY := src.Y
	srcStride := src.YStride
	fullX := baseX >= 0 && baseX+width <= src.Width
	var srcXs [16]int
	precomputedX := !fullX && width <= len(srcXs)
	if precomputedX {
		for col := range width {
			srcXs[col] = clampEncodeCoord(baseX+col, src.Width)
		}
	}
	for row := range height {
		y := clampEncodeCoord(baseY+row, src.Height)
		dstRow := row * dstStride
		srcRow := y * srcStride
		if fullX {
			copy(dst[dstRow:dstRow+width], srcY[srcRow+baseX:srcRow+baseX+width])
			continue
		}
		if precomputedX {
			for col := range width {
				dst[dstRow+col] = srcY[srcRow+srcXs[col]]
			}
		} else {
			for col := range width {
				srcX := clampEncodeCoord(baseX+col, src.Width)
				dst[dstRow+col] = srcY[srcRow+srcX]
			}
		}
	}
}

// SourceImageMatchesReference reports whether the visible source samples match
// the visible reference frame samples.
func SourceImageMatchesReference(src SourceImage, ref *vp8common.Image) bool {
	if ref == nil || src.Width <= 0 || src.Height <= 0 || src.Width != ref.Width || src.Height != ref.Height {
		return false
	}
	if !planeMatches(src.Y, src.YStride, ref.Y, ref.YStride, src.Width, src.Height) {
		return false
	}
	uvWidth, uvHeight := SourceImageUVDimensions(src)
	return planeMatches(src.U, src.UStride, ref.U, ref.UStride, uvWidth, uvHeight) &&
		planeMatches(src.V, src.VStride, ref.V, ref.VStride, uvWidth, uvHeight)
}

func planeMatches(a []byte, aStride int, b []byte, bStride int, width int, height int) bool {
	if !planeHasVisibleRows(a, aStride, width, height) || !planeHasVisibleRows(b, bStride, width, height) {
		return false
	}
	if width > 0 && height > 0 && aStride == width && bStride == width {
		return bytes.Equal(a[:width*height], b[:width*height])
	}
	for row := range height {
		aRow := a[row*aStride : row*aStride+width]
		bRow := b[row*bStride : row*bStride+width]
		if !bytes.Equal(aRow, bRow) {
			return false
		}
	}
	return true
}

func planeHasVisibleRows(plane []byte, stride int, width int, height int) bool {
	if width < 0 || height < 0 || stride < width {
		return false
	}
	if height == 0 {
		return true
	}
	if height > 1 && stride > (int(^uint(0)>>1)-width)/(height-1) {
		return false
	}
	need := (height-1)*stride + width
	return need <= len(plane)
}
