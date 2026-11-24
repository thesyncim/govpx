package dsp

import (
	"testing"
)

// FuzzVP8DSPVariance is a differential SIMD-vs-scalar fuzz harness for the
// VP8 variance / SSE primitives. It mirrors libvpx's
// test/variance_test.cc cross-check pattern: for each fuzz iteration we
// pick an op (block size) via the op-selector byte, construct
// stride-aware src/ref buffers from the fuzz payload, then run both the
// SIMD-dispatched entry point (which routes through the per-arch
// varianceBlock16x16 / varianceBlockSized kernels) and the scalar
// reference (varianceBlockGeneric from variance.go:155, which is the
// canonical libvpx vp8/encoder/variance.c port).
//
// Any byte-level divergence between the two is a real bug — the dispatch
// promises byte-identical output to the generic scalar fallback per
// internal/vp8/dsp/variance.go and the libvpx
// vp8/common/x86/variance_sse2.asm + vp8/common/arm/neon/vp8_subpixelvariance_neon.c
// references.

func FuzzVP8DSPVariance(f *testing.F) {
	// 6 seeds covering the byte-shaped cases per the harness brief:
	// zero, max, alternating, block-aligned, sub-block-misaligned,
	// random-shaped.
	seeds := [][]byte{
		make([]byte, 1024),
		bytes255(1024),
		bytesAlt(1024),
		bytesRamp(1024, 0),
		bytesRamp(1024, 7),
		bytesPattern(1024, 0x51, 0x73),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Need at least 2 plane buffers of 32*32 = 1024 each.
		const planeStride = 32
		const planeRows = 32
		const planeBytes = planeStride * planeRows
		if len(data) < 2*planeBytes+4 {
			return
		}
		op := int(data[0]) % 7
		srcOff := int(data[1]) % 8
		refOff := int(data[2]) % 8
		_ = data[3]
		src := make([]byte, planeBytes)
		ref := make([]byte, planeBytes)
		copy(src, data[4:4+planeBytes])
		copy(ref, data[4+planeBytes:4+2*planeBytes])

		srcView := src[srcOff*planeStride+srcOff:]
		refView := ref[refOff*planeStride+refOff:]

		switch op {
		case 0: // Variance4x4
			got := Variance4x4(srcView, planeStride, refView, planeStride)
			sum, sse := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 4, 4)
			want := sse - (sum*sum)>>4
			if got != want {
				t.Fatalf("Variance4x4 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 1: // Variance8x8
			got := Variance8x8(srcView, planeStride, refView, planeStride)
			sum, sse := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 8, 8)
			want := sse - (sum*sum)>>6
			if got != want {
				t.Fatalf("Variance8x8 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 2: // Variance16x16
			got := Variance16x16(srcView, planeStride, refView, planeStride)
			sum, sse := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 16, 16)
			want := sse - (sum*sum)>>8
			if got != want {
				t.Fatalf("Variance16x16 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 3: // Variance16x8
			got := Variance16x8(srcView, planeStride, refView, planeStride)
			sum, sse := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 16, 8)
			want := sse - (sum*sum)>>7
			if got != want {
				t.Fatalf("Variance16x8 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 4: // Variance8x16
			got := Variance8x16(srcView, planeStride, refView, planeStride)
			sum, sse := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 8, 16)
			want := sse - (sum*sum)>>7
			if got != want {
				t.Fatalf("Variance8x16 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 5: // SSE16x16 (mse16x16 equivalent in libvpx)
			got := SSE16x16(srcView, planeStride, refView, planeStride)
			_, want := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 16, 16)
			if got != want {
				t.Fatalf("SSE16x16 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 6: // SSE4x4 (equivalent of vp8_get4x4sse_cs in libvpx)
			got := SSE4x4(srcView, planeStride, refView, planeStride)
			_, want := varianceBlockGeneric(srcView, planeStride, refView, planeStride, 4, 4)
			if got != want {
				t.Fatalf("SSE4x4 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		}
	})
}

// Helpers shared by every VP8 DSP fuzz target. Kept tiny so the fuzz
// callbacks can construct byte payloads from the fuzz selector without
// reaching into math/rand at fuzz time.

func bytes255(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = 0xFF
	}
	return out
}

func bytesAlt(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		if i&1 == 0 {
			out[i] = 0x00
		} else {
			out[i] = 0xFF
		}
	}
	return out
}

func bytesRamp(n int, seed byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(i) + seed
	}
	return out
}

func bytesPattern(n int, a byte, b byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = a ^ (byte(i) * b)
	}
	return out
}
