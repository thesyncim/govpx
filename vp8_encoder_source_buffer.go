package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

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
