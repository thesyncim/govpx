package pixel

import (
	"math/rand/v2"
	"testing"
)

func TestSumSSE8x8GridMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 99))
	for _, tc := range []struct {
		w, h, ss, rs int
	}{
		{16, 8, 16, 16},
		{16, 16, 24, 32},
		{32, 32, 64, 48},
		{64, 64, 80, 72},
	} {
		src := make([]byte, tc.h*tc.ss+16)
		ref := make([]byte, tc.h*tc.rs+16)
		for i := range src {
			src[i] = byte(rng.Uint32())
		}
		for i := range ref {
			ref[i] = byte(rng.Uint32())
		}
		blocks := (tc.w >> 3) * (tc.h >> 3)
		gotSSE := make([]uint32, blocks)
		gotSum := make([]int, blocks)
		totalSSE, totalSum, ok := SumSSE8x8Grid(src, tc.ss, ref, tc.rs, tc.w, tc.h, gotSSE, gotSum)
		if Enabled && !ok {
			t.Fatalf("%dx%d: expected SIMD SumSSE8x8Grid ok=true", tc.w, tc.h)
		}
		if !ok {
			continue
		}
		var wantTotalSSE uint32
		wantTotalSum := 0
		k := 0
		for y := 0; y < tc.h; y += 8 {
			for x := 0; x < tc.w; x += 8 {
				var bSSE uint32
				bSum := 0
				for yy := range 8 {
					for xx := range 8 {
						d := int(src[(y+yy)*tc.ss+x+xx]) - int(ref[(y+yy)*tc.rs+x+xx])
						bSum += d
						bSSE += uint32(d * d)
					}
				}
				if gotSSE[k] != bSSE || gotSum[k] != bSum {
					t.Fatalf("%dx%d block %d: got (sse=%d, sum=%d), want (sse=%d, sum=%d)",
						tc.w, tc.h, k, gotSSE[k], gotSum[k], bSSE, bSum)
				}
				wantTotalSSE += bSSE
				wantTotalSum += bSum
				k++
			}
		}
		if totalSSE != wantTotalSSE || totalSum != wantTotalSum {
			t.Fatalf("%dx%d total: got (%d, %d), want (%d, %d)",
				tc.w, tc.h, totalSSE, totalSum, wantTotalSSE, wantTotalSum)
		}
	}
}
