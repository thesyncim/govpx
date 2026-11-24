//go:build goexperiment.simd && arm64 && !purego

package pixel

const NativeLFByAxis = true

// CanLoopFilterMacroHorizontal16 proves the complete sequential fused window.
func CanLoopFilterMacroHorizontal16(p []byte, pitch int, mblimit, blimit byte) bool {
	return Enabled && uint(pitch-16)>>30 == 0 && 19*pitch+16 <= len(p) && mblimit != 255 && blimit != 255
}

// LoopFilterMacroHorizontal16Unchecked requires a successful corresponding
// CanLoopFilterMacroHorizontal16 call on this unchanged slice, pitch and limits.
func LoopFilterMacroHorizontal16Unchecked(p []byte, pitch int, mblimit, blimit, limit, thresh byte) {
	loopFilterMacroInternalFusedHorizontal(p, pitch, int(mblimit), int(blimit), int(limit), int(thresh))
}

// CanLoopFilterMacroVertical16 proves the complete sequential fused window.
func CanLoopFilterMacroVertical16(p []byte, pitch int, mblimit, blimit byte) bool {
	return Enabled && uint(pitch-20)>>30 == 0 && 15*pitch+20 <= len(p) && mblimit != 255 && blimit != 255
}

// LoopFilterMacroVertical16Unchecked requires a successful corresponding
// CanLoopFilterMacroVertical16 call on this unchanged slice, pitch and limits.
func LoopFilterMacroVertical16Unchecked(p []byte, pitch int, mblimit, blimit, limit, thresh byte) {
	loopFilterMacroInternalFusedVertical(p, pitch, int(mblimit), int(blimit), int(limit), int(thresh))
}
