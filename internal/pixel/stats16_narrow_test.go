package pixel

import (
	"bytes"
	"fmt"
	"testing"
)

func TestStats16NarrowRowsAndBounds(t *testing.T) {
	for _, h := range []int{0, 1, 2, 3, 4, 7, 8, 16, 31, 32, 63, 64, 65, 66} {
		for _, ss := range []int{0, 1, 7, 16, 17, 64} {
			rs := ss + 3
			for align := range 8 {
				for pattern := range 4 {
					n, m := 16, 16
					if h > 0 {
						n += (h - 1) * ss
						m += (h - 1) * rs
					}
					a, b := make([]byte, n+align), make([]byte, m+align)
					for i := range a {
						a[i] = byte(i*71 + 19)
					}
					for i := range b {
						b[i] = byte(i*37 + 91)
					}
					if pattern < 3 {
						pairs := [3][2]byte{{0, 255}, {255, 0}, {255, 255}}
						for i := range a {
							a[i] = pairs[pattern][0]
						}
						for i := range b {
							b[i] = pairs[pattern][1]
						}
					}
					aa, bb := bytes.Clone(a), bytes.Clone(b)
					gotSum, gotSSE := Stats(a[align:], ss, b[align:], rs, 16, h)
					wantSum, wantSSE := StatsScalar(a[align:], ss, b[align:], rs, 16, h)
					if gotSum != wantSum || gotSSE != wantSSE {
						t.Fatalf("h%d ss%d rs%d align%d pattern%d got=(%d,%d) want=(%d,%d)", h, ss, rs, align, pattern, gotSum, gotSSE, wantSum, wantSSE)
					}
					if !bytes.Equal(a, aa) || !bytes.Equal(b, bb) {
						t.Fatal("statistics modified input")
					}
				}
			}
		}
	}
	for _, stride := range []int{-1, int(^uint(0) >> 1)} {
		a, b := bytes.Repeat([]byte{255}, 16), make([]byte, 16)
		gotSum, gotSSE := Stats(a, stride, b, stride, 16, 1)
		wantSum, wantSSE := StatsScalar(a, stride, b, stride, 16, 1)
		if gotSum != wantSum || gotSSE != wantSSE {
			t.Fatal("single row must not use stride")
		}
	}
}

var stats16NarrowSum int
var stats16NarrowSSE uint32

func BenchmarkStats16NarrowNative(b *testing.B) {
	for _, h := range []int{1, 2, 4, 8, 16, 32, 64, 65, 720} {
		b.Run(fmt.Sprintf("h%d", h), func(b *testing.B) {
			a, r := make([]byte, (h-1)*64+16), make([]byte, (h-1)*67+16)
			for i := range a {
				a[i] = byte(i*17 + 91)
			}
			for i := range r {
				r[i] = byte(i*19 + 13)
			}
			b.ReportAllocs()
			for b.Loop() {
				stats16NarrowSum, stats16NarrowSSE = Stats(a, 64, r, 67, 16, h)
			}
		})
	}
}
