// Package encoder holds VP9 profile 0 bitstream writers.
//
// It covers uncompressed headers, compressed headers, partition and mode
// writers, coefficient token packing, probability updates, cost helpers, and
// tile/frame packing. Rate control, full mode decision, motion search, and
// source-to-residual analysis live outside this package scope.
//
// Upstream:
//
//	libvpx v1.16.0 vp9/encoder/*.{c,h}
package encoder
