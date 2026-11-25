package pixel

import (
	"bytes"
	"testing"
)

func planeSSEScalar(a []byte, ss int, b []byte, rs, w, h int) uint64 {
	var sum uint64
	for y := range h {
		for x := range w {
			d := int(a[y*ss+x]) - int(b[y*rs+x])
			sum += uint64(d * d)
		}
	}
	return sum
}

func TestSSEPlane16Scalar(t *testing.T) {
	if !Enabled {
		if _, ok := SSEPlane16(nil, 16, nil, 16, 16, 1); ok {
			t.Fatal("disabled backend accepted")
		}
		return
	}
	for _, w := range []int{16, 32, 128, 1280, 16384} {
		for _, h := range []int{1, 2, 3, 4, 5, 17, 64} {
			for mode := range 3 {
				ss, rs := w+3, w+7
				a, b := make([]byte, 3+(h-1)*ss+w)[3:], make([]byte, 5+(h-1)*rs+w)[5:]
				for i := range a {
					if mode == 0 {
						a[i] = byte(i*37 + 11)
					} else if mode == 1 {
						a[i] = 255
					}
				}
				for i := range b {
					if mode == 0 {
						b[i] = byte(i*19 + 53)
					} else if mode == 2 {
						b[i] = 255
					}
				}
				want := planeSSEScalar(a, ss, b, rs, w, h)
				got, ok := SSEPlane16(a, ss, b, rs, w, h)
				if !ok || got != want {
					t.Fatalf("%dx%d mode%d got%d want%d accepted%v", w, h, mode, got, want, ok)
				}
			}
		}
	}
	for _, w := range []int{16, 32} {
		for _, h := range []int{1, 5, 64, 4096} {
			for _, stride := range []int{0, 1, w - 1} {
				storage := make([]byte, (h-1)*stride+w+3)
				for i := range storage {
					storage[i] = byte(i*37 + 11)
				}
				a, b := storage[:len(storage)-3], storage[3:]
				want := planeSSEScalar(a, stride, b, stride, w, h)
				got, ok := SSEPlane16(a, stride, b, stride, w, h)
				if !ok || got != want {
					t.Fatalf("overlap%dx%d stride%d got%d want%d", w, h, stride, got, want)
				}
			}
		}
	}
}

func TestSSEPlane16DeclinesUnsupported(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, args := range [][4]int{{16, 16, 0, 1}, {16, 16, 15, 1}, {16, 16, 17, 1}, {16, 16, 16385, 1}, {16, 16, 16, 0}, {16, 16, 16, -1}, {16, 16, 16, 4097}, {-1, 16, 16, 1}, {16, -1, 16, 1}, {maxInt, 16, 16, 2}, {16, maxInt, 16, 2}, {16, 16, maxInt, 1}, {16, 16, 16, maxInt}} {
		if _, ok := SSEPlane16(make([]byte, 32), args[0], make([]byte, 32), args[1], args[2], args[3]); ok {
			t.Fatalf("accepted%v", args)
		}
	}
	for which := range 2 {
		for _, n := range []int{0, 1, 15, 31, 62} {
			a, b := make([]byte, 128), make([]byte, 128)
			a, b = a[:64], b[:64]
			if which == 0 {
				a = a[:n]
			} else {
				b = b[:n]
			}
			if _, ok := SSEPlane16(a, 16, b, 16, 16, 4); ok {
				t.Fatalf("accepted short buffer%d len%d", which, n)
			}
		}
	}
}

func TestSSEPlane16BoundaryGuards(t *testing.T) {
	if _, ok := SSEPlane16(make([]byte, 16), 0, make([]byte, 16), 0, 16, 4097); ok {
		t.Fatal("accepted height beyond exact column sum")
	}
	if _, ok := SSEPlane16(make([]byte, 16400), 0, make([]byte, 16400), 0, 16400, 1); ok {
		t.Fatal("accepted width beyond exact chunk sum")
	}
	if !Enabled {
		return
	}
	a, b := make([]byte, 32), make([]byte, 32)
	for i := range a {
		a[i] = 255
	}
	got, ok := SSEPlane16(a, 0, b, 0, 32, 4096)
	want := uint64(32) * 4096 * 255 * 255
	if !ok || got != want {
		t.Fatalf("wide total got %d accepted %v want %d", got, ok, want)
	}
	maxInt := int(^uint(0) >> 1)
	got, ok = SSEPlane16(a, maxInt, b, maxInt, 32, 1)
	if !ok || got != uint64(32*255*255) {
		t.Fatalf("one row huge strides got %d accepted %v", got, ok)
	}
}

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
