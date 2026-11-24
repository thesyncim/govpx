package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

var fillDCNativeSink byte

func fillDCTestOracle(dst []byte, ds, n int, above, left []byte, up, lf bool) {
	dc, sum, count := 128, 0, 0
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
func fillDCNativeCall(dst []byte, ds, n int, above, left []byte, up, lf bool) {
	if n == 8 {
		IntraDCPredict8x8(dst, ds, above, left, up, lf)
	} else {
		IntraDCPredict16x16(dst, ds, above, left, up, lf)
	}
}
func TestIntraDCFillExactState(t *testing.T) {
	for _, n := range []int{8, 16} {
		for _, ds := range []int{n, 37, 1312} {
			for _, align := range []int{0, 1, 7, 15} {
				for _, both := range []bool{false, true} {
					for _, alias := range []int{0, 1, 2} {
						size := (n-1)*ds + n
						a, b := make([]byte, align+size+23), make([]byte, align+size+23)
						for i := range a {
							a[i] = byte(i*37 + 11)
						}
						copy(b, a)
						above, left := make([]byte, n), make([]byte, n)
						for i := range n {
							above[i] = byte(i*17 + 91)
							left[i] = byte(i*23 + 251)
						}
						ra, rl := bytes.Clone(above), bytes.Clone(left)
						if alias == 1 {
							above = a[align : align+n]
							ra = b[align : align+n]
						}
						if alias == 2 {
							left = a[align : align+n]
							rl = b[align : align+n]
						}
						fillDCTestOracle(b[align:align+size:align+size], ds, n, ra, rl, both, both)
						fillDCNativeCall(a[align:align+size:align+size], ds, n, above, left, both, both)
						if !bytes.Equal(a, b) {
							t.Fatalf("n%d ds%d align%d both%v alias%d", n, ds, align, both, alias)
						}
					}
				}
			}
		}
	}
}
func BenchmarkIntraDCFillExact(b *testing.B) {
	for _, n := range []int{8, 16} {
		for _, both := range []bool{false, true} {
			mode := "none"
			if both {
				mode = "both"
			}
			for _, ds := range []int{n, 37, 1312} {
				b.Run(fmt.Sprintf("n%d/%s/pitch%d", n, mode, ds), func(b *testing.B) {
					size := (n-1)*ds + n
					storage := make([]byte, size+31)
					for i := range storage {
						storage[i] = byte(i*19 + 7)
					}
					above, left := make([]byte, n), make([]byte, n)
					for i := range n {
						above[i] = byte(i*17 + 91)
						left[i] = byte(i*23 + 251)
					}
					beforeA, beforeL := bytes.Clone(above), bytes.Clone(left)
					expected := bytes.Clone(storage)
					dst := storage[7 : 7+size : 7+size]
					fillDCTestOracle(expected[7:7+size:7+size], ds, n, above, left, both, both)
					b.ReportAllocs()
					for b.Loop() {
						if n == 8 {
							IntraDCPredict8x8(dst, ds, above, left, both, both)
						} else {
							IntraDCPredict16x16(dst, ds, above, left, both, both)
						}
					}
					if !bytes.Equal(storage, expected) || !bytes.Equal(above, beforeA) || !bytes.Equal(left, beforeL) {
						b.Fatal("full output/context/canary mismatch")
					}
					fillDCNativeSink = dst[len(dst)-1]
				})
			}
		}
	}
}
