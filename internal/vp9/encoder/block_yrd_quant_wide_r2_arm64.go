//go:build goexperiment.simd && arm64 && !purego

package encoder

func blockYrdQuantWideR2(_ []int16, _ int, _, _, _ [2]int16) (int, uint32, bool) { return 0, 0, false }
