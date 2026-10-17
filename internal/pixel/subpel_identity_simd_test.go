//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"os"
	"testing"
)

func TestSubpelIdentityScalar(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("native SIMD unavailable")
	}
	for _, w := range []int{4, 8, 16, 24, 32, 64} {
		for _, h := range []int{1, 8, 16, 64} {
			for _, strides := range [][2]int{{0, 0}, {1, 3}, {w + 1, w + 7}} {
				ss, rs := strides[0], strides[1]
				sn, rn := h*ss+w+1, (h-1)*rs+w
				shared := make([]byte, max(3+sn, 5+rn)+13)
				for i := range shared {
					shared[i] = byte(i*37 + i/11 + 13)
				}
				src, ref := shared[3:3+sn], shared[5:5+rn]
				before := bytes.Clone(shared)
				f := [2]int16{128, 0}
				a, b := SubpelStats(src, ss, ref, rs, w, h, f, f)
				c, d := SubpelStatsScalar(src, ss, ref, rs, w, h, f, f)
				if a != c || b != d {
					t.Fatalf("w%d h%d strides%v got%d,%d want%d,%d", w, h, strides, a, b, c, d)
				}
				if !bytes.Equal(shared, before) {
					t.Fatal("aliased read-only input changed")
				}
			}
		}
	}
}

func TestSubpelIdentityPreservesBilinearWindow(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("native SIMD unavailable")
	}
	for _, w := range []int{4, 8, 16, 32, 64} {
		for _, h := range []int{1, 8, 16, 64} {
			ss, rs := w+1, w+7
			sn, rn := h*ss+w+1, (h-1)*rs+w
			src, ref := bytes.Repeat([]byte{137}, sn), bytes.Repeat([]byte{91}, rn)
			f := [2]int16{128, 0}
			for _, shortSource := range []bool{false, true} {
				a, b := src, ref
				if shortSource {
					a = a[:len(a)-1]
				} else {
					b = b[:len(b)-1]
				}
				func() {
					defer func() {
						if recover() == nil {
							t.Fatalf("short bilinear window accepted w%d h%d source%v", w, h, shortSource)
						}
					}()
					SubpelStats(a, ss, b, rs, w, h, f, f)
				}()
			}
		}
	}
}
