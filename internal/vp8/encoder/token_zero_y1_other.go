//go:build !goexperiment.simd || !arm64 || purego || govpx_oracle_trace

// Empty Y1 token semantics follow libvpx v1.16.0 vp8/encoder/tokenize.c.
package encoder

func zeroY1TokenRecords(_ *TokenContextPlanes, _ *TokenContextPlanes, _ *MacroblockCoefficients, _ *coefficientTokenCounts, _ *InterCoefficientTokenRecords, _, _ int) bool {
	return false
}
