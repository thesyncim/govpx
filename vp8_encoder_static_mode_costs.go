package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// Keyframe probabilities are fixed by VP8. Like libvpx's vp8_init_mode_costs,
// resolve each immutable tree once rather than walking it for every candidate.
var vp8StaticModeCosts = func() (out struct {
	b  [10][10][10]int
	y  [5]int
	uv [4]int
}) {
	for a := range 10 {
		for l := range 10 {
			for m := range 10 {
				out.b[a][l][m] = vp8enc.TreeTokenCost(vp8tables.BModeTree[:], vp8tables.KeyFrameBModeProbs[a][l][:], m)
			}
		}
	}
	for m := range out.y {
		out.y[m] = vp8enc.TreeTokenCost(vp8tables.KeyFrameYModeTree[:], vp8tables.KeyFrameYModeProbs[:], m)
	}
	for m := range out.uv {
		out.uv[m] = vp8enc.TreeTokenCost(vp8tables.UVModeTree[:], vp8tables.KeyFrameUVModeProbs[:], m)
	}
	return
}()
