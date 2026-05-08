//go:build amd64

package dsp

// libvpx v1.16.0 vpx_dsp/x86/sad_sse2.asm-style dispatch wrappers. SSE2 is
// part of the x86-64 baseline so the SIMD entry points are always safe to
// call without runtime detection. AVX2 entry points are gated by
// internal/cpu.HasAVX2 — when present, the 16-wide and 8-wide SADs route
// through the YMM kernels in sad_avx2_amd64.s for ~2x throughput.

import "github.com/thesyncim/govpx/internal/cpu"

func sadBlock16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	if cpu.HasAVX2 {
		return int(sadBlock16x16AVX2(&src[0], srcStride, &ref[0], refStride))
	}
	return int(sadBlock16x16SSE2(&src[0], srcStride, &ref[0], refStride))
}

func sadBlock16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	// The limit kernel returns the running sum at the row boundary
	// where it exceeds the limit; mirroring that exactly under AVX2's
	// natural 2-row granularity would lose byte parity with the SSE2
	// path, so we keep this on SSE2.
	if limit > 0x7fffffff {
		limit = 0x7fffffff
	}
	if limit < 0 {
		limit = 0
	}
	return int(sadBlock16x16LimitSSE2(&src[0], srcStride, &ref[0], refStride, int32(limit)))
}

func sadBlock16x8(src []byte, srcStride int, ref []byte, refStride int) int {
	if cpu.HasAVX2 {
		return int(sadBlock16x8AVX2(&src[0], srcStride, &ref[0], refStride))
	}
	return int(sadBlock16x8SSE2(&src[0], srcStride, &ref[0], refStride))
}

func sadBlock8x16(src []byte, srcStride int, ref []byte, refStride int) int {
	// 8-wide SAD does not benefit from AVX2: packing four 8-byte rows
	// into a YMM costs more memory ops (8x MOVQ + PUNPCKLQDQ +
	// VINSERTI128) than the SSE2 schedule's two-row PSADBW form. The
	// AVX2 entry point exists for parity testing but is not routed.
	return int(sadBlock8x16SSE2(&src[0], srcStride, &ref[0], refStride))
}

func sadBlock8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	// See sadBlock8x16 — 8-wide stays on SSE2.
	return int(sadBlock8x8SSE2(&src[0], srcStride, &ref[0], refStride))
}

func sadBlock4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock4x4SSE2(&src[0], srcStride, &ref[0], refStride))
}
