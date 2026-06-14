//go:build amd64 && !purego

package dsp

// libvpx v1.16.0 vpx_dsp/x86/sad_sse2.asm-style dispatch wrappers. SSE2 is
// part of the x86-64 baseline so the SIMD entry points are always safe to
// call without runtime detection. AVX2 entry points are gated by
// internal/cpu.HasAVX2 — when present, the 16-wide and 8-wide SADs route
// through the YMM kernels in sad_avx2_amd64.s for ~2x throughput.
//
// The wrappers pull the slice base pointers via unsafe.SliceData so the
// dispatch stays inlineable and free of the runtime.panicBounds + stack
// frame the compiler emits for &src[0] / &ref[0]. Callers in the motion
// search hot path (vp8_encoder_reconstruct.go) always pass non-empty slices
// shaped to cover the read window, matching the implicit contract of
// the underlying SSE2/AVX2 kernels.

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/cpu"
)

func sadBlock16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	if !dspWindowOK(src, srcStride, 16, 16) || !dspWindowOK(ref, refStride, 16, 16) {
		return sadBlockScalarFallback(src, srcStride, ref, refStride, 16, 16)
	}
	srcPtr := unsafe.SliceData(src)
	refPtr := unsafe.SliceData(ref)
	if cpu.HasAVX2 {
		return int(sadBlock16x16AVX2(srcPtr, srcStride, refPtr, refStride))
	}
	return int(sadBlock16x16SSE2(srcPtr, srcStride, refPtr, refStride))
}

// SAD16x16PtrFast is the SIMD-bypass entry point for the inter motion
// picker. See sad_dispatch_arm64.go for caller contract.
func SAD16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) int {
	if cpu.HasAVX2 {
		return int(sadBlock16x16AVX2(src, srcStride, ref, refStride))
	}
	return int(sadBlock16x16SSE2(src, srcStride, ref, refStride))
}

// SAD16x16LimitPtrFast is the limited SIMD-bypass entry point. The
// caller must have already validated the in-bounds 16x16 windows AND
// that limit fits in int32 (always true for cost-pruned motion search).
func SAD16x16LimitPtrFast(src *byte, srcStride int, ref *byte, refStride int, limit int) int {
	// AVX2's 2-row granularity would lose byte parity with SSE2 here, so
	// the limit kernel stays on SSE2 — same rationale as sadBlock16x16Limit.
	return int(sadBlock16x16LimitSSE2(src, srcStride, ref, refStride, int32(limit)))
}

// SAD16x16x4PtrFast mirrors libvpx's vpx_sad16x16x4d interface. amd64 keeps
// the existing scalar-dispatch semantics for now; the arm64 motion-search path
// gets the fused NEON kernel first.
func SAD16x16x4PtrFast(src *byte, srcStride int, ref0 *byte, ref1 *byte, ref2 *byte, ref3 *byte, refStride int, out *[4]uint32) {
	out[0] = uint32(SAD16x16PtrFast(src, srcStride, ref0, refStride))
	out[1] = uint32(SAD16x16PtrFast(src, srcStride, ref1, refStride))
	out[2] = uint32(SAD16x16PtrFast(src, srcStride, ref2, refStride))
	out[3] = uint32(SAD16x16PtrFast(src, srcStride, ref3, refStride))
}

func sadBlock16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	if !dspWindowOK(src, srcStride, 16, 16) || !dspWindowOK(ref, refStride, 16, 16) {
		return sadBlockLimitScalarFallback(src, srcStride, ref, refStride, 16, 16, limit)
	}
	// The limit kernel returns the running sum at the row boundary
	// where it exceeds the limit; mirroring that exactly under AVX2's
	// natural 2-row granularity would lose byte parity with the SSE2
	// path, so we keep this on SSE2.
	return int(sadBlock16x16LimitSSE2(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride, sadLimitClamp32(limit)))
}

// sadLimitClamp32 narrows the caller-supplied limit to the SSE2 kernel's
// int32 range. Split out so the SAD dispatch entry stays inlineable.
func sadLimitClamp32(limit int) int32 {
	// Branchless clamp to int32 [0, MaxInt32]: min(max(limit, 0), 0x7fffffff).
	return int32(min(max(limit, 0), 0x7fffffff))
}

func sadBlock16x8(src []byte, srcStride int, ref []byte, refStride int) int {
	if !dspWindowOK(src, srcStride, 16, 8) || !dspWindowOK(ref, refStride, 16, 8) {
		return sadBlockScalarFallback(src, srcStride, ref, refStride, 16, 8)
	}
	srcPtr := unsafe.SliceData(src)
	refPtr := unsafe.SliceData(ref)
	if cpu.HasAVX2 {
		return int(sadBlock16x8AVX2(srcPtr, srcStride, refPtr, refStride))
	}
	return int(sadBlock16x8SSE2(srcPtr, srcStride, refPtr, refStride))
}

func sadBlock8x16(src []byte, srcStride int, ref []byte, refStride int) int {
	if !dspWindowOK(src, srcStride, 8, 16) || !dspWindowOK(ref, refStride, 8, 16) {
		return sadBlockScalarFallback(src, srcStride, ref, refStride, 8, 16)
	}
	// 8-wide SAD does not benefit from AVX2: packing four 8-byte rows
	// into a YMM costs more memory ops (8x MOVQ + PUNPCKLQDQ +
	// VINSERTI128) than the SSE2 schedule's two-row PSADBW form. The
	// AVX2 entry point exists for parity testing but is not routed.
	return int(sadBlock8x16SSE2(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride))
}

func sadBlock8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	if !dspWindowOK(src, srcStride, 8, 8) || !dspWindowOK(ref, refStride, 8, 8) {
		return sadBlockScalarFallback(src, srcStride, ref, refStride, 8, 8)
	}
	// See sadBlock8x16 — 8-wide stays on SSE2.
	return int(sadBlock8x8SSE2(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride))
}

func sadBlock4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	if !dspWindowOK(src, srcStride, 4, 4) || !dspWindowOK(ref, refStride, 4, 4) {
		return sadBlockScalarFallback(src, srcStride, ref, refStride, 4, 4)
	}
	return int(sadBlock4x4SSE2(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride))
}
