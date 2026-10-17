//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// LoopFilterMacroInternal16 applies a horizontal VP8 macroblock edge and its
// three internal edges in order to sixteen lanes. The vertical path remains
// separate. Unsupported or incomplete windows decline without writing.
func LoopFilterMacroInternal16(p []byte, step, outer, mblimit, blimit, limit, thresh int) bool {
	if !Enabled || step <= 0 || outer <= 0 || mblimit < 0 || mblimit >= 255 || blimit < 0 || blimit >= 255 || limit < 0 || limit > 255 || thresh < 0 || thresh > 255 {
		return false
	}
	if !(outer == 1 && step >= 16) || len(p) == 0 {
		return false
	}
	// Prove 19*step+15*outer < len(p) before either multiplication. Each
	// sixteen-byte row load/store stays inside this complete twenty-row window,
	// including unaligned addresses and the smallest accepted pitch.
	avail := len(p) - 1
	if step > avail/19 {
		return false
	}
	avail -= 19 * step
	if outer > avail/15 {
		return false
	}
	defer archsimd.ClearAVXUpperBits()
	loopFilterMacroInternalFusedHorizontal(p, step, mblimit, blimit, limit, thresh)
	return true
}
