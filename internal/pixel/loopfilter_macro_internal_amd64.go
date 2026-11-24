//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// LoopFilterMacroInternal16 applies a VP8 macroblock edge and its three
// internal edges in order to sixteen lanes, carrying modified neighbors.
// Unsupported or incomplete windows decline without writing.
func LoopFilterMacroInternal16(p []byte, step, outer, mblimit, blimit, limit, thresh int) bool {
	if !Enabled || step <= 0 || outer <= 0 || mblimit < 0 || mblimit >= 255 || blimit < 0 || blimit >= 255 || limit < 0 || limit > 255 || thresh < 0 || thresh > 255 {
		return false
	}
	if !((outer == 1 && step >= 16) || (step == 1 && outer >= 20)) || len(p) == 0 {
		return false
	}
	// Prove 19*step+15*outer < len(p) before either multiplication. Each
	// exact row/column load/store stays inside this complete 20x16 window,
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
	if outer == 1 {
		loopFilterMacroInternalFusedHorizontal(p, step, mblimit, blimit, limit, thresh)
	} else {
		loopFilterMacroInternalFusedVertical(p, outer, mblimit, blimit, limit, thresh)
	}
	return true
}
