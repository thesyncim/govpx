package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

func BenchmarkHadamardScoreTransformNative(b *testing.B) {
	for _, n := range []int{8, 16} {
		b.Run(fmt.Sprintf("tx%d", n), func(b *testing.B) {
			var src [4096]int16
			var coeff [256]int16
			for i := range src {
				src[i] = int16(i*13%511 - 255)
			}
			b.ReportAllocs()
			for b.Loop() {
				hadamardBlockYrd(src[:], 64, coeff[:], n)
			}
			if coeff[0] == 32767 {
				b.Fatal("unexpected DC")
			}
		})
	}
}

func BenchmarkHadamardScoreConsumerNative(b *testing.B) {
	for _, n := range []int{8, 16} {
		for _, bw := range []int{32, 64} {
			for pattern := range 3 {
				b.Run(fmt.Sprintf("tx%d/block%d/pattern%d", n, bw, pattern), func(b *testing.B) {
					src, dst := make([]byte, bw*bw), make([]byte, bw*bw)
					for i := range src {
						src[i] = byte(i * 13)
						dst[i] = byte(i * 7)
						if pattern == 0 {
							dst[i] = src[i]
						}
						if pattern == 1 {
							src[i] = 129
							dst[i] = 128
						}
					}
					var scratch [16384]int16
					tx := common.Tx8x8
					if n == 16 {
						tx = common.Tx16x16
					}
					b.ReportAllocs()
					for b.Loop() {
						benchmarkBlockYrdResult = BlockYrd(src, bw, 0, 0, dst, bw, 0, 0, bw, bw, tx, [2]int16{100, 127}, 123456, scratch[:])
					}
					if !benchmarkBlockYrdResult.Valid {
						b.Fatal("invalid score")
					}
				})
			}
		}
	}
}
