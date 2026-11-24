//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"os"
	"testing"
)

func TestSSE8ExactRowsScalar(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("native SIMD disabled")
	}
	for _, h := range []int{1, 2, 3, 4, 8, 16, 63, 64, 65} {
		for _, strides := range [][2]int{{0, 0}, {1, 3}, {8, 8}, {9, 17}, {31, 37}} {
			ss, rs := strides[0], strides[1]
			for align := range 8 {
				for _, extreme := range []bool{false, true} {
					sn, rn := (h-1)*ss+8, (h-1)*rs+8
					src, ref := make([]byte, align+sn+13), make([]byte, align+1+rn+17)
					for i := range src {
						src[i] = byte(i*37 + i/7 + 11)
					}
					for i := range ref {
						ref[i] = byte(i*71 + i/13 + 29)
					}
					if extreme {
						clear(src)
						for i := range ref {
							ref[i] = 255
						}
					}
					a, b := src[align:align+sn], ref[align+1:align+1+rn]
					beforeA, beforeB := bytes.Clone(src), bytes.Clone(ref)
					if got, want := SSE8(a, ss, b, rs, h), SSE8Scalar(a, ss, b, rs, h); got != want {
						t.Fatalf("h%d strides%v align%d extreme%v got%d want%d", h, strides, align, extreme, got, want)
					}
					if !bytes.Equal(src, beforeA) || !bytes.Equal(ref, beforeB) {
						t.Fatal("read-only input changed")
					}
					shared := make([]byte, max(3+sn, 5+rn)+19)
					for i := range shared {
						shared[i] = byte(i*43 + i/11)
					}
					a, b = shared[3:3+sn], shared[5:5+rn]
					before := bytes.Clone(shared)
					if SSE8(a, ss, b, rs, h) != SSE8Scalar(a, ss, b, rs, h) || !bytes.Equal(shared, before) {
						t.Fatal("mutually aliased read-only rows differ")
					}
				}
			}
		}
	}
}

func TestSSE8ExactRowsBoundsAndFallback(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("native SIMD disabled")
	}
	for _, h := range []int{-1, 0} {
		if SSE8(nil, -1, nil, -1, h) != 0 {
			t.Fatal("empty geometry changed")
		}
	}
	maxInt := int(^uint(0) >> 1)
	for _, stride := range []int{-maxInt - 1, -1, 1 << 30, maxInt} {
		a, b := bytes.Repeat([]byte{255}, 8), make([]byte, 8)
		if SSE8(a, stride, b, stride, 1) != 8*255*255 {
			t.Fatalf("single-row unused stride %d changed", stride)
		}
	}
	for _, h := range []int{1, 2, 3, 8, 16, 64, 65} {
		ss, rs := 9, 17
		src, ref := make([]byte, (h-1)*ss+8), make([]byte, (h-1)*rs+8)
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
						t.Fatalf("short window accepted h%d source%v", h, shortSource)
					}
				}()
				SSE8(a, ss, b, rs, h)
			}()
		}
	}
}

// Exercise every byte pair through the bounded SIMD path. Each call handles
// 128 distinct pairs, including both directions of the maximum difference.
func TestSSE8ExactRowsEveryBytePair(t *testing.T) {
	if !Enabled {
		if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" {
			t.Fatal("required SIMD unavailable")
		}
		t.Skip("native SIMD disabled")
	}
	src, ref := make([]byte, 128), make([]byte, 128)
	for base := 0; base < 1<<16; base += len(src) {
		var want uint32
		for i := range src {
			pair := base + i
			src[i], ref[i] = byte(pair>>8), byte(pair)
			d := int(src[i]) - int(ref[i])
			want += uint32(d * d)
		}
		if got := SSE8(src, 8, ref, 8, 16); got != want {
			t.Fatalf("byte pairs %d..%d: got %d want %d", base, base+127, got, want)
		}
	}
}
