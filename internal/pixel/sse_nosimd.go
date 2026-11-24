//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func SSE16(src []byte, ss int, ref []byte, rs, h int) uint32 { return SSE16Scalar(src, ss, ref, rs, h) }
func SSE8(src []byte, ss int, ref []byte, rs, h int) uint32  { return SSE8Scalar(src, ss, ref, rs, h) }
