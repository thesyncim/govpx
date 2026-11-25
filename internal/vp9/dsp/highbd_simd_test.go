package dsp

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func TestHighbdSIMDMatchesScalar(t *testing.T) {
	const stride = 96
	src := make([]uint16, stride*96)
	dstBase := make([]uint16, stride*96)
	for i := range src {
		src[i] = uint16((i*37 + (i>>3)*19) & 0xfff)
		dstBase[i] = uint16((i*53 + 17) & 0xfff)
	}

	filters := []*([16][8]int16){
		&tables.SubPelFilters8,
		&tables.SubPelFilters8s,
		&tables.SubPelFilters8lp,
		&tables.BilinearFilters,
	}

	for _, bd := range []int{10, 12} {
		mask := uint16((1 << uint(bd)) - 1)
		for i := range src {
			src[i] &= mask
			dstBase[i] &= mask
		}
		for _, sz := range [][2]int{{8, 8}, {16, 16}, {32, 16}, {64, 64}} {
			w, h := sz[0], sz[1]
			srcOff := 8*stride + 8
			dstOff := 4*stride + 4

			// Avg parity
			got := append([]uint16(nil), dstBase...)
			want := append([]uint16(nil), dstBase...)
			if highbdConvolveAvgSIMD(src, srcOff, stride, got, dstOff, stride, w, h) {
				for r := range h {
					for c := range w {
						di := dstOff + r*stride + c
						si := srcOff + r*stride + c
						want[di] = uint16(highbdRoundPowerOfTwo(int(want[di])+int(src[si]), 1))
					}
				}
				for i := range got {
					if got[i] != want[i] {
						t.Fatalf("HighbdConvolveAvg bd=%d %dx%d mismatch at %d: got=%d want=%d", bd, w, h, i, got[i], want[i])
					}
				}
			}

			// Horiz + Vert (non-avg and avg) parity
			for fi, flt := range filters {
				for _, q4 := range []int{1, 4, 8, 13, 15} {
					for _, avg := range []bool{false, true} {
						gotH := append([]uint16(nil), dstBase...)
						wantH := append([]uint16(nil), dstBase...)
						if highbdConvolve8HorizSIMD(src, srcOff, stride, gotH, dstOff, stride, flt, q4, 16, w, h, bd, avg) {
							if avg {
								highbdConvolveAvgHoriz(src, srcOff, stride, wantH, dstOff, stride, flt, q4, 16, w, h, bd)
							} else {
								highbdConvolveHoriz(src, srcOff, stride, wantH, dstOff, stride, flt, q4, 16, w, h, bd)
							}
							for i := range gotH {
								if gotH[i] != wantH[i] {
									t.Fatalf("Horiz bd=%d %dx%d f=%d q4=%d avg=%v mismatch at %d: got=%d want=%d",
										bd, w, h, fi, q4, avg, i, gotH[i], wantH[i])
								}
							}
						}

						gotV := append([]uint16(nil), dstBase...)
						wantV := append([]uint16(nil), dstBase...)
						if highbdConvolve8VertSIMD(src, srcOff, stride, gotV, dstOff, stride, flt, q4, 16, w, h, bd, avg) {
							if avg {
								highbdConvolveAvgVert(src, srcOff, stride, wantV, dstOff, stride, flt, q4, 16, w, h, bd)
							} else {
								highbdConvolveVert(src, srcOff, stride, wantV, dstOff, stride, flt, q4, 16, w, h, bd)
							}
							for i := range gotV {
								if gotV[i] != wantV[i] {
									t.Fatalf("Vert bd=%d %dx%d f=%d q4=%d avg=%v mismatch at %d: got=%d want=%d",
										bd, w, h, fi, q4, avg, i, gotV[i], wantV[i])
								}
							}
						}
					}
				}
			}
		}

		// HighbdSAD parity
		for _, n := range []int{16, 64, 256, 77} {
			got := HighbdSAD(src[:n], dstBase[:n], n)
			want := 0
			for i := range n {
				d := int(src[i]) - int(dstBase[i])
				if d < 0 {
					d = -d
				}
				want += d
			}
			if got != want {
				t.Fatalf("HighbdSAD bd=%d n=%d: got=%d want=%d", bd, n, got, want)
			}
		}
	}
}

func BenchmarkHighbdConvolve8Horiz16x16Scalar(b *testing.B) {
	const stride = 64
	src := make([]uint16, stride*48)
	dst := make([]uint16, stride*48)
	for i := range src {
		src[i] = uint16((i * 17) & 0x3ff)
	}
	b.ResetTimer()
	for range b.N {
		highbdConvolveHoriz(src, 8*stride+8, stride, dst, 0, stride, &tables.SubPelFilters8, 5, 16, 16, 16, 10)
	}
}

func BenchmarkHighbdConvolve8Horiz16x16SIMD(b *testing.B) {
	const stride = 64
	src := make([]uint16, stride*48)
	dst := make([]uint16, stride*48)
	for i := range src {
		src[i] = uint16((i * 17) & 0x3ff)
	}
	b.ResetTimer()
	for range b.N {
		HighbdConvolve8Horiz(src, 8*stride+8, stride, dst, 0, stride, &tables.SubPelFilters8, 5, 16, 16, 16, 10)
	}
}

func BenchmarkHighbdConvolve8Vert16x16Scalar(b *testing.B) {
	const stride = 64
	src := make([]uint16, stride*48)
	dst := make([]uint16, stride*48)
	for i := range src {
		src[i] = uint16((i * 17) & 0x3ff)
	}
	b.ResetTimer()
	for range b.N {
		highbdConvolveVert(src, 8*stride+8, stride, dst, 0, stride, &tables.SubPelFilters8, 5, 16, 16, 16, 10)
	}
}

func BenchmarkHighbdConvolve8Vert16x16SIMD(b *testing.B) {
	const stride = 64
	src := make([]uint16, stride*48)
	dst := make([]uint16, stride*48)
	for i := range src {
		src[i] = uint16((i * 17) & 0x3ff)
	}
	b.ResetTimer()
	for range b.N {
		HighbdConvolve8Vert(src, 8*stride+8, stride, dst, 0, stride, &tables.SubPelFilters8, 5, 16, 16, 16, 10)
	}
}

func BenchmarkHighbdConvolveAvg16x16Scalar(b *testing.B) {
	const stride = 64
	src := make([]uint16, stride*32)
	dst := make([]uint16, stride*32)
	b.ResetTimer()
	for range b.N {
		sOff, dOff := 0, 0
		for range 16 {
			for x := range 16 {
				dst[dOff+x] = uint16(highbdRoundPowerOfTwo(int(dst[dOff+x])+int(src[sOff+x]), 1))
			}
			sOff += stride
			dOff += stride
		}
	}
}

func BenchmarkHighbdConvolveAvg16x16SIMD(b *testing.B) {
	const stride = 64
	src := make([]uint16, stride*32)
	dst := make([]uint16, stride*32)
	b.ResetTimer()
	for range b.N {
		HighbdConvolveAvg(src, 0, stride, dst, 0, stride, 16, 16)
	}
}

func BenchmarkHighbdSAD64Scalar(b *testing.B) {
	a := make([]uint16, 64)
	c := make([]uint16, 64)
	for i := range a {
		a[i] = uint16(i * 13)
		c[i] = uint16((63 - i) * 11)
	}
	var sink int
	b.ResetTimer()
	for range b.N {
		sum := 0
		for i := range 64 {
			d := int(a[i]) - int(c[i])
			if d < 0 {
				d = -d
			}
			sum += d
		}
		sink += sum
	}
	_ = sink
}

func BenchmarkHighbdSAD64SIMD(b *testing.B) {
	a := make([]uint16, 64)
	c := make([]uint16, 64)
	for i := range a {
		a[i] = uint16(i * 13)
		c[i] = uint16((63 - i) * 11)
	}
	var sink int
	b.ResetTimer()
	for range b.N {
		sink += HighbdSAD(a, c, 64)
	}
	_ = sink
}
