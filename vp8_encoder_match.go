package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func sourceMatchesReference(src Image, ref *vp8common.Image) bool {
	return vp8enc.SourceImageMatchesReference(sourceImageFromImage(src), ref)
}
