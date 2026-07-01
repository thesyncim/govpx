//go:build arm64 && !purego

package dsp

import "unsafe"

//go:noescape
func avg8x8QuadNEON(src *byte, stride int, out *[4]int32)

// avg8x8QuadAsm validates the 16x16 read window and dispatches to the
// NEON kernel. The kernel accumulates each 16-byte row with
// UADDLP/UADALP (libvpx vpx_avg_8x8_neon uses VADDL/VADDW chains; the
// pairwise form sums the same 64 bytes per sub-block) and applies the
// identical (sum + 32) >> 6 rounding.
func avg8x8QuadAsm(src []uint8, off, stride int, out *[4]int32) bool {
	if off < 0 || stride < 16 {
		return false
	}
	end := off + 15*stride + 16
	if end < off || end > len(src) {
		return false
	}
	avg8x8QuadNEON(unsafe.SliceData(src[off:]), stride, out)
	return true
}
