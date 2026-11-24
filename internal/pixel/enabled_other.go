//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

const Enabled = false
