package dsp

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/pixel"
	"os"
	"testing"
)

func TestSADLimitSliceRows(t *testing.T) {
	if os.Getenv("GOVPX_REQUIRE_SIMD") == "1" && !pixel.Enabled {
		t.Fatal("SIMD backend required")
	}
	for _, stride := range []int{0, 1, 15, 16, 19, 1312} {
		for mode := 0; mode < 3; mode++ {
			a, b := make([]byte, 3+15*stride+16)[3:], make([]byte, 5+15*(stride+3)+16)[5:]
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
			ac, bc := bytes.Clone(a), bytes.Clone(b)
			limits := []int{-1, 0, 1024, 65279, 65280, 65281, int(^uint(0) >> 1)}
			for _, h := range []int{1, 2, 4, 8, 15} {
				v := sadLimitRowsOracle(a, stride, b, stride+3, h, 1<<20)
				limits = append(limits, v-1, v, v+1)
			}
			for _, limit := range limits {
				want := sadLimitRowsOracle(a, stride, b, stride+3, 16, limit)
				if got := SAD16x16Limit(a, stride, b, stride+3, limit); got != want {
					t.Fatalf("stride%d mode%d limit%d got%d want%d", stride, mode, limit, got, want)
				}
			}
			if !bytes.Equal(a, ac) || !bytes.Equal(b, bc) {
				t.Fatal("modified inputs")
			}
		}
	}
	backing := make([]byte, 15*35+32)
	for i := range backing {
		backing[i] = byte(i*61 + 17)
	}
	before := bytes.Clone(backing)
	for _, offset := range []int{0, 1, 7, 16} {
		for _, limit := range []int{-1, 0, 1024, 16384, 65280} {
			a, b := backing, backing[offset:]
			want := sadLimitRowsOracle(a, 32, b, 33, 16, limit)
			if got := SAD16x16Limit(a, 32, b, 33, limit); got != want {
				t.Fatalf("alias offset%d limit%d got%d want%d", offset, limit, got, want)
			}
		}
	}
	if !bytes.Equal(backing, before) {
		t.Fatal("alias input changed")
	}
}

// Enabled dispatch historically reconstructs full windows from SliceData.
// Scalar dispatch validates each visible slice before any row-limit exit.
func TestSADLimitSliceVisibleSpan(t *testing.T) {
	a, b := make([]byte, 15*32+16), make([]byte, 15*35+16)
	for i := range a {
		a[i] = byte(i*19 + 11)
	}
	for i := range b {
		b[i] = byte(i*37 + 23)
	}
	for _, limit := range []int{-1, 0, 1024, 16384, 65280} {
		for _, n := range []int{0, 1, -1} {
			na, nb := n, n
			if n < 0 {
				na, nb = len(a)-1, len(b)-1
			}
			for _, tc := range []struct {
				name     string
				src, ref []byte
			}{
				{"source", a[:na], b}, {"reference", a, b[:nb]}, {"both", a[:na], b[:nb]},
			} {
				func() {
					defer func() {
						r := recover()
						if !pixel.Enabled && r == nil {
							t.Errorf("disabled %s short input did not panic n%d limit%d", tc.name, n, limit)
						}
						if pixel.Enabled && r != nil {
							t.Errorf("enabled %s backed short input panicked: %v", tc.name, r)
						}
					}()
					got := SAD16x16Limit(tc.src, 32, tc.ref, 35, limit)
					if pixel.Enabled {
						want := sadLimitRowsOracle(a, 32, b, 35, 16, limit)
						if got != want {
							t.Errorf("%s n%d limit%d got%d want%d", tc.name, n, limit, got, want)
						}
					}
				}()
			}
		}
	}
}
