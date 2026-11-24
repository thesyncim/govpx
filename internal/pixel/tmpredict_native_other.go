//go:build !goexperiment.simd || !arm64 || purego

package pixel

const TMNative = false

// TMPredictNative preserves the original generic pixel semantics on other builds.
func TMPredictNative(dst []byte, ds int, above, left []byte, top byte, w int) {
	TMPredict(dst, ds, above, left, top, w)
}
