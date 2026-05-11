package dsp

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestBilinearPredict4x4Horizontal(t *testing.T) {
	src := makeGradient(8, 8)
	dst := make([]byte, 4*4)

	BilinearPredict4x4(src, 8, 4, 0, dst, 4)

	for y := range 4 {
		for x := range 4 {
			a := int(src[y*8+x])
			b := int(src[y*8+x+1])
			want := byte((a + b + 1) >> 1)
			if got := dst[y*4+x]; got != want {
				t.Fatalf("dst[%d,%d] = %d, want %d", x, y, got, want)
			}
		}
	}
}

func TestBilinearPredict4x4Vertical(t *testing.T) {
	src := makeGradient(8, 8)
	dst := make([]byte, 4*4)

	BilinearPredict4x4(src, 8, 0, 4, dst, 4)

	for y := range 4 {
		for x := range 4 {
			a := int(src[y*8+x])
			b := int(src[(y+1)*8+x])
			want := byte((a + b + 1) >> 1)
			if got := dst[y*4+x]; got != want {
				t.Fatalf("dst[%d,%d] = %d, want %d", x, y, got, want)
			}
		}
	}
}

func TestBilinearPredict16x16AllocatesZero(t *testing.T) {
	src := makeGradient(32, 32)
	dst := make([]byte, 32*32)
	allocs := testing.AllocsPerRun(1000, func() {
		BilinearPredict16x16(src, 32, 3, 5, dst, 32)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestSixTapPredict16x16ZeroOffsetsCopiesCentralBlock(t *testing.T) {
	const stride = 32
	src := makeSixTapSource(stride, 21)
	dst := make([]byte, 16*16)

	SixTapPredict16x16(src, stride, 0, 0, dst, 16)

	for y := range 16 {
		for x := range 16 {
			want := src[(y+2)*stride+x+2]
			if got := dst[y*16+x]; got != want {
				t.Fatalf("dst[%d,%d] = %d, want %d", x, y, got, want)
			}
		}
	}
}

func TestSixTapPredictRectangularMatchesScalar(t *testing.T) {
	const stride = 32
	cases := []struct {
		name string
		w, h int
		fn   func([]byte, int, int, int, []byte, int)
	}{
		{"16x8", 16, 8, SixTapPredict16x8},
		{"8x16", 8, 16, SixTapPredict8x16},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := makeSixTapSource(stride, tc.h+5)
			dst := make([]byte, tc.h*tc.w)
			want := make([]byte, tc.h*tc.w)

			for xoff := range 8 {
				for yoff := range 8 {
					clear(dst)
					clear(want)

					tc.fn(src, stride, xoff, yoff, dst, tc.w)
					sixTapPredict(src, stride, xoff, yoff, want, tc.w, tc.w, tc.h)

					for i := range want {
						if dst[i] != want[i] {
							y := i / tc.w
							x := i % tc.w
							t.Fatalf("xoff=%d yoff=%d dst[%d,%d] = %d, want %d",
								xoff, yoff, x, y, dst[i], want[i])
						}
					}
				}
			}
		})
	}
}

func TestSixTapPredictHorizontalHalfPixel(t *testing.T) {
	const stride = 16
	src := makeSixTapSource(stride, 9)
	dst := make([]byte, 4*4)
	filter := tables.SubPelFilters[4]

	SixTapPredict4x4(src, stride, 4, 0, dst, 4)

	for y := range 4 {
		for x := range 4 {
			row := (y + 2) * stride
			v := int(src[row+x+0])*int(filter[0]) +
				int(src[row+x+1])*int(filter[1]) +
				int(src[row+x+2])*int(filter[2]) +
				int(src[row+x+3])*int(filter[3]) +
				int(src[row+x+4])*int(filter[4]) +
				int(src[row+x+5])*int(filter[5]) +
				tables.FilterWeight/2
			want := ClipPixel(v >> tables.FilterShift)
			if got := dst[y*4+x]; got != want {
				t.Fatalf("dst[%d,%d] = %d, want %d", x, y, got, want)
			}
		}
	}
}

func TestSixTapPredictVerticalHalfPixel(t *testing.T) {
	const stride = 16
	src := makeSixTapSource(stride, 9)
	dst := make([]byte, 4*4)
	filter := tables.SubPelFilters[4]

	SixTapPredict4x4(src, stride, 0, 4, dst, 4)

	for y := range 4 {
		for x := range 4 {
			col := x + 2
			v := int(src[(y+0)*stride+col])*int(filter[0]) +
				int(src[(y+1)*stride+col])*int(filter[1]) +
				int(src[(y+2)*stride+col])*int(filter[2]) +
				int(src[(y+3)*stride+col])*int(filter[3]) +
				int(src[(y+4)*stride+col])*int(filter[4]) +
				int(src[(y+5)*stride+col])*int(filter[5]) +
				tables.FilterWeight/2
			want := ClipPixel(v >> tables.FilterShift)
			if got := dst[y*4+x]; got != want {
				t.Fatalf("dst[%d,%d] = %d, want %d", x, y, got, want)
			}
		}
	}
}

func TestSixTapPredict16x16AllocatesZero(t *testing.T) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	allocs := testing.AllocsPerRun(1000, func() {
		SixTapPredict16x16(src, 32, 3, 5, dst, 32)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkBilinearPredict16x16(b *testing.B) {
	src := makeGradient(32, 32)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		BilinearPredict16x16(src, 32, 3, 5, dst, 32)
	}
}

func BenchmarkBilinearPredict8x8(b *testing.B) {
	src := makeGradient(32, 32)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		BilinearPredict8x8(src, 32, 3, 5, dst, 32)
	}
}

func BenchmarkBilinearPredict8x4(b *testing.B) {
	src := makeGradient(32, 32)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		BilinearPredict8x4(src, 32, 3, 5, dst, 32)
	}
}

func BenchmarkBilinearPredict4x4(b *testing.B) {
	src := makeGradient(32, 32)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		BilinearPredict4x4(src, 32, 3, 5, dst, 32)
	}
}

func BenchmarkSixTapPredict16x16(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict16x16(src, 32, 3, 5, dst, 32)
	}
}

func BenchmarkSixTapPredict16x16ZeroOffset(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict16x16(src, 32, 0, 0, dst, 32)
	}
}

func BenchmarkSixTapPredict16x8(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict16x8(src, 32, 3, 5, dst, 32)
	}
}

func BenchmarkSixTapPredict16x8ZeroOffset(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict16x8(src, 32, 0, 0, dst, 32)
	}
}

func BenchmarkSixTapPredict8x16(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict8x16(src, 32, 3, 5, dst, 32)
	}
}

func BenchmarkSixTapPredict8x16ZeroOffset(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict8x16(src, 32, 0, 0, dst, 32)
	}
}

func BenchmarkSixTapPredict8x8(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict8x8(src, 32, 3, 5, dst, 32)
	}
}

func BenchmarkSixTapPredict8x8ZeroOffset(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict8x8(src, 32, 0, 0, dst, 32)
	}
}

func BenchmarkSixTapPredict8x4(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict8x4(src, 32, 3, 5, dst, 32)
	}
}

func BenchmarkSixTapPredict8x4ZeroOffset(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict8x4(src, 32, 0, 0, dst, 32)
	}
}

func BenchmarkSixTapPredict4x4(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict4x4(src, 32, 3, 5, dst, 32)
	}
}

func BenchmarkSixTapPredict4x4ZeroOffset(b *testing.B) {
	src := makeSixTapSource(32, 21)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		SixTapPredict4x4(src, 32, 0, 0, dst, 32)
	}
}

func makeGradient(width int, height int) []byte {
	buf := make([]byte, width*height)
	for y := range height {
		for x := range width {
			buf[y*width+x] = byte((x + y*7) & 255)
		}
	}
	return buf
}

func makeSixTapSource(stride int, rows int) []byte {
	buf := make([]byte, stride*rows)
	for y := range rows {
		for x := range stride {
			buf[y*stride+x] = byte((x*11 + y*17 + x*y*3) & 255)
		}
	}
	return buf
}
