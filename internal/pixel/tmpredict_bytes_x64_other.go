//go:build !goexperiment.simd || !amd64 || purego

package pixel

func tmpredictBytesX64(dst []byte, ds int, above, left []byte, top byte, w int) bool { return false }
