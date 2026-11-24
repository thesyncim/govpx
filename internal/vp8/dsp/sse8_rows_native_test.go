package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

var sse8RowsNativeSink int

func BenchmarkSSE8RowsNative(b *testing.B) {
	for _, api := range []struct {
		name string
		h    int
		fn   func([]byte, int, []byte, int) int
	}{{"slice8x4", 4, SSE8x4}, {"slice8x8", 8, SSE8x8}, {"slice8x16", 16, SSE8x16}, {"fast8x8", 8, SSE8x8Fast}} {
		for _, strides := range [][2]int{{8, 8}, {32, 37}, {1312, 1320}} {
			for pattern := range 3 {
				b.Run(fmt.Sprintf("%s/pitch%d-%d/pattern%d", api.name, strides[0], strides[1], pattern), func(b *testing.B) {
					ss, rs := strides[0], strides[1]
					sn, rn := (api.h-1)*ss+8, (api.h-1)*rs+8
					src, ref := make([]byte, 3+sn+13), make([]byte, 5+rn+17)
					for i := range src {
						src[i] = byte(i*37 + i/11 + 19)
					}
					for i := range ref {
						ref[i] = byte(i*71 + i/7 + 31)
					}
					if pattern == 1 {
						clear(src)
						for i := range ref {
							ref[i] = 255
						}
					} else if pattern == 2 {
						for y := range api.h {
							copy(ref[5+y*rs:5+y*rs+8], src[3+y*ss:3+y*ss+8])
						}
					}
					a, r := src[3:3+sn], ref[5:5+rn]
					beforeA, beforeB := bytes.Clone(src), bytes.Clone(ref)
					want := 0
					for y := range api.h {
						for x := range 8 {
							d := int(a[y*ss+x]) - int(r[y*rs+x])
							want += d * d
						}
					}
					got := api.fn(a, ss, r, rs)
					if got != want {
						b.Fatalf("public SSE8 got%d want%d", got, want)
					}
					b.ReportAllocs()
					b.ResetTimer()
					fn := api.fn
					for b.Loop() {
						got = fn(a, ss, r, rs)
					}
					if got != want || !bytes.Equal(src, beforeA) || !bytes.Equal(ref, beforeB) {
						b.Fatal("final SSE8 result, immutable inputs or backing canaries changed")
					}
					sse8RowsNativeSink = got
				})
			}
		}
	}
}
