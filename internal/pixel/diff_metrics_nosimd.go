//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func diffMetricsSIMD(src []byte, ss int, ref []byte, rs, w, h int) (int, int, uint32) {
	panic("SIMD backend unavailable")
}
