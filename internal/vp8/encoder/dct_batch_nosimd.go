//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

// Batched dispatch retains the libvpx v1.16.0 scalar/per-block contract.
package encoder

func forwardDCT4BatchGo(_ []int16, _ []int16, _ int) int          { return 0 }
func forwardDCT4BatchResidualsGo(_ []int16, _ []int16, _ int) int { return 0 }
