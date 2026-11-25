package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vpx/codecapi"
)

// Image is an I420/YV12-style planar 8-bit 4:2:0 image.
type Image = codecapi.Image

func publicImageFromVP8(src *vp8common.Image) Image {
	return codecapi.PublicImageFromVP8(src)
}
