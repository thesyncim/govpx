// Package pixel supplies allocation-free pixel kernels shared by VP8 and VP9.
// Go 1.27 archsimd implementations are selected with GOEXPERIMENT=simd;
// purego and other architectures use the independent scalar implementations.
package pixel
