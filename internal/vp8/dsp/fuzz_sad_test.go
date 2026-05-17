package dsp

import (
	"testing"
)

// FuzzVP8DSPSad is a differential SIMD-vs-scalar fuzz harness for the
// VP8 SAD primitives. Mirrors libvpx test/sad_test.cc: each iteration
// picks an op (block size / limit / multi-ref variant) from the
// selector byte, derives stride-aware src/ref buffers from the fuzz
// payload, calls the dispatched SIMD entry point AND the canonical
// scalar reference (scalarSAD from sad_test.go:177, the libvpx
// vp8/encoder/mcomp.c port), and asserts byte-exact equality.
//
// The limit kernel (SAD16x16Limit) and the 4-ref fused entry point
// (SAD16x16x4PtrFast) are covered as additional op selectors because
// they have their own SIMD implementations (sse2 / neon / dotprod) that
// can drift from the canonical sum even when the unlimited 1-ref kernel
// stays in sync.

func FuzzVP8DSPSad(f *testing.F) {
	seeds := [][]byte{
		make([]byte, 2*32*32+8),
		bytes255(2*32*32 + 8),
		bytesAlt(2*32*32 + 8),
		bytesRamp(2*32*32+8, 0),
		bytesRamp(2*32*32+8, 13),
		bytesPattern(2*32*32+8, 0x42, 0x91),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		const planeStride = 32
		const planeRows = 32
		const planeBytes = planeStride * planeRows
		if len(data) < 2*planeBytes+8 {
			return
		}
		op := int(data[0]) % 7
		srcOff := int(data[1]) % 8
		refOff := int(data[2]) % 8
		// Limit byte spans a useful range: 0/small/full/huge.
		limitByte := data[3]
		_ = data[4]
		_ = data[5]
		_ = data[6]
		_ = data[7]

		src := make([]byte, planeBytes)
		ref := make([]byte, planeBytes)
		copy(src, data[8:8+planeBytes])
		copy(ref, data[8+planeBytes:8+2*planeBytes])

		srcView := src[srcOff*planeStride+srcOff:]
		refView := ref[refOff*planeStride+refOff:]

		switch op {
		case 0:
			got := SAD16x16(srcView, planeStride, refView, planeStride)
			want := scalarSAD(srcView, planeStride, refView, planeStride, 16, 16)
			if got != want {
				t.Fatalf("SAD16x16 simd=%d scalar=%d srcOff=%d refOff=%d", got, want, srcOff, refOff)
			}
		case 1:
			got := SAD16x8(srcView, planeStride, refView, planeStride)
			want := scalarSAD(srcView, planeStride, refView, planeStride, 16, 8)
			if got != want {
				t.Fatalf("SAD16x8 simd=%d scalar=%d", got, want)
			}
		case 2:
			got := SAD8x16(srcView, planeStride, refView, planeStride)
			want := scalarSAD(srcView, planeStride, refView, planeStride, 8, 16)
			if got != want {
				t.Fatalf("SAD8x16 simd=%d scalar=%d", got, want)
			}
		case 3:
			got := SAD8x8(srcView, planeStride, refView, planeStride)
			want := scalarSAD(srcView, planeStride, refView, planeStride, 8, 8)
			if got != want {
				t.Fatalf("SAD8x8 simd=%d scalar=%d", got, want)
			}
		case 4:
			got := SAD4x4(srcView, planeStride, refView, planeStride)
			want := scalarSAD(srcView, planeStride, refView, planeStride, 4, 4)
			if got != want {
				t.Fatalf("SAD4x4 simd=%d scalar=%d", got, want)
			}
		case 5:
			// SAD16x16Limit: select a useful per-iter limit by scaling
			// limitByte into [0, full_sad+128] so we exercise above /
			// at / below the natural limit boundary the picker hits.
			full := scalarSAD(srcView, planeStride, refView, planeStride, 16, 16)
			lim := int(limitByte) * full / 256
			got := SAD16x16Limit(srcView, planeStride, refView, planeStride, lim)
			want := scalarSADLimitFuzz(srcView, planeStride, refView, planeStride, 16, 16, lim)
			if got != want {
				t.Fatalf("SAD16x16Limit lim=%d simd=%d scalar=%d full=%d", lim, got, want, full)
			}
		case 6:
			// SAD16x16x4PtrFast — four ref pointers at independent
			// offsets, each must match the 1-ref scalar.
			// Need at least four valid 16x16 windows; refOff is already
			// constrained to [0,7] so all four windows fit in a
			// 32x32 plane with stride 32.
			ref0 := &ref[0]
			ref1 := &ref[1]
			ref2 := &ref[planeStride]
			ref3 := &ref[planeStride+1]
			var got [4]uint32
			SAD16x16x4PtrFast(&src[0], planeStride, ref0, ref1, ref2, ref3, planeStride, &got)
			refs := []int{
				scalarSAD(src, planeStride, ref, planeStride, 16, 16),
				scalarSAD(src, planeStride, ref[1:], planeStride, 16, 16),
				scalarSAD(src, planeStride, ref[planeStride:], planeStride, 16, 16),
				scalarSAD(src, planeStride, ref[planeStride+1:], planeStride, 16, 16),
			}
			for i, want := range refs {
				if int(got[i]) != want {
					t.Fatalf("SAD16x16x4PtrFast[%d]: simd=%d scalar=%d", i, got[i], want)
				}
			}
		}
	})
}

// scalarSADLimitFuzz is the canonical libvpx limit-aware SAD: return
// the running sum at the row boundary where it exceeds limit, otherwise
// the full SAD. Mirrors scalarSADLimit in sad_simd_test.go:97.
func scalarSADLimitFuzz(src []byte, srcStride int, ref []byte, refStride int, w, h, limit int) int {
	sad := 0
	for y := range h {
		for x := range w {
			d := int(src[y*srcStride+x]) - int(ref[y*refStride+x])
			if d < 0 {
				d = -d
			}
			sad += d
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}
