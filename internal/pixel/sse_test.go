package pixel

import (
	"bytes"
	"testing"
)

func TestSquaredErrorOnly(t *testing.T) {
	for _, w := range []int{8, 16} {
		for _, h := range []int{0, 1, 3, 16, 65, 720, 8193} {
			ss, rs := w+3, w+7
			for align := range 4 {
				for _, pair := range [][2]byte{{0, 255}, {255, 0}, {255, 255}, {37, 149}} {
					ns, nr := 0, 0
					if h > 0 {
						ns, nr = (h-1)*ss+w, (h-1)*rs+w
					}
					a := bytes.Repeat([]byte{pair[0]}, align+ns)[align:]
					b := bytes.Repeat([]byte{pair[1]}, align+nr)[align:]
					d := int(pair[0]) - int(pair[1])
					want := uint32(d * d * w * h)
					var got uint32
					if w == 8 {
						got = SSE8(a, ss, b, rs, h)
					} else {
						got = SSE16(a, ss, b, rs, h)
					}
					if got != want {
						t.Fatalf("%dx%d align%d %v: %d != %d", w, h, align, pair, got, want)
					}
				}
			}
		}
	}
	for _, h := range []int{1, 3, 16, 720} {
		a, b := make([]byte, 19*h), make([]byte, 23*h)
		for i := range a {
			a[i] = byte(i*37 + 11)
		}
		for i := range b {
			b[i] = byte(i*19 + 53)
		}
		for _, w := range []int{8, 16} {
			_, want := StatsScalar(a, 19, b, 23, w, h)
			var got, alias uint32
			if w == 8 {
				got, alias = SSE8(a, 19, b, 23, h), SSE8(a, 19, a, 19, h)
			} else {
				got, alias = SSE16(a, 19, b, 23, h), SSE16(a, 19, a, 19, h)
			}
			if got != want || alias != 0 {
				t.Fatalf("random/alias %dx%d: %d/%d != %d/0", w, h, got, alias, want)
			}
		}
	}
}

// Read-only rows may overlap, including a zero stride. SIMD must preserve
// the scalar sum for those layouts without reading beyond the last window.
func TestSquaredErrorOverlappingRows(t *testing.T) {
	for _, w := range []int{8, 16} {
		for _, h := range []int{1, 3, 65, 8193} {
			for _, ss := range []int{0, 1, w - 1} {
				for _, rs := range []int{0, 2, w - 2} {
					a, b := make([]byte, (h-1)*ss+w+3), make([]byte, (h-1)*rs+w+5)
					a, b = a[3:], b[5:]
					for i := range a {
						a[i] = byte(i*19 + 53)
					}
					for i := range b {
						b[i] = byte(i*37 + 11)
					}
					_, want := StatsScalar(a, ss, b, rs, w, h)
					var got uint32
					if w == 8 {
						got = SSE8(a, ss, b, rs, h)
					} else {
						got = SSE16(a, ss, b, rs, h)
					}
					if got != want {
						t.Fatalf("%dx%d strides %d/%d: %d != %d", w, h, ss, rs, got, want)
					}
				}
			}
		}
	}
}
