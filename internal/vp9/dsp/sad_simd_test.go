package dsp

import (
	"math/rand/v2"
	"testing"
)

// TestVP9SadSimdAgreement validates each public SAD wrapper against
// the scalar reference (`sad`) over random and edge-case inputs.
// On a purego build the wrappers route to the scalar reference
// directly, so the comparison still passes (it's just a self-check).

type sadCase struct {
	name string
	w, h int
	fn   func(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32
}

func vp9SadCases() []sadCase {
	return []sadCase{
		{"4x4", 4, 4, VpxSad4x4},
		{"4x8", 4, 8, VpxSad4x8},
		{"8x4", 8, 4, VpxSad8x4},
		{"8x8", 8, 8, VpxSad8x8},
		{"8x16", 8, 16, VpxSad8x16},
		{"16x8", 16, 8, VpxSad16x8},
		{"16x16", 16, 16, VpxSad16x16},
		{"16x32", 16, 32, VpxSad16x32},
		{"32x16", 32, 16, VpxSad32x16},
		{"32x32", 32, 32, VpxSad32x32},
		{"32x64", 32, 64, VpxSad32x64},
		{"64x32", 64, 32, VpxSad64x32},
		{"64x64", 64, 64, VpxSad64x64},
	}
}

func TestVP9SadSimdRandomAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0x9c5c, 0xd7e1))
	const stride = 96
	const off = 8
	for _, c := range vp9SadCases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			for trial := 0; trial < 12; trial++ {
				src := make([]uint8, stride*(c.h+off+8))
				ref := make([]uint8, stride*(c.h+off+8))
				for i := range src {
					src[i] = uint8(r.UintN(256))
					ref[i] = uint8(r.UintN(256))
				}
				got := c.fn(src, off*stride+off, stride, ref, off*stride+off, stride)
				want := sad(src, off*stride+off, stride, ref, off*stride+off, stride, c.w, c.h)
				if got != want {
					t.Fatalf("trial %d: got %d want %d", trial, got, want)
				}
			}
		})
	}
}

func TestVP9SadSimdEdgeCases(t *testing.T) {
	const stride = 96
	const off = 8
	for _, c := range vp9SadCases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			cases := []struct {
				name      string
				srcFill   uint8
				refFill   uint8
				pokeDelta int // optional poke at (off, off): src[..]+=delta
			}{
				{"allZero", 0, 0, 0},
				{"all255", 255, 255, 0},
				{"src255_ref0", 255, 0, 0},
				{"src0_ref255", 0, 255, 0},
				{"singlePixelDiff", 100, 100, 17},
			}
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
					got := c.fn(src, off*stride+off, stride, ref, off*stride+off, stride)
					want := sad(src, off*stride+off, stride, ref, off*stride+off, stride, c.w, c.h)
					if got != want {
						t.Fatalf("%s: got %d want %d", ec.name, got, want)
					}
				})
			}
		})
	}
}

func TestVP9SadSimdStrides(t *testing.T) {
	r := rand.New(rand.NewPCG(0x12bf, 0x09a7))
	strides := []int{64, 67, 80, 96, 128, 129}
	for _, c := range vp9SadCases() {
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
				got := c.fn(src, off, stride, ref, off, stride)
				want := sad(src, off, stride, ref, off, stride, c.w, c.h)
				if got != want {
					t.Fatalf("stride=%d off=%d: got %d want %d", stride, off, got, want)
				}
			}
		})
	}
}

func BenchmarkVP9Sad16x16(b *testing.B) {
	r := rand.New(rand.NewPCG(0x1234, 0x5678))
	const stride = 64
	const off = 8
	src := make([]uint8, stride*(16+off+8))
	ref := make([]uint8, stride*(16+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSad16x16(src, off*stride+off, stride, ref, off*stride+off, stride)
	}
}

func BenchmarkVP9Sad32x32(b *testing.B) {
	r := rand.New(rand.NewPCG(0x4321, 0x8765))
	const stride = 96
	const off = 8
	src := make([]uint8, stride*(32+off+8))
	ref := make([]uint8, stride*(32+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSad32x32(src, off*stride+off, stride, ref, off*stride+off, stride)
	}
}

func BenchmarkVP9Sad64x64(b *testing.B) {
	r := rand.New(rand.NewPCG(0xabcd, 0xef01))
	const stride = 128
	const off = 8
	src := make([]uint8, stride*(64+off+8))
	ref := make([]uint8, stride*(64+off+8))
	for i := range src {
		src[i] = uint8(r.UintN(256))
		ref[i] = uint8(r.UintN(256))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxSad64x64(src, off*stride+off, stride, ref, off*stride+off, stride)
	}
}
