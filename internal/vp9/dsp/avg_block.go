package dsp

// 8x8 block-average helpers for the VP9 variance-partition picker.
// Ported from libvpx v1.16.0 vpx_dsp/avg.c:
//
//	unsigned int vpx_avg_8x8_c(const uint8_t *s, int p) {
//	  int i, j;
//	  int sum = 0;
//	  for (i = 0; i < 8; ++i, s += p)
//	    for (j = 0; j < 8; sum += s[j], ++j) {
//	    }
//	  return (sum + 32) >> 6;
//	}
//
// fill_variance_8x8avg (vp9/encoder/vp9_encodeframe.c:750-784) calls
// vpx_avg_8x8 once per 8x8 sub-block of a 16x16 region, for the source
// and (on inter frames) the predictor. Avg8x8Quad batches the four
// sub-block averages of one fully-in-frame 16x16 region into a single
// call so the arm64 path can run them in one NEON kernel
// (vpx_dsp/arm/avg_neon.c vpx_avg_8x8_neon arithmetic per sub-block).

// VpxAvg8x8 mirrors vpx_avg_8x8_c for one 8x8 block at src[off:].
func VpxAvg8x8(src []uint8, off, stride int) int {
	sum := 0
	for r := range 8 {
		row := src[off+r*stride:]
		_ = row[7]
		sum += int(row[0]) + int(row[1]) + int(row[2]) + int(row[3]) +
			int(row[4]) + int(row[5]) + int(row[6]) + int(row[7])
	}
	return (sum + 32) >> 6
}

// Avg8x8Quad computes the four 8x8 rounded averages of the 16x16
// region at src[off:], in fill_variance_8x8avg's k order:
//
//	out[0] = (0,0)  out[1] = (8,0)  out[2] = (0,8)  out[3] = (8,8)
//
// The region must be fully inside the buffer; the caller handles the
// clamped frame-edge cases.
func Avg8x8Quad(src []uint8, off, stride int, out *[4]int32) {
	if avg8x8QuadAsm(src, off, stride, out) {
		return
	}
	out[0] = int32(VpxAvg8x8(src, off, stride))
	out[1] = int32(VpxAvg8x8(src, off+8, stride))
	out[2] = int32(VpxAvg8x8(src, off+8*stride, stride))
	out[3] = int32(VpxAvg8x8(src, off+8*stride+8, stride))
}
