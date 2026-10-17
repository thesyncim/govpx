package dsp

import (
	"fmt"
	"testing"
)

var sadLimitX4NativeSink [4]uint32

func BenchmarkSADLimitX4Native(b *testing.B) {
	for _, pitch := range []int{32, 1312} {
		for _, extreme := range []bool{false, true} {
			for _, tc := range []struct {
				name  string
				limit int32
			}{{"bound", 65280}, {"wide", 1 << 20}, {"near", 65279}, {"early", 1024}, {"middle", 16384}} {
				b.Run(fmt.Sprintf("pitch%d/extreme%v/%s", pitch, extreme, tc.name), func(b *testing.B) {
					ss, rs := pitch, pitch+32
					a := make([]byte, 3+15*ss+16)[3:]
					var refs [4][]byte
					for i := range a {
						if !extreme {
							a[i] = byte(i*37 + 11)
						}
					}
					var want, out [4]uint32
					limits := [4]int32{tc.limit, tc.limit, tc.limit, tc.limit}
					for n := range refs {
						refs[n] = make([]byte, 5+n+15*rs+16)[5+n:]
						for i := range refs[n] {
							if extreme {
								refs[n][i] = 255
							} else {
								refs[n][i] = byte(i*(19+n*2) + 53 + n*7)
							}
						}
						want[n] = uint32(sadLimitRowsOracle(a, ss, refs[n], rs, 16, int(limits[n])))
					}
					b.ReportAllocs()
					for b.Loop() {
						SAD16x16x4LimitPtrFast(&a[0], ss, &refs[0][0], &refs[1][0], &refs[2][0], &refs[3][0], rs, &limits, &out)
					}
					if out != want {
						b.Fatalf("got %v want %v", out, want)
					}
					sadLimitX4NativeSink = out
				})
			}
		}
	}
}
