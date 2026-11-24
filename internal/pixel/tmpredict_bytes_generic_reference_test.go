//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func tmBytesPrior(dst []byte, ds int, above, left []byte, top byte, w int) {
	for y := range w {
		for x := range w {
			dst[y*ds+x] = byte(min(max(int(above[x])+int(left[y])-int(top), 0), 255))
		}
	}
}
