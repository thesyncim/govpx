package gopvx

import (
	"bytes"

	vp8common "github.com/thesyncim/gopvx/internal/vp8/common"
	vp8enc "github.com/thesyncim/gopvx/internal/vp8/encoder"
)

func sourceMatchesReference(src Image, ref *vp8common.Image) bool {
	return sourceImageMatchesReference(vp8enc.SourceImage{
		Width:   src.Width,
		Height:  src.Height,
		Y:       src.Y,
		U:       src.U,
		V:       src.V,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}, ref)
}

func sourceImageMatchesReference(src vp8enc.SourceImage, ref *vp8common.Image) bool {
	if ref == nil || src.Width != ref.Width || src.Height != ref.Height {
		return false
	}
	if !planeMatches(src.Y, src.YStride, ref.Y, ref.YStride, src.Width, src.Height) {
		return false
	}
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	return planeMatches(src.U, src.UStride, ref.U, ref.UStride, uvWidth, uvHeight) &&
		planeMatches(src.V, src.VStride, ref.V, ref.VStride, uvWidth, uvHeight)
}

func planeMatches(a []byte, aStride int, b []byte, bStride int, width int, height int) bool {
	for row := 0; row < height; row++ {
		aRow := a[row*aStride : row*aStride+width]
		bRow := b[row*bStride : row*bStride+width]
		if !bytes.Equal(aRow, bRow) {
			return false
		}
	}
	return true
}
