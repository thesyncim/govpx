//go:build amd64 && !purego

package dsp

// SSE2 port of the libvpx v1.16.0 vpx_dsp/x86/sad_sse2.asm SAD primitives,
// plus a govpx-specific 16x16 limit-aware variant matching the scalar
// limited-SAD semantics. PSADBW makes byte abs-diff + horizontal-sum a
// single instruction, so SAD is essentially load-bound.
//
// AVX2 entry points (sad_avx2_amd64.s) double the per-iteration row
// throughput by packing two 16-byte rows (or four 8-byte rows) into a
// single YMM and running VPSADBW on 32 bytes at a time.

//go:noescape
func sadBlock16x16SSE2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock16x16LimitSSE2(src *byte, srcStride int, ref *byte, refStride int, limit int32) int32

//go:noescape
func sadBlock16x16x4SSE2(src *byte, srcStride int, ref0 *byte, ref1 *byte, ref2 *byte, ref3 *byte, refStride int, out *[4]uint32)

//go:noescape
func sadBlock16x8SSE2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock8x16SSE2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock8x8SSE2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock4x4SSE2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock16x16AVX2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock16x16x4AVX2(src *byte, srcStride int, ref0 *byte, ref1 *byte, ref2 *byte, ref3 *byte, refStride int, out *[4]uint32)

//go:noescape
func sadBlock16x8AVX2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock8x16AVX2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock8x8AVX2(src *byte, srcStride int, ref *byte, refStride int) int32
