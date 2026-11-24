//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"os"
	"testing"
)

func TestSubpelPointerNativeSelected(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("native SIMD unavailable")
	}
	const w = 32
	for _, h := range []int{1, 3, 8, 16, 32, 65} {
		for _, strides := range [][2]int{{0, 0}, {1, 3}, {33, 32}, {49, 57}} {
			ss, rs := strides[0], strides[1]
			for align := range 8 {
				sn, rn := h*ss+w+1, (h-1)*rs+w
				shared := make([]byte, align+max(sn, rn)+17)
				for i := range shared {
					shared[i] = byte(i*37 + i/7 + 13)
				}
				src, ref := shared[align:align+sn], shared[align:align+rn]
				before := bytes.Clone(shared)
				for x := 0; x < 8; x++ {
					for y := 0; y < 8; y++ {
						fx, fy := [2]int16{int16(128 - 16*x), int16(16 * x)}, [2]int16{int16(128 - 16*y), int16(16 * y)}
						a, b := SubpelStats(src, ss, ref, rs, w, h, fx, fy)
						c, d := SubpelStatsScalar(src, ss, ref, rs, w, h, fx, fy)
						if a != c || b != d {
							t.Fatalf("h%d strides%v align%d phase%d,%d got%d,%d want%d,%d", h, strides, align, x, y, a, b, c, d)
						}
					}
				}
				if !bytes.Equal(shared, before) {
					t.Fatal("aliased read-only input changed")
				}
			}
		}
	}
}

func TestSubpelPointerGeometryFallback(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("native SIMD unavailable")
	}
	src, ref := make([]byte, 17), make([]byte, 16)
	for i := range src {
		src[i] = byte(i * 29)
	}
	for i := range ref {
		ref[i] = byte(i * 17)
	}
	fx, fy := [2]int16{80, 48}, [2]int16{48, 80}
	for _, rs := range []int{1 << 30, int(^uint(0) >> 1)} {
		a, b := SubpelStats(src, 0, ref, rs, 16, 1, fx, fy)
		c, d := SubpelStatsScalar(src, 0, ref, rs, 16, 1, fx, fy)
		if a != c || b != d {
			t.Fatalf("unusual stride fallback mismatch rs%d", rs)
		}
	}
	for _, sizes := range [][2]int{{0, 0}, {16, 16}, {17, 15}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("short span accepted %v", sizes)
				}
			}()
			SubpelStats(src[:sizes[0]], 0, ref[:sizes[1]], 0, 16, 1, fx, fy)
		}()
	}
}
