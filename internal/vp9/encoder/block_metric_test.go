package encoder

import "testing"

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
