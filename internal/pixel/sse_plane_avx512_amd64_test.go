//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"simd/archsimd"
	"testing"
)

func TestSSEPlane16AVX512(t *testing.T) {
	t.Logf("AVX2=%v AVX512=%v; production AVX512 threshold: width >=128 and divisible by32", archsimd.X86.AVX2(), archsimd.X86.AVX512())
	if !Enabled || !archsimd.X86.AVX512() {
		t.Skip("AVX512 unavailable: public fallback is covered by TestSSEPlane16Scalar")
	}
	for _, w := range []int{112, 128, 144, 160, 176, 256, 1280, 16384} {
		for _, h := range []int{1, 2, 3, 4, 5, 7, 64} {
			for _, stride := range []int{0, 1, w - 1, w, w + 17} {
				for mode := range 3 {
					n := (h-1)*stride + w
					store := make([]byte, n+5)
					for i := range store {
						if mode == 0 {
							store[i] = byte(i*37 + 11)
						} else if i&1 == 0 {
							store[i] = 255
						}
					}
					a, b := store[:n:n], store[5:5+n:5+n]
					if mode == 1 {
						a = make([]byte, n)
						for i := range a {
							a[i] = 255
						}
						b = make([]byte, n)
					}
					if mode == 2 {
						b = a
					}
					want := planeSSEScalar(a, stride, b, stride, w, h)
					got, ok := SSEPlane16(a, stride, b, stride, w, h)
					if !ok || got != want {
						t.Fatalf("%dx%d stride%d mode%d got%d want%d accepted%v", w, h, stride, mode, got, want, ok)
					}
				}
			}
		}
	}
	// Every lane and the complete four-row chunk approach their maximum.
	// The frame total deliberately exceeds uint32 and must widen per chunk.
	const w, h = 16384, 4096
	a, b := make([]byte, w), make([]byte, w)
	for i := range a {
		a[i] = 255
	}
	got, ok := SSEPlane16(a, 0, b, 0, w, h)
	want := uint64(w) * h * 255 * 255
	if !ok || got != want {
		t.Fatalf("maximum chunk/frame got%d want%d accepted%v", got, want, ok)
	}
	maxInt := int(^uint(0) >> 1)
	got, ok = SSEPlane16(a, maxInt, b, maxInt, w, 1)
	if !ok || got != uint64(w)*255*255 {
		t.Fatalf("unused terminal stride got%d accepted%v", got, ok)
	}
	for _, width := range []int{128, 160, 16384} {
		for _, short := range []int{1, width - 1} {
			for which := range 2 {
				a, b := make([]byte, width*3), make([]byte, width*3)
				if which == 0 {
					a = a[:len(a)-short]
				} else {
					b = b[:len(b)-short]
				}
				if _, ok := SSEPlane16(a, width, b, width, width, 3); ok {
					t.Fatalf("short input%d width%d missing%d accepted", which, width, short)
				}
			}
		}
	}
}
