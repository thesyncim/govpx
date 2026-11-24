package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"testing"
)

var densePrivateSink uint64

func BenchmarkDenseTokenPrivatePrepack(b *testing.B) {
	for _, pattern := range []string{"DenseMixed", "DenseSmall", "Gappy"} {
		var q [16]int16
		for i := range q {
			switch pattern {
			case "DenseMixed":
				q[i] = []int16{0, 1, -2, 4, -5, 6, -7, 10, -11, 18, -19, 34, -35, 66, -67, 2048}[i]
			case "DenseSmall":
				q[i] = int16(i%9 - 4)
			case "Gappy":
				if i == 15 {
					q[i] = -2048
				}
			}
		}
		var want [16]uint32
		for i, c := range q {
			want[i], _ = denseOracleBase(c)
		}
		for _, kind := range []string{"ScalarLUTAtomic", "ScalarLUTStream", "NativeCLZ"} {
			b.Run(pattern+"/"+kind, func(b *testing.B) {
				if kind == "NativeCLZ" && (!denseTokenAvailable || !pixel.Enabled) {
					b.Skip("ARM SIMD required")
				}
				var out [16]uint32
				b.ReportAllocs()
				if kind == "NativeCLZ" {
					if !denseTokenPrepack(&q, &out) || out != want {
						b.Fatal("prepack oracle")
					}
					for b.Loop() {
						if !denseTokenPrepack(&q, &out) {
							b.Fatal("declined")
						}
					}
				} else if kind == "ScalarLUTStream" {
					if !denseTokenPrepackScalarStream(&q, &out) || out != want {
						b.Fatal("prepack oracle")
					}
					for b.Loop() {
						if !denseTokenPrepackScalarStream(&q, &out) {
							b.Fatal("declined")
						}
					}
				} else {
					if !denseTokenPrepackScalar(&q, &out) || out != want {
						b.Fatal("prepack oracle")
					}
					for b.Loop() {
						if !denseTokenPrepackScalar(&q, &out) {
							b.Fatal("declined")
						}
					}
				}

				if out != want {
					b.Fatal("after oracle")
				}
				for _, v := range out {
					densePrivateSink += uint64(v)
				}
			})
		}
	}
}
