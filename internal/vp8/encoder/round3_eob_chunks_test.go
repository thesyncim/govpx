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

func round3MeanLumaFrozen(src SourceImage, mbRow int, mbCol int) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampSourceCoord(baseY+row, src.Height)
		for col := range 16 {
			srcX := clampSourceCoord(baseX+col, src.Width)
			v := int(src.Y[srcY*src.YStride+srcX])
			sum += v
			sse += v * v
		}
	}
	variance := sse - int((int64(sum)*int64(sum)+128)>>8)
	if variance < 0 {
		return 0
	}
	return variance
}

func TestRound3MeanLumaStats(t *testing.T) {
	for _, dim := range []int{1, 15, 16, 17, 32, 65} {
		for _, pad := range []int{0, 13} {
			stride := dim + pad
			src := SourceImage{Width: dim, Height: dim, YStride: stride, Y: make([]byte, stride*dim)}
			for i := range src.Y {
				src.Y[i] = byte(i*73 + 19)
			}
			for row := -1; row < 5; row++ {
				for col := -1; col < 5; col++ {
					a := MacroblockMeanLumaSSE(src, row, col)
					b := round3MeanLumaFrozen(src, row, col)
					if a != b {
						t.Fatalf("dim%d stride%d mb%d/%d", dim, stride, row, col)
					}
				}
			}
		}
	}
}

var round3MeanLumaSink uint64

func BenchmarkRound3MeanLumaStats(b *testing.B) {
	for _, dim := range []int{16, 17, 32} {
		b.Run(fmt.Sprintf("dim%d", dim), func(b *testing.B) {
			s := SourceImage{Width: dim, Height: dim, YStride: dim, Y: make([]byte, dim*dim)}
			for i := range s.Y {
				s.Y[i] = byte(i*17 + 29)
			}
			v := 0
			b.ReportAllocs()
			for b.Loop() {
				v = MacroblockMeanLumaSSE(s, 0, 0)
			}
			if v != round3MeanLumaFrozen(s, 0, 0) {
				b.Fatal("mean")
			}
			round3MeanLumaSink += uint64(v)
		})
	}
}
