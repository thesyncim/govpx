package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

func fusedDCSupplementCall(dst []byte, ds, n int, above, left []byte, up, lf bool) {
	if n == 4 {
		Intra4x4DCPredict(dst, ds, above, left)
	} else if n == 8 {
		IntraDCPredict8x8(dst, ds, above, left, up, lf)
	} else {
		IntraDCPredict16x16(dst, ds, above, left, up, lf)
	}
}
func TestFusedDCSupplementState(t *testing.T) {
	for _, n := range []int{4, 8, 16} {
		for _, ds := range []int{n, 37, 1312} {
			for _, align := range []int{0, 1, 7, 15} {
				for _, mode := range []int{0, 1, 2} {
					if (n == 4) != (mode == 2) {
						continue
					}
					up, lf := mode != 1, mode != 0
					for _, alias := range []int{0, 1, 2, 3} {
						size := (n-1)*ds + n
						storage := make([]byte, align+size+23)
						for i := range storage {
							storage[i] = byte(i*37 + 11)
						}
						expected := bytes.Clone(storage)
						above, left := make([]byte, n+17), make([]byte, n+17)
						for i := range above {
							above[i] = byte(i*17 + 91)
							left[i] = byte(i*23 + 251)
						}
						a, l := above[7:7+n:7+n], left[7:7+n:7+n]
						ra, rl := bytes.Clone(a), bytes.Clone(l)
						beforeA, beforeL := bytes.Clone(above), bytes.Clone(left)
						if alias&1 != 0 {
							a = storage[align : align+n : align+n]
							ra = expected[align : align+n : align+n]
						}
						if alias&2 != 0 {
							l = storage[align+size-n : align+size : align+size]
							rl = expected[align+size-n : align+size : align+size]
						}
						fillDCTestOracle(expected[align:align+size:align+size], ds, n, ra, rl, up, lf)
						fusedDCSupplementCall(storage[align:align+size:align+size], ds, n, a, l, up, lf)
						if !bytes.Equal(storage, expected) || !bytes.Equal(above, beforeA) || !bytes.Equal(left, beforeL) {
							t.Fatalf("n%d ds%d align%d mode%d alias%d", n, ds, align, mode, alias)
						}
					}
				}
			}
		}
	}
}
func BenchmarkIntraDCFillSupplement(b *testing.B) {
	for _, n := range []int{4, 8, 16} {
		for _, mode := range []int{0, 1, 2} {
			if (n == 4) != (mode == 2) {
				continue
			}
			name := "up-only"
			if mode == 1 {
				name = "left-only"
			}
			if mode == 2 {
				name = "both"
			}
			up, lf := mode != 1, mode != 0
			for _, ds := range []int{n, 37, 1312} {
				b.Run(fmt.Sprintf("n%d/%s/pitch%d", n, name, ds), func(b *testing.B) {
					size := (n-1)*ds + n
					storage := make([]byte, size+31)
					for i := range storage {
						storage[i] = byte(i*19 + 7)
					}
					above, left := make([]byte, n+17), make([]byte, n+17)
					for i := range above {
						above[i] = byte(i*17 + 91)
						left[i] = byte(i*23 + 251)
					}
					beforeA, beforeL := bytes.Clone(above), bytes.Clone(left)
					a, l := above[7:7+n:7+n], left[7:7+n:7+n]
					expected := bytes.Clone(storage)
					dst := storage[7 : 7+size : 7+size]
					fillDCTestOracle(expected[7:7+size:7+size], ds, n, a, l, up, lf)
					// Validate the actual public call before entering the timed loop.
					fusedDCSupplementCall(dst, ds, n, a, l, up, lf)
					if !bytes.Equal(storage, expected) || !bytes.Equal(above, beforeA) || !bytes.Equal(left, beforeL) {
						b.Fatal("preflight full output/input/canary mismatch")
					}
					b.ReportAllocs()
					for b.Loop() {
						if n == 4 {
							Intra4x4DCPredict(dst, ds, a, l)
						} else if n == 8 {
							IntraDCPredict8x8(dst, ds, a, l, up, lf)
						} else {
							IntraDCPredict16x16(dst, ds, a, l, up, lf)
						}
					}
					if !bytes.Equal(storage, expected) || !bytes.Equal(above, beforeA) || !bytes.Equal(left, beforeL) {
						b.Fatal("full output/input/canary mismatch")
					}
					fillDCNativeSink = dst[len(dst)-1]
				})
			}
		}
	}
}
