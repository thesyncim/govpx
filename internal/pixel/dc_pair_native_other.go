//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

// DCPairNative is false outside the native SIMD selection; public DSP retains
// its original libvpx v1.16.0 idctllm.c dispatch and scalar fallback.
const DCPairNative = false

func DCPairInt32Native(c0, c1 int32, pred []byte, ps int, dst []byte, ds int) {
	dcPairNativeScalar(c0, c1, pred, ps, dst, ds)
}

// DCPairInPlaceInt32Native is the canonical disabled-DSP fallback outside
// the native SIMD selection of libvpx v1.16.0's paired DC operation.
func DCPairInPlaceInt32Native(c0, c1 int32, b []byte, stride int) {
	dcPairNativeScalar(c0, c1, b, stride, b, stride)
}
