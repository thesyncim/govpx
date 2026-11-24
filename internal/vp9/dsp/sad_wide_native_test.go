package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

var wideSADNativeSink uint32

func BenchmarkVP9WideSADNative(b *testing.B) {
	for _, shape := range []struct {
		w, h    int
		fn      func([]byte, int, int, []byte, int, int) uint32
		control bool
	}{
		{32, 16, VpxSad32x16, false}, {32, 32, VpxSad32x32, false}, {32, 64, VpxSad32x64, false}, {64, 32, VpxSad64x32, false}, {64, 64, VpxSad64x64, false},
		{8, 8, VpxSad8x8, true}, {8, 16, VpxSad8x16, true}, {16, 16, VpxSad16x16, true},
	} {
		for _, layout := range []string{"packed", "padded", "largepitch"} {
			for _, pattern := range []string{"patterned", "zero", "max"} {
				if shape.control && pattern != "patterned" {
					continue
				}
				b.Run(fmt.Sprintf("%dx%d/%s/%s", shape.w, shape.h, layout, pattern), func(b *testing.B) {
					ss, rs := shape.w, shape.w
					if layout == "padded" {
						ss += 5
						rs += 11
					}
					if layout == "largepitch" {
						ss, rs = 1312, 1320
					}
					const so, ro = 5, 11
					a := make([]byte, so+(shape.h-1)*ss+shape.w+19)
					r := make([]byte, ro+(shape.h-1)*rs+shape.w+23)
					for i := range a {
						a[i] = byte(i*37 + i/5 + 11)
					}
					for i := range r {
						r[i] = byte(i*29 + i/7 + 17)
					}
					if pattern != "patterned" {
						for i := range a {
							a[i] = 0
						}
						for i := range r {
							r[i] = 0
						}
						if pattern == "max" {
							for i := range r {
								r[i] = 255
							}
						}
					}
					// Pass exact minimum visible windows while keeping backing canaries.
					av, rv := a[:len(a)-19], r[:len(r)-23]
					beforeA, beforeR := bytes.Clone(a), bytes.Clone(r)
					var want uint32
					for y := range shape.h {
						for x := range shape.w {
							d := int(av[so+y*ss+x]) - int(rv[ro+y*rs+x])
							if d < 0 {
								d = -d
							}
							want += uint32(d)
						}
					}
					if got := shape.fn(av, so, ss, rv, ro, rs); got != want {
						b.Fatalf("oracle before timing got%d want%d", got, want)
					}
					b.ReportAllocs()
					var got uint32
					for b.Loop() {
						got = shape.fn(av, so, ss, rv, ro, rs)
					}
					if got != want || !bytes.Equal(a, beforeA) || !bytes.Equal(r, beforeR) {
						b.Fatal("result or input canary changed")
					}
					wideSADNativeSink = got
				})
			}
		}
	}
}
