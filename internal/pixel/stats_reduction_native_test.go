package pixel

import (
	"bytes"
	"fmt"
	"testing"
)

var statsReductionSink int

func BenchmarkStatsReductionNative(b *testing.B) {
	for _, w := range []int{8, 32, 64} {
		for _, h := range []int{1, 7, 8, 64, 4096, 4097} {
			b.Run(fmt.Sprintf("w%d/h%d", w, h), func(b *testing.B) {
				ss, rs := w+13, w+17
				a, r := make([]byte, (h-1)*ss+w+3), make([]byte, (h-1)*rs+w+5)
				for i := range a {
					a[i] = byte(i*37 + 11)
				}
				for i := range r {
					r[i] = byte(i*19 + 53)
				}
				aa, rr := bytes.Clone(a), bytes.Clone(r)
				wantSum, wantSSE := StatsScalar(a[3:], ss, r[5:], rs, w, h)
				b.ReportAllocs()
				var sum int
				var sse uint32
				for b.Loop() {
					sum, sse = Stats(a[3:], ss, r[5:], rs, w, h)
				}
				if sum != wantSum || sse != wantSSE || !bytes.Equal(a, aa) || !bytes.Equal(r, rr) {
					b.Fatal("publicStats mismatch/fullinput mutation")
				}
				statsReductionSink = sum + int(sse)
			})
		}
	}
}
