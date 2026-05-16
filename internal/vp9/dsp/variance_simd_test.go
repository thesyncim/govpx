package dsp

import (
	"math/rand/v2"
	"testing"
)

// TestVP9VarianceSimdAgreement verifies each public VpxVariance{W}x{H}
// wrapper matches the scalar reference for both the returned variance
// and the sse out-parameter on randomized and edge-case inputs.

type varCase struct {
	name string
	w, h int
	fn   func(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32
}

func vp9VarCases() []varCase {
	return []varCase{
		{"4x4", 4, 4, VpxVariance4x4},
		{"4x8", 4, 8, VpxVariance4x8},
		{"8x4", 8, 4, VpxVariance8x4},
		{"8x8", 8, 8, VpxVariance8x8},
		{"8x16", 8, 16, VpxVariance8x16},
		{"16x8", 16, 8, VpxVariance16x8},
		{"16x16", 16, 16, VpxVariance16x16},
		{"16x32", 16, 32, VpxVariance16x32},
		{"32x16", 32, 16, VpxVariance32x16},
		{"32x32", 32, 32, VpxVariance32x32},
		{"32x64", 32, 64, VpxVariance32x64},
		{"64x32", 64, 32, VpxVariance64x32},
		{"64x64", 64, 64, VpxVariance64x64},
	}
}

func TestVP9VarianceSimdRandomAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0xfeed, 0xface))
	const stride = 96
	const off = 8
	for _, c := range vp9VarCases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			for trial := 0; trial < 12; trial++ {
				src := make([]uint8, stride*(c.h+off+8))
				ref := make([]uint8, stride*(c.h+off+8))
				for i := range src {
					src[i] = uint8(r.UintN(256))
					ref[i] = uint8(r.UintN(256))
				}
				var sseGot, sseWant uint32
				got := c.fn(src, off*stride+off, stride, ref, off*stride+off, stride, &sseGot)
				want := varianceScalar(c.w, c.h, src, off*stride+off, stride, ref, off*stride+off, stride, &sseWant)
				if got != want || sseGot != sseWant {
					t.Fatalf("trial %d: got var=%d sse=%d want var=%d sse=%d", trial, got, sseGot, want, sseWant)
				}
			}
		})
	}
}

func TestVP9VarianceSimdEdgeCases(t *testing.T) {
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
		{"src127_ref128", 127, 128, 0}, // uniform negative diff
		{"singlePixelDiff", 100, 100, 17},
	}
	for _, c := range vp9VarCases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			for _, ec := range cases {
				ec := ec
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
					var sseGot, sseWant uint32
					got := c.fn(src, off*stride+off, stride, ref, off*stride+off, stride, &sseGot)
					want := varianceScalar(c.w, c.h, src, off*stride+off, stride, ref, off*stride+off, stride, &sseWant)
					if got != want || sseGot != sseWant {
						t.Fatalf("%s: got var=%d sse=%d want var=%d sse=%d", ec.name, got, sseGot, want, sseWant)
					}
				})
			}
		})
	}
}

func TestVP9VarianceSimdStrides(t *testing.T) {
	r := rand.New(rand.NewPCG(0xdead, 0xbeef))
	strides := []int{64, 67, 80, 96, 128, 129}
	for _, c := range vp9VarCases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			for _, stride := range strides {
				if stride < c.w {
					continue
				}
				off := stride + 3
				src := make([]uint8, stride*(c.h+8)+off+c.w)
				ref := make([]uint8, stride*(c.h+8)+off+c.w)
				for i := range src {
					src[i] = uint8(r.UintN(256))
					ref[i] = uint8(r.UintN(256))
				}
				var sseGot, sseWant uint32
				got := c.fn(src, off, stride, ref, off, stride, &sseGot)
				want := varianceScalar(c.w, c.h, src, off, stride, ref, off, stride, &sseWant)
				if got != want || sseGot != sseWant {
					t.Fatalf("stride=%d off=%d: got var=%d sse=%d want var=%d sse=%d", stride, off, got, sseGot, want, sseWant)
				}
			}
		})
	}
}

func BenchmarkVP9Variance16x16(b *testing.B) {
	r := rand.New(rand.NewPCG(0x1234, 0x5678))
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
		VpxVariance16x16(src, off*stride+off, stride, ref, off*stride+off, stride, &sse)
	}
}

func BenchmarkVP9Variance32x32(b *testing.B) {
	r := rand.New(rand.NewPCG(0x4321, 0x8765))
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
		VpxVariance32x32(src, off*stride+off, stride, ref, off*stride+off, stride, &sse)
	}
}

func BenchmarkVP9Variance64x64(b *testing.B) {
	r := rand.New(rand.NewPCG(0xabcd, 0xef01))
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
		VpxVariance64x64(src, off*stride+off, stride, ref, off*stride+off, stride, &sse)
	}
}
