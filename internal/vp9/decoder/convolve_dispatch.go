package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// VP9 subpel-convolve dispatch for inter prediction. Ported from
// libvpx v1.16.0 — the sf->predict[subpel_x != 0][subpel_y != 0][ref]
// table that inter_predictor in vp9/common/vp9_reconinter.h consults
// per 4×4 sub-block. The dispatch picks among:
//
//   [0][0]  : straight copy / avg (no subpel offset in either axis)
//   [1][0]  : horizontal-only 8-tap subpel
//   [0][1]  : vertical-only 8-tap subpel
//   [1][1]  : 2-D 8-tap subpel
//
// The `ref` dimension selects between fresh-write (ref==0) and
// dst-blend (ref==1, compound prediction averaging into dst). Keep the
// dispatch as direct calls rather than a function table: this is a hot
// reconstruction path, and table dispatch makes slice parameters escape.

// InterPredictor mirrors libvpx's inter_predictor inline helper.
// Dispatches by (subpel_x != 0, subpel_y != 0, ref), threading the
// per-frame InterpKernel `filter`, the (x0, x_step) / (y0, y_step) Q4 pair,
// and the (w, h) block size through unchanged.
func InterPredictor(
	src []byte, srcStride int, dst []byte, dstStride int,
	subpelX, subpelY int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	xStepQ4, yStepQ4, w, h int,
	ref int,
	srcOffset int,
) {
	InterPredictorWithScratch(src, srcStride, dst, dstStride, subpelX, subpelY,
		filter, xStepQ4, yStepQ4, w, h, ref, srcOffset, nil)
}

// InterPredictorWithScratch is InterPredictor with caller-owned convolve
// scratch. The scratch is used for 2-D subpel prediction and SIMD compound
// one-axis averaging paths; nil keeps the package-level fallback pool used by
// existing callers.
func InterPredictorWithScratch(
	src []byte, srcStride int, dst []byte, dstStride int,
	subpelX, subpelY int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	xStepQ4, yStepQ4, w, h int,
	ref int,
	srcOffset int,
	scratch *dsp.Convolve8Scratch,
) {
	key := ref
	if uint(ref) > 1 {
		panic("govpx/vp9/decoder: invalid inter predictor ref")
	}
	if subpelX != 0 || xStepQ4 != SubpelShifts {
		key |= 4
	}
	if subpelY != 0 || yStepQ4 != SubpelShifts {
		key |= 2
	}

	switch key {
	case 0:
		dsp.VpxConvolveCopy(src, srcStride, dst, dstStride, w, h, srcOffset)
	case 1:
		dsp.VpxConvolveAvg(src, srcStride, dst, dstStride, w, h, srcOffset)
	case 2:
		dsp.VpxConvolve8Vert(src, srcStride, dst, dstStride, filter,
			subpelX, xStepQ4, subpelY, yStepQ4, w, h, srcOffset)
	case 3:
		if scratch != nil {
			dsp.VpxConvolve8AvgVertWithScratch(src, srcStride, dst, dstStride,
				filter, subpelX, xStepQ4, subpelY, yStepQ4, w, h, srcOffset, scratch)
			return
		}
		dsp.VpxConvolve8AvgVert(src, srcStride, dst, dstStride, filter,
			subpelX, xStepQ4, subpelY, yStepQ4, w, h, srcOffset)
	case 4:
		dsp.VpxConvolve8Horiz(src, srcStride, dst, dstStride, filter,
			subpelX, xStepQ4, subpelY, yStepQ4, w, h, srcOffset)
	case 5:
		if scratch != nil {
			dsp.VpxConvolve8AvgHorizWithScratch(src, srcStride, dst, dstStride,
				filter, subpelX, xStepQ4, subpelY, yStepQ4, w, h, srcOffset, scratch)
			return
		}
		dsp.VpxConvolve8AvgHoriz(src, srcStride, dst, dstStride, filter,
			subpelX, xStepQ4, subpelY, yStepQ4, w, h, srcOffset)
	case 6:
		dsp.VpxConvolve8WithScratch(src, srcStride, dst, dstStride, filter,
			subpelX, xStepQ4, subpelY, yStepQ4, w, h, srcOffset, scratch)
	case 7:
		dsp.VpxConvolve8AvgWithScratch(src, srcStride, dst, dstStride, filter,
			subpelX, xStepQ4, subpelY, yStepQ4, w, h, srcOffset, scratch)
	}
}
