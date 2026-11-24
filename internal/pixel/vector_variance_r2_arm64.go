//go:build goexperiment.simd && arm64 && !purego

package pixel

func vectorVarianceWideR2(_, _ []int16, _, _ int) (int, bool) { return 0, false }
