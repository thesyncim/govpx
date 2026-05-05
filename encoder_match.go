package libgopx

import vp8common "github.com/thesyncim/libgopx/internal/vp8/common"

func sourceMatchesReference(src Image, ref *vp8common.Image) bool {
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
		for col := 0; col < width; col++ {
			if aRow[col] != bRow[col] {
				return false
			}
		}
	}
	return true
}
