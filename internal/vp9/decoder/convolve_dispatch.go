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
// dst-blend (ref==1, compound prediction averaging into dst).

// ConvolveFn matches libvpx's convolve_fn_t signature, adapted for
// the Go DSP package's pre-offset src convention.
type ConvolveFn func(
	src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
)

// ConvolveCopyAdapter wraps VpxConvolveCopy / VpxConvolveAvg behind
// the ConvolveFn signature so the dispatch table can index them
// uniformly. The filter / step arguments are ignored.
func ConvolveCopyAdapter(
	src []byte, srcStride int, dst []byte, dstStride int,
	_ *[tables.SubpelShifts][tables.SubpelTaps]int16,
	_, _, _, _, w, h, srcOffset int,
) {
	dsp.VpxConvolveCopy(src, srcStride, dst, dstStride, w, h, srcOffset)
}

// ConvolveAvgAdapter wraps VpxConvolveAvg.
func ConvolveAvgAdapter(
	src []byte, srcStride int, dst []byte, dstStride int,
	_ *[tables.SubpelShifts][tables.SubpelTaps]int16,
	_, _, _, _, w, h, srcOffset int,
) {
	dsp.VpxConvolveAvg(src, srcStride, dst, dstStride, w, h, srcOffset)
}

// PredictTable mirrors libvpx's scale_factors.predict[2][2][2] —
// indexed by (hasHorizSubpel, hasVertSubpel, isAvg).
var PredictTable = [2][2][2]ConvolveFn{
	{ // hasHoriz=0
		{ // hasVert=0
			ConvolveCopyAdapter, // ref=0: fresh copy
			ConvolveAvgAdapter,  // ref=1: blend with dst
		},
		{ // hasVert=1
			dsp.VpxConvolve8Vert,
			dsp.VpxConvolve8AvgVert,
		},
	},
	{ // hasHoriz=1
		{ // hasVert=0
			dsp.VpxConvolve8Horiz,
			dsp.VpxConvolve8AvgHoriz,
		},
		{ // hasVert=1
			dsp.VpxConvolve8,
			dsp.VpxConvolve8Avg,
		},
	},
}

// InterPredictor mirrors libvpx's inter_predictor inline helper.
// Looks up PredictTable by (subpel_x != 0, subpel_y != 0, ref) and
// dispatches the convolve, threading the per-frame InterpKernel
// `filter`, the (x0, x_step) / (y0, y_step) Q4 pair, and the (w, h)
// block size through unchanged.
func InterPredictor(
	src []byte, srcStride int, dst []byte, dstStride int,
	subpelX, subpelY int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	xStepQ4, yStepQ4, w, h int,
	ref int,
	srcOffset int,
) {
	hx := 0
	if subpelX != 0 || xStepQ4 != SubpelShifts {
		hx = 1
	}
	hy := 0
	if subpelY != 0 || yStepQ4 != SubpelShifts {
		hy = 1
	}
	PredictTable[hx][hy][ref](src, srcStride, dst, dstStride, filter,
		subpelX, xStepQ4, subpelY, yStepQ4, w, h, srcOffset)
}
