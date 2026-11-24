//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package dsp

func inverseTransformGo(_ []int16, _ []byte, _ int, _ int, _ int, _ int) bool { return false }
func inverseWHTGo(_ []int16, _ []byte, _ int, _ bool) bool                    { return false }
