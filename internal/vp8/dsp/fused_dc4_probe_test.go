package dsp

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"testing"
)

func fusedDC4Prior(dst []byte, ds int, above, left []byte, n int, up, lf bool) {
	_ = above[3]
	_ = left[3]
	_ = dst[3*ds+3]
	if pixel.Enabled {
		intraDCGo(dst, ds, above, left, 4, true, true)
	} else {
		intra4x4DCPredictScalar(dst, ds, above, left)
	}
}
func fusedDC4Call(dst []byte, ds int, above, left []byte, n int, up, lf bool) {
	Intra4x4DCPredict(dst, ds, above, left)
}
func fusedDC4Scalar(dst []byte, ds int, above, left []byte, n int, up, lf bool) {
	sum, count, dc := 0, 0, 128
	if up {
		for i := range n {
			sum += int(above[i])
		}
		count += n
	}
	if lf {
		for i := range n {
			sum += int(left[i])
		}
		count += n
	}
	if count > 0 {
		dc = (sum + count/2) / count
	}
	for y := range n {
		for x := range n {
			dst[y*ds+x] = byte(dc)
		}
	}
}
func fusedDC4Capture(f func()) (panicValue string) {
	defer func() {
		if x := recover(); x != nil {
			panicValue = fmt.Sprint(x)
		}
	}()
	f()
	return
}
func TestFusedDC4AllSums(t *testing.T) {
	for _, n := range []int{4} {
		for _, up := range []bool{true} {
			for _, lf := range []bool{true} {
				count := 0
				if up {
					count += n
				}
				if lf {
					count += n
				}
				for total := 0; total <= count*255; total++ {
					a, l := make([]byte, n), make([]byte, n)
					remaining := total
					if up {
						for i := range n {
							a[i] = byte(min(255, remaining))
							remaining -= int(a[i])
						}
					}
					if lf {
						for i := range n {
							l[i] = byte(min(255, remaining))
							remaining -= int(l[i])
						}
					}
					if !up {
						a = nil
					}
					if !lf {
						l = nil
					}
					const off = 7
					size := n * n
					got, want := make([]byte, off+size+13), make([]byte, off+size+13)
					for i := range got {
						got[i] = 19
					}
					copy(want, got)
					fusedDC4Scalar(want[off:off+size:off+size], n, a, l, n, up, lf)
					fusedDC4Call(got[off:off+size:off+size], n, a, l, n, up, lf)
					if !bytes.Equal(got, want) {
						t.Fatalf("n%d up%v lf%v total%d", n, up, lf, total)
					}
				}
			}
		}
	}
}
func TestFusedDC4FullBackingAliases(t *testing.T) {
	for _, n := range []int{4} {
		for _, ds := range []int{0, 1, n - 1, n, n + 3, 37, 1312} {
			for _, align := range []int{0, 1, 7, 15} {
				for _, mode := range []int{3} {
					up, lf := mode&1 != 0, mode&2 != 0
					for alias := range 5 {
						size := (n-1)*ds + n
						off := 31 + align
						a, b := make([]byte, off+size+2*n+73), make([]byte, off+size+2*n+73)
						for i := range a {
							a[i] = byte(i*37 + i/7 + 11)
						}
						copy(b, a)
						ao, lo := off+size+7, off+size+7+n
						if alias == 1 {
							ao = off
						}
						if alias == 2 {
							lo = off
						}
						if alias == 3 {
							ao, lo = off, off
						}
						if alias == 4 {
							lo = ao + 1
						}
						aa, al := a[ao:ao+n:ao+n], a[lo:lo+n:lo+n]
						ba, bl := b[ao:ao+n:ao+n], b[lo:lo+n:lo+n]
						fusedDC4Scalar(b[off:off+size:off+size], ds, ba, bl, n, up, lf)
						fusedDC4Call(a[off:off+size:off+size], ds, aa, al, n, up, lf)
						if !bytes.Equal(a, b) {
							t.Fatalf("n%d ds%d align%d mode%d alias%d", n, ds, align, mode, alias)
						}
					}
				}
			}
		}
	}
}
func TestFusedDC4FallbackPartialPanic(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, n := range []int{4} {
		for _, ds := range []int{-1, 0, 1, n - 1, n, n + 3, 37, 1312, (1 << 30) - 1, 1 << 30, maxInt, -maxInt - 1} {
			for _, mode := range []int{3} {
				up, lf := mode&1 != 0, mode&2 != 0
				span := n
				if ds >= 0 && ds <= 1312 {
					span = (n-1)*ds + n
				}
				space := span + 2*n + 71
				for _, dwin := range [][2]int{{0, 0}, {0, span}, {span - 1, span - 1}, {span - 1, span}, {span, span}} {
					for _, rlen := range []int{0, 1, n - 1, n} {
						for _, clip := range []bool{false, true} {
							for alias := range 4 {
								const off = 7
								a, b := make([]byte, space+off), make([]byte, space+off)
								for i := range a {
									a[i] = byte(i*37 + 11)
								}
								copy(b, a)
								ao, lo := off+span+11, off+span+11+n
								if alias == 1 {
									ao = off
								}
								if alias == 2 {
									lo = off
								}
								if alias == 3 {
									ao, lo = off, off
								}
								ac, lc := n, n
								if clip {
									ac, lc = rlen, rlen
								}
								aa, al := a[ao:ao+rlen:ao+ac], a[lo:lo+rlen:lo+lc]
								ba, bl := b[ao:ao+rlen:ao+ac], b[lo:lo+rlen:lo+lc]
								gp := fusedDC4Capture(func() { fusedDC4Call(a[off:off+dwin[0]:off+dwin[1]], ds, aa, al, n, up, lf) })
								wp := fusedDC4Capture(func() { fusedDC4Prior(b[off:off+dwin[0]:off+dwin[1]], ds, ba, bl, n, up, lf) })
								if gp != wp || !bytes.Equal(a, b) {
									t.Fatalf("n%d ds%d mode%d dst%v refs%d clip%v alias%d panic%q/%q", n, ds, mode, dwin, rlen, clip, alias, gp, wp)
								}
							}
						}
					}
				}
			}
		}
	}
}
