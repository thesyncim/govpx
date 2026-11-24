package dsp

import "github.com/thesyncim/govpx/internal/pixel"

func avg8x8QuadAsm(src []uint8, off, stride int, out *[4]int32) bool {
	if !pixel.Enabled {
		return false
	}
	return pixel.AverageQuad16(src[off:], stride, out)
}
