package pixel

import (
	"bytes"
	"testing"
)

func TestFilterBilinear2DBitExact(t *testing.T) {
	if !Enabled {
		t.Skip("SIMD disabled")
	}
	taps := [][2]uint8{
		{64, 64},
		{112, 16},
		{96, 32},
		{80, 48},
		{48, 80},
		{32, 96},
		{16, 112},
		{120, 8},
	}
	sizes := [][2]int{
		{4, 4},
		{8, 4},
		{8, 8},
		{16, 8},
		{16, 16},
		{24, 16},
		{32, 32},
		{64, 64},
	}
	src := make([]byte, 80*80)
	for i := range src {
		src[i] = byte((i*37 + i/13*19 + 7) & 0xff)
	}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		ss, ds := 80, 80
		for _, hx := range taps {
			for _, hy := range taps {
				got := make([]byte, h*ds)
				if !FilterBilinear2D(src, ss, got, ds, w, h, hx[0], hx[1], hy[0], hy[1]) {
					continue
				}
				tmp := make([]byte, (h+1)*w)
				want := make([]byte, h*ds)
				FilterBilinear(src, ss, tmp, w, w, h+1, 1, hx[0], hx[1])
				FilterBilinear(tmp, w, want, ds, w, h, w, hy[0], hy[1])
				for y := range h {
					if !bytes.Equal(got[y*ds:y*ds+w], want[y*ds:y*ds+w]) {
						t.Fatalf("size=%dx%d hx=%v hy=%v row=%d mismatch", w, h, hx, hy, y)
					}
				}
			}
		}
	}
}

func BenchmarkFilterBilinear2D16x16(b *testing.B) {
	src := make([]byte, 32*32)
	dst := make([]byte, 32*32)
	for i := range src {
		src[i] = byte(i * 17)
	}
	b.Run("Fused1Pass_QuarterPel", func(b *testing.B) {
		for range b.N {
			FilterBilinear2D(src, 32, dst, 32, 16, 16, 96, 32, 48, 80)
		}
	})
	b.Run("TwoPass_QuarterPel", func(b *testing.B) {
		var tmp [17 * 16]byte
		for range b.N {
			FilterBilinear(src, 32, tmp[:], 16, 16, 17, 1, 96, 32)
			FilterBilinear(tmp[:], 16, dst, 32, 16, 16, 16, 48, 80)
		}
	})
	b.Run("Fused1Pass_HalfPel", func(b *testing.B) {
		for range b.N {
			FilterBilinear2D(src, 32, dst, 32, 16, 16, 64, 64, 64, 64)
		}
	})
	b.Run("TwoPass_HalfPel", func(b *testing.B) {
		var tmp [17 * 16]byte
		for range b.N {
			FilterBilinear(src, 32, tmp[:], 16, 16, 17, 1, 64, 64)
			FilterBilinear(tmp[:], 16, dst, 32, 16, 16, 16, 64, 64)
		}
	})
}
