//go:build amd64

package dsp

// SSE2 port of the libvpx v1.16.0 vpx_dsp/x86/sad_sse2.asm SAD primitives,
// plus a govpx-specific 16x16 limit-aware variant matching the scalar
// sadBlockLimit semantics. PSADBW makes byte abs-diff + horizontal-sum a
// single instruction, so SAD is essentially load-bound.

//go:noescape
func sadBlock16x16SSE2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock16x16LimitSSE2(src *byte, srcStride int, ref *byte, refStride int, limit int32) int32

//go:noescape
func sadBlock16x8SSE2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock8x16SSE2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock8x8SSE2(src *byte, srcStride int, ref *byte, refStride int) int32

//go:noescape
func sadBlock4x4SSE2(src *byte, srcStride int, ref *byte, refStride int) int32
