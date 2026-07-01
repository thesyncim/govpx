package dsp

import (
	"math/rand/v2"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestVP9Convolve8SimdAgreement verifies VpxConvolve8Horiz / Vert / 8
// match the scalar reference on randomized inputs across the 16
// subpel positions and the full filter table.

type convCase struct {
	name string
	w, h int
}

func vp9ConvCases() []convCase {
	return []convCase{
		{"8x8", 8, 8},
		{"8x16", 8, 16},
		{"16x8", 16, 8},
		{"16x16", 16, 16},
		{"16x32", 16, 32},
		{"32x16", 32, 16},
		{"32x32", 32, 32},
		{"32x64", 32, 64},
		{"64x32", 64, 32},
		{"64x64", 64, 64},
	}
}

func filterSet() []*[tables.SubpelShifts][tables.SubpelTaps]int16 {
	return []*[tables.SubpelShifts][tables.SubpelTaps]int16{
		&tables.SubPelFilters8,
		&tables.SubPelFilters8lp,
		&tables.SubPelFilters8s,
	}
}

func runConvolveScalarHoriz(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, w, h, srcOffset int,
) {
	convolveHoriz(src, srcStride, dst, dstStride, filter, x0Q4, tables.SubpelShifts, w, h, srcOffset)
}

func runConvolveScalarVert(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	y0Q4, w, h, srcOffset int,
) {
	convolveVert(src, srcStride, dst, dstStride, filter, y0Q4, tables.SubpelShifts, w, h, srcOffset)
}

func runConvolveScalarAvgHoriz(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, w, h, srcOffset int,
) {
	convolveAvgHoriz(src, srcStride, dst, dstStride, filter, x0Q4,
		tables.SubpelShifts, w, h, srcOffset)
}

func runConvolveScalarAvgVert(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	y0Q4, w, h, srcOffset int,
) {
	convolveAvgVert(src, srcStride, dst, dstStride, filter, y0Q4,
		tables.SubpelShifts, w, h, srcOffset)
}

func TestVP9Convolve8HorizSimdAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0xb00b, 0xface))
	for _, c := range vp9ConvCases() {
		for fi, f := range filterSet() {
			t.Run(c.name, func(t *testing.T) {
				stride := c.w + 32
				margin := 16
				src := make([]byte, stride*(c.h+margin*2))
				for i := range src {
					src[i] = uint8(r.UintN(256))
				}
				for x0Q4 := range tables.SubpelShifts {
					srcOffset := margin*stride + margin
					gotDst := make([]byte, stride*c.h)
					wantDst := make([]byte, stride*c.h)
					VpxConvolve8Horiz(src, stride, gotDst, stride, f, x0Q4, tables.SubpelShifts, 0, tables.SubpelShifts, c.w, c.h, srcOffset)
					runConvolveScalarHoriz(src, stride, wantDst, stride, f, x0Q4, c.w, c.h, srcOffset)
					for y := 0; y < c.h; y++ {
						for x := 0; x < c.w; x++ {
							if gotDst[y*stride+x] != wantDst[y*stride+x] {
								t.Fatalf("filter=%d x0Q4=%d (%dx%d) at (%d,%d): got %d want %d",
									fi, x0Q4, c.w, c.h, x, y, gotDst[y*stride+x], wantDst[y*stride+x])
							}
						}
					}
				}
			})
		}
	}
}

func TestVP9Convolve8AvgHorizSimdAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0xa8a8, 0x1111))
	for _, c := range vp9ConvCases() {
		for fi, f := range filterSet() {
			t.Run(c.name, func(t *testing.T) {
				stride := c.w + 32
				margin := 16
				src := make([]byte, stride*(c.h+margin*2))
				for i := range src {
					src[i] = uint8(r.UintN(256))
				}
				for x0Q4 := range tables.SubpelShifts {
					srcOffset := margin*stride + margin
					gotDst := make([]byte, stride*c.h)
					wantDst := make([]byte, stride*c.h)
					for i := range gotDst {
						v := uint8(r.UintN(256))
						gotDst[i] = v
						wantDst[i] = v
					}
					VpxConvolve8AvgHoriz(src, stride, gotDst, stride, f,
						x0Q4, tables.SubpelShifts, 0, tables.SubpelShifts,
						c.w, c.h, srcOffset)
					runConvolveScalarAvgHoriz(src, stride, wantDst, stride, f,
						x0Q4, c.w, c.h, srcOffset)
					for y := 0; y < c.h; y++ {
						for x := 0; x < stride; x++ {
							if gotDst[y*stride+x] != wantDst[y*stride+x] {
								t.Fatalf("filter=%d x0Q4=%d (%dx%d) at (%d,%d): got %d want %d",
									fi, x0Q4, c.w, c.h, x, y, gotDst[y*stride+x], wantDst[y*stride+x])
							}
						}
					}
				}
			})
		}
	}
}

func TestVP9Convolve8VertSimdAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0xdada, 0xbeef))
	for _, c := range vp9ConvCases() {
		for fi, f := range filterSet() {
			t.Run(c.name, func(t *testing.T) {
				stride := c.w + 32
				margin := 16
				src := make([]byte, stride*(c.h+margin*2))
				for i := range src {
					src[i] = uint8(r.UintN(256))
				}
				for y0Q4 := range tables.SubpelShifts {
					srcOffset := margin*stride + margin
					gotDst := make([]byte, stride*c.h)
					wantDst := make([]byte, stride*c.h)
					VpxConvolve8Vert(src, stride, gotDst, stride, f, 0, tables.SubpelShifts, y0Q4, tables.SubpelShifts, c.w, c.h, srcOffset)
					runConvolveScalarVert(src, stride, wantDst, stride, f, y0Q4, c.w, c.h, srcOffset)
					for y := 0; y < c.h; y++ {
						for x := 0; x < c.w; x++ {
							if gotDst[y*stride+x] != wantDst[y*stride+x] {
								t.Fatalf("filter=%d y0Q4=%d (%dx%d) at (%d,%d): got %d want %d",
									fi, y0Q4, c.w, c.h, x, y, gotDst[y*stride+x], wantDst[y*stride+x])
							}
						}
					}
				}
			})
		}
	}
}

func TestVP9Convolve8AvgVertSimdAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0xa8a8, 0x2222))
	for _, c := range vp9ConvCases() {
		for fi, f := range filterSet() {
			t.Run(c.name, func(t *testing.T) {
				stride := c.w + 32
				margin := 16
				src := make([]byte, stride*(c.h+margin*2))
				for i := range src {
					src[i] = uint8(r.UintN(256))
				}
				for y0Q4 := range tables.SubpelShifts {
					srcOffset := margin*stride + margin
					gotDst := make([]byte, stride*c.h)
					wantDst := make([]byte, stride*c.h)
					for i := range gotDst {
						v := uint8(r.UintN(256))
						gotDst[i] = v
						wantDst[i] = v
					}
					VpxConvolve8AvgVert(src, stride, gotDst, stride, f, 0,
						tables.SubpelShifts, y0Q4, tables.SubpelShifts,
						c.w, c.h, srcOffset)
					runConvolveScalarAvgVert(src, stride, wantDst, stride, f,
						y0Q4, c.w, c.h, srcOffset)
					for y := 0; y < c.h; y++ {
						for x := 0; x < stride; x++ {
							if gotDst[y*stride+x] != wantDst[y*stride+x] {
								t.Fatalf("filter=%d y0Q4=%d (%dx%d) at (%d,%d): got %d want %d",
									fi, y0Q4, c.w, c.h, x, y, gotDst[y*stride+x], wantDst[y*stride+x])
							}
						}
					}
				}
			})
		}
	}
}

func TestVP9Convolve8FullSimdAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0xfeed, 0xc0de))
	cases := []convCase{
		{"8x8", 8, 8},
		{"16x16", 16, 16},
		{"32x32", 32, 32},
		{"64x64", 64, 64},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stride := c.w + 32
			margin := 16
			src := make([]byte, stride*(c.h+margin*2))
			for i := range src {
				src[i] = uint8(r.UintN(256))
			}
			for trial := range 6 {
				x0Q4 := int(r.UintN(tables.SubpelShifts))
				y0Q4 := int(r.UintN(tables.SubpelShifts))
				f := &tables.SubPelFilters8
				srcOffset := margin*stride + margin
				gotDst := make([]byte, stride*c.h)
				wantDst := make([]byte, stride*c.h)
				VpxConvolve8(src, stride, gotDst, stride, f, x0Q4, tables.SubpelShifts, y0Q4, tables.SubpelShifts, c.w, c.h, srcOffset)
				// Reference scalar via direct two-pass.
				var temp [64 * 135]byte
				intH := (((c.h-1)*tables.SubpelShifts + y0Q4) >> tables.SubpelBits) + tables.SubpelTaps
				convolveHoriz(src, stride, temp[:], 64, f, x0Q4, tables.SubpelShifts, c.w, intH, srcOffset-stride*(tables.SubpelTaps/2-1))
				convolveVert(temp[:], 64, wantDst, stride, f, y0Q4, tables.SubpelShifts, c.w, c.h, 64*(tables.SubpelTaps/2-1))
				for y := 0; y < c.h; y++ {
					for x := 0; x < c.w; x++ {
						if gotDst[y*stride+x] != wantDst[y*stride+x] {
							t.Fatalf("trial=%d x0Q4=%d y0Q4=%d (%dx%d) at (%d,%d): got %d want %d",
								trial, x0Q4, y0Q4, c.w, c.h, x, y, gotDst[y*stride+x], wantDst[y*stride+x])
						}
					}
				}
			}
		})
	}
}

func TestVP9Convolve8SimdEdgeCases(t *testing.T) {
	cases := []convCase{
		{"8x8", 8, 8},
		{"16x16", 16, 16},
		{"32x32", 32, 32},
		{"64x64", 64, 64},
	}
	fills := []struct {
		name string
		fill uint8
	}{
		{"zero", 0},
		{"max", 255},
		{"mid", 128},
	}
	for _, c := range cases {
		for _, fc := range fills {
			t.Run(c.name+"_"+fc.name, func(t *testing.T) {
				stride := c.w + 32
				margin := 16
				src := make([]byte, stride*(c.h+margin*2))
				for i := range src {
					src[i] = fc.fill
				}
				srcOffset := margin*stride + margin
				f := &tables.SubPelFilters8
				gotH := make([]byte, stride*c.h)
				wantH := make([]byte, stride*c.h)
				for _, q4 := range []int{0, 1, 7, 8, 15} {
					VpxConvolve8Horiz(src, stride, gotH, stride, f, q4, tables.SubpelShifts, 0, tables.SubpelShifts, c.w, c.h, srcOffset)
					runConvolveScalarHoriz(src, stride, wantH, stride, f, q4, c.w, c.h, srcOffset)
					for y := 0; y < c.h; y++ {
						for x := 0; x < c.w; x++ {
							if gotH[y*stride+x] != wantH[y*stride+x] {
								t.Fatalf("fill=%s q4=%d (%dx%d) horiz at (%d,%d): got %d want %d",
									fc.name, q4, c.w, c.h, x, y, gotH[y*stride+x], wantH[y*stride+x])
							}
						}
					}
					VpxConvolve8Vert(src, stride, gotH, stride, f, 0, tables.SubpelShifts, q4, tables.SubpelShifts, c.w, c.h, srcOffset)
					runConvolveScalarVert(src, stride, wantH, stride, f, q4, c.w, c.h, srcOffset)
					for y := 0; y < c.h; y++ {
						for x := 0; x < c.w; x++ {
							if gotH[y*stride+x] != wantH[y*stride+x] {
								t.Fatalf("fill=%s q4=%d (%dx%d) vert at (%d,%d): got %d want %d",
									fc.name, q4, c.w, c.h, x, y, gotH[y*stride+x], wantH[y*stride+x])
							}
						}
					}
				}
			})
		}
	}
}

func TestVP9ConvolveAvgSimdAgreement(t *testing.T) {
	r := rand.New(rand.NewPCG(0xa66a, 0x5eed))
	cases := []convCase{
		{"4x4_scalar_fallback", 4, 4},
		{"8x8", 8, 8},
		{"16x16", 16, 16},
		{"32x16", 32, 16},
		{"64x64", 64, 64},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srcStride := c.w + 37
			dstStride := c.w + 29
			margin := 4
			src := make([]byte, srcStride*(c.h+margin*2))
			gotDst := make([]byte, dstStride*c.h)
			wantDst := make([]byte, dstStride*c.h)
			for i := range src {
				src[i] = uint8(r.UintN(256))
			}
			for i := range gotDst {
				v := uint8(r.UintN(256))
				gotDst[i] = v
				wantDst[i] = v
			}
			srcOffset := margin*srcStride + 11
			VpxConvolveAvg(src, srcStride, gotDst, dstStride, c.w, c.h, srcOffset)
			vpxConvolveAvgScalar(src, srcStride, wantDst, dstStride, c.w, c.h, srcOffset)
			for y := 0; y < c.h; y++ {
				for x := 0; x < dstStride; x++ {
					if gotDst[y*dstStride+x] != wantDst[y*dstStride+x] {
						t.Fatalf("(%dx%d) at (%d,%d): got %d want %d",
							c.w, c.h, x, y, gotDst[y*dstStride+x], wantDst[y*dstStride+x])
					}
				}
			}
		})
	}
}

func drainConvolve8AvgTempPool(t *testing.T) {
	t.Helper()
	drained := make([]*convolve8AvgTempBuf, 0, cap(convolve8AvgTempPool))
	for {
		select {
		case b := <-convolve8AvgTempPool:
			drained = append(drained, b)
		default:
			t.Cleanup(func() {
				for _, b := range drained {
					convolve8AvgTempPut(b)
				}
			})
			return
		}
	}
}

func TestVP9Convolve8AvgAxisWithScratchDoesNotAllocateUnderPoolPressure(t *testing.T) {
	drainConvolve8AvgTempPool(t)
	stride := 64
	margin := 16
	src := make([]byte, stride*(16+margin*2))
	dstH := make([]byte, stride*16)
	dstV := make([]byte, stride*16)
	for i := range src {
		src[i] = uint8((i*7 + 3) & 0xff)
	}
	for i := range dstH {
		v := uint8((i*13 + 5) & 0xff)
		dstH[i] = v
		dstV[i] = v
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	var scratch Convolve8Scratch

	allocs := testing.AllocsPerRun(1000, func() {
		VpxConvolve8AvgHorizWithScratch(src, stride, dstH, stride, f, 5,
			tables.SubpelShifts, 0, tables.SubpelShifts, 16, 16, srcOffset, &scratch)
		VpxConvolve8AvgVertWithScratch(src, stride, dstV, stride, f, 0,
			tables.SubpelShifts, 5, tables.SubpelShifts, 16, 16, srcOffset, &scratch)
	})
	if allocs != 0 {
		t.Fatalf("VpxConvolve8Avg*WithScratch allocations = %.1f, want 0", allocs)
	}
}

func BenchmarkVP9Convolve8Horiz16x16(b *testing.B) {
	stride := 64
	margin := 16
	src := make([]byte, stride*(16+margin*2))
	dst := make([]byte, stride*16)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8Horiz(src, stride, dst, stride, f, 5, tables.SubpelShifts, 0, tables.SubpelShifts, 16, 16, srcOffset)
	}
}

func BenchmarkVP9Convolve8Vert16x16(b *testing.B) {
	stride := 64
	margin := 16
	src := make([]byte, stride*(16+margin*2))
	dst := make([]byte, stride*16)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8Vert(src, stride, dst, stride, f, 0, tables.SubpelShifts, 5, tables.SubpelShifts, 16, 16, srcOffset)
	}
}

func BenchmarkVP9Convolve8Horiz64x64(b *testing.B) {
	stride := 128
	margin := 16
	src := make([]byte, stride*(64+margin*2))
	dst := make([]byte, stride*64)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8Horiz(src, stride, dst, stride, f, 5, tables.SubpelShifts, 0, tables.SubpelShifts, 64, 64, srcOffset)
	}
}

func BenchmarkVP9Convolve8Vert64x64(b *testing.B) {
	stride := 128
	margin := 16
	src := make([]byte, stride*(64+margin*2))
	dst := make([]byte, stride*64)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8Vert(src, stride, dst, stride, f, 0, tables.SubpelShifts, 5, tables.SubpelShifts, 64, 64, srcOffset)
	}
}

func BenchmarkVP9Convolve8AvgHoriz16x16(b *testing.B) {
	stride := 64
	margin := 16
	src := make([]byte, stride*(16+margin*2))
	dst := make([]byte, stride*16)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8AvgHoriz(src, stride, dst, stride, f, 5,
			tables.SubpelShifts, 0, tables.SubpelShifts, 16, 16, srcOffset)
	}
}

func BenchmarkVP9Convolve8AvgHoriz16x16Scratch(b *testing.B) {
	stride := 64
	margin := 16
	src := make([]byte, stride*(16+margin*2))
	dst := make([]byte, stride*16)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	var scratch Convolve8Scratch
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8AvgHorizWithScratch(src, stride, dst, stride, f, 5,
			tables.SubpelShifts, 0, tables.SubpelShifts, 16, 16, srcOffset, &scratch)
	}
}

func BenchmarkVP9Convolve8AvgVert16x16(b *testing.B) {
	stride := 64
	margin := 16
	src := make([]byte, stride*(16+margin*2))
	dst := make([]byte, stride*16)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8AvgVert(src, stride, dst, stride, f, 0,
			tables.SubpelShifts, 5, tables.SubpelShifts, 16, 16, srcOffset)
	}
}

func BenchmarkVP9Convolve8AvgVert16x16Scratch(b *testing.B) {
	stride := 64
	margin := 16
	src := make([]byte, stride*(16+margin*2))
	dst := make([]byte, stride*16)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	var scratch Convolve8Scratch
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8AvgVertWithScratch(src, stride, dst, stride, f, 0,
			tables.SubpelShifts, 5, tables.SubpelShifts, 16, 16, srcOffset, &scratch)
	}
}

func BenchmarkVP9Convolve8AvgHoriz64x64(b *testing.B) {
	stride := 128
	margin := 16
	src := make([]byte, stride*(64+margin*2))
	dst := make([]byte, stride*64)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8AvgHoriz(src, stride, dst, stride, f, 5,
			tables.SubpelShifts, 0, tables.SubpelShifts, 64, 64, srcOffset)
	}
}

func BenchmarkVP9Convolve8AvgVert64x64(b *testing.B) {
	stride := 128
	margin := 16
	src := make([]byte, stride*(64+margin*2))
	dst := make([]byte, stride*64)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8AvgVert(src, stride, dst, stride, f, 0,
			tables.SubpelShifts, 5, tables.SubpelShifts, 64, 64, srcOffset)
	}
}

func BenchmarkVP9Convolve8Full16x16(b *testing.B) {
	stride := 64
	margin := 16
	src := make([]byte, stride*(16+margin*2))
	dst := make([]byte, stride*16)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8(src, stride, dst, stride, f, 5, tables.SubpelShifts, 5, tables.SubpelShifts, 16, 16, srcOffset)
	}
}

func BenchmarkVP9ConvolveAvg8x8(b *testing.B) {
	stride := 64
	src := make([]byte, stride*8)
	dst := make([]byte, stride*8)
	for i := range src {
		src[i] = uint8(i & 0xff)
		dst[i] = uint8((i * 3) & 0xff)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolveAvg(src, stride, dst, stride, 8, 8, 0)
	}
}

func BenchmarkVP9ConvolveAvg16x16(b *testing.B) {
	stride := 64
	src := make([]byte, stride*16)
	dst := make([]byte, stride*16)
	for i := range src {
		src[i] = uint8(i & 0xff)
		dst[i] = uint8((i * 3) & 0xff)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolveAvg(src, stride, dst, stride, 16, 16, 0)
	}
}

func BenchmarkVP9ConvolveAvg64x64(b *testing.B) {
	stride := 128
	src := make([]byte, stride*64)
	dst := make([]byte, stride*64)
	for i := range src {
		src[i] = uint8(i & 0xff)
		dst[i] = uint8((i * 3) & 0xff)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolveAvg(src, stride, dst, stride, 64, 64, 0)
	}
}

func BenchmarkVP9Convolve8AvgFull16x16(b *testing.B) {
	stride := 64
	margin := 16
	src := make([]byte, stride*(16+margin*2))
	dst := make([]byte, stride*16)
	for i := range src {
		src[i] = uint8(i & 0xff)
	}
	srcOffset := margin*stride + margin
	f := &tables.SubPelFilters8
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VpxConvolve8Avg(src, stride, dst, stride, f, 5, tables.SubpelShifts, 5, tables.SubpelShifts, 16, 16, srcOffset)
	}
}
