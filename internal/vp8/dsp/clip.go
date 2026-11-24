package dsp

import "math/bits"

// Ported from libvpx v1.16.0 vpx_dsp/vpx_dsp_common.h.

// signShift is the right-shift required to splat an int's sign bit across
// every bit position (i.e. produce -1 for negatives, 0 for non-negatives).
const signShift = bits.UintSize - 1

// ClipPixel saturates v into [0, 255] without conditional branches so the
// caller's inner loop keeps a straight-line shape. Compiles to a couple of
// shifts, an AND-NOT, and an OR on amd64/arm64 and stays well under the
// inliner's budget.
func ClipPixel(v int) uint8 {
	// nMask is all-ones when v < 0, zero otherwise; clearing those bits
	// snaps any negative input to 0.
	nMask := v >> signShift
	v &^= nMask
	// pMask is all-ones when v > 255 (because then 255 - v is negative);
	// in that case we OR in 255 and clear the high bits we computed.
	pMask := (255 - v) >> signShift
	return uint8((v &^ pMask) | (255 & pMask))
}

// ClipPixelAdd is the per-pixel add-and-saturate used by IDCT/intra/inter
// reconstruction. Folding the add into the clip lets the compiler keep the
// whole operation in registers.
func ClipPixelAdd(dst uint8, delta int) uint8 {
	return ClipPixel(int(dst) + delta)
}

// Clamp restricts v to [low, high]. Uses the language builtins so the
// compiler can emit CMOV/CSEL instead of branches.
func Clamp(v int, low int, high int) int {
	return min(max(v, low), high)
}
