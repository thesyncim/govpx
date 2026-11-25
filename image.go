package govpx

import "github.com/thesyncim/govpx/internal/vpx/codecapi"

// ChromaSubsampling identifies the chroma plane subsampling of an [Image].
type ChromaSubsampling = codecapi.ChromaSubsampling

const (
	Subsampling420 = codecapi.Subsampling420
	Subsampling422 = codecapi.Subsampling422
	Subsampling440 = codecapi.Subsampling440
	Subsampling444 = codecapi.Subsampling444
)

// Image is an 8-bit planar YUV/RGB image (4:2:0 by default).
type Image = codecapi.Image
