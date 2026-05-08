package dsp

import "testing"

func TestLoopFilterSimpleHorizontalEdge(t *testing.T) {
	const stride = 16
	buf := makeLoopFilterRows(stride, []byte{100, 100, 110, 110})

	LoopFilterSimpleHorizontalEdge(buf, stride, 30)

	assertRows(t, "simple horizontal", buf, stride, []byte{100, 102, 107, 110}, 16)
}

func TestLoopFilterSimpleVerticalEdge(t *testing.T) {
	const stride = 4
	buf := makeLoopFilterCols(stride, 16, []byte{100, 100, 110, 110})

	LoopFilterSimpleVerticalEdge(buf, stride, 30)

	assertCols(t, "simple vertical", buf, stride, 16, []byte{100, 102, 107, 110})
}

func TestLoopFilterHorizontalEdge(t *testing.T) {
	const stride = 8
	buf := makeLoopFilterRows(stride, []byte{100, 100, 100, 100, 110, 110, 110, 110})

	LoopFilterHorizontalEdge(buf, stride, 30, 20, 0, 1)

	assertRows(t, "normal horizontal", buf, stride, []byte{100, 100, 102, 104, 106, 108, 110, 110}, 8)
}

func TestLoopFilterVerticalEdge(t *testing.T) {
	const stride = 8
	buf := makeLoopFilterCols(stride, 8, []byte{100, 100, 100, 100, 110, 110, 110, 110})

	LoopFilterVerticalEdge(buf, stride, 30, 20, 0, 1)

	assertCols(t, "normal vertical", buf, stride, 8, []byte{100, 100, 102, 104, 106, 108, 110, 110})
}

func TestMBLoopFilterHorizontalEdge(t *testing.T) {
	const stride = 8
	buf := makeLoopFilterRows(stride, []byte{100, 100, 100, 100, 110, 110, 110, 110})

	MBLoopFilterHorizontalEdge(buf, stride, 30, 20, 0, 1)

	assertRows(t, "mb horizontal", buf, stride, []byte{100, 101, 103, 104, 106, 107, 109, 110}, 8)
}

func TestMBLoopFilterVerticalEdge(t *testing.T) {
	const stride = 8
	buf := makeLoopFilterCols(stride, 8, []byte{100, 100, 100, 100, 110, 110, 110, 110})

	MBLoopFilterVerticalEdge(buf, stride, 30, 20, 0, 1)

	assertCols(t, "mb vertical", buf, stride, 8, []byte{100, 101, 103, 104, 106, 107, 109, 110})
}

func TestLoopFilterMaskPreventsFiltering(t *testing.T) {
	const stride = 8
	buf := makeLoopFilterRows(stride, []byte{0, 255, 100, 100, 110, 110, 110, 110})
	want := append([]byte(nil), buf...)

	LoopFilterHorizontalEdge(buf, stride, 30, 20, 0, 1)

	for i, got := range buf {
		if got != want[i] {
			t.Fatalf("buf[%d] = %d, want unchanged %d", i, got, want[i])
		}
	}
}

func TestLoopFilterAllocatesZero(t *testing.T) {
	simple := makeLoopFilterRows(16, []byte{100, 100, 110, 110})
	normal := makeLoopFilterRows(8, []byte{100, 100, 100, 100, 110, 110, 110, 110})
	allocs := testing.AllocsPerRun(1000, func() {
		LoopFilterSimpleHorizontalEdge(simple, 16, 30)
		LoopFilterHorizontalEdge(normal, 8, 30, 20, 0, 1)
		MBLoopFilterHorizontalEdge(normal, 8, 30, 20, 0, 1)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkLoopFilterSimpleHorizontalEdge(b *testing.B) {
	buf := makeLoopFilterRows(16, []byte{100, 100, 110, 110})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		LoopFilterSimpleHorizontalEdge(buf, 16, 30)
	}
}

func BenchmarkLoopFilterSimpleVerticalEdge(b *testing.B) {
	buf := makeLoopFilterCols(4, 16, []byte{100, 100, 110, 110})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		LoopFilterSimpleVerticalEdge(buf, 4, 30)
	}
}

func BenchmarkLoopFilterHorizontalEdge(b *testing.B) {
	buf := makeLoopFilterRows(8, []byte{100, 100, 100, 100, 110, 110, 110, 110})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		LoopFilterHorizontalEdge(buf, 8, 30, 20, 0, 1)
	}
}

func BenchmarkLoopFilterVerticalEdge(b *testing.B) {
	buf := makeLoopFilterCols(8, 8, []byte{100, 100, 100, 100, 110, 110, 110, 110})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		LoopFilterVerticalEdge(buf, 8, 30, 20, 0, 1)
	}
}

func BenchmarkMBLoopFilterHorizontalEdge(b *testing.B) {
	buf := makeLoopFilterRows(8, []byte{100, 100, 100, 100, 110, 110, 110, 110})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		MBLoopFilterHorizontalEdge(buf, 8, 30, 20, 0, 1)
	}
}

func BenchmarkMBLoopFilterVerticalEdge(b *testing.B) {
	buf := makeLoopFilterCols(8, 8, []byte{100, 100, 100, 100, 110, 110, 110, 110})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		MBLoopFilterVerticalEdge(buf, 8, 30, 20, 0, 1)
	}
}

func BenchmarkLoopFilterHorizontalEdgeY(b *testing.B) {
	buf := makeLoopFilterRows(32, []byte{100, 100, 100, 100, 110, 110, 110, 110})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		LoopFilterHorizontalEdge(buf, 32, 30, 20, 0, 2)
	}
}

func BenchmarkLoopFilterVerticalEdgeY(b *testing.B) {
	buf := makeLoopFilterRows(32, []byte{100, 100, 100, 100, 110, 110, 110, 110, 100, 100, 100, 100, 110, 110, 110, 110})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		LoopFilterVerticalEdge(buf[4:], 32, 30, 20, 0, 2)
	}
}

func BenchmarkMBLoopFilterHorizontalEdgeY(b *testing.B) {
	buf := makeLoopFilterRows(32, []byte{100, 100, 100, 100, 110, 110, 110, 110})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		MBLoopFilterHorizontalEdge(buf, 32, 30, 20, 0, 2)
	}
}

func BenchmarkMBLoopFilterVerticalEdgeY(b *testing.B) {
	buf := makeLoopFilterRows(32, []byte{100, 100, 100, 100, 110, 110, 110, 110, 100, 100, 100, 100, 110, 110, 110, 110})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		MBLoopFilterVerticalEdge(buf[4:], 32, 30, 20, 0, 2)
	}
}

func makeLoopFilterRows(stride int, values []byte) []byte {
	buf := make([]byte, stride*len(values))
	for y, v := range values {
		row := y * stride
		for x := 0; x < stride; x++ {
			buf[row+x] = v
		}
	}
	return buf
}

func makeLoopFilterCols(stride int, rows int, values []byte) []byte {
	buf := make([]byte, stride*rows)
	for y := 0; y < rows; y++ {
		row := y * stride
		for x, v := range values {
			buf[row+x] = v
		}
	}
	return buf
}

func assertRows(t *testing.T, name string, buf []byte, stride int, want []byte, width int) {
	t.Helper()
	for y, w := range want {
		for x := 0; x < width; x++ {
			if got := buf[y*stride+x]; got != w {
				t.Fatalf("%s[%d,%d] = %d, want %d", name, x, y, got, w)
			}
		}
	}
}

func assertCols(t *testing.T, name string, buf []byte, stride int, rows int, want []byte) {
	t.Helper()
	for y := 0; y < rows; y++ {
		row := y * stride
		for x, w := range want {
			if got := buf[row+x]; got != w {
				t.Fatalf("%s[%d,%d] = %d, want %d", name, x, y, got, w)
			}
		}
	}
}
