package encoder

// Ported from libvpx v1.16.0 vp9/encoder/vp9_picklpf.c and
// vpx_dsp/psnr.c loop-filter scoring helpers.

// LoopFilterClamp mirrors libvpx vpx_ports/vpx_clamp.h clamp(value, low, high).
func LoopFilterClamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// LoopFilterRoundPowerOfTwo mirrors libvpx ROUND_POWER_OF_TWO.
func LoopFilterRoundPowerOfTwo(value, n int) int {
	shift := uint(n)
	bias := 1 << (shift - 1)
	return (value + bias) >> shift
}

// LoopFilterYSSE mirrors libvpx vpx_get_y_sse for 8-bit luma planes.
func LoopFilterYSSE(src []byte, srcStride int,
	recon []byte, reconStride int,
	width, height int,
) int64 {
	var sse int64
	for row := range height {
		srcRow := src[row*srcStride : row*srcStride+width]
		recRow := recon[row*reconStride : row*reconStride+width]
		for col := range width {
			d := int64(srcRow[col]) - int64(recRow[col])
			sse += d * d
		}
	}
	return sse
}
