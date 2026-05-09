package dsp

import "testing"

func TestSADBlocks(t *testing.T) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	for y := range 32 {
		for x := range 32 {
			src[y*32+x] = byte(x + y)
			ref[y*32+x] = byte(x*2 + y)
		}
	}

	if got, want := SAD16x16(src, 32, ref, 32), scalarSAD(src, 32, ref, 32, 16, 16); got != want {
		t.Fatalf("SAD16x16 = %d, want %d", got, want)
	}
	if got, want := SAD16x8(src[2*32+3:], 32, ref[5*32+1:], 32), scalarSAD(src[2*32+3:], 32, ref[5*32+1:], 32, 16, 8); got != want {
		t.Fatalf("SAD16x8 = %d, want %d", got, want)
	}
	if got, want := SAD8x16(src[1*32+9:], 32, ref[6*32+4:], 32), scalarSAD(src[1*32+9:], 32, ref[6*32+4:], 32, 8, 16); got != want {
		t.Fatalf("SAD8x16 = %d, want %d", got, want)
	}
	if got, want := SAD8x8(src[3*32+5:], 32, ref[4*32+7:], 32), scalarSAD(src[3*32+5:], 32, ref[4*32+7:], 32, 8, 8); got != want {
		t.Fatalf("SAD8x8 = %d, want %d", got, want)
	}
	if got, want := SAD4x4(src[11*32+13:], 32, ref[9*32+2:], 32), scalarSAD(src[11*32+13:], 32, ref[9*32+2:], 32, 4, 4); got != want {
		t.Fatalf("SAD4x4 = %d, want %d", got, want)
	}
}

func TestSAD16x16Limit(t *testing.T) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	for i := range src {
		src[i] = byte(i)
		ref[i] = byte(i * 3)
	}
	want := SAD16x16(src, 32, ref, 32)
	if got := SAD16x16Limit(src, 32, ref, 32, want); got != want {
		t.Fatalf("SAD16x16Limit at exact limit = %d, want %d", got, want)
	}
	if got := SAD16x16Limit(src, 32, ref, 32, want+1); got != want {
		t.Fatalf("SAD16x16Limit above limit = %d, want %d", got, want)
	}
	low := want / 4
	if got := SAD16x16Limit(src, 32, ref, 32, low); got <= low {
		t.Fatalf("SAD16x16Limit below limit = %d, want above %d", got, low)
	}
}

func TestSADAllocatesZero(t *testing.T) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	allocs := testing.AllocsPerRun(1000, func() {
		_ = SAD16x16(src, 32, ref, 32)
		_ = SAD16x16Limit(src, 32, ref, 32, 1)
		_ = SAD16x8(src, 32, ref, 32)
		_ = SAD8x16(src, 32, ref, 32)
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

func BenchmarkSAD16x16LimitEarly(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	for i := range ref {
		ref[i] = 255
	}
	b.ReportAllocs()
	b.SetBytes(16 * 16)
	for i := 0; i < b.N; i++ {
		_ = SAD16x16Limit(src, 32, ref, 32, 1024)
	}
}

func BenchmarkSAD16x8(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(16 * 8)
	for i := 0; i < b.N; i++ {
		_ = SAD16x8(src, 32, ref, 32)
	}
}

func BenchmarkSAD8x16(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(8 * 16)
	for i := 0; i < b.N; i++ {
		_ = SAD8x16(src, 32, ref, 32)
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
	for y := range height {
		for x := range width {
			diff := int(src[y*srcStride+x]) - int(ref[y*refStride+x])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
	}
	return sad
}

func BenchmarkSAD16x16NEON(b *testing.B) {
	src, ref := benchSAD16x16Source()
	for i := 0; i < b.N; i++ {
		_ = SAD16x16(src, 64, ref, 64)
	}
}

func BenchmarkSAD16x16Generic(b *testing.B) {
	src, ref := benchSAD16x16Source()
	for i := 0; i < b.N; i++ {
		_ = sadScalarReference16x16(src, 64, ref, 64)
	}
}

func benchSAD16x16Source() ([]byte, []byte) {
	src := make([]byte, 64*16)
	ref := make([]byte, 64*16)
	for i := range src {
		src[i] = byte(7 + i*3)
		ref[i] = byte(11 + i*5)
	}
	return src, ref
}

func sadScalarReference16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	sad := 0
	for y := range 16 {
		s := src[y*srcStride:]
		r := ref[y*refStride:]
		for x := range 16 {
			d := int(s[x]) - int(r[x])
			if d < 0 {
				d = -d
			}
			sad += d
		}
	}
	return sad
}
