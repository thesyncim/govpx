package encoder

import "testing"

func TestPlaneRectFits(t *testing.T) {
	huge := int(^uint(0) >> 1)
	buf := make([]byte, 16)
	tests := []struct {
		name               string
		stride, x, y, w, h int
		want               bool
	}{
		{name: "fits", stride: 4, x: 1, y: 2, w: 2, h: 2, want: true},
		{name: "crosses-row", stride: 4, x: 3, y: 0, w: 2, h: 1, want: false},
		{name: "past-buffer", stride: 4, x: 0, y: 3, w: 4, h: 2, want: false},
		{name: "row-overflow", stride: huge, x: 0, y: huge, w: 1, h: 2, want: false},
		{name: "width-overflow", stride: huge, x: huge - 1, y: 0, w: 2, h: 1, want: false},
	}
	for _, tt := range tests {
		got := planeRectFits(buf, tt.stride, tt.x, tt.y, tt.w, tt.h)
		if got != tt.want {
			t.Fatalf("%s: planeRectFits(stride=%d x=%d y=%d w=%d h=%d) = %v, want %v",
				tt.name, tt.stride, tt.x, tt.y, tt.w, tt.h, got, tt.want)
		}
	}
}

func TestBlockSADNoLimitMatchesScalar(t *testing.T) {
	const stride = 80
	src := make([]byte, stride*80)
	ref := make([]byte, stride*80)
	for i := range src {
		src[i] = byte((i*17 + i/7) & 0xff)
		ref[i] = byte((i*29 + 11) & 0xff)
	}
	cases := []struct {
		w, h int
	}{
		{64, 64}, {64, 32}, {32, 64}, {32, 32}, {32, 16},
		{16, 32}, {16, 16}, {16, 8}, {8, 16}, {8, 8},
		{8, 4}, {4, 8}, {4, 4},
	}
	for _, tc := range cases {
		got := BlockSAD(src, stride, ref, stride,
			3, 5, 7, 11, tc.w, tc.h, ^uint64(0))
		want := BlockSAD(src, stride, ref, stride,
			3, 5, 7, 11, tc.w, tc.h, 1<<63)
		if got != want {
			t.Fatalf("%dx%d SAD = %d, want scalar %d", tc.w, tc.h, got, want)
		}
	}
}

func TestBlockSADSkipRowsOffsets(t *testing.T) {
	const stride = 4
	src := []byte{
		1, 2, 3, 4,
		10, 20, 30, 40,
		5, 6, 7, 8,
		50, 60, 70, 80,
	}
	ref := []byte{
		2, 4, 6, 8,
		9, 18, 27, 36,
		1, 1, 1, 1,
		40, 50, 60, 70,
	}
	even := BlockSADSkipRowsOffsets(src, 0, stride, ref, 0, stride,
		4, 4, ^uint64(0))
	if even != 64 {
		t.Fatalf("even-row skip SAD = %d, want 64", even)
	}
	odd := BlockSADSkipRowsOffsets(src, stride, stride, ref, stride, stride,
		4, 4, ^uint64(0))
	if odd != 100 {
		t.Fatalf("odd-row skip SAD = %d, want 100", odd)
	}
}

func TestBlockSSEMatchesScalar(t *testing.T) {
	const stride = 80
	src := make([]byte, stride*80)
	ref := make([]byte, stride*80)
	for i := range src {
		src[i] = byte((i*13 + i/5) & 0xff)
		ref[i] = byte((i*23 + 19) & 0xff)
	}

	got := BlockSSE(src, stride, ref, stride, 3, 5, 7, 11, 32, 16)
	var want uint64
	for y := range 16 {
		srcRow := src[(5+y)*stride+3:]
		refRow := ref[(11+y)*stride+7:]
		for x := range 32 {
			diff := int(srcRow[x]) - int(refRow[x])
			want += uint64(diff * diff)
		}
	}
	if got != want {
		t.Fatalf("SSE = %d, want %d", got, want)
	}
}

func TestBlockDiffVarianceSSEClampedSourceExtendsVisibleEdges(t *testing.T) {
	src := []byte{
		1, 2, 3, 200,
		4, 5, 6, 201,
		7, 8, 9, 202,
	}
	ref := make([]byte, 4*4)
	variance, sse, ok := BlockDiffVarianceSSEClampedSource(
		src, 4, 3, 3, ref, 4, 1, 1, 0, 0, 4, 4)
	if !ok {
		t.Fatal("BlockDiffVarianceSSEClampedSource returned !ok")
	}
	if sse != 1054 {
		t.Fatalf("sse = %d, want 1054 from visible-edge source extension", sse)
	}
	if variance != 30 {
		t.Fatalf("variance = %d, want 30 from full 4x4 block", variance)
	}
}

func TestBlockDiffVarianceSSEClampedSourceKeepsFullVisibleFastPath(t *testing.T) {
	src := []byte{
		1, 2, 3, 4,
		5, 6, 7, 8,
		9, 10, 11, 12,
		13, 14, 15, 16,
	}
	ref := []byte{
		16, 15, 14, 13,
		12, 11, 10, 9,
		8, 7, 6, 5,
		4, 3, 2, 1,
	}
	wantVar, wantSSE := BlockDiffVarianceSSE(src, 4, ref, 4, 0, 0, 0, 0, 4, 4)
	gotVar, gotSSE, ok := BlockDiffVarianceSSEClampedSource(
		src, 4, 4, 4, ref, 4, 0, 0, 0, 0, 4, 4)
	if !ok {
		t.Fatal("BlockDiffVarianceSSEClampedSource returned !ok")
	}
	if gotVar != wantVar || gotSSE != wantSSE {
		t.Fatalf("fast path = var/sse %d/%d, want %d/%d",
			gotVar, gotSSE, wantVar, wantSSE)
	}
}

func TestBlockDiffVarianceSSEClampedSourceRejectsOverflowSpan(t *testing.T) {
	huge := int(^uint(0) >> 1)
	if _, _, ok := BlockDiffVarianceSSEClampedSource(
		[]byte{1}, huge/2+1, huge/2+1, 3,
		make([]byte, 16), 4, 0, 0, 0, 0, 4, 4); ok {
		t.Fatal("BlockDiffVarianceSSEClampedSource accepted overflowing source span")
	}
	if _, _, ok := BlockDiffVarianceSSEClampedSource(
		make([]byte, 16), 4, 4, 4,
		make([]byte, 16), 4, huge, 0, 0, 0, 4, 4); ok {
		t.Fatal("BlockDiffVarianceSSEClampedSource accepted overflowing source x")
	}
}

func TestSourceVarianceAreaPerPixel(t *testing.T) {
	const side = 16
	buf := make([]byte, side*side)
	for i := range buf {
		buf[i] = 200
	}
	if got := SourceVarianceAreaPerPixel(buf, side, 0, 0, side, side); got != 0 {
		t.Fatalf("flat source variance = %d, want 0", got)
	}

	for i := range buf {
		if i%2 == 0 {
			buf[i] = 0
		} else {
			buf[i] = 255
		}
	}
	if got := SourceVarianceAreaPerPixel(buf, side, 0, 0, side, side); got != 16256 {
		t.Fatalf("checker source variance = %d, want 16256", got)
	}
}

func TestInterSkipFilterSearch(t *testing.T) {
	if InterSkipFilterSearch(0, 0) {
		t.Fatal("zero threshold skipped filter search")
	}
	if !InterSkipFilterSearch(99, 100) {
		t.Fatal("variance below threshold did not skip filter search")
	}
	if InterSkipFilterSearch(100, 100) {
		t.Fatal("variance at threshold skipped filter search")
	}
}

var blockSSEBenchmarkSink uint64

func BenchmarkBlockSSE64x64(b *testing.B) {
	const stride = 96
	src := make([]byte, stride*96)
	ref := make([]byte, stride*96)
	for i := range src {
		src[i] = byte((i*13 + i/5) & 0xff)
		ref[i] = byte((i*23 + 19) & 0xff)
	}

	b.ReportAllocs()
	var sum uint64
	for b.Loop() {
		sum += BlockSSE(src, stride, ref, stride, 3, 5, 7, 11, 64, 64)
	}
	blockSSEBenchmarkSink = sum
}
