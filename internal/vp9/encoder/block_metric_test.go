package encoder

import (
	"fmt"
	"testing"
)

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

func TestBlockSADSkipRowsMatchesScalar(t *testing.T) {
	const stride = 96
	src := make([]byte, stride*96)
	ref := make([]byte, stride*96)
	for i := range src {
		src[i] = byte((i*17 + i/7 + 13) & 0xff)
		ref[i] = byte((i*29 + i/5 + 11) & 0xff)
	}
	cases := []struct {
		w, h int
	}{
		{64, 64}, {64, 32}, {32, 64}, {32, 32}, {32, 16},
		{16, 32}, {16, 16}, {16, 8}, {8, 16}, {8, 8},
		{8, 4}, {4, 8}, {4, 4},
		// Unsupported by the specialized SAD table; must still match scalar.
		{12, 12},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			srcOff := 5*stride + 3
			refOff := 11*stride + 7
			got, ok := BlockSADSkipRowsNoLimitOffsets(src, srcOff, stride,
				ref, refOff, stride, tc.w, tc.h)
			if !ok {
				t.Fatalf("BlockSADSkipRowsNoLimitOffsets returned !ok")
			}
			want := scalarSkipRowsSAD(src, srcOff, stride, ref, refOff, stride,
				tc.w, tc.h)
			if got != want {
				t.Fatalf("skip SAD = %d, want scalar %d", got, want)
			}

			oddGot, ok := BlockSADSkipRowsNoLimitOffsets(src, srcOff+stride,
				stride, ref, refOff+stride, stride, tc.w, tc.h)
			if !ok {
				t.Fatalf("odd BlockSADSkipRowsNoLimitOffsets returned !ok")
			}
			oddWant := scalarSkipRowsSAD(src, srcOff+stride, stride, ref,
				refOff+stride, stride, tc.w, tc.h)
			if oddGot != oddWant {
				t.Fatalf("odd skip SAD = %d, want scalar %d", oddGot, oddWant)
			}
		})
	}
}

func TestBlockSADSkipRows4MatchesSingle(t *testing.T) {
	const stride = 96
	src := make([]byte, stride*96)
	ref := make([]byte, stride*96)
	for i := range src {
		src[i] = byte((i*31 + i/3 + 19) & 0xff)
		ref[i] = byte((i*43 + i/9 + 23) & 0xff)
	}
	cases := []struct {
		w, h int
	}{
		{64, 64}, {32, 32}, {16, 16}, {16, 8}, {8, 8}, {4, 8}, {12, 12},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			srcOff := 9*stride + 5
			refOffs := [4]int{
				13*stride + 7,
				15*stride + 9,
				17*stride + 11,
				19*stride + 13,
			}
			var got [4]uint32
			if !BlockSADSkipRows4NoLimitOffsets(src, srcOff, stride, ref,
				refOffs, stride, tc.w, tc.h, &got) {
				t.Fatalf("BlockSADSkipRows4NoLimitOffsets returned !ok")
			}
			for i, refOff := range refOffs {
				want, ok := BlockSADSkipRowsNoLimitOffsets(src, srcOff, stride,
					ref, refOff, stride, tc.w, tc.h)
				if !ok {
					t.Fatalf("single skip SAD %d returned !ok", i)
				}
				if got[i] != want {
					t.Fatalf("candidate %d skip SAD = %d, want %d", i, got[i],
						want)
				}
			}
		})
	}
}

func scalarSkipRowsSAD(src []byte, srcOff, srcStride int,
	ref []byte, refOff, refStride int, w, h int,
) uint32 {
	var sad uint32
	for y := 0; y < h; y += 2 {
		srcRow := srcOff + y*srcStride
		refRow := refOff + y*refStride
		for x := range w {
			a, b := src[srcRow+x], ref[refRow+x]
			if a >= b {
				sad += uint32(a - b)
			} else {
				sad += uint32(b - a)
			}
		}
	}
	return sad * 2
}

func TestBlockSSEMatchesScalar(t *testing.T) {
	const stride = 80
	src := make([]byte, stride*80)
	ref := make([]byte, stride*80)
	for i := range src {
		src[i] = byte((i*13 + i/5) & 0xff)
		ref[i] = byte((i*23 + 19) & 0xff)
	}
	cases := []struct {
		w, h int
	}{
		{64, 64}, {64, 32}, {32, 64}, {32, 32}, {32, 16},
		{16, 32}, {16, 16}, {16, 8}, {8, 16}, {8, 8},
		{8, 4}, {4, 8}, {4, 4},
		// Unsupported by VP9 DSP; must stay on the scalar fallback.
		{12, 12},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			got := BlockSSE(src, stride, ref, stride, 3, 5, 7, 11,
				tc.w, tc.h)
			var want uint64
			for y := range tc.h {
				srcRow := src[(5+y)*stride+3:]
				refRow := ref[(11+y)*stride+7:]
				for x := range tc.w {
					diff := int(srcRow[x]) - int(refRow[x])
					want += uint64(diff * diff)
				}
			}
			if got != want {
				t.Fatalf("SSE = %d, want %d", got, want)
			}
		})
	}
}

func TestBlockDiffStatsMatchesScalarForVP9DSPBlockSizes(t *testing.T) {
	const stride = 96
	src := make([]byte, stride*96)
	ref := make([]byte, stride*96)
	for i := range src {
		src[i] = byte((i*19 + i/3 + 7) & 0xff)
		ref[i] = byte((i*31 + i/11 + 23) & 0xff)
	}
	cases := []struct {
		w, h int
	}{
		{64, 64}, {64, 32}, {32, 64}, {32, 32}, {32, 16},
		{16, 32}, {16, 16}, {16, 8}, {8, 16}, {8, 8},
		{8, 4}, {4, 8}, {4, 4},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			got := blockDiffStats(src, stride, ref, stride,
				3, 5, 7, 11, tc.w, tc.h)
			want := blockDiffStatsScalar(src, stride, ref, stride,
				3, 5, 7, 11, tc.w, tc.h)
			if got != want {
				t.Fatalf("blockDiffStats = %+v, want scalar %+v", got, want)
			}
			dspGot, ok := blockDiffStatsVP9DSP(src, stride, ref, stride,
				3, 5, 7, 11, tc.w, tc.h)
			if !ok {
				t.Fatalf("blockDiffStatsVP9DSP returned !ok")
			}
			if dspGot != want {
				t.Fatalf("blockDiffStatsVP9DSP = %+v, want scalar %+v", dspGot, want)
			}
		})
	}
}

func TestBlockDiffStatsUnsupportedSizeFallsBackToScalar(t *testing.T) {
	const stride = 32
	src := make([]byte, stride*32)
	ref := make([]byte, stride*32)
	for i := range src {
		src[i] = byte((i*7 + 3) & 0xff)
		ref[i] = byte((i*5 + 17) & 0xff)
	}
	got := blockDiffStats(src, stride, ref, stride, 1, 2, 3, 4, 12, 12)
	want := blockDiffStatsScalar(src, stride, ref, stride, 1, 2, 3, 4, 12, 12)
	if got != want {
		t.Fatalf("unsupported block stats = %+v, want scalar %+v", got, want)
	}
	if _, ok := blockDiffStatsVP9DSP(src, stride, ref, stride, 1, 2, 3, 4, 12, 12); ok {
		t.Fatal("blockDiffStatsVP9DSP returned ok for unsupported 12x12 block")
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

func TestBlockSourceVariance128MatchesScalar(t *testing.T) {
	const stride = 96
	src := make([]byte, stride*96)
	for i := range src {
		src[i] = byte((i*37 + i/9 + 41) & 0xff)
	}
	cases := []struct {
		w, h int
	}{
		{64, 64}, {64, 32}, {32, 64}, {32, 32}, {32, 16},
		{16, 32}, {16, 16}, {16, 8}, {8, 16}, {8, 8},
		{8, 4}, {4, 8}, {4, 4},
		// Unsupported by VP9 DSP; must stay on the scalar fallback.
		{12, 12},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.h), func(t *testing.T) {
			got := BlockSourceVariance128(src, stride, 3, 5, tc.w, tc.h)
			want := blockSourceVariance128Scalar(src, stride, 3, 5, tc.w, tc.h)
			if got != want {
				t.Fatalf("source variance = %d, want scalar %d", got, want)
			}
		})
	}
}

func blockSourceVariance128Scalar(src []byte, srcStride int,
	srcX, srcY, w, h int,
) uint64 {
	var sum int64
	var sse uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		for x := range w {
			diff := int64(srcRow[x]) - 128
			sum += diff
			sse += uint64(diff * diff)
		}
	}
	n := int64(w * h)
	if n <= 0 {
		return 0
	}
	meanSquares := uint64((sum * sum) / n)
	if sse <= meanSquares {
		return 0
	}
	return sse - meanSquares
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
var blockSourceVariance128BenchmarkSink uint64

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

func BenchmarkBlockSourceVariance12864x64(b *testing.B) {
	const stride = 96
	src := make([]byte, stride*96)
	for i := range src {
		src[i] = byte((i*37 + i/9 + 41) & 0xff)
	}

	b.ReportAllocs()
	var sum uint64
	for b.Loop() {
		sum += BlockSourceVariance128(src, stride, 3, 5, 64, 64)
	}
	blockSourceVariance128BenchmarkSink = sum
}
