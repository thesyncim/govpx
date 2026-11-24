//go:build !goexperiment.simd || !arm64 || purego

package pixel

func Intra4x4DCFill(dst []byte, ds int, above, left []byte) bool { return false }
