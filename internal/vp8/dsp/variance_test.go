package dsp

import "testing"

func TestVarianceBlocks(t *testing.T) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src[y*32+x] = byte(17 + x*3 + y)
			ref[y*32+x] = byte(11 + x + y*2)
		}
	}

	sum, sse := scalarVariance(src, 32, ref, 32, 16, 16)
	if got := SSE16x16(src, 32, ref, 32); got != sse {
		t.Fatalf("SSE16x16 = %d, want %d", got, sse)
	}
	if got, want := Variance16x16(src, 32, ref, 32), sse-(sum*sum>>8); got != want {
		t.Fatalf("Variance16x16 = %d, want %d", got, want)
	}

	sum, sse = scalarVariance(src[2*32+3:], 32, ref[5*32+1:], 32, 16, 8)
	if got := SSE16x8(src[2*32+3:], 32, ref[5*32+1:], 32); got != sse {
		t.Fatalf("SSE16x8 = %d, want %d", got, sse)
	}
	if got, want := Variance16x8(src[2*32+3:], 32, ref[5*32+1:], 32), sse-(sum*sum>>7); got != want {
		t.Fatalf("Variance16x8 = %d, want %d", got, want)
	}

	sum, sse = scalarVariance(src[1*32+9:], 32, ref[6*32+4:], 32, 8, 16)
	if got := SSE8x16(src[1*32+9:], 32, ref[6*32+4:], 32); got != sse {
		t.Fatalf("SSE8x16 = %d, want %d", got, sse)
	}
	if got, want := Variance8x16(src[1*32+9:], 32, ref[6*32+4:], 32), sse-(sum*sum>>7); got != want {
		t.Fatalf("Variance8x16 = %d, want %d", got, want)
	}

	sum, sse = scalarVariance(src[3*32+5:], 32, ref[4*32+7:], 32, 8, 8)
	if got := SSE8x8(src[3*32+5:], 32, ref[4*32+7:], 32); got != sse {
		t.Fatalf("SSE8x8 = %d, want %d", got, sse)
	}
	if got, want := Variance8x8(src[3*32+5:], 32, ref[4*32+7:], 32), sse-(sum*sum>>6); got != want {
		t.Fatalf("Variance8x8 = %d, want %d", got, want)
	}

	sum, sse = scalarVariance(src[11*32+13:], 32, ref[9*32+2:], 32, 4, 4)
	if got := SSE4x4(src[11*32+13:], 32, ref[9*32+2:], 32); got != sse {
		t.Fatalf("SSE4x4 = %d, want %d", got, sse)
	}
	if got, want := Variance4x4(src[11*32+13:], 32, ref[9*32+2:], 32), sse-(sum*sum>>4); got != want {
		t.Fatalf("Variance4x4 = %d, want %d", got, want)
	}
}

func TestVarianceAllocatesZero(t *testing.T) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	allocs := testing.AllocsPerRun(1000, func() {
		_ = SSE16x16(src, 32, ref, 32)
		_ = Variance16x16(src, 32, ref, 32)
		_ = SSE16x8(src, 32, ref, 32)
		_ = Variance16x8(src, 32, ref, 32)
		_ = SSE8x16(src, 32, ref, 32)
		_ = Variance8x16(src, 32, ref, 32)
		_ = SSE8x8(src, 32, ref, 32)
		_ = Variance8x8(src, 32, ref, 32)
		_ = SSE4x4(src, 32, ref, 32)
		_ = Variance4x4(src, 32, ref, 32)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkSSE16x16(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(16 * 16)
	for i := 0; i < b.N; i++ {
		_ = SSE16x16(src, 32, ref, 32)
	}
}

func BenchmarkSSE16x8(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(16 * 8)
	for i := 0; i < b.N; i++ {
		_ = SSE16x8(src, 32, ref, 32)
	}
}

func BenchmarkSSE8x16(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(8 * 16)
	for i := 0; i < b.N; i++ {
		_ = SSE8x16(src, 32, ref, 32)
	}
}

func BenchmarkVariance16x16(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(16 * 16)
	for i := 0; i < b.N; i++ {
		_ = Variance16x16(src, 32, ref, 32)
	}
}

func BenchmarkVariance16x8(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(16 * 8)
	for i := 0; i < b.N; i++ {
		_ = Variance16x8(src, 32, ref, 32)
	}
}

func BenchmarkVariance8x16(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(8 * 16)
	for i := 0; i < b.N; i++ {
		_ = Variance8x16(src, 32, ref, 32)
	}
}

func BenchmarkVariance8x8(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(8 * 8)
	for i := 0; i < b.N; i++ {
		_ = Variance8x8(src, 32, ref, 32)
	}
}

func BenchmarkVariance4x4(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(4 * 4)
	for i := 0; i < b.N; i++ {
		_ = Variance4x4(src, 32, ref, 32)
	}
}

func scalarVariance(src []byte, srcStride int, ref []byte, refStride int, width int, height int) (int, int) {
	sum := 0
	sse := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			diff := int(src[y*srcStride+x]) - int(ref[y*refStride+x])
			sum += diff
			sse += diff * diff
		}
	}
	return sum, sse
}
