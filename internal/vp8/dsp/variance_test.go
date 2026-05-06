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

	sum, sse = scalarVariance(src[6*32+8:], 32, ref[7*32+3:], 32, 8, 4)
	if got := SSE8x4(src[6*32+8:], 32, ref[7*32+3:], 32); got != sse {
		t.Fatalf("SSE8x4 = %d, want %d", got, sse)
	}
	if got, want := Variance8x4(src[6*32+8:], 32, ref[7*32+3:], 32), sse-(sum*sum>>5); got != want {
		t.Fatalf("Variance8x4 = %d, want %d", got, want)
	}

	sum, sse = scalarVariance(src[4*32+12:], 32, ref[6*32+9:], 32, 4, 8)
	if got := SSE4x8(src[4*32+12:], 32, ref[6*32+9:], 32); got != sse {
		t.Fatalf("SSE4x8 = %d, want %d", got, sse)
	}
	if got, want := Variance4x8(src[4*32+12:], 32, ref[6*32+9:], 32), sse-(sum*sum>>5); got != want {
		t.Fatalf("Variance4x8 = %d, want %d", got, want)
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
		_ = SSE8x4(src, 32, ref, 32)
		_ = Variance8x4(src, 32, ref, 32)
		_ = SSE4x8(src, 32, ref, 32)
		_ = Variance4x8(src, 32, ref, 32)
		_ = SSE4x4(src, 32, ref, 32)
		_ = Variance4x4(src, 32, ref, 32)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestSubpelVarianceMatchesLibvpxC(t *testing.T) {
	src := makeSubpelVarianceSource()
	ref := makeSubpelVarianceReference()
	tests := []struct {
		name     string
		srcStart int
		refStart int
		xOffset  int
		yOffset  int
		wantVar  int
		wantSSE  int
		variance func([]byte, int, int, int, []byte, int) (int, int)
	}{
		{name: "16x16", srcStart: 3*40 + 5, refStart: 4*40 + 2, xOffset: 3, yOffset: 5, wantVar: 1554648, wantSSE: 1560732, variance: SubpelVariance16x16},
		{name: "16x8", srcStart: 7*40 + 4, refStart: 8*40 + 3, xOffset: 6, yOffset: 2, wantVar: 937876, wantSSE: 945688, variance: SubpelVariance16x8},
		{name: "8x16", srcStart: 2*40 + 11, refStart: 5*40 + 9, xOffset: 1, yOffset: 7, wantVar: 1492524, wantSSE: 1494956, variance: SubpelVariance8x16},
		{name: "8x8", srcStart: 9*40 + 6, refStart: 10*40 + 1, xOffset: 4, yOffset: 4, wantVar: 389036, wantSSE: 399440, variance: SubpelVariance8x8},
		{name: "8x4", srcStart: 12*40 + 8, refStart: 13*40 + 4, xOffset: 5, yOffset: 3, wantVar: 162876, wantSSE: 169036, variance: SubpelVariance8x4},
		{name: "4x8", srcStart: 14*40 + 9, refStart: 15*40 + 7, xOffset: 2, yOffset: 6, wantVar: 237096, wantSSE: 237624, variance: SubpelVariance4x8},
		{name: "4x4", srcStart: 16*40 + 3, refStart: 17*40 + 5, xOffset: 7, yOffset: 1, wantVar: 255674, wantSSE: 256074, variance: SubpelVariance4x4},
	}
	for _, tc := range tests {
		gotVar, gotSSE := tc.variance(src[tc.srcStart:], 40, tc.xOffset, tc.yOffset, ref[tc.refStart:], 40)
		if gotVar != tc.wantVar || gotSSE != tc.wantSSE {
			t.Fatalf("%s = var:%d sse:%d, want %d/%d", tc.name, gotVar, gotSSE, tc.wantVar, tc.wantSSE)
		}
	}
}

func TestSubpelVarianceZeroOffsetMatchesVariance(t *testing.T) {
	src := makeSubpelVarianceSource()
	ref := makeSubpelVarianceReference()
	if got, sse := SubpelVariance16x16(src[3*40+5:], 40, 0, 0, ref[4*40+2:], 40); got != Variance16x16(src[3*40+5:], 40, ref[4*40+2:], 40) || sse != SSE16x16(src[3*40+5:], 40, ref[4*40+2:], 40) {
		t.Fatalf("16x16 zero-offset = %d/%d, want variance/SSE", got, sse)
	}
	if got, sse := SubpelVariance8x4(src[12*40+8:], 40, 0, 0, ref[13*40+4:], 40); got != Variance8x4(src[12*40+8:], 40, ref[13*40+4:], 40) || sse != SSE8x4(src[12*40+8:], 40, ref[13*40+4:], 40) {
		t.Fatalf("8x4 zero-offset = %d/%d, want variance/SSE", got, sse)
	}
	if got, sse := SubpelVariance4x8(src[14*40+9:], 40, 0, 0, ref[15*40+7:], 40); got != Variance4x8(src[14*40+9:], 40, ref[15*40+7:], 40) || sse != SSE4x8(src[14*40+9:], 40, ref[15*40+7:], 40) {
		t.Fatalf("4x8 zero-offset = %d/%d, want variance/SSE", got, sse)
	}
}

func TestSubpelVarianceAllocatesZero(t *testing.T) {
	src := makeSubpelVarianceSource()
	ref := makeSubpelVarianceReference()
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = SubpelVariance16x16(src, 40, 3, 5, ref, 40)
		_, _ = SubpelVariance16x8(src, 40, 3, 5, ref, 40)
		_, _ = SubpelVariance8x16(src, 40, 3, 5, ref, 40)
		_, _ = SubpelVariance8x8(src, 40, 3, 5, ref, 40)
		_, _ = SubpelVariance8x4(src, 40, 3, 5, ref, 40)
		_, _ = SubpelVariance4x8(src, 40, 3, 5, ref, 40)
		_, _ = SubpelVariance4x4(src, 40, 3, 5, ref, 40)
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

func BenchmarkSSE8x8(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(8 * 8)
	for i := 0; i < b.N; i++ {
		_ = SSE8x8(src, 32, ref, 32)
	}
}

func BenchmarkSSE8x4(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(8 * 4)
	for i := 0; i < b.N; i++ {
		_ = SSE8x4(src, 32, ref, 32)
	}
}

func BenchmarkSSE4x8(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(4 * 8)
	for i := 0; i < b.N; i++ {
		_ = SSE4x8(src, 32, ref, 32)
	}
}

func BenchmarkSSE4x4(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(4 * 4)
	for i := 0; i < b.N; i++ {
		_ = SSE4x4(src, 32, ref, 32)
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

func BenchmarkVariance8x4(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(8 * 4)
	for i := 0; i < b.N; i++ {
		_ = Variance8x4(src, 32, ref, 32)
	}
}

func BenchmarkVariance4x8(b *testing.B) {
	src := make([]byte, 32*32)
	ref := make([]byte, 32*32)
	b.ReportAllocs()
	b.SetBytes(4 * 8)
	for i := 0; i < b.N; i++ {
		_ = Variance4x8(src, 32, ref, 32)
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

func BenchmarkSubpelVariance16x16(b *testing.B) {
	benchmarkSubpelVariance(b, 16*16, SubpelVariance16x16)
}

func BenchmarkSubpelVariance16x8(b *testing.B) {
	benchmarkSubpelVariance(b, 16*8, SubpelVariance16x8)
}

func BenchmarkSubpelVariance8x16(b *testing.B) {
	benchmarkSubpelVariance(b, 8*16, SubpelVariance8x16)
}

func BenchmarkSubpelVariance8x8(b *testing.B) {
	benchmarkSubpelVariance(b, 8*8, SubpelVariance8x8)
}

func BenchmarkSubpelVariance8x4(b *testing.B) {
	benchmarkSubpelVariance(b, 8*4, SubpelVariance8x4)
}

func BenchmarkSubpelVariance4x8(b *testing.B) {
	benchmarkSubpelVariance(b, 4*8, SubpelVariance4x8)
}

func BenchmarkSubpelVariance4x4(b *testing.B) {
	benchmarkSubpelVariance(b, 4*4, SubpelVariance4x4)
}

func benchmarkSubpelVariance(b *testing.B, bytes int64, fn func([]byte, int, int, int, []byte, int) (int, int)) {
	src := makeSubpelVarianceSource()
	ref := makeSubpelVarianceReference()
	b.ReportAllocs()
	b.SetBytes(bytes)
	for i := 0; i < b.N; i++ {
		_, _ = fn(src, 40, 3, 5, ref, 40)
	}
}

func makeSubpelVarianceSource() []byte {
	buf := make([]byte, 40*40)
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			buf[y*40+x] = byte((x*11 + y*17 + x*y*3 + 29) & 255)
		}
	}
	return buf
}

func makeSubpelVarianceReference() []byte {
	buf := make([]byte, 40*40)
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			buf[y*40+x] = byte((x*5 + y*19 + x*y + 7) & 255)
		}
	}
	return buf
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
