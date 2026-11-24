package govpx

import (
	"fmt"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestVP8KeyframeCoefCostsReference(t *testing.T) {
	for pat := range 5 {
		var c [16]int16
		for i := range c {
			switch pat {
			case 0:
			case 1:
				c[i] = 1
			case 2:
				c[i] = int16(i*13 - 100)
			case 3:
				c[i] = int16(i*317 - 2048)
			case 4:
				c[i] = 32767
			}
		}
		for ty := -1; ty <= 4; ty++ {
			for ctx := -1; ctx <= 3; ctx++ {
				for dc := -1; dc <= 2; dc++ {
					for eob := -1; eob <= 17; eob++ {
						got := coefficientBlockTokenRate(&vp8tables.DefaultCoefProbs, nil, ty, ctx, dc, &c, eob)
						want := vp8enc.CoefficientBlockTokenRate(&vp8tables.DefaultCoefProbs, ty, ctx, dc, &c, eob)
						if got != want {
							t.Fatalf("pat%d ty%d ctx%d dc%d eob%d %d!=%d", pat, ty, ctx, dc, eob, got, want)
						}
					}
				}
			}
		}
	}
}

var vp8CoefCacheSink int

func BenchmarkVP8KeyframeCoefCosts(b *testing.B) {
	for _, pat := range []int{0, 1, 2} {
		b.Run(fmt.Sprint(pat), func(b *testing.B) {
			var c [16]int16
			for i := range c {
				if pat == 1 {
					c[i] = 1
				} else if pat == 2 {
					c[i] = int16(i*13 - 100)
				}
			}
			eob := 16
			if pat == 0 {
				eob = 0
			}
			i, sum := 0, 0
			b.ReportAllocs()
			for b.Loop() {
				sum += coefficientBlockTokenRate(&vp8tables.DefaultCoefProbs, nil, 3, i%3, 0, &c, eob)
				i++
			}
			vp8CoefCacheSink = sum
		})
	}
}
