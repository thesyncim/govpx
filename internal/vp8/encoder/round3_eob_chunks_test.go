package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func round3EOBChunksCapture(fn func()) (pan string) {
	defer func() {
		if p := recover(); p != nil {
			pan = fmt.Sprint(p)
		}
	}()
	fn()
	return
}

func round3EOBChunksFrozen(qcoeff *[16]int16, skipDC int) int {
	for pos := 15; pos >= skipDC; pos-- {
		rc := int(tables.DefaultZigZag1D[pos])
		if qcoeff[rc] != 0 {
			return pos + 1
		}
	}
	return skipDC
}

func TestRound3EOBChunks(t *testing.T) {
	for _, skip := range []int{-1, 0, 1, 2, 15, 16, 17} {
		for pos := -1; pos < 16; pos++ {
			var c [16]int16
			if pos >= 0 {
				c[pos] = 1
			}
			for _, v := range []int16{1, -1, 32767, -32768} {
				if pos >= 0 {
					c[pos] = v
				}
				a, b := 0, 0
				ap := round3EOBChunksCapture(func() { a = BlockCoeffEOB(&c, skip) })
				bp := round3EOBChunksCapture(func() { b = round3EOBChunksFrozen(&c, skip) })
				if a != b || ap != bp {
					t.Fatalf("skip%d pos%d got%d/%q want%d/%q", skip, pos, a, ap, b, bp)
				}
			}
		}
	}
}

var round3EOBChunksSink uint64

func BenchmarkRound3EOBChunks(b *testing.B) {
	for _, pos := range []int{-1, 0, 15} {
		b.Run(fmt.Sprintf("pos%d", pos), func(b *testing.B) {
			var c [16]int16
			if pos >= 0 {
				c[pos] = 7
			}
			v := 0
			b.ReportAllocs()
			for b.Loop() {
				v = BlockCoeffEOB(&c, 0)
			}
			if v != round3EOBChunksFrozen(&c, 0) {
				b.Fatal("frozen mismatch")
			}
			round3EOBChunksSink += uint64(v)
		})
	}
}
