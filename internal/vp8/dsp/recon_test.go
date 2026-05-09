package dsp

import "testing"

func TestCopyBlocks(t *testing.T) {
	src := make([]byte, 32*32)
	dst := make([]byte, 32*32)
	for i := range src {
		src[i] = byte(i)
	}

	Copy16x16(src, 32, dst, 32)
	for y := range 16 {
		for x := range 16 {
			if dst[y*32+x] != src[y*32+x] {
				t.Fatalf("dst[%d,%d] = %d, want %d", x, y, dst[y*32+x], src[y*32+x])
			}
		}
	}
	if dst[16] != 0 {
		t.Fatalf("copy wrote past 16-pixel row")
	}
}

func TestAddResidual4x4(t *testing.T) {
	dst := []byte{
		10, 20, 30, 40, 99,
		50, 60, 70, 80, 99,
		90, 100, 110, 120, 99,
		130, 140, 150, 160, 99,
	}
	residual := [16]int16{
		-20, -1, 1, 300,
		5, -80, 10, 20,
		-90, 30, -200, 1,
		125, 126, 127, 128,
	}

	AddResidual4x4(dst, 5, &residual)

	want := []byte{
		0, 19, 31, 255, 99,
		55, 0, 80, 100, 99,
		0, 130, 0, 121, 99,
		255, 255, 255, 255, 99,
	}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("dst[%d] = %d, want %d", i, dst[i], want[i])
		}
	}
}

func TestReconAllocatesZero(t *testing.T) {
	src := make([]byte, 32*32)
	dst := make([]byte, 32*32)
	residual := [16]int16{}
	allocs := testing.AllocsPerRun(1000, func() {
		Copy4x4(src, 32, dst, 32)
		AddResidual4x4(dst, 32, &residual)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkCopy16x16(b *testing.B) {
	src := make([]byte, 32*32)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Copy16x16(src, 32, dst, 32)
	}
}

func BenchmarkCopy8x8(b *testing.B) {
	src := make([]byte, 32*32)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Copy8x8(src, 32, dst, 32)
	}
}

func BenchmarkCopy8x4(b *testing.B) {
	src := make([]byte, 32*32)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Copy8x4(src, 32, dst, 32)
	}
}

func BenchmarkCopy4x4(b *testing.B) {
	src := make([]byte, 32*32)
	dst := make([]byte, 32*32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Copy4x4(src, 32, dst, 32)
	}
}

func BenchmarkAddResidual4x4(b *testing.B) {
	dst := make([]byte, 32*32)
	residual := [16]int16{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		AddResidual4x4(dst, 32, &residual)
	}
}
