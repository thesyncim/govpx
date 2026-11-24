//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func Gather16Fast(_ []byte, _ int, _ []byte, _ int, _ []int16) bool { return false }
func Gather8Fast(_ []byte, _ int, _ []byte, _ int, _ []int16) bool  { return false }
