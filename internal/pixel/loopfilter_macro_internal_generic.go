//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

// LoopFilterMacroInternal16 leaves unsupported targets to the separate edge paths.
func LoopFilterMacroInternal16(p []byte, step, outer, mblimit, blimit, limit, thresh int) bool {
	return false
}
