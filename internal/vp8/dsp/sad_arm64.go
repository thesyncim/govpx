//go:build arm64 && !purego

package dsp

// ARMv8 NEON port of the libvpx v1.16.0 vpx_dsp/arm/sad_neon.c 16x16
// SAD primitives. Two entry points:
//
//   sadBlock16x16NEON - full 16x16 SAD (no early exit).
//   sadBlock16x16LimitNEON - 16x16 SAD with a per-row early-exit
//     check; mirrors the scalar limited-SAD semantics so the
//     motion-search caller keeps its existing best-so-far pruning.

//go:noescape
func sadBlock16x16NEON(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock16x16LimitNEON(src *byte, srcStride int, ref *byte, refStride int, limit int32) int32

//go:noescape
func sadBlock16x16DotProd(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock16x16x4NEON(src *byte, srcStride int, ref0 *byte, ref1 *byte, ref2 *byte, ref3 *byte, refStride int, out *[4]uint32)

//go:noescape
func sadBlock16x16x4DotProd(src *byte, srcStride int, ref0 *byte, ref1 *byte, ref2 *byte, ref3 *byte, refStride int, out *[4]uint32)

//go:noescape
func sadBlock16x16x4LimitNEON(src *byte, srcStride int, ref0 *byte, ref1 *byte, ref2 *byte, ref3 *byte, refStride int, limits *[4]int32, out *[4]uint32)

//go:noescape
func sadBlock16x8NEON(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock16x8DotProd(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock8x16NEON(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock8x8NEON(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock4x4NEON(src *byte, srcStride int, ref *byte, refStride int) int32
