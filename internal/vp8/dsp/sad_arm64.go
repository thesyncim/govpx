//go:build arm64

package dsp

// ARMv8 NEON port of the libvpx v1.16.0 vpx_dsp/arm/sad_neon.c 16x16
// SAD primitives. Two entry points:
//
//   sadBlock16x16NEON - full 16x16 SAD (no early exit).
//   sadBlock16x16LimitNEON - 16x16 SAD with a per-row early-exit
//     check; mirrors the scalar sadBlockLimit's semantics so the
//     motion-search caller keeps its existing best-so-far pruning.

//go:noescape
func sadBlock16x16NEON(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock16x16LimitNEON(src *byte, srcStride int, ref *byte, refStride int, limit int32) int32
