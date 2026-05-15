//go:build arm64 && !purego

package dsp

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/cpu"
)

// libvpx v1.16.0 vpx_dsp/arm/sad_neon.c-style dispatch wrappers.
//
// The wrappers pull the slice base pointers via unsafe.SliceData so the
// dispatch stays inlineable and free of the runtime.panicBounds + stack
// frame the compiler emits for &src[0] / &ref[0]. Callers in the motion
// search hot path (encoder_reconstruct.go) always pass non-empty slices
// shaped to cover the read window, matching the implicit contract of
// the underlying NEON kernels.

func sadBlock16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	if cpu.HasARM64DotProd {
		return int(sadBlock16x16DotProd(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride))
	}
	return int(sadBlock16x16NEON(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride))
}

// SAD16x16PtrFast is the SIMD-bypass entry point for the inter motion
// picker. It assumes the caller has already validated:
//
//   - src and ref point to 16x16 windows fully in-bounds for their
//     respective buffers (`baseY+16 <= height && baseX+16 <= width`)
//   - srcStride and refStride match the stride of the originating slice
//
// The whole-MB SAD result fits in [0, 16*16*255 = 65280] so the int32
// kernel return is always exact (no saturation possible).
func SAD16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) int {
	if cpu.HasARM64DotProd {
		return int(sadBlock16x16DotProd(src, srcStride, ref, refStride))
	}
	return int(sadBlock16x16NEON(src, srcStride, ref, refStride))
}

// SAD16x16LimitPtrFast is the limited SIMD-bypass entry point. The
// caller must have already validated the in-bounds 16x16 windows AND
// that limit is in [0, 0x7fffffff] (typical motion-search limits are
// at most a few hundred thousand, so this is satisfied trivially by
// the cost-pruned picker walks).
func SAD16x16LimitPtrFast(src *byte, srcStride int, ref *byte, refStride int, limit int) int {
	return int(sadBlock16x16LimitNEON(src, srcStride, ref, refStride, int32(limit)))
}

// SAD16x16x4PtrFast mirrors libvpx's vpx_sad16x16x4d_neon entry: compare one
// source 16x16 block against four in-bounds 16x16 reference blocks and write
// four SADs in candidate order.
func SAD16x16x4PtrFast(src *byte, srcStride int, ref0 *byte, ref1 *byte, ref2 *byte, ref3 *byte, refStride int, out *[4]uint32) {
	if cpu.HasARM64DotProd {
		sadBlock16x16x4DotProd(src, srcStride, ref0, ref1, ref2, ref3, refStride, out)
		return
	}
	sadBlock16x16x4NEON(src, srcStride, ref0, ref1, ref2, ref3, refStride, out)
}

func sadBlock16x16Limit(src []byte, srcStride int, ref []byte, refStride int, limit int) int {
	// The NEON kernel takes a 32-bit signed limit; the wrapper hands it a
	// fast clamp so the dispatch stays inlineable. The hot motion-search
	// caller passes positive ints in the [0, 0x7fffffff] range, where
	// uint(limit) <= 0x7fffffff matches the int32 fast path (negative
	// ints become huge unsigned and bail to the cold slow path).
	return int(sadBlock16x16LimitNEON(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride, sadLimitClamp32(limit)))
}

// sadLimitClamp32 narrows the caller-supplied limit to the NEON kernel's
// int32 range. Split out so the SAD dispatch entry stays inlineable.
func sadLimitClamp32(limit int) int32 {
	// Branchless clamp to int32 [0, MaxInt32]: min(max(limit, 0), 0x7fffffff).
	// Same three-way result as the original branch ladder without compares.
	return int32(min(max(limit, 0), 0x7fffffff))
}

func sadBlock16x8(src []byte, srcStride int, ref []byte, refStride int) int {
	if cpu.HasARM64DotProd {
		return int(sadBlock16x8DotProd(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride))
	}
	return int(sadBlock16x8NEON(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride))
}

func sadBlock8x16(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock8x16NEON(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride))
}

func sadBlock8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock8x8NEON(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride))
}

func sadBlock4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	return int(sadBlock4x4NEON(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride))
}
