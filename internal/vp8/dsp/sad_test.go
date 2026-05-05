package dsp

import "testing"

func TestSADBlocks(t *testing.T) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src[y*32+x] = byte(x + y)
			ref[y*32+x] = byte(x*2 + y)
		}
	}

	if got, want := SAD16x16(src, 32, ref, 32), scalarSAD(src, 32, ref, 32, 16, 16); got != want {
		t.Fatalf("SAD16x16 = %d, want %d", got, want)
	}
	if got, want := SAD8x8(src[3*32+5:], 32, ref[4*32+7:], 32), scalarSAD(src[3*32+5:], 32, ref[4*32+7:], 32, 8, 8); got != want {
		t.Fatalf("SAD8x8 = %d, want %d", got, want)
	}
	if got, want := SAD4x4(src[11*32+13:], 32, ref[9*32+2:], 32), scalarSAD(src[11*32+13:], 32, ref[9*32+2:], 32, 4, 4); got != want {
		t.Fatalf("SAD4x4 = %d, want %d", got, want)
	}
}

func TestSADAllocatesZero(t *testing.T) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	allocs := testing.AllocsPerRun(1000, func() {
		_ = SAD16x16(src, 32, ref, 32)
		_ = SAD8x8(src, 32, ref, 32)
		_ = SAD4x4(src, 32, ref, 32)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkSAD16x16(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(16 * 16)
	for i := 0; i < b.N; i++ {
		_ = SAD16x16(src, 32, ref, 32)
	}
}

func BenchmarkSAD8x8(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(8 * 8)
	for i := 0; i < b.N; i++ {
		_ = SAD8x8(src, 32, ref, 32)
	}
}

func BenchmarkSAD4x4(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(4 * 4)
	for i := 0; i < b.N; i++ {
		_ = SAD4x4(src, 32, ref, 32)
	}
}

func scalarSAD(src []byte, srcStride int, ref []byte, refStride int, width int, height int) int {
	sad := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			diff := int(src[y*srcStride+x]) - int(ref[y*refStride+x])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
	}
	return sad
}
