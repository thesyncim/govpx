//go:build !goexperiment.simd || !arm64 || purego

package encoder

// Backend selection preserves libvpx v1.16.0-compatible token semantics.

const denseTokenAvailable = false

func denseTokenPrepack(q *[16]int16, out *[16]uint32) bool { return false }
