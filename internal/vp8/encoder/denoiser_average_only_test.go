package encoder

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/pixel"
	"testing"
)

func TestDenoiserAverageOnlyMatchesScalar(t *testing.T) {
	handledCases := 0
	for _, stride := range []int{16, 23} {
		for _, motion := range []uint32{0, 24, 25, 100} {
			for _, increase := range []bool{false, true} {
				for seed := range 35 {
					mc, sig, avg := make([]byte, 16*stride), make([]byte, 16*stride), make([]byte, 16*stride)
					for i := range sig {
						sig[i] = byte(i*31 + seed*17)
						mc[i] = byte(int(sig[i]) + seed - 17)
						avg[i] = byte(i*19 + seed)
					}
					before := append([]byte(nil), sig...)
					wantSig := append([]byte(nil), sig...)
					wantAvg := append([]byte(nil), avg...)
					want := DenoiserFilterYScalar(mc, stride, wantAvg, stride, wantSig, stride, motion, increase)
					got, handled := DenoiserFilterYAverageOnly(mc, stride, avg, stride, sig, stride, motion, increase)
					if !handled {
						if !bytes.Equal(sig, before) {
							t.Fatal("decline signal write")
						}
						continue
					}
					handledCases++
					if got != want || !bytes.Equal(avg, wantAvg) || !bytes.Equal(sig, before) {
						t.Fatalf("stride=%d motion=%d increase=%t seed=%d got=%d want=%d", stride, motion, increase, seed, got, want)
					}
				}
			}
		}
	}
	if pixel.Enabled && handledCases == 0 {
		t.Fatal("average-only SIMD was never reached")
	}
}
