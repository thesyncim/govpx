// Package bitstream implements the VP9 boolean range coder.
//
// VP9 uses an arithmetic coder over a 257-state probability space, derived
// from the same family as VP8's bool coder but with separate end-of-stream
// rules, marker bits, and reference-tile reset semantics. This package
// provides matched reader and writer halves: the reader is used by the
// uncompressed header parser, the compressed header parser, the tile data
// parser, and the residual detokenizer; the writer is used by the
// encoder's frame-pack stage.
//
// The reader and writer types are designed to be allocation-free in
// steady state — both keep their backing buffer as a caller-owned slice
// and never grow internal storage past the constants in this package.
//
// Upstream:
//
//	libvpx v1.16.0 vpx_dsp/bitreader.{h,c}, vpx_dsp/bitwriter.{h,c}
package bitstream
