//go:build govpx_oracle_trace

package encoder

// Ported from libvpx v1.16.0.
func zeroUVTokenRecords(_ *TokenContextPlanes, _ *TokenContextPlanes, _ *MacroblockCoefficients, _ *coefficientTokenCounts, _ *InterCoefficientTokenRecords) bool {
	return false
}

// Empty Y1 token semantics follow libvpx v1.16.0 vp8/encoder/tokenize.c.
func zeroY1TokenRecords(_ *TokenContextPlanes, _ *TokenContextPlanes, _ *MacroblockCoefficients, _ *coefficientTokenCounts, _ *InterCoefficientTokenRecords, _, _ int) bool {
	return false
}
