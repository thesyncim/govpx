package dsp

import "testing"

func TestIntraDCPredict16x16Availability(t *testing.T) {
	above := make([]byte, 16)
	left := make([]byte, 16)
	for i := 0; i < 16; i++ {
		above[i] = byte(i + 1)
		left[i] = byte(101 + i)
	}

	cases := []struct {
		name          string
		upAvailable   bool
		leftAvailable bool
		want          byte
	}{
		{name: "none", want: 128},
		{name: "top", upAvailable: true, want: 9},
		{name: "left", leftAvailable: true, want: 109},
		{name: "both", upAvailable: true, leftAvailable: true, want: 59},
	}

	for _, tc := range cases {
		dst := make([]byte, 16*16)
		IntraDCPredict16x16(dst, 16, above, left, tc.upAvailable, tc.leftAvailable)
		assertBlockFilled(t, tc.name, dst, 16, 16, tc.want)
	}
}

func TestIntraVerticalPredict8x8(t *testing.T) {
	above := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	dst := make([]byte, 8*8)

	IntraVerticalPredict8x8(dst, 8, above)

	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if got := dst[y*8+x]; got != above[x] {
				t.Fatalf("dst[%d,%d] = %d, want %d", x, y, got, above[x])
			}
		}
	}
}

func TestIntraHorizontalPredict8x8(t *testing.T) {
	left := []byte{11, 22, 33, 44, 55, 66, 77, 88}
	dst := make([]byte, 8*8)

	IntraHorizontalPredict8x8(dst, 8, left)

	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if got := dst[y*8+x]; got != left[y] {
				t.Fatalf("dst[%d,%d] = %d, want %d", x, y, got, left[y])
			}
		}
	}
}

func TestIntraTMPredict8x8(t *testing.T) {
	above := []byte{5, 30, 60, 90, 120, 150, 200, 250}
	left := []byte{250, 200, 150, 120, 90, 60, 30, 5}
	const topLeft = 100
	dst := make([]byte, 8*8)

	IntraTMPredict8x8(dst, 8, above, left, topLeft)

	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			want := ClipPixel(int(left[y]) + int(above[x]) - topLeft)
			if got := dst[y*8+x]; got != want {
				t.Fatalf("dst[%d,%d] = %d, want %d", x, y, got, want)
			}
		}
	}
}

func TestIntraPredictAllocatesZero(t *testing.T) {
	above := make([]byte, 16)
	left := make([]byte, 16)
	dst := make([]byte, 16*16)
	allocs := testing.AllocsPerRun(1000, func() {
		IntraDCPredict16x16(dst, 16, above, left, true, true)
		IntraVerticalPredict16x16(dst, 16, above)
		IntraHorizontalPredict16x16(dst, 16, left)
		IntraTMPredict16x16(dst, 16, above, left, 128)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkIntraDCPredict16x16(b *testing.B) {
	above := make([]byte, 16)
	left := make([]byte, 16)
	dst := make([]byte, 16*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		IntraDCPredict16x16(dst, 16, above, left, true, true)
	}
}

func BenchmarkIntraVerticalPredict16x16(b *testing.B) {
	above := make([]byte, 16)
	dst := make([]byte, 16*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		IntraVerticalPredict16x16(dst, 16, above)
	}
}

func BenchmarkIntraHorizontalPredict16x16(b *testing.B) {
	left := make([]byte, 16)
	dst := make([]byte, 16*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		IntraHorizontalPredict16x16(dst, 16, left)
	}
}

func BenchmarkIntraTMPredict16x16(b *testing.B) {
	above := make([]byte, 16)
	left := make([]byte, 16)
	dst := make([]byte, 16*16)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		IntraTMPredict16x16(dst, 16, above, left, 128)
	}
}

func assertBlockFilled(t *testing.T, name string, dst []byte, stride int, size int, want byte) {
	t.Helper()
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if got := dst[y*stride+x]; got != want {
				t.Fatalf("%s dst[%d,%d] = %d, want %d", name, x, y, got, want)
			}
		}
	}
}
