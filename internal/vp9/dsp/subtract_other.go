package dsp

import "github.com/thesyncim/govpx/internal/pixel"

func subtractBlockNonZeroFast(src []uint8, srcOff, srcStride int,
	pred []uint8, predOff, predStride int,
	out []int16, outOff, outStride int,
	w, h int,
) (bool, bool) {
	return pixel.Subtract(src[srcOff:], srcStride, pred[predOff:], predStride, out[outOff:], outStride, w, h), true
}
