package dsp

import (
	"math/rand"
	"testing"
)

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

func BenchmarkIntraDCPredict8x8(b *testing.B) {
	above := make([]byte, 8)
	left := make([]byte, 8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		IntraDCPredict8x8(dst, 8, above, left, true, true)
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

func BenchmarkIntraVerticalPredict8x8(b *testing.B) {
	above := make([]byte, 8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		IntraVerticalPredict8x8(dst, 8, above)
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

func BenchmarkIntraHorizontalPredict8x8(b *testing.B) {
	left := make([]byte, 8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		IntraHorizontalPredict8x8(dst, 8, left)
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

func BenchmarkIntraTMPredict8x8(b *testing.B) {
	above := make([]byte, 8)
	left := make([]byte, 8)
	dst := make([]byte, 8*8)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		IntraTMPredict8x8(dst, 8, above, left, 128)
	}
}

// TestIntraPredictSIMDParity compares the dispatched implementations
// (which on supported architectures route to SIMD) against the scalar
// reference across randomised inputs and several stride/availability
// combinations. Ensures byte-for-byte parity with libvpx semantics.
func TestIntraPredictSIMDParity(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 64; iter++ {
		var above16 [16]byte
		var left16 [16]byte
		for i := range above16 {
			above16[i] = byte(rng.Intn(256))
			left16[i] = byte(rng.Intn(256))
		}
		topLeft := byte(rng.Intn(256))

		for _, stride := range []int{16, 32, 48} {
			for _, up := range []bool{false, true} {
				for _, lf := range []bool{false, true} {
					ref := make([]byte, stride*16+stride)
					got := make([]byte, stride*16+stride)

					intraDCPredictScalar(ref, stride, above16[:], left16[:], 16, up, lf)
					IntraDCPredict16x16(got, stride, above16[:], left16[:], up, lf)
					assertBlocksEqual(t, "DC16", ref, got, stride, 16)
				}
			}
			ref := make([]byte, stride*16+stride)
			got := make([]byte, stride*16+stride)

			intraVerticalPredictScalar(ref, stride, above16[:], 16)
			IntraVerticalPredict16x16(got, stride, above16[:])
			assertBlocksEqual(t, "V16", ref, got, stride, 16)

			intraHorizontalPredictScalar(ref, stride, left16[:], 16)
			IntraHorizontalPredict16x16(got, stride, left16[:])
			assertBlocksEqual(t, "H16", ref, got, stride, 16)

			intraTMPredictScalar(ref, stride, above16[:], left16[:], topLeft, 16)
			IntraTMPredict16x16(got, stride, above16[:], left16[:], topLeft)
			assertBlocksEqual(t, "TM16", ref, got, stride, 16)
		}

		var above8 [8]byte
		var left8 [8]byte
		for i := range above8 {
			above8[i] = byte(rng.Intn(256))
			left8[i] = byte(rng.Intn(256))
		}
		for _, stride := range []int{8, 16, 24} {
			for _, up := range []bool{false, true} {
				for _, lf := range []bool{false, true} {
					ref := make([]byte, stride*8+stride)
					got := make([]byte, stride*8+stride)

					intraDCPredictScalar(ref, stride, above8[:], left8[:], 8, up, lf)
					IntraDCPredict8x8(got, stride, above8[:], left8[:], up, lf)
					assertBlocksEqual(t, "DC8", ref, got, stride, 8)
				}
			}
			ref := make([]byte, stride*8+stride)
			got := make([]byte, stride*8+stride)

			intraVerticalPredictScalar(ref, stride, above8[:], 8)
			IntraVerticalPredict8x8(got, stride, above8[:])
			assertBlocksEqual(t, "V8", ref, got, stride, 8)

			intraHorizontalPredictScalar(ref, stride, left8[:], 8)
			IntraHorizontalPredict8x8(got, stride, left8[:])
			assertBlocksEqual(t, "H8", ref, got, stride, 8)

			intraTMPredictScalar(ref, stride, above8[:], left8[:], topLeft, 8)
			IntraTMPredict8x8(got, stride, above8[:], left8[:], topLeft)
			assertBlocksEqual(t, "TM8", ref, got, stride, 8)
		}
	}
}

func assertBlocksEqual(t *testing.T, name string, want, got []byte, stride, size int) {
	t.Helper()
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			off := y*stride + x
			if got[off] != want[off] {
				t.Fatalf("%s [%d,%d]: got %d want %d", name, x, y, got[off], want[off])
			}
		}
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
