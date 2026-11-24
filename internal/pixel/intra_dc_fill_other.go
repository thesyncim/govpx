//go:build !goexperiment.simd || !arm64 || purego

package pixel

const IntraDCFillNative = false

func IntraDCFill(dst []byte, ds int, above, left []byte, n int, up, lf bool) bool { return false }
