package pixel

// DiffMetrics computes SAD, signed residual sum and SSE in one block walk.
// Codec blocks up to 64x64 use vector accumulators; larger inputs retain the
// scalar contract. Input rows may have arbitrary strides and exact end lengths.
func DiffMetrics(src []byte, ss int, ref []byte, rs, w, h int) (sad, sum int, sse uint32) {
	if Enabled && w > 0 && h > 0 && w <= 64 && h <= 64 {
		return diffMetricsSIMD(src, ss, ref, rs, w, h)
	}
	for y := range h {
		for x := range w {
			d := int(src[y*ss+x]) - int(ref[y*rs+x])
			sad += max(d, -d)
			sum += d
			sse += uint32(d * d)
		}
	}
	return
}
