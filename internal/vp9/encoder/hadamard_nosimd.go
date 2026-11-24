//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package encoder

func hadamardGo(_ []int16, _ int, _ []int16, _ int) bool { return false }
func forwardWHTGo(_ []int16, _ int, _ []int16) bool      { return false }

func hadamardBlockYrdGo(_ []int16, _ int, _ []int16, _ int) bool { return false }
