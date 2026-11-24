//go:build goexperiment.simd && arm64 && !purego

package pixel

// TMPredictBytes retains the checked direct-call contract of the libvpx
// v1.16.0 vpx_dsp/intrapred.c true-motion byte path. Declines read no input and
// write no output. TMPredictNative owns the single arithmetic implementation,
// including sequential per-row left capture when left aliases destination.
func TMPredictBytes(dst []byte, ds int, above, left []byte, top byte, w int) bool {
	if !Enabled || (w != 8 && w != 16) || uint(ds)>>30 != 0 {
		return false
	}
	span := (w-1)*ds + w
	if span > len(dst) || len(above) < w || len(left) < w {
		return false
	}
	if slicesOverlap(dst, above, span, w) {
		return false
	}
	TMPredictNative(dst, ds, above, left, top, w)
	return true
}
