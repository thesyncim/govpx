package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"math/bits"
)

func intProRowStripsAsm(hbuf []int16, ref []byte, off, stride, height, strips int) bool {
	norm := height >> 1
	if !pixel.Enabled || height < 2 || height > 64 || norm&(norm-1) != 0 {
		return false
	}
	for i := range strips {
		pixel.ProjectionRow(ref[off+i*16:], stride, height, hbuf[i*16:], uint64(bits.TrailingZeros(uint(norm))))
	}
	return true
}
func intProColsAsm(vbuf []int16, ref []byte, off, stride, width, rows, norm int) bool {
	if pixel.ProjectionCols16(ref[off:], stride, width, rows, norm, vbuf) {
		return true
	}
	if !pixel.Enabled {
		return false
	}
	for y := range rows {
		vbuf[y] = int16(pixel.Sum(ref[off+y*stride:], width, width, 1)) >> uint(norm)
	}
	return true
}
func vectorVarAsm(ref, src []int16, bwl int) (int, bool) {
	if !pixel.Enabled {
		return 0, false
	}
	return pixel.VectorVariance(ref, src, 4<<bwl, bwl+2), true
}
