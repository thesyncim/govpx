package dsp

import (
	"math/rand"
	"testing"
	"unsafe"
)

// scalarResidualGather is the independent reference used by the SIMD parity
// test. It mirrors the govpx encoder's block-major residual layout: for an
// (W,H) macroblock (W=H=16 luma, W=H=8 chroma), produce ceil(W/4)*ceil(H/4)
// contiguous 4x4 int16 blocks in scan order, each block row-major at stride
// 4 int16s.
func scalarResidualGather(src []byte, srcStride int, pred []byte, predStride int, out []int16, w, h int) {
	bw := w / 4
	bh := h / 4
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			block := by*bw + bx
			for r := 0; r < 4; r++ {
				for c := 0; c < 4; c++ {
					a := int(src[(by*4+r)*srcStride+bx*4+c])
					b := int(pred[(by*4+r)*predStride+bx*4+c])
					out[block*16+r*4+c] = int16(a - b)
				}
			}
		}
	}
}

// TestResidualGatherSIMDMatchesScalar randomly fuzzes the 16x16 luma and
// 8x8 chroma residual gather kernels against an independent scalar
// reference. Strides and starting offsets are varied to cover unaligned
// loads and non-contiguous source layouts.
func TestResidualGatherSIMDMatchesScalar(t *testing.T) {
	const planeStride = 48
	const planeRows = 48
	src := make([]byte, planeStride*planeRows)
	pred := make([]byte, planeStride*planeRows)

	r := rand.New(rand.NewSource(0xDEADBEEF))
	for i := range src {
		src[i] = byte(r.Intn(256))
		pred[i] = byte(r.Intn(256))
	}

	cases := []struct {
		name string
		w, h int
		fn   func(srcP *byte, srcStride int, predP *byte, predStride int, outP *int16)
	}{
		{"16x16", 16, 16, ResidualGather16x16PtrFast},
		{"8x8", 8, 8, ResidualGather8x8PtrFast},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nBlocks := (c.w / 4) * (c.h / 4)
			outSimd := make([]int16, nBlocks*16)
			outScalar := make([]int16, nBlocks*16)
			for srcOff := 0; srcOff < 8; srcOff++ {
				for predOff := 0; predOff < 8; predOff++ {
					srcSlice := src[srcOff*planeStride+srcOff:]
					predSlice := pred[predOff*planeStride+predOff:]
					for i := range outSimd {
						outSimd[i] = 0x7777
						outScalar[i] = 0x7777
					}
					c.fn(
						(*byte)(unsafe.Pointer(&srcSlice[0])),
						planeStride,
						(*byte)(unsafe.Pointer(&predSlice[0])),
						planeStride,
						(*int16)(unsafe.Pointer(&outSimd[0])),
					)
					scalarResidualGather(srcSlice, planeStride, predSlice, planeStride, outScalar, c.w, c.h)
					for i := range outSimd {
						if outSimd[i] != outScalar[i] {
							t.Fatalf("%s offsets (src=%d pred=%d) idx=%d: got %d want %d", c.name, srcOff, predOff, i, outSimd[i], outScalar[i])
						}
					}
				}
			}
		})
	}
}

// TestResidualGatherSIMDExtremeValues stresses the kernels on full-range
// signed differences (src = 0 or 255, pred = 255 or 0) to ensure the
// USUBL/USUBL2 widening preserves the [-255, 255] int16 range.
func TestResidualGatherSIMDExtremeValues(t *testing.T) {
	cases := []struct {
		name      string
		w, h      int
		srcVal    byte
		predVal   byte
		expectVal int16
	}{
		{"16x16_pos", 16, 16, 255, 0, 255},
		{"16x16_neg", 16, 16, 0, 255, -255},
		{"8x8_pos", 8, 8, 255, 0, 255},
		{"8x8_neg", 8, 8, 0, 255, -255},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stride := c.w + 8
			src := make([]byte, stride*c.h)
			pred := make([]byte, stride*c.h)
			for i := range src {
				src[i] = c.srcVal
				pred[i] = c.predVal
			}
			nBlocks := (c.w / 4) * (c.h / 4)
			out := make([]int16, nBlocks*16)
			fn := ResidualGather16x16PtrFast
			if c.w == 8 {
				fn = ResidualGather8x8PtrFast
			}
			fn(
				(*byte)(unsafe.Pointer(&src[0])),
				stride,
				(*byte)(unsafe.Pointer(&pred[0])),
				stride,
				(*int16)(unsafe.Pointer(&out[0])),
			)
			for i, v := range out {
				if v != c.expectVal {
					t.Fatalf("%s idx %d: got %d want %d", c.name, i, v, c.expectVal)
				}
			}
		})
	}
}

func BenchmarkResidualGather16x16(b *testing.B) {
	const stride = 32
	src := make([]byte, stride*16)
	pred := make([]byte, stride*16)
	r := rand.New(rand.NewSource(1))
	for i := range src {
		src[i] = byte(r.Intn(256))
		pred[i] = byte(r.Intn(256))
	}
	out := make([]int16, 16*16)
	srcP := (*byte)(unsafe.Pointer(&src[0]))
	predP := (*byte)(unsafe.Pointer(&pred[0]))
	outP := (*int16)(unsafe.Pointer(&out[0]))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ResidualGather16x16PtrFast(srcP, stride, predP, stride, outP)
	}
}

func BenchmarkResidualGather8x8(b *testing.B) {
	const stride = 16
	src := make([]byte, stride*8)
	pred := make([]byte, stride*8)
	r := rand.New(rand.NewSource(2))
	for i := range src {
		src[i] = byte(r.Intn(256))
		pred[i] = byte(r.Intn(256))
	}
	out := make([]int16, 4*16)
	srcP := (*byte)(unsafe.Pointer(&src[0]))
	predP := (*byte)(unsafe.Pointer(&pred[0]))
	outP := (*int16)(unsafe.Pointer(&out[0]))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ResidualGather8x8PtrFast(srcP, stride, predP, stride, outP)
	}
}

func BenchmarkResidualGather16x16Scalar(b *testing.B) {
	const stride = 32
	src := make([]byte, stride*16)
	pred := make([]byte, stride*16)
	r := rand.New(rand.NewSource(1))
	for i := range src {
		src[i] = byte(r.Intn(256))
		pred[i] = byte(r.Intn(256))
	}
	out := make([]int16, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scalarResidualGather(src, stride, pred, stride, out, 16, 16)
	}
}
