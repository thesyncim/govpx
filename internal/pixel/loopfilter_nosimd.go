//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func LoopFilter(_ []byte, _ int, _ int, _ int, _ int, _ int, _ int, _ int, _ int) bool { return false }
func LoopFilterSimpleHorizontal16(_ []byte, _ int, _ byte) bool                      { return false }
func LoopFilterSimpleVertical16(_ []byte, _ int, _ byte) bool                        { return false }
