package dsp

import (
	"bytes"
	"fmt"
	"slices"
	"testing"
)

var subtractRowsNativeSink bool

func BenchmarkSubtractRowsNative(b *testing.B) {
	for _, shape := range [][2]int{{4, 4}, {8, 8}, {16, 8}, {16, 16}, {32, 16}, {32, 32}, {64, 32}, {64, 64}} {
		for _, padded := range []bool{false, true} {
			for pattern := range 3 {
				b.Run(fmt.Sprintf("%dx%d/padded%v/pattern%d", shape[0], shape[1], padded, pattern), func(b *testing.B) {
					w, h := shape[0], shape[1]
					ss, ps, os := w, w, w
					if padded {
						ss += 17
						ps += 23
						os += 9
					}
					sn, pn, on := (h-1)*ss+w, (h-1)*ps+w, (h-1)*os+w
					src, pred, out := make([]byte, 3+sn+11), make([]byte, 5+pn+13), make([]int16, 1+on+7)
					for i := range src {
						src[i] = byte(i*37 + i/11 + 19)
					}
					for i := range pred {
						pred[i] = byte(i*71 + i/7 + 31)
					}
					if pattern == 1 {
						clear(src)
						for i := range pred {
							pred[i] = 255
						}
					} else if pattern == 2 {
						for y := range h {
							copy(pred[5+y*ps:5+y*ps+w], src[3+y*ss:3+y*ss+w])
						}
					}
					for i := range out {
						out[i] = 12345
					}
					a, p, d := src[3:3+sn], pred[5:5+pn], out[1:1+on]
					beforeA, beforeP := bytes.Clone(src), bytes.Clone(pred)
					want := slices.Clone(out)
					wantNZ := false
					for y := range h {
						for x := range w {
							v := int16(a[y*ss+x]) - int16(p[y*ps+x])
							want[1+y*os+x] = v
							wantNZ = wantNZ || v != 0
						}
					}
					nz, ok := SubtractBlockNonZero(a, 0, ss, p, 0, ps, d, 0, os, w, h)
					if !ok || nz != wantNZ || !slices.Equal(out, want) {
						b.Fatal("preflight residual/scalar/canary mismatch")
					}
					fn := SubtractBlockNonZero
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						nz, ok = fn(a, 0, ss, p, 0, ps, d, 0, os, w, h)
					}
					if !ok || nz != wantNZ || !slices.Equal(out, want) || !bytes.Equal(src, beforeA) || !bytes.Equal(pred, beforeP) {
						b.Fatal("final residual/scalar/input/canary mismatch")
					}
					subtractRowsNativeSink = nz
				})
			}
		}
	}
}
