package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestAlgo129Round3EOBCount(t *testing.T) {
	for n := 0; n < 300; n++ {
		tokens := make([]TokenExtra, n)
		for i := range tokens {
			tokens[i] = TokenExtra{Token: EobToken, ProbOff: uint16((i * 107 % (coefProbsFlatLen / UnconstrainedNodes)) * UnconstrainedNodes)}
		}
		if n%5 == 0 && n > 0 {
			tokens[n/2].ProbOff = 65535
		}
		if n%7 == 0 && n > 0 {
			tokens[n/2].Token = OneToken
		}
		var a, b FrameCoefBranchStats
		a[0][0][0][0][0][0][0] = ^uint32(0)
		b = a
		ga := CountCoefEOBTokens(tokens, &a)
		gb := algo129Round3FrozenEOBCount(tokens, &b)
		if ga != gb || a != b {
			t.Fatalf("n%d", n)
		}
	}
	if CountCoefEOBTokens(nil, nil) {
		t.Fatal("nil")
	}
}

var algo129Round3Stats FrameCoefBranchStats

func BenchmarkAlgo129Round3EOBCount(b *testing.B) {
	for _, n := range []int{1, 16, 256, 4096} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			tokens := make([]TokenExtra, n)
			for i := range tokens {
				tokens[i] = TokenExtra{Token: EobToken, ProbOff: uint16((i * 107 % (coefProbsFlatLen / UnconstrainedNodes)) * UnconstrainedNodes)}
			}
			b.ReportAllocs()
			for b.Loop() {
				CountCoefEOBTokens(tokens, &algo129Round3Stats)
			}
		})
	}
}

func algo129Round3FrozenEOBCount(tokens []TokenExtra, stats *FrameCoefBranchStats) bool {
	if stats == nil {
		return false
	}
	for _, tok := range tokens {
		probOff := int(tok.ProbOff)
		if tok.Token != EobToken || probOff%UnconstrainedNodes != 0 ||
			probOff < 0 || probOff >= coefProbsFlatLen {
			return false
		}
		row := probOff / UnconstrainedNodes
		ctx := row % vp9dec.CoefContexts
		row /= vp9dec.CoefContexts
		band := row % vp9dec.CoefBands
		row /= vp9dec.CoefBands
		ref := row % vp9dec.CoefRefTypes
		row /= vp9dec.CoefRefTypes
		plane := row % vp9dec.CoefPlaneTypes
		row /= vp9dec.CoefPlaneTypes
		if row < 0 || row >= int(common.TxSizes) {
			return false
		}
		recordCoefBranch00(&stats[row][plane][ref][band][ctx])
	}
	return true
}

// Frozen from production870699ce; candidate source never generates expectations.
