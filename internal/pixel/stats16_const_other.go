//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

// Stats16x16Const computes (sum, sse) of (src - c) over a 16x16 byte block
// against a constant predictor byte c.
func Stats16x16Const(src []byte, ss int, c byte) (int, int) {
	sum, sse := 0, 0
	ci := int(c)
	for y := range 16 {
		row := src[y*ss : y*ss+16]
		for x := range 16 {
			d := int(row[x]) - ci
			sum += d
			sse += d * d
		}
	}
	return sum, sse
}
