//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func Copy8Rows(src []byte, ss int, dst []byte, ds int) bool { return false }
