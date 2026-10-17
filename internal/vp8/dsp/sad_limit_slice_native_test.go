package dsp

import (
	"fmt"
	"testing"
)

func BenchmarkSADLimitSliceNative(b *testing.B) {
	for _, pitch := range []int{32, 1312} {
		for _, extreme := range []bool{false, true} {
			for _, tc := range []struct {
				name  string
				limit int
			}{
				{"bound", 16 * 16 * 255}, {"wide", 1 << 20}, {"near", 16*16*255 - 1}, {"early", 1024}, {"middle", 16384},
			} {
				b.Run(fmt.Sprintf("pitch%d/extreme%v/%s", pitch, extreme, tc.name), func(b *testing.B) {
					ss, rs := pitch, pitch+32
					a, ref := make([]byte, 3+15*ss+16)[3:], make([]byte, 5+15*rs+16)[5:]
					for i := range a {
						if !extreme {
							a[i] = byte(i*37 + 11)
						}
					}
					for i := range ref {
						if extreme {
							ref[i] = 255
						} else {
							ref[i] = byte(i*19 + 53)
						}
					}
					want := sadLimitRowsOracle(a, ss, ref, rs, 16, tc.limit)
					got := 0
					b.ReportAllocs()
					for b.Loop() {
						got = SAD16x16Limit(a, ss, ref, rs, tc.limit)
					}
					if got != want {
						b.Fatalf("got %d want %d", got, want)
					}
					sadLimitNativeSink = got
				})
			}
		}
	}
}
