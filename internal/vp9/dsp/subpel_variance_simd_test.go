package dsp

import (
	"math/rand/v2"
	"testing"
)

// TestVP9SubPixelVarianceSimd* verifies the public
// VpxSubPixelVariance{W}x{H} wrappers match the scalar reference on
// randomized and edge-case inputs across every (xOffset, yOffset)
// pair in {0..7}^2.

type subpelVarCase struct {
	name string
	w, h int
	fn   func(src []uint8, srcOff, srcStride, xOffset, yOffset int, ref []uint8, refOff, refStride int, sse *uint32) uint32
}

func vp9SubpelVarCases() []subpelVarCase {
	return []subpelVarCase{
		{"4x4", 4, 4, VpxSubPixelVariance4x4},
		{"4x8", 4, 8, VpxSubPixelVariance4x8},
		{"8x4", 8, 4, VpxSubPixelVariance8x4},
		{"8x8", 8, 8, VpxSubPixelVariance8x8},
		{"8x16", 8, 16, VpxSubPixelVariance8x16},
		{"16x8", 16, 8, VpxSubPixelVariance16x8},
		{"16x16", 16, 16, VpxSubPixelVariance16x16},
		{"16x32", 16, 32, VpxSubPixelVariance16x32},
		{"32x16", 32, 16, VpxSubPixelVariance32x16},
		{"32x32", 32, 32, VpxSubPixelVariance32x32},
		{"32x64", 32, 64, VpxSubPixelVariance32x64},
		{"64x32", 64, 32, VpxSubPixelVariance64x32},
		{"64x64", 64, 64, VpxSubPixelVariance64x64},
	}
}

func TestVP9SubPixelVarianceSimdRandomAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0xc0ff, 0xee99))
	const stride = 96
	const off = 8
	for _, c := range vp9SubpelVarCases() {
		t.Run(c.name, func(t *testing.T) {
			for trial := range 12 {
				src := make([]uint8, stride*(c.h+off+8))
				ref := make([]uint8, stride*(c.h+off+8))
				for i := range src {
					src[i] = uint8(r.UintN(256))
					ref[i] = uint8(r.UintN(256))
				}
				xOff := int(r.UintN(8))
				yOff := int(r.UintN(8))
				var sseGot, sseWant uint32
				got := c.fn(src, off*stride+off, stride, xOff, yOff, ref, off*stride+off, stride, &sseGot)
				want := subPixelVarianceScalar(c.w, c.h, src, off*stride+off, stride, xOff, yOff, ref, off*stride+off, stride, &sseWant)
				if got != want || sseGot != sseWant {
					t.Fatalf("trial %d xOff=%d yOff=%d: got var=%d sse=%d want var=%d sse=%d", trial, xOff, yOff, got, sseGot, want, sseWant)
				}
			}
		})
	}
}

func TestVP9SubPixelVarianceSimdAllSubpelPairs(t *testing.T) {
	r := rand.New(rand.NewPCG(0xbeef, 0xfa11))
	const stride = 96
	const off = 8
	for _, c := range vp9SubpelVarCases() {
		t.Run(c.name, func(t *testing.T) {
			src := make([]uint8, stride*(c.h+off+8))
			ref := make([]uint8, stride*(c.h+off+8))
			for i := range src {
				src[i] = uint8(r.UintN(256))
				ref[i] = uint8(r.UintN(256))
			}
			for xOff := range 8 {
				for yOff := range 8 {
					var sseGot, sseWant uint32
					got := c.fn(src, off*stride+off, stride, xOff, yOff, ref, off*stride+off, stride, &sseGot)
					want := subPixelVarianceScalar(c.w, c.h, src, off*stride+off, stride, xOff, yOff, ref, off*stride+off, stride, &sseWant)
					if got != want || sseGot != sseWant {
						t.Fatalf("xOff=%d yOff=%d: got var=%d sse=%d want var=%d sse=%d", xOff, yOff, got, sseGot, want, sseWant)
					}
				}
			}
		})
	}
}

func TestVP9SubPixelVarianceSimdEdgeCases(t *testing.T) {
	const stride = 96
	const off = 8
	cases := []struct {
		name      string
		srcFill   uint8
		refFill   uint8
		pokeDelta int
	}{
		{"allZero", 0, 0, 0},
		{"all255", 255, 255, 0},
		{"src255_ref0", 255, 0, 0},
		{"src0_ref255", 0, 255, 0},
		{"singlePixelDiff", 100, 100, 17},
	}
	for _, c := range vp9SubpelVarCases() {
		t.Run(c.name, func(t *testing.T) {
			for _, ec := range cases {
				t.Run(ec.name, func(t *testing.T) {
					src := make([]uint8, stride*(c.h+off+8))
					ref := make([]uint8, stride*(c.h+off+8))
					for i := range src {
						src[i] = ec.srcFill
						ref[i] = ec.refFill
					}
					if ec.pokeDelta != 0 {
						src[off*stride+off] = uint8(int(ec.srcFill) + ec.pokeDelta)
					}
					// Probe each corner of the subpel grid.
					for _, xOff := range []int{0, 4, 7} {
						for _, yOff := range []int{0, 4, 7} {
							var sseGot, sseWant uint32
							got := c.fn(src, off*stride+off, stride, xOff, yOff, ref, off*stride+off, stride, &sseGot)
							want := subPixelVarianceScalar(c.w, c.h, src, off*stride+off, stride, xOff, yOff, ref, off*stride+off, stride, &sseWant)
							if got != want || sseGot != sseWant {
								t.Fatalf("%s xOff=%d yOff=%d: got var=%d sse=%d want var=%d sse=%d", ec.name, xOff, yOff, got, sseGot, want, sseWant)
							}
						}
					}
				})
			}
		})
	}
}

func TestVP9SubPixelVarianceSimdStrides(t *testing.T) {
	r := rand.New(rand.NewPCG(0xdeed, 0xbabe))
	strides := []int{72, 80, 96, 128, 129}
	for _, c := range vp9SubpelVarCases() {
		t.Run(c.name, func(t *testing.T) {
			for _, stride := range strides {
				if stride < c.w+1 {
					continue
				}
				off := stride + 3
				src := make([]uint8, stride*(c.h+8)+off+c.w+2)
				ref := make([]uint8, stride*(c.h+8)+off+c.w+2)
				for i := range src {
					src[i] = uint8(r.UintN(256))
					ref[i] = uint8(r.UintN(256))
				}
				for _, xOff := range []int{0, 3, 5, 7} {
					for _, yOff := range []int{0, 2, 4, 6} {
						var sseGot, sseWant uint32
						got := c.fn(src, off, stride, xOff, yOff, ref, off, stride, &sseGot)
						want := subPixelVarianceScalar(c.w, c.h, src, off, stride, xOff, yOff, ref, off, stride, &sseWant)
						if got != want || sseGot != sseWant {
							t.Fatalf("stride=%d off=%d xOff=%d yOff=%d: got var=%d sse=%d want var=%d sse=%d",
								stride, off, xOff, yOff, got, sseGot, want, sseWant)
						}
					}
				}
			}
		})
	}
}

func BenchmarkVP9SubPixelVariance16x16(b *testing.B) {
	r := rand.New(rand.NewPCG(0x1111, 0x2222))
	const stride = 64
	const off = 8
	src := make([]uint8, stride*(16+off+8))
	ref := make([]uint8, stride*(16+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSubPixelVariance16x16(src, off*stride+off, stride, 3, 5, ref, off*stride+off, stride, &sse)
	}
}

func BenchmarkVP9SubPixelVariance32x32(b *testing.B) {
	r := rand.New(rand.NewPCG(0x3333, 0x4444))
	const stride = 96
	const off = 8
	src := make([]uint8, stride*(32+off+8))
	ref := make([]uint8, stride*(32+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSubPixelVariance32x32(src, off*stride+off, stride, 3, 5, ref, off*stride+off, stride, &sse)
	}
}

func BenchmarkVP9SubPixelVariance32x16(b *testing.B) {
	r := rand.New(rand.NewPCG(0x3333, 0x1616))
	const stride = 96
	const off = 8
	src := make([]uint8, stride*(32+off+8))
	ref := make([]uint8, stride*(32+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSubPixelVariance32x16(src, off*stride+off, stride, 3, 5, ref, off*stride+off, stride, &sse)
	}
}

func BenchmarkVP9SubPixelVariance16x32(b *testing.B) {
	r := rand.New(rand.NewPCG(0x3333, 0x3216))
	const stride = 96
	const off = 8
	src := make([]uint8, stride*(32+off+8))
	ref := make([]uint8, stride*(32+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSubPixelVariance16x32(src, off*stride+off, stride, 3, 5, ref, off*stride+off, stride, &sse)
	}
}

func BenchmarkVP9SubPixelVariance32x32HalfPel(b *testing.B) {
	r := rand.New(rand.NewPCG(0x3333, 0x4445))
	const stride = 96
	const off = 8
	src := make([]uint8, stride*(32+off+8))
	ref := make([]uint8, stride*(32+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSubPixelVariance32x32(src, off*stride+off, stride, 4, 4, ref, off*stride+off, stride, &sse)
	}
}

func BenchmarkVP9SubPixelVariance32x32HorizontalOnly(b *testing.B) {
	r := rand.New(rand.NewPCG(0x3333, 0x7777))
	const stride = 96
	const off = 8
	src := make([]uint8, stride*(32+off+8))
	ref := make([]uint8, stride*(32+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSubPixelVariance32x32(src, off*stride+off, stride, 3, 0, ref, off*stride+off, stride, &sse)
	}
}

func BenchmarkVP9SubPixelVariance32x32VerticalOnly(b *testing.B) {
	r := rand.New(rand.NewPCG(0x3333, 0x8888))
	const stride = 96
	const off = 8
	src := make([]uint8, stride*(32+off+8))
	ref := make([]uint8, stride*(32+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSubPixelVariance32x32(src, off*stride+off, stride, 0, 5, ref, off*stride+off, stride, &sse)
	}
}

func BenchmarkVP9SubPixelVariance32x32FullPel(b *testing.B) {
	r := rand.New(rand.NewPCG(0x3333, 0x9999))
	const stride = 96
	const off = 8
	src := make([]uint8, stride*(32+off+8))
	ref := make([]uint8, stride*(32+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSubPixelVariance32x32(src, off*stride+off, stride, 0, 0, ref, off*stride+off, stride, &sse)
	}
}

func BenchmarkVP9SubPixelVariance64x64(b *testing.B) {
	r := rand.New(rand.NewPCG(0x5555, 0x6666))
	const stride = 128
	const off = 8
	src := make([]uint8, stride*(64+off+8))
	ref := make([]uint8, stride*(64+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSubPixelVariance64x64(src, off*stride+off, stride, 3, 5, ref, off*stride+off, stride, &sse)
	}
}

func BenchmarkVP9SubPixelVariance8x8(b *testing.B) {
	r := rand.New(rand.NewPCG(0x7777, 0x8888))
	const stride = 64
	const off = 8
	src := make([]uint8, stride*(8+off+8))
	ref := make([]uint8, stride*(8+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	var sse uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSubPixelVariance8x8(src, off*stride+off, stride, 3, 5, ref, off*stride+off, stride, &sse)
	}
}
