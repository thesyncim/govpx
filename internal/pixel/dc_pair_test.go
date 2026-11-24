//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestDCPairNative(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD disabled")
	}
	for align := range 8 {
		for _, stride := range []int{8, 9, 17} {
			for _, pair := range [][2]int16{{-255, 255}, {-1, 1}, {0, 0}, {127, -128}} {
				src := make([]byte, align+3*stride+8)
				for i := range src {
					src[i] = byte(i*37 + 11)
				}
				for _, inplace := range []bool{false, true} {
					got := bytes.Clone(src)
					want := bytes.Clone(src)
					pred := src[align:]
					if inplace {
						pred = got[align:]
					}
					if !AddConstantPair4(pred, stride, got[align:], stride, pair[0], pair[1]) {
						t.Fatal("native pair not selected")
					}
					for y := range 4 {
						for x := range 8 {
							d := int(pair[0])
							if x >= 4 {
								d = int(pair[1])
							}
							want[align+y*stride+x] = byte(min(255, max(0, int(src[align+y*stride+x])+d)))
						}
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("align%d stride%d pair%v inplace%v", align, stride, pair, inplace)
					}
				}
				saved := bytes.Clone(src)
				if AddConstantPair4(src[align:], stride, src[align+1:], stride, 0, 0) || !bytes.Equal(src, saved) {
					t.Fatal("partial-alias fallback wrote")
				}
				if AddConstantPair4(src[align:], stride, src[align:], stride, 256, 0) || !bytes.Equal(src, saved) {
					t.Fatal("delta fallback wrote")
				}
			}
		}
	}
}

// Exhaust the byte/delta domain against scalar clipping. The opposite
// right-hand delta also checks independent packet halves in every case.
func TestDCPairAllBytesAndDeltas(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD disabled")
	}
	var src, dst [32]byte
	for p := 0; p < 256; p++ {
		for i := range src {
			src[i] = byte(p)
		}
		for d := -255; d <= 255; d++ {
			if !AddConstantPair4(src[:], 8, dst[:], 8, int16(d), int16(-d)) {
				t.Fatal("native pair not selected")
			}
			for y := range 4 {
				for x := range 8 {
					delta := d
					if x >= 4 {
						delta = -d
					}
					want := byte(min(255, max(0, p+delta)))
					if dst[y*8+x] != want {
						t.Fatalf("pixel%d delta%d row%d col%d got%d want%d", p, d, y, x, dst[y*8+x], want)
					}
				}
			}
		}
	}
	maxInt := int(^uint(0) >> 1)
	if AddConstantPair4(src[:], maxInt, dst[:], maxInt, 0, 0) {
		t.Fatal("oversized stride accepted")
	}
}
