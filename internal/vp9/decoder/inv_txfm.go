package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/dsp"
)

// VP9 inverse-transform dispatchers. Ported from libvpx v1.16.0
// vp9/common/vp9_idct.c — vp9_idct{4x4,8x8,16x16,32x32}_add,
// vp9_iwht4x4_add, and the four vp9_iht{4x4,8x8,16x16}_add wrappers.
//
// Each function takes the dequantized coefficient block, picks the
// fast/slow DSP kernel by EOB, and writes the reconstructed pixels
// over the predictor block in-place (add). The dispatcher operates
// on the 8-bit profile.

// Idct4x4Add mirrors libvpx's vp9_idct4x4_add. EOB > 1 selects the
// full 16-coeff kernel; EOB == 1 picks the DC-only fast path.
func Idct4x4Add(input []int16, dest []uint8, stride, eob int) {
	if eob > 1 {
		dsp.Idct4x4_16Add(input, dest, stride)
		return
	}
	dsp.Idct4x4_1Add(input, dest, stride)
}

// Iwht4x4Add mirrors libvpx's vp9_iwht4x4_add (lossless path).
func Iwht4x4Add(input []int16, dest []uint8, stride, eob int) {
	if eob > 1 {
		dsp.Iwht4x4_16Add(input, dest, stride)
		return
	}
	dsp.Iwht4x4_1Add(input, dest, stride)
}

// Idct8x8Add mirrors libvpx's vp9_idct8x8_add. EOB partitions:
//
//	eob == 1: DC-only fast path.
//	eob <= 12: upper-left 4x4 dense fast path.
//	default: full 64-coeff kernel.
func Idct8x8Add(input []int16, dest []uint8, stride, eob int) {
	switch {
	case eob == 1:
		dsp.Idct8x8_1Add(input, dest, stride)
	case eob <= 12:
		dsp.Idct8x8_12Add(input, dest, stride)
	default:
		dsp.Idct8x8_64Add(input, dest, stride)
	}
}

// Idct16x16Add mirrors libvpx's vp9_idct16x16_add. Four EOB tiers
// (1 / 10 / 38 / 256) pick progressively denser kernels.
func Idct16x16Add(input []int16, dest []uint8, stride, eob int) {
	switch {
	case eob == 1:
		dsp.Idct16x16_1Add(input, dest, stride)
	case eob <= 10:
		dsp.Idct16x16_10Add(input, dest, stride)
	case eob <= 38:
		dsp.Idct16x16_38Add(input, dest, stride)
	default:
		dsp.Idct16x16_256Add(input, dest, stride)
	}
}

// Idct32x32Add mirrors libvpx's vp9_idct32x32_add. EOB tiers
// (1 / 34 / 135 / 1024).
func Idct32x32Add(input []int16, dest []uint8, stride, eob int) {
	switch {
	case eob == 1:
		dsp.Idct32x32_1Add(input, dest, stride)
	case eob <= 34:
		dsp.Idct32x32_34Add(input, dest, stride)
	case eob <= 135:
		dsp.Idct32x32_135Add(input, dest, stride)
	default:
		dsp.Idct32x32_1024Add(input, dest, stride)
	}
}

// Iht4x4Add mirrors libvpx's vp9_iht4x4_add. The DCT_DCT case falls
// back to the EOB-cascaded Idct dispatcher; every other tx_type
// drives the full 16-coeff iht kernel directly (libvpx doesn't have
// EOB-tiered fast paths for the hybrid transforms).
func Iht4x4Add(txType common.TxType, input []int16, dest []uint8, stride, eob int) {
	if txType == common.DctDct {
		Idct4x4Add(input, dest, stride, eob)
		return
	}
	dsp.Iht4x4_16Add(input, dest, stride, int(txType))
}

// Iht8x8Add mirrors libvpx's vp9_iht8x8_add.
func Iht8x8Add(txType common.TxType, input []int16, dest []uint8, stride, eob int) {
	if txType == common.DctDct {
		Idct8x8Add(input, dest, stride, eob)
		return
	}
	dsp.Iht8x8_64Add(input, dest, stride, int(txType))
}

// Iht16x16Add mirrors libvpx's vp9_iht16x16_add.
func Iht16x16Add(txType common.TxType, input []int16, dest []uint8, stride, eob int) {
	if txType == common.DctDct {
		Idct16x16Add(input, dest, stride, eob)
		return
	}
	dsp.Iht16x16_256Add(input, dest, stride, int(txType))
}

// InverseTransformBlock mirrors libvpx's inverse_transform_block_inter
// dispatch — the top-level call out of the reconstruct path. Picks
// the right DCT (lossless WHT for 4x4 + lossless) or hybrid IHT
// based on (txSize, txType) and the lossless flag.
//
// Caller supplies the per-block parameters; the dispatcher writes
// the residual on top of `dest`.
func InverseTransformBlock(input []int16, dest []uint8, stride int,
	txSize common.TxSize, txType common.TxType, eob int, lossless bool,
) {
	if lossless && txSize == common.Tx4x4 {
		Iwht4x4Add(input, dest, stride, eob)
		return
	}
	switch txSize {
	case common.Tx4x4:
		Iht4x4Add(txType, input, dest, stride, eob)
	case common.Tx8x8:
		Iht8x8Add(txType, input, dest, stride, eob)
	case common.Tx16x16:
		Iht16x16Add(txType, input, dest, stride, eob)
	case common.Tx32x32:
		Idct32x32Add(input, dest, stride, eob)
	}
}
