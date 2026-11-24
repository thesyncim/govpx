//go:build !goexperiment.simd || !arm64 || purego

package pixel

func TMPredictBytes(dst []byte, ds int, above, left []byte, top byte, w int) bool { return false }
