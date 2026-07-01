//go:build !arm64 || purego

package dsp

// Scalar-only builds: Avg8x8Quad falls through to four VpxAvg8x8 calls.

func avg8x8QuadAsm(src []uint8, off, stride int, out *[4]int32) bool {
	return false
}
