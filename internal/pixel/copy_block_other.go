//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

// CopyBlock copies a w x h byte block from src (stride ss) to dst (stride ds).
func CopyBlock(src []byte, ss int, dst []byte, ds, w, h int) {
	for y := range h {
		copy(dst[y*ds:y*ds+w], src[y*ss:y*ss+w])
	}
}
