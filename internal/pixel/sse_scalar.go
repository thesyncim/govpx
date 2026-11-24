package pixel

// Squared-error-only references retain the codec's modulo-2^32 result.
func SSE16Scalar(src []byte, ss int, ref []byte, rs, h int) (sse uint32) {
	for y := range h {
		for x := range 16 {
			d := int(src[y*ss+x]) - int(ref[y*rs+x])
			sse += uint32(d * d)
		}
	}
	return
}
func SSE8Scalar(src []byte, ss int, ref []byte, rs, h int) (sse uint32) {
	for y := range h {
		for x := range 8 {
			d := int(src[y*ss+x]) - int(ref[y*rs+x])
			sse += uint32(d * d)
		}
	}
	return
}
