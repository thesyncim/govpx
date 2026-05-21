package encoder

import (
	"bytes"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

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
	if ref == nil || src.Width != ref.Width || src.Height != ref.Height {
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
	for row := range height {
		aRow := a[row*aStride : row*aStride+width]
		bRow := b[row*bStride : row*bStride+width]
		if !bytes.Equal(aRow, bRow) {
			return false
		}
	}
	return true
}
