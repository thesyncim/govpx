package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/testutil"
)

func appendVP9YCbCrI420(out []byte, img *image.YCbCr) []byte {
	return testutil.AppendYCbCrI420(out, img)
}

func appendVP9I420(out []byte, img Image) []byte {
	return testutil.AppendI420Planes(out, img.Width, img.Height,
		img.Y, img.YStride,
		img.U, img.UStride,
		img.V, img.VStride)
}
