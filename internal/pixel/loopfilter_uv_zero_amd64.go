//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// Preserve the independently measured x64 filter network.
func uvRoundedZero(_ archsimd.Int8x16, _ int) bool { return false }
