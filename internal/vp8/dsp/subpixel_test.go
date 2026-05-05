package dsp

import "testing"

func TestBilinearPredict4x4Horizontal(t *testing.T) {
	src := makeGradient(8, 8)
	dst := make([]byte, 4*4)

	BilinearPredict4x4(src, 8, 4, 0, dst, 4)

	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
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

	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
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

func BenchmarkBilinearPredict16x16(b *testing.B) {
	src := makeGradient(32, 32)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		BilinearPredict16x16(src, 32, 3, 5, dst, 32)
	}
}

func makeGradient(width int, height int) []byte {
	buf := make([]byte, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			buf[y*width+x] = byte((x + y*7) & 255)
		}
	}
	return buf
}
