//go:build !goexperiment.simd || !arm64 || purego

package pixel

// TMPredict4Native retains libvpx v1.16.0 vpx_dsp/intrapred.c semantics
// through the existing generic pixel implementation.
func TMPredict4Native(dst []byte, ds int, above, left []byte, top byte, publicChecked bool) {
	if publicChecked {
		_ = above[3]
		_ = left[3]
		_ = dst[3*ds+3]
	}
	TMPredict(dst, ds, above, left, top, 4)
}
