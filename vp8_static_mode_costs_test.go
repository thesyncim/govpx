package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestVP8StaticModeCostsReference(t *testing.T) {
	for a := range 10 {
		for l := range 10 {
			for m := -1; m <= 10; m++ {
				got := bPredModeRate(true, vp8common.BPredictionMode(m), vp8common.BPredictionMode(a), vp8common.BPredictionMode(l))
				want := vp8enc.TreeTokenCost(vp8tables.BModeTree[:], vp8tables.KeyFrameBModeProbs[a][l][:], m)
				if got != want {
					t.Fatalf("b %d,%d,%d %d!=%d", a, l, m, got, want)
				}
			}
		}
	}
	for m := -1; m <= 6; m++ {
		mode := vp8common.MBPredictionMode(m)
		if got, want := intraYModeRate(true, mode), vp8enc.TreeTokenCost(vp8tables.KeyFrameYModeTree[:], vp8tables.KeyFrameYModeProbs[:], m); got != want {
			t.Fatalf("Y%d %d!=%d", m, got, want)
		}
		if got, want := intraUVModeRate(true, mode), vp8enc.TreeTokenCost(vp8tables.UVModeTree[:], vp8tables.KeyFrameUVModeProbs[:], m); got != want {
			t.Fatalf("UV%d %d!=%d", m, got, want)
		}
	}
}

var vp8ModeCostSink int

func BenchmarkVP8StaticModeCosts(b *testing.B) {
	for _, kind := range []string{"bmode", "ymode", "uvmode"} {
		b.Run(kind, func(b *testing.B) {
			i, sum := 0, 0
			b.ReportAllocs()
			for b.Loop() {
				switch kind {
				case "bmode":
					sum += bPredModeRate(true, vp8common.BPredictionMode(i%10), vp8common.BPredictionMode(i/10%10), vp8common.BPredictionMode(i/100%10))
				case "ymode":
					sum += intraYModeRate(true, vp8common.MBPredictionMode(i%5))
				case "uvmode":
					sum += intraUVModeRate(true, vp8common.MBPredictionMode(i%4))
				}
				i++
			}
			vp8ModeCostSink = sum
		})
	}
}
