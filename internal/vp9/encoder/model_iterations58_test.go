package encoder

import (
	"fmt"
	"testing"
)

func modelRDFrozen58(varN uint32, nLog2 uint, qstep uint32) (rate int, dist int64) {
	if varN == 0 {
		return 0, 0
	}
	xsqQ1064 := ((uint64(qstep) * uint64(qstep)) << (nLog2 + 10)) + uint64(varN>>1)
	xsqQ1064 /= uint64(varN)
	var xsqQ10 int
	if xsqQ1064 > maxXsqQ10 {
		xsqQ10 = maxXsqQ10
	} else {
		xsqQ10 = int(xsqQ1064)
	}
	rQ10, dQ10 := ModelRdNorm(xsqQ10)
	// libvpx ROUND_POWER_OF_TWO(x, n) == (x + (1<<(n-1))) >> n. VP9_PROB_COST_SHIFT==9
	// so n == 10 - 9 == 1; the +1 rounding adds 1 before shifting right by 1.
	const probCostShift = 9
	const round = 10 - probCostShift // == 1
	if round > 0 {
		rate = ((rQ10 << nLog2) + (1 << (round - 1))) >> round
	} else {
		rate = rQ10 << nLog2
	}
	dist = (int64(varN)*int64(dQ10) + 512) >> 10
	return
}
func TestModelRDIterations58(t *testing.T) {
	x := uint32(1873)
	for i := 0; i < 8192; i++ {
		x = x*1664525 + 1013904223
		v := x
		if i%7 == 0 {
			v = 0
		}
		if i%11 == 0 {
			v = uint32(i & 255)
		}
		x = x*1664525 + 1013904223
		q := x
		if i%3 == 0 {
			q = 0
		}
		if i%5 == 0 {
			q = uint32(i % 1024)
		}
		for _, n := range []uint{0, 4, 8, 10, 12, 32, 48, 63, 64} {
			gr, gd := ModelRDFromVarLapndz(v, n, q)
			wr, wd := modelRDFrozen58(v, n, q)
			if gr != wr || gd != wd {
				t.Fatalf("v%d n%d q%d got%d/%d want%d/%d", v, n, q, gr, gd, wr, wd)
			}
		}
	}
}

var modelRate58 int
var modelDist58 int64

func BenchmarkModelRDIteration58(b *testing.B) {
	for _, tc := range []struct {
		v uint32
		n uint
		q uint32
	}{{0, 8, 128}, {4096, 8, 0}, {1 << 24, 12, 0}, {1 << 24, 8, 4}, {65535, 8, 32}, {1, 8, 32}, {65535, 12, 1024}, {4096, 8, 61}, {4096, 8, 62}} {
		b.Run(fmt.Sprintf("v%d-n%d-q%d", tc.v, tc.n, tc.q), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				modelRate58, modelDist58 = ModelRDFromVarLapndz(tc.v, tc.n, tc.q)
			}
		})
	}
}
