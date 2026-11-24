//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package dsp

func intraTM32Fixed(_ []byte, _ int, _, _ []byte, _ byte) bool     { return false }
func intraVFixed(_ []byte, _, _ int, _ []byte) bool                { return false }
func intraHFixed(_ []byte, _, _ int, _ []byte) bool                { return false }
func intraDCFixed(_ []byte, _, _ int, _, _ []byte, _, _ bool) bool { return false }

func inverseTransformGo(_ []int16, _ []byte, _ int, _ int, _ int, _ int) bool { return false }
func inverseWHTGo(_ []int16, _ []byte, _ int, _ bool) bool                    { return false }
