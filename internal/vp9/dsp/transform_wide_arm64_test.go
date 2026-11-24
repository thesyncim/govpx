//go:build goexperiment.simd && arm64 && !purego

package dsp

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/pixel"
	"math/rand/v2"
	"testing"
)

func TestARM64InverseDCTFullSignedDomain(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("native SIMD unavailable")
	}
	r := rand.New(rand.NewPCG(9919, 21213))
	for _, n := range []int{8} {
		for _, rows := range []int{4, 8, 16} {
			if rows > n {
				continue
			}
			for trial := range 180 {
				in := make([]int16, n*rows+1)[1:]
				for i := range in {
					v := int16(r.Uint32())
					if trial < 3 {
						v = -32768
						if trial == 1 || trial == 2 && i%2 == 0 {
							v = 32767
						}
					}
					in[i] = v
				}
				stride := n + 3
				got := make([]byte, (n-1)*stride+n+1)[1:]
				for i := range got {
					got[i] = byte(r.Uint32())
				}
				want := append([]byte(nil), got...)
				if !inverseDCTWideGo(in, got, stride, n, rows) {
					t.Fatal("wide dispatch unavailable")
				}
				if n == 8 {
					if rows == 4 {
						idct8x8_12AddScalar(in, want, stride)
					} else {
						idct8x8_64AddScalar(in, want, stride)
					}
				} else {
					switch rows {
					case 4:
						idct16x16_10AddScalar(in, want, stride)
					case 8:
						idct16x16_38AddScalar(in, want, stride)
					default:
						idct16x16_256AddScalar(in, want, stride)
					}
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("n=%d rows=%d trial=%d", n, rows, trial)
				}
			}
		}
	}
}
